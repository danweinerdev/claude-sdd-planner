# Decision Log

Single source of truth for the **decision ledger** — the persistent, machine-readable record of decisions the user has made: design choices, concept definitions, and answered design questions. Skills and agents treat `accepted` entries as truth in all future interactions; a new decision that collides with a recorded one stops for user reconciliation, never auto-resolves.

> **Naming:** this is the *decision log* (recorded truths). It is unrelated to `shared/decision-framework.md` (the reasoning discipline). Never abbreviate either to "the decision framework/log" ambiguously.

## The Ledger

A decision ledger is a single canonical file of type `decision-log`. The frontmatter `decisions[]` array is the machine-readable layer — same convention as `phases[]`/`tasks[]`. The body may carry optional `## D-NNNN — Title` sections for extended context (options considered, links); the frontmatter entry is canonical.

### Ledger location — decisions live with the repo they represent

- **Planning root inside the repo** (relative `planningRoot`, e.g. `"."` or `".plans"`): the ledger is `<planning-root>/Decisions/decisions.md`. This is the common case.
- **External planning root** (absolute `planningRoot` outside the repo, possibly shared by multiple repos): a decision is stored **in the repo it represents**, at `<repo-root>/DECISIONS.md` (same format, type `decision-log`). Resolve the repo per `shared/path-resolution.md` (`planMapping` → repo key → local path). Decisions about the planning artifacts themselves, with no target repo, fall back to `<planning-root>/Decisions/decisions.md`.

There is deliberately **no cross-repo global ledger**: each repo's truths are versioned with its code, and one repo's decisions never bleed into another. Collision checks, lookups, and onboarding operate on the resolved ledger for the repo at hand (plus its `archive-*.md` siblings). If the resolved ledger doesn't exist when a decision needs recording, create it from `shared/templates/decision-log.md` first.

Throughout this document, "`Decisions/decisions.md`" means the ledger resolved by these rules.

### Entry Schema

```yaml
decisions:
  - id: D-0001                # zero-padded sequential; stable, never reused
    kind: decision            # decision | definition | answered-question | assumption
    status: accepted          # proposed | accepted | rejected | superseded
    date: YYYY-MM-DD
    decided_by: user          # user | user-approved | agent (agent only while proposed)
    statement: "One-sentence declarative truth — the lookup value."
    question: "What was asked?"          # required for kind: answered-question, optional otherwise
    rejected: [Option B, Option C]       # anti-choices explicitly decided against (collision fuel)
    rationale: "Why this over the alternatives."
    confirmation: "How compliance is checked — a grep, a test, a review question"  # optional but recommended
    scope: [Specs/FeatureName, Designs/ComponentName]  # governed artifacts; empty/absent = global
    tags: [tag1, tag2]
    supersedes: D-0000                   # present on the newer entry after reconciliation
    superseded_by: D-0002                # present only when status: superseded
    reversibility: two-way               # one-way | two-way (default two-way); one-way collisions escalate louder
```

| Field | Required | Notes |
|---|---|---|
| `id`, `kind`, `status`, `date`, `decided_by`, `statement` | yes | `statement` must stand alone — a reader with no other context learns the truth from it. `decided_by: agent` is valid only for an unconfirmed `proposed` entry; acceptance changes it to `user-approved` |
| `rationale` | yes | one or two sentences; deeper deliberation goes in a body section |
| `question` | for `answered-question` | verbatim or near-verbatim, so the same question is findable later |
| `rejected`, `scope`, `tags` | recommended | these three power collision detection and scoped lookups |
| `confirmation` | recommended | how a reviewer or `/decide check` verifies the decision is still being honored — reviewers run or apply it when auditing coverage |
| `supersedes`, `superseded_by`, `reversibility` | situational | supersession links are bidirectional — always write both |

### Lifecycle Rules (append-only)

