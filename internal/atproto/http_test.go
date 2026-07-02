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
// two in sync.
func newTestRestrictedDoer(t *testing.T, dialContextAddrs []net.IP, pinnedIP net.IP, host string, rootCAs *x509.CertPool) *restrictedDoer {
	t.Helper()
	doer := newRestrictedDoer(dialContextAddrs, host).(*restrictedDoer)
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
		doer := newTestRestrictedDoer(t, []net.IP{serverIP}, serverIP, serverHost, rootCAs)
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
		doer := newTestRestrictedDoer(t, []net.IP{net.ParseIP(publicIPLiteral)}, serverIP, serverHost, rootCAs)
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
