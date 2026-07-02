# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`bsky-cleaner` is a Go CLI tool that periodically cleans up a single Bluesky
(AT Protocol) account by deleting posts older than a configured retention
period. See [Project Overview](docs/overview.md) for the full scope,
terminology, deletion rules, safety policy, and non-goals.

**Current status**: early-stage — only project scaffolding and documentation
exist so far (`cmd/main.go` is a placeholder). Consult the task-specific
requirements/architecture/plan documents under `docs/tasks/` (see below) for
what is actually being implemented, rather than assuming any package layout.

## Quick Links

**Development Guides:**
- Requirements and Acceptance Criteria Process: [requirements_process.md](docs/dev/developer_guide/requirements_process.md) - Process for implementing new features
- Task Document Templates: [docs/tasks/0000_template/](docs/tasks/0000_template/) - Starting point for a new task's `01_requirements.md` / `02_architecture.md` / `03_implementation_plan.md`
- Task Directory Identification: [task_identification.md](docs/dev/developer_guide/task_identification.md) - How to resolve a task directory under `docs/tasks/`
- Test Organization Guide: [test_organization.md](docs/dev/developer_guide/test_organization.md) - Test helper file organization
- Mermaid Reference: [mermaid_reference.md](docs/dev/developer_guide/mermaid_reference.md) - Diagram conventions for design docs
- [Package Reference](docs/dev/developer_guide/package_reference.md) - Package structure (update as packages are added)

**Design Docs:**
- [Project Overview](docs/overview.md) - Scope, terminology, deletion rules, safety/retry/notification policy
- [Docker Deployment Design](docs/design/docker_deployment.md) - `.env` handling, TOML-to-cron bridging
- [Security Design](docs/design/security.md) - Implementation-level detail for the risk categories listed in the overview

## Documents

- Documents should be placed under `docs/`
- Default language is Japanese (exceptions: README.md, CLAUDE.md)
- Default format is markdown
  - Use Mermaid syntax for diagrams.
  - Follow the style and legend in [mermaid_reference.md](docs/dev/developer_guide/mermaid_reference.md).
  - Use a cylinder shape for "data" nodes instead of the default rectangle (in Mermaid flowcharts a cylinder node can be written as `[(data)]`).
  - **Node label quoting**: Always wrap node labels in double quotes if they contain special characters (parentheses, brackets, colons, slashes, etc.). Example: `A["label (with parens)"]`
  - **Line breaks in labels**: Use `<br>` for line breaks inside node labels, not `\n`. Example: `A["line1<br>line2"]`

### Translation Guidelines (Japanese to English)

When translating Japanese documentation to English:

1. **Translation Workflow**:
   - First create and commit the Japanese version
   - Then create the English version based on the Japanese original

2. **Translation Principles**:
   - **Accuracy over fluency**: Prioritize precise translation over natural-sounding English
   - **Faithful translation**: Do not delete content from the Japanese version or add content not present in the original
   - **Structural consistency**: Match chapter headings and sentence structure between Japanese and English versions

3. **Terminology Management**:
   - Use consistent terminology from [docs/translation_glossary.md](docs/translation_glossary.md)
   - Add new terms to the glossary as needed

## Commands

### Build Commands
- `make build` - Build the `bsky-cleaner` binary into `build/`
- `make clean` - Clean build artifacts

### Test Commands
- `make test` - Run all tests (`go test -tags test ./...`)

### Code Quality
- `make fmt` - Format all files with `gofmt`
- `make lint` - Run linter with golangci-lint
- `make deadcode` - Detect unreachable/dead code with `deadcode ./...`

## Architecture Overview

No implementation exists yet beyond scaffolding. Before writing code for a
feature, check whether a task directory under `docs/tasks/` already has an
**approved** `01_requirements.md` / `02_architecture.md` / `03_implementation_plan.md`
(see [Requirements Process Guide](docs/dev/developer_guide/requirements_process.md));
implementation must not start ahead of an approved plan.

