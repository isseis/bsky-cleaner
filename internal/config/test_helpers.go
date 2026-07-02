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
