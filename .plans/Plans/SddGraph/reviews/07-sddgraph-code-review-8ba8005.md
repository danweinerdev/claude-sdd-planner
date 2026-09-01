---
title: "Phase review: Review Gates and Analytics"
type: review
status: resolved
created: 2026-09-01
updated: 2026-09-01
tags: [review]
related: ["Plans/SddGraph/04-Review-Gates-And-Analytics.md", "Designs/SddGraph"]
review_of: "Plans/SddGraph/04-Review-Gates-And-Analytics.md"
rev: "0976ef5bcfad89e798b6d6e712f826b23cef19eb..8ba800581a7ab1a127de5294f051d32e9b46eef3"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "8ba800581a7ab1a127de5294f051d32e9b46eef3"
review_mode: single-agent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "0976ef5bcfad89e798b6d6e712f826b23cef19eb..8ba800581a7ab1a127de5294f051d32e9b46eef3"
    evidence: "Implementation commits 6e5ae29 (4.1), 95e85c1 (4.2), 6f30d0c (4.3) traced subtask-by-subtask: all 17 subtasks implemented. One unplanned commit in range, cf44896, is a discovered-defect fix-forward, not scope drift: a stale v2.3.5 sdd.exe in build/ had shadowed every make-run binary via Windows PATHEXT resolution since Aug 26, so make test's template gate silently checked nothing new — found while cross-checking the graph-proposal schema pair during 4.1, fixed with a build-target guard plus the stale schema copy regeneration the bypassed gate should have demanded. FU-02 from review 06 discharged in 4.2 as tracked (dead usage consts deleted, staticcheck clean); FU-01 remains tracked into 5.4. 4.2's 'wire next to the real implementation' subtask is satisfied by construction — next delegated to algorithms.CriticalWeight since 3.2, and CriticalPath inlines the same recurrence."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "0976ef5bcfad89e798b6d6e712f826b23cef19eb..8ba800581a7ab1a127de5294f051d32e9b46eef3"
    evidence: "Read the full phase-4 range (~2.5k lines): the review package keeps Scope non-recursive by subtracting recorded full gates' whole closures (equivalent to the recursive definition, proven by the three-level nesting test); Record batches its refusals house-style (all three freeze signals named together, all lane problems together, all out-of-scope finding names together); demotions ride the same CAS cycle as the gate observation so no window exists where the finding is recorded but the node still reads GREEN; render's frozen-view refusal fires in preflight through the shared planWrite dry-run, so the graph write and the refusal can never disagree; analytics algorithms are pure (no store/state/model imports, verified) with deterministic sorted walks; fuzz corpora encode real attack classes and the real-emitter seeds are asserted to parse. Single-agent mode: reviewer carries implementation context, disclosed."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "0976ef5bcfad89e798b6d6e712f826b23cef19eb..8ba800581a7ab1a127de5294f051d32e9b46eef3"
    evidence: "Range checked against Designs/SddGraph obligations: DD-9 (scope derives from the graph — nested gates review disjoint increments whose union covers the closure, no diff reviewed twice; only full gates carry closure; compile refuses unknown lanes naming the closed vocabulary), D-0020 (the gate greens only from resolved + frozen + Aligned read together — the Trap's exact wording — with the reopened-artifact case tested), D-0022 (closed derives and is never stored; GREEN is assumed-closed; views project the distinction and frozen-view refusal keys on closed; full gates self-close on their own frozen review so terminal groups can freeze), DD-6 (the gate observation anchors the aggregate scope diff by content digest with VCS provenance supplementary; drift derives STALE via ordinary digest staleness), DD-14 (path prices parallelism, risk names waists, silhouette diagnoses decomposition — all read-only, all guard-allowlisted), § Structural Verification (all three named parsers fuzzed, structured errors only, corpus replay as ordinary tests)."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "0976ef5bcfad89e798b6d6e712f826b23cef19eb..8ba800581a7ab1a127de5294f051d32e9b46eef3"
    evidence: "Real catches this phase: the stale-sdd.exe PATHEXT shadowing (a silent CI-gate bypass running a week-old binary — the worst kind of green), found by refusing to accept a passing gate that contradicted a manual byte diff; the never-freezable-view gap (Closed originally never marked gate nodes, so any view containing its own gate could not freeze — fixed with gate self-closure on its own frozen review); demotion semantics pinned deliberately (a finding's nodes: field IS the demotion request; a deferred hygiene finding without it demotes nothing — otherwise this plan's own deferred-finding reviews would break GREEN nodes routinely). Probed and safe: demotion leaves claims in place (the holder continues rework); byte-identical re-renders of frozen views stay no-ops so compile remains runnable on completed plans; the explicit-delete escape for legitimately reopened phases is tested. One wrinkle deferred as F-01: nothing binds a gate to ITS review artifact."
findings:
  - id: F-01
    severity: minor
    title: "A review gate accepts any frozen Aligned artifact; nothing validates the artifact reviewed THIS gate's scope"
    status: deferred
followups:
  - id: FU-01
    finding: F-01
    summary: "When the skill rewrites script the gate flow, bind artifact to gate: validate the artifact's review_of/rev against the plan and record the gate node id in the artifact (or refuse artifacts whose rev range does not cover the scope's provenance)"
    tracked_in: "5.2"
---

# Phase review: Review Gates and Analytics

Reviewed `Plans/SddGraph/04-Review-Gates-And-Analytics.md` at frozen identity `0976ef5bcfad89e798b6d6e712f826b23cef19eb..8ba800581a7ab1a127de5294f051d32e9b46eef3`.

## Findings

### F-01 — Nothing binds a gate to its own review artifact

`sdd graph review --node G --artifact R` verifies R is resolved, frozen, and
Aligned, that its lanes satisfy G's lane set, and that finding-named nodes
lie in G's scope — but nothing verifies R was a review OF that scope. An
operator pointing gate G2 at the frozen artifact that reviewed G1 would
green G2 with G1's evidence. Mitigations today: the act is operator-
deliberate (an explicit --artifact path), the artifact's digest is recorded
on the observation so the binding is auditable after the fact, and the
aggregate scope digests still gate staleness honestly. Blind-spots lane.

## Resolution Log

### F-01 — deferred

2026-09-01 — deferred to FU-01, tracked in plan task `5.2`: the implement-
skill rewrite scripts the gate flow end to end and is the right place to
bind artifact to gate — validate `review_of`/`rev` against the plan and
scope provenance, or stamp the gate node id into the artifact at scaffold
time. Non-blocking here: the misuse requires a deliberate wrong argument,
leaves an auditable digest trail, and cannot silently persist — the next
compile's rendered views and the recorded ReportDigest both name the
artifact actually used.
