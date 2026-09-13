---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "drift-detector (agent, delta 4d65872..b4a8456: c976667, 7d8f13d, 377372e, b4a8456): propagation-validator rev 3 lands as specified — verifyCleanGitIdentity incl. ancestry, verifyCleanP4Identity, validGitTaskReviewIdentity, verifyPhaseReviewIdentity, verifyPhaseReviewPlanningRevision and verifyGitPlanPhaseCheckpoints stay silent on ErrOperational and emit absence only after a successful no or unsupported syntax; VerifyRetirementSource wraps the post-detection probe with %w; both new gate tests and the three prior ones pass; go build clean; the artifacts array now lists evidence.go and phasereview.go; 7d8f13d answers F-02. The p4 guard's change to errors.Is(err, ErrOperational) is the review's own requested correction and equivalent on the adapter's actual error surface (vcs.go, git.go, p4.go). Each amendment is one clean commit. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "quality-scanner (agent, delta 4d65872..b4a8456 on evidence.go, phasereview.go, retirement.go, rules/operational_test.go, ops/operational_test.go; staticcheck, go vet, gofmt and go test -race clean on internal/rules and cmd/sdd; fake repo and repoCache seams validated; retirement shim documented like the remap one): Major — internal/rules/phasereview.go:684, :911, :926, :931, :1159 still use 'if ok, err := repo.IsAncestor(...); err != nil || !ok' and emit SDD167/SDD173/SDD175 on an operational failure, while git.go:134-147 proves IsAncestor returns ErrOperational; :911/:926 sit in verifyPhaseReviewIdentity whose RevisionExists calls were fixed three lines above, and no TestIdentityQueryFailuresAreOperational subtest covers IsAncestor outside verifyCleanGitIdentity. Question: phasereview.go:816's gate silently sets allExist=false on an operational error. Minor: installOneShotGitShim duplicated between rules and ops test files. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "spec-compliance (agent, delta 4d65872..b4a8456 on internal/rules and internal/graph/ops; go build, go vet, go test -race -count=1 on rules, graph/ops and vcs, and staticcheck with cache under TMPDIR all clean): DD-10/FR-16 identity sites at evidence.go:567-576, :584-589, :959-968 and phasereview.go:862-871, :894-901, :914-920, :979-991, :1092-1106, :1150-1156 now check ErrOperational first; retirement.go:53-56 splits the probe error with %w; the p4 guard's ErrOperational-only condition is a precision improvement with the ErrUnsupported subtest proving SDD072 still fires; DD-7 — rules.go:87-121 deep copies with TestFreshRootsAreIndependentInProcess; the evaluate.go:44-76 collector abort re-verified. Noted for completeness: phasereview.go:684 IsAncestor and :698 ChangedPaths still fold operational and negative into one fail(), safe only through the sweep-level discard. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "blind-spot-finder (agent, diff only 4d65872..b4a8456 on evidence.go, phasereview.go, retirement.go and the two operational test files; read root.go's recordingRepo/OperationalFailure and evaluate.go's sweep in full, walked every caller of the changed functions, ran TestIdentityQueryFailuresAreOperational -v): the silent-return path is doubly defended because Root.Repo always returns a recordingRepo and evaluate() checks OperationalFailure after every callback and discards output, so no sweep entry point can pass silently. Major maintenance trap: phasereview.go:684, :911, :926, :931, :1159 keep 'err != nil || !ok' after IsAncestor and would emit SDD173/SDD175 on ErrOperational if called directly (the pattern the new tests themselves use); the fake repos set only existsErr for those functions, ancestrErr only for verifyCleanGitIdentity, so the suite's coverage claim overstates. validGitTaskReviewIdentity's bool is discarded by its caller (evidence.go:907), inert. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: major
    title: "Ancestry, cleanliness and content queries in phase-review and evidence rules still fold an operational failure into a diagnostic"
    status: open
    action: revise
    nodes: [propagation-validator]
    revise:
      contract: "Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection and VerifyRetirementSource wraps a failed post-detection RevisionExists so ErrOperational survives errors.Is, every repository query made from a rule callback in internal/rules that can emit or suppress a diagnostic — RevisionExists, IsAncestor, Clean, Head, RevisionsAfter, ChangedPaths and FileAt, across the identity, phase-review post-state, planning-revision, checkpoint and evidence-committed checks — emits an absence or state diagnostic only when the query succeeded and answered negatively (or the revision syntax is unsupported) and stays silent on ErrOperational, which the collector already holds, and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run."
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
  - id: F-02
    severity: minor
    title: "installOneShotGitShim is duplicated between two test packages"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3`.

## Findings
### F-01 — Ancestry, cleanliness and content queries in phase-review and evidence rules still fold an operational failure into a diagnostic

Four lanes converged on the same sites: `internal/rules/phasereview.go:684`, `:911`, `:926`, `:931`, `:1159` (`IsAncestor`), `:658` (`Clean`), `:663` (`Head`), `:690`/`:698` (`RevisionsAfter`/`ChangedPaths`), `:816` (the SDD173 gate), and `internal/rules/evidence.go:653`, `:676`, `:706`, `:724` (`FileAt`). `git.go:134-147` proves `IsAncestor` returns `ErrOperational`. They are safe today only because `evaluate.go:51-76` discards the sweep after the recording repo notes the failure; a direct caller (the pattern the new tests use) sees a false SDD167/SDD173/SDD175/SDD072, and the rev 3 commit's own claim was broader than what it delivered.

### F-02 — installOneShotGitShim is duplicated between two test packages

`internal/rules/operational_test.go:432-448` and `internal/graph/ops/operational_test.go:26-42` are identical.

## Resolution Log
### F-02 — rejected

2026-09-13: Two copies with a provenance comment are acceptable; extract a shared test helper if a third appears. Recorded in SDD-DOGFOOD-NOTES.md follow-ups.
