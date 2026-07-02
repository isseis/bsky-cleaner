package atproto

import (
	"context"
	"errors"
	"net"
	"net/http"
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
