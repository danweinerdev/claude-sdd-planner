---
title: "v2-3-execution-loop"
type: phase
plan: "SddGraph"
phase: 3
status: planned
created: 2026-09-01
updated: 2026-09-01
deliverable: "Graph view: 7 node(s) under phase label v2-3-execution-loop"
tasks: []
---

# Phase 3: v2-3-execution-loop

<!-- GENERATED VIEW — source of truth: SddGraph-Graph.json. Regenerate with `sdd compile --plan SddGraph`. Edits here are overwritten. -->

## Overview

Rendered view of 7 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### task-3-1

- Contract: Node states derive on read -- RED outranks BLOCKED, workable is not frontier, staleness propagates by seq, digest, and intent -- and nothing derivable is ever stored.
- Justifies: `DD-3`, `DD-4`, `D-0022`
- Depends on: `task-2-1`, `task-2-2`, `task-2-3`, `task-2-4`, `task-2-5`, `task-2-6`
- Gate: tests — `TestStateTable` in internal/graph/states/states_test.go; `TestNothingDerivableIsStored` in internal/graph/states/states_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 3.1 — verified 2026-09-01; revision e5e9ed859d14e91b99e2709c419215ecfd547136
- Observation: none yet
- Closure: open — state BLOCKED

### task-3-2

- Contract: `sdd next --claim` claims the heaviest claimable frontier node under compare-and-swap with a leased identity; double-claims are structurally impossible.
- Justifies: `DD-10`, `D-0022`
- Depends on: `task-2-1`, `task-2-2`, `task-2-3`, `task-2-4`, `task-2-5`, `task-2-6`, `task-3-1`
- Gate: tests — `TestClaimPicksTheHeaviestFrontierNode` in internal/graph/claims/claims_test.go; `TestClaimScreensClaimsArtifactsAndCapacity` in internal/graph/claims/claims_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 3.2 — verified 2026-09-01; revision f6fe384534017e87733746548e46513bef5d5580
- Observation: none yet
- Closure: open — state BLOCKED

### task-3-3

- Contract: Workspace providers supply isolation and provenance per VCS: git worktrees N-way on per-claim branches, p4 one shared client, plain trees digest-only -- every guarantee holds at capacity 1.
- Justifies: `DD-6`, `DD-7`, `DD-8`
- Depends on: `task-2-1`, `task-2-2`, `task-2-3`, `task-2-4`, `task-2-5`, `task-2-6`, `task-3-2`
- Gate: tests — `TestGitProviderWorktreeRoundTrip` in internal/graph/provider/provider_test.go; `TestGitProviderForcedCapacityOne` in internal/graph/provider/provider_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 3.3 — verified 2026-09-01; revision 7629a68409e61cb2cb1e589d909c94773872b8cc
- Observation: none yet
- Closure: open — state BLOCKED

### task-3-4

- Contract: `sdd graph sync` records observations only from parsed reports or real exit codes -- honest buckets for unresolved and ambiguous tests, red_seq stamped on first failure, no assert path.
- Justifies: `DD-5`, `DD-6`, `D-0022`
- Depends on: `task-2-1`, `task-2-2`, `task-2-3`, `task-2-4`, `task-2-5`, `task-2-6`, `task-3-3`
- Gate: tests — `TestSyncRecordsObservationWithAnchors` in internal/graph/sync/sync_test.go; `TestSyncRefusesWithoutGuessing` in internal/graph/sync/sync_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 3.4 — verified 2026-09-01; revision 0cec5cb851a480edb199ad06fb94b5e4f41623ae
- Observation: none yet
- Closure: open — state BLOCKED

### task-3-5

- Contract: A clean pass by the claim holder merges atomically; red-before-green refuses a never-failed hazard-discharging test; split, set-tests, and gc mutate single nodes under the store lock.
- Justifies: `DD-5`, `DD-7`, `DD-10`, `D-0022`
- Depends on: `task-2-1`, `task-2-2`, `task-2-3`, `task-2-4`, `task-2-5`, `task-2-6`, `task-3-4`
- Gate: tests — `TestRedBeforeGreenGatesHazardTests` in internal/graph/sync/sync_test.go; `TestSplitRetiresRewiresAndInherits` in internal/graph/ops/ops_test.go; `TestGCReapsOrphansAndStalePayloadsOnly` in internal/graph/ops/ops_test.go
- Hazards: none (explicit claim)
- Estimate: 2
- History: complete as v1 task 3.5 — verified 2026-09-01; revision 027be4ae31c32f32ad57b007a2b2b28d0beefa53
- Observation: none yet
- Closure: open — state BLOCKED

### task-3-6

- Contract: Multi-process walks are safe: racing claimants never double-claim, readers never see torn graphs, and gc never reaps an allocation in flight -- proven cross-process on both flock semantics.
- Justifies: `DD-10`, `DD-3`
- Depends on: `task-2-1`, `task-2-2`, `task-2-3`, `task-2-4`, `task-2-5`, `task-2-6`, `task-3-5`
- Gate: tests — `TestGraphConcurrentClaimStress` in cmd/sdd/graph_stress_test.go; `TestGraphLeaseTakeoverAcrossProcesses` in cmd/sdd/graph_stress_test.go
- Hazards: none (explicit claim)
- Estimate: 2
- History: complete as v1 task 3.6 — verified 2026-09-01; revision e5b0f9267cd8a73fc173012068f41a2e06067b63
- Observation: none yet
- Closure: open — state BLOCKED

### gate-v2-3-execution-loop

- Contract: Every node under v2-3-execution-loop survives a full four-lane review of its aggregate diff.
- Justifies: `DD-9`
- Depends on: `task-3-1`, `task-3-2`, `task-3-3`, `task-3-4`, `task-3-5`, `task-3-6`
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
