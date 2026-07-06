package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// XRPC collections this package reads to build a post inventory.
const (
	collectionFeedPost = "app.bsky.feed.post"
	collectionRepost   = "app.bsky.feed.repost"
	collectionProfile  = "app.bsky.actor.profile"
)

// embed.$type values that mark a app.bsky.feed.post record as a quote post
// (3.2 architecture note): a plain record embed, or a record embed
// combined with media.
const (
	embedTypeRecord          = "app.bsky.embed.record"
	embedTypeRecordWithMedia = "app.bsky.embed.recordWithMedia"
)

// listRecordsPageLimit bounds how many records each listRecords page
// request returns, so pagination in listAllRecords exercises more than one
// request for any account with a realistic number of posts.
const listRecordsPageLimit = "100"

// maxListTotalBytes is the maximum total bytes of all record values
// accumulated during listAllRecords pagination. A legitimate account's
// posts should fit within a few hundred MB. 256 MiB provides ample margin
// while protecting against memory exhaustion from a malicious PDS.
const maxListTotalBytes = 256 << 20 // 256 MiB

// maxListPages is the maximum number of pages listAllRecords will fetch.
// A legitimate large account should not need more than 10,000 pages
// (1,000,000 posts at 100 per page). This protects against a malicious
// PDS returning empty pages with different cursors forever.
const maxListPages = 10000

// maxListRecords is the maximum total number of records listAllRecords
// will accumulate. A legitimate large account should not have more than
// 1,000,000 posts. This protects against a malicious PDS returning many
// tiny records to exhaust memory via slice/struct overhead.
const maxListRecords = 1_000_000

// PostType classifies an app.bsky.feed.post record, or marks a record
// retrieved from the separate app.bsky.feed.repost collection.
type PostType int

// PostType values, in the priority order classifyPostRecord checks them
// (a record matching more than one condition is classified by the
// earliest-listed applicable type).
const (
	PostTypeOriginal PostType = iota
	PostTypeReply
	PostTypeQuote
	PostTypeRepost
)

// Post is the subset of a post/repost record ListPosts exposes to callers:
// enough to decide whether and how to delete it, without the full record
// body (which downstream filtering does not need).
type Post struct {
	RKey      string
	Type      PostType
	CreatedAt time.Time
	Pinned    bool
}

// listRecord is one entry of a com.atproto.repo.listRecords response.
// Value is kept raw so callers can decode it into the record shape
// appropriate for the collection being listed.
type listRecord struct {
	URI   string          `json:"uri"`
	Value json.RawMessage `json:"value"`
}

// listRecordsResponse is the subset of the com.atproto.repo.listRecords
// response body this package needs.
type listRecordsResponse struct {
	Cursor  string       `json:"cursor,omitempty"`
	Records []listRecord `json:"records"`
}

// feedPostValue is the subset of an app.bsky.feed.post record this package
// needs to classify its PostType and CreatedAt (3.2 architecture note).
type feedPostValue struct {
	CreatedAt string          `json:"createdAt"`
	Reply     json.RawMessage `json:"reply,omitempty"`
	Embed     *embedValue     `json:"embed,omitempty"`
}

// embedValue is the subset of an embed this package needs: only its
// discriminator, to detect a quote post (app.bsky.embed.record /
// app.bsky.embed.recordWithMedia).
type embedValue struct {
	Type string `json:"$type"`
}

// repostValue is the subset of an app.bsky.feed.repost record this package
// needs.
type repostValue struct {
	CreatedAt string `json:"createdAt"`
}

// getRecordResponse is the subset of the com.atproto.repo.getRecord
// response body this package needs, specialized to the
// app.bsky.actor.profile record.
type getRecordResponse struct {
	Value profileRecordValue `json:"value"`
}

// profileRecordValue is the subset of an app.bsky.actor.profile record
// this package needs: the pinned post reference, if any.
type profileRecordValue struct {
	PinnedPost *strongRef `json:"pinnedPost,omitempty"`
}

// strongRef is an AT Protocol "strong reference" (an AT-URI plus its CID).
// Only URI is used here, to extract the pinned post's rkey.
type strongRef struct {
	URI string `json:"uri"`
}

