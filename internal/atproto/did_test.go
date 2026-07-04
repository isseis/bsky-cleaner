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

// stubSymbolicHostLookup makes lookupIPAddr resolve any non-IP-literal
// host (e.g. "alice.test", "plc.directory") to publicIPLiteral, so tests
// that exercise resolveHandleToDID/resolveDIDDocument through NewClient
// (both now gated by checkRequestHostSafety) don't perform a real DNS
// query. Hosts that are already IP literals (used by the boundary-value
// rejection tests) are passed through to the real resolver, which
// resolves them locally without any network I/O either way.
func stubSymbolicHostLookup(t *testing.T) {
	t.Helper()
	prev := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = prev })
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if net.ParseIP(host) != nil {
			return prev(ctx, host)
		}
		return []net.IPAddr{{IP: net.ParseIP(publicIPLiteral)}}, nil
	}
}

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
	stubSymbolicHostLookup(t)
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
	stubSymbolicHostLookup(t)
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
	stubSymbolicHostLookup(t)
	const handle = "alice.test"
	const did = "did:plc:test123"

	mock := &atprototestutil.MockHTTPDoer{Handler: handleResolutionHandler(t, handle, did, "plc.directory", "https://10.0.0.1")}

	client, err := NewClient(context.Background(), handle, mock)

	require.Error(t, err)
	assert.Nil(t, client)
	assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	assert.Equal(t, 2, mock.CallCount(), "expected exactly handle resolution + DID document requests, no further request after rejection")
}

func TestDIDWebDocumentURL(t *testing.T) {
	tests := []struct {
		name string
		did  string
		want string
	}{
		{name: "bare domain", did: "did:web:example.com", want: "https://example.com/.well-known/did.json"},
		{name: "path segments", did: "did:web:example.com:user:alice", want: "https://example.com/user/alice/did.json"},
		{name: "percent-encoded port", did: "did:web:example.com%3A3000", want: "https://example.com:3000/.well-known/did.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := didWebDocumentURL(tt.did)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDIDWebDocumentURL_RejectsInvalidDomainSegment(t *testing.T) {
	tests := []struct {
		name string
		did  string
	}{
		{name: "path injection via percent-encoding", did: "did:web:evil.com%2Fx"},
		{name: "userinfo injection via percent-encoding", did: "did:web:x%40evil.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := didWebDocumentURL(tt.did)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDIDResolutionFailed)
		})
	}
}

func TestDIDWebDocumentURL_RejectsInvalidPathSegment(t *testing.T) {
	tests := []struct {
		name string
		did  string
	}{
		{name: "path separator injection via percent-encoding", did: "did:web:example.com:user%2Fevil"},
		{name: "query injection via percent-encoding", did: "did:web:example.com:user%3Fq%3D1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := didWebDocumentURL(tt.did)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDIDResolutionFailed)
		})
	}
}

func TestResolveDIDDocument_RejectsUnsafeDidWebHost(t *testing.T) {
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(req *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected request to untrusted did:web host: %s %s", req.Method, req.URL)
			return nil, nil
		},
	}

	_, err := resolveDIDDocument(context.Background(), newHostSafetyCheckedDoer(mock), "did:web:127.0.0.1")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	assert.Equal(t, 0, mock.CallCount(), "must not send a request to an unsafe did:web host")
}

// TestNewHostSafetyCheckedDoer_RevalidatesOnEveryCall verifies that
// checkRequestHostSafety runs on every Do call, not only before the first
// attempt -- the property retry.Doer's retries depend on to keep the
// DNS-rebinding protection intact across retried attempts of the same
// logical call.
func TestNewHostSafetyCheckedDoer_RevalidatesOnEveryCall(t *testing.T) {
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(_ *http.Request) (*http.Response, error) {
			return atprototestutil.JSONResponse(http.StatusOK, `{}`), nil
		},
	}
	doer := newHostSafetyCheckedDoer(mock)

	safeReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+publicIPLiteral+"/xrpc/test", nil)
	require.NoError(t, err)
	resp, err := doer.Do(safeReq)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, 1, mock.CallCount(), "first call to a safe host must reach the inner doer")

	unsafeReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://10.0.0.1/xrpc/test", nil)
	require.NoError(t, err)
	_, err = doer.Do(unsafeReq)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUntrustedPDSEndpoint)
	assert.Equal(t, 1, mock.CallCount(), "second call to an unsafe host must not reach the inner doer")
}

// TestCheckRequestHostSafety_LookupFailureIsNotPermanent guards against a
// transient DNS lookup failure being classified as a permanent SSRF
// rejection: since newHostSafetyCheckedDoer is wrapped in retry.NewDoer
// (client.go), an *SSRFError here (Permanent() == true, errors.go) would
// make a mere resolver hiccup during DID resolution fail the whole call
// immediately instead of being retried like any other transient failure.
func TestCheckRequestHostSafety_LookupFailureIsNotPermanent(t *testing.T) {
	prev := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = prev })
	lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return nil, errors.New("temporary resolver failure")
	}

	err := checkRequestHostSafety(context.Background(), "https://alice.test")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDIDResolutionFailed)
	_, isSSRFError := errors.AsType[*SSRFError](err)
	assert.False(t, isSSRFError, "a DNS lookup failure must not be classified as a permanent SSRF rejection")
}

func TestResolveHandleToDID_RejectsMalformedHandle(t *testing.T) {
	tests := []struct {
		name   string
		handle string
	}{
		{name: "userinfo injection", handle: "evil.com/x@attacker.com"},
		{name: "path injection", handle: "evil.com/../secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &atprototestutil.MockHTTPDoer{
				Handler: func(req *http.Request) (*http.Response, error) {
					t.Fatalf("unexpected request for malformed handle: %s %s", req.Method, req.URL)
					return nil, nil
				},
			}

			_, err := resolveHandleToDID(context.Background(), mock, tt.handle)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDIDResolutionFailed)
			assert.Equal(t, 0, mock.CallCount(), "must not send a request for a malformed handle")
		})
	}
}

func TestResolveHandleToDID_RejectsNonDIDResponse(t *testing.T) {
	mock := &atprototestutil.MockHTTPDoer{
		Handler: func(_ *http.Request) (*http.Response, error) {
			return atprototestutil.JSONResponse(http.StatusOK, "not-a-did"), nil
		},
	}

	_, err := resolveHandleToDID(context.Background(), mock, publicIPLiteral)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDIDResolutionFailed)
}
