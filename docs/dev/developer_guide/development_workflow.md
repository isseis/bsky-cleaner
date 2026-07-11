# Development Workflow

This page is the entry point for `docs/dev/developer_guide/` — what each
document is for, what order to read them in, how the requirements → design →
implementation → PR process actually runs command by command, which AI
commands exist for each assistant, and how bilingual documentation is kept in
sync. Read this page first; the other guides go into detail on the pieces it
references.

## 1. Document map and reading order

| # | Document | Read when |
|---|----------|-----------|
| 1 | [requirements_process.md](requirements_process.md) | Before starting any new feature or security-relevant change — defines the `01_requirements.md` / `02_architecture.md` / `03_implementation_plan.md` structure, the approval gate, and acceptance-criteria traceability. |
| 2 | [task_identification.md](task_identification.md) | Whenever you or an AI command need to resolve which `docs/tasks/XXXX_feature/` directory a command should operate on. |
| 3 | [mermaid_reference.md](mermaid_reference.md) | When drawing diagrams in `02_architecture.md` or any other design doc. |
| 4 | [test_organization.md](test_organization.md) | When writing or placing test helper files during implementation. |
| 5 | [package_reference.md](package_reference.md) | To see the current `cmd/`/`internal/` package layout before adding new code. |
| 6 | [build_from_source.md](build_from_source.md) | When building the binary locally instead of using a release/Docker image. |
| 7 | [notify_preview.md](notify_preview.md) | When working on Slack notification formatting and you want to preview a message before running the full pipeline. |

This document itself (development_workflow.md) is the only one describing the
end-to-end process; the others are referenced from it as needed.

## 2. The development process, step by step

Each feature or security-relevant change goes through three gated documents
under `docs/tasks/XXXX_feature/`, then implementation, then PR review. Every
`draft → approved` transition in this flow is a **human** action — no AI
command is allowed to set a document's status to `approved` itself (see
[requirements_process.md](requirements_process.md) § 0).

1. **Write `01_requirements.md`** (status: `draft`).
   No dedicated command — start from the template at
   [docs/tasks/0000_template/01_requirements.md](../../tasks/0000_template/01_requirements.md),
   copy it into a new `docs/tasks/XXXX_feature/` directory, then discuss the
   spec with Claude in conversation and have it fill in the requirements
   document from that discussion. This step is inherently interactive, which
   is why it has no dedicated slash command.
   → Human reviews and sets status to `approved`.

2. **`/mkarch`** — generates `02_architecture.md` (status: `draft`) from the
   approved requirements doc.
   → Human reviews and sets status to `approved`.

3. **`/mkplan`** — generates `03_implementation_plan.md` (status: `draft`)
   from the approved architecture doc.
   → Human reviews and sets status to `approved`.

4. **`/mkplan2`** — inserts `### PR-N 作成ポイント` markers into
   `03_implementation_plan.md`, grouping steps into reviewable PRs. Runs once
   per plan, after step 3's document is approved and before the first
   `/runplan` call in step 5 — it decides where one PR ends and the next
   begins.

