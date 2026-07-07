// Package sanitize provides the low-level control-character stripping
// primitive shared by internal/notify (Slack payload text) and
// internal/report (stdout text), so externally-sourced values (post rkeys,
// error text) can be neutralized field-by-field while each caller keeps its
// own literal formatting (spaces, newlines) intact.
package sanitize

import "strings"

// ControlChars strips C0 control characters (0x00-0x1F, including ESC
// 0x1B, which neutralizes ANSI escape sequences, and \n/\r, which prevents
// log-line/message-structure injection) and DEL (0x7F) from s. Callers
// apply it to individual externally-sourced field values -- not to an
// entire pre-formatted multi-line report -- so their own structural
// newlines survive.
func ControlChars(s string) string {
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
