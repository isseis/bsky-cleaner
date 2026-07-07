package sanitize_test

import (
	"testing"

	"github.com/isseis/bsky-cleaner/internal/sanitize"
	"github.com/stretchr/testify/assert"
)

func TestControlChars_RemovesANSIEscapeSequence(t *testing.T) {
	got := sanitize.ControlChars("before\x1b[31mred\x1b[0mafter")
	assert.Equal(t, "before[31mred[0mafter", got)
	assert.NotContains(t, got, "\x1b")
}

func TestControlChars_RemovesNewlinesAndCarriageReturns(t *testing.T) {
	got := sanitize.ControlChars("line1\nline2\r\nline3")
	assert.Equal(t, "line1line2line3", got)
}

func TestControlChars_LeavesOrdinaryTextUnchanged(t *testing.T) {
	input := "ordinary text with 日本語 and emoji 🎉"
	assert.Equal(t, input, sanitize.ControlChars(input))
}
