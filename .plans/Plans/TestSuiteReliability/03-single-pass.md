---
title: "03-single-pass"
type: phase
plan: "TestSuiteReliability"
phase: 3
status: planned
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 2 node(s) under phase label 03-single-pass"
tasks: []
---

# Phase 3: 03-single-pass

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

## Overview

Rendered view of 2 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### single-pass-evaluation

- Contract: One ordinary evaluation runs each root rule once and each artifact rule once per applicable artifact; SDD176/SDD177 derive from that single result instead of a second sweep; strict Run and reporting RunWithWaivers keep their distinct semantics and canonical order, proven against the frozen corpus expectations.
- Justifies: `Specs/TestSuiteReliability:FR-07`, `Specs/TestSuiteReliability:AC-04`, `Designs/TestSuiteReliability:DD-6`
- Depends on: (nothing)
- Gate: tests — `TestOrdinaryEvaluationOnce` in internal/rules/evaluate_test.go; `TestStrictAndReportingSemanticsPreserved` in internal/rules/evaluate_test.go (satisfies derives-state)
- Hazards: derives-state
- Artifacts: internal/rules/rules.go, internal/rules/waivers.go, internal/rules/evaluate.go, internal/rules/evaluate_test.go
- Estimate: 3
- Observation: none yet
- Closure: open — state READY

### append-only-scan-once

- Contract: The append-only history scan runs once per evaluation and distributes its findings to SDD154, SDD155, SDD156 and SDD164; family results are evaluation-local, so a newly loaded root after a HEAD, index or worktree change observes the new state.
- Justifies: `Specs/TestSuiteReliability:FR-08`, `Specs/TestSuiteReliability:FR-09`, `Specs/TestSuiteReliability:AC-04`, `Designs/TestSuiteReliability:DD-6`, `Designs/TestSuiteReliability:DD-7`
- Depends on: `single-pass-evaluation`
- Gate: tests — `TestAppendOnlyScanOnce` in internal/rules/appendonly_scan_test.go; `TestReloadSeesSCMMutation` in internal/rules/appendonly_scan_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/rules/appendonly.go, internal/rules/root.go, internal/rules/appendonly_scan_test.go
- Estimate: 2
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
