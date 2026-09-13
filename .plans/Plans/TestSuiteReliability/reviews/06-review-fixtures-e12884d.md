---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "e12884d971369a69a2366790380a166411d1cf99"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "drift-detector (agent, delta b4a8456..e12884d, six files across 4eaa3cf and e12884d): 4eaa3cf is confined to rules.go, rules_test.go and waivers.go — allRules() is unexported, used only by Run (rules.go:245), RunWithWaivers (:278), knownCode (waivers.go:152) and bareOnce (:248), sorts by Code exactly as All() does, and evaluate.go is untouched so single-pass-evaluation's one-sweep contract and strict/reporting semantics hold; TestStrictAndReportingSemanticsPreserved, TestOrdinaryEvaluationOnce and TestAllReturnsDistinctExampleMaps pass (the latter two under -race); go build and go vet clean. e12884d belongs to propagation-validator and stays off rules.go. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "quality-scanner (agent, delta b4a8456..e12884d on rules.go, rules_test.go, waivers.go; gofmt -l, go vet and staticcheck clean, go test -race -count=2 ./internal/rules ok in 13.0s): allRules() is unexported with exactly four callers (rules.go:245, :278, waivers.go:152, :248) feeding evaluate/runWith/runWithWaiversWith, which read only Check/CheckRoot/Code/Severity, so no registry pointer leaks past the package; Explain, All and Get still go through exportRule. Minor: rules_test.go:67 '&ex.Files == &other.Files' compares addresses of two local struct fields and can never be true (repro under TMPDIR printed false for an aliased map), so that assertion was dead; the mutate-then-observe check at :70-76 is the real guard and would catch swapping All() for allRules(). VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "spec-compliance (agent, delta b4a8456..e12884d; go build, go vet, go test -race on internal/rules, the design's -p=2 -parallel=4 command over procexec, rules, vcs and tools/regression, TestRunIsDeterministic (147 subtests), TestOrdinaryEvaluationOnce, TestAppendOnlyScanOnce, TestIgnoredRepoErrorAbortsEvaluation, TestRepoQueryFailuresAreOperational and TestAllReturnsDistinctExampleMaps all pass): FR-07 single sweep and FR-04/FR-05 repeatability unchanged (evaluate.go:51-76 untouched); DD-10 guards in evidence.go and phasereview.go consume the recordingRepo's classification; no weakened assertions. Minor: evaluate.go:137,140 RunChecked/RunWithWaiversChecked — the functions cmd/sdd/validate.go:103-105 calls — still used All(), so the copy elimination missed the CLI path. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "blind-spot-finder (agent, diff only b4a8456..e12884d on rules.go, rules_test.go, waivers.go; read rules.go, waivers.go and evaluate.go in full, grepped every All()/allRules() caller in internal/ and cmd/, go build and go test -race clean, standalone repro under TMPDIR): no caller stores or mutates a *Rule from allRules(); the slice is fresh per call and concurrent Run calls share only read-only *Rule values; bareDiagnostics is per-Root and mutex-guarded. Minor: rules_test.go:67-69 pointer-equality assertion is dead (always false even for an aliased map) while the mutate-then-reread block at :70-76 is the real guard. Minor: evaluate.go:137 and :140 — RunChecked and RunWithWaiversChecked, the functions cmd/sdd/validate.go:103-105 calls for the real sdd validate path, were left on All(), so the stated hot-path saving did not reach the CLI's own validate. VERDICT: Aligned."
findings:
  - id: F-01
    severity: minor
    title: "TestAllReturnsDistinctExampleMaps carried a pointer assertion that can never fire"
    status: answered
  - id: F-02
    severity: minor
    title: "RunChecked and RunWithWaiversChecked were left on All()"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99`.

## Findings
### F-01 — TestAllReturnsDistinctExampleMaps carried a pointer assertion that can never fire

`internal/rules/rules_test.go:67-69` compared the addresses of two local struct fields; the mutate-then-reread block was the real guard.

### F-02 — RunChecked and RunWithWaiversChecked were left on All()

`internal/rules/evaluate.go:137,140`, the functions `cmd/sdd/validate.go:103-105` calls, still cloned every example per run.

## Resolution Log
### F-01 — answered

2026-09-13: Deleted inline as a4c0d4b (test-only).

### F-02 — answered

2026-09-13: Switched inline as 650750e. Both inline commits landed while this round was open, which moved HEAD past this artifact's identity; the following round reviews them at the true head (SDD-DOGFOOD-NOTES.md P-19).
