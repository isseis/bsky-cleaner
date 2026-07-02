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
)

// maxDIDResponseBytes bounds how much of a handle-resolution response body
// this package will read; a bare DID string is at most a few hundred
// bytes, so this is a generous ceiling against a misbehaving server.
const maxDIDResponseBytes = 8 << 10

// atprotoPDSServiceType is the DID document service "type" value that
// identifies the entry describing an account's PDS.
const atprotoPDSServiceType = "AtprotoPersonalDataServer"

// resolveHandleToDID resolves handle to a DID using the HTTPS well-known
// method (GET https://{handle}/.well-known/atproto-did), the resolution
// mechanism that requires no separate directory service.
func resolveHandleToDID(ctx context.Context, httpDoer HTTPDoer, handle string) (string, error) {
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
	if !strings.HasPrefix(did, "did:") {
		return "", fmt.Errorf("resolve handle to DID: response is not a DID: %w", ErrDIDResolutionFailed)
	}
	return did, nil
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
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
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
	pathParts := parts[1:]
	if len(pathParts) == 0 {
		return "https://" + domain + "/.well-known/did.json", nil
	}
	return "https://" + domain + "/" + strings.Join(pathParts, "/") + "/did.json", nil
}

// lookupIPAddr resolves host to its IP addresses. It defaults to
// net.DefaultResolver.LookupIPAddr, the real ctx-aware resolver production
// code always uses; it is a package variable purely so tests can
// substitute a fake resolver for the "one host returns both public and
// private addresses" boundary case without real network I/O.
var lookupIPAddr = net.DefaultResolver.LookupIPAddr

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
		ip := addr.IP
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return nil, "", &SSRFError{Endpoint: serviceEndpoint, Stage: SSRFStageInitialValidation, Err: ErrUntrustedPDSEndpoint}
		}
		verifiedAddrs = append(verifiedAddrs, ip)
	}

	return verifiedAddrs, host, nil
}
