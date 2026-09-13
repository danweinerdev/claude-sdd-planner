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
    evidence: "Read the delta 63da43f..57d4ffb over the gate's scope (8 files, commits e6a7c00 and 57d4ffb): checkHookBinary (cmd/sdd/doctor.go:305-320) now uses procexec.LookPath and procexec.Run under hookProbePolicy with no os/exec left in doctor.go (grep confirms); gitOutput (git_post_rewrite.go:354-365) no longer appends pe.Stderr after %w so the fatal line renders once; the three gate tests TestProvisionUsesOwnedRunner, TestDoctorProbeUsesOwnedRunner (AST guard plus fifo-bounded hanging hook binary) and TestGitOutputRendersStderrOnce exist and pass; hook_probe_unix/other_test.go carry the platform-split helper. procexec.go:113-119 and posix_test.go belong to posix-containment rev 4, reviewed by the execution gate. go build ./... and go test on procexec, provision and cmd/sdd pass; sha256 of all seven provision-owned-execution artifacts and all eight posix-containment artifacts match the graph's recorded digests at rev 2 and rev 4. No missing work, creep or drift."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
    evidence: "Intent-blind review of the delta 63da43f..57d4ffb with full-file context: checkHookBinary routes through procexec.LookPath/Run under the hookProbePolicy seam (doctor.go:296-320) and the AST guard in TestDoctorProbeUsesOwnedRunner confirms no os/exec remains; gitOutput's manual pe.Stderr append is gone because procexec.Error.Error (errors.go:77-93) already renders the excerpt, pinned by TestGitOutputRendersStderrOnce; the procexec.go:113-119 precedence rule (!swept || containErr == nil) was traced against sweepGroupBeforeReap/cleanupGroup/cleanupGroupPostReap (contain_posix.go:72-134) across all four (swept, containErr) cases. go build ./..., go vet ./..., gofmt -l clean; go test on cmd/sdd, procexec and provision pass. No findings; note only that comments cite frozen review ids, consistent with repo convention."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
    evidence: "Checked the delta 63da43f..57d4ffb (doctor.go, doctor_test.go, hook_probe_unix/other_test.go, git_post_rewrite.go, owned_runner_test.go, procexec.go) against FR-13/FR-14/FR-16, AC-08, DD-10's doctor-seam inventory line and DD-9 with full-file reads: checkHookBinary (doctor.go:304-324) runs through procexec.LookPath/Run under hookProbePolicy (30s/5s defaults) and reports inability ('did not answer version') rather than absence; gitOutput renders stderr once (TestGitOutputRendersStderrOnce); procexec.go:110-121 keeps a resolved pre-reap probe error from masking a successful cleanup (TestResolvedProbeFailureIsNotAnError). Every test named in both frozen F-01 revise gates was located and run: TestDoctorProbeUsesOwnedRunner, TestGitOutputRendersStderrOnce, TestProvisionUsesOwnedRunner and the six posix-containment tests pass under -race; go build ./... and go vet clean. No coverage gap, no contract violation, no new unsourced surface."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39"
    evidence: "Diff-only read of the delta 63da43f..57d4ffb with full bodies of procexec.go, contain_posix.go, contain_other.go, containment.go, doctor.go and git_post_rewrite.go; go build, go vet and go test -race on the three touched packages pass, and GOOS=windows go build ./cmd/sdd compiles. One validated finding, F-01 major: checkHookBinary (doctor.go:304-327) now calls procexec.Run unconditionally, and Run refuses with CauseContainment on any platform without an adapter (contain_other.go:16-18), so on Windows (a live target per doctor.go:312's .exe branch) sdd doctor reports a healthy pinned binary as 'present but did not answer version: ... no process-containment adapter', unlike the git-hook probe at doctor.go:96-102 which gates on !containmentOK with 'not probed'; the prior raw exec had no containment dependency, so this is a regression of this delta. Question only: hookProbePolicy is a package var overwritten by the test (no t.Parallel today). Not flagged: the swept/containErr merge (every branch traced correct), the stderr dedup (pinned by test), the AST guard's doctor.go-only scope."
findings:
  - id: F-01
    severity: major
    title: "doctor's hook-binary probe refuses on adapter-less platforms and reports a healthy binary as broken"
    status: open
    action: revise
    nodes: [provision-owned-execution]
    revise:
      contract: "Every child process launched by internal/provision and by sdd doctor's hook-binary probe (checkHookBinary) runs through procexec.Run/procexec.LookPath under a finite policy, so doctor never forks an uncontained or unbounded child; on a platform without a containment adapter the probe is skipped and reported as not probed with the platform reason, never as a broken binary; an error from a failed git carries its stderr exactly once; tests prove internal/provision and cmd/sdd/doctor.go have no direct os/exec call, that a hanging git or a hanging hook binary on the doctor path fails within the runner's bounds, and that an unsupported platform yields the not-probed report."
      gate:
        type: tests
        tests:
          - {id: TestProvisionUsesOwnedRunner, file: internal/provision/owned_runner_test.go}
          - {id: TestDoctorProbeUsesOwnedRunner, file: cmd/sdd/doctor_test.go}
          - {id: TestGitOutputRendersStderrOnce, file: internal/provision/owned_runner_test.go}
          - {id: TestDoctorProbeSkipsWhenContainmentUnsupported, file: cmd/sdd/doctor_test.go}
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..57d4ffb8f27d04b90231ce5b5baacce7a9336b39`.

## Findings
### F-01 — doctor's hook-binary probe refuses on adapter-less platforms and reports a healthy binary as broken

`checkHookBinary` (`cmd/sdd/doctor.go:304-327`) calls `procexec.Run` unconditionally; `Run` refuses with `CauseContainment` on any platform without an adapter (`contain_other.go:16-18`), so on Windows `sdd doctor` prints "present but did not answer `version`: ... no process-containment adapter" for a healthy pinned binary. The git-hook probe a few lines below already gates on `!containmentOK` with a "not probed" detail; the binary probe needs the same gate. Regression of the previous rework (the raw exec it replaced had no containment dependency).

## Resolution Log
None.
