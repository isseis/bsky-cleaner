package retry

import (
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
}

// NewDoer builds a Doer wrapping inner per policy, using clock to wait
// between attempts.
func NewDoer(inner HTTPDoer, policy Policy, clock Clock) *Doer {
	return &Doer{inner: inner, policy: policy, clock: clock}
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

		if resp != nil {
			if drainErr := drainAndClose(resp.Body); drainErr != nil {
				return nil, drainErr
			}
		}

		if attempt >= d.policy.MaxRetries {
			return outcomeResp, outcomeErr
		}

		wait := backoffDelay(d.policy, attempt, retryAfter)
		logRetry(req, attempt+1, wait)

		if sleepErr := d.clock.Sleep(ctx, wait); sleepErr != nil {
			return nil, sleepErr
		}
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
// trade-off against unbounded drain time/memory for an oversized body. If
// the read fails because req's ctx was canceled/expired mid-drain, that
// error is returned so the caller aborts immediately instead of scheduling
// a retry (the same "stop without delay" treatment as a Clock.Sleep
// cancellation).
func drainAndClose(body io.ReadCloser) error {
	_, err := io.CopyN(io.Discard, body, maxDrainBytes+1)
	closeErr := body.Close()

	if err == nil || errors.Is(err, io.EOF) {
		// Either fully drained (EOF, possibly exactly at the cap) or the
		// cap was reached without EOF (CopyN returns nil error only when
		// it copies the full n bytes) -- both are the "give up on reuse,
		// but not a failure" case.
		return closeErr
	}
	return err
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

// logRetry emits a single observability line for a scheduled retry
// (including a final one that will be abandoned once MaxRetries is hit),
// so an on-call responder can tell an immediate failure apart from one
// that exhausted retries. The URL never carries secrets: XRPC callers
// always send credentials via the request body or Authorization header,
// never as a query parameter.
func logRetry(req *http.Request, nextAttempt int, wait time.Duration) {
	slog.Default().Warn("retrying HTTP request",
		"attempt", nextAttempt,
		"wait", wait,
		"method", req.Method,
		"url", req.URL.String(),
	)
}
