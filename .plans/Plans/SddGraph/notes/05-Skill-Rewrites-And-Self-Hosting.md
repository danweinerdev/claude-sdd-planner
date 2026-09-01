---
title: "Phase 5 Debrief: Skill Rewrites and Self-Hosting"
type: debrief
plan: "SddGraph"
phase: 5
phase_title: "Skill Rewrites and Self-Hosting"
status: complete
created: 2026-09-01
updated: 2026-09-01
tags: [graph, self-hosting, skills, pilot, coexistence]
related: [Designs/SddGraph, Plans/SddGraph]
---

# Phase 5 Debrief: Skill Rewrites and Self-Hosting

## Decisions Made
Key choices during implementation, with rationale.

- **The plan skill ships with no portable variant** — the rewritten protocol is CLI-shaped and harness-neutral end to end, so the generated transform suffices; the decision is recorded in the skill's header comment for the next editor (task 5.1).
- **The implement skill's portable variant retains the full v1 protocol** as its coexistence section rather than a compact summary: its portable-specific adapters (immutable scoped commits, lifecycle cadence, transport rules) are load-bearing for v1 users and compression would have shed rules, not verbosity (task 5.3, repo editing rule 2 applied deliberately).
- **`minSddVersion` floor advanced to 2.7.0** via `bump-version.py set-floor` (D-0021 discipline): the first release carrying every graph verb the rewritten skills name.
- **Mainline integration is a named, deliberate step after the merging sync** — the pilot walkthrough showed a merged node honestly deriving STALE until the workspace branch lands on the mainline; both skills now teach integrate-after-every-merge and record-gates-after-integration (task 5.2).
- **Gate observations bind to their artifacts** (review-07 followup discharged in 5.2): `review_of` must name a document under the plan, and one frozen artifact greens exactly one gate — cross-gate reuse refuses naming the prior gate.
- **Branch retention policy** (review-06 followup, deferred to the pilot by design): a claim branch whose tip the mainline contains and which no worktree holds is litter — gc prunes it; unmerged branches are the only reference to their work and always survive (pilot node, hazard derives-state).
- **Hazard triage for converted complete work is the explicit empty list**, with the rationale recorded in evidence: the v1 evidence trail discharged those failure classes, and claiming shaped hazard tests that do not exist would launder risk rather than triage it (task 5.4 sentinel resolution).
- **Frozen v1 phase docs are never retired by conversion of a completed plan** — the pilot relabeled phases (`v2-N-*`) so rendered views land beside the v1 history instead of over it; convert's warning makes the alternative a deliberate act, and this plan chose preservation.
- **Compile's coverage demand is scoped to the plan's own directly-related specs** (task 5.5, filed by the pilot): transitively reachable ids stay citable and fingerprinted, but only the plan's own requirement surface demands covering nodes.
- **v1 validation rules are scoped to v1 artifacts rather than waived** (task 5.6): generated views are exempt from the phases-array listing demand by their marker, and lifecycle normalization strips the generated README section — with the marker strings single-sourced in `rules` so the renderer and validator can never drift.

## Requirements Assessment
How well the phase met its acceptance criteria.

| Criterion | Status | Notes |
|-----------|--------|-------|
| Both rewritten skills drive the graph workflow end to end, no hand-written plan/phase markdown, no narrated completion | Met | Manual walkthroughs in 5.1/5.2 evidence: interview ledger, payload-compile-silhouette loop with the serial-shape re-proposal signal firing; claim-red-green-merge-integrate with red-before-green refusals observed |
| Generated trees regenerate cleanly; docs/templates agree with the verb surface; floor advanced deliberately | Met | `make plugins-check` in sync, provenance clean; six docs synced in lockstep; set-floor 2.7.0 with rationale in the commit (5.3) |
| Pilot walked two or more real nodes with red_seq recorded and clean-isolation merges; finding list recorded | Met | Both pilot nodes armed red_seq before any green counted (seq 28/30), merged clean (seq 29/31), integrated, and derive GREEN; nine-entry finding list in 5.4's evidence, every material entry filed as a task |
| `make test` green across the phase | Met | Green at the frozen phase head and again at the plan-close head |

## Deviations
What changed from the original plan and why.

- **Two tasks (5.5, 5.6) did not exist at plan approval.** Both were filed mid-phase from the pilot's findings, exactly as 5.4's own subtask instructs — material discrepancies become tasks, never silent fixes — and each landed with red observed first.
- **The phase's SDD173 waiver carries a second exception class**: recording the phase gate's own frozen review onto the committed graph necessarily post-dates the freeze, and graph observations are lifecycle-class bookkeeping the rule's classification predates (pilot observation F-10, same coexistence family task 5.6 fixed for the other two rules).
- **Two post-plan maintenance commits followed plan completion**: restoring the graph-view begin marker the evidence writer swallowed at plan close, and the structural fix — the renderer now inserts the Graph View section before the evidence section, out of reach of section-replacing writers (red observed first; golden moved with it).

## Risks & Issues Encountered
Problems hit and how they were resolved.

