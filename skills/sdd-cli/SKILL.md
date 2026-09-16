---
name: sdd-cli
description: "How to drive the sdd binary — artifact reads/writes, lifecycle transitions, evidence, validation, plan decisions, graph review/amend. Load whenever about to run sdd, record completion evidence, transition a task/phase/plan status, create or edit an SDD artifact, or query plans outside a lifecycle skill."
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
plugin version's hook set. Portable runtimes carry no plugin hooks, so doctor
neither locates nor inspects their plugin installation. Independently of runtime,
in a Git worktree doctor installs or repairs the repository's managed
`post-rewrite` capture dispatcher, honoring `core.hooksPath` and preserving user
hooks. It does not change Git configuration or install a binary. See
`shared/vcs-detection.md` for capture, lineage remapping, and worktree lifetime.

That last part is why it matters: `hooks.json` is generated per platform, so a
plugin upgrade leaves the previous version's file in place. The events it
declares keep firing, which means a newly added event silently never runs and
nothing looks wrong. `doctor` is the only thing that compares. Pass `--check`
to report without repairing. For the Git capture hook, `--check` exits 1 for
repairable findings and 2 for operational/unsafe failures. Read-only reviewers
must use `sdd doctor --check`, never the default repairing invocation.

## Contracts (apply to every subcommand)

- **Exit codes**: `0` success · `1` refused mutation or authoritative
  findings · `2` malformed invocation or the operation could not run. Exit
  `1` means the gate is doing its job — fix the input or complete the
  missing prerequisite; never work around it by editing the file directly.
- **Severities follow the compiler model.** Only `error` (and `operational`,
  meaning a check could not run) makes a root invalid and sets a
  failing exit status. `warning` is a real defect that cannot threaten
  correctness (e.g. citing a superseded decision, SDD191); `candidate` is a
  signal for a human to judge; `waived` is a finding someone explicitly
  excepted. All three are reported and none gates. A clean run with
  non-blocking findings still says `Valid`, with the count — treat "no
  findings" and "findings, none blocking" as different states.
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
| Claim the frontier head under a lease | `sdd next --plan P --claim --by WHO [--json]` |
| Reprint the current holder's claim payload without claiming | `sdd next --plan P --show --by WHO [--json]` |
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
| Record one plan decision (user-approved statement only) | `sdd decide add --plan P --statement TEXT [--supersedes ID] [--source REF] [--json]` |
| List one plan's decisions | `sdd decide list --plan P [--json]` |
| Standing decisions across plans | `sdd decide current [--plan P] [--json]` |
| Look up one decision and its supersession chain | `sdd decide lookup ID [--plan P] [--json]` |
| Render the plan's generated Design.md at plan close | `sdd decide render --plan P [--json]` |
| Copy related designs' DD bullets into the plan's decisions file without compiling a proposal | `sdd decide sync --plan P [--json]` |
| Record a review node's observation from a frozen artifact | `sdd graph review --plan P --node R --artifact A [--by WHO] [--json]` |
| Edit a node's declared artifact write set (holder-only) | `sdd graph set-artifacts --plan P --node N --by WHO [--add PATH]... [--remove PATH]... [--no-render] [--json]` — also re-renders the plan's generated views unless `--no-render` |
| Record an observation from a test report or command result | `sdd graph sync --plan P --node N --by WHO --report FILE [--report-exit N] [--red-kind baseline\|sensitivity] [--fault NAME] \| --command-exit N --command-log FILE [--verbose] [--json]` |
| Fold one report against every unclaimed, non-review node | `sdd graph reverify --plan P --report FILE \| --command-exit N --command-log FILE [--all] [--verbose] [--json]` |
| Apply a frozen review's open findings as amendments | `sdd graph amend --plan P --node R --from-review A --expect-digest D --expect-report-digest RD [--by WHO] [--dry-run] [--json]` |
| Record Git old→new revision lineage without changing proof | `sdd graph remap-revisions --plan P --map FILE --expect-digest D [--dry-run] [--json]` (omit the digest for preview) |
| Render a review node's self-contained claim brief | `sdd graph show <node-id> --plan P --brief [--json]` |
| Migrate a legacy artifact | `sdd migrate <path> [--dry-run] [--diff]` |
| Check the environment and repair repository/plugin hooks | `sdd doctor [--check] [--json]` |

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
- **`decide add` is the only decisions write path, and only after user approval.**
  Show the exact statement, get explicit approval, then run `decide add`
  exactly once — it appends one entry to `Plans/<P>/<P>-Decisions.json` under
  whole-file CAS and prints what it wrote. There is no draft state, no accept
  step. Never hand-edit a `-Decisions.json` file. `--supersedes` names one id
  (or, to reconcile competing successors after a merge, a comma-separated
  list); a second successor for the same id is refused at write time.
- **A review node with open findings writes nothing until amended.**
  `sdd graph review` records a pass only when every finding is terminal; with
  any `open` finding it prints the amendment preview and an `expect-digest`
  and writes nothing to the node. Show that preview to the user before
  running `sdd graph amend --expect-digest`; a mismatched digest is refused
  atomically with nothing written, and the driver re-previews.
- **Writes are not commits.** Every write above lands in the working tree;
  lifecycle state is committed once at phase open and once at phase close
  (`shared/autonomy.md` § SCM boundary cadence). `task complete`
  reports committed-copy checks as pending mid-phase — that is the expected
  state, not a prompt to commit per task.
- **Silence a check only with a reasoned waiver, never by editing around it.**
  A `waivers:` entry (`code` + `reason`) marks a finding as accepted; it is
  still reported, as `waived`, with the reason attached. A plan's decisions
  file has no waiver mechanism — it is append-only and content-addressed, so
  malformed entries and competing successors are refused and reconciled, not
  waived.
- **Read-only contexts stay read-only.** Review and research agents may run
  `validate`, `show`, `list`, `next`, `schema`, `decide list|current|lookup`,
  `version`, and `doctor --check` — never `apply`, `section set`, `evidence add`,
  `decide add`, `graph amend`, or a lifecycle transition. (The PreToolUse
  guard enforces exactly this allowlist for the plugin's read-only agents.)
- **Missing or outdated binary is a stop.** If `sdd` is absent or below the
  plugin's `minSddVersion`, report the exact remedy —
  `go install github.com/danweinerdev/claude-sdd-planner/v2/cmd/sdd@latest` —
  and do not substitute hand edits or model judgment for its checks.
