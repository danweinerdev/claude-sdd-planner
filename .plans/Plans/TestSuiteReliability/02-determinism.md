---
title: "02-determinism"
type: phase
plan: "TestSuiteReliability"
phase: 2
status: planned
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 3 node(s) under phase label 02-determinism"
tasks: []
---

# Phase 2: 02-determinism

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

## Overview

Rendered view of 3 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### prepare-once-determinism

- Contract: Each Bad example's determinism case prepares files and Git history exactly once, evaluates four independently loaded roots, compares complete ordered diagnostics (code, severity, path, line, message, correction, implicated, waived reason) so a drifting message fails even when codes match, and leaves fixture bytes, index and HEAD unchanged.
- Justifies: `Specs/TestSuiteReliability:FR-04`, `Specs/TestSuiteReliability:FR-05`, `Specs/TestSuiteReliability:AC-02`, `Designs/TestSuiteReliability:DD-8`
- Depends on: `test-git-policy`
- Gate: tests — `TestDeterminismPreparationCount` in internal/rules/harness_test.go; `TestCompleteDiagnosticComparison` in internal/rules/harness_test.go (satisfies deterministic-replay); `TestValidationLeavesFixtureUnchanged` in internal/rules/harness_test.go; `TestRunIsDeterministic` in internal/rules/rules_test.go
- Hazards: deterministic-replay
- Artifacts: internal/rules/rules_test.go, internal/rules/harness_test.go
- Estimate: 2
- Observation: none yet
- Closure: open — state BLOCKED

### fixture-reproducibility

- Contract: Two independently constructed real-Git fixtures of the same example produce identical complete diagnostics after enumerated root-alias normalization, and evaluating a local rule list in reversed registration order produces the same canonical diagnostics as registry order.
- Justifies: `Specs/TestSuiteReliability:FR-05`, `Specs/TestSuiteReliability:AC-02`, `Designs/TestSuiteReliability:DD-8`
- Depends on: `prepare-once-determinism`, `single-pass-evaluation`
- Gate: tests — `TestIndependentFixtureReproducibility` in internal/rules/reproducibility_test.go; `TestRuleOrderIndependence` in internal/rules/reproducibility_test.go (satisfies order-sensitive)
- Hazards: order-sensitive
- Artifacts: internal/rules/reproducibility_test.go
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### pure-selection

- Contract: A documented `make test-pure` selection executes a nonzero declared inventory of TestPure* cases with no SCM on PATH and zero process starts, and an inventory test proves every relocated real-SCM boundary assertion still executes in the full suite.
- Justifies: `Specs/TestSuiteReliability:FR-06`, `Specs/TestSuiteReliability:AC-03`, `Designs/TestSuiteReliability:DD-8`
- Depends on: `prepare-once-determinism`
- Gate: tests — `TestPureSelectionInventory` in internal/rules/inventory_test.go; `TestSCMBoundaryInventory` in internal/rules/inventory_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/rules/pure_test.go, internal/rules/inventory_test.go, Makefile, CLAUDE.md
- Estimate: 2
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
