package atproto

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"time"
)

// dialTimeout bounds how long the restricted dialer waits to establish a
// TCP connection to a verified address.
const dialTimeout = 10 * time.Second

// HTTPDoer is the minimal interface this package needs from an HTTP
// client, so unit tests can supply a mock instead of performing real
// network I/O.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// doXRPC issues an XRPC call to base + "/xrpc/" + xrpcMethod, JSON-encoding
// reqBody (when non-nil) as the request body, and JSON-decoding the
// response into out (when non-nil). Every failure is wrapped in *HTTPError
// (or returned as the *SSRFError produced by a restricted HTTPDoer), so a
// raw net/http error or *url.Error -- either of which may embed the
// request URL/headers -- never reaches the caller.
func doXRPC(ctx context.Context, doer HTTPDoer, base *url.URL, httpMethod, xrpcMethod string, query url.Values, reqBody, out any, authHeader string) error {
	u := *base
	u.Path = path.Join(u.Path, "xrpc", xrpcMethod)
	if query != nil {
		u.RawQuery = query.Encode()
	}

	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return &HTTPError{Method: xrpcMethod, StatusCode: 0, Err: fmt.Errorf("encode request: %w", err)}
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, u.String(), bodyReader)
	if err != nil {
		return &HTTPError{Method: xrpcMethod, StatusCode: 0, Err: fmt.Errorf("%w: %w", ErrTransportFailure, err)}
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := doer.Do(req)
	if err != nil {
		if ssrfErr, ok := errors.AsType[*SSRFError](err); ok {
			return ssrfErr
		}
		return &HTTPError{Method: xrpcMethod, StatusCode: 0, Err: fmt.Errorf("%w: %w", ErrTransportFailure, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Method: xrpcMethod, StatusCode: resp.StatusCode, Err: ErrHTTPStatus}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return &HTTPError{Method: xrpcMethod, StatusCode: resp.StatusCode, Err: fmt.Errorf("decode response: %w", err)}
		}
	}
	return nil
}

// restrictedDoer is an HTTPDoer that only ever connects to addresses in a
// pre-verified set (never re-resolving the host at connect time -- closing
// the DNS-rebinding TOCTOU window between validatePDSEndpoint and the
// first connection) and rejects all HTTP redirects. It rewrites each
// outgoing request's URL host to the pinned verified IP so the underlying
// Transport's DialContext receives an IP literal rather than a hostname
// (see newRestrictedDialContext); the request's Host header and TLS SNI
// still use the original, validated hostname.
type restrictedDoer struct {
	client   *http.Client
	host     string
	pinnedIP net.IP
}

// newRestrictedDoer builds a restricted HTTPDoer bound to verifiedAddrs and
// host, as validated by validatePDSEndpoint. verifiedAddrs must be
// non-empty.
func newRestrictedDoer(verifiedAddrs []net.IP, host string) HTTPDoer {
	dialer := &net.Dialer{Timeout: dialTimeout}
	transport := &http.Transport{
		DialContext:     newRestrictedDialContext(verifiedAddrs, dialer),
		TLSClientConfig: &tls.Config{ServerName: host},
	}
	return &restrictedDoer{
		client:   &http.Client{Transport: transport, CheckRedirect: rejectRedirect},
		host:     host,
		pinnedIP: verifiedAddrs[0],
	}
}

func (d *restrictedDoer) Do(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Host = d.host
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	req.URL.Host = net.JoinHostPort(d.pinnedIP.String(), port)
	return d.client.Do(req) //nolint:gosec // req.URL.Host is pinned to an address validatePDSEndpoint already verified, not an attacker-controlled URL
}

// rejectRedirect is an http.Client.CheckRedirect policy that always
// refuses to follow a redirect: XRPC calls never expect a 3xx response,
// and the default http.Client behavior of following same-host redirects
// while replaying the Authorization header/body would risk forwarding
// secrets to a host outside the verified set.
func rejectRedirect(req *http.Request, _ []*http.Request) error {
	return &SSRFError{Endpoint: req.URL.String(), Stage: SSRFStageDialRevalidation, Err: ErrUntrustedPDSEndpoint}
}

// newRestrictedDialContext returns a DialContext function that only dials
// addresses present in verifiedAddrs, rejecting everything else with an
// *SSRFError before ever calling dialer.DialContext. This is the mechanism
// that lets restrictedDoer connect by IP literal without re-resolving the
// hostname.
func newRestrictedDialContext(verifiedAddrs []net.IP, dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("restricted dial: parse address %q: %w", addr, err)
		}
		ip := net.ParseIP(host)
		if ip == nil || !slices.ContainsFunc(verifiedAddrs, ip.Equal) {
			return nil, &SSRFError{Endpoint: addr, Stage: SSRFStageDialRevalidation, Err: ErrUntrustedPDSEndpoint}
		}
		conn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrTransportFailure, err)
		}
		return conn, nil
	}
}
