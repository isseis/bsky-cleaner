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

	err := client.DeleteRecord(context.Background(), "abc123")

	require.NoError(t, err)
	assert.Equal(t, 1, mock.CallCount())
}

// TestClient_DeleteRecord_AlreadyDeleted_Idempotent verifies AC-12: deleting
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

	err := client.DeleteRecord(context.Background(), "already-deleted-rkey")

	assert.NoError(t, err)
}

func TestClient_DeleteRecord_UsesOwnDID(t *testing.T) {
	client, mock := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
	})

	err := client.DeleteRecord(context.Background(), "abc123")
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

	err := client.DeleteRecord(context.Background(), "abc123")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAuthenticationFailed)
	assert.Equal(t, 0, mock.CallCount())
}

func TestClient_DeleteRecord_HTTPError(t *testing.T) {
	client, _ := newDeleteTestClient(deleteTestSession, func(_ *http.Request) (*http.Response, error) {
		return atprototestutil.JSONResponse(http.StatusInternalServerError, `{"error":"InternalServerError"}`), nil
	})

	err := client.DeleteRecord(context.Background(), "abc123")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHTTPStatus)
}
