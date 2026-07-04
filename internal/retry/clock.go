// Package retry provides a generic HTTPDoer decorator that retries
// transient HTTP failures (transport errors, 429, 5xx) with bounded
// exponential backoff. It has no dependency on internal/atproto.
package retry

import (
	"context"
	"time"
)

// Clock abstracts the backoff wait so tests can simulate elapsed time
// without a real sleep. Sleep returns ctx.Err() if ctx is done before d
// elapses, letting a caller detect a forced interruption during the wait
// itself, not only during the HTTP round trip.
type Clock interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// RealClock is the production Clock: it sleeps for the real duration d, or
// returns ctx.Err() early if ctx is canceled/expires first.
type RealClock struct{}

// Sleep waits for d, or returns ctx.Err() as soon as ctx is done, whichever
// comes first. d <= 0 is checked against ctx.Err() explicitly up front,
// rather than relying on a select race with an already-past timer, so a
// canceled ctx is always detected even when there is nothing left to wait
// for.
func (RealClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
