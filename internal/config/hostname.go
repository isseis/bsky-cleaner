package config

import "os"

// ResolveHostname returns cfg.Hostname if non-empty (TOML-configured value
// takes priority), otherwise the result of os.Hostname(). If os.Hostname()
// also fails, it returns "" rather than propagating the error: a best-effort
// hostname field must never cause notification delivery itself to fail.
func ResolveHostname(cfg Config) string {
	if cfg.Hostname != "" {
		return cfg.Hostname
	}
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}
