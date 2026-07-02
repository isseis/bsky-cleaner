package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
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
				if !errors.Is(err, ErrInvalidValue) {
					t.Fatalf("Load() error = %v, want wrapping ErrInvalidValue", err)
				}
				fieldErr, ok := errors.AsType[*FieldError](err)
				if !ok {
					t.Fatalf("Load() error = %v, want *FieldError", err)
				}
				if fieldErr.Value != strconv.Itoa(tt.retentionDays) {
					t.Errorf("FieldError.Value = %q, want %q", fieldErr.Value, strconv.Itoa(tt.retentionDays))
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if cfg.RetentionDays != tt.retentionDays {
				t.Errorf("RetentionDays = %d, want %d", cfg.RetentionDays, tt.retentionDays)
			}
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
				if !errors.Is(err, ErrInvalidValue) {
					t.Fatalf("Load() error = %v, want wrapping ErrInvalidValue", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			want := time.Duration(tt.seconds) * time.Second
			if cfg.ExecutionTimeout != want {
				t.Errorf("ExecutionTimeout = %v, want %v", cfg.ExecutionTimeout, want)
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
