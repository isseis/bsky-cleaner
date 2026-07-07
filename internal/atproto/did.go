package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxDIDResponseBytes bounds how much of a handle-resolution response body
// this package will read; a bare DID string is at most a few hundred
// bytes, so this is a generous ceiling against a misbehaving server.
const maxDIDResponseBytes = 8 << 10

// maxDIDDocumentResponseBytes bounds how much of a DID document response
// body this package will decode; real DID documents are a few KB at most,
// so this is a generous ceiling against a misbehaving or malicious server.
const maxDIDDocumentResponseBytes = 64 << 10

// atprotoPDSServiceType is the DID document service "type" value that
// identifies the entry describing an account's PDS.
const atprotoPDSServiceType = "AtprotoPersonalDataServer"

// invalidHandleChars are characters that have special meaning in a URL (or
// are otherwise not valid in a DNS hostname) and must never appear in a
// handle used to build the well-known resolution URL: allowing them would
// let a malformed handle change the URL's structure (e.g. injecting a
// path, query, fragment, or userinfo component).
const invalidHandleChars = "/?#@ \t\r\n"

// resolveHandleToDID resolves handle to a DID using the HTTPS well-known
// method (GET https://{handle}/.well-known/atproto-did), the resolution
// mechanism that requires no separate directory service. The DNS TXT
// record method (_atproto.<handle> TXT "did=...") is not supported; an
// account that only configured DNS-based handle verification will fail to
// resolve here.
func resolveHandleToDID(ctx context.Context, httpDoer HTTPDoer, handle string) (string, error) {
	if strings.ContainsAny(handle, invalidHandleChars) {
		return "", fmt.Errorf("resolve handle to DID: invalid handle %q: %w", handle, ErrDIDResolutionFailed)
	}
	reqURL := "https://" + handle + "/.well-known/atproto-did"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("resolve handle to DID: build request: %w", err)
	}

	resp, err := httpDoer.Do(req)
	if err != nil {
		if ssrfErr, ok := errors.AsType[*SSRFError](err); ok {
			return "", ssrfErr
		}
		return "", fmt.Errorf("resolve handle to DID: %w: %w", ErrDIDResolutionFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve handle to DID: unexpected status %d: %w", resp.StatusCode, ErrDIDResolutionFailed)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDIDResponseBytes))
	if err != nil {
		return "", fmt.Errorf("resolve handle to DID: read response: %w: %w", ErrDIDResolutionFailed, err)
	}

	did := strings.TrimSpace(string(body))
	if !looksLikeDID(did) {
		return "", fmt.Errorf("resolve handle to DID: response is not a DID: %w", ErrDIDResolutionFailed)
	}
	return did, nil
}

// looksLikeDID reports whether s has the "did:" prefix common to every DID
// method AT Protocol uses, so callers can reject an obviously-malformed
// resolution result before it flows into resolveDIDDocument.
func looksLikeDID(s string) bool {
	return strings.HasPrefix(s, "did:")
}

// txtLookuper is the minimal DNS interface this package needs, so unit
// tests can supply a fake instead of performing a real DNS query.
type txtLookuper interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// lookupTXT defaults to net.DefaultResolver, structurally satisfying
// txtLookuper without an adapter. Tests substitute a fake resolver
// (stubTXTLookuper in did_test.go for now; a shared exported helper
// follows in a later phase of this task, see the implementation plan);
// resolveHandleToDIDViaDNS below references this package variable
// directly, exactly as checkRequestHostSafety/validatePDSEndpoint
// reference lookupIPAddr.
var lookupTXT txtLookuper = net.DefaultResolver

// dnsTXTLookupTimeout bounds a single DNS TXT lookup (NF-004), independent
// of xrpcRequestTimeout (http.go), which bounds HTTP requests. No retry is
// applied at this layer: resolveHandle's fallback to the HTTPS well-known
// method is the retry-equivalent for this method's failure.
const dnsTXTLookupTimeout = 3 * time.Second

// didTXTRecordPrefix identifies the TXT record value carrying the DID, per
// the AT Protocol DNS TXT handle-resolution method
// (_atproto.<handle> TXT "did=...").
const didTXTRecordPrefix = "did="

// resolveHandleToDIDViaDNS resolves handle to a DID using the DNS TXT
// record method. It returns ErrDNSHandleResolutionFailed-wrapped errors
// for "no record", "multiple candidate records", and resolver-level
// failures alike -- callers that only need to decide "fall back or not"
// can treat them uniformly via errors.Is. The underlying resolver error,
// if any, is preserved via %w so it remains available to
// errors.AsType[*net.DNSError] and similar.
func resolveHandleToDIDViaDNS(ctx context.Context, handle string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dnsTXTLookupTimeout)
	defer cancel()

	records, err := lookupTXT.LookupTXT(ctx, "_atproto."+handle)
	if err != nil {
		return "", fmt.Errorf("resolve handle to DID via DNS: %w: %w", ErrDNSHandleResolutionFailed, err)
	}

	var candidates []string
	for _, record := range records {
		if did, ok := strings.CutPrefix(record, didTXTRecordPrefix); ok && looksLikeDID(did) {
			candidates = append(candidates, did)
		}
	}

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("resolve handle to DID via DNS: no %q TXT record found: %w", didTXTRecordPrefix, ErrDNSHandleResolutionFailed)
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("resolve handle to DID via DNS: multiple %q TXT records found: %w", didTXTRecordPrefix, ErrDNSHandleResolutionFailed)
	}
}

