//go:build test

// Package main holds the CLI-level integration tests for secret non-leakage
// (happy path and error paths). Every test uses high-distinction secret literals distinct
// from setEnvCredentials — which uses weak values ("app-password",
// "access-jwt") that would produce false-negative passes.
package main

import (
	"bytes"
	"fmt"
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

// High-distinction secret literals. These values are deliberately long and
// structurally distinct from any expected output, so a partial-string match
// on any output surface is a genuine leak indication.
const (
	secretAppPassword       = "SECRET-APP-PW-xyz789"
	secretAccessJWT         = "SECRET-ACCESS-JWT-abc123"
	secretWebhookURLSuccess = "https://hooks.slack.com/services/T00/B00/successSecret"
	secretWebhookURLFailure = "https://hooks.slack.com/services/T00/B00/failureSecret"
)

// setupSecretLeakEnv injects the high-distinction secret literals into the
// environment. Every test in this file MUST call this helper and MUST NOT
// use setEnvCredentials (whose values are too short/weak to guarantee a
// false-negative-free leak search).
func setupSecretLeakEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BSKY_HANDLE", publicIPLiteral)
	t.Setenv("BSKY_APP_PASSWORD", secretAppPassword)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", secretWebhookURLSuccess)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", secretWebhookURLFailure)
}

// forbiddenSecrets returns the set of values that must never appear in
// stdout, stderr, or Slack notification payloads. The Bearer-prefixed
// variant is included because Authorization headers carry it in full.
func forbiddenSecrets() []string {
	return []string{
		secretAppPassword,
		secretAccessJWT,
		"Bearer " + secretAccessJWT,
		secretWebhookURLSuccess,
		secretWebhookURLFailure,
	}
}

// assertNoSecrets verifies that none of the forbidden secrets appear as
// substrings in stdout or stderr, and optionally in Slack request bodies.
// slackBodies may be empty (dry-run mode sends no Slack requests).
func assertNoSecrets(t *testing.T, stdout, stderr string, slackBodies ...string) {
	t.Helper()
	secrets := forbiddenSecrets()
	check := func(name, content string) {
		for i, s := range secrets {
			if strings.Contains(content, s) {
				t.Errorf("forbidden secret at index %d found in %s", i, name)
			}
		}
	}
	check("stdout", stdout)
	check("stderr", stderr)
	for i, body := range slackBodies {
		check(fmt.Sprintf("Slack payload %d", i), body)
	}
}

// slackRequestBodies returns the request bodies sent to hooks.slack.com.
func slackRequestBodies(t *testing.T, mock *atprototestutil.MockHTTPDoer) []string {
	t.Helper()
	var bodies []string
	for _, req := range mock.Requests() {
		if strings.HasPrefix(req.URL, "https://hooks.slack.com/") {
			bodies = append(bodies, string(req.Body))
		}
	}
	return bodies
}

// ---------- Phase-3-specific handler factories ----------

// secretLeakListRecordsHandler handles listRecords and createSession
// similarly to listRecordsHandler in main_test.go, but returns
// secretAccessJWT from createSession so the leak search has a
// high-distinction value to detect.
func secretLeakListRecordsHandler(t *testing.T, postsPage string) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession"):
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.CreateSessionResponseJSON(testDID, secretAccessJWT)), nil
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

// secretLeakDeleteRecordHandler wraps secretLeakListRecordsHandler via
// hermeticHandler and additionally answers deleteRecord requests per
// deleteStatus, so apply-mode tests can script per-post success/failure.
func secretLeakDeleteRecordHandler(t *testing.T, postsPage string, deleteStatus map[string]int) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	base := secretLeakListRecordsHandler(t, postsPage)
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

// ---------- Happy path (dry-run and --apply) ----------

