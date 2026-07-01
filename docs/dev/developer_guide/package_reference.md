# Package Structure Reference

This document provides a reference of the package structure in this codebase.
`mkplan.md` step 5 and `mkarch.md` point here when inspecting or designing
around existing packages — keep it in sync with `cmd/` and `internal/` as the
codebase grows.

## Directory Structure

No packages exist yet — this project has not started implementation. When the
first package is added under `cmd/` or `internal/`, replace this section with
an actual directory listing (see the reference format below) and keep it
updated on every architecture change.

Expected format once populated:

```
- `cmd/`: Command-line entry points
  - `<binary>/`: <one-line purpose>
- `internal/`: Core implementation
  - `<package>/`: <one-line purpose>
    - `<subpackage>/`: <one-line purpose>
- `docs/`: Project documentation with requirements and architecture
```

## Package Responsibilities

Once packages exist, list each under a short category heading (e.g. "AT
Protocol client", "Cleanup engine", "CLI"), one bullet per package, with a
one-line description of its responsibility — mirroring the Directory
Structure section above so both stay consistent.

## Key Design Patterns

- **Separation of Concerns**: Each package has a single responsibility
- **Interface-based Design**: Use interfaces at package boundaries for testability
- **Error Handling**: Comprehensive error types and validation
