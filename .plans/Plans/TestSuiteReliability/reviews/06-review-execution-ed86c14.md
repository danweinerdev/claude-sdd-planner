---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ed86c14e9e6991c2a5616ec9e5d0674e3f9657c9"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "ed86c14e9e6991c2a5616ec9e5d0674e3f9657c9"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ed86c14e9e6991c2a5616ec9e5d0674e3f9657c9"
    evidence: "drift-detector (agent, delta a917c60..ed86c14, HEAD confirmed unmoved, two commits 85f038e and ed86c14 each single-concern with F-02/F-01 citations): candidateArtifactErrors at transition.go:349-357 uses RunWithWaiversChecked and returns before the path filter, TestReviewResolveOperationalSweepExits drives the compiled binary and proves the artifact unchanged; evidence.go:808 contentComplete includes haveBody; retirement.go:94-166 splits problems from operational and the SDD181 init routes operational to r.recordFailure; every artifact digest on propagation-graph-callers (11/11, seq 373) and propagation-validator (8/8, seq 372) matches sha256sum at HEAD; amendment seq 361's report digest matches the frozen a917c60 review; all cited gate tests pass live. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ed86c14e9e6991c2a5616ec9e5d0674e3f9657c9"
    evidence: "quality-scanner (agent, delta a917c60..ed86c14 on evidence.go, retirement.go, rules/operational_test.go, transition.go, cmd/sdd/operational_test.go; go vet, gofmt -l and go test -race -count=1 on rules, graph/..., cmd/sdd incl. TestExamplesBehaveAsDeclared all pass; staticcheck clean except two SA4006 dead stores at operational_test.go:1943/:1990): the review-resolve and haveBody fixes are correct with control cases. Major regression: retirement.go:93-96's public RetirementProblems does 'problems, _ := retirementProblems(...)' so graph/ops/retirement.go:53 (RetireWithSource behind sdd graph retire, which only refuses on len(problems)) and compile.go:452 (semanticFindings) now proceed on an operational VerifyRetirementSource failure that git show a917c60:internal/rules/retirement.go shows was previously appended to problems and refused; neither caller can reach recordFailure. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ed86c14e9e6991c2a5616ec9e5d0674e3f9657c9"
    evidence: "spec-compliance (agent, delta a917c60..ed86c14 on internal/rules and cmd/sdd; go test and the design's -race -count=1 -p=2 -parallel=4 command over procexec, rules, vcs, tools/regression and cmd/sdd all green): the three open items are closed — transition.go:317-360 candidateArtifactErrors on RunWithWaiversChecked with TestReviewResolveOperationalSweepExits; evidence.go:812 contentComplete includes haveBody with TestMissingEvidenceHeadingSurvivesPlanLookupFailure; retirement.go:93-186 separates operational results and the SDD181 init records them via r.recordFailure with TestRetirementRuleFailureIsOperational; no production caller of the unchecked Run/RunWithWaivers remains (validate.go:103-105, transition.go:257,355 checked); every rule-callback repo query traced to r.Repo's recording decorator (root.go:173-260); gitCapable sites at evidence.go:565,641, appendonly.go:162, phasereview.go:399,657,1010 record at detection. Not examined: the public RetirementProblems wrapper's non-Root callers. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..ed86c14e9e6991c2a5616ec9e5d0674e3f9657c9"
    evidence: "blind-spot-finder (agent, diff only a917c60..ed86c14; read candidateArtifactErrors with its defer/restore ordering, cmdReviewResolve 585-759, retirement.go and evaluate.go in full, root.go:95-134, evidence.go:769-859; go build and the three new tests pass): the deferred WriteAtomic restore registered before the checked call still fires on the error return; non-phase-gate review resolve runs no sweep by documented design (review.go:706-712); no unchecked rules.Run/RunWithWaivers call remains in cmd/sdd (validate.go:103/105, transition.go:257/355 all checked); no t.Parallel in internal/rules tests so the shim's t.Setenv cannot leak. Question only: a genuine SDD181 finding computed in the same sweep as an operational one is discarded by evaluate()'s documented whole-sweep abort. VERDICT: Aligned."
