package atproto

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestrictedDialContext_RejectsUnverifiedAddress(t *testing.T) {
	verifiedAddrs := []net.IP{net.ParseIP("203.0.113.5")}
	dial := newRestrictedDialContext(verifiedAddrs, &net.Dialer{})

	_, err := dial(context.Background(), "tcp", "198.51.100.7:443")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	ssrfErr, ok := errors.AsType[*SSRFError](err)
	require.True(t, ok)
	assert.Equal(t, SSRFStageDialRevalidation, ssrfErr.Stage)
}

func TestRestrictedDialContext_ConnectFailureIsTransportError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	// Close immediately: nothing is listening on this port anymore, so a
	// connection attempt to it fails with "connection refused" -- this
	// test only exercises newRestrictedDialContext's own transport-error
	// wrapping in isolation, so a loopback address is fine here even
	// though validatePDSEndpoint would reject it upstream in production.
	require.NoError(t, listener.Close())

	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	verifiedAddrs := []net.IP{net.ParseIP(host)}
	dial := newRestrictedDialContext(verifiedAddrs, &net.Dialer{})

	_, dialErr := dial(context.Background(), "tcp", addr)

	require.Error(t, dialErr)
	assert.ErrorIs(t, dialErr, ErrTransportFailure)
}

func TestCheckRedirect_AlwaysRejects(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://pds.example/xrpc/com.atproto.repo.listRecords", nil)
	require.NoError(t, err)

	err = rejectRedirect(req, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	ssrfErr, ok := errors.AsType[*SSRFError](err)
	require.True(t, ok)
	assert.Equal(t, SSRFStageDialRevalidation, ssrfErr.Stage)
}

// newTestRestrictedDoer builds a *restrictedDoer wired to trust server's
// TLS certificate and, unlike newRestrictedDoer, lets the test pin a
// server address independently of the DialContext's verifiedAddrs set, so
// TestRestrictedDoer_Do can exercise the DialContext's own defense-in-depth
// check even though restrictedDoer's own construction normally keeps the
// two in sync. The timeout parameter is passed to newRestrictedDoer.
func newTestRestrictedDoer(t *testing.T, dialContextAddrs []net.IP, pinnedIP net.IP, host string, rootCAs *x509.CertPool, timeout time.Duration) *restrictedDoer {
	t.Helper()
	doer := newRestrictedDoer(dialContextAddrs, host, timeout).(*restrictedDoer)
	transport, ok := doer.client.Transport.(*http.Transport)
	require.True(t, ok)
	transport.TLSClientConfig.RootCAs = rootCAs
	doer.pinnedIP = pinnedIP
	return doer
}

func TestRestrictedDoer_Do(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	serverHost, _, err := net.SplitHostPort(serverURL.Host)
	require.NoError(t, err)
	serverIP := net.ParseIP(serverHost)
	require.NotNil(t, serverIP)

	transport, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	rootCAs := transport.TLSClientConfig.RootCAs

	t.Run("connects to the pinned verified address, rewriting host and TLS SNI", func(t *testing.T) {
		doer := newTestRestrictedDoer(t, []net.IP{serverIP}, serverIP, serverHost, rootCAs, xrpcRequestTimeout)
		req, err := http.NewRequest(http.MethodGet, server.URL+"/xrpc/test", nil)
		require.NoError(t, err)

		resp, err := doer.Do(req)

		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("SSRFError from DialContext surfaces through Do when pinnedIP is not in verifiedAddrs", func(t *testing.T) {
		// verifiedAddrs deliberately excludes serverIP, simulating an
		// inconsistency between the doer's pinned dial target and its
		// DialContext's verified set, to prove the DialContext check
		// itself -- not just newRestrictedDoer's construction-time
		// invariant -- is what blocks the connection, and that the
		// resulting *SSRFError survives http.Client.Do's *url.Error
		// wrapping and is still detectable by errors.AsType.
		doer := newTestRestrictedDoer(t, []net.IP{net.ParseIP(publicIPLiteral)}, serverIP, serverHost, rootCAs, xrpcRequestTimeout)
		req, err := http.NewRequest(http.MethodGet, server.URL+"/xrpc/test", nil)
		require.NoError(t, err)

		_, doErr := doer.Do(req)

		require.Error(t, doErr)
		assert.ErrorIs(t, doErr, ErrUntrustedPDSEndpoint)
		ssrfErr, ok := errors.AsType[*SSRFError](doErr)
		require.True(t, ok)
		assert.Equal(t, SSRFStageDialRevalidation, ssrfErr.Stage)
	})
}

func TestDoXRPC_ResponseSizeLimit(t *testing.T) {
	t.Run("response within limit decodes successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Small response body guaranteed to fit within the limit
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		}))
		t.Cleanup(server.Close)

		baseURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		var out map[string]any
		err = doXRPC(context.Background(), server.Client(), baseURL, http.MethodGet, "test.method", nil, nil, &out, "")

		require.NoError(t, err)
		assert.Equal(t, "ok", out["result"])
	})

	t.Run("response exceeding limit returns ErrResponseTooLarge", func(t *testing.T) {
		// Create a response body that exceeds maxXRPCResponseBytes
		excessData := make([]byte, maxXRPCResponseBytes+1024)
		for i := range excessData {
			excessData[i] = 'x'
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(excessData)
		}))
		t.Cleanup(server.Close)

		baseURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		var out map[string]any
		err = doXRPC(context.Background(), server.Client(), baseURL, http.MethodGet, "test.method", nil, nil, &out, "")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
		httpErr, ok := errors.AsType[*HTTPError](err)
		require.True(t, ok)
		assert.Equal(t, responseTooLargeErrorName, httpErr.ErrorName)
	})

	t.Run("response exactly at limit decodes successfully", func(t *testing.T) {
		// Create a valid JSON response exactly at the limit
		// {"data":"..."} where total size equals maxXRPCResponseBytes
		// The template is {"data":"<data>"} which is 10 bytes + data length
		templatePrefix := `{"data":"`
		templateSuffix := `"}`
		dataSize := maxXRPCResponseBytes - len(templatePrefix) - len(templateSuffix)
		exactData := make([]byte, dataSize)
		for i := range exactData {
			exactData[i] = 'x'
		}
		response := []byte(templatePrefix + string(exactData) + templateSuffix)
		require.Equal(t, maxXRPCResponseBytes, len(response), "response size should be exactly at limit")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(response)
		}))
		t.Cleanup(server.Close)

		baseURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		var out map[string]any
		err = doXRPC(context.Background(), server.Client(), baseURL, http.MethodGet, "test.method", nil, nil, &out, "")

		require.NoError(t, err)
		assert.NotNil(t, out["data"])
	})

	t.Run("response one byte over limit returns ErrResponseTooLarge", func(t *testing.T) {
		// Create a valid JSON response one byte over the limit
		templatePrefix := `{"data":"`
		templateSuffix := `"}`
		dataSize := maxXRPCResponseBytes - len(templatePrefix) - len(templateSuffix) + 1
		exactData := make([]byte, dataSize)
		for i := range exactData {
			exactData[i] = 'x'
		}
		response := []byte(templatePrefix + string(exactData) + templateSuffix)
		require.Equal(t, maxXRPCResponseBytes+1, len(response), "response size should be exactly one byte over limit")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(response)
		}))
		t.Cleanup(server.Close)

		baseURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		var out map[string]any
		err = doXRPC(context.Background(), server.Client(), baseURL, http.MethodGet, "test.method", nil, nil, &out, "")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
	})
}

