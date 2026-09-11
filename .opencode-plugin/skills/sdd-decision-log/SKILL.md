---
name: sdd-decision-log
description: "Recording user decisions as durable truth in the decision ledger. Load whenever the user makes a design or architecture choice, defines a project concept or term, answers a design question, reverses an earlier decision, or when current work touches a topic the ledger may already govern — including plain conversation outside any sdd-planner skill."
disable-model-invocation: true
---

# Decision Log — Capture and Collision Discipline

The full convention (entry schema, lifecycle rules, collision procedure, distribution rules) lives in `shared/decision-log.md` in the plugin directory — read it before your first ledger write of a session. This skill exists so decision moments *outside* the lifecycle skills still reach the ledger.

Inspect the represented repository's `decisionLog` selection first. Explicit `fork`/`detached` mode runs `sdd decide capabilities --json`, requires canonical `decision_forks`, then uses `sdd decide effective --json`, history, and qualified lookup. Missing support stops with user-install guidance for a fork-capable binary. With no `decisionLog`, use `sdd decide list --status accepted --json`, `sdd decide search <term> --json`, and conventional live/archive reads. Fork commands are not legacy aliases.

## When the user just decided something

1. Recognize the moment: a stated choice between alternatives, a definition of a project term, an answer to a design question, or an explicit reversal. Then apply the **admission test** (`shared/decision-log.md` § Capture) before writing anything: the truth has to outlive the document being worked on, not already be carried by that document, and have had a real alternative that lost. Status updates, task events, one-off dispositions, and choices that a spec or design already states in full are not ledger entries — leaving them where they are is the correct outcome, not a miss.
2. **Run the collision check first** (`shared/decision-log.md` § Collision Detection): grep the ledger for the new entry's tags, scope, and key nouns; apply the structural checks; judge survivors. On `contradicts`/`supersedes` → STOP and present both entries for the user to reconcile. Never auto-resolve, never pick by recency.
3. Draft the entry (next sequential `D-NNNN`, `decided_by: user` only if the user actually stated the choice — otherwise `status: proposed`) and **show the exact, complete entry text to the user for explicit approval before writing anything**. The ledger is never edited without that approval — the decision being the user's does not make the write approved, and non-objection is not approval. If the user amends the entry, show the amended text and get approval again.
4. Legacy mode may append through the supported CLI only after explicit approval. Fork-mode add/accept/supersede/archive/hygiene writes refuse—never edit inherited files. For a supported operation run `sdd decide fork preview --operation <operation> --file <proposal.json> --json`, show/approve the full exact JSON, then run `sdd decide fork apply --file <unchanged-envelope.json> --approval-digest <digest> --json`; a hash is not consent. Recovery is two distinct invocations: preview with `sdd decide fork recover --operation <id> --action <action> --json`, show/approve those bytes, then apply with `sdd decide fork recover --operation <id> --action <action> --approval-digest <digest> --json`.

## When about to act on a governed topic

Before drafting or implementing in an area the ledger may govern, check `accepted` entries whose tags/scope match. They are constraints: if the current ask contradicts one, surface the collision instead of silently following either side.
