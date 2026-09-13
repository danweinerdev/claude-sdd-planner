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
    evidence: "Read the delta facd924..63da43f over the gate's 36 declared artifacts: the only commit touching the provision-owned-execution amendment's artifact set is a6eeea8 (seven files: doctor.go, procexec.go, git_post_rewrite.go, provision.go, three owned_runner*_test.go). git_post_rewrite.go no longer imports os/exec (gitOutput/gitConfigPath through procexec.Run with gitPolicy defaults at 354-385), provision.go probeVersion routes through procexec.Run with binaryPolicy and both use the new exported procexec.LookPath (procexec.go:174); doctor.go:95-107 skips the hook probe when containment is unsupported. TestProvisionUsesOwnedRunner statically rejects os/exec in the package and bounds a hanging git stub to CauseDeadline; go build ./... and go test on provision, procexec and cmd/sdd pass; all seven artifact digests and the two dependency digests recorded on the node match the live files. checkHookBinary's raw exec in doctor.go predates the range and is not a git call, so outside the contract; the errors.go doc change belongs to the execution gate. No missing work, creep or drift."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
    evidence: "Intent-blind review of the delta facd924..63da43f (eight changed files) with full-file context: doctor.go:99-107 sets only gitHook.Detail when containment is unsupported and --check fails on the containment blocker first (153-165); git_post_rewrite.go gitOutput wraps the procexec error with stderr so the existing 'not a git repository' match still fires (TestPostRewriteNoGitAndBareAreExplicitNoOps passes); gitConfigPath keeps exit-1 as (false, nil) and returns the self-describing procexec error for other causes; procexec.LookPath with env=nil delegates to exec.LookPath, identical to the prior direct calls; TestProvisionUsesOwnedRunner's AST guard asserts a non-zero checked count and its fifo stub yields CauseDeadline under -race in 0.3s. go build, gofmt -l, go vet and go test -race on provision, procexec and cmd/sdd all clean. No findings; note only that doctor.go checkHookBinary (310, 316) still uses os/exec directly for the sdd binary probe, unchanged in this range."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
    evidence: "Checked the delta facd924..63da43f (a1c1491, a6eeea8, 63da43f; only doctor.go, procexec errors.go/procexec.go, provision git_post_rewrite.go/provision.go and the three owned_runner tests changed in scope) against FR-13/FR-14/FR-16, DD-2/DD-10's doctor-seam inventory item and NFR-05: gitPolicy/binaryPolicy zero values resolve to the 30s/5s defaults (policy.go:36-49); gitConfigPath keeps only CauseExit code 1 as a determinate answer and every other cause propagates to exitCode()'s 2; doctor.go:94-104 skips the hook probe under an unsupported platform instead of deriving a git failure (TestDoctorReportsMissingContainmentAdapter passes in both modes); TestProvisionUsesOwnedRunner bounds a hanging git to CauseDeadline and statically forbids os/exec in the package. go build and go vet clean; the touched tests run and pass. Minor: cmd/sdd/doctor.go checkHookBinary (310, 316) still probes the sdd binary through raw os/exec, unchanged since August and outside the provision node's contract; recorded as a tool follow-up."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70"
    evidence: "Diff-only read of the delta facd924..63da43f (398 lines: doctor.go, procexec errors.go/procexec.go, provision git_post_rewrite.go/provision.go, two new test files) with full bodies of doctor.go, cmd/sdd/provision.go, both provision files and procexec's procexec/errors/policy/containment; grepped every caller of CheckPostRewrite/InstallPostRewrite/Resolve/Provision and every os/exec use in cmd/sdd versus internal/provision; go build, go vet and go test on provision, procexec and cmd/sdd pass. Findings: F-01 major, cmd/sdd/doctor.go:310,316 checkHookBinary still uses exec.LookPath and exec.Command(p, version).Run() with no timeout or containment, in the file this delta edits, and the new AST guard is scoped to internal/provision so CI cannot see it; F-02 minor, gitOutput (git_post_rewrite.go:130-139) appends pe.Stderr after %w although procexec.Error.Error already renders it, confirmed by a temporary probe printing the fatal message twice (probe removed). Question: repair-mode doctor exits 0 after printing the containment blocker and skipping the hook install."
findings:
  - id: F-01
    severity: major
    title: "doctor's binary probe still runs through raw os/exec; gitOutput renders stderr twice"
    status: open
    action: revise
    nodes: [provision-owned-execution]
    revise:
      contract: "Every child process launched by internal/provision and by sdd doctor's hook-binary probe (checkHookBinary) runs through procexec.Run/procexec.LookPath under a finite policy, so doctor never forks an uncontained or unbounded child; an error from a failed git carries its stderr exactly once; tests prove internal/provision and cmd/sdd/doctor.go have no direct os/exec call and that a hanging git or a hanging hook binary on the doctor path fails within the runner's bounds."
      gate:
        type: tests
        tests:
          - {id: TestProvisionUsesOwnedRunner, file: internal/provision/owned_runner_test.go}
          - {id: TestDoctorProbeUsesOwnedRunner, file: cmd/sdd/doctor_test.go}
          - {id: TestGitOutputRendersStderrOnce, file: internal/provision/owned_runner_test.go}
  - id: F-02
    severity: minor
    title: "Repair-mode doctor exits 0 after reporting the containment blocker"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..63da43f5ecdc4c2feaecedb40b41b583774d0f70`.

## Findings
### F-01 — doctor's binary probe still runs through raw os/exec; gitOutput renders stderr twice

`cmd/sdd/doctor.go:310,316` (`checkHookBinary`) calls `exec.LookPath("sdd")` and `exec.Command(p, "version").Run()` with no timeout and no containment, in the file this range edits; the package-scoped AST guard in `internal/provision` cannot see it. Three independent lanes raised it. Also `gitOutput` (`internal/provision/git_post_rewrite.go:130-139`) appends `pe.Stderr` after `%w` although `procexec.Error.Error` already renders it, so a failed git prints its fatal line twice (confirmed by execution).

### F-02 — Repair-mode doctor exits 0 after reporting the containment blocker

On a platform without an adapter, `sdd doctor` (repair mode) prints the blocker, skips the hook install and exits 0; only `--check` exits nonzero.

## Resolution Log
### F-02 — answered

2026-09-13: Intended. Repair mode is a report-and-fix pass and has always exited 0 after printing what it found; `--check` is the gating form and fails on the blocker. The blocker line names the platform and the follow-on plan.
