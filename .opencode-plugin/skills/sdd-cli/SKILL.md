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

## Start here

Run `sdd doctor` once when you begin using sdd in a project. It reports the
binary in use, the resolved planning root, and the embedded schema set. Under
Claude Code, where `CLAUDE_PLUGIN_ROOT` identifies the active plugin, it also
regenerates `hooks.json` when that file is absent or does not match this
plugin version's hook set. Portable runtimes carry no hooks, so doctor neither
locates nor inspects their plugin installation.

That last part is why it matters: `hooks.json` is generated per platform, so a
plugin upgrade leaves the previous version's file in place. The events it
declares keep firing, which means a newly added event silently never runs and
nothing looks wrong. `doctor` is the only thing that compares. Pass `--check`
to report without repairing.

## Contracts (apply to every subcommand)

- **Exit codes**: `0` success · `1` refused mutation or authoritative
  findings · `2` malformed invocation or the operation could not run. Exit
  `1` means the gate is doing its job — fix the input or complete the
  missing prerequisite; never work around it by editing the file directly.
- **Severities follow the compiler model.** Only `error` (and `operational`,
  meaning a check could not run) makes a root or ledger invalid and sets a
  failing exit status. `warning` is a real defect that cannot threaten
  correctness — including one inherited history forbids repairing;
  `candidate` is a signal for a human to judge; `waived` is a finding
  someone explicitly excepted. All three are reported and none gates. A
  clean run with non-blocking findings still says `Valid`, with the count —
  treat "no findings" and "findings, none blocking" as different states.
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
| Transition a status (evidence-gated) | `sdd task complete <phase-path> --id ID` · `sdd phase complete <phase-path>` · `sdd plan approve\|activate\|complete <plan-path>` |
| Transition a spec/design status | `sdd spec\|design submit\|approve\|implement\|supersede <path>` (`supersede` **requires** `--by <successor>`) |
| Scaffold a phase-gate review | `sdd review scaffold <phase-path> --frozen <base>..<endpoint>` |
| Record one review lane's observation | `sdd review evidence set <review-path> --lane <id> [--evidence TEXT]` (or evidence on stdin) |
| Close a phase-gate review | `sdd review resolve <review-path> [--accept-followups] [--dry-run]` |
| Ledger: add / list / search / audit | `sdd decide add --statement TEXT [--accept] …` · `sdd decide list\|search` · `sdd decide validate [<ledger>]` |
| Migrate a legacy artifact | `sdd migrate <path> [--dry-run] [--diff]` |
| Check the environment (and repair Claude Code hooks) | `sdd doctor [--check] [--json]` |

## Discipline

- **Statuses move through transitions, not edits.** `task|phase|plan
  complete` enforce the completion-evidence gate; setting `status: complete`
  by editing frontmatter forges a completion. If the transition refuses,
  the evidence or a child status is genuinely missing.
- **A phase-gate review is a transition chain, not an edit.** `review
  scaffold` starts it open and unfrozen; `review evidence set` records each
  lane's real observation; `review resolve` verifies the gate and sets
  `frozen: true` + `status: resolved` in one write. After resolve the
  artifact is immutable (SPK050) — new work gets a fresh review, never an
  edit of the frozen one.
- **Evidence records what actually ran.** `evidence add` takes the exact
  command and its observed result — never a paraphrase of what should have
  happened. Fabricated evidence is worse than pending evidence.
- **Validate before claiming.** Any statement that artifacts are consistent,
  a plan is ready, or a phase can close is checkable: run `sdd validate`
  (scoped where possible) and report its verdict, not your impression.
- **The ledger is append-through-the-tool.** `decide add` runs the collision
  check; a collision with an accepted entry stops for the user. Never
  append to `decisions.md` by hand and never auto-resolve a collision.
- **Writes are not commits.** Every write above lands in the working tree;
  lifecycle state is committed once at phase open and once at phase close
  (`shared/autonomy.md` § SCM boundary cadence, D-0024). `task complete`
  reports committed-copy checks as pending mid-phase — that is the expected
  state, not a prompt to commit per task.
- **Silence a check only with a reasoned waiver, never by editing around it.**
  A `waivers:` entry (`code` + `reason`) marks a finding as accepted; it is
  still reported, as `waived`, with the reason attached. Ledger waivers cover
  only `DLG064`/`DLG065`, the sequencing conditions append-only history can
  forbid repairing — everything else describes a ledger that cannot be
  trusted, and hiding that is not the same as accepting it. An unexplained
  waiver is an error (`DLG078`); one that matches nothing is reported stale
  (`DLG079`), because an exception outliving its cause disables a check
  silently. **Adding a waiver to the ledger is a ledger write: it needs the
  user's explicit approval of the exact text, like any other entry.**
- **Read-only contexts stay read-only.** Review and research agents may run
  `validate`, `show`, `list`, `next`, `schema`, `decide list|search|validate`,
  `version`, and `doctor` — never `apply`, `section set`, `evidence add`,
  `decide add`, or a lifecycle transition. (The PreToolUse guard enforces
  exactly this allowlist for the plugin's read-only agents.)
- **Missing or outdated binary is a stop.** If `sdd` is absent or below the
  plugin's `minSddVersion`, report the exact remedy —
  `go install github.com/danweinerdev/claude-sdd-planner/v2/cmd/sdd@latest` —
  and do not substitute hand edits or model judgment for its checks.
