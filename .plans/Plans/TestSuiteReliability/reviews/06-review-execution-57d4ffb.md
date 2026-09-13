---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
    evidence: "Read the delta 63da43f..57d4ffb (commits e6a7c00 for the fixtures gate's provision rev 2 and 57d4ffb for this gate's posix-containment rev 4). procexec.go:116 now reads 'if !swept || containErr == nil { containErr = postErr }', the exact narrowing F-01 prescribed; TestResolvedProbeFailureIsNotAnError (posix_test.go:307-358) stubs a one-shot EPERM probe that the fallback resolves and asserts err nil, exit 0 and DescendantsCleaned; TestContainmentFailure (140-160) is untouched, keeps signalGroup failing unconditionally and still asserts CauseContainment. Both pass under go test -race on internal/procexec; go build ./... clean. The graph node is at contract_rev 4 with result pass at seq 107 and its gate list matches the revise verbatim. No other file in the reviewed paths changed outside the two accounted commits. No missing work, creep or drift."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
    evidence: "Intent-blind review of the delta 63da43f..57d4ffb with full-file context and an independent fork cross-check, tracing all seven return branches of sweepGroupBeforeReap against Run's merge: the fix for the probe-failure case is correct and TestResolvedProbeFailureIsNotAnError passes under -race; doctor's probe routing is sound and bounded. go vet, gofmt -l, staticcheck clean; go test -race on procexec and cmd/sdd pass. Findings: F-01 major, contain_posix.go:98-100 returns swept=true with the kill error when the probe succeeded but SIGKILL failed non-ESRCH, so procexec.go:118-120's guard keeps the stale killErr and discards cleanupGroup's poll answer even when it observes the group empty; no test drives probe-succeeds/kill-fails (existing stubs fail every call or only the probe). Minor: cleanupGroup's swept success path returns cleaned=false on an empty group, so DescendantsCleaned would read false once F-01 lands; land together."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
    evidence: "Checked the delta 63da43f..57d4ffb against FR-13/FR-14/FR-15/FR-16, AC-06..AC-08, FR-09/DD-7, DD-2/DD-4/DD-9/DD-10 and NFR-05 with full-file reads of procexec.go, contain_posix.go, posix_test.go, doctor.go, doctor_test.go, git_post_rewrite.go and owned_runner_test.go: procexec.go:105-116 gates the containment merge on !swept || containErr == nil, consistent with sweepGroupBeforeReap's contract (swept false only when no kill was attempted) so a resolved transient probe failure is not a detectable cleanup failure while a real fallback failure still surfaces through postErr; TestResolvedProbeFailureIsNotAnError asserts err nil, exit 0 and DescendantsCleaned; doctor's checkHookBinary probes through procexec.LookPath/Run under hookProbePolicy (DD-10 doctor seam) with its bounded-hang test; gitOutput renders stderr once with TestGitOutputRendersStderrOnce. No vcs/rules hunks in the delta. go build ./..., go vet and go test on procexec, provision and cmd/sdd pass; the sibling TestTransientProbeFailureStillKills still passes. No coverage gap, no contract violation."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
    evidence: "Diff-only read of the delta 63da43f..57d4ffb with full bodies of procexec.go, contain_posix.go, contain_other.go, containment.go, wnowait_linux/other.go, policy.go and doctor.go, walking the two callers of Run/checkHookBinary and running the relevant tests with -race (pass). The merge condition '!swept || containErr == nil' correctly prefers the fallback's answer whenever the sweep did not complete and is covered by TestResolvedProbeFailureIsNotAnError. Two minors: the swept=true and kill-failed branch (contain_posix.go:98-100) keeps the kill error even if cleanupGroup's swept-path poll then finds the group empty, and no test exercises a failed kill with vanished descendants; the hanging-binary subtest (doctor_test.go:308-333) registers no cleanup for the fifo stub process on its timeout path, reachable only if the bound regresses. Question: whether doctor's probe should use a shorter timeout than the 30s default. Not flagged: the LookPath migration (nil env delegates to exec.LookPath), contain_other.go untouched."
findings:
  - id: F-01
    severity: major
    title: "A failed pre-reap kill keeps its error even when the post-reap poll finds the group empty"
    status: open
    action: revise
    nodes: [posix-containment]
    revise:
      gate:
        type: tests
        tests:
          - {id: TestOwnedProcessLifecycle, file: internal/procexec/posix_test.go, satisfies: [concurrent-access]}
          - {id: TestEarlyExitInheritedPipes, file: internal/procexec/posix_test.go}
          - {id: TestContainmentFailure, file: internal/procexec/posix_test.go}
          - {id: TestGroupSweepPrecedesReap, file: internal/procexec/posix_test.go}
          - {id: TestTransientProbeFailureStillKills, file: internal/procexec/posix_test.go}
          - {id: TestResolvedProbeFailureIsNotAnError, file: internal/procexec/posix_test.go}
          - {id: TestResolvedKillFailureIsNotAnError, file: internal/procexec/posix_test.go}
  - id: F-02
    severity: minor
    title: "Hanging-binary subtest leaks its fifo stub on the timeout path"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39`.

## Findings
### F-01 — A failed pre-reap kill keeps its error even when the post-reap poll finds the group empty

`sweepGroupBeforeReap` returns `swept=true` with the kill error when the probe succeeded but `SIGKILL` failed non-ESRCH (`contain_posix.go:98-100`); `Run`'s guard `!swept || containErr == nil` (`procexec.go:118-120`) then keeps that stale error and discards `cleanupGroup`'s poll answer even when the poll observes the group empty, so a command that completed with its descendants gone reads as `CauseContainment`. Same class as the probe-failure fix, one branch over; untested (existing stubs fail every call or only the probe). Also make the swept success path report `cleaned=true` on an empty group so `DescendantsCleaned` stays accurate.

### F-02 — Hanging-binary subtest leaks its fifo stub on the timeout path

`cmd/sdd/doctor_test.go` registers no cleanup for the stub process when the bound regresses; reachable only on a regression.

## Resolution Log
### F-02 — rejected

2026-09-13: Test hygiene reachable only when the bound regresses; the provision node's current rework adds the cleanup, and it is recorded in SDD-DOGFOOD-NOTES.md, not a plan revision.
