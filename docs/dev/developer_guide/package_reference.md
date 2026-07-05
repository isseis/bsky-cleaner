# Package Structure Reference

This document provides a reference of the package structure in this codebase.
`mkplan.md` step 5 and `mkarch.md` point here when inspecting or designing
around existing packages — keep it in sync with `cmd/` and `internal/` as the
codebase grows.

## Directory Structure

```
- `cmd/`: Command-line entry points
  - `main.go`: parses CLI flags (`--config`/`-c`, `--apply`) and wires `internal/config`/`internal/atproto`/`internal/runner`/`internal/report` into a runnable CLI; `internal/cleanup` is used inside `internal/runner`, not directly by `main.go` (see docs/tasks/0004_cli_entrypoint)
- `internal/`: Core implementation
  - `config/`: reads and validates the TOML configuration file and environment variables, returning validated configuration values
  - `atproto/`: thin, self-written AT Protocol (XRPC) client for login, post listing, and post deletion (see docs/tasks/0002_atproto_client)
    - `testutil/`: `HTTPDoer` test double and lexicon-checked response fixtures for `internal/atproto`'s own tests
  - `retry/`: generic `HTTPDoer` decorator that retries transient failures (transport errors, 429, 5xx) with bounded exponential backoff, with no dependency on internal/atproto (see docs/tasks/0005_retry_timeout)
  - `cleanup/`: filters an account's post inventory down to deletion targets based on retention days, post type, and pinned status (see docs/tasks/0003_cleanup_engine)
  - `runner/`: wires config/atproto/cleanup together into a single dry-run/apply run, producing a report.Result (see docs/tasks/0004_cli_entrypoint)
  - `report/`: structured run result (Result/Mode/DeleteFailure) and its stdout text rendering (FormatText), independent of how the result was produced (see docs/tasks/0004_cli_entrypoint)
- `docs/`: Project documentation with requirements and architecture
```

## Package Responsibilities

**Configuration**

- `internal/config`: reads the TOML configuration file (`Load`) and secret credentials from the process environment (`LoadCredentials`), validates both (fail-closed on missing/out-of-range values), and combines them into `AppConfig` (`LoadAppConfig`) for callers that need both. See [Configuration Reference](../../design/configuration.md) for the full list of TOML fields and environment variables.

**AT Protocol Client**

- `internal/atproto`: resolves an account's DID and PDS endpoint (SSRF-guarded, `NewClient`), authenticates with an app password (`Client.Login`), lists the account's posts/reposts with type classification and pinned-post detection (`Client.ListPosts`), and deletes a post by rkey (`Client.DeleteRecord`). HTTP access is abstracted behind the `HTTPDoer` interface so all tests run without real network I/O; dry-run/apply switching and post-age/type filtering are out of scope and left to other packages (see docs/tasks/0002_atproto_client/01_requirements.md); retrying transient HTTP failures is delegated to internal/retry, which NewClient wraps every outbound HTTPDoer (DID resolution and the post-validation PDS client) in before use (see docs/tasks/0005_retry_timeout/01_requirements.md).

**Retry**

- `internal/retry`: a generic `HTTPDoer` decorator (`Doer`) that retries transient failures (transport errors, HTTP 429, HTTP 5xx) with bounded exponential backoff, honoring a server's `Retry-After` header when positive and always capping the wait at `Policy.MaxDelay`. Never retries an error satisfying the unexported `permanentError` interface or a non-429 4xx status. Depends only on the standard library, so `internal/atproto` is the only consumer that imports it (see docs/tasks/0005_retry_timeout/01_requirements.md).

**Cleanup Engine**

- `internal/cleanup`: a pure function (`SelectDeletionTargets`) that filters an account's post inventory (`[]atproto.Post`) down to deletion targets, based on retention-day age (UTC), known post type, and pinned status. No network/file I/O, no dependency on the AT Protocol client itself (see docs/tasks/0003_cleanup_engine/01_requirements.md).

**Runner**

- `internal/runner`: performs one wiring pass (`Run`) -- login, list posts, judge deletion targets via `cleanup.SelectDeletionTargets`, and (apply mode only) delete each target, continuing past individual delete failures (a `Login`/`ListPosts` error still aborts the run immediately). Depends on `atproto` only through the package-local `Client` interface, so tests inject a fake instead of a real network client; `cleanup`, `config`, and `report` are used directly as concrete packages (see docs/tasks/0004_cli_entrypoint/01_requirements.md).

**Report**

- `internal/report`: the structured outcome of a run (`Result`/`Mode`/`DeleteFailure`), independent of how it is rendered, plus `FormatText` for stdout rendering. Reused as-is by a future Slack formatter (see docs/tasks/0006_slack_notification/01_requirements.md).

## Key Design Patterns

- **Separation of Concerns**: Each package has a single responsibility
- **Interface-based Design**: Use interfaces at package boundaries for testability
- **Error Handling**: Comprehensive error types and validation
