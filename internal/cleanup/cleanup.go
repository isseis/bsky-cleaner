// Package cleanup selects the subset of an account's post inventory that
// is eligible for deletion under the retention policy. It has no
// dependency on the AT Protocol client itself -- only on the atproto.Post/
// atproto.PostType types -- so it can be tested without mocks.
package cleanup

import (
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
)

// SelectDeletionTargets returns the subset of posts eligible for deletion
// under the retention policy: posts whose CreatedAt is strictly older than
// (now - retentionDays), excluding pinned posts and any record whose Type
// does not match one of the known atproto.PostType values. retentionDays
// is assumed positive (validated by internal/config); now is the caller-
// supplied judgment time so callers can obtain deterministic results
// without depending on time.Now().
func SelectDeletionTargets(posts []atproto.Post, retentionDays int, now time.Time) []atproto.Post {
	threshold := now.UTC().AddDate(0, 0, -retentionDays)

	targets := make([]atproto.Post, 0, len(posts))
	for _, post := range posts {
		if post.Pinned {
			continue
		}

		switch post.Type {
		case atproto.PostTypeOriginal, atproto.PostTypeReply, atproto.PostTypeQuote, atproto.PostTypeRepost:
		default:
			continue
		}

		if !post.CreatedAt.UTC().Before(threshold) {
			continue
		}

		targets = append(targets, post)
	}

	return targets
}