func TestDoXRPC_RequestTimeout(t *testing.T) {
	t.Run("slow response triggers timeout", func(t *testing.T) {
		// Create a server that delays response beyond the timeout
		shortTimeout := 100 * time.Millisecond
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Sleep longer than the timeout
			time.Sleep(500 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		}))
		t.Cleanup(server.Close)

		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		serverHost, _, err := net.SplitHostPort(serverURL.Host)
		require.NoError(t, err)
		serverIP := net.ParseIP(serverHost)
		require.NotNil(t, serverIP)

		// Create a restrictedDoer with short timeout
		doer := newRestrictedDoer([]net.IP{serverIP}, serverHost, shortTimeout)

		var out map[string]any
		err = doXRPC(context.Background(), doer, serverURL, http.MethodGet, "test.method", nil, nil, &out, "")

		require.Error(t, err)
		// Timeout errors are wrapped as ErrTransportFailure
		assert.ErrorIs(t, err, ErrTransportFailure)
	})

	t.Run("fast response completes before timeout", func(t *testing.T) {
		shortTimeout := 100 * time.Millisecond
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":"fast"}`))
		}))
		t.Cleanup(server.Close)

		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		serverHost, _, err := net.SplitHostPort(serverURL.Host)
		require.NoError(t, err)
		serverIP := net.ParseIP(serverHost)
		require.NotNil(t, serverIP)

		// Create a restrictedDoer with short timeout
		doer := newRestrictedDoer([]net.IP{serverIP}, serverHost, shortTimeout)

		var out map[string]any
		err = doXRPC(context.Background(), doer, serverURL, http.MethodGet, "test.method", nil, nil, &out, "")

		require.NoError(t, err)
		assert.Equal(t, "fast", out["result"])
	})
}

// TestNewRedirectRejectingHTTPClient_RejectsRedirect verifies that the
// client returned by NewRedirectRejectingHTTPClient refuses to follow an
// HTTP 3xx redirect, returning *SSRFError with stage DialRevalidation.
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

	ssrfErr, ok := errors.AsType[*SSRFError](err)
	require.True(t, ok, "error must be *SSRFError")
	assert.Equal(t, SSRFStageDialRevalidation, ssrfErr.Stage)

	// Response contains the redirect response; error is the SSRFError.
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
}
