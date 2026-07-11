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

// defaultRequestTimeout bounds a single webhook HTTP call.
const defaultRequestTimeout = 3 * time.Second

// defaultRetryPolicy is deliberately lighter than internal/atproto's
// retry.Policy: a Slack notification failure is non-fatal (it never
// affects the CLI's own exit code), so this package trades a higher chance
// of eventually succeeding for a bounded, short worst-case delay instead.
var defaultRetryPolicy = retry.Policy{
	MaxRetries: 2,
	BaseDelay:  time.Second,
	MaxDelay:   4 * time.Second,
}

// cancelOnCloseBody wraps an io.ReadCloser so that Close() also calls the
// per-attempt cancel func after closing the underlying body. This lets
// perAttemptTimeoutDoer keep the per-attempt context alive while the retry
// loop drains the body for reuse (AC-08/AC-09), and only cancels once the
// body is fully consumed (AC-11).
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
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
//
// Unlike the previous implementation which called cancel() on Do return
// (via defer), this version ties cancellation to the response body's
// Close(): the per-attempt context stays alive while the retry loop drains
// the body or reads it for success/failure determination. When Do returns
// an error, or the response/body is nil, cancellation happens immediately
// since there is nothing to read.
type perAttemptTimeoutDoer struct {
	inner   HTTPDoer
	timeout time.Duration
}

func (d perAttemptTimeoutDoer) Do(req *http.Request) (_ *http.Response, retErr error) {
	ctx, cancel := context.WithTimeout(req.Context(), d.timeout)
	defer func() {
		if retErr != nil {
			cancel()
		}
	}()

	resp, err := d.inner.Do(req.Clone(ctx))
	if err != nil || resp == nil || resp.Body == nil {
		cancel()
		return resp, err
	}

	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
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
	return send(ctx, cfg, doer, clock, outcome, defaultRequestTimeout, defaultRetryPolicy)
}

// send is Send's implementation, with requestTimeout and retryPolicy taken
// as parameters (rather than package-level vars) so notify_test.go can
// inject test-only values without mutating shared package state.
func send(
	ctx context.Context,
	cfg Config,
	doer HTTPDoer,
	clock retry.Clock,
	outcome Outcome,
	requestTimeout time.Duration,
	retryPolicy retry.Policy,
) error {
	failed := isFailure(outcome)

	dest := cfg.SuccessWebhookURL
	if failed {
		dest = cfg.FailureWebhookURL
	}

	webhookURL := dest.Reveal()
	if webhookURL == "" {
		return nil
	}

	body, err := json.Marshal(buildPayload(outcome))
	if err != nil {
		return &SendError{Err: err}
	}

	timeoutDoer := perAttemptTimeoutDoer{inner: doer, timeout: requestTimeout}
	redactedDoer := retry.NewDoer(timeoutDoer, retryPolicy, clock, retry.WithURLRedactor(redactWebhookURL))

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