5. **`/runplan`** — implements one `PR-N` group: writes code and tests, runs
   the green gate (`fmt`/`test`/`lint`/`deadcode`), and opens the PR
   (`gh pr create`).
   - Scoped to a single `PR-N` group per invocation; a plan with 3–6 PR
     groups (the target size — see `/mkplan2`) means 3–6 `/runplan`
     invocations, repeating steps 5–7 below for each group.
   - Default: Claude (Sonnet) runs `/runplan`.
   - Alternative: Cline can run this step instead — `Execute
     @.cline/commands/runplan.md` (see § 3 "Invocation syntax") — typically
     with a smaller/cheaper model. Cline's `runplan.md` reviews its own work
     in-conversation rather than delegating to a separate reviewer subagent,
     so a Cline-driven run of this step must be followed by Claude's
     `/weakreview` (step 5a below) before the PR is merged.

   5a. **(Cline path only) `/weakreview`** in Claude — a targeted second-pass
       review for the judgment-heavy mistakes a lower-capability executing
       model tends to leave behind, which `/code-review`/`/simplify` don't
       specifically target (see the command file for the full failure
       checklist, and § 3 "Recommended: Claude `/weakreview` after a Cline
       `/runplan` run"). Also usable, optionally, after a Claude-driven
       `/runplan` run if a lower-capability model was used for it.

6. **PR review loop.** Human (or reviewer) leaves PR comments.
   - Either fix comments directly, or run `/fixpr` to resolve unresolved PR
     review threads (fetch → triage → fix → reply → re-check). Comments
     you'd rather address by hand don't need `/fixpr`.
   - Repeat until the PR is approved.

7. **Merge and continue.** Merge the PR, switch to a new branch, and go back
   to step 5 for the next `PR-N` group — until `03_implementation_plan.md` is
   fully implemented.

## 3. AI command list (Claude vs. Cline)

Commands live under `.claude/commands/` (Claude Code) and `.cline/commands/`
(Cline). Both read shared project configuration from their own `_context.md`,
but Cline currently has a smaller command set — check the table below before
assuming a command is available in both tools.

| Command | Claude | Cline | Purpose |
|---------|:---:|:---:|---------|
| `mkarch` | ✅ | ❌ | Generate `02_architecture.md` from an approved `01_requirements.md`. |
| `mkplan` | ✅ | ❌ | Generate `03_implementation_plan.md` from an approved `02_architecture.md`. |
| `mkplan2` | ✅ | ❌ | Insert `PR-N` boundary markers into an approved `03_implementation_plan.md`. |
| `runplan` | ✅ | ✅ | Implement one `PR-N` group: code, tests, green gate, PR creation. |
| `fixpr` | ✅ | ❌ | Fetch, triage, and fix unresolved PR review threads, then reply and re-check. |
| `mktrans` | ✅ | ✅ | Translate a bilingual doc pair (`.md` ⇄ `.ja.md`), full or differential. |
| `japrose` | ✅ | ❌ | Improve Japanese prose quality of a Japanese-primary document without changing its content. |
| `weakreview` | ✅ | ❌ | Targeted second-pass review for mistakes typical of a lower-capability executing model. |

When porting a command to Cline, follow the porting notes embedded in the
Claude version of the command file (e.g. `mkplan2.md` states which
Go/`cmd`/`internal`-specific parts need adjusting) and add the shared
project-context values to `.cline/commands/_context.md`.

### Invocation syntax

The two tools invoke commands differently — a command's *content* may be
shared or ported between `.claude/commands/` and `.cline/commands/`, but how
you call it is tool-specific:

- **Claude Code**: slash command with the task ID as an argument, e.g.:
  ```
  /mkarch 0001
  /runplan 0001
  ```
- **Cline**: `Execute` the command file directly, with the task ID as an
  argument, e.g.:
  ```
  Execute @.cline/commands/runplan.md 0001
  ```
  (Cline has no slash-command registry — `@.cline/commands/<name>.md` is a
  file reference, and `Execute` tells Cline to treat that file's contents as
  the instructions to follow, with everything after the path passed through
  as `$ARGUMENTS`/`$1`-style input the same way a Claude slash command
  argument would be.)

### Recommended: Claude `/weakreview` after a Cline `/runplan` run

Cline's `runplan.md` reviews its own work in-conversation (via
`.cline/commands/_lib/review-self-pattern.md`) rather than delegating to a
separate reviewer subagent, and it is typically run with a smaller/cheaper
model than Claude's default. Because of that, after a Cline `runplan` session
finishes a `PR-N` group (and before or shortly after opening the PR), run
Claude's `/weakreview` command with the same range/PR so a stronger model
catches the judgment-heavy mistakes a weaker executing model tends to leave
behind (see § 2's `/weakreview` note, and the command file's own rationale for
why it's a distinct pass from `/code-review`/`/simplify`):

```
/weakreview 0001          # or a PR number / commit range
```

This is a recommendation, not a hard gate — use it whenever the implementing
session used a lower-capability model, regardless of which tool ran it.

## 4. Bilingual documentation handling

Some documents exist as an English/Japanese pair (`foo.md` + `foo.ja.md`) —
currently `README`, `docs/overview`, `docs/design/*`, and this document
(`development_workflow.md` / `development_workflow.ja.md`). The rest of
`docs/dev/developer_guide/` and all of `docs/tasks/XXXX_feature/` are **not**
bilingual pairs: the other developer guides are English-only, and task
documents (`01_requirements.md`/`02_architecture.md`/`03_implementation_plan.md`)
are Japanese-primary working documents with no translation counterpart.

For an actual bilingual pair, the rule is: **update one language first, then
translate with `/mktrans`.**

1. Edit the source language version directly (see
   [Translation Guidelines](../../../CLAUDE.md#translation-guidelines-japanese-to-english)
   in `CLAUDE.md` for which direction is canonical per doc — generally the
   Japanese version is written/updated first, then translated to English).
2. Commit the source-language change.
3. Run `/mktrans <path-to-source-file>`. It infers direction from the file
   extension (`*.ja.md` source → English output; `*.md` source → `*.ja.md`
   output), does a full translation if the output file doesn't exist yet or a
   differential translation (only the changed sections, via `git diff` against
   the commit that last touched the output file) if it does, runs a
   translator-persona review pass, and commits the translated file (and any
   glossary updates) separately.
4. Never hand-edit both language versions in the same commit outside of
   `/mktrans` — that defeats the differential-translation sync point (`git log`
   on the output file) the command relies on to know what changed.

New terminology encountered during translation is added to
`docs/translation_glossary.md` automatically by `/mktrans`; `/japrose` also
reads that glossary to keep Japanese-primary documents (task docs, other
Japanese guides) terminologically consistent, but does not perform
translation itself.
