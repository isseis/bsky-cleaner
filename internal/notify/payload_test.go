package notify

import (
	"errors"
	"strings"
	"testing"
	"time"
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

func TestBuildPayload_SuccessOutcome_TextHasNoDeleteCount(t *testing.T) {
	outcome := Outcome{
		Result: &report.Result{
			Mode:    report.ModeApply,
			Deleted: []atproto.Post{{RKey: "a"}, {RKey: "b"}},
		},
		Host:    "worker-1",
		Account: "alice.bsky.social",
	}
	got := buildPayload(outcome)
	assert.Contains(t, got.Text, emojiSuccess)
	assert.NotContains(t, got.Text, "2")
	assert.Contains(t, strings.ToLower(got.Text), "succeeded")
	// Always one attachment with Host/Account fields, colored good on success.
	assert.Len(t, got.Attachments, 1)
	assert.Equal(t, colorGood, got.Attachments[0].Color)
	assert.Equal(t, "worker-1", findField(t, got.Attachments[0].Fields, "Host").Value)
	assert.Equal(t, "alice.bsky.social", findField(t, got.Attachments[0].Fields, "Account").Value)
}

func TestBuildPayload_RunError_IncludesErrorKind(t *testing.T) {
	someErr := &atproto.HTTPError{Method: "com.atproto.server.createSession", StatusCode: 401, Err: errors.New("unauthorized")}
	outcome := Outcome{Result: nil, Err: someErr}
	got := buildPayload(outcome)
	// text carries only a short summary, not the error category detail
	assert.Contains(t, got.Text, emojiFailure)
	assert.NotContains(t, got.Text, "atproto http error")
	// the error category goes into a structured, danger-colored field instead
	assert.Len(t, got.Attachments, 1)
	assert.Equal(t, colorDanger, got.Attachments[0].Color)
	// Host/Account/Error = 3 fields
	require.Len(t, got.Attachments[0].Fields, 3)
	assert.Equal(t, "Error", findField(t, got.Attachments[0].Fields, "Error").Title)
	assert.Contains(t, findField(t, got.Attachments[0].Fields, "Error").Value, "atproto http error: com.atproto.server.createSession status=401")
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
	// text should not contain individual failure details
	assert.Contains(t, got.Text, emojiFailure)
	assert.NotContains(t, got.Text, "rkey1")
	assert.NotContains(t, got.Text, "rkey2")
	assert.Len(t, got.Attachments, 1)
	assert.Equal(t, colorDanger, got.Attachments[0].Color)
	// failure details go into the attachment's fields instead
	val := findField(t, got.Attachments[0].Fields, "Failed posts").Value
	assert.Contains(t, val, "rkey1")
	assert.Contains(t, val, "atproto http error: com.atproto.repo.deleteRecord status=500")
	assert.Contains(t, val, "rkey2")
	assert.Contains(t, val, "atproto http error: com.atproto.repo.deleteRecord status=429")
}

func TestBuildPayload_PartialFailure_TextHasNoFailureCount(t *testing.T) {
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
	assert.NotContains(t, got.Text, "2")
}

func TestBuildPayload_TextRetainsEmojiForSuccessAndFailure(t *testing.T) {
	successOutcome := Outcome{
		Result: &report.Result{Mode: report.ModeApply, Deleted: []atproto.Post{{RKey: "a"}}},
	}
	partialFailureOutcome := Outcome{
		Result: &report.Result{
			Mode:   report.ModeApply,
			Failed: []report.DeleteFailure{{Post: atproto.Post{RKey: "rkey1"}, Err: errors.New("boom")}},
		},
	}
	runErrorOutcome := Outcome{
		Result: nil,
		Err:    &atproto.HTTPError{Method: "com.atproto.server.createSession", StatusCode: 401, Err: errors.New("unauthorized")},
	}

	assert.Contains(t, buildPayload(successOutcome).Text, emojiSuccess)
	assert.Contains(t, buildPayload(partialFailureOutcome).Text, emojiFailure)
	assert.Contains(t, buildPayload(runErrorOutcome).Text, emojiFailure)
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
	// Host/Account/Targets/Deleted/Duration/Failed posts = 6 fields.
	require.Len(t, got.Attachments[0].Fields, 6)
	// full equality on the Failed posts field ensures no extra fields leak in
	assert.Equal(t, "Failed posts", got.Attachments[0].Fields[5].Title)
	assert.Equal(t, "rkey1: atproto http error: com.atproto.repo.deleteRecord status=500", got.Attachments[0].Fields[5].Value)
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
	val := findField(t, got.Attachments[0].Fields, "Failed posts").Value
	assert.NotContains(t, val, "<!channel>")
	escapedMention := "&lt;!channel&gt;"
	assert.Contains(t, val, escapedMention)
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
	val := findField(t, got.Attachments[0].Fields, "Failed posts").Value
	assert.NotContains(t, val, "\x1b")
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
	val := findField(t, got.Attachments[0].Fields, "Failed posts").Value
	// Sanitize strips newlines, so the output must not contain a newline character
	assert.NotContains(t, val, "\n")
}

// TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint covers
// the outcome.Err branch of buildPayload, which the other mention/ANSI/
// newline sanitization tests above do not: SSRFError.Endpoint is derived
// from DID/PDS resolution (see internal/atproto's SSRF threat model) and so,
// like a failed post's RKey, is externally-influenced text that must not
// reach Slack as live mrkdwn mention syntax. Since the run-error errorKind
// detail lives in the attachment's Error field (not text), that field is
// the sanitization target here.
func TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint(t *testing.T) {
	err := &atproto.SSRFError{
		Endpoint: "https://evil.example.com/<!channel>",
		Stage:    atproto.SSRFStageInitialValidation,
		Err:      errors.New("rejected"),
	}
	outcome := Outcome{Result: nil, Err: err}

	got := buildPayload(outcome)

	require.Len(t, got.Attachments, 1)
	val := findField(t, got.Attachments[0].Fields, "Error").Value
	assert.NotContains(t, val, "<!channel>")
	escapedMention := "&lt;!channel&gt;"
	assert.Contains(t, val, escapedMention)
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
	val := findField(t, got.Attachments[0].Fields, "Failed posts").Value
	assert.LessOrEqual(t, len(val), maxPayloadLength)
	assert.True(t, strings.HasSuffix(val, truncatedMarker))
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
		failures = append(failures, report.DeleteFailure{Post: atproto.Post{RKey: "\u65e5\u672c\u8a9e\u306e\u30ea\u30ad\u30fc\u8b58\u5225\u5b50\u3067\u3059"}, Err: err})
	}
	outcome := Outcome{
		Result: &report.Result{
			Mode:   report.ModeApply,
			Failed: failures,
		},
	}

	got := buildPayload(outcome)

	require.Len(t, got.Attachments, 1)
	val := findField(t, got.Attachments[0].Fields, "Failed posts").Value
	assert.LessOrEqual(t, len(val), maxPayloadLength)
	assert.True(t, strings.HasSuffix(val, truncatedMarker))
	assert.True(t, utf8.ValidString(val), "truncated field value must not split a multi-byte rune")
}

