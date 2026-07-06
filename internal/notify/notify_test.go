package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/isseis/bsky-cleaner/internal/report"
	"github.com/isseis/bsky-cleaner/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSecretString(t *testing.T, value string) config.SecretString {
	t.Helper()
	return config.NewSecretStringForTest(value)
}

func succeededOutcome() Outcome {
	return Outcome{Result: &report.Result{Mode: report.ModeApply}}
}

func runErrorOutcome() Outcome {
	return Outcome{Err: assertErr}
}

var assertErr = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "boom" }

func partialFailureOutcome() Outcome {
	return Outcome{Result: &report.Result{
		Mode: report.ModeApply,
		Failed: []report.DeleteFailure{
			{Err: assertErr},
		},
	}}
}

func TestSend_Success_PostsToSelectedWebhook(t *testing.T) {
	bodyCh := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAll(r)
		bodyCh <- body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := Config{SuccessWebhookURL: newSecretString(t, server.URL)}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome())
	require.NoError(t, err)

	var gotBody []byte
	select {
	case gotBody = <-bodyCh:
	default:
		t.Fatal("handler was not invoked")
	}

	var payload webhookPayload
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	// Literal expected substring (not buildPayload(succeededOutcome())): a bug
	// in buildPayload itself must not go undetected just because both sides
	// of the comparison would share it.
	assert.Contains(t, payload.Text, "bsky-cleaner run succeeded: deleted 0 post(s).")
}

func TestSend_ChannelRouting_AllSucceeded_UsesSuccessURL(t *testing.T) {
	successServer := newRecordingServer(t, http.StatusOK)
	failureServer := newFatalServer(t)

	cfg := Config{
		SuccessWebhookURL: newSecretString(t, successServer.url),
		FailureWebhookURL: newSecretString(t, failureServer.url),
	}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome())
	require.NoError(t, err)
	assert.Equal(t, int32(1), successServer.count.Load())
}

func TestSend_ChannelRouting_RunError_UsesFailureURL(t *testing.T) {
	failureServer := newRecordingServer(t, http.StatusOK)
	successServer := newFatalServer(t)

	cfg := Config{
		SuccessWebhookURL: newSecretString(t, successServer.url),
		FailureWebhookURL: newSecretString(t, failureServer.url),
	}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, runErrorOutcome())
	require.NoError(t, err)
	assert.Equal(t, int32(1), failureServer.count.Load())
}

func TestSend_ChannelRouting_PartialFailure_UsesFailureURL(t *testing.T) {
	failureServer := newRecordingServer(t, http.StatusOK)
	successServer := newFatalServer(t)

	cfg := Config{
		SuccessWebhookURL: newSecretString(t, successServer.url),
		FailureWebhookURL: newSecretString(t, failureServer.url),
	}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, partialFailureOutcome())
	require.NoError(t, err)
	assert.Equal(t, int32(1), failureServer.count.Load())
}

func TestSend_SameWebhookURLForBothChannels_RoutesCorrectlyInBothOutcomes(t *testing.T) {
	server := newRecordingServer(t, http.StatusOK)
	cfg := Config{
		SuccessWebhookURL: newSecretString(t, server.url),
		FailureWebhookURL: newSecretString(t, server.url),
	}
	require.NoError(t, Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome()))
	require.NoError(t, Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, partialFailureOutcome()))
	assert.Equal(t, int32(2), server.count.Load())
}

func TestSend_SelectedWebhookURLEmpty_SkipsSendReturnsNil(t *testing.T) {
	server := newFatalServer(t)
	cfg := Config{SuccessWebhookURL: config.SecretString{}, FailureWebhookURL: newSecretString(t, server.url)}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome())
	require.NoError(t, err)
}

func TestSend_HTTPTimeout_ReturnsSendError(t *testing.T) {
	const testTimeout = 50 * time.Millisecond

	var count atomic.Int32
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		<-block
	}))
	// Cleanup runs LIFO: unblock the handler before Close, otherwise
	// Close would wait forever for the still-blocked in-flight request.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(block) })

	cfg := Config{SuccessWebhookURL: newSecretString(t, server.URL)}
	err := send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome(), testTimeout, defaultRetryPolicy)
	require.Error(t, err)
	sendErr, ok := errorsAsSendError(err)
	require.True(t, ok)
	assert.Equal(t, 0, sendErr.StatusCode)
	// Every attempt hangs (the handler never returns), so each individually
	// exhausts testTimeout: this asserts the retry loop actually made
	// MaxRetries+1 separate attempts rather than giving up after the first
	// once a shared deadline expired (see TestSend_EachRetryAttemptGetsFreshTimeout).
	assert.Equal(t, int32(defaultRetryPolicy.MaxRetries+1), count.Load())
}

