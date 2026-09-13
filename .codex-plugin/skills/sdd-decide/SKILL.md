---
name: sdd-decide
description: "Record or look up a plan's decisions — the append-only, per-plan record of decided truths. record decision, log this decision, what did we decide, plan decisions, supersede decision"
---

# Plan Decisions

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
- Record a decision made during implementation or ad hoc conversation as durable truth in a plan's decisions file
- Look up what was decided in a plan, or across every plan ("what did we decide about auth?")
- Reconcile competing successors after a merge

The convention — entry schema, write protocol, cross-plan citation, conflicts — is defined in `shared/decision-log.md` (single source of truth). Read it before operating on a decisions file.

## Invocation

```
/decide <statement>                     # Record a decision in the current plan (default subcommand)
/decide list <plan>                     # List one plan's decisions in append order
/decide current [plan]                  # Standing decisions across every plan (or one plan)
/decide lookup <id>                     # Show one decision and its supersession chain
/decide render <plan>                   # Regenerate the plan's Design.md from its decisions and graph
/decide sync <plan>                     # Copy related designs' DD bullets into the plan's decisions file (no proposal needed)
```

## Process

### Record (default)
1. Draft the statement in one or a few sentences — the statement is the decision; no rejected options, rationale, scope, tags, or status fields. Pull it from the conversation.
2. **Show the exact, complete statement to the user for explicit approval before writing anything.** If the user amends it, show the amended text and get approval again.
3. Once approved, run `sdd decide add --plan <Name> --statement "..."` exactly once. If this decision replaces an earlier one, add `--supersedes <id>`; if it traces to a design or a resolved review finding, add `--source Designs/<X>:DD-N` or `--source <review>.md:F-NN`. The tool prints the written entry. Done — there is no draft state and no accept step.

### List / Current / Lookup
- `sdd decide list --plan <Name> [--json]` — one plan's file, in append order.
- `sdd decide current [--plan <Name>] [--json]` — standing decisions (supersession chains followed, superseded entries omitted).
- `sdd decide lookup <id> [--json]` — one entry and its supersession chain in both directions.

### Sync
`sdd decide sync --plan <Name>` is the decisions half of `sdd compile` on its own: it copies every related design's `DD-N` bullet into the plan's decisions file, verbatim, with any declared `Supersedes DD-N` / `Supersedes Design:DD-N` edge. Use it when a design gained decisions after the plan was compiled, or for a record-only plan that has no proposal to compile. Idempotent; a dangling `Supersedes` refuses.

### Render
`sdd decide render --plan <Name>` writes `Plans/<Name>/Design.md` (type `decisions-view`, never validated, never hand-edited): every standing decision with the nodes that cite it, and every superseded one struck through with its successor. Run it when a plan closes, or whenever a reader wants "how this plan works"; regenerating for the same inputs is byte-identical.

### Reconciling competing successors
When `sdd decide current` or validator rule SDD192 reports two entries that each supersede the same id (a merge of independent branches), draft **one new entry** whose `--supersedes` lists every competing successor, comma-separated. Show it for approval like any other entry, then run `sdd decide add` once. Never pick a winner by recency.

## Output
A write appends one entry to `Plans/<Name>/<Name>-Decisions.json` through `sdd decide add`. A write is not a commit: planning history keeps its own SCM boundary (`shared/autonomy.md` § SCM boundary cadence).

## Context
- Convention (single source of truth): `shared/decision-log.md`
- Schema: `shared/frontmatter-schema.md`
- Orchestration: `shared/orchestration.md`
- Autonomy: `shared/autonomy.md` — `decide add` runs only after the user approves the exact statement