// didDocument is the subset of a DID document this package needs: the
// service entries, one of which (identified by atprotoPDSServiceType)
// gives the account's PDS endpoint.
type didDocument struct {
	ID      string       `json:"id"`
	Service []didService `json:"service"`
}

type didService struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ServiceEndpoint string `json:"serviceEndpoint"`
}

// resolveDIDDocument fetches did's DID document and returns the service
// endpoint of its AtprotoPersonalDataServer entry (its PDS), unvalidated
// -- validatePDSEndpoint must be called on the result before it is used
// for any request carrying credentials.
func resolveDIDDocument(ctx context.Context, httpDoer HTTPDoer, did string) (serviceEndpoint string, err error) {
	docURL, err := didDocumentURL(did)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return "", fmt.Errorf("resolve DID document: build request: %w", err)
	}

	resp, err := httpDoer.Do(req)
	if err != nil {
		if ssrfErr, ok := errors.AsType[*SSRFError](err); ok {
			return "", ssrfErr
		}
		return "", fmt.Errorf("resolve DID document: %w: %w", ErrDIDResolutionFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve DID document: unexpected status %d: %w", resp.StatusCode, ErrDIDResolutionFailed)
	}

	var doc didDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDIDDocumentResponseBytes)).Decode(&doc); err != nil {
		return "", fmt.Errorf("resolve DID document: decode response: %w: %w", ErrDIDResolutionFailed, err)
	}

	for _, svc := range doc.Service {
		if svc.Type == atprotoPDSServiceType {
			return svc.ServiceEndpoint, nil
		}
	}
	return "", fmt.Errorf("resolve DID document: no %s service found: %w", atprotoPDSServiceType, ErrDIDResolutionFailed)
}

// didDocumentURL maps a DID to the URL its DID document is published at.
// Only did:plc (resolved via the PLC directory) and did:web (resolved via
// a well-known path on the identified domain) are supported; these are
// the two DID methods AT Protocol accounts use in practice.
func didDocumentURL(did string) (string, error) {
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		return "https://plc.directory/" + did, nil
	case strings.HasPrefix(did, "did:web:"):
		return didWebDocumentURL(did)
	default:
		return "", fmt.Errorf("resolve DID document: unsupported DID method %q: %w", did, ErrDIDResolutionFailed)
	}
}

// didWebDocumentURL implements the did:web method's URL mapping: each
// ":"-separated path segment after the domain is percent-decoded and
// becomes a path segment, and the document is always named did.json (at
// .well-known/did.json for a bare domain).
func didWebDocumentURL(did string) (string, error) {
	id := strings.TrimPrefix(did, "did:web:")
	parts := strings.Split(id, ":")
	for i, p := range parts {
		decoded, err := url.PathUnescape(p)
		if err != nil {
			return "", fmt.Errorf("resolve did:web document URL: %w: %w", ErrDIDResolutionFailed, err)
		}
		parts[i] = decoded
	}

	domain := parts[0]
	if strings.ContainsAny(domain, "/?#@") {
		return "", fmt.Errorf("resolve did:web document URL: invalid domain segment %q: %w", domain, ErrDIDResolutionFailed)
	}
	pathParts := parts[1:]
	if len(pathParts) == 0 {
		return "https://" + domain + "/.well-known/did.json", nil
	}
	for _, p := range pathParts {
		if strings.ContainsAny(p, "/?#@") {
			return "", fmt.Errorf("resolve did:web document URL: invalid path segment %q: %w", p, ErrDIDResolutionFailed)
		}
	}
	return "https://" + domain + "/" + strings.Join(pathParts, "/") + "/did.json", nil
}

// lookupIPAddr resolves host to its IP addresses. It defaults to
// net.DefaultResolver.LookupIPAddr, the real ctx-aware resolver production
// code always uses; it is a package variable purely so tests can
// substitute a fake resolver for the "one host returns both public and
// private addresses" boundary case without real network I/O.
var lookupIPAddr = net.DefaultResolver.LookupIPAddr

