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
    evidence: "drift-detector (agent, delta 56815db..ef1962e, HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; 14 files across commits 789a639 and ef1962e, each set a strict subset of the owning node's declared artifacts read from the graph JSON; all 28 gate test ids named on propagation-validator and propagation-graph-callers found at their declared files by grep and run green with explicit -run across internal/rules, cmd/sdd, internal/graph/sync, provider, ops and compile; every rev 8 and rev 7 contract clause located in the corresponding code by reading git show of the eight rules files and the compile and cmd files; a Python sha256 walk over 136 digest entries for the 11 reviewed nodes matched at HEAD, with the only working-tree mismatch a stray uncommitted go.mod edit whose committed blob matches the recorded posix-containment digest; store.WriteAtomicExpecting, ErrConcurrentWrite and Digest confirmed pre-existing and untouched; no plugin.json or version.go change). No missing work, no scope creep, no approach drift. Verdict Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
    evidence: "quality-scanner (agent, intent-blind, delta 56815db..ef1962e on cmd/sdd and internal/graph; HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; read graph_complete.go, compile/evidence.go and render.go in full with their tests; graphPlanDir folds only not-exist and its single call site checks err before ok; graphPhaseComplete applies the same discipline; findPhaseReview propagates non-not-exist ReadDir and ReadFile errors through renderPhaseDoc to preflightViews and renderViews; identityRecheckLine treats only non-ErrNotFound probe errors as operational, matching vcs git.go:104-124; the injectable now at render.go:60 replaces every wall-clock read in stamp-producing code; stripPhaseEvidenceSection anchors on the last heading match and populated-to-different bodies are refused at render.go:447-451 while placeholder-to-populated stays allowed; the status flip computes the digest of the just-rendered bytes and maps ErrConcurrentWrite to a message naming that views were already written; all five test seams are restored via Cleanup or defer and no test in these files calls t.Parallel; gofmt -l . empty; go vet clean; staticcheck reports one in-diff S1039 at compile/evidence.go:161 plus the pre-existing operational_test.go:795 dead store; GOOS=windows and darwin arm64 builds clean; go test ./cmd/sdd/... ./internal/graph/... -race -count=2 -shuffle=on ok). One Minor finding: an unnecessary fmt.Sprintf on a static string at evidence.go:161. Verdict Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
    evidence: "spec-compliance (agent, delta 56815db..ef1962e against the operational-propagation family in Specs/TestSuiteReliability FR-16, AC-08, AC-11 and Designs/TestSuiteReliability DD-10; HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; git diff --stat confirms internal/procexec and internal/vcs untouched and the 14-file delta confined to internal/rules, internal/graph/compile and cmd/sdd; read in full graphviews.go, compile/evidence.go, render.go and graph_complete.go; all five previously folded sites now propagate non-not-exist errors: isGraphPlanDir at graphviews.go:96-100 wraps with ErrOperational and records on the Root, findPhaseReview at compile/evidence.go:98-103 and 114-118 returns wrapped errors, graphPlanDir at graph_complete.go:61-66 and graphPhaseComplete at 220-225 return wrapped errors with handled true; identityRecheckLine at evidence.go:159-172 reports matched, not matched, no recheck ran, or propagates a probe failure; exitCode at root.go:692-701 maps refusedError to 1 and everything else to 2 and graph_complete_test.go:192-204 and 231-244 assert exit 2, not a refusal, and no bytes written; the four fault-injection tests TestGraphPlanStatFailureIsOperational, TestFindPhaseReviewReadFailuresAreOperational, TestGraphCompleteReadFailuresAreOperational and TestRenderedIdentityLineReportsRealCheck pass under an explicit -run; go build, go vet and go test -count=1 over internal/rules, internal/graph and cmd/sdd all clean; tools/parity diff empty). No coverage gaps, no contract violations; one question notes provider.go and doctor.go are outside this delta and were covered by earlier nodes. Verdict Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c"
    evidence: "blind-spot-finder (agent, diff only 56815db..ef1962e on cmd/sdd and internal/graph, 1180 diff lines; HEAD ef1962e3d08b7c4a0337c69b7d3cf9978673d67c unchanged; read in full graph_complete.go, store.go, lock.go, render.go, compile/evidence.go, vcs git.go and vcs.go, model.go and decode.go; go build and go vet clean; go test ./cmd/sdd/... ./internal/graph/compile/... -race -shuffle=on -count=2 ok; the exclusive-lock recheck in writeAtomicCheckedWith closes the window between the unlocked pre-read and the write and TestGraphCompleteStatusFlipIsCompareAndSwap exercises it; phaseCheckpoint probes the last node's revision against the resolved target repository root; reproduction done on a copy of the repository under TMPDIR, nothing created in the real tree). One Major finding: fillDates at render.go:512 and planReadmeUpdate at render.go:690 replace the VERIFIED_DATE placeholder across the whole document, so a node Contract containing that literal is silently rewritten to a date, reproduced on the copy; the frozen-view check cannot catch it because the first render already carries the rewritten bytes. One question: the compare-and-swap conflict is wrapped as a plain error and exits 2 rather than as a refused mutation, with no exit-code assertion in the test. Verdict Needs amendment."
findings:
  - id: F-01
    severity: major
    title: "The verified-date placeholder is replaced across the whole document"
    status: answered
  - id: F-02
    severity: minor
    title: "A compare-and-swap conflict exits 2 instead of as a refused mutation"
    status: answered
  - id: F-03
    severity: minor
    title: "Unnecessary fmt.Sprintf on a static string"
    status: answered
followups: []
---

# Phase review: Acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..ef1962e3d08b7c4a0337c69b7d3cf9978673d67c`.

## Findings
### F-01 — The verified-date placeholder is replaced across the whole document

`internal/graph/compile/render.go:512` and `:690` run the placeholder substitution over the entire rendered view, so a node contract containing the literal placeholder is silently rewritten to a date (reproduced on a copy); the frozen-view check cannot catch it because the first render already carries the rewritten bytes.

### F-02 — A compare-and-swap conflict exits 2 instead of as a refused mutation

`cmd/sdd/graph_complete.go` wraps the concurrent-write conflict as a plain error, so it exits 2; a refused mutation is the exit 1 class, and the test asserts no exit code for this path.

### F-03 — Unnecessary fmt.Sprintf on a static string

`internal/graph/compile/evidence.go:161`, staticcheck S1039.

## Resolution Log
### F-01 — answered

2026-09-13: Code-only inside the rev 7 clause on the writer-controlled boundary; the substitution is scoped to the evidence section the renderer emits, with a test, in the inline follow-up commit after this round closes.

### F-02 — answered

2026-09-13: The conflict becomes a refusal (exit 1) with an exit-code assertion in the same follow-up commit.

### F-03 — answered

2026-09-13: Corrected in the same follow-up commit.
