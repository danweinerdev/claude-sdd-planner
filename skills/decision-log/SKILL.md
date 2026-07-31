---
name: decision-log
description: "Recording user decisions as durable truth in the decision ledger. Load whenever the user makes a design or architecture choice, defines a project concept or term, answers a design question, reverses an earlier decision, or when current work touches a topic the ledger may already govern — including plain conversation outside any sdd-planner skill."
disable-model-invocation: true
---

# Decision Log — Capture and Collision Discipline

The full convention (entry schema, lifecycle rules, collision procedure, distribution rules) lives in `shared/decision-log.md` in the plugin directory — read it before your first ledger write of a session. This skill exists so decision moments *outside* the lifecycle skills still reach the ledger.

Resolve the ledger per `shared/decision-log.md` § Ledger location — `<planning-root>/Decisions/decisions.md` for an in-repo planning root, `<repo-root>/DECISIONS.md` when the planning root is external (decisions live with the repo they represent). Create it from `shared/templates/decision-log.md` if missing.

## When the user just decided something

1. Recognize the moment: a stated choice between alternatives, a definition of a project term, an answer to a design question, or an explicit reversal. Then apply the **admission test** (`shared/decision-log.md` § Capture) before writing anything: the truth has to outlive the document being worked on, not already be carried by that document, and have had a real alternative that lost. Status updates, task events, one-off dispositions, and choices that a spec or design already states in full are not ledger entries — leaving them where they are is the correct outcome, not a miss.
2. **Run the collision check first** (`shared/decision-log.md` § Collision Detection): grep the ledger for the new entry's tags, scope, and key nouns; apply the structural checks; judge survivors. On `contradicts`/`supersedes` → STOP and present both entries for the user to reconcile. Never auto-resolve, never pick by recency.
3. Append the entry (next sequential `D-NNNN`, `decided_by: user` only if the user actually stated the choice — otherwise `status: proposed`), update the ledger's `updated` date, and confirm the frontmatter still parses as YAML.
4. Mention the recording in one line (e.g., "Recorded as D-0007 in the decision ledger") — no ceremony.

## When about to act on a governed topic

Before drafting or implementing in an area the ledger may govern, check `accepted` entries whose tags/scope match. They are constraints: if the current ask contradicts one, surface the collision instead of silently following either side.
