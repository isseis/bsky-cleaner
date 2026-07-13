// Command bump-release-version updates the release version embedded in
// docker-compose.yml and README.md/README.ja.md ahead of a release.
//
// Usage:
//
//	go run ./scripts/bump_release_version vX.Y.Z
//
// or:
//
//	make bump-version ARGS=vX.Y.Z
//
// It does not commit, branch, or tag - run it, review the diff, then
// commit/push/PR/tag yourself (see docs/design/docker_deployment.md).
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// errInvalidVersion is returned when the version argument does not match
// the semver format vX.Y.Z anchored to the whole string.
var errInvalidVersion = errors.New("version does not match semver format vX.Y.Z")

// errorKind classifies why processing a single target file failed.
type errorKind int

const (
	errorKindFileNotFound errorKind = iota
	errorKindPatternNotFound
	errorKindIO
)

// updateError identifies which target file failed, and how, so callers can
// use errors.AsType[*updateError] and branch on Kind instead of matching on
// the error message string.
type updateError struct {
	Path string
	Kind errorKind
	Err  error
}

func (e *updateError) Error() string {
	return e.Err.Error()
}

func (e *updateError) Unwrap() error {
	return e.Err
}

// target describes one file whose embedded version string is rewritten.
// Pattern is the single regexp used for both the existence check and the
// replacement, so the replaced range is never wider than what was
// verified to be present. ReplacementTemplate is a fmt-style template with
// one %s for the new version, applied to Pattern's captured groups (e.g.
// "${1}%s" keeps a line-prefix group, "${1}%s${2}" also keeps a trailing
// group such as a comment).
type target struct {
	Path                string
	Pattern             *regexp.Regexp
	ReplacementTemplate string
}

// buildTargets returns the descriptors for the three files this tool
// updates, resolved relative to the current working directory (the caller
// is expected to run this from the repo root; see package doc comment).
func buildTargets() []target {
	return []target{
		{
			Path: "docker-compose.yml",
			// Anchored to a line that starts (after optional indentation)
			// with "image: ghcr.io/isseis/bsky-cleaner:" and ends right
			// after the version, so a commented-out digest-pinned line
			// (e.g. "# image: ghcr.io/isseis/bsky-cleaner@sha256:...")
			// never matches.
			Pattern:             regexp.MustCompile(`(?m)^( *image: ghcr\.io/isseis/bsky-cleaner:)v[0-9]+\.[0-9]+\.[0-9]+$`),
			ReplacementTemplate: "${1}%s",
		},
		{
			Path: "README.md",
			// Group 2 captures only the single boundary character (a
			// space, or end of line) right after the version, so any
			// further trailing comment on the line falls outside the
			// matched range and is left untouched.
			Pattern:             regexp.MustCompile(`(?m)^(VERSION=)v[0-9]+\.[0-9]+\.[0-9]+( |$)`),
			ReplacementTemplate: "${1}%s${2}",
		},
		{
			Path:                "README.ja.md",
			Pattern:             regexp.MustCompile(`(?m)^(VERSION=)v[0-9]+\.[0-9]+\.[0-9]+( |$)`),
			ReplacementTemplate: "${1}%s${2}",
		},
	}
}

// validateVersion checks that arg matches the semver format vX.Y.Z,
// anchored to the entire string (not just a line within it), so a
// multi-line or otherwise malformed argument is rejected outright.
func validateVersion(arg string) error {
	if !versionPattern.MatchString(arg) {
		return errInvalidVersion
	}
	return nil
}

// versionPattern has no (?m) flag, so ^/$ anchor to the start/end of the
// entire string rather than per line: a value like "v9.9.9\n<payload>"
// fails to match because $ requires the true end of the string, not just
// the end of the first line.
var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// preparedUpdate holds the in-memory result of phase 1 (validation) for one
// target: the replacement content and the original mode to restore after
// writing.
type preparedUpdate struct {
	path    string
	content []byte
	mode    fs.FileMode
}

