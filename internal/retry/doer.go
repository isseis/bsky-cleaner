package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// HTTPDoer is the minimal HTTP interface this package retries. It is
// structurally identical to atproto.HTTPDoer but declared independently so
// this package has no dependency on internal/atproto.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Policy bounds retry behavior for a single logical HTTP call: MaxRetries
// additional attempts beyond the first, exponential backoff starting at
// BaseDelay and doubling on every subsequent attempt, capped at MaxDelay
// regardless of the computed backoff or a server-supplied Retry-After
// value.
type Policy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// permanentError is implemented by errors that must never be retried
// regardless of their transport-level shape (e.g. an SSRF rejection --
// retrying would not help, since the same verified-address check would
// reject it again). Doer checks for this via a plain type assertion, so
// callers do not need to import this package's types to opt out of retry.
type permanentError interface {
	Permanent() bool
}

// maxDrainBytes bounds how much of an intermediate (retryable) response
// body Doer.Do reads before giving up on draining it for connection reuse,
// so a hostile or buggy server cannot make a retry loop spend unbounded
// time discarding an oversized 429/5xx body.
const maxDrainBytes = 64 * 1024

// Doer wraps an HTTPDoer, retrying transient failures (transport errors,
// HTTP 429, HTTP 5xx) per policy, and never retrying an error satisfying
// the unexported permanentError interface or an HTTP status outside the
// retryable set (in particular 401 and other non-429 4xx).
type Doer struct {
	inner  HTTPDoer
	policy Policy
	clock  Clock
	redact func(*http.Request) string
}

// Option customizes a Doer built by NewDoer beyond Policy/Clock.
type Option func(*Doer)

// WithURLRedactor overrides the URL text Doer's own retry/give-up logging
// emits, for callers whose request URL itself carries a secret (e.g. a
// Slack Incoming Webhook token embedded in the path). Callers that do not
// need this (internal/atproto's existing usage) omit it, preserving the
// current req.URL.String() logging unchanged.
func WithURLRedactor(redact func(*http.Request) string) Option {
	return func(d *Doer) { d.redact = redact }
}

// NewDoer builds a Doer wrapping inner per policy, using clock to wait
// between attempts.
func NewDoer(inner HTTPDoer, policy Policy, clock Clock, opts ...Option) *Doer {
	d := &Doer{inner: inner, policy: policy, clock: clock}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Do implements HTTPDoer, retrying per d.policy and d.clock. It never
// returns a nil response together with a nil error: exactly one of the two
// non-permanent outcomes (an error, or a response the caller must Close)
// is returned on every path.
func (d *Doer) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	for attempt := 0; ; attempt++ {
		attemptReq, err := cloneForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}

		resp, doErr := d.inner.Do(attemptReq)

		retryable, retryAfter, outcomeResp, outcomeErr := classify(resp, doErr)
		if !retryable {
			return outcomeResp, outcomeErr
		}

		if attempt >= d.policy.MaxRetries {
			// Give up: outcomeResp/outcomeErr is the final outcome, and
			// its body (if any) must reach the caller unread and unclosed
			// -- it must not be drained/closed like the intermediate
			// responses below.
			d.logGivingUp(req, attempt+1)
			return outcomeResp, outcomeErr
		}

		if resp != nil {
			if abortErr := drainAndClose(resp.Body); abortErr != nil {
				return nil, abortErr
			}
		}

		wait := backoffDelay(d.policy, attempt, retryAfter)

		if sleepErr := d.clock.Sleep(ctx, wait); sleepErr != nil {
			return nil, sleepErr
		}
		d.logRetrying(req, attempt+1, wait)
	}
}

// cloneForAttempt returns req unchanged on the first attempt (attempt ==
// 0). On a retry, it returns a clone carrying a fresh body obtained from
// req.GetBody, so a body already consumed by an earlier attempt is not
// resent empty.
func cloneForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 0 || req.GetBody == nil {
		return req, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("retry: rebuild request body for attempt %d: %w", attempt, err)
	}
	clone := req.Clone(req.Context())
	clone.Body = body
	return clone, nil
}

// classify determines whether the outcome of one attempt should be
// retried, and what retryAfter hint (if any, from a 429 response) applies.
// When retryable is false, outcomeResp/outcomeErr is the exact
// (response, error) pair Do should return: never retried permanent errors,
// non-retryable statuses (2xx, 401, or other non-429 4xx), and successes.
func classify(resp *http.Response, doErr error) (retryable bool, retryAfter time.Duration, outcomeResp *http.Response, outcomeErr error) {
	if doErr != nil {
		if permErr, ok := doErr.(permanentError); ok && permErr.Permanent() {
			return false, 0, nil, doErr
		}
		return true, 0, nil, doErr
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return true, parseRetryAfter(resp.Header.Get("Retry-After")), resp, nil
	case resp.StatusCode >= 500:
		return true, 0, resp, nil
	default:
		// 2xx, 3xx, and non-429 4xx (including 401, which never resolves
		// by retrying) are never retried.
		return false, 0, resp, nil
	}
}

