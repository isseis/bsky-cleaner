package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecretString_StringRedacted(t *testing.T) {
	s := SecretString{value: "super-secret"}

	assert.Equal(t, redacted, fmt.Sprintf("%v", s))
	assert.Equal(t, redacted, s.String())
}

func TestSecretString_GoStringRedacted(t *testing.T) {
	s := SecretString{value: "super-secret"}

	assert.Equal(t, redacted, fmt.Sprintf("%#v", s))
}

func TestSecretString_LogValueRedacted(t *testing.T) {
	s := SecretString{value: "super-secret"}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("msg", "secret", s)

	out := buf.String()
	assert.NotContains(t, out, "super-secret", "log output leaked the underlying value")
	assert.Contains(t, out, redacted)
}

func TestSecretString_Reveal(t *testing.T) {
	s := SecretString{value: "super-secret"}

	assert.Equal(t, "super-secret", s.Reveal())
}
