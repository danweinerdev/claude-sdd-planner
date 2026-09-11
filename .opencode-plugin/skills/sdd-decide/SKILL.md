---
name: sdd-decide
description: "Record, look up, or reconcile entries in the decision ledger — the persistent log of decided truths. record decision, log this decision, what did we decide, decision ledger, supersede decision"
---

# Decision Ledger

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
- Explicitly record a decision, concept definition, or answered design question as durable truth — a truth that must be findable without knowing which document to open, not a choice a spec or design already states (`shared/decision-log.md` § Capture, the admission test)
- Look up what was decided about a topic ("what did we decide about auth?")
- Reconcile a collision — supersede an old decision with a new one
- Backfill a decision the automatic capture missed
- Audit ledger hygiene (`sdd-decide check`) — stale citations, missed collisions, malformed entries

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
Resolve the represented repository and inspect its own `planning-config.json`. Config-level `repositoryId` is a sibling of `decisionLog`; `decisionLog` owns `version`, `mode`, `path`, and `ledgerId`. Only when mode is explicitly `fork` or `detached`, run `sdd decide capabilities --json`, require canonical `decision_forks`, then use `sdd decide effective --json`, `sdd decide history --json`, and `sdd decide lookup <qualified-id> --json`. Missing support stops with user-install guidance for a fork-capable binary. With no `decisionLog`, use legacy `sdd decide list --status accepted --json`, `sdd decide search <term> --json`, and conventional live/archive reads; never invoke fork reads as legacy aliases.

### Record (default)
0. An explicit `sdd-decide` is the user asking for the entry — record it. But if the statement plainly fails the admission test (it restates what an artifact already says, or it's an event or one-off disposition), say so in one line and name the artifact that already carries it, then follow the user's call. Don't refuse, and don't silently skip.
1. Draft the entry per the schema in `shared/decision-log.md`: next sequential `D-NNNN`, `kind`, `statement` (one standalone sentence), `rationale`, and — pull these from the conversation or ask briefly — `rejected[]` alternatives, `scope`, `tags`. `decided_by: user` and `status: accepted` only when the user actually made the choice; an agent-inferred decision is `status: proposed` with `decided_by: agent` (acceptance later flips it to `user-approved`).
2. **Run the collision check** (`shared/decision-log.md` § Collision Detection) before appending. On `contradicts`/`supersedes`, STOP and present both entries; the user chooses supersede / keep-old / both-hold-with-narrowed-scope. Never auto-resolve.
3. In legacy mode, show the drafted entry to the user **verbatim and in full, and wait for explicit approval** before using `sdd decide add`; an amended draft is re-shown and re-approved. In fork mode, `add` is unsupported: do not instruct a direct local or inherited-file edit. Refuse and explain that only an applicable `override` or `reconcile` can currently express a local authority change through the exact-preview workflow below; otherwise leave the request pending.

### List / Search / Show
In explicit `fork`/`detached` mode, use `sdd decide effective --json` for current authority, `sdd decide history --json` for historical inspection, and `sdd decide lookup <qualified-id> --json` for one record. With no selector, use `sdd decide list --status accepted --json`, `sdd decide search <term> --json`, and direct conventional frontmatter/archive lookup for a bare id. The fork commands do not support legacy authority.

### Accept
Promoting a legacy `proposed` entry is an **append-equivalent event** (`shared/decision-log.md` lifecycle rules): only the user can accept, and the full collision check re-runs first. Fork-mode `accept` is unsupported and must explicitly refuse without editing either local or inherited files; do not route it through the legacy writer.

### Supersede
1. Read the target through effective/history lookup. If it isn't `accepted`, tell the user (rejected/superseded entries need no supersession; proposed legacy entries can be edited only through the legacy workflow).
2. Draft the replacement entry with `supersedes: D-NNNN`; show the exact, complete entry text and the status-flip mutation to the old entry, and get the user's explicit approval of both before writing.
3. Append the approved entry unchanged; on the old entry set `status: superseded` and `superseded_by: <new id>` — touching nothing else in it.
4. Run the **supersession cascade** from `shared/decision-log.md`: grep `Specs/`, `Designs/`, `Plans/` for citations of the superseded entry and report possibly-stale artifacts. Don't rewrite them unasked.

Fork-mode `supersede` is unsupported and explicitly refuses. If the intended change replaces an inherited authority, use a complete `override` proposal; if an existing local override's upstream basis changed, use `reconcile`. Neither is a generic synonym for supersession.

### Exact fork writes and recovery

Supported fork operations are `adopt`, `override`, `reconcile`, `restore`, `rebind`, and `detach`:

```text
sdd decide fork preview --operation <operation> --file <proposal.json> --json
sdd decide fork apply --file <unchanged-envelope.json> --approval-digest <digest> --json
sdd decide fork inspect --operation <operation-id> --json
sdd decide fork recover --operation <operation-id> --action <finish|rollback|discard-staging> --json
sdd decide fork recover --operation <operation-id> --action <finish|rollback|discard-staging> --approval-digest <digest> --json
```

Preview is read-only. Show the user the preview's **full exact JSON bytes**, including every config/fork byte and authority delta, and obtain explicit approval before passing `--approval-digest`. The hash binds bytes but is not proof of human consent. Save and apply the unchanged envelope; never regenerate approved values. Inspection is read-only. Recovery must first produce and show its own full exact JSON preview, then receive separate explicit user approval before its approval digest is supplied. Never edit inherited sources directly.

### Check (hygiene audit)
Inspect `decisionLog` first. Explicit `fork`/`detached` mode runs capability admission, `sdd decide effective --json`, `sdd decide history --json`, and `sdd decide validate --format json`. With no selector, run `sdd decide list --status accepted --json`, inspect conventional archives, and run `sdd decide validate <resolved-ledger> --format json`. Report findings without hiding diagnostics. Fork repair/hygiene/archive writes explicitly refuse and preserve every file.

## Output
Legacy mode appends through `sdd decide add`; fork mode mutates only the selected local collection through exact preview/apply. Never writes inherited sources. A write is not a commit: source config and independently stored planning history retain their own SCM boundaries (`shared/autonomy.md` § SCM boundary cadence, D-0024).

## Context
- Convention (single source of truth): `shared/decision-log.md`
- Template: `shared/templates/decision-log.md`
- Schema: `shared/frontmatter-schema.md`
- Orchestration: `shared/orchestration.md`
- Autonomy: `shared/autonomy.md` — every ledger write stops for explicit user approval of the exact entry text; collisions additionally stop for reconciliation
