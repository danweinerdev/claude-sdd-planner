---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "a917c60c4a126fec7b0e952435980137701ed023"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "drift-detector (agent, final round 6, HEAD confirmed a917c60 with no commits during the window, delta e12884d..a917c60 of seven files across four single-concern commits): every artifact and dependency digest recorded on single-pass-evaluation (rev 2), propagation-validator (rev 5) and propagation-graph-callers (rev 3) re-hashes to the working tree; all seven touched files map to those nodes' artifacts; go build clean; the assertion removal and the allRules switch are consistent with rules.go:170-215's documented copy contract. Major: propagation-graph-callers' contract names review evidence, yet candidateArtifactErrors (transition.go:317-350, sole caller review.go:715 for phase-gate review resolve) still calls unchecked RunWithWaivers at :349 — the only remaining unchecked sweep call in cmd/sdd, internal/rules and internal/graph — with no test. VERDICT: Needs changes."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "quality-scanner (agent, final round 6, HEAD unmoved; delta read in full; gofmt -l only the untouched algorithms_test.go, go vet ./..., staticcheck ./..., GOOS=windows and GOOS=darwin go build ./..., make test, and go test -race -count=2 -shuffle=on on rules and cmd/sdd all clean): gateDiagnostics and verifyCommittedLifecycle fixes are correct with strong red-then-green tests. Two Major sites the pattern did not reach: internal/rules/retirement.go:93-110 RetirementProblems folds VerifyRetirementSource's ErrOperational into a plain problem string the SDD181 CheckRoot (:151-165) emits as a content finding, and because VerifyRetirementSource calls vcs.DetectChecked directly (:29) rather than r.Repo, the collector never records it and evaluate() cannot abort; cmd/sdd/transition.go:349 candidateArtifactErrors still uses unchecked RunWithWaivers so review resolve refuses with exit 1 and a misleading message (or, per the path filter, proceeds) instead of exit 2. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "spec-compliance (agent, final round 6, HEAD unmoved at a917c60; exhaustive grep of every rules sweep call and diagnostic filter in cmd/sdd and internal/graph): exactly two sweep call sites exist outside internal/rules — transition.go:257 gateDiagnostics (RunWithWaiversChecked, covered by TestTransitionGateOperationalSweepExits) and transition.go:349 candidateArtifactErrors (unchecked RunWithWaivers whose SDD198 with Path '.' is dropped by the 'd.Path == rel' filter, so review resolve can freeze a phase-gate review — immutable per review.go:701-708 — on an unvalidated root; no test exercises it); validate.go:104-112 checked; internal/graph consumes no sweep and its DD-10 seams provider.go:89-98 and sync.go:295-313 are unchanged; the phasereview/evidence content sites are guarded with TestContentQueryFailuresAreOperational; tools/parity/frozen-expectations.json byte-identical to 04c1e61. Question on the removed pointer assertion answered by the sibling lanes (it compared local addresses; the mutation check remains). Critical: the review-resolve freeze gate. VERDICT: Needs changes."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "blind-spot-finder (agent, final round 6, diff only e12884d..a917c60; read candidateArtifactErrors and review.go:590-750 in full, rules.go:277-283, ran a scratch reproduction of the path filter under TMPDIR and the rules and cmd/sdd suites): Critical — review resolve on a phase-gate review runs frontmatter-only checks (review.go:631-674) and then candidateArtifactErrors (transition.go:317-355, its only VCS-touching step), which calls unchecked RunWithWaivers and drops the synthesized SDD198 (Path '.') through 'd.Path == rel' at :350, returning (nil, nil), so review.go:720 proceeds to write status resolved and frozen: true — a permanent freeze of an unvalidated review; no test covers review resolve with git unavailable. Question — tools/regression/corpus.go:419 uses plain rules.Run, plausibly intentional for corpus fixtures. gateDiagnostics, the evidence/phasereview guards and the allRules switch checked and sound. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: critical
    title: "review resolve can freeze a phase-gate review on an unanswered sweep"
    status: answered
  - id: F-02
    severity: major
    title: "Retirement rule folds an operational failure into a finding; evidence suppression misses the heading case"
    status: answered
  - id: F-03
    severity: minor
    title: "tools/regression/corpus.go runs the plain sweep"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023`.

## Findings
### F-01 — review resolve can freeze a phase-gate review on an unanswered sweep

`cmd/sdd/transition.go:349-350`.

### F-02 — Retirement rule folds an operational failure into a finding; evidence suppression misses the heading case

`internal/rules/retirement.go:93-110`; `internal/rules/evidence.go:808`.

### F-03 — tools/regression/corpus.go runs the plain sweep

`tools/regression/corpus.go:419` uses `rules.Run`, which records an SDD198 fixture diagnostic rather than aborting the corpus build.

## Resolution Log
### F-01 — answered

2026-09-13: Revised through the sibling execution-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-execution-a917c60.md`, F-01), contract rev 4 of `propagation-graph-callers`.

### F-02 — answered

2026-09-13: Revised through the sibling execution-gate review at this identity (F-02), contract rev 6 of `propagation-validator`.

### F-03 — rejected

2026-09-13: The corpus deliberately records the unexcused state of each fixture, and a fixture root without git is itself a corpus defect the recorded SDD198 exposes; the lane raised it as a question, not a defect. Recorded in SDD-DOGFOOD-NOTES.md follow-ups.
