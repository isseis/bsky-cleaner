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
	"strconv"
	"strings"
	"time"

	"github.com/isseis/bsky-cleaner/internal/atproto"
	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/isseis/bsky-cleaner/internal/notify"
	"github.com/isseis/bsky-cleaner/internal/report"
	"github.com/isseis/bsky-cleaner/internal/retry"
	"github.com/isseis/bsky-cleaner/internal/runner"
)

// version and commit are set at build time via -ldflags -X.
// When built without -ldflags (local development), version is "dev" and
// commit is empty.
var (
	version = "dev"
	commit  = ""
)

// errVersionRequested is a sentinel error returned by parseFlags when
// --version/-v was given, analogous to flag.ErrHelp.
var errVersionRequested = errors.New("version requested")

// formatVersion renders the build-time version/commit variables into the
// single-line string --version prints. When commit is empty (a build with
// no embedded version information), it returns version alone.
func formatVersion() string {
	if commit == "" {
		return version
	}
	return version + " (" + commit + ")"
}

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
// timeout x up to 3 attempts plus 1s/2s backoff) plus a safety margin, so a
// slow-but-not-hung Slack endpoint cannot silently exceed this budget before
// Send's own bounded retries give up.
const notifyTimeout = 15 * time.Second

// ScheduleValidationError reports that the schedule field failed
// cron-syntax validation.
type ScheduleValidationError struct {
	Reason string
}

func (e *ScheduleValidationError) Error() string {
	return "schedule validation failed: " + e.Reason
}

// cronField describes the valid range for one cron field.
type cronField struct {
	name  string
	lower int
	upper int
}

// cronRanges defines the five standard cron fields and their valid ranges.
var cronRanges = []cronField{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day of month", 1, 31},
	{"month", 1, 12},
	{"day of week", 0, 7},
}

