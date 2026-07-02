package config

import (
	"errors"
	"testing"
	"time"
)

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempTOML(t, `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d, want 30", cfg.RetentionDays)
	}
	if cfg.Schedule != "0 3 * * *" {
		t.Errorf("Schedule = %q, want %q", cfg.Schedule, "0 3 * * *")
	}
	if cfg.ExecutionTimeout != time.Hour {
		t.Errorf("ExecutionTimeout = %v, want %v", cfg.ExecutionTimeout, time.Hour)
	}
}

func TestLoad_SyntaxError(t *testing.T) {
	path := writeTempTOML(t, `retention_days = [30`)

	_, err := Load(path)
	if !errors.Is(err, ErrParseFailed) {
		t.Fatalf("Load() error = %v, want wrapping ErrParseFailed", err)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.toml")
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("Load() error = %v, want wrapping ErrFileNotFound", err)
	}
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
			if !errors.Is(err, ErrMissingField) {
				t.Fatalf("Load() error = %v, want wrapping ErrMissingField", err)
			}

			fieldErr, ok := errors.AsType[*FieldError](err)
			if !ok {
				t.Fatalf("Load() error = %v, want *FieldError", err)
			}
			if fieldErr.Field != tt.field {
				t.Errorf("FieldError.Field = %q, want %q", fieldErr.Field, tt.field)
			}
			if fieldErr.Value != "" {
				t.Errorf("FieldError.Value = %q, want empty string", fieldErr.Value)
			}
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
	if !errors.Is(err, ErrParseFailed) {
		t.Fatalf("Load() error = %v, want wrapping ErrParseFailed", err)
	}
}
