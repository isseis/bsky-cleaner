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
	"github.com/isseis/bsky-cleaner/internal/report"
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
		_, _ = fmt.Fprintln(stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return exitSetupOrRunFail
	}

	// Bound the whole network-calling pass by cfg.ExecutionTimeout so a
	// stuck PDS request cannot hang the CLI (and thus a cron invocation)
	// indefinitely.
	ctx, cancel := context.WithTimeout(ctx, cfg.ExecutionTimeout)
	defer cancel()

	client, err := atproto.NewClient(ctx, cfg.Handle, httpDoer)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return exitSetupOrRunFail
	}

	result, err := runner.Run(ctx, client, cfg.AppPassword, cfg.RetentionDays, apply, now)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return exitSetupOrRunFail
	}

	_, _ = fmt.Fprint(stdout, report.FormatText(*result)) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	if len(result.Failed) > 0 {
		return exitPartialFailure
	}
	return exitOK
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
