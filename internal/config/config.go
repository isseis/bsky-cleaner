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
	RetentionDays int
	// Schedule is the cron expression used only by the print-schedule
	// subcommand (for Docker/cron deployments). It is optional: running
	// the binary directly or registering it in crontab without Docker
	// does not need it. An absent TOML key yields "".
	Schedule         string
	ExecutionTimeout time.Duration
	// SlackAllowedHost is the required host for any configured Slack
	// webhook URL (validated by validateSlackAllowedHost in validate.go).
	// Unlike the other fields above, an empty string and an absent TOML
	// key are both treated as "not configured" -- there is no need to
	// distinguish them here.
	SlackAllowedHost string
}

// rawConfig mirrors the TOML file structure with pointer fields, so a
// missing key (nil) can be distinguished from an explicit zero value.
type rawConfig struct {
	RetentionDays           *int    `toml:"retention_days"`
	Schedule                *string `toml:"schedule"`
	ExecutionTimeoutSeconds *int    `toml:"execution_timeout_seconds"`
	SlackAllowedHost        string  `toml:"slack_allowed_host"`
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

	cfg, err := validateConfig(raw)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
