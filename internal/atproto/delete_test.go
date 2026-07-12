package atproto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deleteTestSession is the session bound to every *Client built by
// newDeleteTestClient below, so mock handlers can assert the "repo" field
// of the request body always equals its DID, never a caller-supplied
// value.
var deleteTestSession = &Session{DID: "did:plc:test123", AccessJWT: newSecretString("test-access-jwt")}

// newDeleteTestClient builds a *Client (via the Phase 3 newTestClient
// factory) wired to a mock whose responses are driven by handler, with
// session set to deleteTestSession (or nil, if session is explicitly nil).
func newDeleteTestClient(session *Session, handler func(req *http.Request) (*http.Response, error)) (*Client, *atprototestutil.MockHTTPDoer) {
	mock := &atprototestutil.MockHTTPDoer{Handler: handler}
	client := newTestClient(mock, &url.URL{Scheme: "https", Host: "pds.test"}, "alice.test", "did:plc:test123", session)
	return client, mock
}

func TestClient_DeleteRecord_Success(t *testing.T) {
	client, mock := newDeleteTestClient(deleteTestSession, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Equal(t, "/xrpc/com.atproto.repo.deleteRecord", req.URL.Path)
		assert.Equal(t, "Bearer test-access-jwt", req.Header.Get("Authorization"))
		return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: PostTypeOriginal})

	require.NoError(t, err)
	assert.Equal(t, 1, mock.CallCount())
}

// TestClient_DeleteRecord_AlreadyDeleted_Idempotent verifies that deleting
// an already-absent rkey does not crash and is treated as success. Per the
// com.atproto.repo.deleteRecord lexicon ("Delete a repository record, or
// ensure it doesn't exist"), the PDS responds 200 in this case exactly as
// it would for a first-time deletion, so this test reuses the same canned
// 200 response as TestClient_DeleteRecord_Success -- there is no distinct
// "not found" response shape to script.
func TestClient_DeleteRecord_AlreadyDeleted_Idempotent(t *testing.T) {
	client, _ := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "already-deleted-rkey", Type: PostTypeOriginal})

	assert.NoError(t, err)
}

func TestClient_DeleteRecord_UsesOwnDID(t *testing.T) {
	client, mock := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: PostTypeOriginal})
	require.NoError(t, err)

	requests := mock.Requests()
	require.Len(t, requests, 1)
	var sentBody deleteRecordRequest
	require.NoError(t, json.Unmarshal(requests[0].Body, &sentBody))
	assert.Equal(t, deleteTestSession.DID, sentBody.Repo, "repo must always be the session's own DID, never a caller-supplied value")
	assert.Equal(t, collectionFeedPost, sentBody.Collection)
	assert.Equal(t, "abc123", sentBody.RKey)
}

func TestClient_DeleteRecord_WithoutSession_ReturnsError(t *testing.T) {
	client, mock := newDeleteTestClient(nil, func(_ *http.Request) (*http.Response, error) {
		t.Fatal("no request should be sent when session is nil")
		return nil, nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: PostTypeOriginal})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAuthenticationFailed)
	assert.Equal(t, 0, mock.CallCount())
}

func TestClient_DeleteRecord_HTTPError(t *testing.T) {
	client, _ := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		return atprototestutil.JSONResponse(http.StatusInternalServerError, `{"error":"InternalServerError"}`), nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: PostTypeOriginal})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHTTPStatus)
}

// TestClient_DeleteRecord_Repost_UsesRepostCollection verifies that
// deleting a PostTypeRepost sends collection=app.bsky.feed.repost, not the
// app.bsky.feed.post default.
func TestClient_DeleteRecord_Repost_UsesRepostCollection(t *testing.T) {
	client, mock := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: PostTypeRepost})
	require.NoError(t, err)

	requests := mock.Requests()
	require.Len(t, requests, 1)
	var sentBody deleteRecordRequest
	require.NoError(t, json.Unmarshal(requests[0].Body, &sentBody))
	assert.Equal(t, collectionRepost, sentBody.Collection)
}

// TestClient_DeleteRecord_NonRepostTypes_UsesFeedPostCollection verifies
// that original/reply/quote posts keep sending
// collection=app.bsky.feed.post.
func TestClient_DeleteRecord_NonRepostTypes_UsesFeedPostCollection(t *testing.T) {
	tests := []struct {
		name     string
		postType PostType
	}{
		{"original", PostTypeOriginal},
		{"reply", PostTypeReply},
		{"quote", PostTypeQuote},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, mock := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
				return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
			})

			err := client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: tt.postType})
			require.NoError(t, err)

			requests := mock.Requests()
			require.Len(t, requests, 1)
			var sentBody deleteRecordRequest
			require.NoError(t, json.Unmarshal(requests[0].Body, &sentBody))
			assert.Equal(t, collectionFeedPost, sentBody.Collection)
		})
	}
}

// TestClient_DeleteRecord_Repost_SendsOnlyRepostCollection_NotFeedPost
// verifies that deleting a repost whose rkey happens to collide with an
// unrelated app.bsky.feed.post record's rkey must not touch that other
// record. Per the com.atproto.repo.deleteRecord lexicon, a delete request
// can only ever identify a record via the (repo, collection, rkey) triple
// it sends -- a request with collection=app.bsky.feed.repost cannot act on
// any app.bsky.feed.post record, regardless of rkey. So asserting that the
// single request sent carries collection=app.bsky.feed.repost is sufficient
// proof that no cross-collection deletion occurred; the mock does not need
// to model a real app.bsky.feed.post record at the same rkey to
// demonstrate it survives.
func TestClient_DeleteRecord_Repost_SendsOnlyRepostCollection_NotFeedPost(t *testing.T) {
	client, mock := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "shared-rkey", Type: PostTypeRepost})
	require.NoError(t, err)

	require.Equal(t, 1, mock.CallCount())
	requests := mock.Requests()
	var sentBody deleteRecordRequest
	require.NoError(t, json.Unmarshal(requests[0].Body, &sentBody))
	assert.Equal(t, collectionRepost, sentBody.Collection)
}

// TestClient_DeleteRecord_UnknownPostType_NoRequestSent verifies the
// fail-closed design of collectionForPostType: an unrecognized PostType
// must not cause DeleteRecord to guess a collection and send a request.
func TestClient_DeleteRecord_UnknownPostType_NoRequestSent(t *testing.T) {
	client, mock := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		t.Fatal("no request should be sent for an unknown post type")
		return nil, nil
	})

	err := client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: PostType(99)})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownPostType)
	assert.Equal(t, 0, mock.CallCount())
}
