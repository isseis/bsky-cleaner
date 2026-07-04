//go:build test

package runner

import (
	"context"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/config"
)

// fakeClient is a lightweight in-memory Client used by runner's own tests
// to observe which calls Run makes without any network I/O.
type fakeClient struct {
	LoginErr error

	ListPostsResult []atproto.Post
	ListPostsErr    error

	// DeleteRecordErrs maps rkey to the error DeleteRecord should return
	// for it. An rkey absent from the map succeeds.
	DeleteRecordErrs map[string]error

	// DeleteRecordCalls records the rkey of each DeleteRecord call, in
	// call order, so tests can assert both which rkeys were called and
	// how many times.
	DeleteRecordCalls []string
}

func (c *fakeClient) Login(_ context.Context, _ config.SecretString) error {
	return c.LoginErr
}

func (c *fakeClient) ListPosts(_ context.Context) ([]atproto.Post, error) {
	return c.ListPostsResult, c.ListPostsErr
}

func (c *fakeClient) DeleteRecord(_ context.Context, rkey string) error {
	c.DeleteRecordCalls = append(c.DeleteRecordCalls, rkey)
	return c.DeleteRecordErrs[rkey]
}
