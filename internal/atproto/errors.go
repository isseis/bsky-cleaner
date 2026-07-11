package atproto

import (
	"errors"
	"fmt"
)

// Sentinel errors identifying the category of a client-side failure.
// Callers use errors.Is to check for these regardless of which XRPC call
// produced them.
var (
	ErrDIDResolutionFailed = errors.New("DID resolution failed")
	// ErrDNSHandleResolutionFailed identifies a DNS TXT handle-resolution
	// failure: no record, multiple ambiguous records, NXDOMAIN, timeout, or
	// any other resolver-level error. Callers distinguish the specific
	// cause only if they need to (e.g. via errors.AsType[*net.DNSError]);
	// the fallback decision in resolveHandle treats all of these
	// uniformly.
	ErrDNSHandleResolutionFailed = errors.New("DNS TXT handle resolution failed")
	ErrUntrustedPDSEndpoint      = errors.New("PDS endpoint is not trusted")
	ErrAuthenticationFailed      = errors.New("authentication failed")
	ErrHTTPStatus                = errors.New("unexpected HTTP status")
	ErrTransportFailure          = errors.New("HTTP transport failure")            // timeout, DNS failure, connection refused, etc.
	ErrPaginationStalled         = errors.New("pagination cursor did not advance") // server protocol misbehavior, not a transport failure
	// ErrResponseTooLarge is returned when an XRPC response body exceeds
	// maxXRPCResponseBytes. It is wrapped in *HTTPError and returned to
	// the caller.
	ErrResponseTooLarge = errors.New("XRPC response exceeds size limit")
	// ErrPaginationLimitExceeded is returned when listAllRecords exceeds
	// any of its limits: total bytes, total pages, or total records.
	ErrPaginationLimitExceeded = errors.New("pagination byte/page/record limit exceeded")
	// ErrUnknownPostType is returned by collectionForPostType when given a
	// PostType it does not recognize, so DeleteRecord fails closed instead
	// of guessing a collection to delete from.
	ErrUnknownPostType = errors.New("unknown post type")
	// ErrSessionDIDMismatch reports that the DID returned by createSession
	// does not match the DID NewClient resolved and validated for the
	// handle, so the session must not be trusted for any authenticated
	// (delete) call.
	ErrSessionDIDMismatch = errors.New("session DID does not match resolved DID")
)

// SSRFStage identifies which validation step rejected a PDS endpoint, so a
// caller can distinguish an ordinary misconfiguration (rejected before any
// connection) from a same-request DNS answer change or redirect (rejected
// after the initial validation already passed).
type SSRFStage int

const (
	// SSRFStageInitialValidation means validatePDSEndpoint itself rejected
	// the endpoint (bad scheme or untrusted host), before any connection.
	SSRFStageInitialValidation SSRFStage = iota
	// SSRFStageDialRevalidation means the endpoint passed
	// validatePDSEndpoint but was rejected by re-validation afterward: the
	// DialContext wrapper refusing to connect to an address outside the
	// verified set, or CheckRedirect refusing a 3xx redirect. A
	// DialContext connection failure itself (transport failure, e.g.
	// timeout/connection refused) is ErrTransportFailure, not an
	// SSRFError -- it is not an SSRF rejection.
	SSRFStageDialRevalidation
)

func (s SSRFStage) String() string {
	switch s {
	case SSRFStageInitialValidation:
		return "InitialValidation"
	case SSRFStageDialRevalidation:
		return "DialRevalidation"
	default:
		return fmt.Sprintf("SSRFStage(%d)", int(s))
	}
}

// HTTPError identifies an XRPC call that received an unexpected HTTP
// response, or that failed at the transport level. It never embeds the raw
// *http.Request/*http.Response, only the XRPC method name and status code,
// so it cannot leak the Authorization header or request/response bodies
// (which may contain the app password or session JWT).
type HTTPError struct {
	Method     string // XRPC method name, e.g. "com.atproto.repo.listRecords"
	StatusCode int    // 0 for transport-level failures (no response received)
	ErrorName  string // ATProto XRPC error name (the body's "error" field), e.g. "RecordNotFound"; "" if absent/unparseable or for transport-level failures
	Err        error
}

func (e *HTTPError) Error() string {
	if e.ErrorName == "" {
		return fmt.Sprintf("%s: HTTP status %d: %v", e.Method, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("%s: HTTP status %d: %s: %v", e.Method, e.StatusCode, e.ErrorName, e.Err)
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}

// SSRFError identifies a DID resolution result, or a subsequent
// connection, rejected as an untrusted destination. It carries the
// scheme/host that was rejected -- neither value can contain secrets,
// since resolution happens before any app password is sent.
type SSRFError struct {
	Endpoint string
	Stage    SSRFStage
	Err      error
}

func (e *SSRFError) Error() string {
	return fmt.Sprintf("untrusted endpoint %q (stage %s): %v", e.Endpoint, e.Stage, e.Err)
}

func (e *SSRFError) Unwrap() error {
	return e.Err
}

// Permanent reports that an SSRFError must never be retried: retrying
// would not help, since the same verified-address check would reject the
// same endpoint again. This implements the internal/retry package's
// unexported permanentError interface via structural typing, without
// atproto importing internal/retry.
func (e *SSRFError) Permanent() bool { return true }
