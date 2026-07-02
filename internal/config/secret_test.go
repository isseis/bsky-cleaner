package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretString_StringRedacted(t *testing.T) {
	s := SecretString{value: "super-secret"}

	if got := fmt.Sprintf("%v", s); got != redacted {
		t.Errorf("Sprintf(%%v) = %q, want %q", got, redacted)
	}
	if got := s.String(); got != redacted {
		t.Errorf("String() = %q, want %q", got, redacted)
	}
	if strings.Contains(fmt.Sprintf("%v", s), "super-secret") {
		t.Errorf("Sprintf(%%v) leaked the underlying value")
	}
}

func TestSecretString_GoStringRedacted(t *testing.T) {
	s := SecretString{value: "super-secret"}

	got := fmt.Sprintf("%#v", s)
	if got != redacted {
		t.Errorf("Sprintf(%%#v) = %q, want %q", got, redacted)
	}
	if strings.Contains(got, "super-secret") {
		t.Errorf("Sprintf(%%#v) leaked the underlying value")
	}
}

func TestSecretString_LogValueRedacted(t *testing.T) {
	s := SecretString{value: "super-secret"}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("msg", "secret", s)

	out := buf.String()
	if strings.Contains(out, "super-secret") {
		t.Errorf("log output leaked the underlying value: %s", out)
	}
	if !strings.Contains(out, redacted) {
		t.Errorf("log output = %q, want it to contain %q", out, redacted)
	}
}

func TestSecretString_Reveal(t *testing.T) {
	s := SecretString{value: "super-secret"}

	if got := s.Reveal(); got != "super-secret" {
		t.Errorf("Reveal() = %q, want %q", got, "super-secret")
	}
}
