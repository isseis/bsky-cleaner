//go:build test

package retry

import (
	"context"
	"time"
)

// fakeClock is a Clock that never actually sleeps: it records every
// requested delay in SleepCalls and returns immediately, letting tests
// assert on backoff timing without spending real wall-clock time. Like
// RealClock, it still honors ctx: a canceled/expired ctx makes Sleep
// return ctx.Err() immediately without recording the call, so tests can
// simulate the "deadline hit during backoff wait" path with the same
// implementation.
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
