package atproto

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publicIPLiteral is a documented, non-routable-for-real-traffic address
// (RFC 5737 TEST-NET-3) that is nonetheless not private/loopback/link-
// local/unspecified, so validatePDSEndpoint accepts it. Using an IP
// literal as the host means net.DefaultResolver.LookupIPAddr resolves it
// locally without any real DNS query, so this test needs no network I/O.
const publicIPLiteral = "203.0.113.5"

func handleResolutionHandler(t *testing.T, handle, did, plcHost, pdsEndpoint string) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == handle && req.URL.Path == "/.well-known/atproto-did":
			return atprototestutil.JSONResponse(http.StatusOK, did), nil
		case req.URL.Host == plcHost && req.URL.Path == "/"+did:
			return atprototestutil.JSONResponse(http.StatusOK, `{"id":"`+did+`","service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"`+pdsEndpoint+`"}]}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}
}

func TestValidatePDSEndpoint_Success(t *testing.T) {
	verifiedAddrs, host, err := validatePDSEndpoint(context.Background(), "https://"+publicIPLiteral)

	require.NoError(t, err)
	assert.Equal(t, publicIPLiteral, host)
	require.Len(t, verifiedAddrs, 1)
	assert.True(t, verifiedAddrs[0].Equal(net.ParseIP(publicIPLiteral)))
}

func TestNewClient_ResolvesHandleToDIDAndPDSEndpoint(t *testing.T) {
	const handle = "alice.test"
	const did = "did:plc:test123"
	pdsEndpoint := "https://" + publicIPLiteral

	mock := &atprototestutil.MockHTTPDoer{Handler: handleResolutionHandler(t, handle, did, "plc.directory", pdsEndpoint)}

	client, err := NewClient(context.Background(), handle, mock)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, handle, client.handle)
	assert.Equal(t, did, client.did)
	assert.Equal(t, publicIPLiteral, client.pdsBaseURL.Host)
	assert.Equal(t, "https", client.pdsBaseURL.Scheme)
}

func TestValidatePDSEndpoint_RejectsNonHTTPSScheme(t *testing.T) {
	_, _, err := validatePDSEndpoint(context.Background(), "http://"+publicIPLiteral)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	ssrfErr, ok := errors.AsType[*SSRFError](err)
	require.True(t, ok)
	assert.Equal(t, SSRFStageInitialValidation, ssrfErr.Stage)
}

func TestValidatePDSEndpoint_RejectsUntrustedHost(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{name: "private address (RFC 1918)", endpoint: "https://10.0.0.1"},
		{name: "loopback IPv4", endpoint: "https://127.0.0.1"},
		{name: "loopback IPv6", endpoint: "https://[::1]"},
		{name: "link-local unicast", endpoint: "https://169.254.1.1"},
		{name: "link-local multicast", endpoint: "https://224.0.0.251"},
		{name: "unspecified address", endpoint: "https://0.0.0.0"},
		{name: "IPv4-mapped IPv6 loopback", endpoint: "https://[::ffff:127.0.0.1]"},
		{name: "IPv6 unique local address", endpoint: "https://[fd00::1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := validatePDSEndpoint(context.Background(), tt.endpoint)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
		})
	}

	t.Run("hostname resolves to both public and private addresses", func(t *testing.T) {
		prev := lookupIPAddr
		t.Cleanup(func() { lookupIPAddr = prev })
		lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return []net.IPAddr{
				{IP: net.ParseIP(publicIPLiteral)},
				{IP: net.ParseIP("10.0.0.1")},
			}, nil
		}

		_, _, err := validatePDSEndpoint(context.Background(), "https://mixed.example")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	})
}

func TestNewClient_DIDResolutionFailure(t *testing.T) {
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(_ *http.Request) (*http.Response, error) {
			return atprototestutil.JSONResponse(http.StatusNotFound, ""), nil
		},
	}

	client, err := NewClient(context.Background(), "alice.test", mock)

	require.Error(t, err)
	assert.Nil(t, client)
	assert.ErrorIs(t, err, ErrDIDResolutionFailed)
}

func TestNewClient_RejectsUntrustedHost_NoFurtherRequest(t *testing.T) {
	const handle = "alice.test"
	const did = "did:plc:test123"

	mock := &atprototestutil.MockHTTPDoer{Handler: handleResolutionHandler(t, handle, did, "plc.directory", "https://10.0.0.1")}

	client, err := NewClient(context.Background(), handle, mock)

	require.Error(t, err)
	assert.Nil(t, client)
	assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	assert.Equal(t, 2, mock.CallCount(), "expected exactly handle resolution + DID document requests, no further request after rejection")
}
