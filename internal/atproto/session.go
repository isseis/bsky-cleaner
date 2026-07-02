package atproto

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/isseis/bsky-cleaner/internal/config"
)

// secretRedacted is the fixed placeholder returned by all string/log
// representations of secretString, regardless of the wrapped value. Kept
// identical to config.SecretString's masking (internal/config/secret.go)
// so both types behave the same way under fmt/slog formatting.
const secretRedacted = "[REDACTED]"

// secretString wraps a secret value this package obtains itself (the
// session access JWT returned by createSession), masking it under
// fmt/slog formatting the same way config.SecretString does.
// config.SecretString cannot be reused here because it exposes no public
// constructor (internal/config/secret.go): it only wraps values the config
// package itself already holds (2.1 architecture note).
type secretString struct {
	value string
}

// newSecretString wraps value in a secretString.
func newSecretString(value string) secretString {
	return secretString{value: value}
}

// Reveal returns the underlying secret value.
func (s secretString) Reveal() string {
	return s.value
}

// String implements fmt.Stringer, masking the underlying value for %v/%s.
func (s secretString) String() string {
	return secretRedacted
}

// GoString implements fmt.GoStringer, masking the underlying value for %#v.
func (s secretString) GoString() string {
	return secretRedacted
}

// LogValue implements slog.LogValuer, masking the underlying value in
// structured log output.
func (s secretString) LogValue() slog.Value {
	return slog.StringValue(secretRedacted)
}

// Session holds the credentials obtained from a successful Login, used to
// authenticate subsequent XRPC calls (ListPosts, DeleteRecord).
type Session struct {
	DID       string
	AccessJWT secretString
}

// createSessionRequest is the com.atproto.server.createSession request
// body.
type createSessionRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"` //nolint:gosec // request DTO field, not a stored secret; value is read from config.SecretString.Reveal() only immediately before marshaling in Login
}

// createSessionResponse is the subset of the com.atproto.server.createSession
// response body this package needs.
type createSessionResponse struct {
	DID       string `json:"did"`
	AccessJWT string `json:"accessJwt"`
}

// Login authenticates as c.handle (resolved and verified by NewClient) with
// appPassword, and on success stores the returned session so subsequent
// calls (ListPosts, DeleteRecord) can authenticate. appPassword.Reveal() is
// called only here, immediately before building the request body -- never
// logged, never stored. On failure c.session is left unchanged (nil, if
// this is the first Login call), so no authenticated call can proceed.
func (c *Client) Login(ctx context.Context, appPassword config.SecretString) error {
	reqBody := createSessionRequest{
		Identifier: c.handle,
		Password:   appPassword.Reveal(),
	}

	var respBody createSessionResponse
	err := doXRPC(ctx, c.httpDoer, c.pdsBaseURL, http.MethodPost, "com.atproto.server.createSession", nil, reqBody, &respBody, "")
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	c.session = &Session{
		DID:       respBody.DID,
		AccessJWT: newSecretString(respBody.AccessJWT),
	}
	return nil
}
