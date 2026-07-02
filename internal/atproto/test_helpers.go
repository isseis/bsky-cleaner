//go:build test

package atproto

import "net/url"

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
