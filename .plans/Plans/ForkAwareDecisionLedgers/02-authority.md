---
title: "02-authority"
type: phase
plan: "ForkAwareDecisionLedgers"
phase: 2
status: planned
created: 2026-09-08
updated: 2026-09-08
deliverable: "Graph view: 6 node(s) under phase label 02-authority"
tasks: []
---

# Phase 2: 02-authority

<!-- GENERATED VIEW — source of truth: ForkAwareDecisionLedgers-Graph.json. Regenerate with `sdd compile --plan ForkAwareDecisionLedgers`. Edits here are overwritten. -->

## Overview

Rendered view of 6 node(s) from the plan graph (schema v1, seq 0).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### effective-authority

- Contract: Parent-first composition produces independently specified binding/history/unresolved sets, never auto-precedes or transfers overrides, and preserves ancestor failures even when file order opposes inheritance order.
- Justifies: `FR-06`, `FR-07`, `FR-08`, `FR-11`, `FR-12`, `NFR-01`, `NFR-04`, `DD-6`, `DD-7`, `AC-04`, `AC-05`, `AC-08`, `AC-10`
- Depends on: `source-continuity`
- Gate: tests — `TestForkEffectiveIndependentSets` in internal/decisionview/resolve_test.go (satisfies derives-state); `TestForkEffectiveReverseNaturalOrder` in internal/decisionview/resolve_test.go (satisfies order-sensitive); `TestForkEffectiveStaleAncestorAndDuplicates` in internal/decisionview/resolve_test.go
- Hazards: derives-state, order-sensitive
- Artifacts: internal/decisionview/resolve.go, internal/decisionview/resolve_test.go, internal/decisionview/collisions.go
- Estimate: 5
- Observation: none yet
- Closure: open — state BLOCKED

### citations-and-scopes

- Contract: Qualified and legacy citations retain source identity while effective applicability and related scopes stay inside the declared logical owner; ambiguous or missing context cannot capture a local ID or hide an affected constraint.
- Justifies: `FR-04`, `FR-05`, `FR-08`, `FR-15`, `DD-7`, `DD-8`, `AC-03`, `AC-12`
- Depends on: `effective-authority`
- Gate: tests — `TestForkCitationScopeIndependentSets` in internal/decisionview/citations_test.go (satisfies derives-state); `TestForkCitationHostileRoundTrip` in internal/decisionview/citations_test.go (satisfies external-format); `TestForkLegacyContextAndExternalOwners` in internal/decisionview/citations_test.go
- Hazards: derives-state, external-format
- Artifacts: internal/decisionview/citations.go, internal/decisionview/citations_test.go, internal/decisionview/scopes.go
- Estimate: 4
- Observation: none yet
- Closure: open — state BLOCKED

### approved-change-preview

- Contract: Preview computes complete exact-byte local/config changes and independent authority deltas for adoption, whole overrides, reconciliation, restoration, rebinding and detachment without writing or claiming human approval.
- Justifies: `FR-07`, `FR-08`, `FR-09`, `FR-16`, `DD-6`, `DD-9`, `DD-12`, `AC-04`, `AC-06`, `AC-09`
- Depends on: `citations-and-scopes`, `canonical-bases`
- Gate: tests — `TestForkPreviewIndependentAuthorityDelta` in internal/decisionview/preview_test.go (satisfies derives-state); `TestForkPreviewExactHostileEnvelope` in internal/decisionview/preview_test.go (satisfies external-format); `TestForkPreviewNoWriteAndNoImplicitAuthority` in internal/decisionview/preview_test.go
- Hazards: derives-state, external-format
- Artifacts: internal/decisionview/preview.go, internal/decisionview/preview_test.go, internal/decisionview/lifecycle.go
- Estimate: 5
- Observation: none yet
- Closure: open — state BLOCKED

### transaction-journal

- Contract: Private planning-root journals and shared collection barriers round-trip durable operation state and serialize competing owners without allowing readers to create support files or ignore pending authority.
- Justifies: `FR-16`, `NFR-02`, `NFR-03`, `DD-10`, `AC-13`
- Depends on: `atomic-local-store`
- Gate: tests — `TestForkJournalPublicRoundTrip` in internal/decisionview/journal_test.go (satisfies persists-state); `TestForkJournalSharedBarrierNoOpGuard` in internal/decisionview/journal_test.go (satisfies concurrent-access); `TestForkJournalReadOnlyAndPrivate` in internal/decisionview/journal_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/journal.go, internal/decisionview/journal_test.go
- Estimate: 4
- Observation: none yet
- Closure: open — state BLOCKED

### cross-store-publication

- Contract: Applying an approved multi-file envelope across source/planning stores exposes only old/new complete or recovery-required authority, refuses stale inputs and never overwrites external divergence or claims cross-volume rename atomicity.
- Justifies: `FR-08`, `FR-16`, `NFR-02`, `DD-10`, `AC-02`, `AC-06`, `AC-09`, `AC-13`
- Depends on: `transaction-journal`, `approved-change-preview`, `config-selection`
- Gate: tests — `TestForkTransactionPublicRoundTrip` in internal/decisionview/transaction_test.go (satisfies persists-state); `TestForkTransactionSourceRaceNoOpGuard` in internal/decisionview/transaction_test.go (satisfies concurrent-access); `TestForkTransactionCrossStoreFailpoints` in internal/decisionview/transaction_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/transaction.go, internal/decisionview/transaction_test.go
- Estimate: 5
- Observation: none yet
- Closure: open — state BLOCKED

### explicit-recovery

- Contract: Inspection is side-effect-free and approved recovery handles every interrupted publication/reply state without activating staged intent, erasing committed history or overwriting divergent files.
- Justifies: `FR-09`, `FR-16`, `NFR-02`, `DD-10`, `DD-12`, `AC-06`, `AC-13`
- Depends on: `cross-store-publication`
- Gate: tests — `TestForkRecoveryPublicRoundTrip` in internal/decisionview/recovery_test.go (satisfies persists-state); `TestForkRecoveryDivergenceNoOpGuard` in internal/decisionview/recovery_test.go (satisfies concurrent-access); `TestForkRecoveryCrashAndLostReply` in internal/decisionview/recovery_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/recovery.go, internal/decisionview/recovery_test.go
- Estimate: 4
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