// TestBuildPayload_ErrorFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker
// guards the run-error attachment field: errorKind(outcome.Err) can embed
// atproto.HTTPError.ErrorName, which is PDS-response-derived and has no
// length limit of its own (see
// docs/tasks/0012_slack_rich_formatting/02_architecture.md §3.4), so this
// field must be truncated independently of the (now fixed-length) text
// summary.
func TestBuildPayload_ErrorFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker(t *testing.T) {
	outcome := Outcome{
		Result: nil,
		Err: &atproto.HTTPError{
			Method:     "com.atproto.repo.deleteRecord",
			StatusCode: 500,
			ErrorName:  strings.Repeat("x", 5000),
		},
	}
	got := buildPayload(outcome)
	require.Len(t, got.Attachments, 1)
	val := findField(t, got.Attachments[0].Fields, "Error").Value
	assert.LessOrEqual(t, len(val), maxPayloadLength)
	assert.True(t, strings.HasSuffix(val, truncatedMarker))
}

// TestBuildPayload_ResultAndErrNil_AttachmentHasOnlyHostAccountFields guards the
// defensive outcome.Result == nil && outcome.Err == nil case: isFailure()
// reports this as a failure, but buildPayload has no failure detail to
// report (the Error and Failed-posts branches both require a non-nil
// source), so it must create an attachment with only the Host and Account
// fields (empty string values) but no additional fields.
func TestBuildPayload_ResultAndErrNil_AttachmentHasOnlyHostAccountFields(t *testing.T) {
	got := buildPayload(Outcome{})
	assert.Len(t, got.Attachments, 1)
	// isFailure() reports this defensive case as a failure (see doc comment
	// above), so the attachment is colored danger, not good.
	assert.Equal(t, colorDanger, got.Attachments[0].Color)
	assert.Equal(t, []slackField{
		{Title: "Host", Value: "", Short: true},
		{Title: "Account", Value: "", Short: true},
	}, got.Attachments[0].Fields)
}