// validateSchedule checks that s is a syntactically valid cron expression:
// exactly 5 whitespace-separated fields whose values fall within the
// conventional cron ranges. It also rejects any value containing a newline
// (crontab injection prevention).
func validateSchedule(s string) error {
	// Reject newlines (crontab injection prevention, AC-02).
	for _, r := range s {
		if r == '\n' || r == '\r' {
			return &ScheduleValidationError{Reason: "schedule value contains newline"}
		}
	}

	// Split on whitespace. strings.Fields handles multiple spaces/tabs.
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return &ScheduleValidationError{
			Reason: fmt.Sprintf("expected 5 cron fields, got %d", len(fields)),
		}
	}

	for i, field := range fields {
		r := cronRanges[i]
		// Each field is a comma-separated list of items.
		// strings.Fields already stripped surrounding whitespace from the
		// whole field, so individual items need no TrimSpace.
		for item := range strings.SplitSeq(field, ",") {
			if item == "" {
				return &ScheduleValidationError{
					Reason: fmt.Sprintf("empty element in %s field", r.name),
				}
			}
			if err := validateCronItem(item, r); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateCronItem validates a single cron field item (after splitting on
// commas) and checks that its bounds fall within the field's valid range.
func validateCronItem(item string, r cronField) error {
	low, high, err := parseCronItem(item, r.name)
	if err != nil {
		return err
	}
	// Wildcard and wildcard-based steps signal -1,-1 to skip bounds check.
	if low == -1 && high == -1 {
		return nil
	}
	if low < r.lower || high > r.upper || low > high {
		return &ScheduleValidationError{
			Reason: fmt.Sprintf("value %s out of range [%d-%d] in %s field", item, r.lower, r.upper, r.name),
		}
	}
	return nil
}

// parseCronItem parses a single cron field item and returns its numeric
// bounds. It handles wildcards, step expressions, ranges, and single values.
// For wildcards and wildcard-based steps, -1,-1 is returned as a signal to
// validateCronItem to skip the bounds check.
func parseCronItem(item, fieldName string) (int, int, error) {
	switch {
	case item == "*":
		// Wildcard: covers the whole range -- signal caller to skip check.
		return -1, -1, nil
	case strings.Contains(item, "/"):
		// Step form: */n, a-b/n, or n/m.
		return parseCronStep(item, fieldName)
	case strings.Contains(item, "-"):
		// Range form: a-b.
		return parseRange(item, fieldName)
	default:
		// Single value.
		val, err := strconv.Atoi(item)
		if err != nil {
			return 0, 0, &ScheduleValidationError{
				Reason: fmt.Sprintf("invalid value %q in %s field", item, fieldName),
			}
		}
		return val, val, nil
	}
}

// parseCronStep parses a step expression (containing "/") and returns the
// numeric bounds of the range part.
func parseCronStep(item, fieldName string) (int, int, error) {
	parts := strings.SplitN(item, "/", 2)
	if len(parts) != 2 {
		return 0, 0, &ScheduleValidationError{
			Reason: fmt.Sprintf("invalid step expression %q in %s field", item, fieldName),
		}
	}
	rangePart := parts[0]
	stepStr := parts[1]
	step, err := strconv.Atoi(stepStr)
	if err != nil || step <= 0 {
		return 0, 0, &ScheduleValidationError{
			Reason: fmt.Sprintf("invalid step value %q in %s field", stepStr, fieldName),
		}
	}
	_ = step // step > 0 is sufficient; value is not range-checked further

	switch {
	case rangePart == "*":
		return -1, -1, nil
	case strings.Contains(rangePart, "-"):
		return parseRange(rangePart, fieldName)
	default:
		val, err := strconv.Atoi(rangePart)
		if err != nil {
			return 0, 0, &ScheduleValidationError{
				Reason: fmt.Sprintf("invalid value %q in %s field", rangePart, fieldName),
			}
		}
		return val, val, nil
	}
}

// parseRange parses a "a-b" range string and returns the bounds.
func parseRange(item, fieldName string) (int, int, error) {
	parts := strings.SplitN(item, "-", 2)
	if len(parts) != 2 {
		return 0, 0, &ScheduleValidationError{
			Reason: fmt.Sprintf("invalid range %q in %s field", item, fieldName),
		}
	}
	low, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, &ScheduleValidationError{
			Reason: fmt.Sprintf("invalid range lower bound %q in %s field", parts[0], fieldName),
		}
	}
	high, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, &ScheduleValidationError{
			Reason: fmt.Sprintf("invalid range upper bound %q in %s field", parts[1], fieldName),
		}
	}
	return low, high, nil
}

// printUsage writes the full CLI help message -- a one-line synopsis
// followed by the registered flag list -- to out. It is used both as
// fs.Usage (invoked automatically by flag.FlagSet.Parse on a parse error)
// and explicitly for -h/--help, so the flag list shown always reflects the
// flags actually registered on fs rather than a hand-maintained duplicate.
func printUsage(fs *flag.FlagSet, out io.Writer) {
	_, _ = fmt.Fprintln(out, "Usage: bsky-cleaner --config <path> [--apply]")                            //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintln(out)                                                                             //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintln(out, "bsky-cleaner deletes posts older than a configured retention period from") //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintln(out, "a single Bluesky account. It runs in dry-run mode by default and only")    //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintln(out, "deletes posts when --apply is given.")                                     //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintln(out)                                                                             //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	_, _ = fmt.Fprintln(out, "Flags:")                                                                   //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	fs.SetOutput(out)
	fs.PrintDefaults()
}

// parseFlags parses args (excluding the program name) into a config path
// and the apply flag. It returns an error -- never calling os.Exit --
// whenever flag.FlagSet.Parse fails, --config/-c is missing, or unexpected
// positional arguments remain, so main's exit-code behavior stays testable
// without ending the test process. On -h/--help it returns flag.ErrHelp
// after writing the full help message (flag list included) to out.
func parseFlags(args []string, out io.Writer) (configPath string, apply bool, err error) {
	fs := flag.NewFlagSet("bsky-cleaner", flag.ContinueOnError)
	// Discard flag's own error+usage output: fs.Parse would otherwise write
	// the same error message that main prints via the returned err, so this
	// avoids printing it twice. fs.Usage below still fires and writes to
	// out regardless of this setting, since flag.FlagSet.usage bypasses
	// f.output() and calls fs.Usage directly.
	fs.SetOutput(io.Discard)
	fs.Usage = func() { printUsage(fs, out) }

	var help bool
	var showVersion bool
	fs.StringVar(&configPath, "config", "", "path to the TOML configuration file")
	fs.StringVar(&configPath, "c", "", "path to the TOML configuration file (shorthand for --config)")
	fs.BoolVar(&apply, "apply", false, "actually delete posts (default: dry-run)")
	fs.BoolVar(&help, "help", false, "show this help message and exit")
	fs.BoolVar(&help, "h", false, "show this help message and exit (shorthand for --help)")
	fs.BoolVar(&showVersion, "version", false, "print version information and exit")
	fs.BoolVar(&showVersion, "v", false, "print version information and exit (shorthand for --version)")

	// Scan for a help/version token before calling fs.Parse: fs.Parse returns
	// immediately on the first unknown/invalid flag, so if -h/--help appeared
	// alongside a malformed flag (e.g. "--help --unknown"), the help check
	// below would never be reached and the command would exit as a usage
	// error instead of showing help. Checking args directly here guarantees
	// -h/--help always succeeds regardless of other flags.
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fs.Usage()
			return "", false, flag.ErrHelp
		}
		if arg == "-v" || arg == "-version" || arg == "--version" {
			return "", false, errVersionRequested
		}
		if arg == "--" {
			// Everything after "--" is positional, not a flag.
			break
		}
	}

	if err := fs.Parse(args); err != nil {
		return "", false, err
	}

	if help {
		fs.Usage()
		return "", false, flag.ErrHelp
	}

	if showVersion {
		return "", false, errVersionRequested
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
		// report.FormatText already sanitizes each externally-sourced field
		// (rkey, error text) individually via internal/sanitize, so its own
		// structural newlines/spacing are not stripped here the way a
		// whole-string notify.Sanitize pass would.
		_, _ = fmt.Fprint(stdout, report.FormatText(*result)) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
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

// parsePrintScheduleFlags parses args (excluding the "print-schedule"
// subcommand name) into a config path. It returns an error when
// --config/-c is missing or unexpected positional arguments remain,
// consistent with parseFlags's error-return contract.
func parsePrintScheduleFlags(args []string) (configPath string, err error) {
	fs := flag.NewFlagSet("print-schedule", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&configPath, "config", "", "path to the TOML configuration file")
	fs.StringVar(&configPath, "c", "", "path to the TOML configuration file (shorthand for --config)")

	if err := fs.Parse(args); err != nil {
		return "", err
	}

	if fs.NArg() > 0 {
		return "", fmt.Errorf("unexpected positional argument(s): %v", fs.Args())
	}

	if configPath == "" {
		return "", fmt.Errorf("--config (or -c) is required")
	}

	return configPath, nil
}

// runPrintSchedule loads the TOML file at configPath, validates the
// schedule field as a cron expression, and writes it to stdout.
// Errors are written to stderr. config.Load is reused -- no second
// TOML parse.
func runPrintSchedule(configPath string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return exitSetupOrRunFail
	}

	if err := validateSchedule(cfg.Schedule); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		return exitSetupOrRunFail
	}

	_, _ = fmt.Fprintln(stdout, cfg.Schedule) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
	return exitOK
}

func main() {
	// print-schedule subcommand: hidden, used by entrypoint.sh for cron
	// integration. Must check len(os.Args) > 1 before indexing os.Args[1]
	// to avoid index out of range panic on no-argument invocation.
	if len(os.Args) > 1 && os.Args[1] == "print-schedule" {
		configPath, err := parsePrintScheduleFlags(os.Args[2:])
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
			os.Exit(exitUsageError)
		}
		os.Exit(runPrintSchedule(configPath, os.Stdout, os.Stderr))
	}

	configPath, apply, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// The full help message (flag list included) was already
			// written to os.Stderr by parseFlags/printUsage; avoid
			// printing the redundant "flag: help requested" error text.
			os.Exit(exitOK)
		}
		if errors.Is(err, errVersionRequested) {
			_, _ = fmt.Fprintln(os.Stdout, formatVersion()) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
			os.Exit(exitOK)
		}
		_, _ = fmt.Fprintln(os.Stderr, err.Error()) //nolint:gosec // stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
		os.Exit(exitUsageError)
	}

	now := time.Now()
	os.Exit(run(configPath, apply, now, http.DefaultClient, os.Stdout, os.Stderr))
}
