---
title: "04-owned-execution"
type: phase
plan: "TestSuiteReliability"
phase: 4
status: planned
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 3 node(s) under phase label 04-owned-execution"
tasks: []
---

# Phase 4: 04-owned-execution

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

## Overview

Rendered view of 3 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### bounded-runner

- Contract: internal/procexec runs a resolved argv without a shell under a context deadline (default 30s) plus a bounded cleanup/drain allowance (5s, clipped to the enclosing deadline), retains a 64 KiB diagnostic excerpt per stream with a truncation flag while draining, enforces a finite machine-output limit whose overflow returns an incomplete-result error never parsed as success, and returns typed causes for unavailable executable, deadline, access failure, nonzero exit, overflow and drain failure.
- Justifies: `Specs/TestSuiteReliability:FR-13`, `Specs/TestSuiteReliability:FR-15`, `Designs/TestSuiteReliability:DD-2`, `Designs/TestSuiteReliability:DD-9`
- Depends on: (nothing)
- Gate: tests — `TestDeadlineIsOperational` in internal/procexec/procexec_test.go; `TestOutputOverflow` in internal/procexec/procexec_test.go; `TestLargeMachineOutput` in internal/procexec/procexec_test.go; `TestStderrDrain` in internal/procexec/procexec_test.go; `TestTypedCauses` in internal/procexec/procexec_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/procexec/procexec.go, internal/procexec/policy.go, internal/procexec/errors.go, internal/procexec/procexec_test.go, internal/procexec/helper_test.go
- Estimate: 3
- Observation: none yet
- Closure: open — state READY

### posix-containment

- Contract: On POSIX the runner starts each command in its own process group, signals only that validated positive group on deadline or cancellation, reaps direct children, cleans group-staying descendants on normal completion too, leaves an unrelated sentinel process alive, and reports a detectable containment or cleanup failure as an operational error; platforms without a containment adapter fail explicitly.
- Justifies: `Specs/TestSuiteReliability:FR-14`, `Specs/TestSuiteReliability:AC-06`, `Designs/TestSuiteReliability:DD-4`
- Depends on: `bounded-runner`
- Gate: tests — `TestOwnedProcessLifecycle` in internal/procexec/posix_test.go (satisfies concurrent-access); `TestEarlyExitInheritedPipes` in internal/procexec/posix_test.go; `TestContainmentFailure` in internal/procexec/posix_test.go
- Hazards: concurrent-access
- Artifacts: internal/procexec/contain_posix.go, internal/procexec/contain_other.go, internal/procexec/posix_test.go
- Estimate: 3
- Observation: none yet
- Closure: open — state BLOCKED

### fixture-runners-owned

- Contract: The rules example harness and the regression corpus prepare step launch fixture commands through procexec with the hermetic policy, so a hanging or over-producing setup command fails within the runner's bounds instead of stalling the package.
- Justifies: `Specs/TestSuiteReliability:FR-13`, `Specs/TestSuiteReliability:FR-14`, `Designs/TestSuiteReliability:DD-2`
- Depends on: `bounded-runner`, `prepare-once-determinism`
- Gate: tests — `TestFixtureSetupUsesOwnedRunner` in internal/rules/harness_test.go; `TestCorpusPrepareUsesOwnedRunner` in tools/regression/corpus_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/rules/harness_test.go, tools/regression/corpus.go, tools/regression/corpus_test.go
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
