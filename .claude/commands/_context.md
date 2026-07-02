# Project Context for Claude Commands

This file is the single source of truth for project-specific configuration values
the commands in this directory depend on. Commands reference the **names** defined
here (e.g. "the task root", "the build checks") so those values are configured in
one place rather than scattered across command files.

**To port these commands to another project:**
1. Edit this file — update all three sections (Process convention, Tech-stack
   convention, Domain-specific) for the new project.
2. Edit the command bodies for Domain-specific content — the illustrative examples
   embedded in `mkplan.md` and `runplan.md` (e.g. per-call request identifiers,
   session teardown, `FormatDeleteSummary`) are drawn from this project's domain
   and must be replaced or removed. See the "Domain-specific" section below for
   where each example lives.
3. Edit or replace the guide documents this file points to.

The values are grouped by the layer they belong to, so you can see at a glance
what changes under which condition:

- **Process convention** — changes only if the new project adopts a different
  requirements → architecture → plan → PR workflow.
- **Tech-stack convention** — changes only if the new project is not Go, or uses
  different build tooling / source layout.
- **Domain-specific** — always changes; specific to this project.

---

## Process convention

| Name | Value |
|---|---|
| Task root | `docs/tasks/` |
| Task identification guide | `docs/dev/developer_guide/task_identification.md` |
| Requirements process guide | `docs/dev/developer_guide/requirements_process.md` |
| Test organization guide | `docs/dev/developer_guide/test_organization.md` |
| Mermaid reference guide | `docs/dev/developer_guide/mermaid_reference.md` |
| Package reference guide | `docs/dev/developer_guide/package_reference.md` |
| Requirements document | `01_requirements.md` |
| Architecture document | `02_architecture.md` |
| Implementation plan document | `03_implementation_plan.md` |
| Document status values | `draft` → `approved` |
| Document language | Japanese |
| Translation glossary | `docs/translation_glossary.md` |
| Translation language pair | Japanese (primary) ⇄ English *(reference only — `mktrans.md` determines direction from file extension, not this value)* |

### PR marker conventions

PR boundary markers embedded in the implementation plan use these labels:

- Section heading: `### PR-N 作成ポイント: <scope label in English>`
- Fields: `**対象ステップ**`, `**推奨タイトル**`, `**レビュー観点**`
- Conventional commit scope format: `feat(<task-id>): <concise English title>`

---

## Tech-stack convention

| Name | Value |
|---|---|
| Build checks (run after edits) | `make fmt` (Go only) → `make test` → `make lint` |
| Dead-code check | `make deadcode` |
| Green gate (must pass before PR) | `make test && make lint` |
| Source layout | `cmd/`, `internal/` |
| Cross-package test helpers | `testutil/` |
| Package-internal test helpers | `test_helpers.go` / `test_helpers_<category>.go` with `//go:build test` |
| Source-language rule | Go comments, identifiers, and string literals must be English |

---

## Domain-specific (replace wholesale when porting)

bsky-cleaner is a tool for cleaning up a Bluesky (AT Protocol) account (e.g.
deleting old posts, unfollowing accounts). It has no privilege-elevation security model,
so those conditional guides from the reference project do not apply here.

| Name | Value |
|---|---|
| Conditional design-note guide | none yet — if a feature touches the Bluesky/AT Protocol API client, rate limiting, authentication/session handling, or destructive operations (delete/unfollow), add a dedicated design note under `docs/dev/architecture_design/` before relying on one existing |
| Conditional-guide trigger | the feature touches the AT Protocol API client, rate limiting/backoff, session/auth handling, or irreversible destructive operations (post deletion, unfollow, block) |

### Domain examples referenced by commands

The methodology commands (`mkplan`, `runplan`) include illustrative examples
drawn from this project's domain (request identifiers, dry-run semantics,
session teardown, log message formatting). When porting, these examples no
longer apply — replace them with examples from the new project's domain, or
drop them. They appear in:

- `runplan.md` step 5 ("State invariants before coding"): the per-call request
  identifier, `--dry-run` side-effect, and session-teardown examples.
- `mkplan.md` step 5 (symbol-verification `rg` example) and step 6
  (`FormatDeleteSummary` before/after string-literal example).
