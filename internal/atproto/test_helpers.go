//go:build test

package atproto

import "net/url"

// newTestClient builds a *Client with its unexported fields set directly,
// bypassing NewClient's DID resolution and Login's authentication so tests
// can exercise ListPosts/DeleteRecord in isolation. session may be nil to
// exercise the "not logged in" guard paths.
func newTestClient(httpDoer HTTPDoer, pdsBaseURL *url.URL, did string, session *Session) *Client {
	return &Client{
		httpDoer:   httpDoer,
		pdsBaseURL: pdsBaseURL,
		did:        did,
		session:    session,
	}
}
