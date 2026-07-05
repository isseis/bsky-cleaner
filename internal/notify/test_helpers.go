//go:build test

package notify

import (
	"context"
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
