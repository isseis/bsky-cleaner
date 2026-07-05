package config

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAppConfig_Success(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
`)
	setAllCredentialEnv(t, "https://hooks.slack.com/services/success", "https://hooks.slack.com/services/failure")

	cfg, err := LoadAppConfig(path)
	require.NoError(t, err)

	assert.Equal(t, 30, cfg.RetentionDays)
	assert.Equal(t, "0 3 * * *", cfg.Schedule)
	assert.Equal(t, time.Hour, cfg.ExecutionTimeout)
	assert.Equal(t, "alice.bsky.social", cfg.Handle)
	assert.Equal(t, "app-password", cfg.AppPassword.Reveal())
	assert.Equal(t, "https://hooks.slack.com/services/success", cfg.SlackSuccessWebhookURL.Reveal())
	assert.Equal(t, "https://hooks.slack.com/services/failure", cfg.SlackFailureWebhookURL.Reveal())
}

func TestLoadAppConfig_SlackAllowedHost_BothURLsMatchAllowedHost_Succeeds(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
`)
	setAllCredentialEnv(t, "https://hooks.slack.com/services/success", "https://hooks.slack.com/services/failure")

	_, err := LoadAppConfig(path)
	require.NoError(t, err)
}

func TestLoadAppConfig_SlackAllowedHost_OnlySuccessURLSet_MatchesAllowedHost_Succeeds(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
`)
	setAllCredentialEnv(t, "https://hooks.slack.com/services/success", "")

	_, err := LoadAppConfig(path)
	require.NoError(t, err)
}

func TestLoadAppConfig_SlackAllowedHost_SuccessURLHostMismatch_ReturnsError(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
`)
	setAllCredentialEnv(t, "https://evil.example.com/services/success", "https://hooks.slack.com/services/failure")

	_, err := LoadAppConfig(path)
	require.ErrorIs(t, err, ErrWebhookHostMismatch)

	fieldErr, ok := errors.AsType[*FieldError](err)
	require.Truef(t, ok, "LoadAppConfig() error = %v, want *FieldError", err)
	assert.Equal(t, "BSKY_SLACK_WEBHOOK_URL_SUCCESS", fieldErr.Field)
}

func TestLoadAppConfig_SlackAllowedHost_FailureURLHostMismatch_ReturnsError(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
`)
	setAllCredentialEnv(t, "https://hooks.slack.com/services/success", "https://evil.example.com/services/failure")

	_, err := LoadAppConfig(path)
	require.ErrorIs(t, err, ErrWebhookHostMismatch)

	fieldErr, ok := errors.AsType[*FieldError](err)
	require.Truef(t, ok, "LoadAppConfig() error = %v, want *FieldError", err)
	assert.Equal(t, "BSKY_SLACK_WEBHOOK_URL_FAILURE", fieldErr.Field)
}

func TestLoadAppConfig_SlackAllowedHost_PortAndCaseIgnoredInComparison_Succeeds(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
`)
	setAllCredentialEnv(t, "https://Hooks.Slack.com:443/services/success", "https://Hooks.Slack.com:443/services/failure")

	_, err := LoadAppConfig(path)
	require.NoError(t, err)
}

func TestLoadAppConfig_SlackAllowedHost_MissingWhileWebhookURLSet_ReturnsError(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`)
	setAllCredentialEnv(t, "https://hooks.slack.com/services/success", "")

	_, err := LoadAppConfig(path)
	require.ErrorIs(t, err, ErrSlackAllowedHostMissing)

	fieldErr, ok := errors.AsType[*FieldError](err)
	require.Truef(t, ok, "LoadAppConfig() error = %v, want *FieldError", err)
	assert.Equal(t, "slack_allowed_host", fieldErr.Field)
}

func TestLoadAppConfig_SlackAllowedHost_NotRequiredWhenBothWebhookURLsUnset_Succeeds(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`)
	setAllCredentialEnv(t, "", "")

	_, err := LoadAppConfig(path)
	require.NoError(t, err)
}

func TestLoadAppConfig_ConfigLoadError(t *testing.T) {
	path := writeTempTOML(t, `retention_days = [30`)
	setAllCredentialEnv(t, "", "")

	_, err := LoadAppConfig(path)
	require.ErrorIs(t, err, ErrParseFailed)
}

func TestLoadAppConfig_CredentialsLoadError(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`)
	unsetEnv(t, "BSKY_HANDLE")
	unsetEnv(t, "BSKY_APP_PASSWORD")

	_, err := LoadAppConfig(path)
	require.ErrorIs(t, err, ErrMissingEnv)
}
