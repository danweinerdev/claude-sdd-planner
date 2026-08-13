---
name: sdd-cli
description: "How to drive the sdd binary — artifact reads/writes, lifecycle transitions, evidence, validation, the decision ledger. Load whenever about to run sdd, record completion evidence, transition a task/phase/plan status, create or edit an SDD artifact, or query plans outside a lifecycle skill."
disable-model-invocation: true
---

# The `sdd` CLI — Interface Discipline

The `sdd` binary is the **write path** for SDD artifacts and the deterministic
half of validation. Artifacts are compiled documents: create and modify them
through the binary, never by hand-editing frontmatter or evidence sections —
a hand edit bypasses schema compilation, digest tracking, and the refusal
gates the workflow depends on.

Run `sdd help` for the authoritative usage; `sdd schema list` / `sdd schema
show <type>` for the artifact contracts. This skill carries the discipline
and the task→command map, not every flag.

## Contracts (apply to every subcommand)

- **Exit codes**: `0` success · `1` refused mutation or authoritative
  findings · `2` malformed invocation or the operation could not run. Exit
  `1` means the gate is doing its job — fix the input or complete the
  missing prerequisite; never work around it by editing the file directly.
- **Machine reads**: prefer `--json` over parsing rendered markdown.
- **Preview before mutate**: `--dry-run` and `--diff` are available on the
  writing commands; use them when the change is non-obvious.
- **Concurrent-edit safety**: `sdd show` reports a content digest; pass it
  back via `--expect DIGEST` on `apply`/`section set` so the write is
  refused if the artifact changed underneath you.
- **stdin writers**: `apply` reads a full Markdown proposal on stdin;
  `section set` reads one section body on stdin and leaves everything else
  byte-identical (aside from `updated`).

## Task → command

| You need to… | Run |
|---|---|
| Inspect an artifact (frontmatter, digest) | `sdd show <path> [--json]` |
| List artifacts by type | `sdd list [spec\|design\|plan\|research] [--root PATH] [--json]` |
| Find the next actionable plan task | `sdd next [PLAN-PATH] [--json]` |
| Create/replace an artifact from a proposal | `sdd apply <path> [--create] [--expect DIGEST]` (proposal on stdin) |
| Replace one section only | `sdd section set <path> --heading "## Overview" [--expect DIGEST]` (body on stdin) |
| Start a new artifact from its template | `sdd template <type> [--out PATH]` |
| Validate (deterministic layer) | `sdd validate [--root PATH] [--scope PATH] [--format json]` |
| Record completion evidence | `sdd evidence add <path> --task ID\|--phase\|--plan --verified-by CMD --result TEXT [--working-dir PATH]` |
| Transition a status (evidence-gated) | `sdd task complete <phase-path> --id ID` · `sdd phase complete <phase-path>` · `sdd plan complete <plan-path>` |
| Scaffold a phase-gate review | `sdd review scaffold <phase-path> --frozen <base>..<endpoint>` |
| Ledger: add / list / search / audit | `sdd decide add --statement TEXT [--accept] …` · `sdd decide list\|search` · `sdd decide validate [<ledger>]` |
| Migrate a legacy artifact | `sdd migrate <path> [--dry-run] [--diff]` |
| Diagnose the environment | `sdd doctor [--json]` |

## Discipline

- **Statuses move through transitions, not edits.** `task|phase|plan
  complete` enforce the completion-evidence gate; setting `status: complete`
  by editing frontmatter forges a completion. If the transition refuses,
  the evidence or a child status is genuinely missing.
- **Evidence records what actually ran.** `evidence add` takes the exact
  command and its observed result — never a paraphrase of what should have
  happened. Fabricated evidence is worse than pending evidence.
- **Validate before claiming.** Any statement that artifacts are consistent,
  a plan is ready, or a phase can close is checkable: run `sdd validate`
  (scoped where possible) and report its verdict, not your impression.
- **The ledger is append-through-the-tool.** `decide add` runs the collision
  check; a collision with an accepted entry stops for the user. Never
  append to `decisions.md` by hand and never auto-resolve a collision.
- **Read-only contexts stay read-only.** Review and research agents may run
  `validate`, `show`, `list`, `next`, `schema`, `decide list|search|validate`,
  `version`, and `doctor` — never `apply`, `section set`, `evidence add`,
  `decide add`, or a lifecycle transition. (The PreToolUse guard enforces
  exactly this allowlist for the plugin's read-only agents.)
- **Missing or outdated binary is a stop.** If `sdd` is absent or below the
  plugin's `minSddVersion`, report the exact remedy —
  `go install github.com/danweinerdev/claude-sdd-planner/cmd/sdd@latest` —
  and do not substitute hand edits or model judgment for its checks.