- **Entries are immutable once `accepted`**, except `status` and `superseded_by`. A change of mind is a **new entry** that `supersedes` the old one — never an edit to the old statement.
- **`rejected` entries are kept** with their rationale. They are negative truths that prevent relitigating.
- **`proposed`** marks an entry awaiting user confirmation (e.g., drafted from a brainstorm recommendation the user hasn't endorsed). An agent-originated proposal uses `decided_by: agent`; only `accepted` entries bind future work, and `proposed` entries are surfaced rather than enforced. A `proposed` entry may still be edited freely — it isn't immutable yet.
- **Accepting a `proposed` entry is an append-equivalent event.** Only the user can accept, and the full collision check below re-runs at acceptance time — entries accepted since the proposal was logged may collide with it. Flip `status` to `accepted`, set `decided_by: user-approved` for an agent-originated proposal, and update `date` only after the check passes.
- **`assumption`-kind entries** may additionally carry `refresh_when` triggers (see `shared/frontmatter-schema.md`); an invalidated assumption is reconciled like any collision — flag every entry and artifact that cited it.

## Deterministic Validation

Before trusting or mutating a resolved ledger, and again after every mutation,
run the bundled read-only validator:

```bash
python3 <plugin-dir>/scripts/sdd_decision_validate.py <resolved-ledger> --format json
```

The validator discovers `archive-*.md` siblings and checks UTF-8/LF and YAML
frontmatter, canonical filenames and lifecycle status, common and entry field
types, real ISO dates, decision id uniqueness, archive eligibility, safe relative
scope syntax, optional body-section links, supersession existence/reciprocity/
state/cycles, structural collision candidates among accepted and proposed
entries, and Git-backed immutability of previously accepted entries. It is
read-only. Exit `0` means no deterministic error was found, but the JSON may
still contain `severity: candidate` diagnostics requiring the judgment pass
below. Exit `1` means error diagnostics are authoritative structural findings,
and exit `2` means the validator could not run. PyYAML is declared in the
plugin's `requirements.txt`; a dependency or operational failure is a stop, not
permission to substitute model parsing.

`--no-history` disables only the Git-backed immutability comparison and is for
unversioned fixtures or an explicitly historical/non-worktree inspection. Do
not use it during a normal ledger write.

Diagnostics for same-question answers, chosen-versus-rejected options, and
conflicting definitions are deterministic **collision candidates**, not an
automatic verdict. The judgment pass below still decides whether candidates
contradict, supersede, refine, or are unrelated, and only the user resolves a
real collision. The full `/validate` skill remains responsible for repository
graph checks that a focused ledger command cannot prove, including scope
resolution, artifact citations, and related-artifact scope connectivity.

## Capture — when to record

A decision is recorded **after the user makes it**, at these moments:

| Moment | Where |
|---|---|
| An open question is resolved at an approval gate | `/specify`, `/design`, `/plan` — each resolved question becomes an `answered-question` entry |
| The user answers an escalation | `/implement` escalation rules — spec ambiguity, scope, destructive-action, blocked-task decisions |
| The user resolves a review finding that required a design decision | the `/poke-holes` / `/code-review` resolution flow (`shared/review-artifacts.md`) — the chosen approach becomes an entry, cited in the Resolution Log |
| The user accepts a brainstorm recommendation | `/brainstorm` — the accepted approach becomes a `decision` entry (unaccepted recommendations stay out, or go in as `proposed`) |
| The user defines a project concept or term | any context — a `definition` entry |
| The user decides ad hoc in conversation | any context — the `decision-log` model-only skill covers moments outside lifecycle skills |
| A debrief captures decisions never logged | `/debrief` backfills "Decisions Made" items as entries |

Rules of capture:

- **Making the decision is the user's; writing the entry is autonomous** (it's a template-following artifact write per `shared/autonomy.md`). Draft the entry, show it in-flow (a short block, not a modal ceremony), and append. `decided_by: user` requires the user actually stated the choice; an agent inference the user merely didn't object to is `proposed`, not `accepted`.
- **Record decisions, not events.** "We chose PostgreSQL over DynamoDB for X" is an entry; "phase 2 completed" is not. Test: would a future session act differently for knowing this?
- **Don't double-log.** Prose sections (Key Decisions, Design Decisions, Decisions Made) still exist for narrative; the ledger entry is the machine-readable pointer of record. Cross-reference the artifact in `scope` rather than duplicating its full deliberation.
- **Cite ids in governed artifacts (bidirectional linking).** When an artifact section is governed by a ledger entry, cite the id inline — e.g., `## Key Decisions` … `Use JWT for session tokens (D-0010)`. The entry points at the artifact via `scope`; the artifact points back via the citation. The supersession cascade and `/decide check`'s stale-citation pass grep for these ids — without citations they are blind.
- **Capture guarantee, stated honestly.** Capture at the structured moments (approval gates, escalations, debrief backfill) is reliable — it's written into the skills. Ad-hoc conversational capture depends on the `decision-log` model-only skill loading, which is best-effort; `/decide` is the manual recovery path and `/decide check` the periodic net. Do not present conversational capture as guaranteed.

## Collision Detection — before every append

Run this check before appending any new entry E. Cheapest layer first; later layers only see survivors of earlier ones.

