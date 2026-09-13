---
title: "Owned Execution"
type: phase
plan: "TestSuiteReliability"
phase: 4
status: complete
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 3 node(s) under phase label Owned Execution"
tasks: []
---

# Phase 4: Owned Execution

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

<!-- FROZEN VIEW — every node in this phase is closed: GREEN and covered by a passing frozen full review gate. This projection is history; a render that would change it is refused. -->

## Overview

Rendered view of 3 node(s) from the plan graph (schema v1, seq 494).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### bounded-runner

- Contract: internal/procexec runs a resolved argv without a shell under a context deadline (default 30s) plus a bounded cleanup/drain allowance (5s, clipped to the enclosing deadline), retains a 64 KiB diagnostic excerpt per stream with a truncation flag while draining, enforces a finite machine-output limit whose overflow returns an incomplete-result error never parsed as success, returns typed causes for unavailable executable, deadline, access failure, nonzero exit, overflow and drain failure, and never re-executes a command on its own: a mutating command that fails ambiguously runs exactly once per Run call.
- Justifies: `Specs/TestSuiteReliability:FR-13`, `Specs/TestSuiteReliability:FR-15`, `Designs/TestSuiteReliability:DD-2`, `Designs/TestSuiteReliability:DD-9`
- Depends on: (nothing)
- Gate: tests — `TestDeadlineIsOperational` in internal/procexec/procexec_test.go; `TestOutputOverflow` in internal/procexec/procexec_test.go; `TestLargeMachineOutput` in internal/procexec/procexec_test.go; `TestStderrDrain` in internal/procexec/procexec_test.go; `TestTypedCauses` in internal/procexec/procexec_test.go; `TestMutationNotRetried` in internal/procexec/procexec_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/procexec/procexec.go, internal/procexec/policy.go, internal/procexec/errors.go, internal/procexec/procexec_test.go, internal/procexec/helper_test.go
- Estimate: 3
- Observation: **pass** at seq 475 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### posix-containment

- Contract: On POSIX the runner starts each command in its own process group, signals only that validated positive group on deadline or cancellation, reaps direct children, cleans group-staying descendants on normal completion too, leaves an unrelated sentinel process alive, and reports a detectable containment or cleanup failure as an operational error; the descendant sweep signals the group while the leader is still unreaped (observed exited but not yet waited), so a recycled pid can never be targeted, and after reaping only an emptiness probe remains; platforms without a containment adapter fail explicitly.
- Justifies: `Specs/TestSuiteReliability:FR-14`, `Specs/TestSuiteReliability:AC-06`, `Designs/TestSuiteReliability:DD-4`
- Depends on: `bounded-runner`
- Gate: tests — `TestOwnedProcessLifecycle` in internal/procexec/posix_test.go (satisfies concurrent-access); `TestEarlyExitInheritedPipes` in internal/procexec/posix_test.go; `TestContainmentFailure` in internal/procexec/posix_test.go; `TestGroupSweepPrecedesReap` in internal/procexec/posix_test.go; `TestTransientProbeFailureStillKills` in internal/procexec/posix_test.go; `TestResolvedProbeFailureIsNotAnError` in internal/procexec/posix_test.go; `TestResolvedKillFailureIsNotAnError` in internal/procexec/posix_test.go
- Hazards: concurrent-access
- Artifacts: internal/procexec/contain_posix.go, internal/procexec/contain_other.go, internal/procexec/posix_test.go, internal/procexec/procexec.go, internal/procexec/helper_test.go, internal/procexec/wnowait_linux.go, internal/procexec/wnowait_other.go, go.mod
- Estimate: 3
- Observation: **pass** at seq 478 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### fixture-runners-owned

- Contract: The rules example harness and the regression corpus prepare step launch fixture commands through procexec with the hermetic policy, so a hanging or over-producing setup command fails within the runner's bounds instead of stalling the package.
- Justifies: `Specs/TestSuiteReliability:FR-13`, `Specs/TestSuiteReliability:FR-14`, `Designs/TestSuiteReliability:DD-2`
- Depends on: `bounded-runner`, `prepare-once-determinism`
- Gate: tests — `TestFixtureSetupUsesOwnedRunner` in internal/rules/harness_test.go; `TestCorpusPrepareUsesOwnedRunner` in tools/regression/corpus_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/rules/harness_test.go, tools/regression/corpus.go, tools/regression/corpus_test.go
- Estimate: 1
- Observation: **pass** at seq 488 — isolation clean, provenance git 3554448493ec
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
| `sdd graph status --plan TestSuiteReliability` | . | PASS (exit 0) | phase 4: 3/3 node(s) closed (GREEN, covered by a passing frozen full review gate) |

### Completed task identities
