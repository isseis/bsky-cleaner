package main

import (
	"bytes"
	"encoding/json"
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
	const body = "retention_days = 30\nschedule = \"0 3 * * *\"\nexecution_timeout_seconds = 3600\n"
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
// extracted from the request path) so apply-mode tests can script
// per-post success/failure.
func deleteRecordHandler(t *testing.T, postsPage string, deleteStatus map[string]int) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	base := listRecordsHandler(t, postsPage)
	return hermeticHandler(t, func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "com.atproto.repo.deleteRecord") {
			return base(req)
		}
		body, err := readDeleteRecordRKey(req)
		require.NoError(t, err)
		status, ok := deleteStatus[body]
		require.True(t, ok, "unexpected deleteRecord rkey: %s", body)
		if status >= 200 && status < 300 {
			return atprototestutil.JSONResponse(status, atprototestutil.DeleteRecordResponseJSON()), nil
		}
		return atprototestutil.JSONResponse(status, `{"error":"InvalidRequest"}`), nil
	})
}

func TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setEnvCredentials(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: deleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusOK})}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout.String(), "Deleted 1 post(s), 0 failure(s)")
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
	mock := &atprototestutil.MockHTTPDoer{Handler: deleteRecordHandler(t, postsPage, map[string]int{
		okRkey:   http.StatusOK,
		failRkey: http.StatusInternalServerError,
	})}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitPartialFailure, code)
	assert.Contains(t, stdout.String(), "1 failure(s)")
	assert.Contains(t, stdout.String(), failRkey)
}
