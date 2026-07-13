package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedRepo creates a temp directory containing docker-compose.yml,
// README.md, and README.ja.md seeded with version v1.2.1, mirroring the
// layout run expects relative to the current working directory.
// docker-compose.yml also carries a commented-out digest-pinned line, so
// tests can verify it is left untouched.
func seedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const version = "v1.2.1"

	compose := "services:\n" +
		"  bsky-cleaner:\n" +
		"    # image: ghcr.io/isseis/bsky-cleaner@sha256:deadbeefcafebabe0000000000000000000000000000000000000000000000\n" +
		"    image: ghcr.io/isseis/bsky-cleaner:" + version + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644))

	readmeEN := "VERSION=" + version + "  # replace with the release version you want to use\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(readmeEN), 0o644))

	readmeJA := "VERSION=" + version + "  # 使いたいリリースバージョンに置き換える\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.ja.md"), []byte(readmeJA), 0o644))

	return dir
}

// runInDir switches the current directory to dir (restored automatically by
// t.Chdir) and invokes run in-process, returning the exit code and
// stdout/stderr content.
func runInDir(t *testing.T, dir string, args ...string) (exitCode int, stdout, stderr string) {
	t.Helper()
	t.Chdir(dir)

	var outBuf, errBuf bytes.Buffer
	exitCode = run(args, &outBuf, &errBuf)
	return exitCode, outBuf.String(), errBuf.String()
}

func TestBumpReleaseVersion_UpdatesAllFiles(t *testing.T) {
	dir := seedRepo(t)

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

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
	dir := seedRepo(t)

	code, _, stderr := runInDir(t, dir, "not-a-version")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "does not match semver format")

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compose), "v1.2.1", "file must be left untouched on validation failure")
}

func TestBumpReleaseVersion_RejectsNewlineInjectedVersion(t *testing.T) {
	dir := seedRepo(t)

	// The first line is a valid version; the rest looks like an injected
	// payload. A line-based validator would accept this on its clean first
	// line. The whole-string anchored validator must reject it, leaving
	// every file untouched.
	code, _, _ := runInDir(t, dir, "v9.9.9\n#p;e touch injected-proof")
	require.Equal(t, 1, code)

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compose), "v1.2.1", "file must be left untouched on a rejected version")

	_, err = os.Stat(filepath.Join(dir, "injected-proof"))
	require.True(t, os.IsNotExist(err), "no payload must have been executed or written")
}

func TestBumpReleaseVersion_FailsIfPatternMissing(t *testing.T) {
	dir := seedRepo(t)
	// Break the expected pattern in docker-compose.yml.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644))

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 1, code, "stderr: %s", stderr)
	require.Contains(t, stderr, "docker-compose.yml")

	readmeEN, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(readmeEN), "v1.2.1", "README must not be updated when another target fails validation")
}

func TestValidateVersion(t *testing.T) {
	require.NoError(t, validateVersion("v1.2.3"))

	err := validateVersion("not-a-version")
	require.ErrorIs(t, err, errInvalidVersion)

	err = validateVersion("v9.9.9\n#p;e touch injected-proof")
	require.ErrorIs(t, err, errInvalidVersion)
}

func TestUpdate_ReturnsTypedErrorForEachFailureKind(t *testing.T) {
	pattern := regexp.MustCompile(`(?m)^(VERSION=)v[0-9]+\.[0-9]+\.[0-9]+$`)
	newTarget := func(path string) target {
		return target{Path: path, Pattern: pattern, ReplacementTemplate: "${1}%s"}
	}

	t.Run("file_not_found", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "VERSION")

		err := update("v1.3.0", []target{newTarget(missing)})
		require.Error(t, err)

		updErr, ok := errors.AsType[*updateError](err)
		require.True(t, ok, "update must return a *updateError, got %T: %v", err, err)
		require.Equal(t, errorKindFileNotFound, updErr.Kind)
		require.Equal(t, missing, updErr.Path)
	})

	t.Run("pattern_not_found", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "VERSION")
		require.NoError(t, os.WriteFile(path, []byte("no version here\n"), 0o644))

		err := update("v1.3.0", []target{newTarget(path)})
		require.Error(t, err)

		updErr, ok := errors.AsType[*updateError](err)
		require.True(t, ok, "update must return a *updateError, got %T: %v", err, err)
		require.Equal(t, errorKindPatternNotFound, updErr.Kind)
		require.Equal(t, path, updErr.Path)
	})
}

