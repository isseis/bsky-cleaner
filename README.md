English | [Japanese](README.ja.md)

# bsky-cleaner

A Go CLI tool that periodically cleans up [Bluesky](https://bsky.app/) (AT Protocol) accounts.
It deletes posts older than the configured retention period.

## Prerequisites

- A Bluesky account [app password](https://bsky.app/settings/app-passwords)
- Docker Compose (v2 or later) ※ if using the Docker Compose execution method

## Installation and Execution (Docker Compose)

Docker images are distributed via [GHCR](https://ghcr.io/isseis/bsky-cleaner),
and using Docker Compose is the standard usage method.

### 1. Prepare an environment variable file

```sh
curl -O https://raw.githubusercontent.com/isseis/bsky-cleaner/main/dot.env.example
cp dot.env.example .env
```

Edit `.env` to set the following sensitive information. Replace `BSKY_HANDLE` and `BSKY_APP_PASSWORD`
with your own Bluesky account values, not the example ones.

```sh
BSKY_HANDLE=alice.bsky.social      # replace with your handle
BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx  # replace with your app password
# Only needed if you use Slack notifications (optional)
BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/...
BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/...
```

`BSKY_APP_PASSWORD` is not your regular login password — use a dedicated one issued at the
[app password issuance page](https://bsky.app/settings/app-passwords).

See [Environment Variables](#environment-variables) below for details on each variable.

### 2. Prepare a configuration file (TOML)

```sh
mkdir -p config
```

Create `config/config.toml`.

```toml
retention_days = 30
schedule = "0 3 * * *"  # Runs daily at 3:00 (order: minute hour day month weekday; * means "not specified = every")
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"  # Only needed if you use Slack notifications
```

See [TOML Configuration File](#toml-configuration-file) below for details on each field.

### 3. Prepare docker-compose.yml

```sh
curl -O https://raw.githubusercontent.com/isseis/bsky-cleaner/main/docker-compose.yml
```

Check and adjust the version tag in `image:` to match the release version you want to use.

### 4. Start the container

```sh
docker compose pull
docker compose up -d
```

The container runs periodically according to the `schedule` field in the TOML configuration file.
Use the following commands to verify that it started successfully.

```sh
docker compose ps
docker compose logs
```

### 5. Verify the configuration (dry run)

Waiting for periodic execution via `schedule` means a configuration mistake might not be noticed
until the next day at the earliest. By invoking the binary inside the container directly, you can
do a dry run on the spot without waiting for `schedule`.

```sh
docker compose exec bsky-cleaner /usr/local/bin/bsky-cleaner --config /config/config.toml
```

Since this is a dry run, posts are not deleted. If login, configuration loading, and listing of
posts to be deleted all complete without error, `.env` and `config.toml` are configured correctly.

### Upgrading

1. Edit `docker-compose.yml` and bump the version tag in `image:`
2. Run `docker compose pull` to fetch the new image
3. Run `docker compose up -d` to restart with the new image

## Installation and Execution (Pre-built Binary)

If you want to run directly on the host without Docker, you can download pre-built binaries
(`linux/amd64`) from [GitHub Releases](https://github.com/isseis/bsky-cleaner/releases).

```sh
# Replace vX.Y.Z with the release version you want to use
curl -LO https://github.com/isseis/bsky-cleaner/releases/download/vX.Y.Z/bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz
curl -LO https://github.com/isseis/bsky-cleaner/releases/download/vX.Y.Z/SHA256SUMS

# Verify the checksum
sha256sum -c SHA256SUMS

tar xzf bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz
```

Run the extracted `bsky-cleaner` binary according to [Usage](#usage) below.
For periodic execution, use the system's cron (see [Scheduling with cron](#scheduling-with-cron)).

Currently, only `linux/amd64` binaries are distributed. macOS/Windows binaries are not provided.

If you want to build from source, see [Building from Source](docs/dev/developer_guide/build_from_source.md).

## Configuration

bsky-cleaner reads non-sensitive settings from a **TOML configuration file** and sensitive information
from **environment variables**. Sensitive information is never written to the TOML file.

### TOML Configuration File

Create a TOML file (e.g., `config.toml`).

```toml
retention_days = 30
schedule = "0 3 * * *"  # Runs daily at 3:00
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `retention_days` | int | Yes | Deletes posts older than this number of days. Must be 1 or greater |
| `execution_timeout_seconds` | int | Yes | Maximum execution time (seconds). 1–86400 |
| `schedule` | string | No | Specify the time for periodic execution in [cron format](https://en.wikipedia.org/wiki/Cron#Overview) (5 fields: `minute hour day month weekday`; `*` means "not specified". Example: `0 3 * * *` = daily at 3:00). Only required when using scheduled execution via Docker/cron. Omit when using direct execution or system crontab |
| `slack_allowed_host` | string | Conditional | Required when setting a Slack webhook URL. Validates that the webhook URL points to this host (e.g., `hooks.slack.com`) |

See [Configuration Reference](docs/design/configuration.md) for details.

### Environment Variables

Replace `BSKY_HANDLE` and `BSKY_APP_PASSWORD` with your own Bluesky account values, not the example
ones.

```sh
export BSKY_HANDLE=alice.bsky.social      # replace with your handle
export BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx  # replace with your app password
# Only needed if you use Slack notifications (optional)
export BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/...
export BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/...
```

| Variable | Required | Description |
|----------|----------|-------------|
| `BSKY_HANDLE` | Yes | Bluesky handle (e.g., `alice.bsky.social`) |
| `BSKY_APP_PASSWORD` | Yes | Bluesky app password. Not your regular login password — use a dedicated one issued at the [app password issuance page](https://bsky.app/settings/app-passwords) |
| `BSKY_SLACK_WEBHOOK_URL_SUCCESS` | No | Slack webhook for success notifications |
| `BSKY_SLACK_WEBHOOK_URL_FAILURE` | No | Slack webhook for failure notifications |

### Verify the configuration (dry run)

If you're not using Docker, once the TOML configuration file and environment variables are ready,
start by doing a dry run to confirm the configuration is correct.

```sh
./bsky-cleaner --config config.toml
```

Since this is a dry run, posts are not deleted. If login, configuration loading, and listing of
posts to be deleted all complete without error, the configuration is set up correctly.

## Usage

```sh
# Dry run: list posts that would be deleted without actually deleting them
bsky-cleaner --config config.toml

# Apply: actually delete posts
bsky-cleaner --config config.toml --apply

# Display version information
bsky-cleaner --version
# Example output: v1.2.3 (a1b2c3d)

# Display the cron schedule (for scheduled execution via Docker/cron)
bsky-cleaner print-schedule --config config.toml
```

### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success — all target posts were deleted (or dry run completed) |
| `1` | Setup/execution failure — configuration error, login failure, network error, etc. |
| `2` | Usage error — missing `--config`, unknown flag, extra positional argument, etc. |
| `3` | Partial failure — some posts were deleted but at least one deletion failed |

### Scheduling with cron

If you want to run periodic execution on the host without Docker, register the binary in the
system's crontab (do not use the TOML `schedule` field).

Writing sensitive information directly into the crontab entry can cause cron to log the entire
command line (including the environment variable values) to syslog when it runs the job,
unintentionally leaving the sensitive information in the logs. To avoid this, write the sensitive
information to a file in `export VAR=VALUE` format (e.g. `cron.env`; use a different name from the
Docker Compose version's `.env`, since the format differs), restrict its permissions to `600`, and
have the crontab entry only read it.

`cron.env` (example; replace `BSKY_HANDLE` and `BSKY_APP_PASSWORD` with your own values):

```sh
export BSKY_HANDLE=alice.bsky.social
export BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
# Set the following only if you use Slack notifications (optional)
export BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/...
export BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/...
```

```sh
chmod 600 /path/to/cron.env
```

A crontab entry is written in the format `minute hour day month weekday command` (`*` means "not
specified = every"). In the example below, `0 3 * * *` means "run daily at 3:00".

```cron
0 3 * * * . /path/to/cron.env && /path/to/bsky-cleaner --apply --config /path/to/config.toml
```

Running `crontab -e` opens an editor; add the line above and save.

## Safety

- **Dry run by default** — posts are not deleted unless `--apply` is specified
- **Fail-closed** — invalid configuration (e.g., `retention_days = 0`, Slack webhook host mismatch)
  does not fall back to default values; it fails at startup
- **Sensitive information masking** — app passwords and webhook URLs are
  displayed as `[REDACTED]` in logs and error messages
- **Pinned posts are excluded from deletion targets**

See [Security Design](docs/design/security.md) for details.

## Developer Information

- **GitHub repository**: [https://github.com/isseis/bsky-cleaner](https://github.com/isseis/bsky-cleaner)
- **Source code retrieval and build**: [Building from Source](docs/dev/developer_guide/build_from_source.md)
- **Project conventions**: [CLAUDE.md](CLAUDE.md)
- **Developer documentation**: [docs/dev/developer_guide/](docs/dev/developer_guide/)

## License

[MIT](LICENSE)
