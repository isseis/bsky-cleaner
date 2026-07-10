package notify

import (
	"context"
	"testing"
	"time"
)

// fakeClock is a retry.Clock that never actually sleeps: it records every
// requested delay in SleepCalls and returns immediately, letting tests
// assert on retry behavior without spending real wall-clock time. Mirrors
// internal/retry's own test-only fakeClock (internal/retry/test_helpers.go).
type fakeClock struct {
	SleepCalls []time.Duration
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.SleepCalls = append(c.SleepCalls, d)
	return nil
}

// findField searches fields for the first element whose Title matches title
// and returns it. If no match is found, it calls t.Fatalf immediately.
// This is a B2 helper (private, package-internal only) that lets tests
// assert on individual field values without depending on field index, which
// changes as phases add Host/Account/statistics fields.
func findField(t *testing.T, fields []slackField, title string) slackField {
	t.Helper()
	for _, f := range fields {
		if f.Title == title {
			return f
		}
	}
	t.Fatalf("findField: no field with Title %q found among %d fields", title, len(fields))
	return slackField{} // unreachable
}
