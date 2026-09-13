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
verdict: Amend
reviewed_planning_revision: "e12884d971369a69a2366790380a166411d1cf99"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "drift-detector (agent, delta b4a8456..e12884d, six files; git show --stat e12884d touches only evidence.go +37/-2, phasereview.go +35/-3, operational_test.go +936): propagation-validator rev 4 lands as contracted — Clean, Head, IsAncestor, RevisionsAfter and ChangedPaths in verifyGitPhasePostReviewState, the SDD173 gate loop (now documented), all three IsAncestor sites in verifyPhaseReviewIdentity (:931, :949, :956), the checkpoint IsAncestor (:1189) and both FileAt sites in each evidence-committed check (with the planCommitted closure's operational flag consumed by the emit wrapper) check ErrOperational before any fail or emit; TestRepoQueryFailuresAreOperational at operational_test.go:543 runs 28 subtests, all pass with the five prior gate tests; go build, go vet and go test -race on rules and graph/ops clean; graph carries contract_rev 4 with matching gate list. 4eaa3cf is the disclosed inline fixtures answer. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "quality-scanner (agent, delta b4a8456..e12884d on evidence.go, phasereview.go, operational_test.go; gofmt -l, go vet, staticcheck clean on rules and cmd/sdd; go test -race -count=1 ok on both; TestExamplesBehaveAsDeclared all pass with no verdict flips; TestRepoQueryFailuresAreOperational 28/28): every new ErrOperational branch at evidence.go:655-663, :686-689, :727-731 and phasereview.go:661-664, :672-673, :693-695, :702-705, :713-716, :834-839, :933-936, :952-955, :961-963, :1191-1194 is additive before the existing fail path and never widens; the scripted fakes are keyed by revision id or rev+path, not call order, and the gate-loop failAfter:2 lands on the gate's own call because task 1.1 contributes no RevisionExists; the operational return-instead-of-continue in the phase and task loops mirrors the pre-existing exists() closure. No findings. VERDICT: Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "spec-compliance (agent, delta b4a8456..e12884d on internal/rules; full DD-10 audit of every repository method call in evidence.go and phasereview.go; targeted tests pass): the five callers touched by e12884d (verifyGitEvidenceCommitted, verifyP4EvidenceCommitted, verifyGitPhasePostReviewState, verifyPhaseReviewIdentity, verifyGitPlanPhaseCheckpoints) are guarded with operational and negative-control subtest pairs in TestRepoQueryFailuresAreOperational. Two unguarded FileAt callers remain, pre-existing and untested: phasereview.go:413 verifyGitPhaseReviewCommitted emits SDD170 'not committed at HEAD' on any FileAt error, and gitLifecycleNormalized at :791 returns the raw error to :745-749 (SDD170 'cannot compare canonical intent') and :1034-1037 (SDD173 'cannot load at planning revision'); a grep of the package's test files for SDD170 and gitLifecycleNormalized finds no test. Rated Critical by the lane; collector-masked on the sweep path. VERDICT: Needs changes."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99"
    evidence: "blind-spot-finder (agent, diff only b4a8456..e12884d on evidence.go, phasereview.go, operational_test.go; read evaluate.go in full; mutation-tested by removing one Clean() guard, observing the matching subtest fail with an attributable message, then restoring — git status --porcelain clean): the early-return-in-loop hypothesis (an operational answer on one task or phase skipping a later unrelated diagnostic) is not observable because evaluate() checks Root.OperationalFailure after every callback and discards the whole sweep for SDD198; the operational flag default and the else-if binding on the task-vs-base IsAncestor check match the pre-diff determinate-error behaviour when diffed against b4a8456. No findings; optional dedupe of the repeated guard into the exists()-style helper. VERDICT: Aligned."
findings:
  - id: F-01
    severity: major
    title: "Four FileAt sites feeding SDD170/SDD173/SDD174 and the evidence-committed suppression scope remain"
    status: open
    action: revise
    nodes: [propagation-validator]
    revise:
      contract: "Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection and VerifyRetirementSource wraps a failed post-detection RevisionExists so ErrOperational survives errors.Is, every repository query made from a rule callback in internal/rules that can emit or suppress a diagnostic — RevisionExists, IsAncestor, Clean, Head, RevisionsAfter, ChangedPaths and FileAt, including the review-committed check and the lifecycle-normalized content loads behind the intent comparison and the planning-revision load — emits an absence or state diagnostic only when the query succeeded and answered negatively (or the revision syntax is unsupported) and stays silent on ErrOperational, which the collector already holds; an operational plan lookup inside the evidence-committed checks suppresses only the plan-state contribution, never a content diagnostic computed without the repository; and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run."
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
  - id: F-02
    severity: major
    title: "The transition gate can deduplicate an operational sweep away and let a transition proceed"
    status: open
    action: revise
    nodes: [propagation-graph-callers]
    revise:
      contract: "Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check; remap-revisions treats a RevisionExists error that is not ErrNotFound as an operational failure and never reports it as a revision that is not an available commit; the lifecycle transition gate runs its before and after sweeps through the checked evaluation entry points and exits 2 with the cause when either sweep is operational, so an operational diagnostic present in both sweeps is never deduplicated into an empty introduced set."
      gate:
        type: tests
        tests:
          - {id: TestSyncCleanFailureIsOperational, file: internal/graph/sync/operational_test.go}
          - {id: TestProviderDetectionFailureIsOperational, file: internal/graph/provider/operational_test.go}
          - {id: TestLifecycleVerbsOperationalExit, file: cmd/sdd/operational_test.go}
          - {id: TestRemapRevisionsQueryFailureIsOperational, file: internal/graph/ops/operational_test.go}
          - {id: TestTransitionGateOperationalSweepExits, file: cmd/sdd/operational_test.go}
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..e12884d971369a69a2366790380a166411d1cf99`.

## Findings
### F-01 — Four FileAt sites feeding SDD170/SDD173/SDD174 and the evidence-committed suppression scope remain

`internal/rules/phasereview.go:413` (review-committed check), `:745-746` and `:1034` through `gitLifecycleNormalized` (`:791`) still turn any `FileAt` error into a diagnostic; no subtest reaches them because an earlier query fails first. In `evidence.go:653-753` the `operational` flag from the plan lookup suppresses even a content-only diagnostic (a scratch reproduction produced zero diagnostics for a phase with unchecked acceptance criteria). All masked on the sweep path by the evaluator's discard.

### F-02 — The transition gate can deduplicate an operational sweep away and let a transition proceed

`cmd/sdd/transition.go:234-251` runs `rules.RunWithWaivers` for both sweeps and never inspects the operational code; the same SDD198 in both sweeps makes the introduced set empty. `TestLifecycleVerbsOperationalExit` covers only a missing git binary, where detection fails before the gate.

## Resolution Log
