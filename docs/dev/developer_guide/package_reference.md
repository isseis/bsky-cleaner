# Package Structure Reference

This document provides a reference of the package structure in this codebase.
`mkplan.md` step 5 and `mkarch.md` point here when inspecting or designing
around existing packages — keep it in sync with `cmd/` and `internal/` as the
codebase grows.

## Directory Structure

```
- `cmd/`: Command-line entry points
  - `main.go`: placeholder only, no config loading logic yet (see docs/tasks/0004_cli_entrypoint)
- `internal/`: Core implementation
  - `config/`: reads and validates the TOML configuration file and environment variables, returning validated configuration values
  - `atproto/`: thin, self-written AT Protocol (XRPC) client for login, post listing, and post deletion (see docs/tasks/0002_atproto_client)
    - `testutil/`: `HTTPDoer` test double and lexicon-checked response fixtures for `internal/atproto`'s own tests
- `docs/`: Project documentation with requirements and architecture
```

## Package Responsibilities

**Configuration**

- `internal/config`: reads the TOML configuration file (`Load`) and secret credentials from the process environment (`LoadCredentials`), validates both (fail-closed on missing/out-of-range values), and combines them into `AppConfig` (`LoadAppConfig`) for callers that need both. See [Configuration Reference](../../design/configuration.md) for the full list of TOML fields and environment variables.

**AT Protocol Client**

- `internal/atproto`: resolves an account's DID and PDS endpoint (SSRF-guarded, `NewClient`), authenticates with an app password (`Client.Login`), lists the account's posts/reposts with type classification and pinned-post detection (`Client.ListPosts`), and deletes a post by rkey (`Client.DeleteRecord`). HTTP access is abstracted behind the `HTTPDoer` interface so all tests run without real network I/O; retries, dry-run/apply switching, and post-age/type filtering are out of scope and left to other packages (see docs/tasks/0002_atproto_client/01_requirements.md).

## Key Design Patterns

- **Separation of Concerns**: Each package has a single responsibility
- **Interface-based Design**: Use interfaces at package boundaries for testability
- **Error Handling**: Comprehensive error types and validation
