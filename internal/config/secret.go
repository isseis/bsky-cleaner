package config

import "log/slog"

// redacted is the fixed placeholder returned by all string/log
// representations of SecretString, regardless of the wrapped value.
const redacted = "[REDACTED]"

// SecretString wraps a secret configuration value (e.g. an app password
// or a Slack webhook URL) so that fmt/slog formatting never exposes the
// underlying value. Callers must call Reveal only at the point of use
// (e.g. building an authentication request or a Slack notification
// payload), never pass the result to a logger or formatter.
type SecretString struct {
	value string
}

// Reveal returns the underlying secret value.
func (s SecretString) Reveal() string {
	return s.value
}

// String implements fmt.Stringer, masking the underlying value for %v/%s.
func (s SecretString) String() string {
	return redacted
}

// GoString implements fmt.GoStringer, masking the underlying value for %#v.
func (s SecretString) GoString() string {
	return redacted
}

// LogValue implements slog.LogValuer, masking the underlying value in
// structured log output.
func (s SecretString) LogValue() slog.Value {
	return slog.StringValue(redacted)
}
