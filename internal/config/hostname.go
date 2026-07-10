package config

import (
	"os"
	"strings"
)

// ResolveHostname returns cfg.Hostname if non-empty after trimming
// whitespace (TOML-configured value takes priority, AC-03), otherwise the
// result of os.Hostname() (AC-04). If os.Hostname() also fails, it returns
// ("", err) so the caller can log the failure (AC-17) while still treating
// the hostname as empty for the notification payload (AC-05 best-effort
// policy).
func ResolveHostname(cfg Config) (string, error) {
	if strings.TrimSpace(cfg.Hostname) != "" {
		return cfg.Hostname, nil
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return hostname, nil
}
