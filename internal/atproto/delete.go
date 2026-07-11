package atproto

import (
	"context"
	"fmt"
	"net/http"
)

// deleteRecordRequest is the com.atproto.repo.deleteRecord request body.
// swapRecord/swapCommit (compare-and-swap) are not used: this package has
// no need to detect a concurrent modification between ListPosts and
// DeleteRecord, and omitting them keeps deleteRecord's only defined error
// (InvalidSwap) out of scope entirely.
type deleteRecordRequest struct {
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	RKey       string `json:"rkey"`
}

// DeleteRecord deletes post from the authenticated account's own
// repository, choosing the collection from post.Type (PostTypeRepost ->
// app.bsky.feed.repost; PostTypeOriginal/PostTypeReply/PostTypeQuote ->
// app.bsky.feed.post). Unrecognized PostType values return an error and
// send no request. repo is
// caller-supplied value, so a caller cannot direct a deletion at another
// account's repository (AC-13). Unlike ListPosts's read endpoints,
// deleteRecord requires auth per the AT Protocol lexicon, so the request
// carries c.session.AccessJWT as a Bearer Authorization header; if Login
// has not succeeded (c.session is nil), no request is sent at all (AC-05).
//
// Per the com.atproto.repo.deleteRecord lexicon ("Delete a repository
// record, or ensure it doesn't exist"), deleting an already-absent rkey
// still succeeds with a 200 response -- there is no separate error name
// for "not found" to special-case, unlike ListPosts's getRecord fallback.
// DeleteRecord therefore treats any 2xx response (via doXRPC's shared
// success check) as success and any other response as a genuine failure.
func (c *Client) DeleteRecord(ctx context.Context, post Post) error {
	if c.session == nil {
		return fmt.Errorf("delete record: %w", ErrAuthenticationFailed)
	}

	collection, err := collectionForPostType(post.Type)
	if err != nil {
		return fmt.Errorf("delete record: %w", err)
	}

	reqBody := deleteRecordRequest{
		Repo:       c.session.DID,
		Collection: collection,
		RKey:       post.RKey,
	}

	authHeader := "Bearer " + c.session.AccessJWT.Reveal()
	if err := doXRPC(ctx, c.httpDoer, c.pdsBaseURL, http.MethodPost, "com.atproto.repo.deleteRecord", nil, reqBody, nil, authHeader); err != nil {
		return fmt.Errorf("delete record: %w", err)
	}
	return nil
}
