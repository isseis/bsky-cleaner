package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publicIPLiteral is a documented, non-routable-for-real-traffic address
// (RFC 5737 TEST-NET-3) that atproto's SSRF checks accept. Using it as
// both the handle and the DID-web host means net.DefaultResolver resolves
// it locally without any real DNS query (same rationale as
// internal/atproto/did_test.go's publicIPLiteral).
const publicIPLiteral = "203.0.113.5"

const testDID = "did:web:" + publicIPLiteral

// validConfigPath writes a config.toml with a 30-day retention period,
// the value every test in this file that reaches config.LoadAppConfig
// needs (posts fixed at year 2000 are unambiguously past any reasonable
// retention period).
func validConfigPath(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/config.toml"
	const body = "retention_days = 30\nschedule = \"0 3 * * *\"\nexecution_timeout_seconds = 3600\nslack_allowed_host = \"hooks.slack.com\"\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// hermeticHandler always answers the two DID-resolution requests
// (.well-known/atproto-did, .well-known/did.json) for publicIPLiteral,
// pointing the DID document's PDS serviceEndpoint back at the same IP
// literal so no real DNS resolution is ever required. Any other request is
// delegated to next, which each test sets up to handle
// createSession/listRecords/deleteRecord.
func hermeticHandler(t *testing.T, next func(req *http.Request) (*http.Response, error)) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == publicIPLiteral && req.URL.Path == "/.well-known/atproto-did":
			return atprototestutil.JSONResponse(http.StatusOK, testDID), nil
		case req.URL.Host == publicIPLiteral && req.URL.Path == "/.well-known/did.json":
			body := fmt.Sprintf(`{"id":%q,"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://%s"}]}`, testDID, publicIPLiteral)
			return atprototestutil.JSONResponse(http.StatusOK, body), nil
		default:
			return next(req)
		}
	}
}

func setEnvCredentials(t *testing.T) {
	t.Helper()
	atproto.StubDNSTXTLookup(t)
	t.Setenv("BSKY_HANDLE", publicIPLiteral)
	t.Setenv("BSKY_APP_PASSWORD", "app-password")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", "https://hooks.slack.com/services/success")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", "https://hooks.slack.com/services/failure")
}

func TestParseFlags_ConfigLongFlag_Accepted(t *testing.T) {
	configPath, _, err := parseFlags([]string{"--config", "path/to.toml"}, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, "path/to.toml", configPath)
}

func TestParseFlags_ConfigShortFlag_Accepted(t *testing.T) {
	configPath, _, err := parseFlags([]string{"-c", "path/to.toml"}, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, "path/to.toml", configPath)
}

func TestParseFlags_ApplyNotSpecified_DefaultsToFalse(t *testing.T) {
	_, apply, err := parseFlags([]string{"--config", "path/to.toml"}, &bytes.Buffer{})

	require.NoError(t, err)
	assert.False(t, apply)
}

func TestParseFlags_ApplySpecified_True(t *testing.T) {
	_, apply, err := parseFlags([]string{"--config", "path/to.toml", "--apply"}, &bytes.Buffer{})

	require.NoError(t, err)
	assert.True(t, apply)
}

func TestParseFlags_MissingConfig_ReturnsError(t *testing.T) {
	_, _, err := parseFlags([]string{}, &bytes.Buffer{})

	require.Error(t, err)
}

func TestParseFlags_UnknownFlag_ReturnsError(t *testing.T) {
	_, _, err := parseFlags([]string{"--unknown"}, &bytes.Buffer{})

	require.Error(t, err)
}

func TestParseFlags_UnexpectedPositionalArgument_ReturnsError(t *testing.T) {
	_, _, err := parseFlags([]string{"--config", "path/to.toml", "extra-arg"}, &bytes.Buffer{})

	require.Error(t, err)
}

func TestParseFlags_HelpLongFlag_ReturnsErrHelpAndPrintsFlagList(t *testing.T) {
	var out bytes.Buffer

	_, _, err := parseFlags([]string{"--help"}, &out)

	require.ErrorIs(t, err, flag.ErrHelp)
	assert.Contains(t, out.String(), "-config string")
	assert.Contains(t, out.String(), "-apply")
}

