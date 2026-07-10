# bsky-cleaner Project Overview

- Created: 2026-07-02
- Last updated: 2026-07-02
- Status: Draft

## Overview

`bsky-cleaner` is a CLI tool written in Go that periodically cleans up posts from a Bluesky (bsky) account. It automatically deletes posts older than a configured number of days.

## Background / Purpose

Continued posting on Bluesky leads to an accumulation of past posts. This tool addresses the need to periodically delete posts older than a certain period, keeping the account focused on "current content."

## Scope

### In Scope

- On a single run, targets a single Bluesky account and deletes posts older than the configured number of days
- Scheduled execution equivalent to cron (built into the Docker image)
- Slack notification of execution results

### Out of Scope (Non-goals)

- Batch management of multiple accounts
- Web UI / GUI (provided only as a CLI tool)
- Post archiving or backup functionality (this tool assumes deletion; no means of restoring deleted posts is provided)
- Post editing or modification
- Support for social networks other than Bluesky

## Glossary

| Term | Description |
|------|-------------|
| AT Protocol | The federation (decentralized) social networking protocol adopted by Bluesky |
| PDS (Personal Data Server) | The server that actually holds user data (posts, etc.) in AT Protocol. Not necessarily `bsky.social`; it may differ per user |
| DID (Decentralized Identifier) | A persistent identifier for an account in AT Protocol. Resolving a DID reveals the location of the account's PDS, among other information |
| rkey (record key) | A key that uniquely identifies an AT Protocol record (such as a post). Specified when deleting a post |
| AT-URI | A URI format pointing to a record on AT Protocol (e.g., `at://did:plc:xxxx/app.bsky.feed.post/yyyy`) |
| XRPC | An HTTP-based RPC method adopted by AT Protocol. The protocol used by this tool to communicate with Bluesky |
| app password | Authentication credentials that can be issued separately from a Bluesky account's regular password, intended for external application integration |
| fail-closed | A design principle that, when an error or anomaly is detected, favors the safe side (stopping/failing) rather than "continuing anyway." In this tool, validation errors cause startup or sending to fail rather than continuing with incomplete processing |

## Assumptions and Constraints

### Assumptions

- The target Bluesky account and an app password for that account must have already been issued by the user before using this tool

### Constraints

- **Language**: Go 1.26.2 or later (see `go.mod`)
- **Target account**: A single run targets only one account. To manage multiple accounts, prepare a set of TOML configuration files and environment variables (`.env`) for each account, and run the tool as multiple processes (or multiple Docker containers)
- **Sensitive information**: The Bluesky app password and Slack Webhook URLs (for normal and error cases) are obtained from environment variables (sensitive information is not written in configuration files). Slack Webhook URLs are treated as sensitive information just like app passwords, since anyone who knows the value can post to the corresponding channel
- **Configuration file format**: TOML
- **Minimize external dependencies**: To keep maintenance costs low and ensure stable operation over time, dependencies on external libraries and tools are kept to the necessary minimum. Do not replace functionality that can be adequately implemented in-house (e.g., a thin XRPC client) with a large dependency. When adding a dependency, weigh the implementation cost it reduces against the maintenance burden it adds (version tracking, vulnerability response, license management, etc.)

## Development and Distribution Strategy

Ease of deployment is prioritized, using the following two-tier structure.

- **The binary itself**: Designed purely as a "one-shot CLI." It does not include daemonization or scheduling functionality. During development, build, run, and test directly without Docker (maintaining a simple development cycle)
- **Distribution format**: Packaged as a Docker image at release time. The image includes a lightweight cron, allowing the container to act as a built-in scheduler. Users only need to prepare environment variables (sensitive information) and a TOML configuration file (volume mount) in `docker-compose.yml` to get started
- **Centralized configuration**: All settings including the execution schedule and execution timeout are centralized in TOML. Only sensitive information is separated into `.env`, which is referenced via variables in `docker-compose.yml`

For implementation-level details (such as git management policy for `.env`, how TOML schedule settings are bridged to the built-in cron), see [Docker Deployment Design Details](design/docker_deployment.md).

## Deletion Specifications

### Posts to Delete

The following are included as deletion targets.

- Regular posts (original posts)
- Replies
- Reposts
- Quote posts

### Posts Not to Delete

- Pinned posts: Excluded from deletion targets even if they meet the age condition

### Criteria for Determining Deletion

- The configuration file specifies the age threshold (in days, as an integer) for posts to be deleted
- The elapsed days are calculated based on the post's creation time (`createdAt`) in **UTC**

## Safety Policy

Since deletion is an irreversible operation, the following basic policies apply.

- **Default is dry-run**: Unless an explicit flag (e.g., `--apply`) is specified, actual deletion is not performed; only the "list of posts to delete" is displayed
- **No upper limit on deletion count**: All posts matching the condition are targeted for deletion. However, guardrails to prevent unintentional mass deletion due to configuration mistakes (e.g., `retention_days=0`), such as a confirmation prompt when the count exceeds a threshold or a `--max-delete` flag, are left as future considerations