// ListPosts fetches the account's full post inventory: every
// app.bsky.feed.post record (classified as original/reply/quote) and every
// app.bsky.feed.repost record (as PostTypeRepost), with pinned status
// resolved from the account's app.bsky.actor.profile record.
//
// listRecords and getRecord are public read endpoints per the AT Protocol
// lexicon (they carry no auth requirement, unlike createSession/
// deleteRecord), so ListPosts sends no Authorization header and does not
// require a prior Login -- unlike DeleteRecord, it has no c.session guard.
func (c *Client) ListPosts(ctx context.Context) ([]Post, error) {
	postRecords, err := c.listAllRecords(ctx, collectionFeedPost)
	if err != nil {
		return nil, err
	}
	repostRecords, err := c.listAllRecords(ctx, collectionRepost)
	if err != nil {
		return nil, err
	}

	posts := make([]Post, 0, len(postRecords)+len(repostRecords))
	for _, rec := range postRecords {
		post, err := classifyPostRecord(rec)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	for _, rec := range repostRecords {
		post, err := classifyRepostRecord(rec)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	pinnedRKey, err := c.pinnedPostRKey(ctx)
	if err != nil {
		return nil, err
	}
	if pinnedRKey != "" {
		for i := range posts {
			if posts[i].RKey == pinnedRKey {
				posts[i].Pinned = true
			}
		}
	}

	return posts, nil
}

// listAllRecords fetches every record in collection for the account,
// following the response cursor until it comes back empty. If the cursor
// fails to advance between two consecutive non-empty responses (a server
// protocol misbehavior distinct from a transport failure, 6.2 architecture
// note), it stops and returns ErrPaginationStalled rather than looping
// forever. It also enforces limits on total bytes, total pages, and total
// records to protect against a malicious PDS exhausting memory.
func (c *Client) listAllRecords(ctx context.Context, collection string) ([]listRecord, error) {
	return c.listAllRecordsWithLimits(ctx, collection, maxListTotalBytes, maxListPages, maxListRecords)
}

// listAllRecordsWithLimits is like listAllRecords but accepts custom limits.
// This is useful for tests that want to verify limit behavior with small
// thresholds instead of the production defaults.
func (c *Client) listAllRecordsWithLimits(ctx context.Context, collection string, maxBytes, maxPages, maxRecords int) ([]listRecord, error) {
	var all []listRecord
	cursor := ""
	var totalPages, totalRecords int
	var totalBytes int
	for {
		query := url.Values{}
		query.Set("repo", c.did)
		query.Set("collection", collection)
		query.Set("limit", listRecordsPageLimit)
		if cursor != "" {
			query.Set("cursor", cursor)
		}

		var resp listRecordsResponse
		if err := doXRPC(ctx, c.httpDoer, c.pdsBaseURL, http.MethodGet, "com.atproto.repo.listRecords", query, nil, &resp, ""); err != nil {
			return nil, err
		}

		// Compute this page's contribution before appending
		pageRecords := len(resp.Records)
		var pageBytes int
		for _, rec := range resp.Records {
			pageBytes += len(rec.Value)
		}

		// Check limits BEFORE appending to prevent memory exhaustion from a malicious PDS
		if totalBytes+pageBytes > maxBytes {
			return nil, fmt.Errorf("list posts: list %s: %w", collection, ErrPaginationLimitExceeded)
		}
		if totalPages+1 > maxPages {
			return nil, fmt.Errorf("list posts: list %s: %w", collection, ErrPaginationLimitExceeded)
		}
		if totalRecords+pageRecords > maxRecords {
			return nil, fmt.Errorf("list posts: list %s: %w", collection, ErrPaginationLimitExceeded)
		}

		// Safe to append now that limits are verified
		all = append(all, resp.Records...)

		// Accumulate totals
		totalPages++
		totalRecords += pageRecords
		totalBytes += pageBytes

		if resp.Cursor == "" {
			return all, nil
		}
		if resp.Cursor == cursor {
			return nil, fmt.Errorf("list posts: list %s: %w", collection, ErrPaginationStalled)
		}
		cursor = resp.Cursor
	}
}

// pinnedPostRKeyNotFoundStatus is the HTTP status the AT Protocol
// com.atproto.repo.getRecord endpoint returns when the requested record
// does not exist (e.g. an account with no customized profile record),
// carrying an InvalidRequest/RecordNotFound error name in its body. Only
// this specific status is treated as "no pinned post"; any other error
// status (5xx, rate limiting, a decode failure on a 2xx body, etc.) is a
// genuine failure and must propagate rather than being silently reported
// as "not pinned" (pinned-post exclusion is a safety guarantee downstream
// cleanup logic relies on -- see docs/overview.md's pinned-post exclusion
// rule).
const pinnedPostRKeyNotFoundStatus = http.StatusBadRequest

// pinnedPostRKeyNotFoundErrorName is the ATProto XRPC error name the
// com.atproto.repo.getRecord endpoint returns, alongside
// pinnedPostRKeyNotFoundStatus, when the requested record does not exist.
// A 400 response is only treated as "no pinned post" when it carries this
// exact error name; any other 400 (including one with a missing or
// unparseable error name) is a genuine failure and must propagate, per
// this codebase's fail-closed principle.
const pinnedPostRKeyNotFoundErrorName = "RecordNotFound"

// pinnedPostRKey returns the rkey of the account's pinned post, or "" if
// none is pinned or the profile record does not exist (3.2 architecture
// note 3). See pinnedPostRKeyNotFoundStatus/pinnedPostRKeyNotFoundErrorName
// for which failures are treated as "no pinned post" versus propagated as
// errors.
func (c *Client) pinnedPostRKey(ctx context.Context) (string, error) {
	query := url.Values{}
	query.Set("repo", c.did)
	query.Set("collection", collectionProfile)
	query.Set("rkey", "self")

	var resp getRecordResponse
	err := doXRPC(ctx, c.httpDoer, c.pdsBaseURL, http.MethodGet, "com.atproto.repo.getRecord", query, nil, &resp, "")
	if err != nil {
		if httpErr, ok := errors.AsType[*HTTPError](err); ok &&
			httpErr.StatusCode == pinnedPostRKeyNotFoundStatus &&
			httpErr.ErrorName == pinnedPostRKeyNotFoundErrorName {
			return "", nil
		}
		return "", err
	}

	if resp.Value.PinnedPost == nil {
		return "", nil
	}
	return rkeyFromURI(resp.Value.PinnedPost.URI)
}

// classifyPostRecord decodes an app.bsky.feed.post record and classifies
// it: a reply field marks PostTypeReply (checked first, since a reply can
// also carry a quote embed -- 3.2 architecture note says reply takes
// priority), an app.bsky.embed.record(WithMedia) embed marks
// PostTypeQuote, otherwise PostTypeOriginal.
func classifyPostRecord(rec listRecord) (Post, error) {
	rkey, err := rkeyFromURI(rec.URI)
	if err != nil {
		return Post{}, err
	}

	var value feedPostValue
	if err := json.Unmarshal(rec.Value, &value); err != nil {
		return Post{}, fmt.Errorf("list posts: decode %s: %w", rec.URI, err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, value.CreatedAt)
	if err != nil {
		return Post{}, fmt.Errorf("list posts: parse createdAt for %s: %w", rec.URI, err)
	}

	// A present-but-null "reply" field (some PDS implementations may emit
	// this instead of omitting the key) must not count as a reply -- raw
	// byte length alone can't distinguish "absent" from "null".
	hasReply := len(value.Reply) > 0 && strings.TrimSpace(string(value.Reply)) != "null"

	postType := PostTypeOriginal
	switch {
	case hasReply:
		postType = PostTypeReply
	case value.Embed != nil && (value.Embed.Type == embedTypeRecord || value.Embed.Type == embedTypeRecordWithMedia):
		postType = PostTypeQuote
	}

	return Post{RKey: rkey, Type: postType, CreatedAt: createdAt}, nil
}

// classifyRepostRecord decodes an app.bsky.feed.repost record. Every
// record from this collection is a repost regardless of its content.
func classifyRepostRecord(rec listRecord) (Post, error) {
	rkey, err := rkeyFromURI(rec.URI)
	if err != nil {
		return Post{}, err
	}

	var value repostValue
	if err := json.Unmarshal(rec.Value, &value); err != nil {
		return Post{}, fmt.Errorf("list posts: decode %s: %w", rec.URI, err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, value.CreatedAt)
	if err != nil {
		return Post{}, fmt.Errorf("list posts: parse createdAt for %s: %w", rec.URI, err)
	}

	return Post{RKey: rkey, Type: PostTypeRepost, CreatedAt: createdAt}, nil
}

// rkeyFromURI extracts the final "/"-separated segment of an AT-URI
// (at://did/collection/rkey), which is the record key.
func rkeyFromURI(uri string) (string, error) {
	idx := strings.LastIndex(uri, "/")
	if idx < 0 || idx == len(uri)-1 {
		return "", fmt.Errorf("list posts: malformed record URI %q", uri)
	}
	return uri[idx+1:], nil
}
