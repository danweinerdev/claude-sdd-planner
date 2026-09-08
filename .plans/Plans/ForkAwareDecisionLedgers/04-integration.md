---
title: "04-integration"
type: phase
plan: "ForkAwareDecisionLedgers"
phase: 4
status: planned
created: 2026-09-08
updated: 2026-09-08
deliverable: "Graph view: 4 node(s) under phase label 04-integration"
tasks: []
---

# Phase 4: 04-integration

<!-- GENERATED VIEW — source of truth: ForkAwareDecisionLedgers-Graph.json. Regenerate with `sdd compile --plan ForkAwareDecisionLedgers`. Edits here are overwritten. -->

## Overview

Rendered view of 4 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### workflows-and-portable

- Contract: Every required decision workflow including decide check, onboarding and setup documents the real fork commands/capability and storage contracts, preserves intent isolation and generates matching portable guidance.
- Justifies: `FR-14`, `FR-17`, `NFR-03`, `NFR-05`, `DD-11`, `D-0025`, `AC-11`, `AC-14`, `AC-15`
- Depends on: `decision-write-cli`, `graph-intent-consumer`, `hook-context-consumer`
- Gate: tests — `TestForkWorkflowCommandsAndReferences` in cmd/sdd/fork_workflows_test.go (satisfies ships-prose); `TestForkWorkflowIntentIsolation` in internal/portable/fork_workflows_test.go
- Hazards: ships-prose
- Artifacts: commands/, skills/, agents/, shared/, README.md, AGENTS.md, CLAUDE.md, .claude-plugin/plugin.json, internal/version/version.go, internal/provision/provision.go, cmd/sdd/fork_workflows_test.go, internal/portable/fork_workflows_test.go, .codex-plugin/, .opencode-plugin/
- Estimate: 4
- Observation: none yet
- Closure: open — state BLOCKED

### end-to-end-contract

- Contract: Independent same-seed end-to-end CLI scenarios agree across every consumer, preserve source/unrelated bytes through upstream updates and recovery, and distinguish structural/candidate/operational outcomes without private fixture data.
- Justifies: `FR-01`, `FR-02`, `FR-03`, `FR-04`, `FR-05`, `FR-06`, `FR-07`, `FR-08`, `FR-09`, `FR-10`, `FR-11`, `FR-12`, `FR-13`, `FR-14`, `FR-15`, `FR-16`, `FR-17`, `FR-18`, `NFR-01`, `NFR-02`, `NFR-03`, `NFR-04`, `NFR-05`, `AC-01`, `AC-02`, `AC-03`, `AC-04`, `AC-05`, `AC-06`, `AC-07`, `AC-08`, `AC-09`, `AC-10`, `AC-11`, `AC-12`, `AC-13`, `AC-14`, `AC-15`, `AC-16`
- Depends on: `workflows-and-portable`, `authority-core-review`
- Gate: tests — `TestForkEndToEndRealEntryPoint` in cmd/sdd/fork_e2e_test.go (satisfies user-entrypoint); `TestForkEndToEndSeedReplay` in cmd/sdd/fork_e2e_test.go (satisfies deterministic-replay); `TestForkEndToEndHostileFormats` in cmd/sdd/fork_e2e_test.go (satisfies external-format)
- Hazards: user-entrypoint, deterministic-replay, external-format
- Artifacts: cmd/sdd/fork_e2e_test.go, tools/forkfixtures/
- Estimate: 4
- Observation: none yet
- Closure: open — state BLOCKED

### full-repository-gate

- Contract: The complete repository test, template, frozen-corpus and portable gates pass, with Go vet and supported cross-compilation checks passing and native path/recovery evidence supplied by the implementation tests.
- Justifies: `NFR-05`, `AC-15`
- Depends on: `end-to-end-contract`
- Gate: command — `make test && go vet ./... && make build-all && make plugins-check`
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### full-feature-review

- Contract: The complete integrated feature has a frozen Aligned full four-lane review whose aggregate diff digest still matches all covered implementation nodes.
- Justifies: `D-0022`, `NFR-05`, `AC-15`
- Depends on: `full-repository-gate`
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
