package config

import (
	"os"
	"strings"
)

// ResolveHostname returns cfg.Hostname if non-empty after trimming whitespace
// (TOML-configured value takes priority), otherwise the result of
// os.Hostname(). If os.Hostname() also fails, it returns ("", err) so the
// caller can log the failure while still treating the hostname as empty for
// the notification payload (best-effort policy).
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
