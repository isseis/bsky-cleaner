//go:build test

package atprototestutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// jsonString returns s encoded as a JSON string literal (quotes included).
// It uses encoding/json rather than fmt's %q verb: %q escapes the way Go
// source literals do, not the way JSON does, and can emit an escape (e.g.
// \a, \v) that is invalid JSON for certain control-byte inputs. The
// fixture builders below take arbitrary caller-supplied strings (DIDs,
// JWTs, cursors), so they go through this helper instead of %q.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("jsonString: %v", err))
	}
	return string(b)
}

// This file provides response fixtures for the four XRPC endpoints
// internal/atproto calls (com.atproto.server.createSession,
// com.atproto.repo.listRecords, com.atproto.repo.getRecord,
// com.atproto.repo.deleteRecord), together with Go structs mirroring each
// endpoint's response shape per the official AT Protocol lexicon. Phase 2-5
// tests build their mock responses through the JSON-producing functions
// below rather than writing the envelope JSON inline, so every test's
// mocked response shape is checked against the lexicon once, here, by
// fixtures_test.go (NF-006) instead of by each test file separately.
//
// Record *contents* (e.g. the app.bsky.feed.post fields that distinguish a
// reply from a quote post) are not modeled here: those are a different,
// per-collection lexicon each test constructs directly to exercise a
// specific classification case, and NF-006 only asks for the com.atproto.*
// envelope shapes below.

// CreateSessionResponse is the com.atproto.server.createSession response
// body. Per the lexicon, accessJwt/refreshJwt/handle/did are all required.
type CreateSessionResponse struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	Handle     string `json:"handle"`
	DID        string `json:"did"`
}

// CreateSessionResponseRequiredFields lists the CreateSessionResponse JSON
// field names the lexicon marks required.
var CreateSessionResponseRequiredFields = []string{"accessJwt", "refreshJwt", "handle", "did"}

// CreateSessionResponseJSON returns a lexicon-shaped
// com.atproto.server.createSession success response binding did/accessJwt
// to the given values; handle/refreshJwt are filled with fixed placeholders
// a caller can ignore.
func CreateSessionResponseJSON(did, accessJwt string) string {
	return fmt.Sprintf(`{"accessJwt":%s,"refreshJwt":"fixture-refresh-jwt","handle":"alice.test","did":%s}`, jsonString(accessJwt), jsonString(did))
}

// ListRecordsRecord is one entry of a com.atproto.repo.listRecords
// response. Per the lexicon, uri/cid/value are all required.
type ListRecordsRecord struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

// ListRecordsResponse is the com.atproto.repo.listRecords response body.
// Per the lexicon, records is required; cursor is optional.
type ListRecordsResponse struct {
	Cursor  string              `json:"cursor,omitempty"`
	Records []ListRecordsRecord `json:"records"`
}

// ListRecordsResponseRequiredFields lists the ListRecordsResponse JSON
// field names the lexicon marks required.
var ListRecordsResponseRequiredFields = []string{"records"}

// ListRecordsRecordRequiredFields lists the ListRecordsRecord JSON field
// names the lexicon marks required.
var ListRecordsRecordRequiredFields = []string{"uri", "cid", "value"}

// ListRecordsResponseJSON assembles a lexicon-shaped
// com.atproto.repo.listRecords response body from pre-built record JSON
// snippets (each a full {"uri":...,"cid":...,"value":...} object) and an
// optional cursor.
func ListRecordsResponseJSON(records []string, cursor string) string {
	body := `{"records":[` + strings.Join(records, ",") + `]`
	if cursor != "" {
		body += `,"cursor":` + jsonString(cursor)
	}
	body += `}`
	return body
}

// GetRecordResponse is the com.atproto.repo.getRecord response body. Per
// the lexicon, uri and value are required; cid is optional.
type GetRecordResponse struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid,omitempty"`
	Value json.RawMessage `json:"value"`
}

// GetRecordResponseRequiredFields lists the GetRecordResponse JSON field
// names the lexicon marks required.
var GetRecordResponseRequiredFields = []string{"uri", "value"}

// GetRecordResponseJSON returns a lexicon-shaped com.atproto.repo.getRecord
// success response for the given uri/cid/record value.
func GetRecordResponseJSON(uri, cid, valueJSON string) string {
	return fmt.Sprintf(`{"uri":%s,"cid":%s,"value":%s}`, jsonString(uri), jsonString(cid), valueJSON)
}

// DeleteRecordCommit is the optional "commit" object a
// com.atproto.repo.deleteRecord response may carry. Per the lexicon, cid
// and rev are both required whenever commit is present.
type DeleteRecordCommit struct {
	CID string `json:"cid"`
	Rev string `json:"rev"`
}

// DeleteRecordCommitRequiredFields lists the DeleteRecordCommit JSON field
// names the lexicon marks required (only meaningful when commit is
// present at all, since the field itself is optional on the response).
var DeleteRecordCommitRequiredFields = []string{"cid", "rev"}

// DeleteRecordResponse is the com.atproto.repo.deleteRecord response body.
// Per the lexicon, commit itself is optional; internal/atproto's
// DeleteRecord does not decode this body at all (delete.go passes a nil
// decode target to doXRPC), so this struct exists solely for the
// lexicon-compliance check in fixtures_test.go.
type DeleteRecordResponse struct {
	Commit *DeleteRecordCommit `json:"commit,omitempty"`
}

// DeleteRecordResponseRequiredFields lists the DeleteRecordResponse JSON
// field names the lexicon marks required. It is empty because the
// lexicon's only top-level field (commit) is optional.
var DeleteRecordResponseRequiredFields = []string{}

// DeleteRecordResponseJSON returns a lexicon-shaped
// com.atproto.repo.deleteRecord success response body (empty, since
// internal/atproto never reads it -- see DeleteRecordResponse).
func DeleteRecordResponseJSON() string {
	return `{}`
}

// DeleteRecordResponseWithCommitJSON returns a lexicon-shaped
// com.atproto.repo.deleteRecord success response body that includes the
// optional commit object, for the lexicon-compliance check that decodes a
// commit into DeleteRecordCommit.
func DeleteRecordResponseWithCommitJSON(cid, rev string) string {
	return fmt.Sprintf(`{"commit":{"cid":%s,"rev":%s}}`, jsonString(cid), jsonString(rev))
}
