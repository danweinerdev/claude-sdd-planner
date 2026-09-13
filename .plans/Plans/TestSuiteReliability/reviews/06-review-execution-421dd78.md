---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Read the range restricted to the gate's 40 declared artifacts (3727 insertions, 266 deletions) against phase views 03-single-pass, 04-owned-execution, 05-propagation and the committed graph. Every node in the gate's closure (bounded-runner, posix-containment, fixture-runners-owned, checked-detection, cache-error-policy, propagation-validator, propagation-graph-callers, single-pass-evaluation) has code matching its contract, its named gate tests present and passing, and artifacts landing where the graph declares; git grep confirms no unchecked vcs.Detect or provider.Detect call site remains in non-test Go at 421dd78. go vet, gofmt -l and go test -race clean on every touched package. No missing work, no scope creep, no approach drift in code. One documentation-staleness item: the rendered 05-propagation.md artifact list for propagation-graph-callers omits cmd/sdd/graph.go, internal/graph/ops/ops.go and internal/graph/review/review.go, which the graph JSON and its verified artifact_digests do include (added via set-artifacts); re-render the views."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Intent-blind review of the 40-artifact scope (about 3700 lines) with full-file context, cross-checked by two forked passes: procexec.go/contain_posix.go/errors.go/policy.go, vcs.go/git.go/p4.go/cache.go, rules root.go/evaluate.go/retirement.go, provider.go, sync.go, ops.go, remap.go, review.go, cmd/sdd validate/review/evidence/graph, corpus.go, testenv.go. go build, go vet, gofmt -l and go test -race clean on every touched package. The never-conflate-operational-with-determinate invariant holds at every traced site: cache.go cacheable(), exitCode's refusedError split with no ErrOperational miscategorized, every CLI and graph errors.Is(ErrNotFound) bifurcation; containment has real subprocess-lifecycle tests. Findings: F-03 major, git.go Head() passes the full error text into absent() so the ErrNotFound message doubles at the CLI; F-04 minor, asError wrapper; F-05 minor, provider Release branch-cleanup failure paths untested. Questions only: machineWriter.Write unlock/relock around onOverflow is correct but fragile; recordingRepo must track the vcs.Repo interface by hand."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Checklist of FR-13, FR-14 (POSIX), FR-15, FR-16, FR-09, AC-06..AC-08 and DD-2/DD-4/DD-7/DD-9/DD-10 mapped to the diff: procexec.go context timeout and WaitDelay, contain_posix.go Setpgid and negative-PGID signalling with the pgid<=1 guard, contain_other.go explicit refusal, machineWriter/excerptWriter limits (64 KiB / 64 MiB) with overflow never parsed as success; vcs.go DetectChecked/Unavailable with no error-erasing wrapper, cache.go cacheable() excluding ErrOperational; root.go collector plus recordingRepo, evaluate.go abort-after-callback, retirement.go checked detection, provider execRunner and DetectChecked, sync.go checked Clean, validate.go exit 2 vs refusedError exit 1. Every design Testing Strategy contract for AC-06/07/08 has a same-named test (TestOwnedProcessLifecycle, TestEarlyExitInheritedPipes, TestContainmentFailure, TestOutputOverflow, TestLargeMachineOutput, TestStderrDrain, TestVCSOperationalErrors, TestAuthoritativeSCMAbsence, TestIgnoredRepoErrorAbortsEvaluation, TestValidationOperationalExit, TestLifecycleVerbsOperationalExit). Two optional notes: no TestMutationNotRetried (no retry path exists in procexec.Run or runGit) and no exit-128-specific absence case beyond the CauseExit-gated absent() helper."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Diff-only read of the 40-artifact scope with full-file context: procexec.go, contain_posix.go, contain_other.go, errors.go, policy.go, vcs.go, git.go, p4.go, cache.go, root.go, evaluate.go, retirement.go, provider.go, sync.go, review.go, ops.go, remap.go, cmd/sdd validate/review/evidence/graph, corpus.go, testenv.go; compared git.go/p4.go/provider.go/retirement.go/corpus.go against 04c1e61 to confirm they moved from os/exec to procexec.Run. Two validated findings: F-01 contain_other.go refuses every command on non-unix builds, so on Windows every VCS-touching verb now exits 2 with a message indistinguishable from a missing git (the refusal itself is the recorded decision pd-52a6f11c; the indistinguishable message and the absence of a doctor diagnostic are the gap). F-02 cleanupGroup runs only after cmd.Wait reaped the leader, so its kill(-pgid) probe/kill can target a group whose leader pid was recycled; no test covers reuse. Not flagged: the operational-vs-authoritative core (other lanes), printUntracked UX, EditArtifacts duplication, Release's is-ancestor + branch -d (branch -d is the safety boundary). Questions only: Policy.Apply process-global mutation is called from TestMain once; cache.go check-compute-write gap is duplicate work not corruption in the single-threaded CLI."
findings:
  - id: F-01
    severity: major
    title: "Unsupported-platform refusal is indistinguishable from a missing git"
    status: open
    action: extend
    node:
      id: unsupported-platform-diagnostic
      contract: "On a platform without a process-containment adapter, the runner's refusal names the platform and the missing adapter with a distinct cause, sdd doctor --check reports it as a blocker naming the follow-on plan, and the validate and graph verbs print that platform message rather than a generic could-not-run-git failure."
      deps: [posix-containment]
      gate:
        type: tests
        tests:
          - {id: TestUnsupportedPlatformRefusalIsDistinct, file: internal/procexec/procexec_test.go}
          - {id: TestDoctorReportsMissingContainmentAdapter, file: cmd/sdd/doctor_test.go}
      hazards: []
      artifacts: [internal/procexec/contain_other.go, internal/procexec/errors.go, internal/procexec/procexec_test.go, cmd/sdd/doctor.go, cmd/sdd/doctor_test.go]
  - id: F-02
    severity: major
    title: "Post-reap process-group sweep can target a recycled pid"
    status: open
    action: revise
    nodes: [posix-containment]
    revise:
      contract: "On POSIX the runner starts each command in its own process group, signals only that validated positive group on deadline or cancellation, reaps direct children, cleans group-staying descendants on normal completion too, leaves an unrelated sentinel process alive, and reports a detectable containment or cleanup failure as an operational error; the descendant sweep signals the group while the leader is still unreaped (observed exited but not yet waited), so a recycled pid can never be targeted, and after reaping only an emptiness probe remains; platforms without a containment adapter fail explicitly."
      gate:
        type: tests
        tests:
          - {id: TestOwnedProcessLifecycle, file: internal/procexec/posix_test.go, satisfies: [concurrent-access]}
          - {id: TestEarlyExitInheritedPipes, file: internal/procexec/posix_test.go}
          - {id: TestContainmentFailure, file: internal/procexec/posix_test.go}
          - {id: TestGroupSweepPrecedesReap, file: internal/procexec/posix_test.go}
  - id: F-03
    severity: major
    title: "Head() doubles the underlying error text in its absence message"
    status: open
    action: revise
    nodes: [checked-detection]
    revise:
      gate:
        type: tests
        tests:
          - {id: TestVCSOperationalErrors, file: internal/vcs/checked_test.go}
          - {id: TestAuthoritativeSCMAbsence, file: internal/vcs/checked_test.go}
          - {id: TestAbsenceMessagesAreNotDoubled, file: internal/vcs/checked_test.go}
  - id: F-04
    severity: minor
    title: "asError is a one-line wrapper around errors.As"
    status: rejected
  - id: F-05
    severity: minor
    title: "gitProvider.Release branch-cleanup failure paths are untested"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4`.

## Findings
### F-01 — Unsupported-platform refusal is indistinguishable from a missing git

`internal/procexec/contain_other.go` refuses every command on non-unix builds (the recorded decision pd-52a6f11c: never run uncontained). Because git, p4, fixture setup and the provider now all go through `procexec.Run`, a Windows user sees every VCS-aware verb exit 2 with a message shaped like "git could not run", and `sdd doctor` says nothing. The refusal stands; it must be loud and named.

### F-02 — Post-reap process-group sweep can target a recycled pid

`cleanupGroup` in `internal/procexec/contain_posix.go` runs after `cmd.Wait()` has reaped the group leader, then probes and kills `-pgid` using the leader's now-released pid. Under pid churn the kernel can hand that pid to an unrelated new group leader before the sweep runs. The cancellation path is unaffected (it signals while the leader is alive); only the post-completion sweep is exposed. No test covers reuse.

### F-03 — Head() doubles the underlying error text in its absence message

`internal/vcs/git.go` `Head()` calls `absent(err, "HEAD: "+err.Error())` while every sibling passes a short identifier, so the ErrNotFound branch carries the git error twice once the CLI wraps it again (`cmd/sdd/evidence.go`, `review.go` headOf). Three independent quality passes converged on it.

### F-04 — asError is a one-line wrapper around errors.As

`internal/procexec/errors.go` `asError` has one call site (`IsCause`).

### F-05 — gitProvider.Release branch-cleanup failure paths are untested

The `rev-parse --abbrev-ref HEAD` failure and a `git branch -d` failure after a successful ancestor check in `internal/graph/provider/provider.go` have no test.

## Resolution Log
### F-04 — rejected

2026-09-13: Kept as is. The wrapper exists so `errors.go` owns the typed-error contract; inlining it moves nothing of value and touches a verified node for no behavior change.

### F-05 — rejected

2026-09-13: Out of this plan's contracts. The branch-deletion-on-release code is the sdd tool fix from commit 261176c (dogfood note P-06), not a TestSuiteReliability node; its resilience test is tracked in SDD-DOGFOOD-NOTES.md for the tool follow-up, not as plan work.