func TestParseFlags_HelpShortFlag_ReturnsErrHelpAndPrintsFlagList(t *testing.T) {
	var out bytes.Buffer

	_, _, err := parseFlags([]string{"-h"}, &out)

	require.ErrorIs(t, err, flag.ErrHelp)
	assert.Contains(t, out.String(), "-config string")
	assert.Contains(t, out.String(), "-apply")
}

func TestParseFlags_HelpFlag_DoesNotRequireConfig(t *testing.T) {
	_, _, err := parseFlags([]string{"--help"}, &bytes.Buffer{})

	require.ErrorIs(t, err, flag.ErrHelp)
}

// ── formatVersion tests ──────────────────────────────────────────────────────

func TestFormatVersion_WithCommit_ReturnsVersionAndCommit(t *testing.T) {
	origVersion, origCommit := version, commit
	t.Cleanup(func() { version, commit = origVersion, origCommit })
	version = "v1.2.3"
	commit = "a1b2c3d"

	got := formatVersion()
	assert.Equal(t, "v1.2.3 (a1b2c3d)", got)
}

func TestFormatVersion_NonDevVersionEmptyCommit_ReturnsVersionOnly(t *testing.T) {
	origVersion, origCommit := version, commit
	t.Cleanup(func() { version, commit = origVersion, origCommit })
	version = "v1.2.3"
	commit = ""

	got := formatVersion()
	assert.Equal(t, "v1.2.3", got)
}

func TestFormatVersion_EmptyCommit_ReturnsVersionOnly(t *testing.T) {
	origVersion, origCommit := version, commit
	t.Cleanup(func() { version, commit = origVersion, origCommit })
	version = "dev"
	commit = ""

	got := formatVersion()
	assert.Equal(t, "dev", got)
}

// ── --version/-v parseFlags tests ────────────────────────────────────────────

func TestParseFlags_VersionLongFlag_ReturnsErrVersionRequested(t *testing.T) {
	_, _, err := parseFlags([]string{"--version"}, &bytes.Buffer{})

	require.ErrorIs(t, err, errVersionRequested)
}

func TestParseFlags_VersionShortFlag_ReturnsErrVersionRequested(t *testing.T) {
	_, _, err := parseFlags([]string{"-v"}, &bytes.Buffer{})

	require.ErrorIs(t, err, errVersionRequested)
}

func TestParseFlags_VersionEqualsTrueForm_ReturnsErrVersionRequested(t *testing.T) {
	_, _, err := parseFlags([]string{"--version=true"}, &bytes.Buffer{})

	require.ErrorIs(t, err, errVersionRequested)
}

func TestParseFlags_VersionFlag_DoesNotRequireConfig(t *testing.T) {
	_, _, err := parseFlags([]string{"--version"}, &bytes.Buffer{})

	require.ErrorIs(t, err, errVersionRequested)
}

func TestParseFlags_UnknownFlag_PrintsFlagListToOut(t *testing.T) {
	var out bytes.Buffer

	_, _, err := parseFlags([]string{"--unknown"}, &out)

	require.Error(t, err)
	assert.NotErrorIs(t, err, flag.ErrHelp)
	assert.Contains(t, out.String(), "-config string")
}

func TestParseFlags_MissingConfig_PrintsFlagListToOut(t *testing.T) {
	var out bytes.Buffer

	_, _, err := parseFlags([]string{}, &out)

	require.Error(t, err)
	assert.Contains(t, out.String(), "-config string")
}

func TestRun_ConfigLoadFailure_ReturnsExitCode1(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run("/nonexistent/path/config.toml", false, time.Now(), &atprototestutil.MockHTTPDoer{}, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
}

func TestRun_ClientInitFailure_ReturnsExitCode1(t *testing.T) {
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	mock := &atprototestutil.MockHTTPDoer{Handler: func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == publicIPLiteral && req.URL.Path == "/.well-known/atproto-did" {
			return atprototestutil.JSONResponse(http.StatusNotFound, `{"error":"NotFound"}`), nil
		}
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		return nil, nil
	}}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Now(), mock, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
}

// TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1 verifies that
// exceeding the configured execution timeout aborts the run with a
// non-zero exit code: no prior test in this file actually drives the
// execution timeout to completion. execution_timeout_seconds is set to 1
// (the minimum config.LoadAppConfig accepts), and every request hangs
// until its ctx is canceled, forcing the timeout to fire during DID
// resolution. The resulting ctx error is retried once by internal/retry
// (a transient error, from retry's point of view), but
// retry.RealClock.Sleep sees the ctx is already done and returns
// immediately without actually waiting out defaultRetryPolicy.BaseDelay
// (1s) -- so this test still completes in about 1 second, not 1+1
// seconds, which the elapsed-time assertion below locks in.
func TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1(t *testing.T) {
	setEnvCredentials(t)
	path := t.TempDir() + "/config.toml"
	const body = "retention_days = 30\nschedule = \"0 3 * * *\"\nexecution_timeout_seconds = 1\nslack_allowed_host = \"hooks.slack.com\"\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	mock := &atprototestutil.MockHTTPDoer{Handler: func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}

	var stdout, stderr bytes.Buffer
	start := time.Now()
	code := run(path, false, time.Now(), mock, &stdout, &stderr)
	elapsed := time.Since(start)

	assert.Equal(t, exitSetupOrRunFail, code)
	assert.Less(t, elapsed, 2*time.Second)
}

func TestRun_LoginFailure_ReturnsExitCode1(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	mock := &atprototestutil.MockHTTPDoer{Handler: hermeticHandler(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession") {
			return atprototestutil.JSONResponse(http.StatusUnauthorized, `{"error":"AuthenticationRequired"}`), nil
		}
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		return nil, nil
	})}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Now(), mock, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
}

// TestRun_LoginFailure_StderrSanitizesMaliciousErrorName verifies that a
// login failure's stderr output is sanitized. The createSession XRPC
// response's "error" field is attacker/server-controlled and flows
// verbatim into atproto.HTTPError.ErrorName (internal/atproto/errors.go),
// which HTTPError.Error() interpolates with %s -- unlike SSRFError.Error(),
// which uses %q and so already escapes control characters on its own, this
// is a path where a raw newline or ANSI escape sequence would otherwise
// reach the terminal/log unescaped. Before phase 9's fix, cmd/main.go wrote
// runErr.Error() to stderr without notify.Sanitize().
func TestRun_LoginFailure_StderrSanitizesMaliciousErrorName(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const maliciousErrorName = "AuthenticationRequired\nFAKE LOG LINE\x1b[31m"
	errorNameJSON, err := json.Marshal(maliciousErrorName)
	require.NoError(t, err)
	body := fmt.Sprintf(`{"error":%s}`, errorNameJSON)

	mock := &atprototestutil.MockHTTPDoer{Handler: hermeticHandler(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession") {
			return atprototestutil.JSONResponse(http.StatusUnauthorized, body), nil
		}
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		return nil, nil
	})}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Now(), mock, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	// fmt.Fprintln appends its own trailing newline after the sanitized
	// text, so trim exactly that one before asserting: notify.Sanitize
	// strips every C0 control character (including the embedded newline
	// and ESC byte smuggled in via maliciousErrorName), so nothing but this
	// single trailing newline should remain.
	stderrText := strings.TrimSuffix(stderr.String(), "\n")
	assert.NotContains(t, stderrText, "\n")
	assert.NotContains(t, stderrText, "\x1b")
	assert.Contains(t, stderrText, "FAKE LOG LINE")
}

// postPageResponse builds a single-page com.atproto.repo.listRecords
// response for the app.bsky.feed.post collection containing one post with
// the given rkey and createdAt.
func postPageResponse(rkey, createdAt string) string {
	record := fmt.Sprintf(
		`{"uri":"at://%s/app.bsky.feed.post/%s","cid":"bafycid","value":{"$type":"app.bsky.feed.post","text":"post","createdAt":%q}}`,
		testDID, rkey, createdAt,
	)
	return atprototestutil.ListRecordsResponseJSON([]string{record}, "")
}

