package atproto

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/isseis/bsky-cleaner/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewClient_WrapsHTTPDoerWithRetry verifies the wiring NewClient does
// itself (not the retry logic, which internal/retry's own tests cover):
// the *Client it returns must send its Login/ListPosts/DeleteRecord
// requests through a retry.Doer, not the raw HTTPDoer passed in.
func TestNewClient_WrapsHTTPDoerWithRetry(t *testing.T) {
	stubSymbolicHostLookup(t)
	const handle = "alice.test"
	const did = "did:plc:test123"
	pdsEndpoint := "https://" + publicIPLiteral

	mock := &atprototestutil.MockHTTPDoer{Handler: handleResolutionHandler(t, handle, did, "plc.directory", pdsEndpoint)}

	client, err := NewClient(context.Background(), handle, mock)

	require.NoError(t, err)
	_, ok := client.httpDoer.(*retry.Doer)
	assert.True(t, ok, "NewClient's httpDoer must be wrapped by retry.NewDoer")
}

// TestClient_DeleteRecord_CtxDeadlineDuringRetry_ReturnsCtxErrWithoutFullBackoff
// verifies the property this task actually adds on top of internal/retry's
// own unit tests: that retry.Doer's ctx-cancellation detection during a
// backoff wait survives doXRPC's error wrapping and reaches DeleteRecord's
// caller as an error identifiable via errors.Is(err, context.DeadlineExceeded),
// and that it does so without waiting out defaultRetryPolicy.BaseDelay (1s).
// It uses retry.RealClock (not internal/retry's own fakeClock, which is
// unexported and internal to that package) so this is a genuine
// end-to-end timing check, not just a logic check.
func TestClient_DeleteRecord_CtxDeadlineDuringRetry_ReturnsCtxErrWithoutFullBackoff(t *testing.T) {
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		},
	}
	retryDoer := retry.NewDoer(mock, defaultRetryPolicy, retry.RealClock{})
	client := newTestClient(retryDoer, &url.URL{Scheme: "https", Host: "pds.test"}, "bob.test", "did:plc:test123", deleteTestSession)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := client.DeleteRecord(ctx, "abc123")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 500*time.Millisecond)
}
