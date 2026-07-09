package notify

import (
	"fmt"
	"strings"
	"unicode/utf8"

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

// maxPayloadLength is a conservative upper bound on a single Slack text
// field (text or attachment field Value), guarding against unbounded
// payload growth on a run with a very large number of delete failures or a
// long errorKind string. It is not derived from a verified Slack platform
// limit; see the architecture doc section 3.3 for rationale.
const maxPayloadLength = 4000

// truncatedMarker is appended when truncate's output exceeds
// maxPayloadLength.
const truncatedMarker = "...(truncated)"

// webhookPayload is the Slack Incoming Webhook request body. text carries
// the emoji-prefixed one-line summary (F-001, F-002); attachments always
// holds exactly one color-coded block (AC-06/AC-07), whose fields entry
// carries the failure detail only when outcome has at least one delete
// failure to report (AC-04/AC-05). Uses Slack's legacy attachments API
// (color + fields), not Block Kit (see architecture doc section 5.3 for the
// compatibility rationale and section 9 for why Block Kit is deferred).
type webhookPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

// slackAttachment is a single color-coded block. buildPayload always
// produces exactly one (color always applies -- AC-06/AC-07 require it
// regardless of whether there is failure detail to list); Slack's
// attachments array supports more, but nothing in scope needs a second one
// (YAGNI).
type slackAttachment struct {
	Color  string       `json:"color,omitempty"`
	Fields []slackField `json:"fields,omitempty"`
}

// slackField is a single title/value pair rendered inside an attachment.
// Short=false is always used (this design never needs Slack's two-column
// layout), so the field is omitted from the JSON tag set rather than
// exposed as a knob no caller varies.
type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

// emojiSuccess is prepended to text for a fully successful run.
const emojiSuccess = "✅"

// emojiFailure is prepended to text for a run that has any failure (error
// outcome or partial delete failures).
const emojiFailure = "❌"

// colorGood is the Slack legacy attachment color for a successful run.
const colorGood = "good"

// colorDanger is the Slack legacy attachment color for a failed run.
const colorDanger = "danger"

// isFailure reports whether outcome represents a failed run: an error that
// aborted the run before completion, or a completed run with at least one
// delete failure. Both notify.go's destination-webhook selection (0006
// 3.4節) and payload.go's color/emoji selection (F-001・F-003) call this
// single function, so the two decisions cannot diverge (AC-08).
func isFailure(outcome Outcome) bool {
	return outcome.Err != nil || (outcome.Result != nil && len(outcome.Result.Failed) > 0)
}

// colorFor maps isFailure's result to a Slack legacy attachment color:
// "good" (green) for a fully successful run, "danger" (red) for any
// failure. No intermediate "warning" tier (see architecture doc appendix
// decision history).
func colorFor(failed bool) string {
	if failed {
		return colorDanger
	}
	return colorGood
}

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
// doc section 3.3 describes for any externally-sourced text (post rkeys, error
// category text) included in a Slack payload: Sanitize (strip control
// characters/newlines) first, then escapeSlackMarkup (neutralize mrkdwn
// mention syntax).
func sanitizeForPayload(s string) string {
	return escapeSlackMarkup(Sanitize(s))
}

// truncate cuts s to at most maxPayloadLength bytes, appending
// truncatedMarker at the end if truncation was necessary. It never splits a
// multi-byte UTF-8 rune in half, backing up to the nearest rune boundary
// as needed. s is returned unchanged if its byte length is already at or
// below maxPayloadLength.
func truncate(s string) string {
	if len(s) <= maxPayloadLength {
		return s
	}
	return s[:truncationCutPoint(s)] + truncatedMarker
}

// truncationCutPoint returns the byte offset to cut text at so that
// text[:cut]+truncatedMarker stays within maxPayloadLength without
// splitting a multi-byte UTF-8 rune in half (RKey/error text may contain
// non-ASCII characters). It backs up from the naive byte offset to the
// start of the rune straddling that offset, if any.
func truncationCutPoint(text string) int {
	cut := maxPayloadLength - len(truncatedMarker)
	if cut < 0 {
		cut = 0
	}
	if cut > len(text) {
		cut = len(text)
	}
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return cut
}

// buildPayload renders outcome as a Slack webhookPayload: an emoji-prefixed
// summary line (F-001, F-002) plus exactly one color-coded attachment
// (F-003, AC-06/AC-07) whose single field lists every failed post's rkey
// and error category, populated only when outcome has at least one delete
// failure to report (AC-04/AC-05). It includes only the run's outcome
// (success/failure), delete count, failed posts' rkeys, and error category
// text (errorKind) -- never post body content, which atproto.Post has no
// field for in the first place. outcome.Result may be nil (the run aborted
// before producing one, e.g. a login failure); this never panics, treating
// the delete count as 0 and rendering only outcome.Err's category. Both
// text and the failure-list field value are independently length-bounded
// (architecture doc 3.4節): unlike the pre-task version, text is not a
// fixed-length sentence -- the outcome.Err != nil branch embeds
// errorKind(outcome.Err), which can carry externally-sourced text of
// unbounded length.
func buildPayload(outcome Outcome) webhookPayload {
	failed := isFailure(outcome)

	var text string
	switch {
	case outcome.Err != nil:
		text = fmt.Sprintf("%s bsky-cleaner run failed: %s", emojiFailure, sanitizeForPayload(errorKind(outcome.Err)))
	case outcome.Result == nil:
		text = fmt.Sprintf("%s bsky-cleaner run failed: unknown error", emojiFailure)
	default:
		deleted := len(outcome.Result.Deleted)
		failedCount := len(outcome.Result.Failed)
		if failedCount == 0 {
			text = fmt.Sprintf("%s bsky-cleaner run succeeded: deleted %d post(s).", emojiSuccess, deleted)
		} else {
			text = fmt.Sprintf("%s bsky-cleaner run completed with failures: deleted %d post(s), %d failure(s).", emojiFailure, deleted, failedCount)
		}
	}
	text = truncate(text)

	attachment := slackAttachment{Color: colorFor(failed)}
	if outcome.Result != nil && len(outcome.Result.Failed) > 0 {
		var b strings.Builder
		for i, failure := range outcome.Result.Failed {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sanitizeForPayload(failure.Post.RKey))
			b.WriteString(": ")
			b.WriteString(sanitizeForPayload(errorKind(failure.Err)))
		}
		attachment.Fields = []slackField{
			{Title: "Failed posts", Value: truncate(b.String())},
		}
	}

	return webhookPayload{Text: text, Attachments: []slackAttachment{attachment}}
}
