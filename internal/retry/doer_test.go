package retry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDoerFunc is a lightweight HTTPDoer adapter for scripting inner
// responses in these tests; it is not shared outside this file.
type mockDoerFunc func(*http.Request) (*http.Response, error)

func (f mockDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// countingBody wraps an io.Reader as an io.ReadCloser that counts Close
// calls, so tests can assert an intermediate response body was closed
// exactly once before the next retry attempt.
type countingBody struct {
	io.Reader
	closeCount int
}

func (b *countingBody) Close() error {
	b.closeCount++
	return nil
}

// infiniteReader never returns EOF, simulating an oversized response body
// so tests can exercise Doer.Do's maxDrainBytes cap.
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// canceledBodyReader simulates a response body whose Read fails with a
// ctx-cancellation error, as net/http does when the request's ctx is
// canceled/expires mid-read.
type canceledBodyReader struct{}

func (canceledBodyReader) Read(_ []byte) (int, error) {
	return 0, context.Canceled
}

func newTestRequest(ctx context.Context, t *testing.T, method string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, "http://example.com/xrpc/test", body)
	require.NoError(t, err)
	return req
}

func defaultPolicy() Policy {
	return Policy{MaxRetries: 5, BaseDelay: time.Second, MaxDelay: 30 * time.Second}
}

func TestDoer_Do_SuccessOnFirstAttempt_NoRetry(t *testing.T) {
	calls := 0
	clock := &fakeClock{}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}), defaultPolicy(), clock)

	resp, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, calls)
	assert.Empty(t, clock.SleepCalls)
}

func TestDoer_Do_TransientFailures_RetriesThenSucceeds(t *testing.T) {
	tests := []struct {
		name string
		fail func() (*http.Response, error)
	}{
		{
			name: "transport_error",
			fail: func() (*http.Response, error) { return nil, errors.New("connection reset") },
		},
		{
			name: "429",
			fail: func() (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
			},
		},
		{
			name: "5xx",
			fail: func() (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			clock := &fakeClock{}
			doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
				calls++
				if calls <= 2 {
					return tt.fail()
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			}), defaultPolicy(), clock)

			resp, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, 3, calls)
			assert.Len(t, clock.SleepCalls, 2)
		})
	}
}

func TestDoer_Do_MaxRetriesExceeded_ReturnsLastFailure(t *testing.T) {
	calls := 0
	clock := &fakeClock{}
	policy := Policy{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
	}), policy, clock)

	resp, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, policy.MaxRetries+1, calls)
	assert.Len(t, clock.SleepCalls, policy.MaxRetries)
}

// permanentTestError is a test-only error implementing the unexported
// permanentError interface (Permanent() bool), representing a failure like
// *atproto.SSRFError that must never be retried.
type permanentTestError struct{ msg string }

func (e *permanentTestError) Error() string   { return e.msg }
func (e *permanentTestError) Permanent() bool { return true }

func TestDoer_Do_PermanentFailures_NotRetried(t *testing.T) {
	tests := []struct {
		name       string
		handler    func() (*http.Response, error)
		wantErr    bool
		wantStatus int
	}{
		{
			name: "401",
			handler: func() (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(""))}, nil
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "403_non_429_4xx",
			handler: func() (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(""))}, nil
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "permanent_error",
			handler: func() (*http.Response, error) {
				return nil, &permanentTestError{msg: "ssrf rejected"}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			clock := &fakeClock{}
			doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
				calls++
				return tt.handler()
			}), defaultPolicy(), clock)

			resp, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

			assert.Equal(t, 1, calls)
			assert.Empty(t, clock.SleepCalls)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

func TestDoer_Do_ExponentialBackoffCappedAtMaxDelay(t *testing.T) {
	clock := &fakeClock{}
	policy := Policy{MaxRetries: 6, BaseDelay: time.Second, MaxDelay: 10 * time.Second}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
	}), policy, clock)

	_, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.NoError(t, err)
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
	assert.Equal(t, want, clock.SleepCalls)
}

func TestDoer_Do_LargeRetryAfterCappedAtMaxDelay(t *testing.T) {
	clock := &fakeClock{}
	policy := Policy{MaxRetries: 1, BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
		resp.Header.Set("Retry-After", "3600")
		return resp, nil
	}), policy, clock)

	_, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.NoError(t, err)
	require.Len(t, clock.SleepCalls, 1)
	assert.Equal(t, policy.MaxDelay, clock.SleepCalls[0])
}

func TestDoer_Do_NonPositiveRetryAfterFallsBackToExponential(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"negative_seconds", "-5"},
		{"zero_seconds", "0"},
		{"past_http_date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &fakeClock{}
			policy := Policy{MaxRetries: 1, BaseDelay: 2 * time.Second, MaxDelay: 30 * time.Second}
			doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
				resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
				resp.Header.Set("Retry-After", tt.header)
				return resp, nil
			}), policy, clock)

			_, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

			require.NoError(t, err)
			require.Len(t, clock.SleepCalls, 1)
			assert.Equal(t, policy.BaseDelay, clock.SleepCalls[0])
		})
	}
}

func TestDoer_Do_DrainsAndClosesIntermediateBody_OnNormalEOF(t *testing.T) {
	body := &countingBody{Reader: strings.NewReader("some error body")}
	calls := 0
	clock := &fakeClock{}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}), Policy{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}, clock)

	resp, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, body.closeCount)
}

func TestDoer_Do_DiscardCapExceeded_ClosesWithoutFullDrain(t *testing.T) {
	body := &countingBody{Reader: infiniteReader{}}
	calls := 0
	clock := &fakeClock{}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}), Policy{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}, clock)

	resp, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, body.closeCount)
}

func TestDoer_Do_CtxCanceledDuringBodyDrain_ReturnsCtxErrImmediately(t *testing.T) {
	body := &countingBody{Reader: canceledBodyReader{}}
	calls := 0
	clock := &fakeClock{}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body}, nil
	}), defaultPolicy(), clock)

	_, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, body.closeCount)
	assert.Empty(t, clock.SleepCalls)
}

func TestDoer_Do_RetriesResendFreshBodyFromGetBody(t *testing.T) {
	calls := 0
	var bodies []string
	clock := &fakeClock{}
	doer := NewDoer(mockDoerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(data))
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}), Policy{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Second}, clock)

	req := newTestRequest(context.Background(), t, http.MethodPost, strings.NewReader("payload"))
	require.NotNil(t, req.GetBody)

	_, err := doer.Do(req)

	require.NoError(t, err)
	assert.Equal(t, []string{"payload", "payload"}, bodies)
}

func TestDoer_Do_CtxCanceledDuringSleep_ReturnsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clock := &fakeClock{}
	calls := 0
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
	}), defaultPolicy(), clock)

	_, err := doer.Do(newTestRequest(ctx, t, http.MethodGet, nil))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
	assert.Empty(t, clock.SleepCalls)
}

func TestDoer_Do_LogsRetryAttempt(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	calls := 0
	clock := &fakeClock{}
	doer := NewDoer(mockDoerFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}), Policy{MaxRetries: 3, BaseDelay: time.Second, MaxDelay: 30 * time.Second}, clock)

	_, err := doer.Do(newTestRequest(context.Background(), t, http.MethodGet, nil))

	require.NoError(t, err)
	logged := buf.String()
	assert.Contains(t, logged, "attempt=1")
	assert.Contains(t, logged, "method=GET")
	assert.Contains(t, logged, "url=http://example.com/xrpc/test")
	assert.Contains(t, logged, "wait=1s")
}
