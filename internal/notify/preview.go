//go:build test

package notify

// BuildPayloadPreview exposes buildPayload to internal/notify/notifypreview
// (a //go:build test developer tool that renders canned Outcome values
// without sending them), so the preview tool always renders through the
// exact same formatting code as production Send, never a reimplementation.
// Gated behind the test build tag like notifypreview itself, so it never
// reaches the production binary.
func BuildPayloadPreview(outcome Outcome) string {
	return buildPayload(outcome)
}
