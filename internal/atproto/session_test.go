package atproto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testHandle is the handle bound to every *Client built by
// loginTestClient below, so tests can assert Login always sends this
// value as the identifier, never a caller-supplied one.
const testHandle = "alice.test"

// testAppPassword builds a config.SecretString the same way production
// code obtains one -- through config.LoadCredentials -- since the type has
// no public constructor outside internal/config (architecture 3.1 節).
func testAppPassword(t *testing.T, value string) config.SecretString {
	t.Helper()
	t.Setenv("BSKY_HANDLE", testHandle)
	t.Setenv("BSKY_APP_PASSWORD", value)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", "https://hooks.slack.com/services/success")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", "https://hooks.slack.com/services/failure")

	creds, err := config.LoadCredentials()
	require.NoError(t, err)
	return creds.AppPassword
}

func loginTestClient(mock *atprototestutil.MockHTTPDoer) *Client {
	return newTestClient(mock, &url.URL{Scheme: "https", Host: "pds.test"}, testHandle, "", nil)
}

func TestClient_Login_Success(t *testing.T) {
	appPassword := testAppPassword(t, "correct-horse-battery-staple")
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "/xrpc/com.atproto.server.createSession", req.URL.Path)
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.CreateSessionResponseJSON("did:plc:test123", "secret-access-jwt")), nil
		},
	}
	client := loginTestClient(mock)

	err := client.Login(context.Background(), appPassword)

	require.NoError(t, err)
	require.NotNil(t, client.session)
	assert.Equal(t, "did:plc:test123", client.session.DID)
	assert.Equal(t, "secret-access-jwt", client.session.AccessJWT.Reveal())

	requests := mock.Requests()
	require.Len(t, requests, 1)
	var sentBody createSessionRequest
	require.NoError(t, json.Unmarshal(requests[0].Body, &sentBody))
	assert.Equal(t, testHandle, sentBody.Identifier, "identifier must always be c.handle, never a caller-supplied value")
	assert.Equal(t, "correct-horse-battery-staple", sentBody.Password)
}

func TestClient_Login_InvalidCredentials_NoFurtherCalls(t *testing.T) {
	appPassword := testAppPassword(t, "wrong-password")
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(_ *http.Request) (*http.Response, error) {
			return atprototestutil.JSONResponse(http.StatusUnauthorized, `{"error":"AuthenticationRequired"}`), nil
		},
	}
	client := loginTestClient(mock)

	err := client.Login(context.Background(), appPassword)

	require.Error(t, err)
	assert.Nil(t, client.session)
	assert.Equal(t, 1, mock.CallCount(), "expected only the createSession attempt, no follow-up call")
}

func TestClient_Login_ErrorDoesNotLeakSecrets(t *testing.T) {
	const password = "correct-horse-battery-staple"
	const accessJwt = "leaked-session-jwt-should-not-appear"
	appPassword := testAppPassword(t, password)
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(_ *http.Request) (*http.Response, error) {
			return atprototestutil.JSONResponse(http.StatusUnauthorized, `{"error":"AuthenticationRequired","accessJwt":"`+accessJwt+`"}`), nil
		},
	}
	client := loginTestClient(mock)

	err := client.Login(context.Background(), appPassword)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), password)
	assert.NotContains(t, err.Error(), accessJwt)
}