func TestRun_SecretNonLeak_HappyPath_DryRun(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setupSecretLeakEnv(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: hermeticHandler(t, secretLeakListRecordsHandler(t, postsPage))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	assertNoSecrets(t, stdout.String(), stderr.String())
}

func TestRun_SecretNonLeak_HappyPath_Apply(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setupSecretLeakEnv(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusOK, secretLeakDeleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusOK}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	slackBodies := slackRequestBodies(t, mock)
	assert.NotEmpty(t, slackBodies, "Slack notification should have been sent in apply mode")
	assertNoSecrets(t, stdout.String(), stderr.String(), slackBodies...)
}

// ---------- Error paths ----------

// Auth failure — createSession returns non-2xx.
func TestRun_SecretNonLeak_AuthFailure(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setupSecretLeakEnv(t)
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
	slackBodies := slackRequestBodies(t, mock)
	assertNoSecrets(t, stdout.String(), stderr.String(), slackBodies...)
}

// Network error — the HTTPDoer returns a connection-level error.
// This path fails during DID resolution, before createSession produces any
// AccessJWT, but the validation should still catch app password / webhook
// URL leaks in the stderr output.
func TestRun_SecretNonLeak_NetworkError(t *testing.T) {
	setupSecretLeakEnv(t)
	configPath := validConfigPath(t)

	mock := &atprototestutil.MockHTTPDoer{Handler: func(_ *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("simulated network error")
	}}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Now(), mock, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	assertNoSecrets(t, stdout.String(), stderr.String())
}

// DID resolution error — the well-known endpoints return non-2xx.
// This path fails before createSession: AccessJWT / Bearer assertions are
// vacuous, but the test still validates app password and webhook URLs do not
// leak in stderr.
func TestRun_SecretNonLeak_DIDResolutionError(t *testing.T) {
	setupSecretLeakEnv(t)
	configPath := validConfigPath(t)

	mock := &atprototestutil.MockHTTPDoer{Handler: func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == publicIPLiteral && (req.URL.Path == "/.well-known/atproto-did" || req.URL.Path == "/.well-known/did.json") {
			return atprototestutil.JSONResponse(http.StatusNotFound, `{"error":"NotFound"}`), nil
		}
		t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		return nil, nil
	}}

	var stdout, stderr bytes.Buffer
	code := run(configPath, false, time.Now(), mock, &stdout, &stderr)

	assert.Equal(t, exitSetupOrRunFail, code)
	assertNoSecrets(t, stdout.String(), stderr.String())
}

// Delete failure — deleteRecord returns non-2xx.
func TestRun_SecretNonLeak_DeleteFailure(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setupSecretLeakEnv(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusOK, secretLeakDeleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusInternalServerError}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitPartialFailure, code)
	slackBodies := slackRequestBodies(t, mock)
	assertNoSecrets(t, stdout.String(), stderr.String(), slackBodies...)
}

// Slack send failure — hooks.slack.com returns non-2xx.
// Uses a non-429 4xx so internal/retry does not retry, keeping this test fast.
func TestRun_SecretNonLeak_SlackSendFailure(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	setupSecretLeakEnv(t)
	configPath := validConfigPath(t)

	const rkey = "old-post"
	postsPage := postPageResponse(rkey, "2000-01-01T00:00:00Z")
	mock := &atprototestutil.MockHTTPDoer{Handler: slackWebhookHandler(http.StatusBadRequest, secretLeakDeleteRecordHandler(t, postsPage, map[string]int{rkey: http.StatusOK}))}

	var stdout, stderr bytes.Buffer
	code := run(configPath, true, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), mock, &stdout, &stderr)

	assert.Equal(t, exitOK, code)
	slackBodies := slackRequestBodies(t, mock)
	assertNoSecrets(t, stdout.String(), stderr.String(), slackBodies...)
}

// Execution timeout — the configured timeout fires during DID
// resolution. This path fails before createSession: AccessJWT / Bearer
// assertions are vacuous; the test validates app password and webhook URLs.
func TestRun_SecretNonLeak_ExecutionTimeout(t *testing.T) {
	setupSecretLeakEnv(t)
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
	assertNoSecrets(t, stdout.String(), stderr.String())
}
