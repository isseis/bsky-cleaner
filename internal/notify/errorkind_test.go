package notify

import (
	"errors"
	"fmt"
	"testing"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestErrorKind_ConfigFieldError_ReturnsFieldBasedCategory(t *testing.T) {
	err := &config.FieldError{Field: "slack_allowed_host", Err: config.ErrMissingField}
	got := errorKind(err)
	assert.Contains(t, got, "slack_allowed_host")
}

func TestErrorKind_AtprotoHTTPError_ReturnsMethodAndStatusBasedCategory(t *testing.T) {
	err := &atproto.HTTPError{
		Method:     "com.atproto.repo.deleteRecord",
		StatusCode: 500,
		ErrorName:  "InternalServerError",
		Err:        errors.New("boom"),
	}
	got := errorKind(err)
	assert.Contains(t, got, "com.atproto.repo.deleteRecord")
	assert.Contains(t, got, "500")
	assert.Contains(t, got, "InternalServerError")
}

func TestErrorKind_AtprotoSSRFError_ReturnsEndpointStageBasedCategory(t *testing.T) {
	err := &atproto.SSRFError{
		Endpoint: "https://evil.example.com",
		Stage:    atproto.SSRFStageDialRevalidation,
		Err:      errors.New("untrusted"),
	}
	got := errorKind(err)
	assert.Contains(t, got, "https://evil.example.com")
	assert.Contains(t, got, "DialRevalidation")
}

func TestErrorKind_UnknownErrorType_ReturnsUnknownErrorFallback(t *testing.T) {
	err := errors.New("some error")
	got := errorKind(err)
	assert.Equal(t, "unknown error", got)
}

func TestErrorKind_NeverIncludesRawErrorStringOrSecrets(t *testing.T) {
	secret := "Authorization: Bearer secret-token"
	err := fmt.Errorf("request failed: %s: %w", secret, errors.New("wrapped"))
	got := errorKind(err)
	assert.Equal(t, "unknown error", got)
	assert.NotContains(t, got, secret)
	assert.NotContains(t, got, "secret-token")
}
