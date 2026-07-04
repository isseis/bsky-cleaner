// Package atproto is a thin, self-written XRPC client limited to the
// login, list-posts, and delete-record operations this tool needs against
// a Bluesky (AT Protocol) account's PDS. DID resolution and PDS endpoint
// validation guard every operation against sending credentials to an
// untrusted host.
package atproto

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// Client is an AT Protocol XRPC client bound to a single PDS endpoint,
// resolved and validated by NewClient before any request is sent.
type Client struct {
	httpDoer   HTTPDoer
	pdsBaseURL *url.URL
	handle     string
	did        string
	session    *Session
}

// newPDSDoer builds the HTTPDoer NewClient installs for every request sent
// after PDS-endpoint validation succeeds (Login/ListPosts/DeleteRecord).
// Production always replaces httpDoer with a newly constructed, dial-pinned
// restrictedDoer that does not delegate to it (SSRF/DNS-rebinding
// protection, see newRestrictedDoer in http.go); this default is only ever
// reassigned by a //go:build test file
// (StubPassthroughPDSDoer in test_helpers.go), which installs the original
// httpDoer unchanged so a mock can drive those calls without requiring
// genuine network reachability to the resolved PDS endpoint. That override
// does not relax the checks that already ran by this point
// (resolveHandleToDID/resolveDIDDocument/validatePDSEndpoint) -- only the
// "which HTTPDoer actually sends the request" step changes.
var newPDSDoer = func(_ HTTPDoer, verifiedAddrs []net.IP, host string) HTTPDoer {
	return newRestrictedDoer(verifiedAddrs, host)
}

// NewClient resolves handle to its DID, resolves the DID document to a PDS
// service endpoint, and validates that endpoint is safe to connect to
// (fail-closed) before returning a *Client. No app password is accepted
// here, so a *Client can never be constructed in a state that would let
// Login send credentials to an unverified host.
func NewClient(ctx context.Context, handle string, httpDoer HTTPDoer) (*Client, error) {
	did, err := resolveHandleToDID(ctx, httpDoer, handle)
	if err != nil {
		return nil, err
	}

	serviceEndpoint, err := resolveDIDDocument(ctx, httpDoer, did)
	if err != nil {
		return nil, err
	}

	verifiedAddrs, host, err := validatePDSEndpoint(ctx, serviceEndpoint)
	if err != nil {
		return nil, err
	}

	pdsURL, err := url.Parse(serviceEndpoint)
	if err != nil {
		// validatePDSEndpoint already parsed serviceEndpoint successfully
		// (it must, to extract host/scheme), so this is unreachable.
		return nil, fmt.Errorf("parse validated PDS endpoint: %w", err)
	}

	return &Client{
		httpDoer:   newPDSDoer(httpDoer, verifiedAddrs, host),
		pdsBaseURL: pdsURL,
		handle:     handle,
		did:        did,
	}, nil
}
