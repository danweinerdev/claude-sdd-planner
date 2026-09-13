---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "63da43f5ecdc4c2feaecedb40b41b583774d0f70"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
    evidence: "Read the delta facd924..63da43f over the gate's 49 declared artifacts: three commits. a1c1491 (posix-containment rev 3) makes sweepGroupBeforeReap return swept=false on a non-ESRCH probe failure (contain_posix.go:69-101) with TestTransientProbeFailureStillKills stubbing the first sig-0 probe to EPERM; the node lists all five gate tests and its artifact digests match at 63da43f. 63da43f (unsupported-platform-diagnostic rev 2) makes diagnose add only the label (root.go:706-716) over the new noAdapterError in containment.go, with TestDiagnoseRendersPlatformOnce counting the platform and adapter phrases once; the rev-2 gate list matches. a6eeea8 maps to provision-owned-execution, a fixtures-gate node, and lies outside this diff's file scope. F-03 of the prior review stays rejected with its resolution entry. go build, go vet, windows and darwin builds and go test on procexec and cmd/sdd all clean. No missing work, creep or drift."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
    evidence: "Intent-blind review of the delta facd924..63da43f with full-file context: sweepGroupBeforeReap's probe-error branch (contain_posix.go:93) now returns swept=false and cleanupGroup (117) falls back to cleanupGroupPostReap exactly when swept is false, so the 'survived after SIGKILL' message is reachable only after an attempted kill; TestTransientProbeFailureStillKills (posix_test.go:235) passes under -race. noAdapterError (containment.go:174-185) unwraps through Error.Unwrap to ErrNoContainmentAdapter so errors.Is still matches; diagnose (root.go:698) adds only the label and TestDiagnoseRendersPlatformOnce pins exactly-once rendering, traced by hand. cmdDoctor's switch (doctor.go:100-113) skips the hook probe without setting gitHookErr and --check still fails on the containment blocker at 152. go test -race on internal/procexec and cmd/sdd ok; go vet clean; gofmt clean on touched files. staticcheck was not run in the lane's sandbox (cache dir unwritable); the coordinator ran it earlier on these packages. No findings."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
    evidence: "Checked the delta facd924..63da43f against FR-13/FR-14/FR-15/FR-16, AC-06..AC-08, FR-09/DD-7, DD-2/DD-4/DD-9/DD-10 and NFR-05 with full-file context: contain_posix.go:119-133 returns swept=false on a probe error before any SIGKILL and procexec.go:106-112 always runs cleanupGroup so the post-reap fallback probes and kills (TestTransientProbeFailureStillKills passes under -race); root.go:707-711 diagnose keeps its ErrNoContainmentAdapter gate with TestDiagnoseRendersPlatformOnce pinning the single-mention shape; provision's git and sdd probes run through procexec under the 30s/5s defaults with gitConfigPath preserving exit-1 as the determinate negative and gitOutput re-embedding stderr for the 'not a git repository' match; TestProvisionUsesOwnedRunner's AST guard covers the package. go build and go vet clean; go test -race on procexec, provision and cmd/sdd pass. No coverage gap or contract violation; the remaining doctor.go checkHookBinary seam (310, 316) predates the range and is being revised under the fixtures gate's provision node."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
    evidence: "Diff-only read of the delta facd924..63da43f (8 changed files) with full bodies of procexec.go Run (34-145), contain_posix.go sweepGroupBeforeReap/cleanupGroup/cleanupGroupPostReap, containment.go, errors.go, doctor.go and root.go; build and go test -race on procexec and cmd/sdd pass. One validated finding, F-01 major: procexec.go:106-114 keeps the pre-reap probe error in containErr even when the post-reap fallback then succeeds (postErr nil, postCleaned true), so Run returns CauseContainment with the already-resolved probe error and every caller discards a valid Result.Stdout; reproduced with a throwaway test reusing TestTransientProbeFailureStillKills's stub (error 'containment: containment: probe group N: operation not permitted' while stdout held the grandchild pid; probe test removed), and that gate test never asserts err == nil. Question only: Run and errNoAdapter each call ContainmentSupported (test-only reassignment, no production race). Not flagged: the F-02 duplication (pinned by test), doctor's skip switch, noAdapterError unwrapping."
findings:
  - id: F-01
    severity: major
    title: "A resolved pre-reap probe error masks a successful fallback cleanup"
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
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70`.

## Findings
### F-01 — A resolved pre-reap probe error masks a successful fallback cleanup

`internal/procexec/procexec.go:106-114` merges `postErr` into `containErr` only when `containErr` is nil, so the pre-reap probe error survives even after `cleanupGroup`'s fallback probed, killed and confirmed the group empty. `Run` then returns `CauseContainment` with the stale probe error and every caller discards a valid `Result.Stdout` (a successful `git rev-parse HEAD` reads as operational failure). Reproduced by execution with the transient-EPERM stub; `TestTransientProbeFailureStillKills` never asserts `err == nil`. Keep the pre-reap error only when the fallback did not get a clean answer either.

## Resolution Log
None.
