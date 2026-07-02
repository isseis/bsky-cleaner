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
- `docs/`: Project documentation with requirements and architecture
```

## Package Responsibilities

**Configuration**

- `internal/config`: reads the TOML configuration file (`Load`) and secret credentials from the process environment (`LoadCredentials`), validates both (fail-closed on missing/out-of-range values), and combines them into `AppConfig` (`LoadAppConfig`) for callers that need both. See [Configuration Reference](../../design/configuration.md) for the full list of TOML fields and environment variables.

## Key Design Patterns

- **Separation of Concerns**: Each package has a single responsibility
- **Interface-based Design**: Use interfaces at package boundaries for testability
- **Error Handling**: Comprehensive error types and validation
