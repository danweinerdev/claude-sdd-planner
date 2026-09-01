---
title: "v2-5-skills-self-hosting"
type: phase
plan: "SddGraph"
phase: 5
status: planned
created: 2026-09-01
updated: 2026-09-01
deliverable: "Graph view: 9 node(s) under phase label v2-5-skills-self-hosting"
tasks: []
---

# Phase 5: v2-5-skills-self-hosting

<!-- GENERATED VIEW — source of truth: SddGraph-Graph.json. Regenerate with `sdd compile --plan SddGraph`. Edits here are overwritten. -->

## Overview

Rendered view of 9 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### task-5-1

- Contract: The plan skill drives interview, payload, compile, and silhouette read-back and never writes plan or phase markdown by hand; both portable trees regenerate clean.
- Justifies: `DD-13`, `DD-16`
- Depends on: `task-4-1`, `task-4-2`, `task-4-3`
- Gate: command — `make test`
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 5.1 — verified 2026-09-01; revision 922b2a1dbcf57a22019f3d55e9e2fc069cb2482f
- Observation: none yet
- Closure: open — state BLOCKED

### task-5-2

- Contract: The implement skill drives the walk loop -- claim, red, green, sync, merge, integrate -- with stopping rules verbatim and no narrated completion path; gate observations bind to their own artifacts.
- Justifies: `DD-13`, `D-0022`, `DD-5`
- Depends on: `task-4-1`, `task-4-2`, `task-4-3`, `task-5-1`
- Gate: command — `make test`
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 5.2 — verified 2026-09-01; revision aa914086a9d132a6db6be8315ccd5c20e6005f6b
- Observation: none yet
- Closure: open — state BLOCKED

### task-5-3

- Contract: Portable trees, docs, and the minSddVersion floor move in lockstep: no harness can install skills naming verbs the admitted binary lacks.
- Justifies: `D-0021`, `D-0017`
- Depends on: `task-4-1`, `task-4-2`, `task-4-3`, `task-5-1`, `task-5-2`
- Gate: command — `make test`
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 5.3 — verified 2026-09-01; revision a49e6e5fab59a53c533b2c0af4ec17f18c4c5b63
- Observation: none yet
- Closure: open — state BLOCKED

### task-5-4

- Contract: The self-hosting pilot converts this plan, compiles it with the coverage invariant satisfied, and walks at least two real nodes end to end with red_seq recorded and clean-isolation merges; every design-vs-behavior discrepancy is recorded.
- Justifies: `DD-15`
- Depends on: `task-4-1`, `task-4-2`, `task-4-3`, `task-5-3`
- Gate: command — `make test`
- Hazards: none (explicit claim)
- Estimate: 2
- Observation: none yet
- Closure: open — state BLOCKED

### task-5-5

- Contract: Compile's AC-coverage demand is scoped to the plan's own directly-related specs; transitively reachable ids stay citable and fingerprinted.
- Justifies: `DD-4`
- Depends on: (nothing)
- Gate: tests — `TestACCoverageScopedToDirectSpecs` in internal/graph/compile/compile_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 5.5 -- verified 2026-09-01; revision 074d598ff1cb37917e1d9ac051fc3f4d33850d7f
- Observation: none yet
- Closure: open — state READY

### pilot-gc-branch-pruning

- Contract: `sdd graph gc` prunes workspace branches (graph/<node>-*) whose tips are reachable from the mainline HEAD and which no active claim references; unmerged work and claimed branches always survive.
- Justifies: `DD-10`
- Depends on: (nothing)
- Gate: tests — `TestGCPrunesMergedClaimBranches` in internal/graph/ops/ops_test.go (satisfies derives-state)
- Hazards: derives-state
- Artifacts: internal/graph/ops/ops.go, internal/graph/ops/ops_test.go, internal/graph/provider/provider.go
- Estimate: 2
- Observation: none yet
- Closure: open — state READY

### pilot-junit-vendor-corpus

- Contract: The JUnit parser handles vendor-emitter report shapes -- nested suites carrying properties, system-out, CDATA, quotes and newlines inside case names -- proven by a committed corpus seed and exact count assertions.
- Justifies: `DD-5`
- Depends on: (nothing)
- Gate: tests — `TestJUnitVendorShapes` in internal/graph/sync/report_vendor_test.go (satisfies external-format)
- Hazards: external-format
- Artifacts: internal/graph/sync/testdata/reports/junit-vendor.xml, internal/graph/sync/report_vendor_test.go
- Estimate: 1
- Observation: none yet
- Closure: open — state READY

### gate-v2-5-skills-self-hosting

- Contract: Every node under v2-5-skills-self-hosting survives a full four-lane review of its aggregate diff.
- Justifies: `DD-9`
- Depends on: `pilot-gc-branch-pruning`, `pilot-junit-vendor-corpus`, `task-5-1`, `task-5-2`, `task-5-3`, `task-5-4`, `task-5-5`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### gate-plan-final

- Contract: The whole plan graph survives a terminal full review: every node closed or explicitly carried forward.
- Justifies: `DD-9`, `D-0022`
- Depends on: `gate-v2-1-payload-schema`, `gate-v2-2-store-compiler-convert`, `gate-v2-3-execution-loop`, `gate-v2-4-review-gates-analytics`, `gate-v2-5-skills-self-hosting`
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
