// Package notify builds a Slack Incoming Webhook payload from a run's
// outcome (report.Result / error) and sends it via internal/retry, without
// ever including post body content or leaking secrets (webhook URL,
// Authorization headers) into the payload or its own logging.
package notify

import "strings"

// Sanitize strips C0 control characters (0x00-0x1F, including ESC 0x1B,
// which neutralizes ANSI escape sequences) and DEL (0x7F) from s. Newlines
// (\n, \r) are C0 control characters and are removed by this same pass,
// preventing both log-line injection and Slack message structure
// corruption. Used by this package's own payload construction, and intended
// to also be used by cmd/main.go before writing externally-sourced
// identifiers/error text to stdout once that integration lands, so the
// implementation is centralized in one place rather than duplicated.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7F {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
