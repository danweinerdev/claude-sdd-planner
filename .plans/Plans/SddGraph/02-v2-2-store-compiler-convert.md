---
title: "v2-2-store-compiler-convert"
type: phase
plan: "SddGraph"
phase: 2
status: planned
created: 2026-09-01
updated: 2026-09-01
deliverable: "Graph view: 7 node(s) under phase label v2-2-store-compiler-convert"
tasks: []
---

# Phase 2: v2-2-store-compiler-convert

<!-- GENERATED VIEW — source of truth: SddGraph-Graph.json. Regenerate with `sdd compile --plan SddGraph`. Edits here are overwritten. -->

## Overview

Rendered view of 7 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### task-2-1

- Contract: The committed graph store initializes, loads, saves, and updates under compare-and-swap with advisory locking; concurrent updates lose no increment and readers never observe a torn graph.
- Justifies: `DD-3`
- Depends on: `task-1-1`, `task-1-2`, `task-1-3`
- Gate: tests — `TestUpdateLosesNoIncrement` in internal/graph/store/store_test.go; `TestReadersNeverSeeATornGraph` in internal/graph/store/store_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 2.1 — verified 2026-08-31; revision 085f4f123f1988300c643a322cc800c858e8450c
- Observation: none yet
- Closure: open — state BLOCKED

### task-2-2

- Contract: Payload fragments stage atomically in UUIDv7 order and assemble into one proposal; any refusal stages nothing and names both sides of a collision.
- Justifies: `DD-11`
- Depends on: `task-1-1`, `task-1-2`, `task-1-3`, `task-2-1`
- Gate: tests — `TestStageParksAValidFragment` in internal/graph/proposal/staging_test.go; `TestAssembleMergesDisjointFragmentsInStagingOrder` in internal/graph/proposal/staging_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 2.2 — verified 2026-08-31; revision b011057eb8ee58439ea6fc764cacdeab45cc71c4
- Observation: none yet
- Closure: open — state BLOCKED

### task-2-3

- Contract: `sdd compile` validates a staged proposal wholesale -- every semantic finding batched in one deterministic report -- embeds intent fingerprints, and appends to the committed graph only when clean.
- Justifies: `DD-4`, `DD-9`, `DD-11`
- Depends on: `task-1-1`, `task-1-2`, `task-1-3`, `task-2-2`
- Gate: tests — `TestCompileBatchesEveryFinding` in internal/graph/compile/compile_test.go; `TestCompileHappyPathEmbedsFingerprintsAndConsumes` in internal/graph/compile/compile_test.go
- Hazards: none (explicit claim)
- Estimate: 2
- History: complete as v1 task 2.3 — verified 2026-08-31; revision fe2b76ed66bdaaa974de0a3b09a9f603a48c0ec2
- Observation: none yet
- Closure: open — state BLOCKED

### task-2-4

- Contract: Compile renders phase views and the README projection as marker-guarded generated files; an existing non-generated target refuses the whole compile before the graph write.
- Justifies: `DD-1`, `FR-36`, `DD-2`
- Depends on: `task-1-1`, `task-1-2`, `task-1-3`, `task-2-3`
- Gate: tests — `TestRenderRefusalLeavesGraphAndPayloadUntouched` in internal/graph/compile/compile_test.go; `TestGoldenTriple` in internal/graph/compile/compile_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 2.4 — verified 2026-08-31; revision 4f9e981133e7d6dd73d18db7ca492ff00fe603b6
- Observation: none yet
- Closure: open — state BLOCKED

### task-2-5

- Contract: `sdd graph convert` maps a v1 plan mechanically -- tasks to nodes, phase order to deps, completion provenance to history -- and marks every unmade judgment with a blocking sentinel.
- Justifies: `DD-15`
- Depends on: `task-1-1`, `task-1-2`, `task-1-3`, `task-2-3`
- Gate: tests — `TestConvertMapsMechanicsAndMarksJudgments` in internal/graph/convert/convert_test.go; `TestConvertedGraphRefusesToCompile` in internal/graph/convert/convert_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 2.5 — verified 2026-08-31; revision 10db7546b3c14ed42be02f10eb0293c71e63a661
- Observation: none yet
- Closure: open — state BLOCKED

### task-2-6

- Contract: Guard classification covers every `sdd` and `sdd graph` verb from single-source maps, and parity with the real command tree is enforced by tests.
- Justifies: `D-0014`, `DD-2`, `FR-28`
- Depends on: `task-1-1`, `task-1-2`, `task-1-3`, `task-2-1`
- Gate: tests — `TestGuardParityWithPython` in internal/hook/guard_parity_test.go; `TestHandlerFlagTableIsComplete` in cmd/sdd/root_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 2.6 — verified 2026-08-31; revision b2281b8a9e601475a0cb55f9c55b1e4258bba10a
- Observation: none yet
- Closure: open — state BLOCKED

### gate-v2-2-store-compiler-convert

- Contract: Every node under v2-2-store-compiler-convert survives a full four-lane review of its aggregate diff.
- Justifies: `DD-9`
- Depends on: `task-2-1`, `task-2-2`, `task-2-3`, `task-2-4`, `task-2-5`, `task-2-6`
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
