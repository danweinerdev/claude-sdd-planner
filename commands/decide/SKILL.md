---
name: decide
description: "Record or look up a plan's decisions — the append-only, per-plan record of decided truths. Triggers: /decide, record decision, log this decision, what did we decide, plan decisions, supersede decision"
---

# /decide — Plan Decisions

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) per `shared/path-resolution.md` in the plugin directory.

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