// drainAndClose discards an intermediate (retryable) response's body up to
// maxDrainBytes so the underlying connection can be reused for the next
// attempt (net/http.Transport keep-alive requires the body be read to EOF
// and closed), then closes it. Reading past maxDrainBytes without reaching
// EOF still closes the body but forgoes connection reuse -- a deliberate
// trade-off against unbounded drain time/memory for an oversized body. A
// non-EOF read error that is not ctx-derived (e.g. the connection dropping
// mid-drain) is likewise treated as forgoing reuse only, since it says
// nothing about whether a fresh attempt would succeed. Only a ctx
// cancellation/deadline observed via the read (as net/http surfaces it) is
// returned, so the caller aborts the whole retry loop immediately instead
// of scheduling another attempt (the same "stop without delay" treatment
// as a Clock.Sleep cancellation) -- retrying would just repeat into the
// same dead ctx.
func drainAndClose(body io.ReadCloser) error {
	_, err := io.CopyN(io.Discard, body, maxDrainBytes+1)
	closeErr := body.Close()

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return closeErr
}

// backoffDelay computes the wait before the next attempt (attempt is
// 0-indexed, i.e. attempt 0 just finished). retryAfter is the parsed
// Retry-After hint from a 429 response (0 if none/invalid). The result is
// always capped at policy.MaxDelay.
func backoffDelay(policy Policy, attempt int, retryAfter time.Duration) time.Duration {
	wait := retryAfter
	if wait <= 0 {
		wait = policy.BaseDelay << attempt
		if wait <= 0 {
			// Overflowed time.Duration's range from excessive shifting;
			// treat as "at least as long as the cap".
			wait = policy.MaxDelay
		}
	}
	return min(wait, policy.MaxDelay)
}

// WorstCaseBackoff returns the total time an exhausted retry loop under this
// policy would spend sleeping between attempts, assuming no Retry-After
// hint is ever supplied (backoffDelay's own BaseDelay/MaxDelay exponential
// formula). Exported so callers that need a worst-case latency bound for
// guidance/sizing checks (e.g. internal/notify's execution_timeout_seconds
// test) exercise the real backoff formula instead of duplicating it.
func (p Policy) WorstCaseBackoff() time.Duration {
	var total time.Duration
	for attempt := range p.MaxRetries {
		total += backoffDelay(p, attempt, 0)
	}
	return total
}

// parseRetryAfter interprets a 429 response's Retry-After header value as
// either a delay in seconds or an HTTP-date, per RFC 9110 10.2.3. It
// returns 0 (meaning "no usable hint, fall back to exponential backoff")
// for a missing/unparseable header, or for a value that resolves to zero
// or negative -- a negative delay or a past HTTP-date -- since honoring
// either would mean retrying without any wait, defeating the point of a
// backoff.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil {
		if seconds <= 0 {
			return 0
		}
		return seconds
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// logRetrying emits a single observability line once a backoff wait has
// actually completed and another attempt will follow, so an on-call
// responder can tell an immediate failure apart from one that retried.
// completedAttempt is the 1-indexed attempt number that just failed. The
// URL never carries secrets for callers that omit WithURLRedactor: XRPC
// callers always send credentials via the request body or Authorization
// header, never as a query parameter. Callers whose URL itself is a
// secret (e.g. internal/notify's Slack webhook) supply redact via
// WithURLRedactor instead.
func (d *Doer) logRetrying(req *http.Request, completedAttempt int, wait time.Duration) {
	slog.Default().Warn("retrying HTTP request",
		"attempt", completedAttempt,
		"wait", wait,
		"method", req.Method,
		"url", d.urlText(req),
	)
}

// logGivingUp emits a single observability line when MaxRetries is
// exhausted and Do is about to return the last failure to the caller, so
// "retried until it succeeded" and "retried until it gave up" are each
// unambiguously visible in logs. completedAttempt is the 1-indexed attempt
// number of that final failure.
func (d *Doer) logGivingUp(req *http.Request, completedAttempt int) {
	slog.Default().Warn("giving up retrying HTTP request",
		"attempt", completedAttempt,
		"method", req.Method,
		"url", d.urlText(req),
	)
}

// urlText returns the URL text to use in retry/give-up logging: d.redact's
// output if set, otherwise the raw req.URL.String() (existing behavior).
func (d *Doer) urlText(req *http.Request) string {
	if d.redact != nil {
		return d.redact(req)
	}
	return req.URL.String()
}
