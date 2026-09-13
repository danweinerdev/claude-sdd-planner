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
verdict: Amend
reviewed_planning_revision: "a917c60c4a126fec7b0e952435980137701ed023"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "drift-detector (agent, delta e12884d..a917c60, HEAD unmoved; go test on graph, rules and cmd/sdd all ok, targeted -v runs of every gate test pass): propagation-validator rev 5 — phasereview.go:412-418, :750-757, :1046-1052 check ErrOperational before SDD170/SDD173/SDD174 and evidence.go:758-846 verifyCommittedLifecycle takes planLookupFailed with contentComplete computed before the plan lookup, matching the revised contract; TestContentQueryFailuresAreOperational (10 subtests) passes. propagation-graph-callers rev 3 — transition.go:243-256 uses RunWithWaiversChecked and returns the error before any status write; TestTransitionGateOperationalSweepExits (operational_test.go:139-241) proves plan approve leaves the artifact unchanged with exit 2. Each amendment is one clean commit; recorded artifact digests match the tree. Minor note only: a4c0d4b/650750e belong to single-pass-evaluation, outside this gate's set. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "quality-scanner (agent, delta e12884d..a917c60 on evidence.go, phasereview.go, rules/operational_test.go, transition.go, cmd/sdd/operational_test.go; staticcheck, go vet, gofmt clean; go test -race -count=1 ok on rules 7.0s and cmd/sdd 2.8s incl. TestContentQueryFailuresAreOperational 10/10, TestTransitionGateOperationalSweepExits and TestExamplesBehaveAsDeclared): verifyCommittedLifecycle (evidence.go:769-857) captures contentComplete at :808 before the plan lookup and sets suppressForPlanLookup only when content was complete, single emit path; gitLifecycleNormalized's three callers (:413-424, :750-771, :1047-1057) check ErrOperational with negative controls; gateDiagnostics (transition.go:246-257) uses RunWithWaiversChecked on both sweeps, the error reaches exitCode (root.go:692-701) as 2, and the deferred WriteAtomic restore (:276) fires on the error path; the new test drives a real compiled sdd binary. No findings. VERDICT: Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "spec-compliance (agent, delta e12884d..a917c60 on internal/rules and cmd/sdd; go build, go test and go test -race pass for both packages): FR-16 lifecycle transitions — transition.go:243 RunWithWaiversChecked, evaluate.go:151, root.go:692-701 exit 2, TestTransitionGateOperationalSweepExits passes; DD-10 — phasereview.go:413-419, :750-773, :1046-1052 guarded and covered by TestContentQueryFailuresAreOperational (10 subtests); the RunChecked switch and the test-assertion removal are not weakenings. Major: evidence.go:808 sets contentComplete from lifecycleComplete without folding in haveBody (set at :800 from the evidence heading, plan-independent) while the emit gate at :839 is '!lifecycleComplete || !haveBody', so a phase with status and criteria complete but no evidence heading plus an operational plan lookup sets suppressForPlanLookup and swallows a genuine SDD072; contentBrokenPhase (operational_test.go:501-510) only exercises the unchecked-criteria path. VERDICT: Needs changes."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023"
    evidence: "blind-spot-finder (agent, diff only e12884d..a917c60; read candidateArtifactErrors, cmdReviewResolve and their callers in full; go build and go vet clean; grep of cmd/sdd for rules.Run finds exactly transition.go:257 (fixed) and :349 (not); grep of cmd/sdd tests for candidateArtifactErrors finds nothing): Critical — candidateArtifactErrors (transition.go:317-353, called by review resolve at review.go:715 for phase-gate reviews) still calls the unchecked RunWithWaivers, whose operational fallback is one SDD198 with Path '.', and the loop filter 'd.Severity.Invalidating() && d.Path == rel' drops it, so review resolve can freeze a phase-gate review that was never re-validated. Confirmed planName empty can never set suppressForPlanLookup; evidence add and graph sync run no rules sweep. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: critical
    title: "review resolve can freeze a phase-gate review on an unanswered sweep"
    status: open
    action: revise
    nodes: [propagation-graph-callers]
    revise:
      contract: "Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check; remap-revisions treats a RevisionExists error that is not ErrNotFound as an operational failure and never reports it as a revision that is not an available commit; every rules sweep run by a cmd/sdd verb — the lifecycle transition gate's before and after sweeps and the review-resolve freeze gate — goes through the checked evaluation entry points and exits 2 with the cause when the sweep is operational, so an operational diagnostic is never deduplicated or path-filtered into an empty result and no status or frozen flag is written."
      gate:
        type: tests
        tests:
          - {id: TestSyncCleanFailureIsOperational, file: internal/graph/sync/operational_test.go}
          - {id: TestProviderDetectionFailureIsOperational, file: internal/graph/provider/operational_test.go}
          - {id: TestLifecycleVerbsOperationalExit, file: cmd/sdd/operational_test.go}
          - {id: TestRemapRevisionsQueryFailureIsOperational, file: internal/graph/ops/operational_test.go}
          - {id: TestTransitionGateOperationalSweepExits, file: cmd/sdd/operational_test.go}
          - {id: TestReviewResolveOperationalSweepExits, file: cmd/sdd/operational_test.go}
  - id: F-02
    severity: major
    title: "The evidence-committed suppression ignores a missing evidence heading and the retirement rule folds an operational failure into a finding"
    status: open
    action: revise
    nodes: [propagation-validator]
    revise:
      contract: "Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection, VerifyRetirementSource wraps a failed post-detection RevisionExists so ErrOperational survives errors.Is, and the retirement rule records an operational VerifyRetirementSource failure on the Root and stays silent instead of emitting it as a retirement problem; every repository query made from a rule callback in internal/rules that can emit or suppress a diagnostic — RevisionExists, IsAncestor, Clean, Head, RevisionsAfter, ChangedPaths and FileAt, including the review-committed check and the lifecycle-normalized content loads behind the intent comparison and the planning-revision load — emits an absence or state diagnostic only when the query succeeded and answered negatively (or the revision syntax is unsupported) and stays silent on ErrOperational, which the collector already holds; an operational plan lookup inside the evidence-committed checks suppresses only the plan-state contribution, never a content diagnostic computed without the repository, where content completeness includes the presence of the evidence heading; and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run."
      gate:
        type: tests
        tests:
          - {id: TestIgnoredRepoErrorAbortsEvaluation, file: internal/rules/operational_test.go}
          - {id: TestRetirementDetectionFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestValidationOperationalExit, file: cmd/sdd/operational_test.go, satisfies: [user-entrypoint]}
          - {id: TestP4IdentityQueryFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestIdentityQueryFailuresAreOperational, file: internal/rules/operational_test.go}
          - {id: TestRetirementProbeFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestRepoQueryFailuresAreOperational, file: internal/rules/operational_test.go}
          - {id: TestContentQueryFailuresAreOperational, file: internal/rules/operational_test.go}
          - {id: TestMissingEvidenceHeadingSurvivesPlanLookupFailure, file: internal/rules/operational_test.go}
          - {id: TestRetirementRuleFailureIsOperational, file: internal/rules/operational_test.go}
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..a917c60c4a126fec7b0e952435980137701ed023`.

## Findings
### F-01 — review resolve can freeze a phase-gate review on an unanswered sweep

Four lanes converged: `cmd/sdd/transition.go:349` `candidateArtifactErrors` (sole caller `review.go:715`) runs the unchecked sweep, whose SDD198 carries `Path "."` and is dropped by the `d.Path == rel` filter at `:350`, so `review resolve` writes `resolved` and `frozen: true` on an unvalidated root — an immutable artifact. It is the only remaining unchecked sweep call in cmd/sdd, internal/rules and internal/graph, and no test covers it.

### F-02 — The evidence-committed suppression ignores a missing evidence heading and the retirement rule folds an operational failure into a finding

`internal/rules/evidence.go:808` snapshots `contentComplete` from `lifecycleComplete` without `haveBody` (set at `:800`) while the emit gate at `:839` includes it, so a phase whose only defect is a missing evidence heading loses its SDD072 under an operational plan lookup. `internal/rules/retirement.go:93-110` `RetirementProblems` folds `VerifyRetirementSource`'s `ErrOperational` into a problem string the SDD181 rule emits (`:151-165`); because `VerifyRetirementSource` detects with `vcs.DetectChecked` directly (`:29`), the collector never records it and the evaluator cannot abort.

## Resolution Log
