// Package runner wires config/atproto/cleanup/report together into a
// single dry-run/apply pass over one account's posts.
package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/cleanup"
	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/isseis/bsky-cleaner/internal/report"
)

// Client is the subset of *atproto.Client that Run depends on, so tests
// can supply a fake instead of a real network client.
type Client interface {
	Login(ctx context.Context, appPassword config.SecretString) error
	ListPosts(ctx context.Context) ([]atproto.Post, error)
	DeleteRecord(ctx context.Context, post atproto.Post) error
}

// Run performs one wiring pass: login, list posts, judge deletion targets
// via cleanup.SelectDeletionTargets, and -- only when apply is true --
// delete each target, continuing past individual failures. now is the
// caller-supplied judgment time, passed straight through to
// SelectDeletionTargets. Login/ListPosts errors are returned as-is,
// unwrapped, so callers can use errors.Is/errors.AsType[T] against the
// underlying atproto error types.
func Run(ctx context.Context, client Client, appPassword config.SecretString, retentionDays int, apply bool, now time.Time) (*report.Result, error) {
	if err := client.Login(ctx, appPassword); err != nil {
		return nil, err
	}

	posts, err := client.ListPosts(ctx)
	if err != nil {
		return nil, err
	}

	targets := cleanup.SelectDeletionTargets(posts, retentionDays, now)

	if !apply {
		return &report.Result{Mode: report.ModeDryRun, Targets: targets}, nil
	}

	result := &report.Result{Mode: report.ModeApply, Targets: targets}
	for _, post := range targets {
		if err := client.DeleteRecord(ctx, post); err != nil {
			slog.Default().Error("delete post failed", "rkey", post.RKey, "error", err)
			result.Failed = append(result.Failed, report.DeleteFailure{Post: post, Err: err})
			continue
		}
		slog.Default().Info("delete post succeeded", "rkey", post.RKey)
		result.Deleted = append(result.Deleted, post)
	}

	return result, nil
}
