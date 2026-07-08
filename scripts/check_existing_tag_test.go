//go:build test

package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// scriptPath returns the absolute path to check-existing-tag.sh, resolving
// from the test file's source directory.
func scriptPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	return filepath.Join(filepath.Dir(filename), "check-existing-tag.sh")
}

// runScript invokes check-existing-tag.sh with the given exit code as $1
// and the given stdin content, and returns the exit code.
func runScript(t *testing.T, exitCode string, stdin string) int {
	t.Helper()
	cmd := exec.Command(scriptPath(t), exitCode)
	cmd.Stdin = strings.NewReader(stdin)
	// We don't care about stdout/stderr content, just the exit code.
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	// Command failed to start (e.g. script not executable / missing) — treat
	// as inconclusive / fail-closed.
	t.Logf("runScript: command failed to start: %v", err)
	return 1
}

func TestCheckExistingTag_ManifestFound_ExitsFailClosed(t *testing.T) {
	// docker manifest inspect succeeded (exit 0) → tag exists → fail-closed.
	code := runScript(t, "0", "")
	require.Equal(t, 1, code, "existing tag should exit 1 (fail-closed)")
}

func TestCheckExistingTag_ManifestNotFound_ExitsSuccess(t *testing.T) {
	// docker manifest inspect failed with "manifest unknown" → tag does not
	// exist → safe to proceed.
	code := runScript(t, "1", "manifest unknown for ghcr.io/isseis/bsky-cleaner:nonexistent")
	require.Equal(t, 0, code, "confirmed-absent tag should exit 0 (safe to proceed)")
}

func TestCheckExistingTag_InconclusiveFailure_ExitsFailClosed(t *testing.T) {
	// docker manifest inspect failed with an unrelated error (e.g. rate
	// limit, registry 5xx) → inconclusive → fail-closed.
	code := runScript(t, "1", "rate limit exceeded: retry later")
	require.Equal(t, 1, code, "inconclusive failure should exit 1 (fail-closed)")
}
