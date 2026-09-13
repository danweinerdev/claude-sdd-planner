---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..c5a8018b9a59115ee07e8abcdca9822683b6a0ff"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "c5a8018b9a59115ee07e8abcdca9822683b6a0ff"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..c5a8018b9a59115ee07e8abcdca9822683b6a0ff"
    evidence: "drift-detector (agent, delta 57d4ffb..c5a8018 confined to internal/procexec and cmd/sdd/doctor.go): posix-containment rev 5 lands exactly as F-01 prescribed — sweepGroupBeforeReap returns cleaned=true with the provisional kill error (contain_posix.go:102), Run makes the post-reap poll sole authority (procexec.go:105-116); TestResolvedKillFailureIsNotAnError observed red at seq 120 then green at rev 5; TestContainmentFailure unchanged and still proves an unresolved failure surfaces; all seven gate tests pass under -race. Informational: the frozen F-01 prose about cleanupGroup's immediate-ESRCH branch is subsumed by cleaned = cleaned || postCleaned, not a live gap. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..c5a8018b9a59115ee07e8abcdca9822683b6a0ff"
    evidence: "quality-scanner (agent): enumerated the sweep x cleanupGroup outcome matrix from full reads of procexec.go/contain_posix.go/wnowait_other.go — post-reap authoritative in every case, swept branch never re-signals (only signalGroup(pgid,0)), DescendantsCleaned accurate; build/gofmt/vet clean; go test -race passes incl. TestResolvedKillFailureIsNotAnError. staticcheck SA4006 at internal/procexec/procexec.go:106: pre-reap containErr binding is dead after the unconditional containErr = postErr at :123; rated Major as a required-toolchain lint regression, safe one-line fix. VERDICT: Needs changes (F-01)."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..c5a8018b9a59115ee07e8abcdca9822683b6a0ff"
    evidence: "spec-compliance (agent): FR-14/DD-4 no re-signal after reap — cleanupGroup with swept=true only polls (contain_posix.go:121-143); FR-16 operational failure propagation intact via the deadline branch; DD-10 doctor seam covered at doctor.go:323-329 with --check still failing; NFR-05 go test -race ./internal/procexec passes, go vet clean. No coverage gaps or contract violations. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..c5a8018b9a59115ee07e8abcdca9822683b6a0ff"
    evidence: "blind-spot-finder (agent, diff only; only doctor.go, contain_posix.go, procexec.go, posix_test.go changed): traced Run's containment merge across pre-reap probe failure, pre-reap kill failure, swept poll timeout, swept poll success, and the non-unix stub — no scenario where the unconditional containErr = postErr masks an unresolved failure; TestResolvedKillFailureIsNotAnError delivers a real kill under the injected EPERM (waitDead on the grandchild); go test -race -count=8 on the four sweep/cleanup tests stable. One Minor maintenance trap: cmd/sdd/doctor.go:190-194 and :223-226 print the same ContainmentSupported() platform reason twice (hook_binary_error and containment_blocker). VERDICT: Aligned."
findings:
  - id: F-01
    severity: major
    title: "Run binds a pre-reap containErr that is dead after the unconditional post-reap overwrite"
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
    title: "doctor prints the platform containment reason twice on adapter-less hosts"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..c5a8018b9a59115ee07e8abcdca9822683b6a0ff`.

## Findings
### F-01 — Run binds a pre-reap containErr that is dead after the unconditional post-reap overwrite

`internal/procexec/procexec.go:106` binds `containErr` from `sweepGroupBeforeReap`, and `:123` assigns `containErr = postErr` unconditionally before any read, so the pre-reap value is a dead store (`staticcheck` SA4006). Both quality lanes flagged it independently. The merge logic itself is correct and covered by `TestResolvedProbeFailureIsNotAnError` and `TestResolvedKillFailureIsNotAnError`; the fix is to bind the discarded error to `_` and keep the comment that the post-reap poll is the sole authority. No gate change: the existing seven tests carry over.

### F-02 — doctor prints the platform containment reason twice on adapter-less hosts

`cmd/sdd/doctor.go:190-194` and `:223-226` both render the same `ContainmentSupported()` reason, once on the hook-binary line and once as the blocker, so a wording change to one silently diverges from the other.

## Resolution Log
### F-02 — rejected

2026-09-13: Cosmetic duplication of one static predicate's text; both lines are correct and the `--check` exit path depends on neither. Recorded in SDD-DOGFOOD-NOTES.md follow-ups for a later doctor-output tidy, not a plan revision.
