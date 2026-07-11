package atproto

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRedirectRejectingHTTPClient_RejectsRedirect verifies that the
// client returned by NewRedirectRejectingHTTPClient refuses to follow an
// HTTP 3xx redirect, returning *SSRFError with stage DialRevalidation
// (AC-05).
func TestNewRedirectRejectingHTTPClient_RejectsRedirect(t *testing.T) {
	var redirectReached bool
	var srvURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/target", func(w http.ResponseWriter, _ *http.Request) {
		redirectReached = true
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", srvURL+"/target")
		w.WriteHeader(http.StatusFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	client := NewRedirectRejectingHTTPClient()
	req, err := http.NewRequest("GET", srv.URL+"/redirect", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)

	// Client.Do returns a *url.Error wrapping *SSRFError when
	// CheckRedirect rejects the redirect.
	require.Error(t, err)
	assert.False(t, redirectReached, "redirect target must not be reached")

	var ssrfErr *SSRFError
	ok := errors.As(err, &ssrfErr)
	require.True(t, ok, "error must be *SSRFError")
	assert.Equal(t, SSRFStageDialRevalidation, ssrfErr.Stage)

	// Response contains the redirect response; error is the SSRFError.
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
}
