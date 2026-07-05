package notify

import (
	"fmt"
	"strings"

	"github.com/isseis/bsky-cleaner/internal/report"
)

// Outcome is the input to buildPayload/Send: the structured result of a run
// (nil if the run failed before producing one, e.g. login/list failure) and
// the error that aborted the run (nil on a completed run, including one
// with partial delete failures -- those are represented in Result.Failed
// instead).
type Outcome struct {
	Result *report.Result
	Err    error
}

// maxPayloadLength is a conservative upper bound on the constructed Slack
// message text, guarding against unbounded payload growth on a run with a
// very large number of delete failures. It is not derived from a verified
// Slack platform limit; see the architecture doc 3.3節 for rationale.
const maxPayloadLength = 4000

// truncatedMarker is appended when buildPayload's output exceeds
// maxPayloadLength.
const truncatedMarker = "...(truncated)"

// escapeSlackMarkup applies Slack's mrkdwn escaping rules so a string of
// externally-sourced text cannot be interpreted as Slack markup -- in
// particular, its special mention syntax (<!channel>, <!here>,
// <!subteam^ID>, ...), which always starts with '<'. Order matters: '&'
// must be escaped first so the other two replacements' own '&' characters
// are not re-escaped.
func escapeSlackMarkup(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// sanitizeForPayload applies the two-stage sanitization the architecture
// doc 3.3節 describes for any externally-sourced text (post rkeys, error
// category text) included in a Slack payload: Sanitize (strip control
// characters/newlines) first, then escapeSlackMarkup (neutralize mrkdwn
// mention syntax).
func sanitizeForPayload(s string) string {
	return escapeSlackMarkup(Sanitize(s))
}

// buildPayload renders outcome as Slack mrkdwn text. It includes only the
// run's outcome (success/failure), delete count, failed posts' rkeys, and
// error category text (errorKind) -- never post body content, which
// atproto.Post has no field for in the first place. outcome.Result may be
// nil (the run aborted before producing one, e.g. a login failure); this
// never panics, treating the delete count as 0 and rendering only
// outcome.Err's category.
func buildPayload(outcome Outcome) string {
	var b strings.Builder

	switch {
	case outcome.Err != nil:
		fmt.Fprintf(&b, "bsky-cleaner run failed: %s\n", sanitizeForPayload(errorKind(outcome.Err)))
	case outcome.Result == nil:
		b.WriteString("bsky-cleaner run failed: unknown error\n")
	default:
		deleted := len(outcome.Result.Deleted)
		failed := len(outcome.Result.Failed)
		if failed == 0 {
			fmt.Fprintf(&b, "bsky-cleaner run succeeded: deleted %d post(s).\n", deleted)
		} else {
			fmt.Fprintf(&b, "bsky-cleaner run completed with failures: deleted %d post(s), %d failure(s).\n", deleted, failed)
		}
		for _, failure := range outcome.Result.Failed {
			fmt.Fprintf(&b, "  %s: %s\n", sanitizeForPayload(failure.Post.RKey), sanitizeForPayload(errorKind(failure.Err)))
		}
	}

	text := b.String()
	if len(text) <= maxPayloadLength {
		return text
	}
	return text[:maxPayloadLength-len(truncatedMarker)] + truncatedMarker
}