// isUnsafeIP reports whether ip is a destination this package must never
// connect (or send a request) to: private, loopback, link-local, or
// unspecified. AT Protocol is federated, so there is no host allow-list to
// fall back on -- this is the minimum guard against reaching internal
// network / metadata-service addresses.
func isUnsafeIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// checkRequestHostSafety resolves targetURL's host and rejects it if any
// resolved address is unsafe per isUnsafeIP. It guards every host this
// package sends an unauthenticated GET to during DID resolution (handle
// resolution and DID document fetch), not only to the final validated PDS
// endpoint: a malicious or compromised handle server, or a did:web DID
// whose domain segment is itself untrusted response data, could otherwise
// make this process issue requests to internal network addresses (blind
// SSRF) even though no credentials are sent at this stage. Callers reach
// this via newHostSafetyCheckedDoer below, not by calling it directly.
//
// A DNS lookup failure (lookupErr) is returned as a plain wrapped error,
// not an *SSRFError: since newHostSafetyCheckedDoer is wrapped in
// retry.NewDoer (client.go), and *SSRFError.Permanent() is always true, an
// *SSRFError here would make a merely transient resolver hiccup
// permanently fail the whole call instead of being retried like any other
// transient failure. Only an address that actually resolved and is unsafe
// (private/loopback/link-local/unspecified) represents a genuine,
// non-retryable policy violation.
func checkRequestHostSafety(ctx context.Context, targetURL string) error {
	u, parseErr := url.Parse(targetURL)
	if parseErr != nil || u.Scheme != "https" {
		return &SSRFError{Endpoint: targetURL, Stage: SSRFStageInitialValidation, Err: ErrUntrustedPDSEndpoint}
	}

	addrs, lookupErr := lookupIPAddr(ctx, u.Hostname())
	if lookupErr != nil {
		return fmt.Errorf("check request host safety for %q: %w: %w", targetURL, ErrDIDResolutionFailed, lookupErr)
	}
	if len(addrs) == 0 {
		return &SSRFError{Endpoint: targetURL, Stage: SSRFStageInitialValidation, Err: ErrUntrustedPDSEndpoint}
	}
	for _, addr := range addrs {
		if isUnsafeIP(addr.IP) {
			return &SSRFError{Endpoint: targetURL, Stage: SSRFStageInitialValidation, Err: ErrUntrustedPDSEndpoint}
		}
	}
	return nil
}

// newHostSafetyCheckedDoer wraps inner so that every Do call re-validates
// the request's target host via checkRequestHostSafety before delegating,
// rather than checking once before the first attempt. This keeps the
// DNS-rebinding protection intact even when a retrying HTTPDoer (see
// internal/retry) retries the same logical call multiple times: each
// retried attempt is a fresh Do call and therefore triggers a fresh check.
func newHostSafetyCheckedDoer(inner HTTPDoer) HTTPDoer {
	return &hostSafetyCheckedDoer{inner: inner}
}

type hostSafetyCheckedDoer struct {
	inner HTTPDoer
}

func (d *hostSafetyCheckedDoer) Do(req *http.Request) (*http.Response, error) {
	if err := checkRequestHostSafety(req.Context(), req.URL.String()); err != nil {
		return nil, err
	}
	return d.inner.Do(req)
}

// validatePDSEndpoint checks that serviceEndpoint is safe to send
// credentials to: its scheme must be https, and every address its host
// resolves to must be public and routable. It resolves the host exactly
// once and returns the full verified address set so the caller can pin
// all subsequent connections to those addresses without re-resolving
// (closing the DNS-rebinding TOCTOU window between this check and the
// first connection).
func validatePDSEndpoint(ctx context.Context, serviceEndpoint string) (verifiedAddrs []net.IP, host string, err error) {
	u, parseErr := url.Parse(serviceEndpoint)
	if parseErr != nil || u.Scheme != "https" {
		return nil, "", &SSRFError{Endpoint: serviceEndpoint, Stage: SSRFStageInitialValidation, Err: ErrUntrustedPDSEndpoint}
	}
	host = u.Hostname()

	addrs, lookupErr := lookupIPAddr(ctx, host)
	if lookupErr != nil {
		return nil, "", &SSRFError{Endpoint: serviceEndpoint, Stage: SSRFStageInitialValidation, Err: fmt.Errorf("%w: %w", ErrDIDResolutionFailed, lookupErr)}
	}
	if len(addrs) == 0 {
		return nil, "", &SSRFError{Endpoint: serviceEndpoint, Stage: SSRFStageInitialValidation, Err: ErrUntrustedPDSEndpoint}
	}

	for _, addr := range addrs {
		if isUnsafeIP(addr.IP) {
			return nil, "", &SSRFError{Endpoint: serviceEndpoint, Stage: SSRFStageInitialValidation, Err: ErrUntrustedPDSEndpoint}
		}
		verifiedAddrs = append(verifiedAddrs, addr.IP)
	}

	return verifiedAddrs, host, nil
}
