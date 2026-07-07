// Command bsky-cleaner loads configuration, lists an account's posts,
// judges which are eligible for deletion, and (only with --apply) deletes
// them. It wires together internal/config, internal/atproto,
// internal/runner, and internal/report; it carries no deletion-judgment
// logic of its own.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/isseis/bsky-cleaner/internal/notify"
	"github.com/isseis/bsky-cleaner/internal/report"
	"github.com/isseis/bsky-cleaner/internal/retry"
	"github.com/isseis/bsky-cleaner/internal/runner"
)

// Exit codes. Usage/flag errors (2, exitUsageError) are distinguished from
// other setup-or-run failures such as config/init/login/list (1,
// exitSetupOrRunFail), which are in turn distinguished from partial-delete
// failures (3) because the latter means some posts are already irreversibly
// gone -- cron/monitoring must be able to tell all of these apart without
// parsing stdout.
const (
	exitOK             = 0
	exitUsageError     = 2
	exitSetupOrRunFail = 1
	exitPartialFailure = 3
)

// notifyTimeout bounds internal/notify.Send's own retry loop. It is set to
// roughly the architecture doc's ~12s worst-case Send duration (3s HTTP
// timeout x up to 3 attempts + 1s/2s backoff) plus a safety margin, so a
// slow-but-not-hung Slack endpoint cannot silently exceed this budget before
// Send's own bounded retries give up.
const notifyTimeout = 15 * time.Second

// parseFlags parses args (excluding the program name) into a config path
// and the apply flag. It returns an error -- never calling os.Exit --
// whenever flag.FlagSet.Parse fails, --config/-c is missing, or unexpected
// positional arguments remain, so main's exit-code behavior stays testable
// without ending the test process.
func parseFlags(args []string, _ io.Writer) (configPath string, apply bool, err error) {
	fs := flag.NewFlagSet("bsky-cleaner", flag.ContinueOnError)
	// Discard flag's own error+usage output: fs.Parse would otherwise write
	// the same error message that main prints via the returned err, so this
	// avoids printing it twice.
	fs.SetOutput(io.Discard)

	fs.StringVar(&configPath, "config", "", "path to the TOML configuration file")
	fs.StringVar(&configPath, "c", "", "path to the TOML configuration file (shorthand for --config)")
	fs.BoolVar(&apply, "apply", false, "actually delete posts (default: dry-run)")

	if err := fs.Parse(args); err != nil {
		return "", false, err
	}

	if fs.NArg() > 0 {
		fs.Usage()
		return "", false, fmt.Errorf("unexpected positional argument(s): %v", fs.Args())
	}

	if configPath == "" {
		fs.Usage()
		return "", false, fmt.Errorf("--config (or -c) is required")
	}

	return configPath, apply, nil
}

// run performs one full wiring pass: load configuration, build the
// AT Protocol client, run the cleanup pass, and render the result. It
// takes httpDoer/stdout/stderr as parameters (rather than reaching for
// http.DefaultClient/os.Stdout/os.Stderr directly) so tests can drive it
// without real network I/O or an os.Exit call.
func run(configPath string, apply bool, now time.Time, httpDoer atproto.HTTPDoer, stdout, stderr io.Writer) int {
	ctx := context.Background()

	cfg, err := config.LoadAppConfig(configPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, notify.Sanitize(err.Error())) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return exitSetupOrRunFail
	}

	// Bound the whole network-calling pass by cfg.ExecutionTimeout so a
	// stuck PDS request cannot hang the CLI (and thus a cron invocation)
	// indefinitely.
	ctx, cancel := context.WithTimeout(ctx, cfg.ExecutionTimeout)
	defer cancel()

	client, err := atproto.NewClient(ctx, cfg.Handle, httpDoer)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, notify.Sanitize(err.Error())) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return exitSetupOrRunFail
	}

	result, runErr := runner.Run(ctx, client, cfg.AppPassword, cfg.RetentionDays, apply, now)
	if runErr != nil {
		_, _ = fmt.Fprintln(stderr, notify.Sanitize(runErr.Error())) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	} else {
		_, _ = fmt.Fprint(stdout, notify.Sanitize(report.FormatText(*result))) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	}

	// apply-only, and independent of ctx above: ctx's execution-timeout
	// budget may already be nearly spent by the time runner.Run returns, and
	// reusing it here would make notification least reliable exactly when
	// the run itself errored (architecture doc section 3.5).
	if apply {
		if sendErr := sendNotification(cfg, httpDoer, result, runErr); sendErr != nil {
			_, _ = fmt.Fprintln(stderr, sendErr.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		}
	}

	switch {
	case runErr != nil:
		return exitSetupOrRunFail
	case len(result.Failed) > 0:
		return exitPartialFailure
	default:
		return exitOK
	}
}

// sendNotification builds and sends the Slack notification for one apply
// run, using its own timeout/context independent of run's execution-timeout
// ctx (see the comment at its call site). A delivery failure is returned to
// the caller for stderr reporting only -- it never influences run's exit
// code.
func sendNotification(cfg *config.AppConfig, httpDoer atproto.HTTPDoer, result *report.Result, runErr error) error {
	notifyCtx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()

	notifyCfg := notify.Config{
		SuccessWebhookURL: cfg.SlackSuccessWebhookURL,
		FailureWebhookURL: cfg.SlackFailureWebhookURL,
	}
	return notify.Send(notifyCtx, notifyCfg, httpDoer, retry.RealClock{}, notify.Outcome{Result: result, Err: runErr})
}

func main() {
	configPath, apply, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(exitOK)
		}
		os.Exit(exitUsageError)
	}

	now := time.Now()
	os.Exit(run(configPath, apply, now, http.DefaultClient, os.Stdout, os.Stderr))
}
