---
title: "Skill Rewrites and Self-Hosting"
type: phase
plan: "SddGraph"
phase: 5
status: in-progress
created: 2026-08-31
updated: 2026-09-01
deliverable: "The plan skill rewritten as a decomposition protocol with the structured interview, the implement skill rewritten as the walk loop, regenerated portable trees and synced docs with an advanced version floor, and the self-hosting pilot executed under graph execution"
tasks:
  - id: "5.1"
    title: "Rewrite the plan skill as a decomposition protocol with structured interview"
    status: complete
    verification: "make test — template gate, portable drift gate, and leak gate green after regeneration; manual protocol walkthrough documented in this task's evidence: the rewritten commands/plan/SKILL.md drives interview (S/M/L/XL scope gate, bounded adaptive multiple-choice waves, early ready-to-plan exit, resumable ledger under a gitignored path) -> node proposal payload authored from sdd template graph-proposal -> sdd compile -> silhouette read-back with CHAIN treated as a re-proposal signal; the skill nowhere instructs writing plan/phase markdown by hand and nowhere invents hazards on the operator's behalf."
    justifies: "DD-13 (node granularity is one red-green cycle; the skill's output is a proposal payload, not markdown), DD-16 (structured resumable interview). Prevents the token-waste loop this whole design exists to remove from planning sessions."
  - id: "5.2"
    title: "Rewrite the implement skill as the graph walk loop"
    status: complete
    verification: "make test green after regeneration; manual protocol walkthrough documented in evidence: the rewritten commands/implement/SKILL.md drives next --claim -> write the node's named tests -> observe red (sync the failing report; hazard-discharging tests must record red_seq before green counts) -> implement -> sync the passing report -> merge; stopping rules present verbatim (2 consecutive failures propose split, 3 stop and escalate); INTENT-STALE and finding-demotion responses routed as judgment steps; the skill nowhere writes completion evidence prose as a gate input and nowhere edits Graph.json or rendered views directly."
    justifies: "DD-13, D-0022 (sync-only completion — the skill teaches showing reports, never asserting), DD-5 red-before-green walk protocol. Prevents narrated completion re-entering through skill prose after the binary closed the write path."
    depends_on: ["5.1"]
  - id: "5.3"
    title: "Portable regeneration, documentation sync, version floor advance"
    status: complete
    verification: "make plugins && make plugins-check green (regenerated .codex-plugin/ and .opencode-plugin/ committed with the skill changes, no hand edits, portable variants reviewed for the changed skills); README.md, CLAUDE.md, AGENTS.md skill/agent tables and directory layouts updated consistently (grep spot-checks for the new graph verbs and rewritten skill descriptions); python3 bump-version.py set-floor advances minSddVersion to the release carrying the graph verbs, per the deliberate-floor discipline; sdd plugin status reports clean provenance."
    justifies: "D-0021 (floor advanced deliberately, never by make bump-*), D-0017 (per-harness generated trees are the published artifact — regeneration is mandatory with skill edits), and the docs-stay-in-sync repo rule; prevents harness caches serving stale skills against a binary whose verbs they don't know."
    depends_on: ["5.1", "5.2"]
  - id: "5.4"
    title: "Self-hosting pilot: convert this plan and walk a live slice"
    status: complete
    verification: "Documented pilot run in this task's evidence, executed with the released binary: sdd graph convert --plan SddGraph produces a staged proposal whose sentinels are resolved via the payload path; sdd compile succeeds with the coverage invariant satisfied; at least two nodes corresponding to real remaining work from this phase are executed end to end via next --claim -> red sync -> green sync -> merge with clean isolation and red_seq recorded; sdd graph status/path/shape output captured; every discrepancy between the design's claims and observed behavior is filed as a finding list in the evidence (empty list is a pass, silence is not)."
    justifies: "The design's Testing Strategy names self-hosting as acceptance: the first real plan executed under SddGraph is a slice of its own implementation plan. DD-15 (convert exercised on a real v1 plan, not only fixtures). Prevents shipping a walk loop that has only ever walked fixtures."
    depends_on: ["5.3"]
  - id: "5.5"
    title: "Scope compile's AC-coverage demand to the plan's own specs"
    status: complete
    verification: "go test ./internal/graph/compile/ -run TestACCoverage -count=1 — ACs of specs DIRECTLY related in the plan README still demand covering nodes (existing fixture behavior unchanged); ACs of specs reachable only transitively (via a design's related graph) demand no coverage, while their FR/NFR/AC/DD ids remain citable and fingerprinted; the pilot's reconnaissance compile of the converted SddGraph plan drops its 47 foreign-AC findings."
    justifies: "DD-4 (coverage is an exit code over the plan's OWN requirement surface); pilot finding F-01 from task 5.4 (filed per its subtask: material discrepancies become new tasks, never silent fixes). Prevents every graph plan in a multi-plan root being refused for acceptance criteria owned by other plans' completed specs."
    depends_on: []
  - id: "5.6"
    title: "v1 validation coexists with graph projections"
    status: complete
    verification: "go test ./internal/rules/ -run 'TestLifecycleNormalization|TestPhaseOwnership' -count=1 plus make gen-fixtures and the frozen corpus committed — SDD163 exempts phase docs carrying the GENERATED VIEW marker (projections are owned by the graph, not the README phases[] array); SDD174's lifecycle normalization strips the marker-delimited graph-view section from plan READMEs so compile's projection upsert does not invalidate frozen phase reviews; the pilot's nine coexistence errors clear with zero waivers."
    justifies: "D-0022 (v1 plans keep the markdown protocol until converted — coexistence is a standing state, not a transition instant); pilot findings F-02/F-03 from task 5.4 (filed per its subtask). Prevents graph adoption on any plan with a completed v1 phase from invalidating that phase's frozen review pin."
    depends_on: ["5.5"]
