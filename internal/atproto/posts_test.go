package atproto

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	atprototestutil "github.com/isseis/bsky-cleaner/internal/atproto/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDID is the DID bound to every *Client built by newPostsTestClient
// below, so mock handlers can assert the "repo" query parameter always
// equals this value.
const testDID = "did:plc:test123"

// newPostsTestClient builds a *Client (via the Phase 3 newTestClient
// factory) wired to a mock whose responses are driven by handler.
// ListPosts requires no session (see the rationale comment on ListPosts in
// posts.go), so session is always nil here.
func newPostsTestClient(handler func(req *http.Request) (*http.Response, error)) (*Client, *atprototestutil.MockHTTPDoer) {
	mock := &atprototestutil.MockHTTPDoer{Handler: handler}
	client := newTestClient(mock, &url.URL{Scheme: "https", Host: "pds.test"}, "alice.test", testDID, nil)
	return client, mock
}

// newListPostsHandler dispatches listRecords(app.bsky.feed.post),
// listRecords(app.bsky.feed.repost), and getRecord(app.bsky.actor.profile)
// requests to canned page/response bodies, returning them in order per
// collection so a test can script multi-page pagination.
func newListPostsHandler(t *testing.T, postPages, repostPages []string, profileStatus int, profileBody string) func(req *http.Request) (*http.Response, error) {
	t.Helper()
	postIdx, repostIdx := 0, 0
	return func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		assert.Equal(t, testDID, q.Get("repo"))
		switch {
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && q.Get("collection") == collectionFeedPost:
			if postIdx >= len(postPages) {
				t.Fatalf("unexpected extra listRecords(%s) call", collectionFeedPost)
			}
			body := postPages[postIdx]
			postIdx++
			return atprototestutil.JSONResponse(http.StatusOK, body), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.listRecords") && q.Get("collection") == collectionRepost:
			if repostIdx >= len(repostPages) {
				t.Fatalf("unexpected extra listRecords(%s) call", collectionRepost)
			}
			body := repostPages[repostIdx]
			repostIdx++
			return atprototestutil.JSONResponse(http.StatusOK, body), nil
		case strings.HasSuffix(req.URL.Path, "com.atproto.repo.getRecord"):
			assert.Equal(t, collectionProfile, q.Get("collection"))
			assert.Equal(t, "self", q.Get("rkey"))
			return atprototestutil.JSONResponse(profileStatus, profileBody), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	}
}

// buildListRecordsBody assembles a com.atproto.repo.listRecords response
// body from pre-built record JSON snippets (see postRecordJSON/
// repostRecordJSON) and an optional cursor.
func buildListRecordsBody(records []string, cursor string) string {
	body := `{"records":[` + strings.Join(records, ",") + `]`
	if cursor != "" {
		body += `,"cursor":"` + cursor + `"`
	}
	body += `}`
	return body
}

func postRecordJSON(rkey, valueJSON string) string {
	return fmt.Sprintf(`{"uri":"at://%s/%s/%s","cid":"bafycid","value":%s}`, testDID, collectionFeedPost, rkey, valueJSON)
}

func repostRecordJSON(rkey, createdAt string) string {
	return fmt.Sprintf(`{"uri":"at://%s/%s/%s","cid":"bafycid","value":{"$type":"app.bsky.feed.repost","createdAt":%q,"subject":{"uri":"at://did:plc:other/app.bsky.feed.post/x","cid":"bafyother"}}}`, testDID, collectionRepost, rkey, createdAt)
}

