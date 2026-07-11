package notify

import (
	"context"
	"io"
	"net/http"
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

// ctxSensitiveBody is an io.ReadCloser whose Read method returns data from
// buf exactly once, then io.EOF — but only if ctx is still alive. If ctx
// has already been cancelled (ctx.Err() != nil), Read returns ctx.Err()
// instead. This lets tests assert that per-attempt contexts are still valid
// when the retry loop drains a 429/5xx response body (AC-08/AC-09).
type ctxSensitiveBody struct {
	buf  []byte
	ctx  context.Context
	used bool
}

func (b *ctxSensitiveBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.used {
		return 0, io.EOF
	}
	b.used = true
	return copy(p, b.buf), io.EOF
}

func (b *ctxSensitiveBody) Close() error { return nil }

// blockingUntilCtxDoneBody is an io.ReadCloser whose Read blocks until
// ctx is done and then returns ctx.Err(). This lets tests verify that
// per-attempt timeout bounds the body read as well (AC-11).
type blockingUntilCtxDoneBody struct {
	ctx context.Context
}

func (b *blockingUntilCtxDoneBody) Read(_ []byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *blockingUntilCtxDoneBody) Close() error { return nil }

// scriptedDoer is an HTTPDoer that invokes []handlerFunc in order, one per
// Do call. Each handler receives the request and can inspect its context to
// build ctxSensitiveBody or blockingUntilCtxDoneBody as needed.
type scriptedDoer struct {
	t      *testing.T
	steps  []handlerFunc
	cursor int
}

type handlerFunc func(req *http.Request) (*http.Response, error)

func (d *scriptedDoer) Do(req *http.Request) (*http.Response, error) {
	if d.cursor >= len(d.steps) {
		d.t.Fatalf("scriptedDoer: unexpected call %d (only %d steps registered)", d.cursor, len(d.steps))
		return nil, nil // unreachable
	}
	fn := d.steps[d.cursor]
	d.cursor++
	return fn(req)
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
