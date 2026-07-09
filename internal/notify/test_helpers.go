//go:build test

package notify

// BuildPayloadPreview exposes buildPayload to internal/notify/notifypreview
// (a //go:build test developer tool that renders canned Outcome values
// without sending them), so the preview tool always renders through the
// exact same formatting code as production Send, never a reimplementation.
// Gated behind the test build tag like notifypreview itself, so it never
// reaches the production binary.
//
// reads its fields for display, never constructs one itself.
//
//nolint:revive // unexported return type is intentional: notifypreview only
func BuildPayloadPreview(outcome Outcome) webhookPayload {
	return buildPayload(outcome)
}