1. **Candidate filter (no judgment):** Grep the ledger for E's `tags`, `scope` entries, and the key nouns of its `statement`/`question`. Collect matching entries with status `accepted` or `proposed`. If the ledger is small (≲30 entries), just read all of them.
2. **Structural checks (deterministic):** flag a candidate C when any of:
   - E's chosen option appears in C's `rejected[]` (or vice versa) with overlapping `scope`
   - E and C answer the same `question` differently
   - E and C are `definition`s of the same term with different meanings

   **Scope overlap is defined as:** an empty/absent `scope` is global and overlaps everything; two non-empty scopes overlap when they share a path, when one path is nested under the other, **or when the scoped artifacts are connected through `related` frontmatter** (a spec and the designs/plans that cite it are one decision surface — `Specs/Auth` and `Designs/AuthService` overlap if either's `related` names the other, directly or through one hop). When overlap is ambiguous, treat it as overlapping — a false collision costs the user one dismissal; a missed one silently forks the truth.
3. **Judgment pass:** for each remaining candidate, classify the pair: `contradicts` | `supersedes` | `refines` | `unrelated`. `refines` (narrows scope, adds detail without conflicting) and `unrelated` pass; record `refines` relationships in `related` prose if useful.
4. **On `contradicts` or `supersedes` — STOP for the user.** Present both entries verbatim (id, statement, date, rationale, scope) and the nature of the conflict. Offer:
   - **Supersede** — the new decision wins: append E with `supersedes: C.id`; set C's `status: superseded` and `superseded_by: E.id` (the only permitted mutation of an accepted entry)
   - **Keep the old** — withdraw or amend E
   - **Both hold** — the user declares the scopes disjoint: narrow both entries' `scope` explicitly so the collision is structurally resolved, and append E
   - Never resolve silently, never pick a winner by recency, and treat a collision with a `reversibility: one-way` entry as high-stakes — say so explicitly.
   - **One-step supersession for fresh instructions:** when E comes from an explicit user statement made moments ago (an escalation answer, a direct instruction), don't reopen the decision — present the collision as a single confirmation: "This supersedes D-NNNN (*old statement*) — confirm?" One yes resolves it; anything less than yes falls back to the full menu above.
5. **Supersession cascade:** after a supersession, grep artifacts (`Specs/`, `Designs/`, `Plans/`) for the superseded entry's id — this is why the citation convention above is load-bearing — plus the entry's `scope` artifacts regardless of citation. Report any hits to the user as possibly-stale artifacts (a `/decide check` concern thereafter) — don't rewrite them unasked.

## Consultation — how the ledger is read

- **The researcher agent is the universal read path.** It scans `Decisions/` alongside the other artifact directories and returns a **Recorded Decisions** section: `accepted` entries relevant to the topic (matched by tags/scope/terms), plus any tension between the ledger and other artifacts it noticed. Skills that dispatch the researcher get ledger awareness for free.
- **Accepted entries are constraints.** When drafting a spec/design/plan (or implementing) would contradict an `accepted` entry, that is a collision: surface it per the procedure above — do not silently comply with the ledger against the user's current ask, and do not silently override the ledger either. The user's fresh instruction plus an explicit supersession is the resolution.
- **Session onboarding and post-compaction:** the ledger frontmatter is on both read lists in `shared/orchestration.md` — statuses and statements only; bodies on demand.
- **Reviewers check coverage, not just contradiction.** `plan-reviewer` and `spec-reviewer` cross-check documents under review against `accepted` entries two ways: a document that **contradicts** an entry is Major (Critical when the entry is `reversibility: one-way`), and a document that simply **ignores** an accepted entry scoped to it (or global) is also a finding — the entry must be honored (cite the id), explicitly superseded, or explicitly scoped away. Where an entry carries a `confirmation` field, the reviewer applies it.

### Distribution — who may see the ledger

The ledger is **intent context**. It goes to: the primary context, `researcher`, `plan-reviewer`, `spec-reviewer`, and `code-implementer` dispatches (as scoped excerpts when relevant to a task). It must **never** be given to the intent-isolated review lanes — `quality-scanner` (intent-blind), `blind-spot-finder` (diff-only) — nor added to `drift-detector`'s or `spec-compliance`'s curated bundles. `shared/templates/quality-scan-prompt.md` names it in the prohibition list.

## Concurrency and Merge Conflicts (known limitation)

Sequential ids and a single ledger file assume **one writer at a time**. Two concurrent sessions or two branches can each mint the same `D-NNNN` and will conflict on merge (a YAML array is merge-hostile). This is accepted for the common solo-planner case; teams should expect occasional conflicts. Repair is a `/decide check` job: on duplicate ids, keep the earlier entry's id, renumber the later one to the next free id, and chase every incoming `supersedes`/`superseded_by` link and artifact citation of the renumbered id. Never resolve a duplicate by deleting either entry.

## Hygiene

`/decide check` audits: collisions among `accepted` entries using the scope-overlap definition above (the append-time check can miss pairs that predate it), superseded entries still cited by live artifacts (grep for ids), `scope` references to artifacts that no longer exist, prose decision sections never promoted to the ledger, `proposed` entries older than 30 days, `assumption` entries whose `refresh_when` triggers have fired (an invalidated assumption is reconciled like a collision), duplicate-id repair (above), and malformed entries (missing required fields, broken supersession links).

**Rotation:** when the ledger grows past ~100 entries, `/decide check` offers to move `superseded` and `rejected` entries to `Decisions/archive-<YYYY>.md` (type `decision-log`, status `archived`). Ids stay unique across live ledger and archives. `accepted` and `proposed` entries never rotate. The collision candidate filter greps `Decisions/archive-*.md` too — archived `rejected` entries are still negative truths; only session onboarding is limited to the live ledger.
