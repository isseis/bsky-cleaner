// Package config reads and validates the TOML configuration file and
// environment variables, returning validated configuration values.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config holds the non-secret configuration values read from the TOML
// configuration file.
type Config struct {
	RetentionDays    int
	Schedule         string
	ExecutionTimeout time.Duration
}

// rawConfig mirrors the TOML file structure with pointer fields, so a
// missing key (nil) can be distinguished from an explicit zero value.
type rawConfig struct {
	RetentionDays           *int    `toml:"retention_days"`
	Schedule                *string `toml:"schedule"`
	ExecutionTimeoutSeconds *int    `toml:"execution_timeout_seconds"`
}

// Load reads and validates the TOML file at path and returns the
// non-secret configuration values.
//
// path is expected to be a trusted value already resolved by the caller
// (e.g. from a command-line flag), not raw external input.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a trusted, caller-resolved value, not external input
	if err != nil {
		if _, ok := errors.AsType[*fs.PathError](err); ok {
			return nil, fmt.Errorf("load config: %w: %w", ErrFileNotFound, err)
		}
		return nil, fmt.Errorf("load config: %w", err)
	}

	var raw rawConfig
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw); err != nil {
		return nil, fmt.Errorf("load config: %w: %w", ErrParseFailed, err)
	}

	switch {
	case raw.RetentionDays == nil:
		return nil, &FieldError{Field: "retention_days", Err: ErrMissingField}
	case raw.Schedule == nil:
		return nil, &FieldError{Field: "schedule", Err: ErrMissingField}
	case raw.ExecutionTimeoutSeconds == nil:
		return nil, &FieldError{Field: "execution_timeout_seconds", Err: ErrMissingField}
	}

	return &Config{
		RetentionDays:    *raw.RetentionDays,
		Schedule:         *raw.Schedule,
		ExecutionTimeout: time.Duration(*raw.ExecutionTimeoutSeconds) * time.Second,
	}, nil
}