func TestBumpReleaseVersion_PreservesTrailingComment(t *testing.T) {
	dir := seedRepo(t)

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	readmeEN, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "VERSION=v1.3.0  # replace with the release version you want to use\n", string(readmeEN))
}

func TestBumpReleaseVersion_DoesNotTouchUnrelatedOccurrencesOfTheOldVersion(t *testing.T) {
	dir := seedRepo(t)

	// A comment mentioning the old version elsewhere in the file, sharing
	// the "image: ghcr.io/isseis/bsky-cleaner:vX.Y.Z" substring with the
	// real image line but on a different, non-image line.
	compose := "services:\n" +
		"  bsky-cleaner:\n" +
		"    # Changelog: previously pinned via image: ghcr.io/isseis/bsky-cleaner:v1.2.1 before an incident.\n" +
		"    image: ghcr.io/isseis/bsky-cleaner:v1.2.1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644))

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	got, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(got), "image: ghcr.io/isseis/bsky-cleaner:v1.3.0", "the actual image line must be bumped")
	require.Contains(t, string(got), "previously pinned via image: ghcr.io/isseis/bsky-cleaner:v1.2.1",
		"an unrelated comment mentioning the old version must be left untouched")
}

func TestBumpReleaseVersion_PreservesCommentedDigestLine(t *testing.T) {
	dir := seedRepo(t)

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	got, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(got),
		"# image: ghcr.io/isseis/bsky-cleaner@sha256:deadbeefcafebabe0000000000000000000000000000000000000000000000",
		"the commented-out digest-pinned line must be left untouched")
	require.Contains(t, string(got), "image: ghcr.io/isseis/bsky-cleaner:v1.3.0", "the real image line must be bumped")
}

func TestBumpReleaseVersion_DoesNotFollowPlantedSymlink(t *testing.T) {
	dir := seedRepo(t)

	// An attacker who can predict the temp file name could plant a symlink
	// there ahead of time to redirect the write outside the repo. Plant one
	// at a guessable "<file>.tmp" name pointing at a file outside dir, and
	// confirm it is never followed: the external target is untouched, the
	// planted symlink itself is untouched, and the real target is still
	// updated normally.
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "external-secret.txt")
	require.NoError(t, os.WriteFile(externalFile, []byte("do-not-touch"), 0o600))

	plantedLink := filepath.Join(dir, "docker-compose.yml.tmp")
	require.NoError(t, os.Symlink(externalFile, plantedLink))

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compose), "image: ghcr.io/isseis/bsky-cleaner:v1.3.0", "the real target must still be updated")

	external, err := os.ReadFile(externalFile)
	require.NoError(t, err)
	require.Equal(t, "do-not-touch", string(external), "the symlink target outside the repo must not be written to")

	linkInfo, err := os.Lstat(plantedLink)
	require.NoError(t, err)
	require.True(t, linkInfo.Mode()&os.ModeSymlink != 0, "the planted symlink must still be a symlink, not replaced")

	target, err := os.Readlink(plantedLink)
	require.NoError(t, err)
	require.Equal(t, externalFile, target, "the planted symlink must still point at its original target")
}

func TestBumpReleaseVersion_PreservesFileMode(t *testing.T) {
	dir := seedRepo(t)
	composePath := filepath.Join(dir, "docker-compose.yml")
	// Use a non-default mode so the assertion cannot pass by coincidence
	// (e.g. matching WriteFile's own default mode / the process umask).
	require.NoError(t, os.Chmod(composePath, 0o640))

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	info, err := os.Stat(composePath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestBumpReleaseVersion_RequiresExactlyOneArgument(t *testing.T) {
	dir := seedRepo(t)

	code, _, stderr := runInDir(t, dir)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "Usage:")

	code, _, stderr = runInDir(t, dir, "v1.3.0", "extra-arg")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "Usage:")
}

