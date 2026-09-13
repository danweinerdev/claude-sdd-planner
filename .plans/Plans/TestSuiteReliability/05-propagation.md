---
title: "05-propagation"
type: phase
plan: "TestSuiteReliability"
phase: 5
status: planned
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 4 node(s) under phase label 05-propagation"
tasks: []
---

# Phase 5: 05-propagation

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

## Overview

Rendered view of 4 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### checked-detection

- Contract: vcs exposes checked detection returning (Repo, error): a missing or unrunnable git during a required probe is an operational error, never NoRepo, while a directory that is genuinely not a repository still yields NoRepo; runGit executes through procexec and a missing blob, path or revision still returns ErrNotFound after a successful query.
- Justifies: `Specs/TestSuiteReliability:FR-16`, `Specs/TestSuiteReliability:AC-08`, `Designs/TestSuiteReliability:DD-10`
- Depends on: `bounded-runner`
- Gate: tests — `TestVCSOperationalErrors` in internal/vcs/checked_test.go; `TestAuthoritativeSCMAbsence` in internal/vcs/checked_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/vcs/vcs.go, internal/vcs/git.go, internal/vcs/p4.go, internal/vcs/checked_test.go
- Estimate: 2
- Observation: none yet
- Closure: open — state BLOCKED

### cache-error-policy

- Contract: The vcs memoization decorator never caches an operational failure or a failed detection; a determinate ErrNotFound remains cacheable; a probe that fails is never stored as NoRepo.
- Justifies: `Specs/TestSuiteReliability:FR-09`, `Specs/TestSuiteReliability:AC-04`, `Designs/TestSuiteReliability:DD-7`
- Depends on: `checked-detection`
- Gate: tests — `TestOperationalFailureNotCached` in internal/vcs/cache_test.go; `TestDeterminateAbsenceCached` in internal/vcs/cache_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/vcs/cache.go, internal/vcs/cache_test.go
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### propagation-validator

- Contract: Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection, and `sdd validate` exits 2 with the cause instead of 0 or 1 when git cannot run.
- Justifies: `Specs/TestSuiteReliability:FR-16`, `Specs/TestSuiteReliability:AC-08`, `Designs/TestSuiteReliability:DD-10`
- Depends on: `checked-detection`, `single-pass-evaluation`
- Gate: tests — `TestIgnoredRepoErrorAbortsEvaluation` in internal/rules/operational_test.go; `TestRetirementDetectionFailureIsOperational` in internal/rules/operational_test.go; `TestValidationOperationalExit` in cmd/sdd/operational_test.go (satisfies user-entrypoint)
- Hazards: user-entrypoint
- Artifacts: internal/rules/root.go, internal/rules/evaluate.go, internal/rules/retirement.go, internal/rules/operational_test.go, cmd/sdd/validate.go, cmd/sdd/operational_test.go
- Estimate: 3
- Observation: none yet
- Closure: open — state BLOCKED

### propagation-graph-callers

- Contract: Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check.
- Justifies: `Specs/TestSuiteReliability:FR-16`, `Specs/TestSuiteReliability:AC-08`, `Designs/TestSuiteReliability:DD-10`
- Depends on: `propagation-validator`
- Gate: tests — `TestSyncCleanFailureIsOperational` in internal/graph/sync/operational_test.go; `TestProviderDetectionFailureIsOperational` in internal/graph/provider/operational_test.go; `TestLifecycleVerbsOperationalExit` in cmd/sdd/operational_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/graph/sync/sync.go, internal/graph/sync/operational_test.go, internal/graph/provider/provider.go, internal/graph/provider/operational_test.go, internal/graph/ops/remap.go, cmd/sdd/review.go, cmd/sdd/evidence.go, cmd/sdd/operational_test.go
- Estimate: 2
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
