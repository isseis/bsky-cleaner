// Package notify builds a Slack Incoming Webhook payload from a run's
// outcome (report.Result / error) and sends it via internal/retry, without
// ever including post body content or leaking secrets (webhook URL,
// Authorization headers) into the payload or its own logging.
package notify

import "github.com/isseis/bsky-cleaner/internal/sanitize"

// Sanitize strips C0 control characters (0x00-0x1F, including ESC 0x1B,
// which neutralizes ANSI escape sequences) and DEL (0x7F) from s. Newlines
// (\n, \r) are C0 control characters and are removed by this same pass,
// preventing both log-line injection and Slack message structure
// corruption. Used by this package's own payload construction, and by
// cmd/main.go for single-line, externally-sourced error text written to
// stderr. For pre-formatted multi-line text (e.g. report.FormatText's
// stdout output) callers sanitize individual field values with
// internal/sanitize.ControlChars instead, so structural newlines survive;
// this function delegates to the same primitive.
func Sanitize(s string) string {
	return sanitize.ControlChars(s)
}
