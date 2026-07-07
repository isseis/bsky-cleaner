//go:build test

// Package atproto_test contains integration tests for idempotency and
// cancellation behavior across the runner and atproto packages.
package atproto_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/isseis/bsky-cleaner/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunnerRun_AllTargetsAlreadyDeleted_TreatedAsSuccess verifies AC-05:
// when all target posts are already deleted before the delete phase, the
// runner treats this as success (all rkeys in Deleted, none in Failed).
//
// Overlap with existing tests: TestRunnerRun_WithRealAtprotoClient covers
// the basic 2xx -> success path for a single post. This test differentiates
// itself by exercising the scenario where multiple targets are all already
// deleted (pre-existing deletion), ensuring the runner handles this as a
// valid success case rather than an error.
func TestRunnerRun_AllTargetsAlreadyDeleted_TreatedAsSuccess(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	atproto.StubDNSTXTLookup(t)

	const handle = publicIPLiteral
	const did = "did:web:" + publicIPLiteral

	// Prepare two posts that will be listed but already deleted.
	postRkeys := []string{"deleted-post-1", "deleted-post-2"}
	var postRecords []string
	for _, rkey := range postRkeys {
		record := fmt.Sprintf(
			`{"uri":"at://%s/app.bsky.feed.post/%s","cid":"bafycid","value":{"$type":"app.bsky.feed.post","text":"old","createdAt":%q}}`,
			did, rkey, "2000-01-01T00:00:00Z",
		)
		postRecords = append(postRecords, record)
	}
	postPage := atprototestutil.ListRecordsResponseJSON(postRecords, "")
	repostPage := atprototestutil.ListRecordsResponseJSON(nil, "")

	deleteCallCount := 0
	mock := &atprototestutil.MockHTTPDoer{Handler: func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == handle && req.URL.Path == "/.well-known/atproto-did":
			return atprototestutil.JSONResponse(http.StatusOK, did), nil
		case req.URL.Host == handle && req.URL.Path == "/.well-known/did.json":
			body := fmt.Sprintf(`{"id":%q,"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://%s"}]}`, did, publicIPLiteral)
			return atprototestutil.JSONResponse(http.StatusOK, body), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession"):
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.CreateSessionResponseJSON(did, "access-jwt")), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.post":
			return atprototestutil.JSONResponse(http.StatusOK, postPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.repost":
			return atprototestutil.JSONResponse(http.StatusOK, repostPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.getRecord"):
			// Simulate that the profile record (pinned post) is already gone.
			return atprototestutil.JSONResponse(http.StatusBadRequest, `{"error":"RecordNotFound"}`), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.deleteRecord"):
			// deleteRecord returns 2xx even for already-deleted records (idempotent).
			deleteCallCount++
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}}

	client, err := atproto.NewClient(context.Background(), handle, mock)
	require.NoError(t, err)

	appPassword := integrationAppPassword(t)
	now := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)

	var runnerClient runner.Client = client
	result, err := runner.Run(context.Background(), runnerClient, appPassword, 30, true, now)

	require.NoError(t, err)
	// All targets should be in Deleted, none in Failed.
	assert.Empty(t, result.Failed)
	require.Len(t, result.Deleted, len(postRkeys))
	deletedRkeys := make([]string, len(result.Deleted))
	for i, d := range result.Deleted {
		deletedRkeys[i] = d.RKey
	}
	// Verify all expected rkeys are in Deleted.
	for _, rkey := range postRkeys {
		assert.True(t, slices.Contains(deletedRkeys, rkey), "rkey %s should be in Deleted", rkey)
	}
	// Verify deleteRecord was called for each target.
	assert.Equal(t, len(postRkeys), deleteCallCount)
}

