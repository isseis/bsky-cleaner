# Building From Source

This is for developers who want to build `bsky-cleaner` from source (e.g. to
test an unreleased change). End users who just want to run the tool should
use the Docker Compose or prebuilt-binary instructions in the top-level
[README](../../../README.md) instead.

## Prerequisites

- **Go 1.26.2** or later (see `go.mod` for the exact version)
- A Bluesky account with an [app password](https://bsky.app/settings/app-passwords)

## Clone and build

```sh
git clone https://github.com/isseis/bsky-cleaner.git
cd bsky-cleaner
make build
```

The binary is written to `build/bsky-cleaner`.

## Other Make targets

```sh
make fmt       # Format all Go files
make test      # Run all tests
make lint      # Run golangci-lint
make deadcode  # Detect unreachable code
make clean     # Remove build artifacts
```

See [CLAUDE.md](../../../CLAUDE.md) for project conventions and
[Package Reference](package_reference.md) for the package layout.
