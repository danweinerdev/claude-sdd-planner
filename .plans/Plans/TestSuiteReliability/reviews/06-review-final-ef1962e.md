---
title: "Phase review: Acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
    evidence: "drift-detector (agent, whole-plan final gate, delta 56815db..ef1962e of 14 files in commits 789a639 and ef1962e; HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; a Python check over the graph JSON confirmed every file in each commit is a declared artifact of its owning node with zero undeclared files; a sha256 recomputation of all 325 digest entries across the 21 nodes via git show at HEAD found 10 mismatches, all in the three review gates' stale snapshots of root.go, compile.go, evidence.go, operational_test.go and phasereview.go whose seq 450, 430 and 449 predate the amendment nodes' seq 460 and 471, the expected mid-round state; sdd graph status read-only reported GREEN 18, STALE 3, closed 0 of 21 with only the review gates not green; internal/store untouched so the compare-and-swap reuses pre-existing primitives; go build and go vet clean; gofmt on the three touched packages empty; go test over internal/rules, cmd/sdd and internal/graph/compile ok plain and under -race -count=2 -shuffle=on; all 10 named gate tests pass under explicit -run; both rev 8 and rev 7 contracts compared clause by clause against the diffs; the prior final review's F-01 resolution is confirmed by the revised contract text; grep for windows, telemetry and budget over the delta empty and no version-bump files). No missing work, no scope creep, no approach drift. Verdict Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
    evidence: "quality-scanner (agent, intent-blind, whole-tree final gate on delta 56815db..ef1962e of 14 files read in full; HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; gofmt -l . empty; go vet ./... clean; staticcheck ./... with caches under TMPDIR reports the pre-existing operational_test.go:795 dead store outside the delta and one in-delta S1039 at compile/evidence.go:161; GOOS=windows and darwin arm64 builds exit 0; make test with GOCACHE under TMPDIR green for every package; go test -race -count=2 -shuffle=on over internal/rules, internal/graph and cmd/sdd failed once on TestGraphConcurrentClaimStress in cmd/sdd/graph_stress_test.go, a file untouched by the delta with no reap or gc code in the diff, then passed 3 of 3 targeted reruns and a second full shuffled run, so treated as pre-existing flakiness; reviewed the per-Root cache and document-scoped exemption, the SDD157 move to CheckRoot with the old Check removed and evaluate.go running CheckRoot once per rule, the readReviewDir and readReviewFile seams, the injectable now clock, the last-occurrence anchor with the upgrade-only guard, and the compare-and-swap flip against store.go's documented contract; vcs.Detect never returns nil and Unavailable's RevisionExists wraps ErrOperational so resolveEvidenceRepo cannot swallow a detection failure; no dead code or duplicated helpers; the lane's own GOFLAGS=-mod=mod setting reformatted go.mod, which the orchestrator discards). Two Minor findings: the S1039 fmt.Sprintf on a static string, and the identity line naming an abstract probe where the adjacent table names literal commands. Verdict Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
    evidence: "spec-compliance (agent, whole-plan final gate against Specs/TestSuiteReliability and Designs/TestSuiteReliability scoped to workstreams A, B, C and POSIX E; HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; the delta's 14 files inventoried; tools/parity and tools/regression/fixtures diff empty; go test ./... with -count=1 green for every package; go vet ./... clean; no in-scope test weakened, the one changed assertion in compile/evidence_test.go reflects the no-repo identity line and is backed by TestRenderedIdentityLineReportsRealCheck against a real repository; the DD-10 closing inventory over graphviews.go, compile/evidence.go, render.go and graph_complete.go confirms the five previously folded reads now distinguish not-exist from operational failure and each has a covering test, and the day-stable date, frozen-view anchor and compare-and-swap flip are each covered; read in full graphviews.go, compile/evidence.go, render.go and graph_complete.go; grep shows planGraphIDs and planGraphJustifies are called from reviews.go:1178 and traceability.go:145 and appear in no test file; retirement.go:175-210 carries an explicit SDD180 deferral comment for its analogous continue). One Major coverage gap: planGraphIDs at graphviews.go:148-171 and planGraphJustifies at 179-198, pre-existing code in the same file this delta fixed, still fold any ReadFile or Unmarshal failure into a negative answer with no operational record, so a permission failure on the graph file downgrades a valid graph-node citation to an unknown task or silently falls back to the v1 harvest. Verdict Needs amendment."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
    evidence: "blind-spot-finder (agent, diff only, whole tree 56815db..ef1962e of 1637 diff lines across 14 files, every changed file read in full and callers walked for graphPlanDir, isGraphPlan, writeReadmeStatusComplete, renderPhaseDoc, resolveEvidenceRepo, identityRecheckLine and stripPhaseEvidenceSection; HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; also read root.go, evaluate.go lines 52-69, store.go 65-130, vcs.go and git.go 118-124; go build and go vet clean; go test over cmd/sdd, internal/graph and internal/rules with -race -shuffle=on -count=2 ok; verified the cache does not store the operational branch and the evaluator aborts before the next callback, the day-stability placeholder logic, the last-match anchor and its adversarial test, and the lock-then-recheck compare-and-swap span; no file created in the repository). Findings: Major, renderPhaseDoc now runs a live revision-exists subprocess for every closed phase in both the preflight and the write pass, before planWrite's byte-stability short-circuit, so a plan with N closed phases spawns 2N probes on every compile or complete and a transient git failure fails a would-be no-op render; Minor, the no-recheck line and the frozen-view refusal do not distinguish a lost repository from genuine drift. Verdict Needs amendment."
findings:
  - id: F-01
    severity: major
    title: "planGraphIDs and planGraphJustifies still fold read failures into absence"
    status: answered
  - id: F-02
    severity: major
    title: "The identity probe runs for every closed phase on every render, including no-op renders"
    status: answered
  - id: F-03
    severity: minor
    title: "The no-recheck line and the frozen-view refusal do not name a lost repository as the cause"
    status: answered
followups: []
---

# Phase review: Acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c`.

## Findings
### F-01 — planGraphIDs and planGraphJustifies still fold read failures into absence

`internal/rules/graphviews.go:148-171` and `:179-198`, pre-existing code in the file this round fixed, turn any read or parse failure of the graph file into a negative answer with no operational record; they gate the review citation check at `reviews.go:1178` and the traceability graph branch at `traceability.go:145`, and no test covers the failure path.

### F-02 — The identity probe runs for every closed phase on every render, including no-op renders

`renderPhaseDoc` is called from both the preflight and the write pass, so a plan with N closed phases spawns 2N revision probes on every compile or complete, and a transient git failure now fails a render that would otherwise be a byte-identical no-op.

### F-03 — The no-recheck line and the frozen-view refusal do not name a lost repository as the cause

A checkout without its repository renders the no-recheck line, which differs from the frozen matched line, and the refusal message says only that the graph disagrees with frozen history.

## Resolution Log
### F-01 — answered

2026-09-13: Code-only inside propagation-validator's rev 8 collector discipline; both helpers record a non-not-exist failure on the Root in the inline follow-up commit, with a fault-injection test.

### F-02 — answered

2026-09-13: Code-only inside propagation-graph-callers' rev 7 clause; the probe result is memoized per revision within one render and skipped when the render would be a no-op, in the same follow-up commit.

### F-03 — answered

2026-09-13: The refusal message names the missing repository when that is the cause, in the same follow-up commit.