func TestBumpReleaseVersion_PerformsNoUnexpectedSideEffects(t *testing.T) {
	dir := seedRepo(t)

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{"docker-compose.yml", "README.md", "README.ja.md"}, names,
		"no files or directories beyond the three target files must be created")
}

func TestBumpReleaseVersion_ValidatesAllBeforeWriting(t *testing.T) {
	dir := seedRepo(t)
	// Break only README.ja.md's pattern; docker-compose.yml and README.md
	// remain valid. Even so, neither of them should be written.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.ja.md"), []byte("no version here\n"), 0o644))

	code, _, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 1, code, "stderr: %s", stderr)
	require.Contains(t, stderr, "README.ja.md")

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	require.NoError(t, err)
	require.Contains(t, string(compose), "v1.2.1", "a valid target must not be written when another target fails validation")

	readmeEN, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(readmeEN), "v1.2.1", "a valid target must not be written when another target fails validation")
}

func TestBumpReleaseVersion_FailClosedOnWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed when running as root")
	}

	// Three independent targets, each in its own directory, so a write
	// failure can be isolated to exactly the second one instead of hitting
	// all targets alike (which would not distinguish "stops after the
	// first phase 2 failure" from "fails uniformly for an unrelated
	// reason"). All three pass phase 1 (same pattern, same old version).
	seedTarget := func(mode os.FileMode) (dirPath, filePath string) {
		dirPath = t.TempDir()
		filePath = filepath.Join(dirPath, "VERSION")
		require.NoError(t, os.WriteFile(filePath, []byte("VERSION=v1.2.1\n"), mode))
		return dirPath, filePath
	}
	pattern := regexp.MustCompile(`(?m)^(VERSION=)v[0-9]+\.[0-9]+\.[0-9]+$`)
	newTarget := func(path string) target {
		return target{Path: path, Pattern: pattern, ReplacementTemplate: "${1}%s"}
	}

	_, file1 := seedTarget(0o644)
	dir2, file2 := seedTarget(0o644)
	_, file3 := seedTarget(0o644)

	// Block writes only into dir2, so file2's phase 2 write fails while
	// file1 (processed first) succeeds and file3 (processed after file2)
	// is never reached.
	require.NoError(t, os.Chmod(dir2, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(dir2, 0o755)
	})

	err := update("v1.3.0", []target{newTarget(file1), newTarget(file2), newTarget(file3)})
	require.Error(t, err)

	updErr, ok := errors.AsType[*updateError](err)
	require.True(t, ok, "update must return a *updateError, got %T: %v", err, err)
	require.Equal(t, errorKindIO, updErr.Kind)
	require.Equal(t, file2, updErr.Path)

	got1, readErr := os.ReadFile(file1)
	require.NoError(t, readErr)
	require.Contains(t, string(got1), "v1.3.0", "a target processed before the failing one must have been written")

	got2, readErr := os.ReadFile(file2)
	require.NoError(t, readErr)
	require.Contains(t, string(got2), "v1.2.1", "the failing target itself must be left unmodified")

	got3, readErr := os.ReadFile(file3)
	require.NoError(t, readErr)
	require.Contains(t, string(got3), "v1.2.1", "a target not yet reached by phase 2 must remain unmodified")
}

func TestBumpReleaseVersion_PrintsGuidanceOnSuccess(t *testing.T) {
	dir := seedRepo(t)

	code, stdout, stderr := runInDir(t, dir, "v1.3.0")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	require.Contains(t, stdout, "Updated docker-compose.yml")
	require.Contains(t, stdout, "Updated README.md")
	require.Contains(t, stdout, "Updated README.ja.md")
	require.Contains(t, stdout, "docs/design/docker_deployment.ja.md")
	require.Contains(t, stdout, "VERSION=v1.3.0")
}