func emptyPageResponse() string {
	return atprototestutil.ListRecordsResponseJSON(nil, "")
}

// listRecordsHandler answers com.atproto.repo.listRecords, returning
// postsPage for the app.bsky.feed.post collection and an empty page for
// every other collection this package lists (reposts, pinned-post
// profile lookups etc. do not need their own dedicated fixtures here).
func listRecordsHandler(t *testing.T, postsPage string) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession"):
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.CreateSessionResponseJSON(testDID, "access-jwt")), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.post":
			return atprototestutil.JSONResponse(http.StatusOK, postsPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords"):
			return atprototestutil.JSONResponse(http.StatusOK, emptyPageResponse()), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.getRecord"):
			return atprototestutil.JSONResponse(http.StatusBadRequest, `{"error":"RecordNotFound"}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}
}

func TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: hermeticHandler(t, listRecordsHandler(t, postsPage))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout.String(), rkey)
	assert.Empty(t, stderr.String())
}

func TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	postsPage := emptyPageResponse()
	mock := &atprototestutil.MockHTTPDoer{Handler: hermeticHandler(t, listRecordsHandler(t, postsPage))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout.String(), "No posts to delete")
}

// readDeleteRecordRKey extracts the "rkey" field from a
// com.atproto.repo.deleteRecord request body, restoring req.Body
// afterward so MockHTTPDoer's own request recording (which also reads the
// body) keeps working.
func readDeleteRecordRKey(req *http.Request) (string, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(raw))

	var body struct {
		RKey string `json:"rkey"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", err
	}
	return body.RKey, nil
}

// deleteRecordHandler wraps listRecordsHandler, additionally answering
// com.atproto.repo.deleteRecord requests via deleteResp (keyed by rkey
// extracted from the JSON request body) so apply-mode tests can script
// per-post success/failure.
func deleteRecordHandler(t *testing.T, postsPage string, deleteStatus map[string]int) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	base := listRecordsHandler(t, postsPage)
	return hermeticHandler(t, func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "com.atproto.repo.deleteRecord") {
			return base(req)
		}
		rkey, err := readDeleteRecordRKey(req)
		require.NoError(t, err)
		status, ok := deleteStatus[rkey]
		require.True(t, ok, "unexpected deleteRecord rkey: %s", rkey)
		if status >= 200 && status < 300 {
			return atprototestutil.JSONResponse(status, atprototestutil.DeleteRecordResponseJSON()), nil
		}
		return atprototestutil.JSONResponse(status, `{"error":"InvalidRequest"}`), nil
	})
}

// slackWebhookHandler wraps next, additionally answering any request whose
// host is hooks.slack.com with the given status -- the Slack webhook
// destination configured by setEnvCredentials/validConfigPath in apply-mode
// tests that set up a webhook (not every apply-mode test in this file: some,
// like TestRun_Apply_NoWebhookConfigured_SkipsNotifyWithoutError, deliberately
// leave no webhook configured).
func slackWebhookHandler(status int, next func(req *http.Request) (*http.Response, error)) func(req *http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "hooks.slack.com" {
			return atprototestutil.JSONResponse(status, "ok"), nil
		}
		return next(req)
	}
}

func TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusOK, deleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusOK}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout.String(), "Deleted 1 post(s), 0 failure(s)")
	assert.Empty(t, stderr.String())
	assertOnlySlackRequestURL(t, mock, "https://hooks.slack.com/services/success")
}

func TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const okRkey = "old-post-ok"
	const failRkey = "old-post-fail"
	record := func(rkey string) string {
		return fmt.Sprintf(
			`{"uri":"at://%s/app.bsky.feed.post/%s","cid":"bafycid","value":{"$type":"app.bsky.feed.post","text":"post","createdAt":"2000-01-01T00:00:00Z"}}`,
			testDID, rkey,
		)
	}
	postsPage := atprototestutil.ListRecordsResponseJSON([]string{record(okRkey), record(failRkey)}, "")
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusOK, deleteRecordHandler(t, postsPage, map[string]int{
		okRkey:   http.StatusOK,
		failRkey: http.StatusInternalServerError,
	}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitPartialFailure, code)
	assert.Contains(t, stdout.String(), "1 failure(s)")
	assert.Contains(t, stdout.String(), failRkey)
	assertOnlySlackRequestURL(t, mock, "https://hooks.slack.com/services/failure")
}

// assertOnlySlackRequestURL asserts that exactly one request went to
// hooks.slack.com, and that its URL is wantURL -- proving the run actually
// routed to the expected channel (success vs. failure), not merely that
// some hooks.slack.com request was made.
func assertOnlySlackRequestURL(t *testing.T, mock *atprototestutil.MockHTTPDoer, wantURL string) {
	t.Helper()
	const slackBaseURL = "https://hooks.slack.com/"
	var slackURLs []string
	for _, req := range mock.Requests() {
		if strings.HasPrefix(req.URL, slackBaseURL) {
			slackURLs = append(slackURLs, req.URL)
		}
	}
	assert.Equal(t, []string{wantURL}, slackURLs)
}

func TestRun_ApplyLoginFailure_SendsFailureNotification(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusOK, hermeticHandler(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession") {
			return atprototestutil.JSONResponse(http.StatusUnauthorized, `{"error":"AuthenticationRequired"}`), nil
		}
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		return nil, nil
	}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Now(), mock, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	assertOnlySlackRequestURL(t, mock, "https://hooks.slack.com/services/failure")
}

func TestRun_Apply_SlackNotifyFails_ExitCodeUnaffected(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-06-01T00:00:00Z")
	// Use a non-429 4xx status so internal/retry does not retry it, keeping
	// this test fast despite using retry.RealClock (no fakeClock available
	// through cmd/main.go's wiring).
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusBadRequest, deleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusOK}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
}

func TestRun_Apply_SlackNotifyFails_StderrContainsMaskedFailureMessage(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusBadRequest, deleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusOK}))}

	var stdout, stderr bytes.Buffer
	run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.NotEmpty(t, stderr.String())
	assert.NotContains(t, stderr.String(), "hooks.slack.com/services/success")
}

func TestRun_Apply_NoWebhookConfigured_SkipsNotifyWithoutError(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", "")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", "")
	configPath := validConfigPath(t)

	const rkey = "post-no-webhook"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: deleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusOK})}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	assert.Empty(t, stderr.String())
}

// ── validateSchedule tests ──────────────────────────────────────────────────

func TestValidateSchedule_ValidCronExpression_ReturnsNil(t *testing.T) {
	err := validateSchedule("0 3 * * *")
	assert.NoError(t, err)
}

func TestValidateSchedule_ValidCronWithWildcard_ReturnsNil(t *testing.T) {
	err := validateSchedule("* * * * *")
	assert.NoError(t, err)
}

func TestValidateSchedule_ValidCronWithStep_ReturnsNil(t *testing.T) {
	err := validateSchedule("*/15 * * * *")
	assert.NoError(t, err)
}

func TestValidateSchedule_ValidCronWithRange_ReturnsNil(t *testing.T) {
	err := validateSchedule("0 9-17 * * *")
	assert.NoError(t, err)
}

func TestValidateSchedule_ValidCronWithList_ReturnsNil(t *testing.T) {
	err := validateSchedule("0 3 * * 1,3,5")
	assert.NoError(t, err)
}

func TestValidateSchedule_ValidCronWithRangeAndStep_ReturnsNil(t *testing.T) {
	err := validateSchedule("0 9-17/2 * * *")
	assert.NoError(t, err)
}

func TestValidateSchedule_ValidCronWithListContainingRange_ReturnsNil(t *testing.T) {
	err := validateSchedule("0 3 * * 1-5,6-7")
	assert.NoError(t, err)
}

