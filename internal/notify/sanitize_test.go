package notify_test

import (
	"testing"

	"github.com/isseis/bsky-cleaner/internal/notify"
	"github.com/stretchr/testify/assert"
)

func TestSanitize_RemovesANSIEscapeSequence(t *testing.T) {
	input := "before\x1b[31mred\x1b[0mafter"
	got := notify.Sanitize(input)
	assert.Equal(t, "before[31mred[0mafter", got)
	assert.NotContains(t, got, "\x1b")
}

func TestSanitize_RemovesNewlinesAndCarriageReturns(t *testing.T) {
	input := "line1\nline2\r\nline3"
	got := notify.Sanitize(input)
	assert.Equal(t, "line1line2line3", got)
	assert.NotContains(t, got, "\n")
	assert.NotContains(t, got, "\r")
}

func TestSanitize_LeavesOrdinaryTextUnchanged(t *testing.T) {
	input := "ordinary text with 日本語 and emoji 🎉"
	got := notify.Sanitize(input)
	assert.Equal(t, input, got)
}