---

# Phase 5: Skill Rewrites and Self-Hosting

## Overview

Moves the new execution model from binary capability to lived workflow: the
planning skill becomes a decomposition protocol emitting proposal payloads,
the implementation skill becomes the claim/red/green/sync walk loop, the
portable trees and docs regenerate in lockstep with an advanced version
floor, and the plan proves itself by converting and walking a slice of its
own remaining work. Tool-side enforcement has been live since phase 3; this
phase lands the prose that narrates it (the design's rollout note).

## 5.1: Rewrite the plan skill as a decomposition protocol with structured interview

### Subtasks
- [x] Rewrite `commands/plan/SKILL.md`: interview protocol (S/M/L/XL scope
      gate sets per-wave question budget; bounded multiple-choice waves;
      adaptive continuation until no material assumptions remain; every
      wave offers a ready-to-plan exit; answers persist to a resumable
      ledger under a gitignored path).
- [x] Decomposition section: node granularity = one red→green cycle; 1–2
      sentence falsifiable contracts; named failing tests per node; declared
      artifact sets; hazard triage against `sdd graph hazards` (explicit
      `--no-hazards`-equivalent claim, never silent); estimates; feature
      review-gate placement at integrators with the terminal-gate backstop.
- [x] Output contract: author the payload from `sdd template
      graph-proposal`, `graph propose`, `compile`, read back
      `graph shape` — CHAIN triggers re-proposal; document the repair loop
      as file edits against JSON-path findings.
- [x] Decide and apply the portable-variant question: does
      `commands/plan/SKILL.portable.md` (if introduced) or harness markers
      handle divergence; keep the decision recorded in the skill header
      comment.
- [x] v1 coexistence paragraph: plans without graphs continue under the old
      protocol until converted (D-0022's v1 clause) — the skill routes by
      graph presence.

### Notes
Revision boundary: the canonical skill file rewritten and self-consistent;
portable regeneration is 5.3 (do not hand-edit generated trees here). The
skill's job shrinks to the three LLM-shaped tasks the design names:
negotiate intent, decompose, and read back diagnostics — everything
mechanical is a CLI call. Design references: DD-13, DD-16; § The execution
loop. Keep the interview ledger path inside the plan's `.graph/` area so
init's `.gitignore` already covers it.

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `922b2a1dbcf57a22019f3d55e9e2fc069cb2482f`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `922b2a1dbcf57a22019f3d55e9e2fc069cb2482f`
- Focused review: `git show 922b2a1dbcf57a22019f3d55e9e2fc069cb2482f`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `922b2a1dbcf57a22019f3d55e9e2fc069cb2482f`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `template gate (10 templates), portable drift gate, and leak gate all green after regeneration: make plugins regenerated both portable trees for the rewritten skill (53 generated, 5 variants, 0 overridden), no failing tests anywhere in the full suite, and a leak scan of the generated sdd-plan skill found no harness-isms (agent prefix transformed, /plan trigger stripped)` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `manual protocol walkthrough (CLI, temp git repo, built binary at 922b2a1)` | `drove the rewritten skill's exact sequence: interview ledger under Plans/<P>/.graph/interview.json, sdd template graph-proposal --out, propose, compile, shape read-back` | PASS | `ledger confirmed gitignored by init's .gitignore (git check-ignore); a deliberately serial decomposition compiled then read back silhouette CHAIN with the 'prices zero parallelism' hint — treated as the re-proposal signal per skill step 6 — and the re-proposed independent decomposition read back FLAT with ceiling 1.50x; rendered views carry the GENERATED VIEW marker (never hand-written); the skill text nowhere instructs writing plan/phase markdown by hand and nowhere invents hazards (triage named as interview-grade judgment, empty list explicit-only)` |

### Trap
Do not carry the old skill's "3-7 phases, 2-6 tasks" shape language into
node decomposition. Node count follows from red→green cycles, not from a
document shape; importing the old quotas recreates order-of-thought CHAIN
decompositions the silhouette check exists to reject.

## 5.2: Rewrite the implement skill as the graph walk loop

### Subtasks
- [x] Rewrite `commands/implement/SKILL.md` around the loop: `next --claim`
      → write named tests → run and **sync the failing report** → implement
      → sync the passing report → merge; repeat until the frontier is
      empty; surface `graph status` between rounds.
- [x] Stopping rules verbatim: 2 consecutive failures → propose `split`;
      3 → stop and escalate to the user.
- [x] Reaction protocol: INTENT-STALE (re-read the changed requirement diff
      only; re-hash / rework / replan as judgment), finding-demotion (RED
      nodes re-enter the frontier), lease expiry and `release` etiquette.
- [x] Evidence language: rendered views and observation records replace
      narrated completion-evidence tables for graph plans; v1 plans keep the
      old protocol (routing by graph presence, as in 5.1).
- [x] Update `agents/code-implementer.md` dispatch expectations if the
      inline-task contract (implement_task dispatches carry the task
      inline, per the repo's agent-prompt conventions) needs graph-node
      payloads.

### Notes
Revision boundary: canonical implement skill rewritten; portable
regeneration in 5.3. The prose must never instruct asserting completion —
the binary refuses it anyway (DD-5), but skill text that implies otherwise
trains the model to fight the tool. Design references: § The execution loop,
§ Stopping rules, DD-5, DD-10, D-0022.

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `aa914086a9d132a6db6be8315ccd5c20e6005f6b`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `aa914086a9d132a6db6be8315ccd5c20e6005f6b`
- Focused review: `git show aa914086a9d132a6db6be8315ccd5c20e6005f6b`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `aa914086a9d132a6db6be8315ccd5c20e6005f6b`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `full gate green after regeneration: template gate 10 templates, portable drift and leak gates clean (the implement skill's .portable.md variant deliberately shadows generation until 5.3's variant decision; code-implementer has no derived prompt by design (implement_task dispatches carry the task inline), so both trees are unchanged and provenance stays clean); review package green including the three new gate-binding refusals (missing review_of, wrong plan, cross-gate reuse naming the prior gate); cmd/sdd suite green; the skill text contains the stopping rules verbatim (2 propose split, 3 stop and escalate), routes INTENT-STALE and finding-demotion as judgment steps, and nowhere writes completion-evidence prose as a gate input nor edits Graph.json or rendered views` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `manual walk-loop walkthrough (CLI, temp git repo, built binary at aa91408)` | `drove the rewritten skill's exact sequence on a hazard-carrying node: next --claim, red sync, workspace implement+commit, green sync, mainline integration, gate recording` | PASS | `claim payload printed contract/cited-text/tests/hazards/workspace as the skill promises; the named hazard-discharging test was observed red first (red_seq armed at seq 1); the clean green merged atomically (claim cleared, workspace released); the node then honestly derived STALE from the shared tree until the surviving workspace branch was integrated into mainline — the discovered integration step now documented in the skill — after which status read GREEN=2 closed=2/2; the gate-binding refusal fired on a wrong-plan artifact naming the mismatch and the correct artifact greened exactly one gate` |

## 5.3: Portable regeneration, documentation sync, version floor advance

### Subtasks
- [x] `make plugins` after 5.1/5.2; review generated diffs for the two
      rewritten skills; check whether any `.portable.md` variant shadows
      them and update variants deliberately (repo editing rule 2).
- [x] `make plugins-check` green; `sdd plugin status` provenance clean.
- [x] Sync README.md, CLAUDE.md, AGENTS.md: skill tables, `sdd` verb
      surface (graph family), lifecycle description mentioning graph
      execution and v1 coexistence; setup-generated guidance templates
      (`shared/templates/claude-md-*.md`, `agents-md-*.md`) updated to
      match.
- [x] `python3 bump-version.py set-floor <release>` advancing minSddVersion
      to the first release carrying the graph verbs (deliberate act with
      rationale in the commit).
- [x] Full `make test` as the phase gate.

### Notes
Revision boundary: generated trees, docs, and floor move in one revision so
no harness can install skills that name verbs the admitted binary lacks.
Design references: § Migration/Rollout phase 5; D-0021 (floor discipline);
repo editing rules 1, 2, 9. The version bump itself (make bump-*) is a
release action taken with the user at the boundary — this task prepares
everything the bump publishes.

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `a49e6e5fab59a53c533b2c0af4ec17f18c4c5b63`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `a49e6e5fab59a53c533b2c0af4ec17f18c4c5b63`
- Focused review: `git show a49e6e5fab59a53c533b2c0af4ec17f18c4c5b63`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `a49e6e5fab59a53c533b2c0af4ec17f18c4c5b63`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `full gate green as the phase-gate subtask requires: template gate 10 templates, portable drift gate, leak gate, and the entire go suite with zero failing lines; make plugins-check confirms both generated trees in sync with the canonical tree after regeneration; sdd plugin status reports clean provenance (53 generated, 5 variants, 0 overridden); minSddVersion advanced deliberately to 2.7.0 via python3 bump-version.py set-floor (never make bump-*), the first release carrying every graph verb the rewritten skills name` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `generated-diff review + doc grep spot-checks` | `the two rewritten skills' portable outputs and the six synced docs at a49e6e5` | PASS | `sdd-plan portable output is the pure generated transform (5.1's recorded no-variant decision); sdd-implement portable output comes from the deliberately rewritten variant carrying the walk loop with the v1 protocol retained for its portable-specific adapters; grep spot-checks confirm the graph verb family named in AGENTS.md and CLAUDE.md binary-contract sections, mirrored invariants paragraphs in both (graph plans tighten mechanically; v1 until converted), rewritten skill rows in README/CLAUDE/claude-md templates, and the graph-vs-v1 routing paragraph in agents-md-full` |

## 5.4: Self-hosting pilot: convert this plan and walk a live slice

### Subtasks
- [x] `sdd graph convert --plan SddGraph`; resolve sentinels through the
      payload path (hazard triage and gate specification are real judgments
      — make them, don't default them).
- [x] `sdd compile` to a committed `SddGraph-Graph.json`; capture
      `status`/`path`/`risk`/`shape` output.
- [x] Execute at least two genuinely remaining nodes (documentation
      follow-ups, corpus additions, or fix-forward work discovered by the
      pilot) end to end under `next --claim` → red sync → green sync →
      merge.
- [x] Record every design-vs-behavior discrepancy as a finding list in this
      task's evidence; file material ones as new tasks or design follow-ups
      rather than fixing silently.
- [x] Leave the converted graph committed as the living integration fixture
      (design § Testing Strategy).

### Notes
Revision boundary: the pilot run, its committed graph, and its finding list.
This is the acceptance test the design sets for itself — the walk loop must
walk real work, with the released binary, in this repository, before the
plan closes. If the pilot finds nothing at all, say so explicitly in
evidence; an empty finding list is a result, silence is not. Design
references: § Testing Strategy (self-hosting), DD-15, D-0022.

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `2cb4e4c09dedfec9b3be0d29d83d1f2ad73ed5c3`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `2cb4e4c09dedfec9b3be0d29d83d1f2ad73ed5c3`
- Focused review: `git show 2cb4e4c09dedfec9b3be0d29d83d1f2ad73ed5c3`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `2cb4e4c09dedfec9b3be0d29d83d1f2ad73ed5c3`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd compile --plan SddGraph` | `.` | PASS (`exit 0`) | `the converted plan compiled: 31 nodes into SddGraph-Graph.json with 46 intent fingerprints embedded and the coverage invariant satisfied — after resolving every sentinel through the payload path as real judgments (22 falsifiable contracts, gates naming genuine runner-visible test ids, explicit empty-list hazard triage for completed v1 work with the rationale that its failure classes were discharged by the v1 evidence trail, phase labels deliberately relabeled v2-N-* so rendered views land beside the frozen v1 docs instead of over them) plus five phase gates, the terminal gate, a node for task 5.5, and the two pilot work nodes` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `pilot walk (sdd 2.7.0 plus the in-tree fixes the pilot itself filed as 5.5/5.6)` | `the full loop against the real planning root: re-verification pass, gate recordings from frozen history, two claim-red-green-merge-integrate walks, live gc` | PASS | `re-verification greened all 23 converted nodes from real reports (one go test -json stream for 19 tests gates, one make test log for 4 command gates — history granted nothing, observations did); gates v2-1 through v2-4 recorded from the four real frozen Aligned reviews with scope derivation printed; both pilot nodes walked with genuine red phases — the gc-branch-pruning walk armed red_seq at seq 28 and merged clean at seq 29 anchored to its workspace commit, the junit-vendor walk armed at seq 30 and merged at seq 31 — each integrated to mainline and deriving GREEN; live sdd graph gc then pruned exactly the two merged pilot branches (the first pilot node's own feature reaping its own walk); final derived status GREEN=29 closed=22 of 31 with the phase-5 and terminal gates honestly open awaiting their reviews` |
| focused review of the first pilot slice | `git show 0c2f8d445806fe469d111fd0400853569ebae7e7` | PASS | `gc branch pruning reviewed for correctness, scope, tests, and boundary: prune set derived from git ancestry and checkout state, never graph bookkeeping; unmerged and checked-out branches proven surviving` |
| analytics capture | `sdd graph status/path/risk/shape --plan SddGraph` | PASS | `pre-walk: 27 BLOCKED / 4 READY, critical path 26 of 37 units ceiling 1.42x, one cut vertex (the phase-5 gate), silhouette MIXED with a long near-chain tail from convert's dense phase-order deps; post-walk: GREEN=29 closed=22/31` |
| pilot finding list — design vs observed behavior (the subtask's required record; not empty) | nine entries | RECORDED | `F-01 compile's coverage demand flooded by 47 foreign acceptance criteria reachable only transitively, filed and fixed as task 5.5; F-02 the README graph-view upsert read as changed plan intent by all four frozen phase reviews' pins, filed and fixed as task 5.6; F-03 rendered projection docs tripped the phases-array listing rule, filed and fixed as task 5.6; F-04 rendered views target the v1 phase-doc filenames (convert warns) and retiring frozen v1 history was unacceptable, resolved by operator relabeling in the payload; F-05 unchained phase gates derive overlapping scopes because scope subtraction sees only gates inside the dependency closure — authoring guidance is to chain phase gates for disjoint increments, mechanics behaved as designed; F-06 the sync verb's human output omits the merge outcome its JSON carries, minor UX follow-up; F-07 heaviest-first claim selection leaves a freshly converted plan's frontier dominated by already-complete v1 work until re-verified — the re-verification pass is the honest remedy and belongs in converted-plan guidance; F-08 next takes a filesystem path while graph verbs take plan names, an invocation-surface inconsistency; F-09 rendered views refresh only at compile, so a walk leaves them stale until the next compile` |

### Trap
The pilot will tempt you to hand-pick two trivial already-green nodes so the
walk "passes". The pilot's value is adversarial: pick nodes with real red
phases and at least one hazard-discharging test, or the red-before-green and
demotion machinery ships having never fired outside fixtures.

## 5.5: Scope compile's AC-coverage demand to the plan's own specs

### Subtasks
- [x] Red test: a spec reachable only transitively (plan -> design -> spec)
      carries an unchecked AC; compile must not demand its coverage; the AC's
      id must still resolve as a citation. Observe the test failing against
      current behavior first.
- [x] `rules` API: expose the DIRECT related sources of an artifact (the
      plan README's own `related` list, resolved) alongside the existing
      transitive walk.
- [x] `compile.identifierSources`: collect `acIDs` only from directly
      related specs; `items` (citable ids + fingerprints) stay transitive.
- [x] Re-run the pilot's reconnaissance compile: the 47 foreign-AC findings
      disappear; the 22 nodes' sentinel findings remain (they are the
      operator's real judgments, not this task's scope).

### Notes
Filed from the self-hosting pilot (5.4 finding F-01): Plans/SddGraph reaches
Specs/SDD-Toolchain only through Designs/SddGraph's background citation, and
compile demanded coverage of all 47 of that complete plan's unchecked ACs.
The v1 validator has the same reach and absorbs it with SDD160 waivers;
compile has no waiver mechanism by design, so the scoping must be right
rather than waivable. Revision boundary: the rules API export, the compile
scoping change, and the tests — nothing else. Design references: DD-4;
§ Coverage invariant.

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `074d598ff1cb37917e1d9ac051fc3f4d33850d7f`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `074d598ff1cb37917e1d9ac051fc3f4d33850d7f`
- Focused review: `git show 074d598ff1cb37917e1d9ac051fc3f4d33850d7f`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `074d598ff1cb37917e1d9ac051fc3f4d33850d7f`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/graph/compile/ -run TestACCoverage -count=1` | `.` | PASS (`exit 0`) | `green after red was observed first: the new test initially refused with exactly the foreign-AC finding (coverage demanded for the fixture criterion of a spec reachable only plan->design->spec), then passed under the scoping; the test also proves transitive ids stay citable AND fingerprinted (the node citing the foreign spec's fixture requirement resolves and its intent hash embeds); full sweep of internal/graph, internal/rules, and cmd/sdd suites green; go vet clean; staticcheck clean on the touched packages (six U1000s in untouched internal/rules files are pre-existing debt, noted for the phase review)` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `pilot reconnaissance compile (built binary at 074d598)` | `sdd compile --plan SddGraph against the real planning root with the converted 22-node proposal staged` | PASS | `findings drop from 136 to 89 with zero foreign-AC refusals remaining; every surviving finding is a real operator judgment — the 22 converted nodes' contract/gate/hazard sentinels plus the full-gate coverage backstop, and task-4-3's empty justifies (its v1 prose carried no extractable ids) — which is exactly the set task 5.4's sentinel resolution owns` |

## 5.6: v1 validation coexists with graph projections

### Subtasks
- [x] Red tests: a plan README differing from its historical copy only by the
      marker-delimited graph-view section must lifecycle-normalize
      identically (SDD174 path); a phase doc carrying the GENERATED VIEW
      marker and listed by no README must emit no SDD163. Observe both
      failing against current behavior first.
- [x] Move the three view-marker strings to exported `rules` constants
      (byte-identical to what render.go already emits — existing rendered
      views must keep being recognized); render.go consumes them.
- [x] SDD163: skip generated views; add the Good example; `make gen-fixtures`
      and commit the corpus.
- [x] SDD174: strip the graph-view section (symmetric on both sides of the
      comparison) in plan-README lifecycle normalization.
- [x] Re-validate the planning root: the pilot's nine errors clear.

### Notes
Filed from the self-hosting pilot (5.4 findings F-02/F-03): compiling the
converted plan upserted the README's generated Graph View section — which
SDD174's frozen-review pin read as changed plan intent for all four
completed phases — and rendered five projection docs SDD163 demanded be
listed in the README phases[] array v1 owns. Both rules predate projections;
both fixes scope v1 rules to v1 artifacts rather than waiving. Revision
boundary: rules constants + two rule changes + tests + regenerated corpus.
Design references: D-0022 v1 clause; DD-2 (views are projections).

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `178e13523f23423bfed3c1449deb4c37377239cb`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `178e13523f23423bfed3c1449deb4c37377239cb`
- Focused review: `git show 178e13523f23423bfed3c1449deb4c37377239cb`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `178e13523f23423bfed3c1449deb4c37377239cb`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/rules/ -run 'TestLifecycleNormalization|TestPhaseOwnership' -count=1` | `.` | PASS (`exit 0`) | `green after both reds were observed first: the normalization test initially diverged (the generated Graph View section read as changed intent) and the ownership test initially emitted the listing demand for a marker-carrying view; under the fix a README differing only by the generated section normalizes identically, a real prose change still survives normalization, the generated view is exempt while a rogue non-generated unlisted doc still fires; full sweep green including the regression corpus (untouched by design — it materializes Bad examples only; the new Good example rides the registry meta-test); go vet clean; package staticcheck clean apart from the six pre-existing internal/rules findings noted in 5.5` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `validation of the real pilot artifacts (built binary at 178e135)` | `sdd validate --scope Plans/SddGraph against the real planning root carrying the compiled 31-node graph, five rendered views, and the upserted README section` | PASS | `the pilot's nine coexistence errors clear with zero waivers — the root reports Valid; the root-cause ordering bug (the evidence-section normalizer swallowing the begin marker, an HTML comment rather than a heading) was found by diffing the real pinned README against the real current one and is pinned by the reorder comment` |

## Acceptance Criteria
- [ ] Both rewritten skills drive the graph workflow end to end with no
      hand-written plan/phase markdown and no narrated completion path
      (DD-13, DD-16, D-0022).
- [ ] Generated trees regenerate cleanly; docs and templates agree with the
      shipped verb surface; minSddVersion floor advanced deliberately
      (D-0021).
- [ ] The self-hosting pilot walked ≥2 real nodes with red_seq recorded and
      clean-isolation merges; its finding list is recorded (empty allowed,
      absent not).
- [ ] `make test` green across the phase.

## Phase Completion Evidence

<!-- Keep the exact `Pending — not complete.` line until completion. -->
Pending — not complete.