func TestValidateSchedule_NewlineInValue_ReturnsError(t *testing.T) {
	err := validateSchedule("0 3 * * *\n0 4 * * *")
	assert.Error(t, err)
	var sve *ScheduleValidationError
	assert.ErrorAs(t, err, &sve)
	assert.Contains(t, err.Error(), "newline")
}

func TestValidateSchedule_CarriageReturnInValue_ReturnsError(t *testing.T) {
	err := validateSchedule("0 3 * * *\r")
	assert.Error(t, err)
	var sve *ScheduleValidationError
	assert.ErrorAs(t, err, &sve)
}

func TestValidateSchedule_TooFewFields_ReturnsError(t *testing.T) {
	err := validateSchedule("0 3 * *")
	assert.Error(t, err)
	var sve *ScheduleValidationError
	assert.ErrorAs(t, err, &sve)
	assert.Contains(t, err.Error(), "expected 5 cron fields")
}

func TestValidateSchedule_TooManyFields_ReturnsError(t *testing.T) {
	err := validateSchedule("0 3 * * * extra")
	assert.Error(t, err)
	var sve *ScheduleValidationError
	assert.ErrorAs(t, err, &sve)
	assert.Contains(t, err.Error(), "expected 5 cron fields")
}

func TestValidateSchedule_MinuteOutOfRange_ReturnsError(t *testing.T) {
	err := validateSchedule("60 3 * * *")
	assert.Error(t, err)
	var sve *ScheduleValidationError
	assert.ErrorAs(t, err, &sve)
	assert.Contains(t, err.Error(), "out of range")
}

func TestValidateSchedule_HourOutOfRange_ReturnsError(t *testing.T) {
	err := validateSchedule("0 24 * * *")
	assert.Error(t, err)
}

func TestValidateSchedule_DayOfMonthOutOfRange_ReturnsError(t *testing.T) {
	err := validateSchedule("0 3 0 * *")
	assert.Error(t, err)
}

func TestValidateSchedule_MonthOutOfRange_ReturnsError(t *testing.T) {
	err := validateSchedule("0 3 * 13 *")
	assert.Error(t, err)
}

func TestValidateSchedule_DayOfWeekOutOfRange_ReturnsError(t *testing.T) {
	err := validateSchedule("0 3 * * 8")
	assert.Error(t, err)
}

func TestValidateSchedule_EmptyString_ReturnsError(t *testing.T) {
	err := validateSchedule("")
	assert.Error(t, err)
	var sve *ScheduleValidationError
	assert.ErrorAs(t, err, &sve)
	assert.Contains(t, err.Error(), "expected 5 cron fields")
}

func TestValidateSchedule_AtDailyMacro_ReturnsError(t *testing.T) {
	// @daily is not a 5-field cron expression.
	err := validateSchedule("@daily")
	assert.Error(t, err)
	var sve *ScheduleValidationError
	assert.ErrorAs(t, err, &sve)
	assert.Contains(t, err.Error(), "expected 5 cron fields")
}

func TestValidateSchedule_StepWithInvalidBase_ReturnsError(t *testing.T) {
	err := validateSchedule("*/abc * * * *")
	assert.Error(t, err)
}

func TestValidateSchedule_RangeWithInvertedBounds_ReturnsError(t *testing.T) {
	err := validateSchedule("0 17-9 * * *")
	assert.Error(t, err)
}

// ── parsePrintScheduleFlags tests ───────────────────────────────────────────

func TestParsePrintScheduleFlags_ConfigLongFlag_Accepted(t *testing.T) {
	configPath, err := parsePrintScheduleFlags([]string{"--config", "path/to.toml"})
	require.NoError(t, err)
	assert.Equal(t, "path/to.toml", configPath)
}

func TestParsePrintScheduleFlags_ConfigShortFlag_Accepted(t *testing.T) {
	configPath, err := parsePrintScheduleFlags([]string{"-c", "path/to.toml"})
	require.NoError(t, err)
	assert.Equal(t, "path/to.toml", configPath)
}

