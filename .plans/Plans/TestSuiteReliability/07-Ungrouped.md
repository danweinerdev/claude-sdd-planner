---
title: "Ungrouped"
type: phase
plan: "TestSuiteReliability"
phase: 7
status: complete
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 3 node(s) under phase label Ungrouped"
tasks: []
---

# Phase 7: Ungrouped

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

<!-- FROZEN VIEW — every node in this phase is closed: GREEN and covered by a passing frozen full review gate. This projection is history; a render that would change it is refused. -->

## Overview

Rendered view of 3 node(s) from the plan graph (schema v1, seq 494).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### unsupported-platform-diagnostic

- Contract: On a platform without a process-containment adapter, the runner's refusal names the platform and the missing adapter with a distinct cause, sdd doctor --check reports it as a blocker naming the follow-on plan, and the validate and graph verbs print that platform message rather than a generic could-not-run-git failure.
- Justifies: `Plans/TestSuiteReliability/reviews/06-review-execution-421dd78:F-01`
- Depends on: `posix-containment`
- Gate: tests — `TestUnsupportedPlatformRefusalIsDistinct` in internal/procexec/procexec_test.go; `TestDoctorReportsMissingContainmentAdapter` in cmd/sdd/doctor_test.go; `TestDiagnoseRendersPlatformOnce` in cmd/sdd/root_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/procexec/contain_other.go, internal/procexec/errors.go, internal/procexec/procexec_test.go, cmd/sdd/doctor.go, cmd/sdd/doctor_test.go, internal/procexec/containment.go, internal/procexec/contain_posix.go, internal/procexec/procexec.go, internal/vcs/git.go, internal/vcs/vcs.go, cmd/sdd/root.go, cmd/sdd/main.go, cmd/sdd/root_test.go
- Estimate: 1
- Observation: **pass** at seq 491 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### race-gate

- Contract: make test runs go vet ./... and go test -race over the shared-state packages (internal/rules, internal/procexec, internal/vcs, internal/graph/sync, internal/graph/ops, internal/graph/provider, tools/regression) through vet and test-race targets it depends on, so a vet finding, a removed memo or a containment lock fails the standard gate; gate tests parse the Makefile and refuse a test recipe that does not invoke the vet target or the race target over those packages.
- Justifies: `Plans/TestSuiteReliability/reviews/06-review-fixtures-4d10f15:F-01`
- Depends on: `single-pass-evaluation`
- Gate: tests — `TestMakeTestRunsRaceDetector` in tools/testgate/race_test.go; `TestMakeTestRunsVet` in tools/testgate/race_test.go
- Hazards: none (explicit claim)
- Artifacts: Makefile, tools/testgate/race_test.go, CLAUDE.md
- Estimate: 1
- Observation: **pass** at seq 483 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### provision-owned-execution

- Contract: Every child process launched by internal/provision and by sdd doctor's hook-binary probe (checkHookBinary) runs through procexec.Run/procexec.LookPath under a finite policy, so doctor never forks an uncontained or unbounded child; on a platform without a containment adapter the probe is skipped and reported as not probed with the platform reason, never as a broken binary; an error from a failed git carries its stderr exactly once; tests prove internal/provision and cmd/sdd/doctor.go have no direct os/exec call, that a hanging git or a hanging hook binary on the doctor path fails within the runner's bounds, and that an unsupported platform yields the not-probed report.
- Justifies: `Plans/TestSuiteReliability/reviews/06-review-fixtures-facd924-b:F-01`
- Depends on: `bounded-runner`, `child-validation-hermetic`
- Gate: tests — `TestProvisionUsesOwnedRunner` in internal/provision/owned_runner_test.go; `TestDoctorProbeUsesOwnedRunner` in cmd/sdd/doctor_test.go; `TestGitOutputRendersStderrOnce` in internal/provision/owned_runner_test.go; `TestDoctorProbeSkipsWhenContainmentUnsupported` in cmd/sdd/doctor_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/provision/git_post_rewrite.go, internal/provision/owned_runner_test.go, internal/provision/owned_runner_unix_test.go, internal/provision/owned_runner_other_test.go, internal/provision/provision.go, internal/procexec/procexec.go, cmd/sdd/doctor.go, cmd/sdd/doctor_test.go, cmd/sdd/hook_probe_unix_test.go, cmd/sdd/hook_probe_other_test.go
- Estimate: 1
- Observation: **pass** at seq 489 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

## Acceptance Criteria

- [x] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

- Verified: 2026-09-13
- Repository: /home/daniel/Development/Code/claude-sdd-planner
- VCS: git
- Revision / checkpoint: `3554448493ec74ae41630eb3f40727076c4224d9`
- Identity recheck: revision-exists probe for `3554448493ec74ae41630eb3f40727076c4224d9` at 2026-09-13T00:00:00 — matched

| Command | Working directory | Result | Observable evidence |
| --- | --- | --- | --- |
| `sdd graph status --plan TestSuiteReliability` | . | PASS (exit 0) | phase 7: 3/3 node(s) closed (GREEN, covered by a passing frozen full review gate) |

### Completed task identities
