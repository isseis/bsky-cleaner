English | [Japanese](configuration.ja.md)

# Configuration Reference

- Created: 2026-07-02
- Status: Draft
- Related documents: [Project Overview](../overview.md), [Configuration Management Task](../tasks/0001_config/01_requirements.md)

## Position

This document is the integrated reference for the configuration values loaded by the `internal/config` package (both TOML configuration files and environment variables). It describes the type, required/optional, default value, format and constraints, and example for each item. For implementation-level design decisions (why this structure was chosen), see the [Configuration Management Architecture Design](../tasks/0001_config/02_architecture.md).

## TOML Configuration

Read by `internal/config.Load(path)`. It operates with the following policies.

- **Required items**: All items except `slack_allowed_host` and `schedule`. If any of them is missing, loading fails (no implicit completion with default values for required items).
- **Optional items**: Default values are explicitly shown in the table below.
- **Handling of secrets**: App passwords and Slack Webhook URLs are not written in TOML; they are handled in the "Environment Variables" section below.
- **Reason for placing `slack_allowed_host` in TOML**: Unlike the Webhook URL itself, it does not have posting permissions on its own, so it is not treated as a secret (see [0006_slack_notification](../tasks/0006_slack_notification/01_requirements.md)).

| Item | Type | Required/Optional | Default Value | Format / Constraints |
|---|---|---|---|---|
| `retention_days` | integer | required | none | Positive integer (`1` or greater). `0` or below causes startup failure (fail-closed validation to prevent deletion of all posts immediately) |
| `schedule` | string | optional | `""` (when key omitted) | A cron-like schedule string. Only used by the `print-schedule` subcommand during Docker distribution (see [0007_docker_distribution](../tasks/0007_docker_distribution/01_requirements.md)), so not required when running directly or registering in crontab without Docker. This package itself only handles the presence or absence of the value; validation of the cron syntax is the responsibility of the `print-schedule` side |
| `execution_timeout_seconds` | integer | required | none | Specified in seconds. An integer in the range `1` to `86400` (24 hours). Values `0` or below, or exceeding `86400`, cause startup failure |
| `slack_allowed_host` | string | required if either `BSKY_SLACK_WEBHOOK_URL_SUCCESS` or `BSKY_SLACK_WEBHOOK_URL_FAILURE` is set | not set | The value allowed as the host part of the Slack Webhook URL (e.g., `hooks.slack.com`). If the host part (excluding port number, case-insensitive) of the configured Webhook URL does not match this value, startup fails (fail-closed). If both Webhook URLs are unset, startup does not fail even if this item is not set (see [0006_slack_notification](../tasks/0006_slack_notification/01_requirements.md) F-005) |

### Example

```toml
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
```

### Notes on Setting `execution_timeout_seconds`

`execution_timeout_seconds` should be set considering the worst-case time required for individual API calls such as fetching post lists and deleting posts, including retries. The following factors should be considered.

- **Worst-case wait time for retry policy**: With this tool's retry policy (default: maximum 5 retries, initial backoff 1 second, maximum backoff 30 seconds), the worst-case wait time when a single API call continuously encounters transient errors (429/5xx/timeout) is approximately 31 seconds (see [0005_retry_timeout Architecture Design](../tasks/0005_retry_timeout/02_architecture.md#34-f-002実行タイムアウトの充足状況とリトライポリシーの数値ac-05ac-07)).
- **Proportional to the number of posts to delete**: When there are many posts to delete, the above wait time can accumulate for each call. Therefore, it is recommended to set `execution_timeout_seconds` with a sufficient margin within a range sufficiently smaller than the schedule interval (`schedule`), taking into account the expected maximum number of posts.
- **Safety on timeout expiration**: If the execution timeout is reached, deletion calls in progress are forcibly terminated, but this does not cause data corruption (due to the idempotency of `DeleteRecord`; see [0002_atproto_client](../tasks/0002_atproto_client/01_requirements.md) AC-12).
- **Additional timeout budget for Slack notifications**: The Slack notification process (see [0006_slack_notification Architecture Design](../tasks/0006_slack_notification/02_architecture.md#35-副作用契約dry-run--apply-とネットワーク送信) Section 3.5) consumes an additional timeout budget independent of the deletion process (default: approximately 12 seconds: single HTTP timeout 3 seconds × maximum 3 attempts + backoff 1 second + 2 seconds). Consider this additional budget when evaluating the margin between `execution_timeout_seconds` and the schedule interval.

## Environment Variables

Read by `internal/config.LoadCredentials()`. It operates with the following policies.

- **Secrets**: All are treated as secrets and are not written in TOML.
- **Docker distribution**: Passed via `.env` (see [Docker Deployment Design](docker_deployment.md)).

| Variable Name | Type | Required/Optional | Default Value | Format / Constraints |
|---|---|---|---|---|
| `BSKY_HANDLE` | string | required | none | The handle of the Bluesky account (e.g., `alice.bsky.social`). Empty string or unset both cause startup failure |
| `BSKY_APP_PASSWORD` | string (secret) | required | none | The Bluesky app password. Empty string or unset both cause startup failure |
| `BSKY_SLACK_WEBHOOK_URL_SUCCESS` | string (secret, URL) | optional | not set (no notification to the corresponding channel) | Slack Incoming Webhook URL for success notifications. If set, only validates that the scheme is `https` and that it is a syntactically valid URL including the host part (no constraints on the path or query parameters). If unset, no error is raised and it is treated as a configuration that does not send notifications to that channel. Note that an unset environment variable and a variable set to an empty string are not distinguished; both are treated identically as "no notification" |
| `BSKY_SLACK_WEBHOOK_URL_FAILURE` | string (secret, URL) | optional | not set (no notification to the corresponding channel) | Slack Incoming Webhook URL for failure notifications. The constraints and behavior when unset are the same as `BSKY_SLACK_WEBHOOK_URL_SUCCESS` |

### Host Verification Timing

Verification that the host part of the Slack Webhook URL matches the TOML `slack_allowed_host` requires both the TOML (`Config`) and environment variables (`Credentials`). Therefore, it is performed after `LoadAppConfig()` reads both (see [0006_slack_notification](../tasks/0006_slack_notification/01_requirements.md) F-005).

### Example (`.env`)

```
BSKY_HANDLE=alice.bsky.social
BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/<workspace-id>/<channel-id>/<token>
BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/<workspace-id>/<channel-id>/<token>
```

## Handling of Secrets

`BSKY_APP_PASSWORD` and the Slack Webhook URLs (success and failure) are wrapped in the `internal/config.SecretString` type. This provides the following characteristics.

- **Protection in log output**: When formatted with `%v`/`%s`/`%#v` or used in structured logging via `log/slog`, they are replaced with the fixed string `[REDACTED]`.
- **Retrieving the value**: The actual value can only be obtained by calling `Reveal()`, and it is intended to be called only immediately before use, such as when constructing authentication requests or building Slack notification payloads.
- **`BSKY_HANDLE` is not applicable**: `BSKY_HANDLE` is an account identifier exposed publicly on Bluesky and is not a secret, so it is not wrapped in `SecretString`.
