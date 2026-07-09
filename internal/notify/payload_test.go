package notify

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Contains(t, got.Text, emojiSuccess)
	assert.Contains(t, got.Text, "2")
	assert.Contains(t, strings.ToLower(got.Text), "succeeded")
	assert.Len(t, got.Attachments, 1)
	assert.Equal(t, colorGood, got.Attachments[0].Color)
	assert.Empty(t, got.Attachments[0].Fields)
}

func TestBuildPayload_RunError_IncludesErrorKind(t *testing.T) {
	someErr := &atproto.HTTPError{Method: "com.atproto.server.createSession", StatusCode: 401, Err: errors.New("unauthorized")}
	outcome := Outcome{Result: nil, Err: someErr}
	got := buildPayload(outcome)
	assert.Contains(t, got.Text, emojiFailure)
	assert.Contains(t, got.Text, "atproto http error: com.atproto.server.createSession status=401")
	assert.Len(t, got.Attachments, 1)
	assert.Equal(t, colorDanger, got.Attachments[0].Color)
	assert.Empty(t, got.Attachments[0].Fields)
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
	// AC-03: text should not contain individual failure details
	assert.Contains(t, got.Text, emojiFailure)
	assert.NotContains(t, got.Text, "rkey1")
	assert.NotContains(t, got.Text, "rkey2")
	// AC-07: color should be danger
	assert.Len(t, got.Attachments, 1)
	assert.Equal(t, colorDanger, got.Attachments[0].Color)
	// AC-04: failure details in fields
	assert.Len(t, got.Attachments[0].Fields, 1)
	assert.Contains(t, got.Attachments[0].Fields[0].Value, "rkey1")
	assert.Contains(t, got.Attachments[0].Fields[0].Value, "atproto http error: com.atproto.repo.deleteRecord status=500")
	assert.Contains(t, got.Attachments[0].Fields[0].Value, "rkey2")
	assert.Contains(t, got.Attachments[0].Fields[0].Value, "atproto http error: com.atproto.repo.deleteRecord status=429")
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
	require.Len(t, got.Attachments, 1)
	require.Len(t, got.Attachments[0].Fields, 1)
	// AC-04, AC-12: full equality on the field ensures no extra fields leak in
	assert.Equal(t, "Failed posts", got.Attachments[0].Fields[0].Title)
	assert.Equal(t, "rkey1: atproto http error: com.atproto.repo.deleteRecord status=500", got.Attachments[0].Fields[0].Value)
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
	require.Len(t, got.Attachments, 1)
	require.Len(t, got.Attachments[0].Fields, 1)
	assert.NotContains(t, got.Attachments[0].Fields[0].Value, "<!channel>")
	assert.Contains(t, got.Attachments[0].Fields[0].Value, "&lt;!channel&gt;")
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
	require.Len(t, got.Attachments, 1)
	require.Len(t, got.Attachments[0].Fields, 1)
	assert.NotContains(t, got.Attachments[0].Fields[0].Value, "\x1b")
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
	require.Len(t, got.Attachments, 1)
	require.Len(t, got.Attachments[0].Fields, 1)
	// Sanitize strips newlines, so the output must not contain a newline character
	assert.NotContains(t, got.Attachments[0].Fields[0].Value, "\n")
}

func TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint(t *testing.T) {
	err := &atproto.SSRFError{
		Endpoint: "https://evil.example.com/<!channel>",
		Stage:    atproto.SSRFStageInitialValidation,
		Err:      errors.New("rejected"),
	}
	outcome := Outcome{Result: nil, Err: err}

	got := buildPayload(outcome)

	assert.NotContains(t, got.Text, "<!channel>")
	assert.Contains(t, got.Text, "&lt;!channel&gt;")
}

func TestBuildPayload_FailureFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker(t *testing.T) {
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
	require.Len(t, got.Attachments, 1)
	require.Len(t, got.Attachments[0].Fields, 1)
	assert.LessOrEqual(t, len(got.Attachments[0].Fields[0].Value), maxPayloadLength)
	assert.True(t, strings.HasSuffix(got.Attachments[0].Fields[0].Value, truncatedMarker))
}

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

	require.Len(t, got.Attachments, 1)
	require.Len(t, got.Attachments[0].Fields, 1)
	assert.LessOrEqual(t, len(got.Attachments[0].Fields[0].Value), maxPayloadLength)
	assert.True(t, strings.HasSuffix(got.Attachments[0].Fields[0].Value, truncatedMarker))
	assert.True(t, utf8.ValidString(got.Attachments[0].Fields[0].Value), "truncated field value must not split a multi-byte rune")
}

func TestBuildPayload_TextTruncatesWhenExceedsLimit_AppendsTruncatedMarker(t *testing.T) {
	outcome := Outcome{
		Result: nil,
		Err: &atproto.HTTPError{
			Method:     "com.atproto.repo.deleteRecord",
			StatusCode: 500,
			ErrorName:  strings.Repeat("x", 5000),
		},
	}
	got := buildPayload(outcome)
	assert.LessOrEqual(t, len(got.Text), maxPayloadLength)
	assert.True(t, strings.HasSuffix(got.Text, truncatedMarker))
}

func TestIsFailure_FourOutcomePatterns(t *testing.T) {
	tests := []struct {
		name    string
		outcome Outcome
		want    bool
	}{
		{
			name: "complete_success",
			outcome: Outcome{
				Result: &report.Result{
					Deleted: []atproto.Post{{RKey: "a"}},
				},
			},
			want: false,
		},
		{
			name: "err_not_nil",
			outcome: Outcome{
				Result: &report.Result{},
				Err:    errors.New("some error"),
			},
			want: true,
		},
		{
			name: "result_nil_and_err_nil",
			outcome: Outcome{
				Result: nil,
				Err:    nil,
			},
			want: false,
		},
		{
			name: "partial_failure",
			outcome: Outcome{
				Result: &report.Result{
					Failed: []report.DeleteFailure{
						{Post: atproto.Post{RKey: "rkey1"}, Err: errors.New("boom")},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFailure(tt.outcome)
			assert.Equal(t, tt.want, got)
		})
	}
}
