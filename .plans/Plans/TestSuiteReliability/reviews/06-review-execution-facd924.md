---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Read the delta 421dd78..facd924 (8 commits, 24 files, 789 insertions, 35 deletions) over the gate's declared artifacts plus the graph JSON and all four review artifacts, with full-file context. Every commit maps to a node: unsupported-platform-diagnostic (bf004eb: containment.go ErrNoContainmentAdapter/ContainmentProbe, doctor ContainmentBlocker and --check exit, diagnose() prefix in main.go/root.go), posix-containment rev 2 (4d10f15: sweepGroupBeforeReap via wnowait_linux.go Waitid WNOWAIT, documented fallback in wnowait_other.go, TestGroupSweepPrecedesReap reading /proc state Z), checked-detection rev 2 (6b0baf7: Head() absent(err, HEAD), %v to %w in runGit/IsAncestor/Unavailable.fail, TestAbsenceMessagesAreNotDoubled), plus the fixtures-gate amendments race-gate (138c98d) and child-validation-hermetic rev 2/3 (bd31a4d, facd924). Tool-fix hunks in graph.go, root.go, ops.go and the go.mod x/sys promotion are outside every node's artifact set and outside the plan, as declared. No missing work, no scope creep, no approach drift; F-04/F-05 of the prior review were rejected and correctly have no code. Rendered views still lag the graph until re-rendered."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Intent-blind review of the delta 421dd78..facd924 (24 files, 789 insertions, 35 deletions) with full-file context; go vet, gofmt -l, staticcheck, go test -race on internal/procexec, internal/rules, internal/vcs, internal/graph/ops, cmd/sdd, tools/regression, and windows/darwin cross-builds all clean. The pre-reap sweep closes the pid-recycling window on Linux with an honest fallback elsewhere; bareMu avoids self-deadlock because evaluate excludes waiverRuleCodes; the %v to %w change resolves the Head() doubling with a passing test. Findings: F-02 major, cmd/sdd/root.go:707-716 diagnose re-prepends the reason that procexec.errNoAdapter already embeds, so the CLI refusal repeats the platform sentence twice (reproduced with the fixture's reason string; only call site that does this; no test covers diagnose); F-03 minor, carryOverRedSeqs in internal/graph/ops has no case where a genuine gate change keeps a red. Darwin runtime window is documented, build-only verified."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Checked the delta 421dd78..facd924 (bd31a4d, b34624a, 6b0baf7, c9bf9f7, 4d10f15, bf004eb, 138c98d, facd924) against FR-13, FR-14 POSIX, FR-15, FR-16, FR-09, AC-06..AC-08, DD-2/DD-4/DD-7/DD-9/DD-10 and NFR-05 with full-file context: sweepGroupBeforeReap signals only the validated pgid while the leader is a zombie (contain_posix.go, wnowait_linux.go; TestGroupSweepPrecedesReap), containment.go ErrNoContainmentAdapter/ContainmentSupported with pre-launch refusal (TestUnsupportedPlatformRefusalIsDistinct) and doctor blocker with nonzero --check (TestDoctorReportsMissingContainmentAdapter), exitCode keeps refusedError=1 and everything else 2, git.go/vcs.go %v to %w so errors.Is chains hold and Head() no longer nests the raw git text (TestAbsenceMessagesAreNotDoubled), setupEnv overrides removed in rules_test.go and corpus.go with TestCorpusSetupInheritsPolicy, Makefile test-race over seven shared-state packages as a prerequisite of test with TestMakeTestRunsRaceDetector enforcing it, bareMu on Root with TestWaiverMemoConcurrencySafe. No coverage gap, no contract violation in scope; the repair-red/carry-over hunks belong to ReviewDrivenAmendment and were not scored here. Verification by inspection and test presence, not by an independent -race run."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Diff-only read of the delta 421dd78..facd924 with full files: procexec.go (Start, sweepGroupBeforeReap, Wait, cleanupGroup ordering at 106-116), contain_posix.go, wnowait_linux.go, wnowait_other.go, containment.go, contain_other.go, doctor.go, root.go/main.go diagnose, vcs git.go/vcs.go wrapping, rules root.go/waivers.go bareMu, ops.go carryOverRedSeqs. Build tags compile for linux, darwin and windows; go test -race ./internal/procexec passes. One validated finding: in sweepGroupBeforeReap (contain_posix.go:69-94) a non-ESRCH error from the pre-kill probe returns swept=true before any SIGKILL was sent; cleanupGroup's swept branch (102-124) then only polls, never signals, and after the allowance reports 'descendants survived after SIGKILL' although no kill was issued; TestContainmentFailure stubs a permanent EPERM so this transient path is untested. Checked and clean: bareMu reentrancy (evaluate excludes waiverRuleCodes), multi-%w wrapping, carryOverRedSeqs key collisions (keyed by test id). Questions: kill(-pgid) ESRCH semantics under partial groups; 5s cleanup under CI load."
findings:
  - id: F-01
    severity: major
    title: "A transient probe failure marks the group swept without sending the kill"
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
  - id: F-02
    severity: major
    title: "diagnose() renders the unsupported-platform reason twice"
    status: open
    action: revise
    nodes: [unsupported-platform-diagnostic]
    revise:
      gate:
        type: tests
        tests:
          - {id: TestUnsupportedPlatformRefusalIsDistinct, file: internal/procexec/procexec_test.go}
          - {id: TestDoctorReportsMissingContainmentAdapter, file: cmd/sdd/doctor_test.go}
          - {id: TestDiagnoseRendersPlatformOnce, file: cmd/sdd/root_test.go}
  - id: F-03
    severity: minor
    title: "carryOverRedSeqs has no test for a genuine gate change that keeps a red"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc`.

## Findings
### F-01 — A transient probe failure marks the group swept without sending the kill

`sweepGroupBeforeReap` (`internal/procexec/contain_posix.go:69-94`) returns `swept=true` when the pre-kill `kill(-pgid, 0)` probe fails with a non-ESRCH error, before any SIGKILL was sent. `Run` then calls `cleanupGroup(..., swept=true)`, whose branch only polls and never signals, so a live descendant runs out the allowance and the error says it "survived after SIGKILL" although none was issued. `TestContainmentFailure` stubs a permanent EPERM and never reaches this path. Report `swept` only once a kill was attempted (or ESRCH), so the post-reap fallback still kills.

### F-02 — diagnose() renders the unsupported-platform reason twice

`cmd/sdd/root.go:707-716` builds `"unsupported platform: " + reason + ": " + err.Error()` while `procexec.errNoAdapter()` already embeds the same reason in `err.Error()`, so the final stderr line repeats the platform/adapter/follow-on sentence twice. The same bug class the Head() fix in this range removed; no test covers `diagnose`.

### F-03 — carryOverRedSeqs has no test for a genuine gate change that keeps a red

`internal/graph/ops/ops.go` `carryOverRedSeqs` is only exercised by the no-op and rename cases.

## Resolution Log
### F-03 — rejected

2026-09-13: Outside this plan's contracts. The carry-over is the sdd tool fix from commit c9bf9f7 (dogfood B-01); the missing test case is recorded in SDD-DOGFOOD-NOTES.md for the tool follow-up rather than as plan work.
