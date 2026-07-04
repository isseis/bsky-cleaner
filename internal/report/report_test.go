package report_test

import (
	"errors"
	"testing"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/report"
	"github.com/stretchr/testify/assert"
)

func TestFormatText_DryRun_WithTargets(t *testing.T) {
	targets := []atproto.Post{
		{RKey: "first"},
		{RKey: "second"},
	}

	got := report.FormatText(report.Result{Mode: report.ModeDryRun, Targets: targets})

	assert.Contains(t, got, "first")
	assert.Contains(t, got, "second")
}

func TestFormatText_DryRun_NoTargets(t *testing.T) {
	got := report.FormatText(report.Result{Mode: report.ModeDryRun, Targets: nil})

	assert.Contains(t, got, "No posts to delete")
}

func TestFormatText_Apply_AllSucceeded(t *testing.T) {
	deleted := []atproto.Post{{RKey: "first"}, {RKey: "second"}}

	got := report.FormatText(report.Result{Mode: report.ModeApply, Deleted: deleted, Failed: nil})

	assert.Contains(t, got, "Deleted 2 post(s), 0 failure(s).")
}

func TestFormatText_Apply_WithFailures(t *testing.T) {
	deleted := []atproto.Post{{RKey: "ok"}}
	failed := []report.DeleteFailure{
		{Post: atproto.Post{RKey: "bad-one"}, Err: errors.New("network error")},
		{Post: atproto.Post{RKey: "bad-two"}, Err: errors.New("rate limited")},
	}

	got := report.FormatText(report.Result{Mode: report.ModeApply, Deleted: deleted, Failed: failed})

	assert.Contains(t, got, "2")
	assert.Contains(t, got, "bad-one")
	assert.Contains(t, got, "network error")
	assert.Contains(t, got, "bad-two")
	assert.Contains(t, got, "rate limited")
}