No deletion logs are written to files. Since execution results can be confirmed via Slack notification (described below), separate log recording is deemed unnecessary.

## Retry Policy

If an API call results in an error, it is retried several times with exponential backoff. The defaults are a maximum of 5 retries, an initial backoff of 1 second, and a maximum backoff of 30 seconds. The worst-case wait time for a single API call that encounters persistent temporary errors is approximately 31 seconds. See the [Configuration Reference](design/configuration.md#notes-on-setting-execution_timeout_seconds) for parameter details.

The cumulative retry time is designed to stay within the execution timeout (described below) that is set as a measure against concurrent executions. When deleting a large number of posts, the per-post retries can accumulate and potentially exceed the timeout; therefore, the timeout value should be determined taking the worst-case retry time into account.

## Execution Result Notification

Execution results are notified via Slack.

- Normal and error cases can be posted to different Slack channels (operating with both using the same channel is also possible)
- The Webhook URLs for normal and error notifications are assumed to match the host part (domain) of the allowed host specified in the configuration file (e.g., `hooks.slack.com`)
- If the host part does not match, the system is fail-closed. That is, if a mismatch is detected, the tool does not proceed with sending the notification; instead, it treats the mismatch as a configuration error and fails to start (see [Slack Notification Security Design](design/slack_notification_security.md) for details)

Notification delivery is best-effort and does not guarantee at-least-once
delivery. If notification sending fails for any reason (including a process
crash after runner.Run() completes), the run's result -- in particular an
already-completed deletion -- may never be reported via Slack.

### Information Leakage Protection in Slack Notifications

Measures are taken to prevent sensitive information from inadvertently being included in Slack notifications. For specific implementation details, see the [Slack Notification Security Design](design/slack_notification_security.md).

## Security Considerations

The main risk categories are as follows. For implementation-level details, see the [Security Design](design/security.md).

- **Injection via post body**: Including post body text directly in Slack notifications or console output can lead to mention spreading, ANSI escape injection, and log forgery
- **SSRF (PDS endpoint spoofing)**: Flaws in the DID resolution logic could send authentication credentials to unintended hosts
- **Sensitive information leakage**: Risk of `Authorization` headers or similar data leaking into Slack or panic logs when included in error objects
- **Self-inflicted rate limiting from excessive retries**: Without a backoff cap on retries, this could trigger rate limits or account restrictions on the Bluesky side
- **Concurrent execution**: Risk of overlapping cron invocations during processing. Addressed with an execution timeout
- **Configuration tampering**: Risk of deleting all posts due to tampering with `retention_days`
- **Docker supply chain**: Base images are pinned by digest, not by tag

## APIs Used

Bluesky is built on the AT Protocol. This tool uses the following AT Protocol XRPC endpoints.

- `com.atproto.server.createSession`: Login (session acquisition) using an app password
- `com.atproto.repo.listRecords`: Retrieve a list of one's own posts
- `com.atproto.repo.deleteRecord`: Delete a post

In addition, the following are used for DID and PDS endpoint resolution.

- Handle resolution: First tries DNS TXT records (`_atproto.<handle>` with `did=...`); on failure, falls back to HTTPS well-known (`https://{handle}/.well-known/atproto-did`)
- DID document resolution: For `did:plc`, references the PLC directory (`https://plc.directory/{did}`); for `did:web`, references `.well-known/did.json` (or `did.json` with a path) on the relevant domain

In line with the "minimize external dependencies" policy, the official `bluesky-social/indigo` Go client is not used; instead, a thin custom wrapper (`internal/atproto`) that directly calls XRPC is used. This is because the number of endpoints this tool uses (login, list retrieval, deletion, DID/PDS resolution) is limited, and the cost of importing the large `indigo` library in its entirety is not justified.

## References

- AT Protocol official documentation: https://atproto.com/
- Bluesky official Go client implementation (`indigo`): https://github.com/bluesky-social/indigo

## Definition of Done

The initial release of this project is considered complete when all of the following are satisfied.

- A dry-run display of the post list for a single account is possible
- When the `--apply` flag is specified, posts matching the condition (regular posts, replies, reposts, quote posts) are actually deleted, and pinned posts are excluded
- Execution results (both normal and error cases) are notified via Slack
- Distributed as a Docker image, and scheduled execution equivalent to cron is achievable simply by preparing a `docker-compose.yml`, a TOML configuration file, and `.env`

## Undetermined / Future Considerations

- Whether configuration file permission management and tamper detection are necessary
- Whether guardrails for having no upper limit on deletion count (such as a confirmation prompt when the count exceeds a threshold or a `--max-delete` flag) are necessary
