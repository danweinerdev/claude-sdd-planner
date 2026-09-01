---
title: "01-example"
type: phase
plan: "SamplePlan"
phase: 1
status: planned
created: DATE
updated: DATE
deliverable: "Graph view: 4 node(s) under phase label 01-example"
tasks: []
---

# Phase 1: 01-example

<!-- GENERATED VIEW — source of truth: SamplePlan-Graph.json. Regenerate with `sdd compile --plan SamplePlan`. Edits here are overwritten. -->

## Overview

Rendered view of 4 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### define-schema

- Contract: the schema describes every supported key and rejects an unknown one
- Justifies: `FR-01`
- Depends on: (nothing)
- Gate: tests — `test_schema_covers_every_key` in tests/test_schema.ext
- Hazards: none (explicit claim)
- Artifacts: src/schema.ext
- Estimate: 1
- Observation: none yet

### parse-config

- Contract: loading a config accepts every documented key and refuses an unknown key by name
- Justifies: `AC-01`, `DD-1`
- Depends on: `define-schema`
- Gate: tests — `test_loads_valid_config` in tests/test_config.ext; `test_reparses_hostile_values` in tests/test_config.ext (satisfies external-format)
- Hazards: external-format
- Artifacts: src/config.ext
- Estimate: 2
- Observation: none yet

### build-gate

- Contract: the tree builds clean; REPLACE the untriaged sentinel below with a triaged hazard list or an explicit empty list before compiling
- Justifies: `FR-01`
- Depends on: (nothing)
- Gate: command — `make build`
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet

### feature-review

- Contract: the example feature survives a full validation cycle
- Justifies: `AC-01`
- Depends on: `parse-config`, `build-gate`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
