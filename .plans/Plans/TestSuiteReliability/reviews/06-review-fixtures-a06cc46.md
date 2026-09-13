---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a06cc46da1011202ee98443957cb52191686bf3a"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "a06cc46da1011202ee98443957cb52191686bf3a"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a06cc46da1011202ee98443957cb52191686bf3a"
    evidence: "drift-detector (agent, delta dcbe2cd..a06cc46, HEAD confirmed unmoved; six files under internal/graph/{compile,ops} and cmd/sdd/operational_test.go): none of them appears in the artifacts of this gate's seven nodes or in phase docs 01-03; all 39 recorded artifact digests for those nodes (test-git-policy 2, child-validation-hermetic 22, prepare-once-determinism 2, fixture-reproducibility 1, pure-selection 4, single-pass-evaluation 5, append-only-scan-once 3) recomputed with sha256sum and match byte-for-byte. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a06cc46da1011202ee98443957cb52191686bf3a"
    evidence: "quality-scanner (agent, intent-blind, delta dcbe2cd..a06cc46 on internal/graph/compile, internal/graph/ops and cmd/sdd/operational_test.go; git diff --stat confirms internal/rules untouched in this delta; grepped all six non-test call sites of Sources.Validate at audit.go:124, compile.go:180, amend.go:101 and 109, ops.go:93 and 101 and confirmed each checks the returned error before trusting the finding list; read semanticFindings at compile.go:454 and store.Update at store.go:104-120 confirming a callback error aborts before WriteAtomicExpecting so no partial write occurs; traced graph_audit.go through root.go exitCode confirming a non-refusedError maps to exit 2; ran go test ./internal/rules/... for the determinism, single-pass, examples-behave-as-declared and fresh-roots cases under -race -count=2, all pass; staticcheck, vet and gofmt clean for the fixtures scope). No findings. Verdict Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a06cc46da1011202ee98443957cb52191686bf3a"
    evidence: "spec-compliance (agent, delta dcbe2cd..a06cc46; go build and go vet clean; TestSplitAmendAuditOperationalSourceExit and TestRetireAndCompileOperationalSourceExit pass; FR-04/FR-05 TestRunIsDeterministic and TestDeterminismPreparationCount, FR-07 TestOrdinaryEvaluationOnce, TestStrictAndReportingSemanticsPreserved and the waiver family pass; all 18 packages under rules, graph and cmd/sdd ok): FR-16/AC-08 — Sources.Validate now returns ([]Finding, error) at anchor.go:88-97 and every caller checks it (audit.go:121-127 returns before OK is set, compile.go:175-180, amend.go:98-113 and ops.go:90-104 return before applyAmendments/applySplit and before the store commits); no blank-identifier discard remains at any of the five sites. No gaps, no violations. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a06cc46da1011202ee98443957cb52191686bf3a"
    evidence: "blind-spot-finder (agent, diff only dcbe2cd..a06cc46 on internal/graph/compile and internal/graph/ops; walked all six Validate call sites (grep-confirmed), store.go's Update CAS, semanticFindings/RetirementProblemsChecked semantics and the black-box test at cmd/sdd/operational_test.go:769; go build and go vet clean): semanticFindings returns (nil, err) atomically, both before and after calls in amend.go and ops.go are checked before any Update/WriteAtomicExpecting, Update never writes when its closure errors, and AmendFromReview's DryRun check (amend.go:121) follows both validations. Minor test-precision note: installNShotGitShim(t, 3) at :830 and :885 is a call counter, so the test cannot tell whether the before or the after validation failed. VERDICT: Aligned."
findings:
  - id: F-01
    severity: minor
    title: "The n-shot git shim does not pin which of the before or after validations tripped"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..a06cc46da1011202ee98443957cb52191686bf3a`.

## Findings
### F-01 — The n-shot git shim does not pin which of the before or after validations tripped

`cmd/sdd/operational_test.go:830,885` call `installNShotGitShim(t, 3)`; the count of git invocations before the second `Sources.Validate` call is not asserted, so a regression that re-discards the error only on the after path could hide behind a before-path failure.

## Resolution Log
### F-01 — answered

2026-09-13: Test-precision observation on a passing gate test; both call sites are error-checked today and the finding names no defect. Tracked as a follow-up test hardening item outside the graph, to land after this round closes.
