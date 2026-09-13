# Plan Decisions

Single source of truth for **plan decisions** — the per-plan, append-only record of decided truths. There is no global ledger: each plan owns a flat JSON file, `Plans/<Name>/<Name>-Decisions.json`, and the set of plan folders is the knowledge base.

> **Naming:** this is *plan decisions* (recorded truths). It is unrelated to `shared/decision-framework.md` (the reasoning discipline). Never abbreviate either to "the decision framework/log" ambiguously.

## The File

`Plans/<Name>/<Name>-Decisions.json` is a flat JSON array of entries in append order. Strictly decoded: unknown fields refuse the whole file.

### Entry Schema

```json
{
  "id": "pd-3f9a1c77",
  "date": "2026-09-16",
  "statement": "Manifest reads refused for tenant mismatch emit an audit event carrying only the caller tenant id; the target tenant id is withheld because it leaks tenancy existence.",
  "supersedes": "pd-8b02e7f0",
  "source": "Reviews/2026-09-16-catalog-read-path.md:F-02"
}
```

| Field | Required | Notes |
|---|---|---|
| `id` | yes, tool-set | `pd-` plus the first 8 hex characters of SHA-256 over the normalized statement (trim, collapse internal whitespace, NFC). Never supplied by the caller. |
| `date` | yes, tool-set | ISO date the entry was appended. |
| `statement` | yes | The decision, in one or a few sentences. The only human-authored content — no rejected options, rationale, scope, tags, kind, reversibility, or status fields. A rationale that matters is a sentence in the statement. |
| `supersedes` | no | One id, in this plan (`pd-…`) or another (`<Plan>:pd-…`). A comma-separated list only when one entry reconciles competing successors (see Concurrency). |
| `source` | no | Provenance: `Designs/<X>:DD-N` for a decision compiled from a design, `Reviews/<file>:F-NN` for one that came from a resolved review finding, otherwise absent. |

**A decision is immutable once appended.** Changing one is a new entry that supersedes it — never an edit to the old statement.

## Compile Conversion

`sdd decide sync --plan <Name>` performs this copy on its own for a plan with no proposal to compile, or after a related design gains decisions; it is idempotent.

`sdd compile` reads every design in the plan's `related` frontmatter and appends one entry per top-level `- **DD-N**:` bullet (or `## DD-N` heading), copied verbatim, with `source: Designs/<X>:DD-N`. A `Supersedes DD-N` (or `Supersedes: Other:DD-N`) clause anywhere in the bullet's text becomes that entry's `supersedes` edge. Re-running compile on an existing plan appends only DDs whose digest is not already present, so extending a design and recompiling adds only the new decisions. Designs freeze at handoff (first compile); a DD edited after that point produces a new, un-superseding entry on recompile that the validator reports as pointless, not dangerous.

## Write Protocol

There is exactly one write verb: `sdd decide add --plan P --statement S [--supersedes ID] [--source REF]`. It appends one entry under the same whole-file digest CAS the graph store uses, and prints the entry it wrote. There is no draft state, no proposed status, no accept step.

The driver protocol for an implementation-time decision, in full:

1. Write the statement you propose to record and show it to the user **verbatim**.
2. The user approves it, or edits it and approves the edited text.
3. Run `sdd decide add` with that exact text, once. The tool prints the entry. Done.

A statement the user did not approve is never passed to the tool. This is the only write gate — no admission test, no collision procedure, no fork/detached modes: a content-addressed per-plan file has no shared sequence to keep consistent.

## Cross-Plan Citation and Supersession

Node `justifies` may cite `pd-<hex>` (this plan) or `<Plan>:pd-<hex>` (another plan). A later plan supersedes an earlier plan's entry by citing it in its own file's `supersedes` field — cross-plan supersession is a read-only reference resolved at query time, never a promotion or a second file. Citing a superseded decision from live work is a validator warning (SDD191), not an error: the earlier plan was correct when it closed, and the warning names the successor.

## Conflicts (Concurrency)

Per-file CAS cannot see another branch: two plans on different branches can each supersede the same decision, and `decide add` refuses a second successor it can see at write time. A merge can still land two competing successors from branches that could not see each other; `sdd decide current` and validator rule SDD192 surface this as a conflict naming the contested id and every successor. Reconcile with **one new entry** whose `supersedes` lists every competing successor — never pick a winner by recency or silently drop one.

## Reads

- `sdd decide list --plan P [--json]` — one plan's file, in append order.
- `sdd decide current [--plan P] [--json]` — every plan's standing decisions: follows every `supersedes` chain and prints entries not superseded by anything, collapsing same-id entries across plans into one line.
- `sdd decide lookup <id> [--json]` — one entry and its supersession chain in both directions.

All three are read-only and computed on every call; nothing is cached or stored.

## Consultation

- **The researcher agent is a read path.** It runs `sdd decide current` (optionally scoped `--plan`) to surface standing decisions relevant to the topic under investigation.
- **Reviewers check citations, not a shared ledger.** `plan-reviewer` and `spec-reviewer` treat a plan's compiled decisions as constraints on that plan; a document that contradicts one of its own plan's decisions is a finding.
- **Distribution.** Decisions files are intent context: available to the primary context, `researcher`, `plan-reviewer`, `spec-reviewer`, and `code-implementer` dispatches. They are never given to the intent-isolated review lanes (`quality-scanner`, `blind-spot-finder`) or added to `drift-detector`'s or `spec-compliance`'s curated bundles.

## Ad Hoc Capture

Decisions made during implementation without a review finding still go through the same three-step protocol above — see the `decision-log` model-only skill for the trigger discipline outside lifecycle skills. There are no decisions before a design exists: research and brainstorm stay narrative; a design's `## Design Decisions` section is the only pre-plan home, converted at compile.
