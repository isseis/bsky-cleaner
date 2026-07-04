//go:build test

package atproto

import (
	"net"
	"net/url"
	"testing"
)

// newTestClient builds a *Client with its unexported fields set directly,
// bypassing NewClient's DID resolution and Login's authentication so tests
// can exercise Login/ListPosts/DeleteRecord in isolation. session may be
// nil to exercise the "not logged in" guard paths. handle must be set by
// any test that asserts on Login's request body (Login always sends
// c.handle as the identifier, never a caller-supplied value).
func newTestClient(httpDoer HTTPDoer, pdsBaseURL *url.URL, handle, did string, session *Session) *Client {
	return &Client{
		httpDoer:   httpDoer,
		pdsBaseURL: pdsBaseURL,
		handle:     handle,
		did:        did,
		session:    session,
	}
}

// StubPassthroughPDSDoer overrides NewClient's post-validation HTTPDoer
// construction (newPDSDoer in client.go) to install the original httpDoer
// unchanged instead of replacing it with a newly constructed, non-delegating
// dial-pinned restrictedDoer, for the duration of t. This lets a test's mock
// HTTPDoer drive Login/ListPosts/DeleteRecord through a real *Client built
// via NewClient, without requiring genuine network reachability to the
// resolved PDS endpoint. It does not relax the SSRF checks that already ran
// by this point (resolveHandleToDID/resolveDIDDocument/validatePDSEndpoint)
// -- only the final "which HTTPDoer sends the request" step changes.
// Exported (unlike newTestClient above) so packages that cannot see
// atproto's unexported identifiers -- cmd, or a cross-package integration
// test importing internal/runner -- can still install it; only exists in
// test builds (//go:build test), so it is compiled out of, and
// unreachable from, any production binary.
func StubPassthroughPDSDoer(t *testing.T) {
	t.Helper()
	prev := newPDSDoer
	t.Cleanup(func() { newPDSDoer = prev })
	newPDSDoer = func(original HTTPDoer, _ []net.IP, _ string) HTTPDoer {
		return original
	}
}
