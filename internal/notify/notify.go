package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/isseis/bsky-cleaner/internal/retry"
)

// HTTPDoer is the minimal HTTP interface this package sends webhook
// requests through. Declared independently (structurally identical to
// atproto.HTTPDoer / retry.HTTPDoer), matching this codebase's existing
// convention of a package-local HTTPDoer at each package boundary.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config holds the two webhook destinations. Either field may be the zero
// config.SecretString, meaning "not configured" -- Send skips delivery to
// that channel in that case.
type Config struct {
	SuccessWebhookURL config.SecretString
	FailureWebhookURL config.SecretString
}

// SendError identifies a webhook delivery failure without exposing the
// webhook URL itself (which internal/config treats as a secret). Its
// Error() text never embeds the request URL or a raw net/http/net/url
// error string; callers must render user/log-facing text via Error() only,
// never via Unwrap()'s message.
type SendError struct {
	StatusCode int // 0 for transport-level failures (no response received)
	Err        error
}

func (e *SendError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("slack notify: send failed: %s", errorKind(e.Err))
	}
	return fmt.Sprintf("slack notify: send failed: HTTP status %d", e.StatusCode)
}

func (e *SendError) Unwrap() error {
	return e.Err
}

// requestTimeout bounds a single webhook HTTP call. It is a package
// variable (not a const) purely for test injection -- notify_test.go
// overrides it to exercise the timeout path without a real multi-second
// wait, restoring the original value via t.Cleanup.
var requestTimeout = 3 * time.Second

// defaultRetryPolicy is deliberately lighter than internal/atproto's
// retry.Policy: a Slack notification failure is non-fatal (it never
// affects the CLI's own exit code), so this package trades a higher chance
// of eventually succeeding for a bounded, short worst-case delay instead.
var defaultRetryPolicy = retry.Policy{
	MaxRetries: 2,
	BaseDelay:  time.Second,
	MaxDelay:   4 * time.Second,
}

// webhookPayload is the Slack Incoming Webhook request body: the most
// basic supported shape, a single mrkdwn "text" field. Richer Block Kit
// elements are out of scope (see architecture doc section 3.3).
type webhookPayload struct {
	Text string `json:"text"`
}

// perAttemptTimeoutDoer gives every attempt its own fresh requestTimeout
// deadline, derived from the attempt request's context. internal/retry's
// Doer clones the same original request (and its context) for every retry
// attempt (see internal/retry/doer.go's cloneForAttempt), so without this
// wrapper a single context.WithTimeout applied once before entering the
// retry loop would have its deadline shared across all attempts: once it
// expired (which a single slow-but-not-hung request can trigger on its
// own), every later attempt and backoff sleep would fail immediately with
// a ctx error instead of getting its own chance to succeed, defeating the
// "requestTimeout per attempt" worst-case model in the architecture doc
// (section 3.6).
type perAttemptTimeoutDoer struct {
	inner   HTTPDoer
	timeout time.Duration
}

func (d perAttemptTimeoutDoer) Do(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(req.Context(), d.timeout)
	defer cancel()
	return d.inner.Do(req.Clone(ctx))
}

// Send builds a Slack payload from outcome, selects the destination
// webhook URL (failure channel if outcome.Err != nil or any delete
// failures occurred, success channel otherwise), and posts it via an
// internal/retry.Doer built from doer/defaultRetryPolicy/clock. It returns
// nil when notification is skipped because the selected destination is not
// configured. clock is exposed as a parameter (not hidden behind
// retry.RealClock{} internally) so this package's own tests can inject a
// fake retry.Clock without incurring real backoff waits; production
// callers (cmd/main.go) pass retry.RealClock{}.
func Send(ctx context.Context, cfg Config, doer HTTPDoer, clock retry.Clock, outcome Outcome) error {
	failed := outcome.Err != nil || (outcome.Result != nil && len(outcome.Result.Failed) > 0)

	dest := cfg.SuccessWebhookURL
	if failed {
		dest = cfg.FailureWebhookURL
	}

	webhookURL := dest.Reveal()
	if webhookURL == "" {
		return nil
	}

	body, err := json.Marshal(webhookPayload{Text: buildPayload(outcome)})
	if err != nil {
		return &SendError{Err: err}
	}

	timeoutDoer := perAttemptTimeoutDoer{inner: doer, timeout: requestTimeout}
	redactedDoer := retry.NewDoer(timeoutDoer, defaultRetryPolicy, clock, retry.WithURLRedactor(redactWebhookURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return &SendError{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	resp, err := redactedDoer.Do(req)
	if err != nil {
		return &SendError{Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &SendError{StatusCode: resp.StatusCode, Err: fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)}
	}
	return nil
}

// redactWebhookURL overrides retry.Doer's retry/give-up log URL text with
// the webhook's host only, so a Slack Incoming Webhook token embedded in
// the URL path never reaches logs (architecture doc section 3.6.1).
func redactWebhookURL(req *http.Request) string {
	return fmt.Sprintf("%s://%s/[REDACTED]", req.URL.Scheme, req.URL.Host)
}