func TestParsePrintScheduleFlags_MissingConfig_ReturnsError(t *testing.T) {
	_, err := parsePrintScheduleFlags([]string{})
	require.Error(t, err)
}

func TestParsePrintScheduleFlags_UnknownFlag_ReturnsError(t *testing.T) {
	_, err := parsePrintScheduleFlags([]string{"--unknown"})
	require.Error(t, err)
}

func TestParsePrintScheduleFlags_UnexpectedPositionalArgument_ReturnsError(t *testing.T) {
	_, err := parsePrintScheduleFlags([]string{"--config", "path/to.toml", "extra-arg"})
	require.Error(t, err)
}

// ── runPrintSchedule tests ──────────────────────────────────────────────────

func TestRunPrintSchedule_ValidConfig_ReturnsExitCode0AndPrintsSchedule(t *testing.T) {
	configPath := validConfigPath(t)
	var stdout, stderr bytes.Buffer

	code := runPrintSchedule(configPath, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	assert.Equal(t, "0 3 * * *\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestRunPrintSchedule_FileNotFound_ReturnsExitCode1(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := runPrintSchedule("/nonexistent/path.toml", &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, stderr.String())
}

func TestRunPrintSchedule_InvalidTOML_ReturnsExitCode1(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	require.NoError(t, os.WriteFile(path, []byte("invalid tomldata {{{"), 0o600))

	var stdout, stderr bytes.Buffer
	code := runPrintSchedule(path, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, stderr.String())
}

func TestRunPrintSchedule_MissingScheduleField_ReturnsExitCode1(t *testing.T) {
	// TOML missing schedule field -- config.Load succeeds (schedule is
	// optional for run/dry-run), but validateSchedule then rejects the
	// resulting empty string when the print-schedule subcommand is used.
	path := t.TempDir() + "/config.toml"
	body := "retention_days = 30\nexecution_timeout_seconds = 3600\nslack_allowed_host = \"hooks.slack.com\"\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	var stdout, stderr bytes.Buffer
	code := runPrintSchedule(path, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, stderr.String())
}

func TestRunPrintSchedule_InvalidScheduleValue_ReturnsExitCode1(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	body := "retention_days = 30\nschedule = \"0 3 * *\"\nexecution_timeout_seconds = 3600\nslack_allowed_host = \"hooks.slack.com\"\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	var stdout, stderr bytes.Buffer
	code := runPrintSchedule(path, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, stderr.String())
}

func TestRun_ApplyPartialFailure_ConsoleOutputSanitizesMaliciousRKey(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const okRkey = "old-post-ok"
	const failRkey = "evil\nFAKE LOG LINE"
	record := func(rkey string) string {
		// json.Marshal (not a raw %s substitution) so a literal newline in
		// rkey is properly escaped as \n within the JSON string, rather
		// than breaking the surrounding JSON syntax.
		uriJSON, err := json.Marshal(fmt.Sprintf("at://%s/app.bsky.feed.post/%s", testDID, rkey))
		require.NoError(t, err)
		return fmt.Sprintf(
			`{"uri":%s,"cid":"bafycid","value":{"$type":"app.bsky.feed.post","text":"post","createdAt":"2000-01-01T00:00:00Z"}}`,
			uriJSON,
		)
	}
	postsPage := atprototestutil.ListRecordsResponseJSON([]string{record(okRkey), record(failRkey)}, "")
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusOK, deleteRecordHandler(t, postsPage, map[string]int{
		okRkey:   http.StatusOK,
		failRkey: http.StatusInternalServerError,
	}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitPartialFailure, code)
	// report.FormatText sanitizes failRkey field-by-field, so the newline
	// smuggled inside it is stripped (no fabricated "FAKE LOG LINE" on its
	// own raw line) while the report's own structural newlines survive.
	assert.Contains(t, stdout.String(), "\n")
	assert.NotContains(t, stdout.String(), "evil\nFAKE LOG LINE")
	assert.Contains(t, stdout.String(), "evilFAKE LOG LINE")
	assert.Contains(t, stdout.String(), "1 failure(s)")
}