func TestBuildPayload_SanitizesMentionSyntaxAndControlCharsInHostAndAccount(t *testing.T) {
	outcome := Outcome{
		Host:    "<!channel>",
		Account: "evil\x1b[31mhandle\nnewline",
	}
	got := buildPayload(outcome)
	require.Len(t, got.Attachments, 1)
	// Host should have escaped mention syntax
	hostVal := findField(t, got.Attachments[0].Fields, "Host").Value
	assert.NotContains(t, hostVal, "<!channel>")
	escapedMention := "&lt;!channel&gt;"
	assert.Contains(t, hostVal, escapedMention)
	// Account should have ANSI escape and newline stripped
	accVal := findField(t, got.Attachments[0].Fields, "Account").Value
	assert.NotContains(t, accVal, "\x1b")
	assert.NotContains(t, accVal, "\n")
}

func TestBuildPayload_ErrOutcome_IncludesHostAndAccountFields(t *testing.T) {
	someErr := &atproto.HTTPError{Method: "com.atproto.server.createSession", StatusCode: 401, Err: errors.New("unauthorized")}
	outcome := Outcome{
		Result:  nil,
		Err:     someErr,
		Host:    "worker-1",
		Account: "alice.bsky.social",
	}
	got := buildPayload(outcome)
	assert.Len(t, got.Attachments, 1)
	assert.Equal(t, colorDanger, got.Attachments[0].Color)
	// Verify Host and Account fields exist alongside Error field
	assert.Equal(t, "worker-1", findField(t, got.Attachments[0].Fields, "Host").Value)
	assert.Equal(t, "alice.bsky.social", findField(t, got.Attachments[0].Fields, "Account").Value)
	assert.Contains(t, findField(t, got.Attachments[0].Fields, "Error").Value, "atproto http error")
}

func TestBuildPayload_ResultNotNil_IncludesTargetsDeletedDurationFields(t *testing.T) {
	outcome := Outcome{
		Result: &report.Result{
			Mode:    report.ModeApply,
			Targets: []atproto.Post{{RKey: "a"}, {RKey: "b"}, {RKey: "c"}},
			Deleted: []atproto.Post{{RKey: "a"}, {RKey: "b"}},
		},
		Elapsed: 2 * time.Second,
	}
	got := buildPayload(outcome)
	require.Len(t, got.Attachments, 1)
	assert.Equal(t, "3", findField(t, got.Attachments[0].Fields, "Targets").Value)
	assert.Equal(t, "2", findField(t, got.Attachments[0].Fields, "Deleted").Value)
	assert.Equal(t, "2s", findField(t, got.Attachments[0].Fields, "Duration").Value)
}

func TestBuildPayload_ResultNil_ExcludesTargetsDeletedDurationFields(t *testing.T) {
	someErr := &atproto.HTTPError{Method: "com.atproto.server.createSession", StatusCode: 401, Err: errors.New("unauthorized")}
	outcome := Outcome{Result: nil, Err: someErr}
	got := buildPayload(outcome)
	require.Len(t, got.Attachments, 1)
	for _, title := range []string{"Targets", "Deleted", "Duration"} {
		for _, field := range got.Attachments[0].Fields {
			assert.NotEqual(t, title, field.Title)
		}
	}
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
			// internal/runner.Run's contract guarantees this combination
			// is never produced, but isFailure defensively treats a
			// missing Result as failure so that channel routing and
			// attachment rendering remain consistent if it leaks in.
			outcome: Outcome{
				Result: nil,
				Err:    nil,
			},
			want: true,
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