func TestClient_ListPosts_ClassifiesPostTypes(t *testing.T) {
	const createdAt = "2024-01-01T00:00:00Z"
	tests := []struct {
		name      string
		valueJSON string
		wantType  PostType
	}{
		{
			name:      "original post: no reply, no embed",
			valueJSON: fmt.Sprintf(`{"$type":"app.bsky.feed.post","text":"hello","createdAt":%q}`, createdAt),
			wantType:  PostTypeOriginal,
		},
		{
			name:      "reply: reply field present",
			valueJSON: fmt.Sprintf(`{"$type":"app.bsky.feed.post","text":"hi","createdAt":%q,"reply":{"parent":{"uri":"at://did:plc:other/app.bsky.feed.post/p","cid":"c"},"root":{"uri":"at://did:plc:other/app.bsky.feed.post/r","cid":"c"}}}`, createdAt),
			wantType:  PostTypeReply,
		},
		{
			name:      "quote: embed.$type is app.bsky.embed.record",
			valueJSON: fmt.Sprintf(`{"$type":"app.bsky.feed.post","text":"look","createdAt":%q,"embed":{"$type":"app.bsky.embed.record","record":{"uri":"at://did:plc:other/app.bsky.feed.post/q","cid":"c"}}}`, createdAt),
			wantType:  PostTypeQuote,
		},
		{
			name:      "quote with media: embed.$type is app.bsky.embed.recordWithMedia",
			valueJSON: fmt.Sprintf(`{"$type":"app.bsky.feed.post","text":"look","createdAt":%q,"embed":{"$type":"app.bsky.embed.recordWithMedia"}}`, createdAt),
			wantType:  PostTypeQuote,
		},
		{
			name:      "reply takes priority over quote embed",
			valueJSON: fmt.Sprintf(`{"$type":"app.bsky.feed.post","text":"hi","createdAt":%q,"reply":{"parent":{"uri":"at://did:plc:other/app.bsky.feed.post/p","cid":"c"},"root":{"uri":"at://did:plc:other/app.bsky.feed.post/r","cid":"c"}},"embed":{"$type":"app.bsky.embed.record","record":{"uri":"at://did:plc:other/app.bsky.feed.post/q","cid":"c"}}}`, createdAt),
			wantType:  PostTypeReply,
		},
		{
			name:      "original post: reply field explicitly null is not a reply",
			valueJSON: fmt.Sprintf(`{"$type":"app.bsky.feed.post","text":"hello","createdAt":%q,"reply":null}`, createdAt),
			wantType:  PostTypeOriginal,
		},
	}

	var records []string
	rkeys := make([]string, len(tests))
	for i, tt := range tests {
		rkey := fmt.Sprintf("rkey%d", i)
		rkeys[i] = rkey
		records = append(records, postRecordJSON(rkey, tt.valueJSON))
	}
	postPage := buildListRecordsBody(records, "")
	repostPage := buildListRecordsBody(nil, "")

	handler := newListPostsHandler(t, []string{postPage}, []string{repostPage}, http.StatusBadRequest, `{"error":"RecordNotFound"}`)
	client, _ := newPostsTestClient(handler)

	posts, err := client.ListPosts(context.Background())

	require.NoError(t, err)
	require.Len(t, posts, len(tests))
	byRKey := make(map[string]Post, len(posts))
	for _, p := range posts {
		byRKey[p.RKey] = p
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post, ok := byRKey[rkeys[i]]
			require.True(t, ok, "post %s not found in result", rkeys[i])
			assert.Equal(t, tt.wantType, post.Type)
			assert.True(t, post.CreatedAt.Equal(mustParseTime(t, createdAt)))
			assert.False(t, post.Pinned)
		})
	}
}

func TestClient_ListPosts_IncludesReposts(t *testing.T) {
	const createdAt = "2024-02-02T00:00:00Z"
	postPage := buildListRecordsBody(nil, "")
	repostPage := buildListRecordsBody([]string{repostRecordJSON("rp1", createdAt)}, "")

	handler := newListPostsHandler(t, []string{postPage}, []string{repostPage}, http.StatusBadRequest, `{"error":"RecordNotFound"}`)
	client, _ := newPostsTestClient(handler)

	posts, err := client.ListPosts(context.Background())

	require.NoError(t, err)
	require.Len(t, posts, 1)
	assert.Equal(t, "rp1", posts[0].RKey)
	assert.Equal(t, PostTypeRepost, posts[0].Type)
	assert.True(t, posts[0].CreatedAt.Equal(mustParseTime(t, createdAt)))
}

