//go:build test

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempTOML writes content to a temporary TOML file and returns its
// path.
func writeTempTOML(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTempTOML: %v", err)
	}
	return path
}

// unsetEnv removes the environment variable key for the duration of the
// test, restoring its prior value (or absence) on cleanup. Unlike
// t.Setenv, this leaves the variable genuinely unset rather than set to
// an empty string.
func unsetEnv(t *testing.T, key string) {
	t.Helper()

	prev, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetEnv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, prev)
		}
	})
}
