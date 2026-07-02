package config

import (
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
`)
	setAllCredentialEnv(t, "alice.bsky.social", "app-password", "https://hooks.slack.com/services/success", "https://hooks.slack.com/services/failure")

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

func TestLoadAppConfig_ConfigLoadError(t *testing.T) {
	path := writeTempTOML(t, `retention_days = [30`)
	setAllCredentialEnv(t, "alice.bsky.social", "app-password", "", "")

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