findings:
  - id: F-01
    severity: critical
    title: "RetirementProblems' public form discards operational failures"
    status: open
    action: revise
    nodes: [propagation-validator]
    revise:
      contract: "Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection, VerifyRetirementSource wraps a failed post-detection RevisionExists so ErrOperational survives errors.Is, the retirement rule records an operational VerifyRetirementSource failure on the Root and stays silent instead of emitting it as a retirement problem, and the exported retirement-problems entry point never discards an operational failure — it returns it as an error alongside the substantive problems so no caller can read an unanswered verification as a clean result; every repository query made from a rule callback in internal/rules that can emit or suppress a diagnostic — RevisionExists, IsAncestor, Clean, Head, RevisionsAfter, ChangedPaths and FileAt, including the review-committed check and the lifecycle-normalized content loads behind the intent comparison and the planning-revision load — emits an absence or state diagnostic only when the query succeeded and answered negatively (or the revision syntax is unsupported) and stays silent on ErrOperational, which the collector already holds; an operational plan lookup inside the evidence-committed checks suppresses only the plan-state contribution, never a content diagnostic computed without the repository, where content completeness includes the presence of the evidence heading; and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run."
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
          - {id: TestRetirementProblemsReturnsOperationalError, file: internal/rules/operational_test.go}
  - id: F-02
    severity: critical
    title: "graph compile and graph retire proceed on an unverified retirement source"
    status: open
    action: revise
    nodes: [propagation-graph-callers]
    revise:
      contract: "Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check; remap-revisions treats a RevisionExists error that is not ErrNotFound as an operational failure and never reports it as a revision that is not an available commit; every rules sweep run by a cmd/sdd verb — the lifecycle transition gate's before and after sweeps and the review-resolve freeze gate — goes through the checked evaluation entry points and exits 2 with the cause when the sweep is operational, so an operational diagnostic is never deduplicated or path-filtered into an empty result and no status or frozen flag is written; and graph compile and graph retire consume the retirement-problems entry point's operational error, so an unanswered retirement-source verification refuses the compile or the retire with exit 2 and writes nothing."
      gate:
        type: tests
        tests:
          - {id: TestSyncCleanFailureIsOperational, file: internal/graph/sync/operational_test.go}
          - {id: TestProviderDetectionFailureIsOperational, file: internal/graph/provider/operational_test.go}
          - {id: TestLifecycleVerbsOperationalExit, file: cmd/sdd/operational_test.go}
          - {id: TestRemapRevisionsQueryFailureIsOperational, file: internal/graph/ops/operational_test.go}
          - {id: TestTransitionGateOperationalSweepExits, file: cmd/sdd/operational_test.go}
          - {id: TestReviewResolveOperationalSweepExits, file: cmd/sdd/operational_test.go}
          - {id: TestRetireAndCompileOperationalSourceExit, file: cmd/sdd/operational_test.go}
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..ed86c14e9e6991c2a5616ec9e5d0674e3f9657c9`.

## Findings
### F-01 — RetirementProblems' public form discards operational failures

Five lanes converged: `internal/rules/retirement.go:93-96` does `problems, _ := retirementProblems(...)`, so the operational slice the rev 6 split introduced never reaches the two non-Root callers, which before ed86c14 (`git show a917c60:internal/rules/retirement.go:119-121`) received the folded error and refused. Two dead stores in the new test (`operational_test.go:1944`, `:1991`) come along.

### F-02 — graph compile and graph retire proceed on an unverified retirement source

`internal/graph/compile/compile.go:452` and `internal/graph/ops/retirement.go:53` gate only on `len(problems)`; with F-01 they embed and write the graph, or commit a new retirement, with exit 0 when an existing retirement's provenance could not be verified. No test in either package exercises an operational `VerifyRetirementSource` failure.

## Resolution Log
