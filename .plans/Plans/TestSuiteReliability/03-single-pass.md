---
title: "Single Pass"
type: phase
plan: "TestSuiteReliability"
phase: 3
status: complete
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 2 node(s) under phase label Single Pass"
tasks: []
---

# Phase 3: Single Pass

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

<!-- FROZEN VIEW — every node in this phase is closed: GREEN and covered by a passing frozen full review gate. This projection is history; a render that would change it is refused. -->

## Overview

Rendered view of 2 node(s) from the plan graph (schema v1, seq 494).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### single-pass-evaluation

- Contract: One ordinary evaluation runs each root rule once and each artifact rule once per applicable artifact; SDD176/SDD177 derive from that single result instead of a second sweep; strict Run and reporting RunWithWaivers keep their distinct semantics and canonical order, proven against the frozen corpus expectations.
- Justifies: `Specs/TestSuiteReliability:FR-07`, `Specs/TestSuiteReliability:AC-04`, `Designs/TestSuiteReliability:DD-6`
- Depends on: (nothing)
- Gate: tests — `TestOrdinaryEvaluationOnce` in internal/rules/evaluate_test.go; `TestStrictAndReportingSemanticsPreserved` in internal/rules/evaluate_test.go (satisfies derives-state); `TestWaiverMemoConcurrencySafe` in internal/rules/evaluate_test.go
- Hazards: derives-state
- Artifacts: internal/rules/rules.go, internal/rules/waivers.go, internal/rules/evaluate.go, internal/rules/evaluate_test.go, internal/rules/root.go
- Estimate: 3
- Observation: **pass** at seq 479 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### append-only-scan-once

- Contract: The append-only history scan runs once per evaluation and distributes its findings to SDD154, SDD155, SDD156 and SDD164; family results are evaluation-local, so a newly loaded root after a HEAD, index or worktree change observes the new state.
- Justifies: `Specs/TestSuiteReliability:FR-08`, `Specs/TestSuiteReliability:FR-09`, `Specs/TestSuiteReliability:AC-04`, `Designs/TestSuiteReliability:DD-6`, `Designs/TestSuiteReliability:DD-7`
- Depends on: `single-pass-evaluation`
- Gate: tests — `TestAppendOnlyScanOnce` in internal/rules/appendonly_scan_test.go; `TestReloadSeesSCMMutation` in internal/rules/appendonly_scan_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/rules/appendonly.go, internal/rules/root.go, internal/rules/appendonly_scan_test.go
- Estimate: 2
- Observation: **pass** at seq 480 — isolation clean, provenance git 3554448493ec
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
| `sdd graph status --plan TestSuiteReliability` | . | PASS (exit 0) | phase 3: 2/2 node(s) closed (GREEN, covered by a passing frozen full review gate) |

### Completed task identities
