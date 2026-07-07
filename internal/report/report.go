// Package report holds the structured outcome of a single runner.Run pass
// and its stdout text rendering, independent of how the result was produced.
package report

import (
	"fmt"
	"strings"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/sanitize"
)

// Mode identifies whether a run actually deleted posts or only reported
// what would be deleted.
type Mode int

// Mode values.
const (
	ModeDryRun Mode = iota
	ModeApply
)

// DeleteFailure pairs a deletion target with the error that occurred
// while deleting it.
type DeleteFailure struct {
	Post atproto.Post
	Err  error
}

// Result is the structured outcome of a single run, independent of how it
// is rendered. FormatText renders it for stdout; internal/notify's payload
// construction renders the same Result for a Slack webhook payload.
type Result struct {
	Mode    Mode
	Targets []atproto.Post  // posts SelectDeletionTargets judged eligible
	Deleted []atproto.Post  // ModeApply only: targets actually deleted
	Failed  []DeleteFailure // ModeApply only: targets whose deletion failed
}

// FormatText renders r as human-readable text for stdout. Never panics: a
// zero-value Result (nil slices) renders a "no targets" / zero-count report
// rather than erroring.
func FormatText(r Result) string {
	var b strings.Builder

	switch r.Mode {
	case ModeDryRun:
		if len(r.Targets) == 0 {
			b.WriteString("No posts to delete.\n")
			break
		}
		fmt.Fprintf(&b, "Posts to delete (%d):\n", len(r.Targets))
		for _, post := range r.Targets {
			fmt.Fprintf(&b, "  - %s\n", sanitize.ControlChars(post.RKey))
		}
	case ModeApply:
		fmt.Fprintf(&b, "Deleted %d post(s), %d failure(s).\n", len(r.Deleted), len(r.Failed))
		for _, failure := range r.Failed {
			fmt.Fprintf(&b, "  - %s: %s\n", sanitize.ControlChars(failure.Post.RKey), sanitize.ControlChars(failure.Err.Error()))
		}
	}

	return b.String()
}
