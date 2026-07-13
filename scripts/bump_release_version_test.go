package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// scriptPath returns the absolute path to bump-release-version.sh, resolving
// from the test file's source directory.
func bumpScriptPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	return filepath.Join(filepath.Dir(filename), "bump-release-version.sh")
}

// setupRepo creates a temp directory containing scripts/bump-release-version.sh
// plus a docker-compose.yml and README.md/README.ja.md seeded with version
// v1.2.1, mirroring the real repo layout the script expects.
func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const version = "v1.2.1"

	require.NoError(t, os.Mkdir(filepath.Join(dir, "scripts"), 0o755))
	script, err := os.ReadFile(bumpScriptPath(t))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "bump-release-version.sh"), script, 0o755))

	compose := "services:\n  bsky-cleaner:\n    image: ghcr.io/isseis/bsky-cleaner:" + version + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644))

	readmeEN := "VERSION=" + version + "  # replace with the release version you want to use\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(readmeEN), 0o644))

	readmeJA := "VERSION=" + version + "  # 使いたいリリースバージョンに置き換える\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.ja.md"), []byte(readmeJA), 0o644))

	return dir
}

func runBumpScript(t *testing.T, dir string, args ...string) (exitCode int, output string) {
	t.Helper()
	cmdArgs := append([]string{filepath.Join(dir, "scripts", "bump-release-version.sh")}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	out, err := cmd.CombinedOutput()
	output = string(out)
	if err == nil {
		return 0, output
	}
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok, "command failed to start: %v", err)
	return exitErr.ExitCode(), output
}

func TestBumpReleaseVersion_UpdatesAllFiles(t *testing.T) {
	dir := setupRepo(t)

	code, output := runBumpScript(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "output: %s", output)

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compose), "image: ghcr.io/isseis/bsky-cleaner:v1.3.0")

	readmeEN, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(readmeEN), "VERSION=v1.3.0")

	readmeJA, err := os.ReadFile(filepath.Join(dir, "README.ja.md"))
	require.NoError(t, err)
	require.Contains(t, string(readmeJA), "VERSION=v1.3.0")
}

func TestBumpReleaseVersion_RejectsInvalidSemver(t *testing.T) {
	dir := setupRepo(t)

	code, _ := runBumpScript(t, dir, "not-a-version")
	require.Equal(t, 1, code)

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compose), "v1.2.1", "file must be left untouched on validation failure")
}

func TestBumpReleaseVersion_RejectsNewlineInjectedVersion(t *testing.T) {
	dir := setupRepo(t)

	// The first line is a valid version; the rest is a sed payload. A
	// line-based grep validator would accept this on its clean first line.
	// The whole-string [[ =~ ]] validator must reject it, leaving every
	// file untouched and never running the payload.
	code, _ := runBumpScript(t, dir, "v9.9.9\n#p;e touch injected-proof")
	require.Equal(t, 1, code)

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compose), "v1.2.1", "file must be left untouched on a rejected version")

	_, err = os.Stat(filepath.Join(dir, "injected-proof"))
	require.True(t, os.IsNotExist(err), "sed payload must not have executed")
}

func TestBumpReleaseVersion_RequiresExactlyOneArgument(t *testing.T) {
	dir := setupRepo(t)

	code, _ := runBumpScript(t, dir)
	require.Equal(t, 1, code)

	code, _ = runBumpScript(t, dir, "v1.3.0", "extra-arg")
	require.Equal(t, 1, code)
}

func TestBumpReleaseVersion_DoesNotTouchUnrelatedOccurrencesOfTheOldVersion(t *testing.T) {
	dir := setupRepo(t)

	// A comment mentioning the old version elsewhere in the file, sharing
	// the "image: ghcr.io/isseis/bsky-cleaner:vX.Y.Z" substring with the
	// real image line but on a different, non-image line.
	compose := "services:\n" +
		"  bsky-cleaner:\n" +
		"    # Changelog: previously pinned via image: ghcr.io/isseis/bsky-cleaner:v1.2.1 before an incident.\n" +
		"    image: ghcr.io/isseis/bsky-cleaner:v1.2.1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644))

	code, output := runBumpScript(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "output: %s", output)

	got, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(got), "image: ghcr.io/isseis/bsky-cleaner:v1.3.0", "the actual image line must be bumped")
	require.Contains(t, string(got), "previously pinned via image: ghcr.io/isseis/bsky-cleaner:v1.2.1",
		"an unrelated comment mentioning the old version must be left untouched")
}

func TestBumpReleaseVersion_FailsIfPatternMissing(t *testing.T) {
	dir := setupRepo(t)
	// Break the expected pattern in docker-compose.yml.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644))

	code, output := runBumpScript(t, dir, "v1.3.0")
	require.Equal(t, 1, code, "output: %s", output)

	readmeEN, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(readmeEN), "v1.2.1", "README must not be updated when an earlier file fails")
}
