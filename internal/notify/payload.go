package notify

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/isseis/bsky-cleaner/internal/report"
)

// Outcome is the input to buildPayload/Send: the structured result of a run
// (nil if the run failed before producing one, e.g. login/list failure) and
// the error that aborted the run (nil on a completed run, including one
// with partial delete failures -- those are represented in Result.Failed
// instead). Host and Account are always set from the TOML hostname field
// (or os.Hostname() fallback) and the configured Bluesky handle; Elapsed
// is meaningful only when Result != nil (a run that aborted before
// producing a Result has no statistics to report).
type Outcome struct {
	Result *report.Result
	Err    error

	// Host identifies the machine that ran bsky-cleaner (config.ResolveHostname's
	// result). Always set by cmd/main.go, regardless of Result/Err.
	Host string

	// Account is the Bluesky handle bsky-cleaner authenticated as
	// (config.Credentials.Handle). Always set by cmd/main.go, regardless
	// of Result/Err.
	Account string

	// Elapsed is the wall-clock duration of the runner.Run() call that
	// produced Result/Err, measured by cmd/main.go immediately before and
	// after that single call. Meaningful only when Result != nil;
	// buildPayload ignores it otherwise (a run that aborted before
	// producing a Result has no statistics to report).
	Elapsed time.Duration
}

// maxPayloadLength is a conservative upper bound on a single Slack text
// field (text or attachment field Value), guarding against unbounded
// payload growth on a run with a very large number of delete failures or a
// long errorKind string. Not a verified Slack platform limit; revisit if
// Slack is observed rejecting (4xx) payloads under this size.
const maxPayloadLength = 4000

// truncatedMarker is appended when truncate's output exceeds
// maxPayloadLength.
const truncatedMarker = "...(truncated)"

// webhookPayload is the Slack Incoming Webhook request body. text carries
// an emoji-prefixed one-line summary, kept separate from the failure
// detail; attachments always holds exactly one block whose fields begin
// with Host and Account (present on every run, success included), followed
// by a failure detail field (Error or Failed posts) when
// isFailure(outcome) is true. The block is always color-coded: colorDanger
// on failure, colorGood on success.
// Uses Slack's legacy attachments API (color + fields) rather than Block
// Kit -- still documented and supported by Slack's Incoming Webhooks, and
// sufficient for the success/failure summary this tool needs.
type webhookPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

// slackAttachment is the single block always present in webhookPayload
// (see webhookPayload). Its fields begin with Host and Account, followed by
// a failure detail field (the aborting error's category for a run-ending
// error, or every failed post's rkey and error category for partial delete
// failures) when the run failed.
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

// colorDanger is the Slack legacy attachment color for a failed run.
const colorDanger = "danger"

// colorGood is the Slack legacy attachment color for a successful run.
// Originally the success attachment carried no color (see 0013 Appendix:
// Decision History), but real-send verification (make notify-preview-send)
// found that a color-less attachment rendered with the client's default
// (blue) bar instead of no bar at all, reading as neither success nor
// failure, so an explicit color is now set on success too, symmetric with
// colorDanger.
const colorGood = "good"

// isFailure reports whether outcome represents a failed run: an error that
// aborted the run before completion, or a completed run with at least one
// delete failure. Both notify.go's destination-webhook selection (which
// Slack webhook URL a notification is sent to) and payload.go's
// attachment/emoji selection call this single function, so the two
// decisions cannot diverge.
func isFailure(outcome Outcome) bool {
	return outcome.Err != nil || outcome.Result == nil || len(outcome.Result.Failed) > 0
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

// sanitizeForPayload applies two-stage sanitization to any
// externally-sourced text (post rkeys, error category text) included in a
// Slack payload: Sanitize strips control characters and newlines first
// (defeating ANSI escape sequences and log/message-structure injection),
// then escapeSlackMarkup neutralizes Slack's mrkdwn mention syntax.
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

// buildPayload renders outcome as a Slack webhookPayload. text is always a
// short, fixed-shape, emoji-prefixed sentence with no numeric counts (counts
// live in the attachment's structured fields instead).
// Exactly one attachment is always generated whose fields always begin with
// Host and Account, followed by an Error or Failed posts field when the
// run failed. The attachment's Color is colorDanger on failure and
// colorGood on success.
func buildPayload(outcome Outcome) webhookPayload {
	var text string
	switch {
	case outcome.Err != nil:
		text = fmt.Sprintf("%s bsky-cleaner run failed.", emojiFailure)
	case outcome.Result == nil:
		text = fmt.Sprintf("%s bsky-cleaner run failed: unknown error.", emojiFailure)
	default:
		failedCount := len(outcome.Result.Failed)
		if failedCount == 0 {
			text = fmt.Sprintf("%s bsky-cleaner run succeeded.", emojiSuccess)
		} else {
			text = fmt.Sprintf("%s bsky-cleaner run completed with failures.", emojiFailure)
		}
	}
	text = truncate(text)

	// Always build one attachment with Host and Account fields.
	fields := []slackField{
		{Title: "Host", Value: truncate(sanitizeForPayload(outcome.Host))},
		{Title: "Account", Value: truncate(sanitizeForPayload(outcome.Account))},
	}

	// Add delete statistics only when the run reached the point of producing
	// a Result; a run that aborted before that (e.g. login failure) has
	// nothing to report here.
	if outcome.Result != nil {
		fields = append(fields,
			slackField{Title: "Targets", Value: strconv.Itoa(len(outcome.Result.Targets))},
			slackField{Title: "Deleted", Value: strconv.Itoa(len(outcome.Result.Deleted))},
			slackField{Title: "Duration", Value: outcome.Elapsed.String()},
		)
	}

	// Append failure detail fields (Error or Failed posts) if the run failed.
	if isFailure(outcome) {
		switch {
		case outcome.Err != nil:
			fields = append(fields, slackField{Title: "Error", Value: truncate(sanitizeForPayload(errorKind(outcome.Err)))})
		case outcome.Result != nil && len(outcome.Result.Failed) > 0:
			var b strings.Builder
			for i, failure := range outcome.Result.Failed {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(sanitizeForPayload(failure.Post.RKey))
				b.WriteString(": ")
				b.WriteString(sanitizeForPayload(errorKind(failure.Err)))
			}
			fields = append(fields, slackField{Title: "Failed posts", Value: truncate(b.String())})
		}
	}

	// Generate exactly one attachment. Color is danger on failure, good on
	// success.
	color := colorGood
	if isFailure(outcome) {
		color = colorDanger
	}
	attachments := []slackAttachment{{Color: color, Fields: fields}}

	return webhookPayload{Text: text, Attachments: attachments}
}
