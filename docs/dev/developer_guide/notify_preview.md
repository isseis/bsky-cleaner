# Slack Notification Preview Tool (notify-preview)

`internal/notify/notifypreview` is a developer tool for checking what a Slack
notification message looks like, without running the production
`bsky-cleaner --apply` pass that actually deletes posts from Bluesky.

## Purpose

The Slack notification body is formatted by `buildPayload` in
[`internal/notify/payload.go`](../../../internal/notify/payload.go) (see
docs/tasks/0006_slack_notification). When improving this format, it is
impractical to reproduce every run outcome pattern -- success, partial
failure, error -- by running a real production pass each time.

`notifypreview` pre-builds canned `report.Result` / `notify.Outcome` values
for those patterns (`fixtures.go`), and renders them through the same
formatting function the production code uses (`notify.BuildPayloadPreview`,
which just calls `buildPayload` internally) and the same send function
(`notify.Send`), so the preview always reflects the real output. Since it
does not reimplement any formatting or sending logic, there is no
divergence from production code.

It is gated behind the `//go:build test` build tag, so it is only built and
run via `go run -tags test ./internal/notify/notifypreview` (`make
notify-preview`, below), and is never included in the production binary
(`make build`'s output).

## Usage

### Print to stdout (no sending)

```sh
make notify-preview
```

Renders every available scenario in turn and prints it to stdout.

To view a single scenario:

```sh
make notify-preview ARGS="-scenario=partial-failure"
```

### Send to a real test Slack channel

Create an Incoming Webhook for a test channel in Slack, set its URL in the
`BSKY_SLACK_WEBHOOK_URL_TEST` environment variable, then run with `-send`:

```sh
export BSKY_SLACK_WEBHOOK_URL_TEST="https://hooks.slack.com/services/..."
make notify-preview-send
```

To send only a single scenario, pass `-scenario` via `ARGS`:

```sh
make notify-preview-send ARGS="-scenario=partial-failure"
```

- `BSKY_SLACK_WEBHOOK_URL_TEST` is a preview-only environment variable,
  independent of the production `BSKY_SLACK_WEBHOOK_URL_SUCCESS` /
  `BSKY_SLACK_WEBHOOK_URL_FAILURE`. Both success and failure scenarios are
  delivered to the same test channel (the preview tool does not route to
  different channels based on outcome).
- Passing `-send` without `BSKY_SLACK_WEBHOOK_URL_TEST` set exits with an
  error.
- Production's `slack_allowed_host` allowed-host validation only runs at
  config-load time (`internal/config`); this tool does not perform it. The
  URL used here is supplied by the developer themselves, not untrusted
  external input, so that check does not apply.

## Scenarios

The scenarios defined in `scenarios()` in `fixtures.go` cover every branch
of `buildPayload`.

| Scenario | Description |
| --- | --- |
| `success-empty` | Success, 0 deletions |
| `success-apply` | Success, multiple deletions |
| `partial-failure` | Success, 2 deleted / 1 failed (exercises failure-channel routing) |
| `run-error` | The run itself aborted with an error (no `report.Result`) |
| `truncation` | 200 delete failures stacked up, to reproduce the 4000-byte payload truncation |

## Related documents

- [Package Reference](package_reference.md) -- where `internal/notify/notifypreview`
  fits in the package layout
- docs/tasks/0006_slack_notification -- requirements/architecture for the
  production Slack notification feature