// update rewrites all targets to version using two phases: phase 1
// validates every target (existence + pattern match) and builds the
// replacement content in memory without touching the filesystem; phase 2
// writes only if every target passed phase 1. A phase 2 write failure
// stops processing of the remaining targets. update performs no side
// effect beyond writing these target files.
func update(version string, targets []target) error {
	prepared := make([]preparedUpdate, 0, len(targets))
	var validationErrs []error

	for _, tg := range targets {
		p, err := prepareTarget(version, tg)
		if err != nil {
			validationErrs = append(validationErrs, err)
			continue
		}
		prepared = append(prepared, p)
	}
	if len(validationErrs) > 0 {
		return errors.Join(validationErrs...)
	}

	for _, p := range prepared {
		if err := writeFileAtomic(p.path, p.content, p.mode); err != nil {
			return &updateError{Path: p.path, Kind: errorKindIO, Err: err}
		}
	}
	return nil
}

// prepareTarget performs phase 1 for a single target: it verifies the file
// exists and matches Pattern, and returns the replacement content and the
// original mode without writing anything.
func prepareTarget(version string, tg target) (preparedUpdate, error) {
	info, err := os.Stat(tg.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return preparedUpdate{}, &updateError{
				Path: tg.Path,
				Kind: errorKindFileNotFound,
				Err:  fmt.Errorf("file not found: %s", tg.Path),
			}
		}
		return preparedUpdate{}, &updateError{
			Path: tg.Path,
			Kind: errorKindIO,
			Err:  fmt.Errorf("stat %s: %w", tg.Path, err),
		}
	}

	data, err := os.ReadFile(tg.Path) //nolint:gosec // tg.Path is one of a fixed set of target files, not external input
	if err != nil {
		return preparedUpdate{}, &updateError{Path: tg.Path, Kind: errorKindIO, Err: err}
	}

	if !tg.Pattern.Match(data) {
		return preparedUpdate{}, &updateError{
			Path: tg.Path,
			Kind: errorKindPatternNotFound,
			Err:  fmt.Errorf("expected pattern %q not found in %s", tg.Pattern.String(), tg.Path),
		}
	}

	replacement := fmt.Sprintf(tg.ReplacementTemplate, version)
	newContent := tg.Pattern.ReplaceAll(data, []byte(replacement))

	return preparedUpdate{path: tg.Path, content: newContent, mode: info.Mode().Perm()}, nil
}

// writeFileAtomic writes content to path by creating a randomly named
// temporary file in the same directory (so the final rename is atomic and
// on the same filesystem), restoring the original permission bits on that
// temp file, then renaming it over path. The temp file's name is
// unpredictable and freshly created, so a pre-planted symlink at a guessed
// path is never followed. Restoring mode before the rename (rather than
// after) means a chmod failure aborts without ever mutating path, and
// prevents the tracked file from silently downgrading to the temp file's
// default permissions.
func writeFileAtomic(path string, content []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	tmp, err := os.CreateTemp(dir, base+".*.tmp") //nolint:gosec // dir/base are derived from a fixed set of target files, not external input
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil { //nolint:gosec // tmpPath is freshly created for one of a fixed set of target files, not external input
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil { //nolint:gosec // tmpPath/path are derived from a fixed set of target files, not external input
		return err
	}

	success = true
	return nil
}

const usageProgName = "go run ./scripts/bump_release_version"

// run is the testable core of the entry point: it takes argv (excluding the
// program name) and output writers, and returns the process exit code.
// main is a thin wrapper: os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintf(stderr, "Usage: %s vX.Y.Z\n", usageProgName) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return 1
	}

	version := args[0]
	if err := validateVersion(version); err != nil {
		_, _ = fmt.Fprintf(stderr, "ERROR: version %q does not match semver format vX.Y.Z\n", version) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return 1
	}

	targets := buildTargets()
	if err := update(version, targets); err != nil {
		_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", err) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return 1
	}

	for _, tg := range targets {
		_, _ = fmt.Fprintf(stdout, "Updated %s\n", tg.Path) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	}
	_, _ = fmt.Fprintln(stdout)                                                                                                                         //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintf(stdout, "Bumped embedded version to %s in docker-compose.yml, README.md, README.ja.md.\n", version)                              //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintf(stdout, "Review the diff, then follow the release procedure in docs/design/docker_deployment.ja.md with VERSION=%s.\n", version) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply

	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
