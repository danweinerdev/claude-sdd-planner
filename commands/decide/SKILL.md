---
name: decide
description: "Record, look up, or reconcile entries in the decision ledger — the persistent log of decided truths. Triggers: /decide, record decision, log this decision, what did we decide, decision ledger, supersede decision"
---

# /decide — Decision Ledger

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) per `shared/path-resolution.md` in the plugin directory.

## When to Use
- Explicitly record a decision, concept definition, or answered design question as durable truth — a truth that must be findable without knowing which document to open, not a choice a spec or design already states (`shared/decision-log.md` § Capture, the admission test)
- Look up what was decided about a topic ("what did we decide about auth?")
- Reconcile a collision — supersede an old decision with a new one
- Backfill a decision the automatic capture missed
- Audit ledger hygiene (`/decide check`) — stale citations, missed collisions, malformed entries

The convention — entry schema, lifecycle rules, collision procedure — is defined in `shared/decision-log.md` (single source of truth). Read it before operating on the ledger.

## Invocation

```
/decide <statement>            # Record a decision (default subcommand)
/decide list [tag|scope]       # List entries, optionally filtered
/decide search <term>          # Find entries matching a term
/decide show D-NNNN            # Show one entry with its body section, if any
/decide accept D-NNNN          # Promote a proposed entry to accepted
/decide supersede D-NNNN       # Replace an entry with a new decision
/decide check                  # Hygiene audit of the ledger and its citations
```

## Process

### 0. Resolve the ledger
Resolve the ledger per `shared/decision-log.md` § Ledger location: `<planning-root>/Decisions/decisions.md` when the planning root is inside the repo; `<repo-root>/DECISIONS.md` when the planning root is external (decisions live with the repo they represent — resolve the repo via `shared/path-resolution.md`). If missing, create it from `shared/templates/decision-log.md` (and `mkdir -p` any needed directory) before proceeding.

### Record (default)
0. An explicit `/decide` is the user asking for the entry — record it. But if the statement plainly fails the admission test (it restates what an artifact already says, or it's an event or one-off disposition), say so in one line and name the artifact that already carries it, then follow the user's call. Don't refuse, and don't silently skip.
1. Draft the entry per the schema in `shared/decision-log.md`: next sequential `D-NNNN`, `kind`, `statement` (one standalone sentence), `rationale`, and — pull these from the conversation or ask briefly — `rejected[]` alternatives, `scope`, `tags`. `decided_by: user` and `status: accepted` only when the user actually made the choice; an agent-inferred decision is `status: proposed` with `decided_by: agent` (acceptance later flips it to `user-approved`).
2. **Run the collision check** (`shared/decision-log.md` § Collision Detection) before appending. On `contradicts`/`supersedes`, STOP and present both entries; the user chooses supersede / keep-old / both-hold-with-narrowed-scope. Never auto-resolve.
3. Show the drafted entry to the user **verbatim and in full, and wait for explicit approval** — even though `/decide` was an explicit ask, the exact entry text must itself be approved before it is written; an amended draft is re-shown and re-approved. Only then append the approved text unchanged to `decisions[]`, update the ledger's `updated` date, and re-read the frontmatter to confirm it parses as YAML.

### List / Search / Show
Read the ledger frontmatter only (frontmatter-first). Present matching entries compactly: `id — status — statement (date)`. For `show`, include the full entry plus any `## D-NNNN` body section. For lookups phrased as questions ("what did we decide about X?"), answer with the matching `statement`s and cite the ids — the statement IS the answer.

### Accept
Promoting a `proposed` entry is an **append-equivalent event** (`shared/decision-log.md` lifecycle rules): only the user can accept, and the full collision check re-runs first — entries accepted since the proposal was logged may collide with it. On a clean check, show the exact mutation (the entry with `status: accepted`, updated `date`, and `decided_by` flip where applicable) and get explicit approval before writing it. On a collision, stop and run the reconciliation menu.

### Supersede
1. Read the target entry. If it isn't `accepted`, tell the user (rejected/superseded entries need no supersession; proposed entries can simply be edited — they aren't immutable yet).
2. Draft the replacement entry with `supersedes: D-NNNN`; show the exact, complete entry text and the status-flip mutation to the old entry, and get the user's explicit approval of both before writing.
3. Append the approved entry unchanged; on the old entry set `status: superseded` and `superseded_by: <new id>` — touching nothing else in it.
4. Run the **supersession cascade** from `shared/decision-log.md`: grep `Specs/`, `Designs/`, `Plans/` for citations of the superseded entry and report possibly-stale artifacts. Don't rewrite them unasked.

### Check (hygiene audit)
Run the audit defined in `shared/decision-log.md` § Hygiene: missed collisions among `accepted` entries, superseded entries still cited by live artifacts, `scope` references to artifacts that no longer exist, prose decision sections never promoted to the ledger, `proposed` entries older than 30 days, fired `refresh_when` triggers on `assumption` entries, duplicate-id repair, and malformed entries. Report findings; repairs and rotation (also defined there) run only with the user's go-ahead.

## Output
Appends to (or creates) `Decisions/decisions.md`. Never deletes or rewrites accepted entries — status and supersession links are the only permitted mutations. The write is not a commit: the entry rides in the boundary commit of the session or phase that produced it (`shared/autonomy.md` § SCM boundary cadence, D-0024).

## Context
- Convention (single source of truth): `shared/decision-log.md`
- Template: `shared/templates/decision-log.md`
- Schema: `shared/frontmatter-schema.md`
- Orchestration: `shared/orchestration.md`
- Autonomy: `shared/autonomy.md` — every ledger write stops for explicit user approval of the exact entry text; collisions additionally stop for reconciliation
