package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, 30, cfg.RetentionDays)
	assert.Equal(t, "0 3 * * *", cfg.Schedule)
	assert.Equal(t, time.Hour, cfg.ExecutionTimeout)
}

func TestLoad_SyntaxError(t *testing.T) {
	path := writeTempTOML(t, `retention_days = [30`)

	_, err := Load(path)
	require.ErrorIs(t, err, ErrParseFailed)
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.toml")
	require.ErrorIs(t, err, ErrFileNotFound)
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
		field   string
	}{
		{
			name: "retention_days missing",
			content: `
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`,
			field: "retention_days",
		},
		{
			name: "schedule missing",
			content: `
retention_days = 30
execution_timeout_seconds = 3600
`,
			field: "schedule",
		},
		{
			name: "execution_timeout_seconds missing",
			content: `
retention_days = 30
schedule = "0 3 * * *"
`,
			field: "execution_timeout_seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempTOML(t, tt.content)

			_, err := Load(path)
			require.ErrorIs(t, err, ErrMissingField)

			fieldErr, ok := errors.AsType[*FieldError](err)
			require.Truef(t, ok, "Load() error = %v, want *FieldError", err)
			assert.Equal(t, tt.field, fieldErr.Field)
			assert.Empty(t, fieldErr.Value)
		})
	}
}

func TestLoad_RetentionDaysValidation(t *testing.T) {
	tests := []struct {
		name          string
		retentionDays int
		wantErr       bool
	}{
		{name: "zero", retentionDays: 0, wantErr: true},
		{name: "negative", retentionDays: -1, wantErr: true},
		{name: "positive", retentionDays: 30, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := fmt.Sprintf(`
retention_days = %d
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`, tt.retentionDays)
			path := writeTempTOML(t, content)

			cfg, err := Load(path)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidValue)
				fieldErr, ok := errors.AsType[*FieldError](err)
				require.Truef(t, ok, "Load() error = %v, want *FieldError", err)
				assert.Equal(t, strconv.Itoa(tt.retentionDays), fieldErr.Value)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.retentionDays, cfg.RetentionDays)
		})
	}
}

func TestLoad_ExecutionTimeoutValidation(t *testing.T) {
	tests := []struct {
		name    string
		seconds int64
		wantErr bool
	}{
		{name: "zero", seconds: 0, wantErr: true},
		{name: "negative", seconds: -1, wantErr: true},
		{name: "exceeds max", seconds: maxExecutionTimeoutSeconds + 1, wantErr: true},
		{name: "extreme overflow-prone value", seconds: math.MaxInt64 / int64(time.Second), wantErr: true},
		{name: "valid", seconds: 3600, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := fmt.Sprintf(`
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = %d
`, tt.seconds)
			path := writeTempTOML(t, content)

			cfg, err := Load(path)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidValue)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, time.Duration(tt.seconds)*time.Second, cfg.ExecutionTimeout)
		})
	}
}

func TestLoad_SlackAllowedHostField_ParsesOptionalTOMLKey(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "key present",
			content: `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
`,
			want: "hooks.slack.com",
		},
		{
			name: "key absent",
			content: `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempTOML(t, tt.content)

			cfg, err := Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.SlackAllowedHost)
		})
	}
}

func TestLoad_UnknownKey(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
retension_days = 30
`)

	_, err := Load(path)
	require.ErrorIs(t, err, ErrParseFailed)
}