Planned high-level shape (see [Project Overview](docs/overview.md) for
rationale):
- **CLI entry point** (`cmd/`): one-shot binary — dry-run by default, `--apply` to actually delete. No daemon/scheduler logic in the binary itself; scheduling is handled by cron inside the distributed Docker image, not by the Go program.
- **AT Protocol client**: thin, self-written XRPC wrapper limited to the few endpoints this tool needs (`createSession`, listing records, `deleteRecord`) rather than a full SDK — see external-dependency-minimization below.
- **Cleanup engine**: filters posts by `createdAt` age (UTC) and type (post/reply/repost/quote), excluding pinned posts.
- **Notification**: Slack webhook notification of run results (success/failure channels), with fail-closed webhook host validation.

Keep [Package Reference](docs/dev/developer_guide/package_reference.md) in
sync with `cmd/` and `internal/` as packages are actually added.

### Key Design Patterns
- **YAGNI**: Use a simple and clear approach to satisfy the requirement. Don't take a complex approach for not-yet-planned features.
- **DRY**: Don't repeat yourself. Before adding new code, check the codebase and prefer reusing existing implementations.
- **External-dependency minimization**: Prefer a self-written implementation (e.g. a thin XRPC client) over pulling in a large dependency (e.g. `indigo`) when only a small slice of its functionality is needed. When adding a dependency, weigh the implementation cost it saves against the maintenance burden it adds (version churn, vulnerability response, license review).
- **Fail-closed**: On error or unexpected state, stop/fail rather than continuing with a best-effort fallback — e.g. mismatched Slack webhook hosts must abort startup, not silently skip notification.
- **Separation of Concerns**: Each package has a single responsibility.
- **Interface-based Design**: Use interfaces at package boundaries for testability.

### Security Considerations

See [Security Design](docs/design/security.md) for implementation-level
detail. Headline risk categories (from [Project Overview](docs/overview.md)):
- Indirect injection via post body content (Slack notification / console output: mention spam, ANSI escape injection, log injection)
- SSRF via DID/PDS endpoint resolution
- Secret leakage through error objects (`Authorization` headers, session JWTs) reaching logs/notifications
- Runaway retries against Bluesky rate limits (bounded exponential backoff)
- Double-execution from cron overlap (mitigated via execution timeout, not lock files)
- Config tampering (e.g. `retention_days` set to 0 causing mass deletion)
- Docker supply-chain risk (pin base images by digest, not tag)

### Testing Strategy

See [Test Organization Guide](docs/dev/developer_guide/test_organization.md)
for the two-tier test-helper classification (`testutil/` vs package-level
`test_helpers.go`).
- **Error Testing**: Use `errors.Is()` / `errors.AsType[T]` to validate error types, not string matching on error messages.
- **Assertions**: Prefer the `github.com/stretchr/testify/assert` and `github.com/stretchr/testify/require` subpackages over hand-written `if ... { t.Errorf(...) }` / `t.Fatalf(...)` checks in new tests — it cuts boilerplate and gives more readable failure output. Use `require` for a failure that should abort the test immediately (e.g. `require.NoError` before using its result), and `assert` for independent checks that should all run even if one fails. Still use `errors.Is` / `errors.AsType[T]` (not `testify`) to unwrap and type-assert errors, then assert on the extracted fields with `testify`.

## Development Notes

- Uses Go modules with Go 1.26.2 (see `go.mod`)
- Security- and safety-focused codebase: destructive operations (post deletion) are irreversible, so validation and dry-run-by-default matter more than in a typical CLI
- Source-language rule: Go comments, identifiers, and string literals must be English (documentation remains Japanese by default — see Documents above)
- After editing Go files, run `make fmt` to format them
- After editing files, run `make test` and `make lint` and fix errors

## Modern Go Idioms (Go 1.21+)

When writing or modifying Go code in this repository, prefer the following modern idioms over older equivalents. These improve readability, reduce boilerplate, and leverage standard library improvements.

