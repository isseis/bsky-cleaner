//go:build test

package atprototestutil

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeStrict decodes body into target, rejecting any field body carries
// that target does not declare -- the mechanism this file uses to detect a
// fixture that has drifted from the lexicon shape it claims to model.
func decodeStrict(t *testing.T, body string, target any) error {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}

func TestFixtures_CreateSessionResponse_MatchesLexicon(t *testing.T) {
	body := CreateSessionResponseJSON("did:plc:test123", "fixture-access-jwt")

	var resp CreateSessionResponse
	require.NoError(t, decodeStrict(t, body, &resp))
	assert.Equal(t, "did:plc:test123", resp.DID)
	assert.Equal(t, "fixture-access-jwt", resp.AccessJwt)
}

func TestFixtures_ListRecordsResponse_MatchesLexicon(t *testing.T) {
	record := `{"uri":"at://did:plc:test123/app.bsky.feed.post/abc","cid":"bafycid","value":{"$type":"app.bsky.feed.post","text":"hello","createdAt":"2024-01-01T00:00:00Z"}}`
	body := ListRecordsResponseJSON([]string{record}, "next-cursor")

	var resp ListRecordsResponse
	require.NoError(t, decodeStrict(t, body, &resp))
	require.Len(t, resp.Records, 1)
	assert.Equal(t, "at://did:plc:test123/app.bsky.feed.post/abc", resp.Records[0].URI)
	assert.Equal(t, "bafycid", resp.Records[0].CID)
	assert.Equal(t, "next-cursor", resp.Cursor)
}

func TestFixtures_GetRecordResponse_MatchesLexicon(t *testing.T) {
	body := GetRecordResponseJSON(
		"at://did:plc:test123/app.bsky.actor.profile/self",
		"bafyprofile",
		`{"$type":"app.bsky.actor.profile","pinnedPost":{"uri":"at://did:plc:test123/app.bsky.feed.post/pinned","cid":"bafypinned"}}`,
	)

	var resp GetRecordResponse
	require.NoError(t, decodeStrict(t, body, &resp))
	assert.Equal(t, "at://did:plc:test123/app.bsky.actor.profile/self", resp.URI)
	assert.Equal(t, "bafyprofile", resp.CID)
}

func TestFixtures_DeleteRecordResponse_MatchesLexicon(t *testing.T) {
	t.Run("empty body (commit omitted)", func(t *testing.T) {
		var resp DeleteRecordResponse
		require.NoError(t, decodeStrict(t, DeleteRecordResponseJSON(), &resp))
		assert.Nil(t, resp.Commit)
	})

	t.Run("body with commit", func(t *testing.T) {
		var resp DeleteRecordResponse
		require.NoError(t, decodeStrict(t, DeleteRecordResponseWithCommitJSON("bafycommit", "rev-1"), &resp))
		require.NotNil(t, resp.Commit)
		assert.Equal(t, "bafycommit", resp.Commit.CID)
		assert.Equal(t, "rev-1", resp.Commit.Rev)
	})
}

// TestFixtures_CreateSessionResponse_RejectsUnknownField proves
// decodeStrict's DisallowUnknownFields is actually active: without this
// test, a regression that swapped decodeStrict for plain json.Unmarshal
// would leave every MatchesLexicon test above green despite no longer
// detecting a field-name drift from the lexicon.
func TestFixtures_CreateSessionResponse_RejectsUnknownField(t *testing.T) {
	body := `{"accessJwt":"a","refreshJwt":"r","handle":"alice.test","did":"did:plc:test123","unexpectedField":"x"}`

	var resp CreateSessionResponse
	err := decodeStrict(t, body, &resp)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpectedField")
}

// structJSONTags returns the "json" struct tag name of every field of v
// (a struct value), in declaration order, ignoring any ",omitempty" etc.
// suffix.
func structJSONTags(t *testing.T, v any) []string {
	t.Helper()
	typ := reflect.TypeOf(v)
	require.Equal(t, reflect.Struct, typ.Kind())

	tags := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		tags = append(tags, name)
	}
	return tags
}

// TestFixtures_RequiredFieldsPresent verifies that every field the lexicon
// marks required for an endpoint is actually declared (under the matching
// JSON name) on the corresponding fixture struct. DisallowUnknownFields
// (used above) only catches a JSON body carrying a field the struct
// doesn't know about; it cannot catch the opposite drift -- a struct
// silently missing a field the lexicon requires -- since json.Decoder
// simply ignores JSON keys it isn't asked to fill.
func TestFixtures_RequiredFieldsPresent(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		required []string
	}{
		{"createSession", CreateSessionResponse{}, CreateSessionResponseRequiredFields},
		{"listRecords", ListRecordsResponse{}, ListRecordsResponseRequiredFields},
		{"listRecords record", ListRecordsRecord{}, ListRecordsRecordRequiredFields},
		{"getRecord", GetRecordResponse{}, GetRecordResponseRequiredFields},
		{"deleteRecord", DeleteRecordResponse{}, DeleteRecordResponseRequiredFields},
		{"deleteRecord commit", DeleteRecordCommit{}, DeleteRecordCommitRequiredFields},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := structJSONTags(t, tt.value)
			for _, field := range tt.required {
				assert.Contains(t, tags, field, "lexicon-required field %q must be declared on %T", field, tt.value)
			}
		})
	}
}
