package atproto_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/isseis/bsky-cleaner/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publicIPLiteral is a documented, non-routable-for-real-traffic address
// (RFC 5737 TEST-NET-3) that is nonetheless not private/loopback/link-
// local/unspecified, so atproto's SSRF checks accept it. Using an IP
// literal as the handle/DID-document host lets net.DefaultResolver
// resolve it locally without any real DNS query (see
// internal/atproto/did_test.go's publicIPLiteral for the same rationale).
const publicIPLiteral = "203.0.113.5"

func integrationAppPassword(t *testing.T) config.SecretString {
	t.Helper()
	t.Setenv("BSKY_HANDLE", publicIPLiteral)
	t.Setenv("BSKY_APP_PASSWORD", "app-password")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", "https://hooks.slack.com/services/success")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", "https://hooks.slack.com/services/failure")

	creds, err := config.LoadCredentials()
	require.NoError(t, err)
	return creds.AppPassword
}

// TestRunnerRun_WithRealAtprotoClient verifies that *atproto.Client
// satisfies runner.Client and that runner.Run can drive it end to end
// (DID resolution -> login -> list -> delete), catching interface shape
// mismatches between the two packages that unit tests using a fake Client
// cannot. Judgment-logic edge cases (pinned posts, partial failure, etc.)
// are covered by internal/runner's own tests, not repeated here.
func TestRunnerRun_WithRealAtprotoClient(t *testing.T) {
	atproto.StubPassthroughPDSDoer(t)
	atproto.StubDNSTXTLookup(t)

	const handle = publicIPLiteral
	const did = "did:web:" + publicIPLiteral
	const rkey = "old-post"

	postRecord := fmt.Sprintf(
		`{"uri":"at://%s/app.bsky.feed.post/%s","cid":"bafycid","value":{"$type":"app.bsky.feed.post","text":"old","createdAt":%q}}`,
		did, rkey, "2000-01-01T00:00:00Z",
	)
	postPage := atprototestutil.ListRecordsResponseJSON([]string{postRecord}, "")
	repostPage := atprototestutil.ListRecordsResponseJSON(nil, "")

	mock := &atprototestutil.MockHTTPDoer{Handler: func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == handle && req.URL.Path == "/.well-known/atproto-did":
			return atprototestutil.JSONResponse(http.StatusOK, did), nil
		case req.URL.Host == handle && req.URL.Path == "/.well-known/did.json":
			body := fmt.Sprintf(`{"id":%q,"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://%s"}]}`, did, publicIPLiteral)
			return atprototestutil.JSONResponse(http.StatusOK, body), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.server.createSession"):
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.CreateSessionResponseJSON(did, "access-jwt")), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.post":
			return atprototestutil.JSONResponse(http.StatusOK, postPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && req.URL.Query().Get("collection") == "app.bsky.feed.repost":
			return atprototestutil.JSONResponse(http.StatusOK, repostPage), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.getRecord"):
			return atprototestutil.JSONResponse(http.StatusBadRequest, `{"error":"RecordNotFound"}`), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.deleteRecord"):
			return atprototestutil.JSONResponse(http.StatusOK, atprototestutil.DeleteRecordResponseJSON()), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}}

	client, err := atproto.NewClient(context.Background(), handle, mock)
	require.NoError(t, err)

	appPassword := integrationAppPassword(t)
	now := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)

	var runnerClient runner.Client = client
	result, err := runner.Run(context.Background(), runnerClient, appPassword, 30, true, now)

	require.NoError(t, err)
	assert.Empty(t, result.Failed)
	require.Len(t, result.Deleted, 1)
	assert.Equal(t, rkey, result.Deleted[0].RKey)
}
