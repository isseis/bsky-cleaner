package runner

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedNow = time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)

const fixedRetentionDays = 30

func testAppPassword(t *testing.T) config.SecretString {
	t.Helper()
	t.Setenv("BSKY_HANDLE", "alice.bsky.social")
	t.Setenv("BSKY_APP_PASSWORD", "app-password")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", "https://hooks.slack.com/services/success")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", "https://hooks.slack.com/services/failure")

	creds, err := config.LoadCredentials()
	require.NoError(t, err)
	return creds.AppPassword
}

func oldPost(rkey string) atproto.Post {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	return atproto.Post{RKey: rkey, Type: atproto.PostTypeOriginal, CreatedAt: threshold.AddDate(0, 0, -1)}
}

func recentPost(rkey string) atproto.Post {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	return atproto.Post{RKey: rkey, Type: atproto.PostTypeOriginal, CreatedAt: threshold.AddDate(0, 0, 1)}
}

func pinnedOldPost(rkey string) atproto.Post {
	post := oldPost(rkey)
	post.Pinned = true
	return post
}

func TestRun_DryRun_DoesNotCallDeleteRecord(t *testing.T) {
	client := &fakeClient{}
	client.ListPostsResult = []atproto.Post{oldPost("target")}

	result, err := Run(context.Background(), client, testAppPassword(t), fixedRetentionDays, false, fixedNow)

	require.NoError(t, err)
	assert.Empty(t, client.DeleteRecordCalls)
	assert.Equal(t, []atproto.Post{oldPost("target")}, result.Targets)
}

func TestRun_Apply_CallsDeleteRecordForAllTargets(t *testing.T) {
	client := &fakeClient{}
	client.ListPostsResult = []atproto.Post{oldPost("first"), oldPost("second")}

	_, err := Run(context.Background(), client, testAppPassword(t), fixedRetentionDays, true, fixedNow)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"first", "second"}, client.DeleteRecordCalls)
}

func TestRun_Apply_RespectsCleanupExclusions_PinnedAndRetention(t *testing.T) {
	client := &fakeClient{}
	client.ListPostsResult = []atproto.Post{
		pinnedOldPost("pinned"),
		recentPost("recent"),
		oldPost("eligible"),
	}

	_, err := Run(context.Background(), client, testAppPassword(t), fixedRetentionDays, true, fixedNow)

	require.NoError(t, err)
	assert.Equal(t, []string{"eligible"}, client.DeleteRecordCalls)
}

func TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure(t *testing.T) {
	client := &fakeClient{}
	client.ListPostsResult = []atproto.Post{oldPost("ok"), oldPost("bad")}
	failErr := errors.New("delete failed")
	client.DeleteRecordErrs = map[string]error{"bad": failErr}

	result, err := Run(context.Background(), client, testAppPassword(t), fixedRetentionDays, true, fixedNow)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ok", "bad"}, client.DeleteRecordCalls)
	assert.Equal(t, []atproto.Post{oldPost("ok")}, result.Deleted)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, oldPost("bad"), result.Failed[0].Post)
	assert.Equal(t, failErr, result.Failed[0].Err)
}

func TestRun_LoginError_ReturnsErrorWithoutListingOrDeleting(t *testing.T) {
	client := &fakeClient{}
	loginErr := errors.New("login failed")
	client.LoginErr = loginErr
	client.ListPostsResult = []atproto.Post{oldPost("target")}

	result, err := Run(context.Background(), client, testAppPassword(t), fixedRetentionDays, true, fixedNow)

	assert.ErrorIs(t, err, loginErr)
	assert.Nil(t, result)
	assert.Empty(t, client.DeleteRecordCalls)
}

func TestRun_ListPostsError_ReturnsErrorWithoutDeleting(t *testing.T) {
	client := &fakeClient{}
	listErr := errors.New("list failed")
	client.ListPostsErr = listErr

	result, err := Run(context.Background(), client, testAppPassword(t), fixedRetentionDays, true, fixedNow)

	assert.ErrorIs(t, err, listErr)
	assert.Nil(t, result)
	assert.Empty(t, client.DeleteRecordCalls)
}

func TestRun_LogsEachDeleteRecordOutcome(t *testing.T) {
	client := &fakeClient{}
	client.ListPostsResult = []atproto.Post{oldPost("ok"), oldPost("bad")}
	client.DeleteRecordErrs = map[string]error{"bad": errors.New("delete failed")}

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	_, err := Run(context.Background(), client, testAppPassword(t), fixedRetentionDays, true, fixedNow)

	require.NoError(t, err)
	logged := logBuf.String()
	assert.Contains(t, logged, "ok")
	assert.Contains(t, logged, "bad")
}
