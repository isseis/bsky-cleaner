// Package atproto is a thin, self-written XRPC client limited to the
// login, list-posts, and delete-record operations this tool needs against
// a Bluesky (AT Protocol) account's PDS. DID resolution and PDS endpoint
// validation guard every operation against sending credentials to an
// untrusted host.
package atproto

import (
	"context"
	"fmt"
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
		httpDoer:   newRestrictedDoer(verifiedAddrs, host),
		pdsBaseURL: pdsURL,
		handle:     handle,
		did:        did,
	}, nil
}