- **The pilot's nine-entry finding list** (full detail in task 5.4's evidence): the foreign-AC coverage flood (fixed, 5.5); the frozen-review pins broken by the README projection upsert and the projection-listing demand (fixed, 5.6); the view-filename collision with frozen v1 docs (resolved by relabeling); overlapping gate scopes from unchained phase gates (authoring guidance); the sync verb's human output omitting the merge outcome its JSON carries; heaviest-first selection burying a converted plan's new work until re-verification; the next/graph invocation-surface inconsistency; views refreshing only at compile.
- **A tenth coexistence case surfaced at plan close**: populating the plan README's evidence section swallowed the graph-view begin marker (a comment, not a heading, inside the section's replaceable extent), orphaning the projection from the lifecycle strip — four spurious frozen-pin mismatches. Repaired by hand, then fixed structurally in the renderer.
- **Evidence narration is validated text**: four validator refusals during closeouts came from my own evidence wording — failure-shaped words outside result rows, fixture ids that read as citations, a superseded ledger citation, and a non-passing result token. Each was reworded, and the lesson stands below.

## Lessons Learned
Insights to carry forward.

- **Self-hosting finds what fixtures cannot.** Every material phase-5 defect came from converting and walking the real plan — none of it surfaced in five phases of green fixture suites. The committed graph is now the living integration fixture precisely so this class keeps being exercised.
- **History grants nothing; re-verification is the converted plan's on-ramp.** A converted plan's frontier is all of its completed v1 work until observations exist. One suite run synced against every node's gate is cheap, honest, and should be the documented first move after conversion.
- **Chain phase gates.** Scope subtraction sees only gates inside the dependency closure; unchained sibling gates re-cover their ancestors. Gate N should depend on gate N-1 when disjoint incremental review scopes are the intent.
- **Comment-delimited generated blocks must live outside replaceable section extents** — or every section writer must learn their markers. The renderer-side placement fix is the cheap, robust half of that rule.
- **Write evidence like an artifact, not like chat.** The validator reads evidence prose: failure-shaped tokens, bare requirement-id-shaped strings, and retired ledger ids all mean something to it.
- **Platform footguns can silently bypass gates**: a stale `sdd.exe` shadowed the freshly built extensionless binary via Windows path resolution for a week, so `make test`'s template gate checked nothing new. The build target now removes the sibling; distrust a gate whose verdict contradicts a manual byte-diff.

## Impact on Subsequent Phases
Any changes needed to downstream phases.

- None within this plan — this was the final phase and the plan is complete.
- For future plans: the terminal gate (`gate-plan-final`) remains honestly open because no plan-level review artifact type exists — one artifact greens one gate, and phase reviews are already bound to their phase gates. A plan-close review artifact (or an explicit carry-forward convention for terminal gates) is a design follow-up worth taking up before the next graph plan closes.
- The remaining pilot observations with remedies (sync's silent merge outcome in human output, the next/graph invocation asymmetry, stale views between compiles, converted-plan re-verification guidance in the implement skill) are small follow-ups that need no plan of their own.

## Skill Opportunities
Repeated actions during this phase that would benefit from becoming a reusable skill, codebase utility, script, or saved query.

- **Phase-close choreography** — executed by hand five times across the plan, with the same three evidence-layout repairs every time (the final-review line's frozen suffix, the tool table landing inside the identities section, the planning-root-relative path form).
  - Where it belongs: the `sdd` binary — `evidence add --phase/--plan` should emit the conforming layout outright, or a `sdd phase close` orchestrator should run the sequence.
  - Why: every hand repair is a validator round-trip; the writer and the rules it must satisfy live in the same binary.
  - Rough shape: `evidence add` gains the frozen-suffix argument and places the tool table before the identities section; inputs are what the flags already carry.
- **Converted-plan re-verification pass** — one suite report synced against 23 nodes by shell loop.
  - Where: a `sdd graph reverify --plan <P> --report <file>` batch verb (or a documented recipe in the implement skill's conversion section).
  - Why: it is the standard on-ramp for every converted plan (see Lessons); the loop is mechanical and the per-node output floods with the advisory untracked bucket.
  - Rough shape: input one report (or command log + exit), fold it against every unclaimed node's gate in dependency order, print one summary table.
- **Fixture lease timestamps** — expiring-lease time bombs (dates that lapse mid-project) were defused twice across two packages.
  - Where: a tiny test helper in the graph packages (`futureLease()` / `pastLease()`).
  - Why: absolute date literals in fixtures rot; two packages independently reinvented `2099-01-01`.
- **Long inline scripts in the working shell** — two heredoc truncations corrupted a source file mid-task and cost a recovery cycle each.
  - Where: process note for agent sessions in this repo (write scripts to a temp file, then execute), not a code change.
- **Version bump at phase close** — `make bump-*` hung once early in the plan and the manual Makefile mirror became the standing workaround, repeated at every close.
  - Where: Makefile / bump tooling; diagnose the hang (suspected interaction with the test-gated prerequisite in this shell) and make `make bump-minor` reliable again.
