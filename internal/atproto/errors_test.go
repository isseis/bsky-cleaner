package atproto

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPError_ErrorsIs(t *testing.T) {
	err := &HTTPError{Method: "com.atproto.repo.deleteRecord", StatusCode: 500, Err: ErrHTTPStatus}

	assert.ErrorIs(t, err, ErrHTTPStatus)
	assert.NotErrorIs(t, err, ErrTransportFailure)
}

func TestHTTPError_AsType(t *testing.T) {
	var err error = &HTTPError{Method: "com.atproto.repo.deleteRecord", StatusCode: 503, Err: ErrHTTPStatus}

	httpErr, ok := errors.AsType[*HTTPError](err)

	require.True(t, ok)
	assert.Equal(t, 503, httpErr.StatusCode)
	assert.Equal(t, "com.atproto.repo.deleteRecord", httpErr.Method)
}

// TestErrors_NoSecretLeakage verifies AC-15 across the client's error
// paths: a timeout (transport failure), a 5xx response, a 4xx response,
// and a login failure. Each case scripts a mock that carries a known app
// password/session JWT value somewhere in the request/response cycle, and
// asserts the returned error's Error() string contains neither value nor
// the Authorization header. This is the cross-cutting check the
// architecture doc's 4.2 節 calls for, run once all Phase 1-5 error paths
// exist.
func TestErrors_NoSecretLeakage(t *testing.T) {
	const password = "correct-horse-battery-staple"
	const accessJwt = "super-secret-session-jwt"

	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{
			name: "transport_failure",
			run: func(t *testing.T) error {
				t.Helper()
				appPassword := testAppPassword(t, password)
				mock := &atprototestutil.MockHTTPDoer{
					Handler: func(_ *http.Request) (*http.Response, error) {
						return nil, fmt.Errorf("dial tcp: i/o timeout")
					},
				}
				client := loginTestClient(mock)
				return client.Login(context.Background(), appPassword)
			},
		},
		{
			name: "5xx_response",
			run: func(t *testing.T) error {
				t.Helper()
				client, _ := newDeleteTestClient(&Session{DID: "did:plc:test123", AccessJWT: newSecretString(accessJwt)}, func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, "Bearer "+accessJwt, req.Header.Get("Authorization"))
					return atprototestutil.JSONResponse(http.StatusInternalServerError, `{"error":"InternalServerError"}`), nil
				})
				return client.DeleteRecord(context.Background(), "abc123")
			},
		},
		{
			name: "4xx_response",
			run: func(t *testing.T) error {
				t.Helper()
				client, _ := newDeleteTestClient(&Session{DID: "did:plc:test123", AccessJWT: newSecretString(accessJwt)}, func(_ *http.Request) (*http.Response, error) {
					return atprototestutil.JSONResponse(http.StatusForbidden, `{"error":"Forbidden"}`), nil
				})
				return client.DeleteRecord(context.Background(), "abc123")
			},
		},
		{
			name: "login_failure",
			run: func(t *testing.T) error {
				t.Helper()
				appPassword := testAppPassword(t, password)
				mock := &atprototestutil.MockHTTPDoer{
					Handler: func(_ *http.Request) (*http.Response, error) {
						return atprototestutil.JSONResponse(http.StatusUnauthorized, `{"error":"AuthenticationRequired","accessJwt":"`+accessJwt+`"}`), nil
					},
				}
				client := loginTestClient(mock)
				return client.Login(context.Background(), appPassword)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), password)
			assert.NotContains(t, err.Error(), accessJwt)
			assert.NotContains(t, err.Error(), "Bearer "+accessJwt)
		})
	}
}
