//go:build test

// Package main (notifypreview) is a developer-only tool that renders canned
// report.Result/notify.Outcome values through the production Slack payload
// formatter (notify.BuildPayloadPreview) and, optionally, sends them via the
// production notify.Send, so message-format changes can be reviewed without
// running a real Bluesky delete pass. It is gated behind the "test" build
// tag and is never linked into the production bsky-cleaner binary.
package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/notify"
	"github.com/isseis/bsky-cleaner/internal/report"
)

// scenario is one named, canned notify.Outcome to preview.
type scenario struct {
	name    string
	outcome notify.Outcome
}

// post builds a minimal atproto.Post fixture. buildPayload only ever reads
// RKey off a Post, but the other fields are filled in for a realistic
// preview.
func post(rkey string) atproto.Post {
	return atproto.Post{
		RKey:      rkey,
		Type:      atproto.PostTypeOriginal,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// scenarios lists every canned Outcome this tool can render, covering each
// branch of notify.buildPayload: success with nothing to report, success
// with deletions, partial failure (routes to the failure channel), a run
// that errored before producing a report.Result, and a large failure list
// that exercises payload truncation.
func scenarios() []scenario {
	return []scenario{
		{
			name: "success-empty",
			outcome: notify.Outcome{
				Result:  &report.Result{Mode: report.ModeApply},
				Host:    "worker-1",
				Account: "alice.bsky.social",
			},
		},
		{
			name: "success-apply",
			outcome: notify.Outcome{
				Result: &report.Result{
					Mode:    report.ModeApply,
					Deleted: []atproto.Post{post("post1"), post("post2"), post("post3")},
				},
				Host:    "worker-1",
				Account: "alice.bsky.social",
			},
		},
		{
			name: "partial-failure",
			outcome: notify.Outcome{
				Result: &report.Result{
					Mode:    report.ModeApply,
					Deleted: []atproto.Post{post("post1"), post("post2")},
					Failed: []report.DeleteFailure{
						{Post: post("post3"), Err: &atproto.HTTPError{Method: "POST", StatusCode: 429, ErrorName: "RateLimitExceeded"}},
					},
				},
				Host:    "worker-1",
				Account: "alice.bsky.social",
			},
		},
		{
			name: "run-error",
			outcome: notify.Outcome{
				Err:     &atproto.SSRFError{Endpoint: "https://example.invalid/xrpc/com.atproto.repo.listRecords", Stage: atproto.SSRFStageInitialValidation},
				Host:    "worker-1",
				Account: "alice.bsky.social",
			},
		},
		{
			name: "truncation",
			outcome: notify.Outcome{
				Result:  truncationResult(),
				Host:    "worker-1",
				Account: "alice.bsky.social",
			},
		},
	}
}

// truncationResult builds a report.Result with enough delete failures that
// notify.BuildPayloadPreview's rendered text exceeds the 4000-byte payload
// limit, so the preview shows the truncation marker in effect.
func truncationResult() *report.Result {
	const failureCount = 200

	failures := make([]report.DeleteFailure, failureCount)
	for i := range failureCount {
		failures[i] = report.DeleteFailure{
			Post: post(fmt.Sprintf("post-%03d", i)),
			Err:  errors.New("some transport error"), // renders as "unknown error" via errorKind; the count, not the text, is what drives truncation here
		}
	}

	return &report.Result{Mode: report.ModeApply, Failed: failures}
}