### Language Features
- Use `any` instead of `interface{}`.
- Use `for range n` (Go 1.22+) instead of `for i := 0; i < n; i++` when the index is unused or only counts iterations.
- Rely on per-iteration loop variable scope (Go 1.22+); do not write `i := i` shadowing inside loop bodies.
- Use range-over-function iterators (Go 1.23+) for custom traversal where appropriate.

### Built-in Functions
- Use `min(a, b)` / `max(a, b)` instead of hand-written comparisons or `math.Max`/`math.Min`.
- Use `clear(m)` to clear maps and slices instead of manual `for k := range m { delete(m, k) }`.

### Standard Library
- Use the `slices` package: `slices.Contains`, `slices.Index`, `slices.Sort`, `slices.SortFunc`, `slices.Equal`, `slices.Clone`, `slices.Concat`, `slices.Delete`, `slices.Insert`, etc., instead of explicit loops.
- Use the `maps` package: `maps.Keys`, `maps.Values`, `maps.Clone`, `maps.Equal`, `maps.Copy`.
- Use `cmp.Or(a, b, c)` to return the first non-zero value instead of chained `if x == zero { x = y }`.
- Use `cmp.Compare` for three-way comparisons, especially in `slices.SortFunc`.
- Use `errors.Join(err1, err2)` for combining multiple errors.
- Use `fmt.Errorf("...: %w", err)` for error wrapping.
- Use `strings.Cut` / `bytes.Cut` instead of `SplitN(s, sep, 2)`.
- Use `strings.CutPrefix` / `strings.CutSuffix` instead of `HasPrefix` + `TrimPrefix` combinations.
- Use `sync.OnceFunc` / `sync.OnceValue` / `sync.OnceValues` instead of `sync.Once` + closure boilerplate.
- Use `log/slog` for structured logging.
- Use `context.WithoutCancel` to detach cancellation propagation.
- Use `reflect.TypeFor[T]()` instead of `reflect.TypeOf((*T)(nil)).Elem()`.

### Generics
- Use type parameters (Go 1.18+) to consolidate duplicated `int`/`int64`/`float64` helpers.
- Prefer `slices.SortFunc` over `sort.Slice` for type-safe, faster sorting without reflection.

### Other Patterns
- Use `map[T]struct{}` instead of `map[T]bool` for set semantics (saves memory).
- Use `errors.Is` / `errors.AsType[T]` instead of string matching on error messages. Prefer `errors.AsType[T]` over `errors.As` — it eliminates the `var target T` declaration:
  ```go
  // Before
  var pathErr *fs.PathError
  if errors.As(err, &pathErr) { ... }

  // After
  if pathErr, ok := errors.AsType[*fs.PathError](err); ok { ... }
  ```
- In tests, use `t.Cleanup` instead of manual `defer` chains, and `t.TempDir` instead of `os.MkdirTemp` + `defer os.RemoveAll`.

## Requirements and Acceptance Criteria

When implementing new features or security-critical functionality, follow the process documented in [Requirements Process Guide](docs/dev/developer_guide/requirements_process.md).

**Quick summary:**
1. Create `01_requirements.md` with explicit acceptance criteria
2. Create `02_architecture.md` with high-level design (Mermaid diagrams)
3. Create `03_implementation_plan.md` with progress tracking (checkboxes) and AC traceability
4. Write tests for each acceptance criterion
5. Link tests to acceptance criteria in the implementation plan

Use [task_identification.md](docs/dev/developer_guide/task_identification.md)
to resolve which `docs/tasks/XXXX_feature/` directory a command should
operate on.

## Tool Execution Safety

**CRITICAL**
- Don't run the following commands without the user's explicit approval:
  - commands interacting with the network, e.g. `git pull`
  - merging pull requests on GitHub
- `git commit` and `git push` may be executed without explicit approval — `git push` here only pushes the current branch to this repository's own remote, not an arbitrary network operation, so it is an intentional exception to the network-interaction rule above
