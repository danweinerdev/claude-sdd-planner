---
title: "01-foundations"
type: phase
plan: "ForkAwareDecisionLedgers"
phase: 1
status: in-progress
created: 2026-09-08
updated: 2026-09-11
deliverable: "Graph view: 6 node(s) under phase label 01-foundations"
tasks: []
---

# Phase 1: 01-foundations

<!-- GENERATED VIEW — source of truth: ForkAwareDecisionLedgers-Graph.json. Regenerate with `sdd compile --plan ForkAwareDecisionLedgers`. Edits here are overwritten. -->

## Overview

Rendered view of 6 node(s) from the plan graph (schema v1, seq 158).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### fork-model

- Contract: Versioned fork/config, owner, binding, basis and authority-event models reject duplicate or unsupported keys/types and losslessly reparse hostile supported values without changing legacy models.
- Justifies: `FR-01`, `FR-04`, `FR-10`, `FR-17`, `DD-3`, `DD-4`, `AC-14`
- Depends on: (nothing)
- Gate: tests — `TestForkModelHostileRoundTrip` in internal/decisionview/model_test.go (satisfies external-format); `TestForkModelRejectsUnsupportedVersions` in internal/decisionview/model_test.go
- Hazards: external-format
- Artifacts: internal/decisionview/model.go, internal/decisionview/model_test.go
- Estimate: 3
- Observation: **pass** at seq 127 — isolation clean, provenance git 8dd1b797d09a
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### config-selection

- Contract: The represented repository's existing config explicitly selects a planning-root ledger and matching owner/collection; internal and external roots work and malformed selection never falls back or creates root files.
- Justifies: `FR-01`, `FR-02`, `FR-04`, `FR-15`, `FR-17`, `D-0025`, `DD-1`, `AC-01`, `AC-02`, `AC-12`
- Depends on: `fork-model`
- Gate: tests — `TestForkSelectionHostileConfig` in internal/decisionview/selection_test.go (satisfies external-format); `TestForkSelectionInternalExternalOwners` in internal/decisionview/selection_test.go; `TestForkSelectionLegacyAndMalformed` in internal/decisionview/selection_test.go
- Hazards: external-format
- Artifacts: internal/decisionview/selection.go, internal/decisionview/selection_test.go, internal/store/store.go
- Estimate: 3
- Observation: **pass** at seq 128 — isolation clean, provenance git 8dd1b797d09a
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### canonical-bases

- Contract: entry-v1 encoding and retained basis values are deterministic across independent same-seed runs, include every supported decision field, and distinguish relevant value edits from layout-only changes.
- Justifies: `FR-10`, `FR-18`, `NFR-01`, `DD-4`, `AC-07`, `AC-08`, `AC-16`
- Depends on: (nothing)
- Gate: tests — `TestForkCanonicalHostileRoundTrip` in internal/decisionview/canonical_test.go (satisfies external-format); `TestForkCanonicalSeedReplay` in internal/decisionview/canonical_test.go (satisfies deterministic-replay); `TestForkCanonicalFieldMatrix` in internal/decisionview/canonical_test.go
- Hazards: external-format, deterministic-replay
- Artifacts: internal/decisionview/canonical.go, internal/decisionview/canonical_test.go
- Estimate: 3
- Observation: **pass** at seq 126 — isolation clean, provenance git 8dd1b797d09a
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### collection-loading

- Contract: Canonical ledgers and explicitly owned archives load as independent namespaces through contained repository/planning anchors; hostile locators, escapes and identity/collection defects fail without read side effects.
- Justifies: `FR-02`, `FR-03`, `FR-04`, `NFR-02`, `NFR-03`, `DD-3`, `AC-03`, `AC-10`, `AC-13`
- Depends on: `config-selection`
- Gate: tests — `TestForkCollectionsHostileSources` in internal/decisionview/collections_test.go (satisfies external-format); `TestForkCollectionsContainedReadOnly` in internal/decisionview/collections_test.go; `TestForkCollectionsOwnArchivesAndIDs` in internal/decisionview/collections_test.go
- Hazards: external-format
- Artifacts: internal/decisionview/collections.go, internal/decisionview/collections_test.go, internal/decisionview/paths_windows.go, internal/decisionview/paths_posix.go, internal/decisionview/model.go, internal/dlg/ledger.go, internal/dlg/validate.go, internal/dlg/collection.go, internal/schema/decision-log.json
- Estimate: 5
- Observation: **pass** at seq 129 — isolation clean, provenance git 8dd1b797d09a
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### source-continuity

- Contract: Binding continuity and target-basis freshness match an independent lifecycle matrix, preserving compatible evolution while making rebinding and incompatible history invalidation explicit and reproducible.
- Justifies: `FR-10`, `FR-11`, `FR-18`, `NFR-01`, `NFR-04`, `DD-5`, `AC-07`, `AC-08`, `AC-16`
- Depends on: `canonical-bases`, `collection-loading`
- Gate: tests — `TestForkContinuityIndependentMatrix` in internal/decisionview/continuity_test.go (satisfies derives-state); `TestForkContinuitySeedReplay` in internal/decisionview/continuity_test.go (satisfies deterministic-replay)
- Hazards: derives-state, deterministic-replay
- Artifacts: internal/decisionview/continuity.go, internal/decisionview/continuity_test.go
- Estimate: 4
- Observation: **pass** at seq 130 — isolation clean, provenance git 8dd1b797d09a
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### atomic-local-store

- Contract: The local fork store round-trips complete bytes through its public API, rejects stale writers under a real guard and preserves source/unrelated bytes without retaining new repo-root support files.
- Justifies: `FR-02`, `FR-09`, `FR-16`, `NFR-02`, `NFR-03`, `DD-2`, `DD-10`, `AC-06`, `AC-13`
- Depends on: (nothing)
- Gate: tests — `TestForkStorePublicRoundTrip` in internal/decisionview/store_test.go (satisfies persists-state); `TestForkStoreCASNoOpGuard` in internal/decisionview/store_test.go (satisfies concurrent-access); `TestForkStorePreservesRootFootprint` in internal/decisionview/store_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/store.go, internal/decisionview/store_test.go, internal/decisionview/store_windows.go, internal/decisionview/store_posix.go
- Estimate: 4
- Observation: **pass** at seq 125 — isolation clean, provenance git 8dd1b797d09a
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