// TestRunnerRun_CancelMidDelete_RemainingFailedThenReRunSafe verifies AC-06:
// when execution is cancelled mid-delete, deleted items go to Deleted,
// remaining items go to Failed, and a second run successfully deletes the
// remaining items (idempotency).
//
// This test uses context.WithCancel (not WithTimeout) to create a
// deterministic cancellation scenario that does not depend on real time.
// The mock cancels the context after N successful deletions, and the test
// verifies that:
// 1. Deleted contains the N successfully deleted rkeys
// 2. Failed contains the remaining rkeys
// 3. Deleted and Failed sets are disjoint
// 4. A second run with a fresh context successfully deletes all Failed items
func TestRunnerRun_CancelMidDelete_RemainingFailedThenReRunSafe(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	atproto.StubDNSTXTLookup(t)

	const handle = publicIPLiteral
	const did = "did:web:" + publicIPLiteral

	// Prepare 5 posts to delete.
	allRkeys := []string{"post-1", "post-2", "post-3", "post-4", "post-5"}
	var postRecords []string
	for _, rkey := range allRkeys {
		record := fmt.Sprintf(
			`{"uri":"at://%s/app.bsky.feed.post/%s","cid":"bafycid","value":{"$type":"app.bsky.feed.post","text":"old","createdAt":%q}}`,
			did, rkey, "2000-01-01T00:00:00Z",
		)
		postRecords = append(postRecords, record)
	}
	postPage := atprototestutil.ListRecordsResponseJSON(postRecords, "")
	repostPage := atprototestutil.ListRecordsResponseJSON(nil, "")

	// Cancel after 3 successful deletions.
	cancelAfter := 3
	deleteCallCount := 0

	// Create a cancellable context that the mock will cancel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock := &atprototestutil.MockHTTPDoer{Handler: func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == handle && req.URL.Path == "/.well-known/atproto-did":
			return atprototestutil.JSONResponse(http.StatusOK, did), nil
		case req.URL.Host == handle && req.URL.Path == "/.well-known/did.json":
			body := fmt.Sprintf(`{"id":%q,"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://%s"}]}`, did, publicIPLiteral)
			return atprototestutil.JSONResponse(http.StatusOK, body), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession"):
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.CreateSessionResponseJSON(did, "access-jwt")), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.post":
			return atprototestutil.JSONResponse(http.StatusOK, postPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.repost":
			return atprototestutil.JSONResponse(http.StatusOK, repostPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.getRecord"):
			return atprototestutil.JSONResponse(http.StatusBadRequest, `{"error":"RecordNotFound"}`), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.deleteRecord"):
			deleteCallCount++
			// Cancel after threshold deletions.
			if deleteCallCount > cancelAfter {
				// Signal cancellation to the runner.
				cancel()
			}
			// Check if context is already cancelled.
			if req.Context().Err() != nil {
				// Return error for cancelled context (net/http behavior).
				return nil, req.Context().Err()
			}
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}}

	client, err := atproto.NewClient(ctx, handle, mock)
	require.NoError(t, err)

	appPassword := integrationAppPassword(t)
	now := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)

	var runnerClient runner.Client = client

	// First run: cancel mid-delete.
	result1, err := runner.Run(ctx, runnerClient, appPassword, 30, true, now)
	// runner.Run may return an error due to context cancellation.
	// We care about the result structure, but must check err to avoid
	// a nil-pointer panic if Run returns (nil, err).
	require.NoError(t, err, "runner.Run should not return an error for mid-delete cancellation")

	// Verify Deleted and Failed are disjoint and cover all rkeys.
	deletedRkeys := make([]string, len(result1.Deleted))
	for i, d := range result1.Deleted {
		deletedRkeys[i] = d.RKey
	}
	failedRkeys := make([]string, len(result1.Failed))
	for i, f := range result1.Failed {
		failedRkeys[i] = f.Post.RKey
	}

	// Cancellation is deterministic (cancelAfter=3), so exactly 3 posts
	// should be deleted and the remaining 2 should be in Failed.
	assert.Len(t, result1.Deleted, cancelAfter, "exactly %d posts should be deleted before cancellation", cancelAfter)
	assert.Len(t, result1.Failed, len(allRkeys)-cancelAfter, "remaining %d posts should fail due to cancellation", len(allRkeys)-cancelAfter)

	// Verify Deleted and Failed are disjoint.
	for _, rkey := range deletedRkeys {
		assert.False(t, slices.Contains(failedRkeys, rkey), "rkey %s should not be in both Deleted and Failed", rkey)
	}

	// Second run: verify idempotency - re-running should not error on already-deleted items.
	// Note: The runner selects targets based on retention date, so all 5 posts will be
	// selected again. The key verification is that deleteRecord succeeds for all of them
	// (including the 3 already deleted in the first run), with no failures.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	secondRunDeleteCount := 0
	mock2 := &atprototestutil.MockHTTPDoer{Handler: func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == handle && req.URL.Path == "/.well-known/atproto-did":
			return atprototestutil.JSONResponse(http.StatusOK, did), nil
		case req.URL.Host == handle && req.URL.Path == "/.well-known/did.json":
			body := fmt.Sprintf(`{"id":%q,"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://%s"}]}`, did, publicIPLiteral)
			return atprototestutil.JSONResponse(http.StatusOK, body), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession"):
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.CreateSessionResponseJSON(did, "access-jwt")), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.post":
			return atprototestutil.JSONResponse(http.StatusOK, postPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.repost":
			return atprototestutil.JSONResponse(http.StatusOK, repostPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.getRecord"):
			return atprototestutil.JSONResponse(http.StatusBadRequest, `{"error":"RecordNotFound"}`), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.deleteRecord"):
			secondRunDeleteCount++
			// All deletions succeed (idempotent), even for already-deleted items.
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}}

	client2, err := atproto.NewClient(ctx2, handle, mock2)
	require.NoError(t, err)

	var runnerClient2 runner.Client = client2
	result2, err := runner.Run(ctx2, runnerClient2, appPassword, 30, true, now)
	require.NoError(t, err)

	// Second run should have no failures (idempotency: re-deletion succeeds).
	assert.Empty(t, result2.Failed, "second run should have no failures")
	// All 5 posts should be deleted in the second run (runner selects all targets again).
	require.Len(t, result2.Deleted, len(allRkeys), "second run should delete all targets")
	// Verify deleteRecord was called for all 5 posts.
	assert.Equal(t, len(allRkeys), secondRunDeleteCount, "deleteRecord should be called for all targets")
}