// TestSend_EachRetryAttemptGetsFreshTimeout guards against a shared,
// single context.WithTimeout being applied once across the whole retry
// loop: internal/retry.Doer reuses the same request/context for every
// attempt (see internal/retry/doer.go's cloneForAttempt), so if Send
// applied requestTimeout to a ctx built before entering the retry loop,
// that one deadline would already be exhausted by the time the second
// attempt is made. Here the first two attempts each individually exceed
// requestTimeout (simulating slow-but-not-hung responses); with each
// attempt getting its own fresh timeout, a third fast attempt must still
// succeed within the retry policy's MaxRetries=2 budget.
func TestSend_EachRetryAttemptGetsFreshTimeout(t *testing.T) {
	const testTimeout = 100 * time.Millisecond

	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) <= 2 {
			time.Sleep(2 * testTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := Config{SuccessWebhookURL: newSecretString(t, server.URL)}
	clock := &fakeClock{}
	err := send(context.Background(), cfg, http.DefaultClient, clock, succeededOutcome(), testTimeout, defaultRetryPolicy)

	require.NoError(t, err)
	assert.Equal(t, int32(3), count.Load())
}

func TestSend_NonRetryableStatus_ReturnsSendErrorWithStatusCode(t *testing.T) {
	server := newRecordingServer(t, http.StatusBadRequest)
	cfg := Config{SuccessWebhookURL: newSecretString(t, server.url)}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome())
	require.Error(t, err)
	sendErr, ok := errorsAsSendError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, sendErr.StatusCode)
	assert.Equal(t, int32(1), server.count.Load())
}

func TestSend_RetriesTransientFailureThenSucceeds_UsesFakeClock(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := Config{SuccessWebhookURL: newSecretString(t, server.URL)}
	clock := &fakeClock{}
	err := Send(context.Background(), cfg, http.DefaultClient, clock, succeededOutcome())
	require.NoError(t, err)
	assert.Equal(t, int32(2), count.Load())
	assert.Len(t, clock.SleepCalls, 1)
}

func TestSend_MaxRetriesExceeded_ReturnsSendError_BoundedAttempts(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	cfg := Config{SuccessWebhookURL: newSecretString(t, server.URL)}
	clock := &fakeClock{}
	err := Send(context.Background(), cfg, http.DefaultClient, clock, succeededOutcome())
	require.Error(t, err)
	assert.Equal(t, int32(defaultRetryPolicy.MaxRetries+1), count.Load())
}

func TestSendError_Error_NeverContainsWebhookURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Hijack and close immediately to force a transport-level error.
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hj.Hijack()
		require.NoError(t, err)
		conn.Close()
	}))
	t.Cleanup(server.Close)

	secretPath := server.URL + "/services/T00000000/B00000000/supersecrettoken"
	cfg := Config{SuccessWebhookURL: newSecretString(t, secretPath)}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "supersecrettoken")
	assert.NotContains(t, err.Error(), secretPath)
}

func TestSend_RetryLog_UsesRedactedURL_NotRawWebhookURL(t *testing.T) {
	var buf bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	secretPath := server.URL + "/services/T00000000/B00000000/supersecrettoken"
	cfg := Config{SuccessWebhookURL: newSecretString(t, secretPath)}
	err := Send(context.Background(), cfg, http.DefaultClient, &fakeClock{}, succeededOutcome())
	require.NoError(t, err)

	logged := buf.String()
	assert.NotContains(t, logged, "supersecrettoken")
	assert.Contains(t, logged, "[REDACTED]")
}

func TestNotifyWorstCaseTime_BoundedBelowExecutionTimeoutGuidance(t *testing.T) {
	server := newRecordingServer(t, http.StatusInternalServerError)
	clock := &fakeClock{}
	cfg := Config{SuccessWebhookURL: newSecretString(t, server.url)}

	err := Send(context.Background(), cfg, http.DefaultClient, clock, succeededOutcome())
	require.Error(t, err)

	var backoff time.Duration
	for _, d := range clock.SleepCalls {
		backoff += d
	}
	worstCase := defaultRequestTimeout*time.Duration(defaultRetryPolicy.MaxRetries+1) + backoff

	assert.Equal(t, 12*time.Second, worstCase)
	// Recommended execution_timeout_seconds guidance (tens of seconds or
	// more) is an order of magnitude above this.
	assert.Less(t, worstCase, 30*time.Second)
}

// --- test helpers ---

type recordingServer struct {
	url   string
	count atomic.Int32
}

func newRecordingServer(t *testing.T, status int) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rs.count.Add(1)
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	rs.url = server.URL
	return rs
}

type fatalRecorder struct {
	url string
}

func newFatalServer(t *testing.T) *fatalRecorder {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.String())
	}))
	t.Cleanup(server.Close)
	return &fatalRecorder{url: server.URL}
}

func readAll(r *http.Request) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

func errorsAsSendError(err error) (*SendError, bool) {
	return errors.AsType[*SendError](err)
}

var _ retry.Clock = (*fakeClock)(nil)
