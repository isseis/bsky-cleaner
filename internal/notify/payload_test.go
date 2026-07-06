package notify

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/report"
	"github.com/stretchr/testify/assert"
)

func TestEscapeSlackMarkup_EscapesAmpersandLtGt(t *testing.T) {
	got := escapeSlackMarkup("a & b < c > d")
	assert.Equal(t, "a &amp; b &lt; c &gt; d", got)
}

func TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus(t *testing.T) {
	outcome := Outcome{
		Result: &report.Result{
			Mode:    report.ModeApply,
			Deleted: []atproto.Post{{RKey: "a"}, {RKey: "b"}},
		},
	}
	got := buildPayload(outcome)
	assert.Contains(t, got, "2")
	assert.Contains(t, strings.ToLower(got), "succeeded")
}

func TestBuildPayload_RunError_IncludesErrorKind(t *testing.T) {
	someErr := &atproto.HTTPError{Method: "com.atproto.server.createSession", StatusCode: 401, Err: errors.New("unauthorized")}
	outcome := Outcome{Result: nil, Err: someErr}
	got := buildPayload(outcome)
	assert.Contains(t, got, "atproto http error: com.atproto.server.createSession status=401")
}

func TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind(t *testing.T) {
	err1 := &atproto.HTTPError{Method: "com.atproto.repo.deleteRecord", StatusCode: 500, Err: errors.New("boom")}
	err2 := &atproto.HTTPError{Method: "com.atproto.repo.deleteRecord", StatusCode: 429, Err: errors.New("rate limited")}
	outcome := Outcome{
		Result: &report.Result{
			Mode: report.ModeApply,
			Failed: []report.DeleteFailure{
				{Post: atproto.Post{RKey: "rkey1"}, Err: err1},
				{Post: atproto.Post{RKey: "rkey2"}, Err: err2},
			},
		},
	}
	got := buildPayload(outcome)
	assert.Contains(t, got, "rkey1")
	assert.Contains(t, got, "atproto http error: com.atproto.repo.deleteRecord status=500")
	assert.Contains(t, got, "rkey2")
	assert.Contains(t, got, "atproto http error: com.atproto.repo.deleteRecord status=429")
}

func TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields(t *testing.T) {
	err := &atproto.HTTPError{Method: "com.atproto.repo.deleteRecord", StatusCode: 500, Err: errors.New("boom")}
	outcome := Outcome{
		Result: &report.Result{
			Mode: report.ModeApply,
			Failed: []report.DeleteFailure{
				{Post: atproto.Post{RKey: "rkey1"}, Err: err},
			},
		},
	}
	got := buildPayload(outcome)
	// atproto.Post structurally has no body field; full equality (not
	// Contains) is required so an accidentally-added extra field would
	// actually fail this regression guard.
	want := "bsky-cleaner run completed with failures: deleted 0 post(s), 1 failure(s).\n" +
		"  rkey1: atproto http error: com.atproto.repo.deleteRecord status=500\n"
	assert.Equal(t, want, got)
}

func TestBuildPayload_EscapesMentionSyntaxInFailedRKey(t *testing.T) {
	err := errors.New("boom")
	outcome := Outcome{
		Result: &report.Result{
			Mode: report.ModeApply,
			Failed: []report.DeleteFailure{
				{Post: atproto.Post{RKey: "<!channel>"}, Err: err},
			},
		},
	}
	got := buildPayload(outcome)
	assert.NotContains(t, got, "<!channel>")
	assert.Contains(t, got, "&lt;!channel&gt;")
}

func TestBuildPayload_SanitizesANSIEscapeInFailedRKey(t *testing.T) {
	err := errors.New("boom")
	outcome := Outcome{
		Result: &report.Result{
			Mode: report.ModeApply,
			Failed: []report.DeleteFailure{
				{Post: atproto.Post{RKey: "rkey\x1b[31m"}, Err: err},
			},
		},
	}
	got := buildPayload(outcome)
	assert.NotContains(t, got, "\x1b")
}

func TestBuildPayload_SanitizesNewlineInFailedRKey(t *testing.T) {
	err := errors.New("boom")
	outcome := Outcome{
		Result: &report.Result{
			Mode: report.ModeApply,
			Failed: []report.DeleteFailure{
				{Post: atproto.Post{RKey: "evil\nFAKE LOG LINE"}, Err: err},
			},
		},
	}
	got := buildPayload(outcome)
	lineCountBefore := strings.Count("evil\nFAKE LOG LINE", "\n")
	assert.Equal(t, 1, lineCountBefore) // sanity check on the fixture itself
	assert.NotContains(t, got, "evil\nFAKE LOG LINE")
}

// TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint covers
// the outcome.Err branch of buildPayload, which the other mention/ANSI/
// newline sanitization tests above do not: SSRFError.Endpoint is derived
// from DID/PDS resolution (see internal/atproto's SSRF threat model) and so,
// like a failed post's RKey, is externally-influenced text that must not
// reach Slack as live mrkdwn mention syntax.
func TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint(t *testing.T) {
	err := &atproto.SSRFError{
		Endpoint: "https://evil.example.com/<!channel>",
		Stage:    atproto.SSRFStageInitialValidation,
		Err:      errors.New("rejected"),
	}
	outcome := Outcome{Result: nil, Err: err}

	got := buildPayload(outcome)

	assert.NotContains(t, got, "<!channel>")
	assert.Contains(t, got, "&lt;!channel&gt;")
}

func TestBuildPayload_TruncatesWhenExceedsLimit_AppendsTruncatedMarker(t *testing.T) {
	failures := make([]report.DeleteFailure, 0, 200)
	err := errors.New("boom")
	for range 200 {
		failures = append(failures, report.DeleteFailure{Post: atproto.Post{RKey: "abcdefghijklmnopqrstuvwxyz0123456789"}, Err: err})
	}
	outcome := Outcome{
		Result: &report.Result{
			Mode:   report.ModeApply,
			Failed: failures,
		},
	}
	got := buildPayload(outcome)
	assert.LessOrEqual(t, len(got), maxPayloadLength)
	assert.True(t, strings.HasSuffix(got, truncatedMarker))
}

// TestBuildPayload_TruncationIsUTF8Safe guards against cutting a
// multi-byte rune in half: RKey values here are all multi-byte Japanese
// characters, sized so the naive byte-offset cut point (maxPayloadLength
// - len(truncatedMarker)) lands mid-rune unless truncationCutPoint backs
// up to a rune boundary.
func TestBuildPayload_TruncationIsUTF8Safe(t *testing.T) {
	failures := make([]report.DeleteFailure, 0, 200)
	err := errors.New("boom")
	for range 200 {
		failures = append(failures, report.DeleteFailure{Post: atproto.Post{RKey: "日本語のリキー識別子です"}, Err: err})
	}
	outcome := Outcome{
		Result: &report.Result{
			Mode:   report.ModeApply,
			Failed: failures,
		},
	}

	got := buildPayload(outcome)

	assert.LessOrEqual(t, len(got), maxPayloadLength)
	assert.True(t, strings.HasSuffix(got, truncatedMarker))
	assert.True(t, utf8.ValidString(got), "truncated payload must not split a multi-byte rune")
}
