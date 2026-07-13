English | [Japanese](docker_deployment.ja.md)

# Docker Deployment Detailed Design

- Created: 2026-07-02
- Last updated: 2026-07-12
- Status: Final
- Related documents: [Project Overview](../overview.md)

## Positioning

This document describes the implementation-level details of the Docker deployment strategy described in the "Development and Distribution Strategy" section of the [Project Overview](../overview.md). For the strategy-level decisions (why this configuration was chosen), refer to the overview; this document only covers "how to implement it."

## Sensitive Information Management (`.env`)

Sensitive information should not be written directly in `docker-compose.yml`; instead, reference values from `.env` through Compose's variable expansion feature.

> **Difference between `.env` and `env_file`**: In Compose, the `.env` file is exclusively for variable expansion in `docker-compose.yml` (e.g., `${BSKY_APP_PASSWORD}`) and does not automatically populate container environment variables. To pass variables to containers, explicitly specify them with the `environment:` key, such as `BSKY_APP_PASSWORD: ${BSKY_APP_PASSWORD}`. The `env_file:` directive is a separate feature that passes all variables in a file to the container; this design does not use it.

- The `.env` file contains the actual app password and other sensitive values
- The handling of `.env` can be selected from the following options depending on the operational needs:
  - **Encrypt only `.env` with `git-crypt`**: Set `.env filter=git-crypt diff=git-crypt` in `.gitattributes`. Files that do not contain sensitive information, such as `docker-compose.yml` and TOML configuration, can be version-controlled and reviewed in plain text, while only sensitive information can be securely shared within the repository. Suitable for scenarios where `.env` itself needs to be version-controlled and shared across multiple people and environments
  - **Add `.env` to `.gitignore` and exclude it from version control**: The simplest and least error-prone approach, but sharing `.env` itself must be handled outside of git (e.g., password manager)
- `dot.env.example` (with empty/dummy values) is committed as usual in either approach, serving as a guide for initial setup

## Schedule Configuration and Built-in Cron Integration

The execution schedule is centralized in TOML, like other settings (not exposed as environment variables in `docker-compose.yml`). TOML is a configuration file that can only be read at container startup and is separate from the crontab definition file that the built-in cron (supercronic) reads. Therefore, the TOML configuration values are bridged to the crontab using the following procedure:

1. The binary provides a hidden subcommand `print-schedule` that extracts only the schedule field (e.g., `schedule = "0 3 * * *"`) from TOML and outputs it to stdout. TOML parsing is consolidated in this subcommand to avoid double implementation. Before output, it verifies that the value is a "single-line cron expression without line breaks" and exits with a non-zero status if the value is invalid (writing a value containing line breaks directly into a crontab could inject arbitrary commands as additional crontab lines)
2. The entry point script bundled with the image calls this subcommand at container startup and dynamically generates a crontab file for `supercronic` using the result
3. The built-in cron is started with `exec supercronic <generated crontab>`, and the main program (`bsky-cleaner --apply --config ...`) is executed periodically according to that schedule

With this approach, the configuration remains centralized in TOML, and the user only needs to start the Docker image; no host-side cron configuration is required.

## Release

Releases are automated by CI (`.github/workflows/release.yml`). The operations performed by the developer are as follows.

### Normal Release

`docker-compose.yml` and `README.md`/`README.ja.md` each embed the release version (the `image:` tag and the
`VERSION=` line in the Docker Compose setup steps, respectively). Before tagging, bump all of them together with
`go run ./scripts/bump_release_version` rather than editing each file by hand — a manual edit is easy to miss in one of
the three files, which would leave the README's setup instructions pointing at a different version than the image
actually built for the tag.

This repository does not allow direct pushes/merges to `main` — every change lands through a reviewed PR. The
version bump is no exception: commit it on a release branch, get it merged, and only then tag the commit that is
now on `main`. Tagging before merge (e.g. tagging a commit that only exists on the release branch) would let CI
build and publish a release from a change that has not gone through review yet.

First, set `VERSION` to the actual release number. Unlike the command sequence below, this line requires editing and cannot be run as written.

```sh
VERSION=vX.Y.Z
```

The commands below can then be run as written, without further changes.

```sh
git checkout -b release-$VERSION
go run ./scripts/bump_release_version $VERSION
# Review the diff, then:
git add docker-compose.yml README.md README.ja.md
git commit -m "release($VERSION): bump embedded version to $VERSION"
git push -u origin HEAD
gh pr create --title "release($VERSION): bump embedded version to $VERSION"

# After the PR is reviewed and merged into main:
git checkout main && git pull
git tag $VERSION
git push --tags
```

Pushing the tag triggers CI to automatically execute the following:

- Validate the semver format and check for duplicate existing tags
- Build the Docker image and push it to GHCR (4 tags: `latest`, `vX`, `vX.Y`, `vX.Y.Z`)
- Generate a binary archive (`bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz`) and SHA256 checksum
- Create a GitHub Release and attach the archive

### Manual Execution for Verification

The same workflow can be executed with `workflow_dispatch` by specifying an arbitrary tag. If the specified tag does not exist, checkout will fail. When triggered by `workflow_dispatch`, a GitHub Release is created in **draft** state. After verification, either delete it with `gh release delete <tag>` or publish it with `gh release edit <tag> --draft=false`.

### Checksum Verification After Download

```sh
# Place the downloaded bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz and SHA256SUMS in the same directory, then run:
sha256sum -c SHA256SUMS
# → If the output shows bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz: OK, the file has not been tampered with
```

See also the "Installation and Execution (Pre-built Executable)" section of `README.md` for details.

## GHCR Package Visibility Switching Procedure (First Time Only)

Immediately after publishing an image to GHCR for the first time, the package visibility is set to **private**. Since the `GITHUB_TOKEN` permissions cannot change the visibility, you must manually switch it to public once using the following procedure:

1. Open the GitHub repository page
2. Click `bsky-cleaner` in the "Packages" section on the right sidebar
3. Click "Package settings" on the package's top page
4. Scroll down to the "Danger Zone" section
5. Click "Change visibility" and select "public" in the confirmation dialog

This operation is only required once when the package is first created; it is not needed for subsequent releases.
