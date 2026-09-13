---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "drift-detector (agent, delta faaf81a..7ee8ef6, 18 files, commits 7ee8ef6, db2e653, 4d1f94a, 46b8a56 plus inline 008c9c1): checked-detection rev 3 — absentP4 at internal/vcs/p4.go:119-124 with call sites :137, :173, :182, :205 and TestP4OperationalErrorsAreNotAbsence (checked_test.go:97-118) passing; bounded-runner rev 2 — procexec.go:162-164 and TestMutationNotRetried (procexec_test.go:158-179, helper_test.go:74-96) passing; race-gate rev 2 — Makefile:152-160 and TestMakeTestRunsVet passing; child-validation-hermetic rev 4 — adoption_test.go plus five main_env_test.go siblings passing. cache-error-policy invariant holds: cache.go:145-152 cacheable() is unchanged and now sees the unwrapped ErrOperational from p4, with TestOperationalFailureNotCached and TestDeterminateAbsenceCached passing. TestPlanAmendmentsScopeVsReach covers 008c9c1. Graph contract_rev and provenance point at 7ee8ef6. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "quality-scanner (agent, delta faaf81a..7ee8ef6 on vcs, procexec, graph, cmd/sdd, all touched files read in full): absentP4 (p4.go:119-124) applied at RevisionExists :137, FileAt :173/:182, ChangedPaths :205 mirroring git.go:106-111 absent(); TestP4OperationalErrorsAreNotAbsence's t.Setenv PATH is safe because no test in internal/vcs calls t.Parallel (grep: zero hits); TestMutationNotRetried runs a real mutate-then-fail subprocess and proves cmd.Start/Wait happen once per Run; Reach (amend.go:157-165) uses algorithms.DependencyClosure which excludes the start node (algorithms.go:210-225), TestPlanAmendmentsScopeVsReach passes, the scope parameter now only feeds plan.Scope so review.go:305-318 and ops/amend.go:78-85 increment semantics are unchanged, and review/command targets are refused (amend.go:223-225); the three main_env_test.go files match the existing convention. go build, go vet, gofmt -l, staticcheck and go test -race pass on internal/vcs, internal/procexec, internal/graph/review. No findings; Reach single-call-site noted only. VERDICT: Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "spec-compliance (agent, delta faaf81a..7ee8ef6 on vcs, procexec, graph, cmd/sdd, Makefile, testgate): FR-16/DD-10 — absentP4 at internal/vcs/p4.go:119-124 applied at :137, :173, :182, :205 keeps runP4's CauseExit-vs-operational split instead of collapsing both into ErrNotFound, TestP4OperationalErrorsAreNotAbsence (checked_test.go:94-118) passes; DD-7 — cache.go:140-152 cacheable() now sees the operational error on the p4 side; FR-13 and the fault-injection acceptance no-retry clause — procexec.go:160-168 has a single cmd.Wait with no loop and TestMutationNotRetried (procexec_test.go:158-181) passes; NFR-05 — Makefile:146-158 'test: vet', make -n test shows go vet ./... before the race and full suite, TestMakeTestRunsVet passes, go vet ./... clean. FR-14 POSIX, FR-15, AC-06, AC-07, DD-2/4/9 untouched by the delta. No gaps, no violations. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "blind-spot-finder (agent, diff only faaf81a..7ee8ef6 on vcs, procexec, graph, cmd/sdd; read p4.go, procexec.go, amend.go, states.go reviewScope/Closed/ReviewStale, ops/amend.go applyAmendments and cache.go in full; grepped every RevisionExists/FileAt/ChangedPaths/procexec.Run caller): Reach admitting a node an inner GREEN review covers is handled by contract-rev staleness demoting the inner review, not a hole; no production caller retries Run; TestP4OperationalErrorsAreNotAbsence's file has no t.Parallel; go build ./... clean. Minor: internal/graph/ops/remap.go:177 still does 'if checkErr != nil || !exists' and reports a now-typed ErrOperational as 'not an available commit', unlike cmd/sdd/evidence.go:113-122 and cmd/sdd/review.go:100-112 which split ErrNotFound. Question: internal/rules/evidence.go:598 verifyCleanP4Identity discards RevisionExists's error ('ok, _ :=') so a p4 outage yields a false SDD072 — pre-existing, unchanged by the delta. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: minor
    title: "remap-revisions reports a failed RevisionExists query as a missing commit"
    status: open
    action: revise
    nodes: [propagation-graph-callers]
    revise:
      contract: "Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check; remap-revisions treats a RevisionExists error that is not ErrNotFound as an operational failure and never reports it as a revision that is not an available commit."
      gate:
        type: tests
        tests:
          - {id: TestSyncCleanFailureIsOperational, file: internal/graph/sync/operational_test.go}
          - {id: TestProviderDetectionFailureIsOperational, file: internal/graph/provider/operational_test.go}
          - {id: TestLifecycleVerbsOperationalExit, file: cmd/sdd/operational_test.go}
          - {id: TestRemapRevisionsQueryFailureIsOperational, file: internal/graph/ops/operational_test.go}
  - id: F-02
    severity: minor
    title: "The validator's Perforce identity check discards a failed RevisionExists query"
    status: open
    action: revise
    nodes: [propagation-validator]
    revise:
      contract: "Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection, the Perforce identity check records a RevisionExists error that is not ErrNotFound in the collector instead of emitting a not-a-submitted-changelist diagnostic, and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run."
      gate:
        type: tests
        tests:
          - {id: TestIgnoredRepoErrorAbortsEvaluation, file: internal/rules/operational_test.go}
          - {id: TestRetirementDetectionFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestValidationOperationalExit, file: cmd/sdd/operational_test.go, satisfies: [user-entrypoint]}
          - {id: TestP4IdentityQueryFailureIsOperational, file: internal/rules/operational_test.go}
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e`.

## Findings
### F-01 — remap-revisions reports a failed RevisionExists query as a missing commit

`internal/graph/ops/remap.go:177` does `if checkErr != nil || !exists` and prints "not an available commit", while `cmd/sdd/evidence.go:113-122` and `cmd/sdd/review.go:100-112` split `ErrNotFound` from other errors for exactly this reason. With the p4 adapter now returning `ErrOperational`, a transient outage during `sdd graph remap-revisions` sends the operator hunting a commit that exists.

### F-02 — The validator's Perforce identity check discards a failed RevisionExists query

`internal/rules/evidence.go:598` reads `ok, _ := repo.RevisionExists(revision)`, so a p4 outage during `sdd validate` yields a false not-a-submitted-changelist diagnostic instead of the collector's operational exit. Pre-existing, but the node's contract is propagation and the fix is the same split.

## Resolution Log
