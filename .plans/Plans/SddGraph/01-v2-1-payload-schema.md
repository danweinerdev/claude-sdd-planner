---
title: "v2-1-payload-schema"
type: phase
plan: "SddGraph"
phase: 1
status: planned
created: 2026-09-01
updated: 2026-09-01
deliverable: "Graph view: 4 node(s) under phase label v2-1-payload-schema"
tasks: []
---

# Phase 1: v2-1-payload-schema

<!-- GENERATED VIEW — source of truth: SddGraph-Graph.json. Regenerate with `sdd compile --plan SddGraph`. Edits here are overwritten. -->

## Overview

Rendered view of 4 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### task-1-1

- Contract: The graph and payload model decodes strictly -- unknown keys, malformed values, and tool-owned payload fields are refused with JSON paths and did-you-mean suggestions, batched -- and encodes deterministically.
- Justifies: `DD-12`, `DD-3`
- Depends on: (nothing)
- Gate: tests — `TestDecodedShapes` in internal/graph/model/decode_test.go; `TestUnknownKeyDidYouMean` in internal/graph/model/decode_test.go; `TestRoundTrip` in internal/graph/model/decode_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 1.1 — verified 2026-08-31; revision e37df494a163ea466fe71143727584d107684cc2
- Observation: none yet
- Closure: open — state READY

### task-1-2

- Contract: The hazard vocabulary is closed and canonical; unknown hazards are refused naming the whole vocabulary; each entry states the test shape that discharges it.
- Justifies: `DD-9`, `DD-13`
- Depends on: `task-1-1`
- Gate: tests — `TestVocabularyIsCompleteAndCanonical` in internal/graph/hazards/hazards_test.go; `TestRequireKnownNamesTheVocabulary` in internal/graph/hazards/hazards_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 1.2 — verified 2026-08-31; revision ed539842fd2c45838fa44d4b0de75fae9bc1e807
- Observation: none yet
- Closure: open — state BLOCKED

### task-1-3

- Contract: `sdd template graph-proposal` emits a placeholder-complete exemplar and its JSON Schema from one source, byte-stable, cross-checked against the model's key sets.
- Justifies: `DD-12`
- Depends on: `task-1-1`, `task-1-2`
- Gate: tests — `TestExemplarDecodesCleanly` in internal/graph/proposal/proposal_test.go; `TestSchemaKeysMatchModel` in internal/graph/proposal/proposal_test.go
- Hazards: none (explicit claim)
- Estimate: 1
- History: complete as v1 task 1.3 — verified 2026-08-31; revision 7483157e34d20584796bc2fa1ecc6f7eaf693aa4
- Observation: none yet
- Closure: open — state BLOCKED

### gate-v2-1-payload-schema

- Contract: Every node under v2-1-payload-schema survives a full four-lane review of its aggregate diff.
- Justifies: `DD-9`
- Depends on: `task-1-1`, `task-1-2`, `task-1-3`
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
