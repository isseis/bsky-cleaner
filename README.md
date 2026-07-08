# bsky-cleaner

A Go CLI tool that periodically cleans up a single Bluesky (AT Protocol) account by deleting posts older than a configured retention period.

## Prerequisites

- **Go 1.26.2** or later
- A Bluesky account with an [app password](https://bsky.app/settings/app-passwords)

## Installation

```sh
git clone https://github.com/isseis/bsky-cleaner.git
cd bsky-cleaner
make build
```

The binary is written to `build/bsky-cleaner`.

## Configuration

bsky-cleaner reads non-secret settings from a **TOML configuration file** and secrets from **environment variables**. Secrets are never written to the TOML file.

### TOML configuration file

Create a TOML file (e.g. `config.toml`):

```toml
retention_days = 30
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
```

| Field | Type | Required | Description |
|---|---|---|---|
| `retention_days` | int | yes | Delete posts older than this many days. Must be ≥ 1. |
| `execution_timeout_seconds` | int | yes | Maximum runtime in seconds (1–86400). |
| `schedule` | string | no | Cron expression. Only needed for Docker/cron deployments; omit when running directly or via system crontab. |
| `slack_allowed_host` | string | conditional | Required if any Slack webhook URL is set. Validates that webhook URLs point to this host (e.g. `hooks.slack.com`). |

See [configuration reference](docs/design/configuration.md) for full details.

### Environment variables

```sh
export BSKY_HANDLE=alice.bsky.social
export BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
export BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/...
export BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/...
```

| Variable | Required | Description |
|---|---|---|
| `BSKY_HANDLE` | yes | Bluesky handle (e.g. `alice.bsky.social`). |
| `BSKY_APP_PASSWORD` | yes | Bluesky app password. |
| `BSKY_SLACK_WEBHOOK_URL_SUCCESS` | no | Slack webhook for success notifications. |
| `BSKY_SLACK_WEBHOOK_URL_FAILURE` | no | Slack webhook for failure notifications. |

## Usage

```sh
# Dry-run: list which posts would be deleted (no actual deletion)
bsky-cleaner --config config.toml

# Apply: actually delete the posts
bsky-cleaner --config config.toml --apply

# Print version information
bsky-cleaner --version
# Example output: v1.2.3 (a1b2c3d)

# Print the cron schedule (for Docker/cron deployments)
bsky-cleaner print-schedule --config config.toml
```

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success — all targeted posts deleted (or dry-run completed). |
| `1` | Setup or run failure — config error, login failure, network error, etc. |
| `2` | Usage error — missing `--config`, unknown flag, extra positional arguments. |
| `3` | Partial failure — some posts were deleted, but at least one deletion failed. |

### Scheduling with cron

To run periodically, register the binary in your system crontab (without the `schedule` TOML field):

```cron
0 3 * * * BSKY_HANDLE=alice.bsky.social BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx /path/to/bsky-cleaner --apply --config /path/to/config.toml
```

## Docker

A pre-built Docker image is available on [GHCR](https://ghcr.io/isseis/bsky-cleaner).

### Prerequisites

- Docker Compose (v2 or later)
- A `.env` file (copy from `dot.env.example` and fill in real values)
- A TOML configuration file (e.g. `config/config.toml`)

### Quick start

```sh
# 1. Prepare the environment file
cp dot.env.example .env
# Edit .env with your Bluesky handle and app password

# 2. Create a config directory with your TOML file
mkdir -p config
# Create config/config.toml (see Configuration section above)

# 3. Check the version tag in docker-compose.yml and update if needed
#    (the default points to the latest stable release)

# 4. Pull the image and start the container
docker compose pull
docker compose up -d
```

The container runs on the schedule defined in your TOML file's `schedule` field. To verify it is running:

```sh
docker compose ps
docker compose logs
```

### Upgrading

1. Edit `docker-compose.yml` and bump the version tag in the `image:` line
2. Run `docker compose pull` to fetch the new image
3. Run `docker compose up -d` to restart with the new image

## Directory structure

```
cmd/                CLI entry point (main)
internal/
  atproto/          AT Protocol XRPC client (login, list posts, delete records)
  cleanup/          Post deletion eligibility logic
  config/           TOML and environment variable parsing / validation
  notify/           Slack notification delivery
  report/           Run result formatting (text output)
  retry/            Exponential backoff retry helper
  runner/           Wires atproto/cleanup/report into a single pass
  sanitize/         Output sanitization (ANSI escape / newline stripping)
docs/               Design documents and developer guides
```

## Safety

- **Dry-run by default** — no posts are deleted unless `--apply` is passed.
- **Fail-closed** — invalid configuration (e.g. `retention_days = 0`, mismatched Slack webhook host) causes startup failure rather than proceeding with defaults.
- **Secret redaction** — app passwords and webhook URLs are wrapped in `SecretString` and rendered as `[REDACTED]` in logs and error messages.
- **Pinned posts are excluded** from deletion.

See [security design](docs/design/security.md) for details.

## Development

```sh
make fmt       # Format all Go files
make test      # Run all tests
make lint      # Run golangci-lint
make build     # Build the binary
make clean     # Remove build artifacts
make deadcode  # Detect unreachable code
```

See [CLAUDE.md](CLAUDE.md) for project conventions and [docs/dev/developer_guide/](docs/dev/developer_guide/) for developer documentation.

## License

[MIT](LICENSE)
