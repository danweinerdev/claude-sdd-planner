---
title: "06-acceptance"
type: phase
plan: "TestSuiteReliability"
phase: 6
status: planned
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 4 node(s) under phase label 06-acceptance"
tasks: []
---

# Phase 6: 06-acceptance

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

## Overview

Rendered view of 4 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### review-fixtures

- Contract: The hermetic-Git, determinism and single-pass slices survive a full four-lane review of their aggregate diff.
- Justifies: `Specs/TestSuiteReliability:AC-01`, `Specs/TestSuiteReliability:AC-02`, `Specs/TestSuiteReliability:AC-03`, `Specs/TestSuiteReliability:AC-04`
- Depends on: `child-validation-hermetic`, `fixture-reproducibility`, `pure-selection`, `append-only-scan-once`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### review-execution

- Contract: The owned-execution and operational-propagation slices survive a full four-lane review of their aggregate diff.
- Justifies: `Specs/TestSuiteReliability:AC-06`, `Specs/TestSuiteReliability:AC-07`, `Specs/TestSuiteReliability:AC-08`
- Depends on: `posix-containment`, `fixture-runners-owned`, `cache-error-policy`, `propagation-graph-callers`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### full-gate

- Contract: The complete uncached Go suite, go vet, the template gate and the portable drift/leak gates pass offline with the frozen corpus expectations unchanged.
- Justifies: `Specs/TestSuiteReliability:AC-11`, `Specs/TestSuiteReliability:NFR-01`, `Specs/TestSuiteReliability:NFR-05`
- Depends on: `review-fixtures`, `review-execution`
- Gate: command — `go vet ./... && make test`
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### review-final

- Contract: The whole plan diff survives a final full four-lane review whose aggregate diff digest matches the integrated tree.
- Justifies: `Specs/TestSuiteReliability:AC-12`
- Depends on: `full-gate`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
