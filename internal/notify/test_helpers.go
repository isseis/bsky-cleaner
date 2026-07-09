//go:build test

package notify

// BuildPayloadPreview exposes buildPayload to internal/notify/notifypreview
// (a //go:build test developer tool that renders canned Outcome values
// without sending them), so the preview tool always renders through the
// exact same formatting code as production Send, never a reimplementation.
// Gated behind the test build tag like notifypreview itself, so it never
// reaches the production binary.
//
// The caller (notifypreview) reads the returned webhookPayload's fields for
// display and never constructs one itself. The unexported return type is
// intentional: notifypreview reads the returned fields directly.
//
//nolint:revive
func BuildPayloadPreview(outcome Outcome) webhookPayload {
	return buildPayload(outcome)
}
