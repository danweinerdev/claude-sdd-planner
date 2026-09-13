---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "4d6587238de8b72301d3bef22c242d9b56f0da84"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "drift-detector (agent, delta 7ee8ef6..4d65872: 4d65872, f9be2ec, ab07682, 82bc0f1; 11 files, every file traces to exactly one commit, each commit is a single clean commit per git show --stat): propagation-graph-callers and propagation-validator both at contract_rev 2 with pass at 4d65872; gate tests TestRemapRevisionsQueryFailureIsOperational (ops/operational_test.go:45) and TestP4IdentityQueryFailureIsOperational (rules/operational_test.go:152) exist at their declared paths and pass; go build ./... clean; f9be2ec's five extra TestMain files are required by child-validation-hermetic rev 5, not creep. Minor: propagation-validator's artifacts array omits internal/rules/evidence.go, where the F-02 fix landed (grep of the graph JSON finds no entry), so the file's digest is not tracked by that node. No missing work, no creep. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "quality-scanner (agent, delta 7ee8ef6..4d65872 on graph, rules, cmd/sdd): staticcheck, gofmt -l and go vet clean on ops, rules, review; the one-shot git shim answers exactly probeGit's rev-parse --show-toplevel and disables itself before RevisionExists's cat-file (memoEnabled is off under go test); fakeP4Repo mirrors Root.Repo's recordingRepo wrapping without leaking between subtests; TestPlanAmendmentsScopeVsReach additions sound. Major x2: internal/rules/evidence.go:570 and :578 (verifyCleanGitIdentity) and :952 (SDD169) still discard the RevisionExists/IsAncestor error and emit absence diagnostics for an operational failure — the same defect the delta fixed for Perforce. Question confirmed by the orchestrator: go test -count=2 ./internal/rules fails on TestExamplesBehaveAsDeclared/SDD161 and TestSDD161ConflationRefusedPerSpec at HEAD and at 7ee8ef6 (second in-process run from a fresh root drops the findings), a pre-existing cross-run leak. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "spec-compliance (agent, delta 7ee8ef6..4d65872 on graph, rules, cmd/sdd; read remap.go, ops/operational_test.go, evidence.go, rules/operational_test.go, root.go, evaluate.go, cmd/sdd/evidence.go, cmd/sdd/review.go): DD-10/FR-16 — remap.go:181-187 and evidence.go:599-604 now split ErrNotFound from other errors with TestRemapRevisionsQueryFailureIsOperational (ops/operational_test.go:33, control plus one-shot shim fault) and TestP4IdentityQueryFailureIsOperational (rules/operational_test.go:151) passing; cmd/sdd/evidence.go:118-125 and cmd/sdd/review.go:105-111 already used the pattern. Minor x2: internal/rules/evidence.go:570 (verifyCleanGitIdentity) and :952 still read 'ok, _ := repo.RevisionExists' — the recordingRepo (root.go:180-197) records the failure and the evaluator aborts (evaluate.go:52,60,69,101), so exit 2 holds end to end, but the local diagnostic text is wrong for an operational failure. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "blind-spot-finder (agent, diff only 7ee8ef6..4d65872 on graph, rules, cmd/sdd; read remap.go, evidence.go, phasereview.go:780-900, root.go, evaluate.go, vcs.go, git.go, cache.go; go build, go vet and the graph, rules, cmd/sdd suites green): Major — the ErrOperational split reached 2 of ~9 RevisionExists sites; verifyCleanGitIdentity (evidence.go:570), validGitTaskReviewIdentity (:952) and phasereview.go:815, :862-864, :964, :1071 still fold an operational failure into absence, masked only by evaluate.go:51-75 discarding a callback's output after recordingRepo.note records the failure — a direct caller of those rule functions (as the new P4 test is) would see the wrong diagnostic. Minor — strace of ops.test shows the one-shot shim in ops/operational_test.go:14-36 is consumed by DetectChecked's rev-parse --show-toplevel, not by RevisionExists's cat-file as its docstring says; the test still proves the intended outcome but would silently drift if detection ever made two calls. fakeP4Repo/repoCache seam and remap's target interpolation checked and sound. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: major
    title: "The remaining identity checks and the retirement probe still fold an operational failure into absence"
    status: open
    action: revise
    nodes: [propagation-validator]
    revise:
      contract: "Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection and VerifyRetirementSource wraps a failed post-detection RevisionExists so ErrOperational survives errors.Is, every identity check in internal/rules that can emit an absence diagnostic (verifyCleanGitIdentity including its ancestry probe, verifyCleanP4Identity, validGitTaskReviewIdentity and the phase-review identity checks) emits it only when the query succeeded and answered no or the revision syntax is unsupported and stays silent on ErrOperational, which the collector already holds, and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run."
      gate:
        type: tests
        tests:
          - {id: TestIgnoredRepoErrorAbortsEvaluation, file: internal/rules/operational_test.go}
          - {id: TestRetirementDetectionFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestValidationOperationalExit, file: cmd/sdd/operational_test.go, satisfies: [user-entrypoint]}
          - {id: TestP4IdentityQueryFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestIdentityQueryFailuresAreOperational, file: internal/rules/operational_test.go}
          - {id: TestRetirementProbeFailureIsOperational, file: internal/rules/operational_test.go}
  - id: F-02
    severity: minor
    title: "The remap test's one-shot git shim docstring names the wrong starved call"
    status: answered
  - id: F-03
    severity: minor
    title: "propagation-validator's artifact list omits the files its fixes land in"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84`.

## Findings
### F-01 — The remaining identity checks and the retirement probe still fold an operational failure into absence

`internal/rules/evidence.go:570` and `:578` (`verifyCleanGitIdentity`), `:952` (`validGitTaskReviewIdentity`) and `internal/rules/phasereview.go:815`, `:862-864`, `:964`, `:1071` discard or collapse the `RevisionExists`/`IsAncestor` error the way the Perforce check did before this round; `internal/rules/retirement.go:54` wraps a post-detection probe error with `%v`, so `errors.Is(err, vcs.ErrOperational)` is lost. The evaluator's abort masks the diagnostics on the registry path today, but a direct caller (the pattern the new Perforce test itself uses) sees the wrong answer. The Perforce check should also suppress only `ErrOperational`, not every non-`ErrNotFound` error.

### F-02 — The remap test's one-shot git shim docstring names the wrong starved call

strace shows the shim in `internal/graph/ops/operational_test.go:14-36` answers detection's `rev-parse --show-toplevel`, and `RevisionExists`'s `cat-file` then fails to start; the outcome is right, the comment is not.

### F-03 — propagation-validator's artifact list omits the files its fixes land in

`internal/rules/evidence.go` (and now `phasereview.go`, `retirement.go` is already listed) are not in the node's artifacts, so their digests are untracked.

## Resolution Log
### F-02 — answered

2026-09-13: Comment-only; corrected in an inline commit alongside this round's amendments (the test file is not a node artifact).

### F-03 — answered

2026-09-13: Fixed with `sdd graph set-artifacts --add` on the node immediately after this amendment applies; artifact lists are node bookkeeping, not contract.
