package cleanup_test

import (
	"testing"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/cleanup"
	"github.com/stretchr/testify/assert"
)

// fixedNow is the judgment time used by every test in this file, so
// results are deterministic and independent of time.Now() (NF-002a).
var fixedNow = time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)

const fixedRetentionDays = 30

func TestSelectDeletionTargets_OlderThanThreshold_Included(t *testing.T) {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	post := atproto.Post{RKey: "old", Type: atproto.PostTypeOriginal, CreatedAt: threshold.AddDate(0, 0, -1)}

	got := cleanup.SelectDeletionTargets([]atproto.Post{post}, fixedRetentionDays, fixedNow)

	assert.Equal(t, []atproto.Post{post}, got)
}

func TestSelectDeletionTargets_WithinThreshold_Excluded(t *testing.T) {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	post := atproto.Post{RKey: "recent", Type: atproto.PostTypeOriginal, CreatedAt: threshold.AddDate(0, 0, 1)}

	got := cleanup.SelectDeletionTargets([]atproto.Post{post}, fixedRetentionDays, fixedNow)

	assert.Empty(t, got)
}

func TestSelectDeletionTargets_ExactlyAtThreshold_Excluded(t *testing.T) {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	post := atproto.Post{RKey: "boundary", Type: atproto.PostTypeOriginal, CreatedAt: threshold}

	got := cleanup.SelectDeletionTargets([]atproto.Post{post}, fixedRetentionDays, fixedNow)

	assert.Empty(t, got)
}

func TestSelectDeletionTargets_NonUTCOffset_NormalizedBeforeComparison(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)

	// In JST wall-clock time this looks newer than the UTC threshold, but
	// converting to UTC (subtracting the 9h offset) puts it before the
	// threshold -- the implementation must compare in UTC, not local time.
	olderInUTC := atproto.Post{
		RKey:      "older-after-utc-conversion",
		Type:      atproto.PostTypeOriginal,
		CreatedAt: threshold.Add(-1 * time.Hour).In(jst),
	}
	// Conversely, this looks newer in JST wall-clock time, and it also
	// converts to a UTC time still within (i.e. not before) the threshold.
	newerInUTC := atproto.Post{
		RKey:      "newer-after-utc-conversion",
		Type:      atproto.PostTypeOriginal,
		CreatedAt: threshold.Add(1 * time.Hour).In(jst),
	}

	got := cleanup.SelectDeletionTargets([]atproto.Post{olderInUTC, newerInUTC}, fixedRetentionDays, fixedNow)

	assert.Equal(t, []atproto.Post{olderInUTC}, got)
}

func TestSelectDeletionTargets_AllKnownPostTypes_Included(t *testing.T) {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	createdAt := threshold.AddDate(0, 0, -1)

	tests := []struct {
		name     string
		postType atproto.PostType
	}{
		{"original", atproto.PostTypeOriginal},
		{"reply", atproto.PostTypeReply},
		{"quote", atproto.PostTypeQuote},
		{"repost", atproto.PostTypeRepost},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post := atproto.Post{RKey: tt.name, Type: tt.postType, CreatedAt: createdAt}

			got := cleanup.SelectDeletionTargets([]atproto.Post{post}, fixedRetentionDays, fixedNow)

			assert.Equal(t, []atproto.Post{post}, got)
		})
	}
}

func TestSelectDeletionTargets_UnknownPostType_Excluded(t *testing.T) {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	post := atproto.Post{RKey: "unknown", Type: atproto.PostType(99), CreatedAt: threshold.AddDate(0, 0, -1)}

	got := cleanup.SelectDeletionTargets([]atproto.Post{post}, fixedRetentionDays, fixedNow)

	assert.Empty(t, got)
}

func TestSelectDeletionTargets_Pinned_Excluded(t *testing.T) {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	post := atproto.Post{RKey: "pinned", Type: atproto.PostTypeOriginal, CreatedAt: threshold.AddDate(0, 0, -1), Pinned: true}

	got := cleanup.SelectDeletionTargets([]atproto.Post{post}, fixedRetentionDays, fixedNow)

	assert.Empty(t, got)
}

func TestSelectDeletionTargets_Unpinned_Included(t *testing.T) {
	threshold := fixedNow.AddDate(0, 0, -fixedRetentionDays)
	post := atproto.Post{RKey: "unpinned", Type: atproto.PostTypeOriginal, CreatedAt: threshold.AddDate(0, 0, -1), Pinned: false}

	got := cleanup.SelectDeletionTargets([]atproto.Post{post}, fixedRetentionDays, fixedNow)

	assert.Equal(t, []atproto.Post{post}, got)
}

func TestSelectDeletionTargets_NilInput_ReturnsEmpty(t *testing.T) {
	got := cleanup.SelectDeletionTargets(nil, fixedRetentionDays, fixedNow)

	assert.Empty(t, got)
	assert.NotNil(t, got)
}
