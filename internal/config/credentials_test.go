package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setAllCredentialEnv(t *testing.T, handle, appPassword, slackSuccessURL, slackFailureURL string) {
	t.Helper()
	t.Setenv("BSKY_HANDLE", handle)
	t.Setenv("BSKY_APP_PASSWORD", appPassword)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", slackSuccessURL)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", slackFailureURL)
}

func TestLoadCredentials_Success(t *testing.T) {
	setAllCredentialEnv(t, "alice.bsky.social", "app-password", "https://hooks.slack.com/services/success", "https://hooks.slack.com/services/failure")

	creds, err := LoadCredentials()
	require.NoError(t, err)

	assert.Equal(t, "alice.bsky.social", creds.Handle)
	assert.Equal(t, "app-password", creds.AppPassword.Reveal())
	assert.Equal(t, "https://hooks.slack.com/services/success", creds.SlackSuccessWebhookURL.Reveal())
	assert.Equal(t, "https://hooks.slack.com/services/failure", creds.SlackFailureWebhookURL.Reveal())
}

func TestLoadCredentials_MissingRequiredEnv(t *testing.T) {
	tests := []struct {
		name  string
		field string
	}{
		{name: "handle missing", field: "BSKY_HANDLE"},
		{name: "app password missing", field: "BSKY_APP_PASSWORD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BSKY_HANDLE", "alice.bsky.social")
			t.Setenv("BSKY_APP_PASSWORD", "app-password")
			unsetEnv(t, tt.field)

			_, err := LoadCredentials()
			require.ErrorIs(t, err, ErrMissingEnv)

			fieldErr, ok := errors.AsType[*FieldError](err)
			require.Truef(t, ok, "LoadCredentials() error = %v, want *FieldError", err)
			assert.Equal(t, tt.field, fieldErr.Field)
			assert.Empty(t, fieldErr.Value)
		})
	}
}

func TestLoadCredentials_SlackWebhookURLOptional(t *testing.T) {
	t.Setenv("BSKY_HANDLE", "alice.bsky.social")
	t.Setenv("BSKY_APP_PASSWORD", "app-password")
	unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_SUCCESS")
	unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_FAILURE")

	creds, err := LoadCredentials()
	require.NoError(t, err)

	assert.Equal(t, SecretString{}, creds.SlackSuccessWebhookURL)
	assert.Equal(t, SecretString{}, creds.SlackFailureWebhookURL)
}

func TestLoadCredentials_SlackWebhookURLInvalidScheme(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		url    string
	}{
		{name: "success http scheme", envVar: "BSKY_SLACK_WEBHOOK_URL_SUCCESS", url: "http://hooks.slack.com/services/success"},
		{name: "success syntactically invalid", envVar: "BSKY_SLACK_WEBHOOK_URL_SUCCESS", url: "://not-a-url"},
		{name: "failure http scheme", envVar: "BSKY_SLACK_WEBHOOK_URL_FAILURE", url: "http://hooks.slack.com/services/failure"},
		{name: "failure syntactically invalid", envVar: "BSKY_SLACK_WEBHOOK_URL_FAILURE", url: "://not-a-url"},
		{name: "success opaque URL", envVar: "BSKY_SLACK_WEBHOOK_URL_SUCCESS", url: "https:example.com"},
		{name: "success empty host", envVar: "BSKY_SLACK_WEBHOOK_URL_SUCCESS", url: "https:///path"},
		{name: "failure opaque URL", envVar: "BSKY_SLACK_WEBHOOK_URL_FAILURE", url: "https:example.com"},
		{name: "failure empty host", envVar: "BSKY_SLACK_WEBHOOK_URL_FAILURE", url: "https:///path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BSKY_HANDLE", "alice.bsky.social")
			t.Setenv("BSKY_APP_PASSWORD", "app-password")
			unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_SUCCESS")
			unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_FAILURE")
			t.Setenv(tt.envVar, tt.url)

			_, err := LoadCredentials()
			require.ErrorIs(t, err, ErrInvalidValue)

			fieldErr, ok := errors.AsType[*FieldError](err)
			require.Truef(t, ok, "LoadCredentials() error = %v, want *FieldError", err)
			assert.Equal(t, tt.envVar, fieldErr.Field)
			assert.Empty(t, fieldErr.Value)
		})
	}
}

func TestLoadCredentials_SlackWebhookURLOnlyOneSet(t *testing.T) {
	t.Setenv("BSKY_HANDLE", "alice.bsky.social")
	t.Setenv("BSKY_APP_PASSWORD", "app-password")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", "https://hooks.slack.com/services/success")
	unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_FAILURE")

	creds, err := LoadCredentials()
	require.NoError(t, err)

	assert.Equal(t, "https://hooks.slack.com/services/success", creds.SlackSuccessWebhookURL.Reveal())
	assert.Equal(t, SecretString{}, creds.SlackFailureWebhookURL)
}