func TestClient_ListPosts_Pagination(t *testing.T) {
	const createdAt = "2024-03-03T00:00:00Z"
	postPage1 := buildListRecordsBody([]string{postRecordJSON("p1", fmt.Sprintf(`{"$type":"app.bsky.feed.post","createdAt":%q}`, createdAt))}, "post-cursor-1")
	postPage2 := buildListRecordsBody([]string{postRecordJSON("p2", fmt.Sprintf(`{"$type":"app.bsky.feed.post","createdAt":%q}`, createdAt))}, "")
	repostPage1 := buildListRecordsBody([]string{repostRecordJSON("r1", createdAt)}, "repost-cursor-1")
	repostPage2 := buildListRecordsBody([]string{repostRecordJSON("r2", createdAt)}, "")

	handler := newListPostsHandler(t, []string{postPage1, postPage2}, []string{repostPage1, repostPage2}, http.StatusBadRequest, `{"error":"RecordNotFound"}`)
	client, mock := newPostsTestClient(handler)

	posts, err := client.ListPosts(context.Background())

	require.NoError(t, err)
	rkeys := make([]string, len(posts))
	for i, p := range posts {
		rkeys[i] = p.RKey
	}
	assert.ElementsMatch(t, []string{"p1", "p2", "r1", "r2"}, rkeys)

	requests := mock.Requests()
	require.Len(t, requests, 5) // 2 post pages + 2 repost pages + 1 profile lookup
	assert.Equal(t, "post-cursor-1", mustParseQuery(t, requests[1].URL).Get("cursor"))
	assert.Equal(t, "repost-cursor-1", mustParseQuery(t, requests[3].URL).Get("cursor"))
}

func TestClient_ListPosts_PaginationStalled(t *testing.T) {
	stalledPage := buildListRecordsBody(nil, "stuck-cursor")
	handler := newListPostsHandler(t, []string{stalledPage, stalledPage}, nil, http.StatusBadRequest, `{"error":"RecordNotFound"}`)
	client, mock := newPostsTestClient(handler)

	posts, err := client.ListPosts(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaginationStalled)
	assert.Nil(t, posts)
	assert.Equal(t, 2, mock.CallCount(), "expected exactly the two non-advancing listRecords calls, no repost/profile call afterward")
}

func TestClient_ListPosts_PinnedDetection(t *testing.T) {
	const createdAt = "2024-04-04T00:00:00Z"
	postPage := buildListRecordsBody([]string{
		postRecordJSON("pinned-post", fmt.Sprintf(`{"$type":"app.bsky.feed.post","createdAt":%q}`, createdAt)),
		postRecordJSON("other-post", fmt.Sprintf(`{"$type":"app.bsky.feed.post","createdAt":%q}`, createdAt)),
	}, "")
	repostPage := buildListRecordsBody(nil, "")
	profileBody := fmt.Sprintf(`{"uri":"at://%s/%s/self","cid":"bafyprofile","value":{"$type":"app.bsky.actor.profile","pinnedPost":{"uri":"at://%s/%s/pinned-post","cid":"bafypinned"}}}`, testDID, collectionProfile, testDID, collectionFeedPost)

	t.Run("pinned post marked", func(t *testing.T) {
		handler := newListPostsHandler(t, []string{postPage}, []string{repostPage}, http.StatusOK, profileBody)
		client, _ := newPostsTestClient(handler)

		posts, err := client.ListPosts(context.Background())

		require.NoError(t, err)
		byRKey := make(map[string]Post, len(posts))
		for _, p := range posts {
			byRKey[p.RKey] = p
		}
		assert.True(t, byRKey["pinned-post"].Pinned)
		assert.False(t, byRKey["other-post"].Pinned)
	})

	t.Run("profile record does not exist: no pinned post, no error", func(t *testing.T) {
		handler := newListPostsHandler(t, []string{postPage}, []string{repostPage}, http.StatusBadRequest, `{"error":"RecordNotFound"}`)
		client, _ := newPostsTestClient(handler)

		posts, err := client.ListPosts(context.Background())

		require.NoError(t, err)
		for _, p := range posts {
			assert.False(t, p.Pinned)
		}
	})

	t.Run("profile fetch fails with a genuine server error: propagated, not swallowed as no pin", func(t *testing.T) {
		handler := newListPostsHandler(t, []string{postPage}, []string{repostPage}, http.StatusInternalServerError, `{"error":"InternalServerError"}`)
		client, _ := newPostsTestClient(handler)

		posts, err := client.ListPosts(context.Background())

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrHTTPStatus)
		assert.Nil(t, posts)
	})
}

func TestClient_ListPosts_EmptyResult(t *testing.T) {
	postPage := buildListRecordsBody(nil, "")
	repostPage := buildListRecordsBody(nil, "")

	handler := newListPostsHandler(t, []string{postPage}, []string{repostPage}, http.StatusBadRequest, `{"error":"RecordNotFound"}`)
	client, _ := newPostsTestClient(handler)

	posts, err := client.ListPosts(context.Background())

	require.NoError(t, err)
	assert.Empty(t, posts)
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}

func mustParseQuery(t *testing.T, rawURL string) url.Values {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u.Query()
}
