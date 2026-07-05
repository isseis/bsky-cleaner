package notify

import (
	"errors"
	"fmt"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/config"
)

// unknownErrorKind is the fallback category for any error that does not
// match one of the typed errors below. Per the architecture doc's "unknown
// error fallback" note, no other information is derivable from an unknown
// error type without risking secret leakage, so this is intentionally the
// only text returned for such errors.
const unknownErrorKind = "unknown error"

// errorKind converts err into a short, fixed-shape category string for
// inclusion in a Slack notification. It only reads fields already
// documented as secret-free on the matched error types (config.FieldError's
// Field, atproto.HTTPError's Method/StatusCode/ErrorName,
// atproto.SSRFError's Endpoint/Stage) and never renders err.Error() or a
// %v/%+v expansion of an unmatched error, since that could embed
// credentials or other sensitive detail from a wrapped error chain.
func errorKind(err error) string {
	if fieldErr, ok := errors.AsType[*config.FieldError](err); ok {
		return fmt.Sprintf("config error: field %q", fieldErr.Field)
	}
	if httpErr, ok := errors.AsType[*atproto.HTTPError](err); ok {
		if httpErr.ErrorName == "" {
			return fmt.Sprintf("atproto http error: %s status=%d", httpErr.Method, httpErr.StatusCode)
		}
		return fmt.Sprintf("atproto http error: %s status=%d error=%s", httpErr.Method, httpErr.StatusCode, httpErr.ErrorName)
	}
	if ssrfErr, ok := errors.AsType[*atproto.SSRFError](err); ok {
		return fmt.Sprintf("atproto ssrf error: endpoint=%q stage=%s", ssrfErr.Endpoint, ssrfErr.Stage)
	}
	return unknownErrorKind
}
