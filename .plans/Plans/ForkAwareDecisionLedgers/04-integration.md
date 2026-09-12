---
title: "04-integration"
type: phase
plan: "ForkAwareDecisionLedgers"
phase: 4
status: in-progress
created: 2026-09-08
updated: 2026-09-11
deliverable: "Graph view: 9 node(s) under phase label 04-integration"
tasks: []
---

# Phase 4: 04-integration

<!-- GENERATED VIEW — source of truth: ForkAwareDecisionLedgers-Graph.json. Regenerate with `sdd compile --plan ForkAwareDecisionLedgers`. Edits here are overwritten. -->

## Overview

Rendered view of 9 node(s) from the plan graph (schema v1, seq 283).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### workflows-and-portable

- Contract: Every required decision workflow including decide check, onboarding and setup documents the real fork commands/capability and storage contracts, preserves intent isolation and generates matching portable guidance.
- Justifies: `FR-14`, `FR-17`, `NFR-03`, `NFR-05`, `DD-11`, `ForkAwareDecisionLedgers:pd-764da52e`, `AC-11`, `AC-14`, `AC-15`
- Depends on: `decision-write-cli-completion`, `graph-intent-consumer`, `hook-context-consumer`, `simple-config-replacement`
- Gate: tests — `TestForkWorkflowCommandsAndReferences` in cmd/sdd/fork_workflows_test.go (satisfies ships-prose); `TestForkWorkflowIntentIsolation` in internal/portable/fork_workflows_test.go
- Hazards: ships-prose
- Artifacts: commands/, skills/, agents/, shared/, README.md, AGENTS.md, CLAUDE.md, .claude-plugin/plugin.json, internal/version/version.go, internal/provision/provision.go, cmd/sdd/fork_workflows_test.go, internal/portable/fork_workflows_test.go, .codex-plugin/, .opencode-plugin/
- Estimate: 4
- Observation: **pass** at seq 280 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### end-to-end-contract

- Contract: Independent same-seed end-to-end CLI scenarios agree across every consumer, preserve source/unrelated bytes through upstream updates and recovery, and distinguish structural/candidate/operational outcomes without private fixture data.
- Justifies: `FR-01`, `FR-02`, `FR-03`, `FR-04`, `FR-05`, `FR-06`, `FR-07`, `FR-08`, `FR-09`, `FR-10`, `FR-11`, `FR-12`, `FR-13`, `FR-14`, `FR-15`, `FR-16`, `FR-17`, `FR-18`, `NFR-01`, `NFR-02`, `NFR-03`, `NFR-04`, `NFR-05`, `AC-01`, `AC-02`, `AC-03`, `AC-04`, `AC-05`, `AC-06`, `AC-07`, `AC-08`, `AC-09`, `AC-10`, `AC-11`, `AC-12`, `AC-13`, `AC-14`, `AC-15`, `AC-16`
- Depends on: `authority-core-closure-review`, `core-cli-review-fixes`, `core-context-review-fixes`, `core-storage-review-fixes`, `hostile-authority-decoding`, `legacy-adoption-recovery`, `legacy-root-resolution-compatibility`, `root-qualified-consumer-diagnostics`, `workflows-and-portable`
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
- Observation: **pass** at seq 281 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### test-gate-checkout-portability

- Contract: The fresh test gate's tripwires handle LF and CRLF checkouts identically, make unavailable or incompatible dynamic checks visible, and document dry-run limits without removing full-suite checks.
- Justifies: `NFR-03`, `NFR-05`, `AC-15`
- Depends on: `test-gate-fresh-execution`
- Gate: tests — `TestMakeGateHandlesCheckoutLineEndings` in tools/testgate/makefile_test.go; `TestMakeGateRunsFreshTests` in tools/testgate/makefile_test.go; `TestMakeHostExecutableMatchesPlatform` in tools/testgate/makefile_test.go
- Hazards: none (explicit claim)
- Artifacts: Makefile, tools/testgate/makefile_test.go
- Estimate: 1
- History: Review test-gate-repair.md F-01/F-02/F-03; spec and blind-spots lanes required a CRLF Windows fix despite other lanes classifying the same defect as minor. Preserve all tests and frozen expectations.
- Observation: **pass** at seq 261 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### test-gate-review-completion

- Contract: The focused test-gate repair and checkout-portability follow-up have a frozen Aligned full four-lane review, including confirmation that all tests, corpus and portable checks remain enabled.
- Justifies: `NFR-05`, `AC-15`
- Depends on: `test-gate-checkout-portability`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state READY

### windows-graph-store-replacement

- Contract: Windows graph/artifact publication tolerates transient rename sharing failures within a bounded deadline while preserving the exclusive lock, staged bytes and conditional-write digest checks; persistent or unrelated errors remain explicit, the prior destination is never removed first, and concurrent graph claims/readers/GC retain their unchanged stress invariants. This targeted platform repair was explicitly authorized after recurrent full-suite failures.
- Justifies: `NFR-03`, `NFR-05`, `AC-13`, `AC-15`
- Depends on: (nothing)
- Gate: tests — `TestAtomicReplaceTransientSharing` in internal/store/replace_windows_test.go (satisfies concurrent-access); `TestAtomicReplaceFailurePreservesState` in internal/store/replace_windows_test.go (satisfies persists-state); `TestGraphConcurrentClaimStress` in cmd/sdd/graph_stress_test.go
- Hazards: concurrent-access, persists-state
- Artifacts: internal/store/store.go, internal/store/replace_windows.go, internal/store/replace_other.go, internal/store/replace_windows_test.go, internal/store/lock_test.go
- Estimate: 8
- Observation: **pass** at seq 279 — isolation clean, provenance git 28f6e921fbc0
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### windows-graph-store-review

- Contract: The targeted Windows graph-store replacement repair has a frozen Aligned full four-lane review confirming bounded retry safety, unchanged graph stress assertions and preserved atomic/CAS guarantees.
- Justifies: `NFR-03`, `NFR-05`, `AC-13`, `AC-15`
- Depends on: `windows-graph-store-replacement`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 3
- Observation: **pass** at seq 283 — isolation clean, provenance git 28f6e921fbc0
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### graph-runtime-artifact-boundary

- Contract: Planning artifact discovery and bulk migration exclude reserved Plans/<Plan>/.graph runtime workspaces before loading or editing their contents, while ordinary artifacts, reviews, unrelated dot directories and similarly named files remain discoverable. Preserved evidence/worktrees remain byte-identical.
- Justifies: `NFR-02`, `NFR-05`, `AC-13`, `AC-15`
- Depends on: `full-repository-gate`
- Gate: tests — `TestGraphRuntimeArtifactBoundary` in cmd/sdd/graph_runtime_artifacts_test.go (satisfies user-entrypoint); `TestGraphRuntimeMigrationPreservesEvidence` in cmd/sdd/graph_runtime_artifacts_test.go (satisfies persists-state)
- Hazards: user-entrypoint, persists-state
- Artifacts: internal/rules/root.go, internal/store/discovery.go, cmd/sdd/migrate.go, cmd/sdd/graph_runtime_artifacts_test.go
- Estimate: 3
- Observation: **pass** at seq 282 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### full-feature-closure-review

- Contract: The complete integrated feature and artifact-discovery remediation have a frozen Aligned full four-lane review whose aggregate digest matches all covered nodes; no incomplete end-to-end or runtime evidence is asserted complete.
- Justifies: `AC-13`, `AC-14`, `AC-15`
- Depends on: `graph-runtime-artifact-boundary`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 2
- Observation: none yet
- Closure: open — state READY

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
