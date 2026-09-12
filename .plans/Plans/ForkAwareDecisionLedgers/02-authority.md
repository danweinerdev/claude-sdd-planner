---
title: "02-authority"
type: phase
plan: "ForkAwareDecisionLedgers"
phase: 2
status: in-progress
created: 2026-09-08
updated: 2026-09-11
deliverable: "Graph view: 14 node(s) under phase label 02-authority"
tasks: []
---

# Phase 2: 02-authority

<!-- GENERATED VIEW — source of truth: ForkAwareDecisionLedgers-Graph.json. Regenerate with `sdd compile --plan ForkAwareDecisionLedgers`. Edits here are overwritten. -->

## Overview

Rendered view of 14 node(s) from the plan graph (schema v1, seq 283).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### effective-authority

- Contract: Parent-first composition produces independently specified binding/history/unresolved sets, never auto-precedes or transfers overrides, and preserves ancestor failures even when file order opposes inheritance order.
- Justifies: `FR-06`, `FR-07`, `FR-08`, `FR-11`, `FR-12`, `NFR-01`, `NFR-04`, `DD-6`, `DD-7`, `AC-04`, `AC-05`, `AC-08`, `AC-10`
- Depends on: `source-continuity`
- Gate: tests — `TestForkEffectiveIndependentSets` in internal/decisionview/resolve_test.go (satisfies derives-state); `TestForkEffectiveReverseNaturalOrder` in internal/decisionview/resolve_test.go (satisfies order-sensitive); `TestForkEffectiveStaleAncestorAndDuplicates` in internal/decisionview/resolve_test.go
- Hazards: derives-state, order-sensitive
- Artifacts: internal/decisionview/resolve.go, internal/decisionview/resolve_test.go, internal/decisionview/collisions.go, internal/decisionview/model.go, internal/decisionview/canonical.go, internal/decisionview/collections.go, internal/decisionview/continuity.go
- Estimate: 5
- Observation: **pass** at seq 250 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### citations-and-scopes

- Contract: Qualified and legacy citations retain source identity while effective applicability and related scopes stay inside the declared logical owner; ambiguous or missing context cannot capture a local ID or hide an affected constraint.
- Justifies: `FR-04`, `FR-05`, `FR-08`, `FR-15`, `DD-7`, `DD-8`, `AC-03`, `AC-12`
- Depends on: `effective-authority`
- Gate: tests — `TestForkCitationScopeIndependentSets` in internal/decisionview/citations_test.go (satisfies derives-state); `TestForkCitationHostileRoundTrip` in internal/decisionview/citations_test.go (satisfies external-format); `TestForkLegacyContextAndExternalOwners` in internal/decisionview/citations_test.go
- Hazards: derives-state, external-format
- Artifacts: internal/decisionview/citations.go, internal/decisionview/citations_test.go, internal/decisionview/scopes.go
- Estimate: 4
- Observation: **pass** at seq 251 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### transaction-journal

- Contract: Private planning-root journals and shared collection barriers round-trip durable operation state and serialize competing owners without allowing readers to create support files or ignore pending authority.
- Justifies: `FR-16`, `NFR-02`, `NFR-03`, `DD-10`, `AC-13`
- Depends on: `atomic-local-store`
- Gate: tests — `TestForkJournalPublicRoundTrip` in internal/decisionview/journal_test.go (satisfies persists-state); `TestForkJournalSharedBarrierNoOpGuard` in internal/decisionview/journal_test.go (satisfies concurrent-access); `TestForkJournalReadOnlyAndPrivate` in internal/decisionview/journal_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/journal.go, internal/decisionview/journal_test.go
- Estimate: 4
- Observation: **pass** at seq 262 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### preview-envelope

- Contract: Exact-byte preview envelopes retain original/proposed local and config bytes, source preconditions and independent authority sets; their versioned digest rejects any alteration and never asserts human approval.
- Justifies: `FR-08`, `FR-16`, `DD-9`, `AC-06`, `AC-09`, `AC-13`
- Depends on: `citations-and-scopes`, `canonical-bases`
- Gate: tests — `TestForkPreviewIndependentAuthorityDelta` in internal/decisionview/preview_test.go (satisfies derives-state); `TestForkPreviewExactHostileEnvelope` in internal/decisionview/preview_test.go (satisfies external-format); `TestForkPreviewNoWriteAndNoImplicitAuthority` in internal/decisionview/preview_test.go
- Hazards: derives-state, external-format
- Artifacts: internal/decisionview/preview.go, internal/decisionview/preview_test.go
- Estimate: 2
- Observation: **pass** at seq 253 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### preview-override-reconciliation

- Contract: Whole-decision override and reconciliation previews allocate only local IDs, restate complete scope/confirmation and target basis, preserve accepted history and reject implicit or duplicate override authority.
- Justifies: `FR-07`, `FR-08`, `FR-09`, `FR-16`, `DD-6`, `DD-9`, `AC-04`, `AC-05`, `AC-06`, `AC-09`
- Depends on: `preview-envelope`
- Gate: tests — `TestForkOverrideIndependentAuthority` in internal/decisionview/lifecycle_test.go (satisfies derives-state); `TestForkOverrideHostileEnvelope` in internal/decisionview/lifecycle_test.go (satisfies external-format)
- Hazards: derives-state, external-format
- Artifacts: internal/decisionview/lifecycle.go, internal/decisionview/lifecycle_test.go
- Estimate: 3
- Observation: **pass** at seq 254 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### preview-restoration-detachment

- Contract: Restoration and detachment previews append explicit authority events, retain historical records, expose the current parent/removed constraints, and never erase committed history or infer reactivation from deleted metadata.
- Justifies: `FR-08`, `FR-09`, `FR-16`, `DD-6`, `DD-12`, `AC-06`, `AC-09`, `AC-13`
- Depends on: `preview-envelope`
- Gate: tests — `TestForkRestoreIndependentAuthority` in internal/decisionview/restoration_test.go (satisfies derives-state); `TestForkRestoreHostileEnvelope` in internal/decisionview/restoration_test.go (satisfies external-format)
- Hazards: derives-state, external-format
- Artifacts: internal/decisionview/restoration.go, internal/decisionview/restoration_test.go
- Estimate: 3
- Observation: **pass** at seq 255 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### renderer-phase-refresh

- Contract: Regenerating graph-owned views refreshes the matching README phase statuses from the same derived state, preserves legacy/mixed-plan entries and unrelated bytes, and remains idempotent and frozen-view safe.
- Justifies: `SddGraph:pd-b9031144`, `NFR-05`, `AC-15`
- Depends on: `preview-envelope`
- Gate: tests — `TestGraphReadmeStatusesTrackDerivedPhaseState` in internal/graph/compile/render_status_test.go (satisfies derives-state); `TestGraphReadmePreservesMixedPhaseOwnership` in internal/graph/compile/render_status_test.go; `TestGraphReadmeStatusPreflight` in internal/graph/compile/render_status_test.go
- Hazards: derives-state
- Artifacts: internal/graph/compile/render.go, internal/graph/compile/render_status_test.go
- Estimate: 2
- History: User-approved dogfooding repair: compile regenerated in-progress phase documents but left README phases planned, producing SDD058. This is a prerequisite to continuing reliable graph authoring, not a manual view correction.
- Observation: **pass** at seq 256 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### test-gate-fresh-execution

- Contract: The authoritative Make test gate runs every Go package without result-cache reuse, and host build/check targets execute the platform-suffixed binary just built. Preserve template, corpus and portable gates; never skip assertions or change frozen expectations.
- Justifies: `NFR-05`, `AC-15`
- Depends on: `renderer-phase-refresh`
- Gate: tests — `TestMakeGateRunsFreshTests` in tools/testgate/makefile_test.go; `TestMakeHostExecutableMatchesPlatform` in tools/testgate/makefile_test.go
- Hazards: none (explicit claim)
- Artifacts: Makefile, tools/testgate/makefile_test.go
- Estimate: 4
- History: User approved focused timeout investigation as a prerequisite. Cache-enabled rules execution prints PASS then go test remains stuck; the full uncached suite passed all 29 test-bearing packages. Force fresh execution in the authoritative gate and use the host executable suffix; do not claim an upstream Go cache fix.
- Observation: **pass** at seq 258 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### preview-adoption-completion

- Contract: Adoption and rebinding previews preserve inherited bytes and unrelated config values, explicitly bind owner/source identities and baselines, and enumerate all local/config changes without writing.
- Justifies: `FR-01`, `FR-02`, `FR-08`, `FR-16`, `FR-18`, `ForkAwareDecisionLedgers:pd-764da52e`, `DD-1`, `DD-5`, `DD-9`, `AC-02`, `AC-09`, `AC-16`
- Depends on: `test-gate-fresh-execution`
- Gate: tests — `TestForkAdoptionIndependentAuthority` in internal/decisionview/adoption_test.go (satisfies derives-state); `TestForkAdoptionHostileEnvelope` in internal/decisionview/adoption_test.go (satisfies external-format)
- Hazards: derives-state, external-format
- Artifacts: internal/decisionview/adoption.go, internal/decisionview/adoption_test.go
- Estimate: 3
- History: Resume the preserved adoption candidate after the user-approved gate prerequisite. The expired original workspace remains untouched; this child retains the same contract and named tests and must obtain its own red/green observations.
- Observation: **pass** at seq 259 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### preview-adoption-gate-check

- Contract: The integrated adoption/rebinding candidate passes the complete repaired repository gate, rather than relying only on its previously passing focused tests.
- Justifies: `NFR-05`, `AC-15`
- Depends on: `preview-adoption-completion`
- Gate: command — `make test`
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: **pass** at seq 260 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### cross-store-selection-publication

- Contract: Approved adopt, rebind and detach envelopes use exact semantic revalidation, collection UUID exclusion and journal/barrier publication across source/planning stores, exposing only old/new complete or recovery-required authority without overwriting external divergence or claiming cross-volume atomic rename.
- Justifies: `FR-08`, `FR-16`, `NFR-02`, `DD-10`, `AC-02`, `AC-06`, `AC-09`, `AC-13`
- Depends on: `config-selection`, `preview-adoption-completion`, `preview-adoption-gate-check`, `preview-envelope`, `preview-override-reconciliation`, `preview-restoration-detachment`, `renderer-phase-refresh`, `transaction-journal`
- Gate: tests — `TestForkTransactionPublicRoundTrip` in internal/decisionview/transaction_test.go (satisfies persists-state); `TestForkTransactionSourceRaceNoOpGuard` in internal/decisionview/transaction_test.go (satisfies concurrent-access); `TestForkTransactionCrossStoreFailpoints` in internal/decisionview/transaction_test.go; `TestForkSelectionPublicationOperations` in internal/decisionview/transaction_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/transaction.go, internal/decisionview/transaction_test.go
- Estimate: 4
- History: Split after implementation exposed two distinct DD-10 protocols. The original adoption-only candidate is preserved for reference, not treated as completion of the full publication contract. Recovery execution remains the existing downstream node.
- Observation: **pass** at seq 263 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### single-file-authority-publication

- Contract: Approved override, reconcile and restore envelopes use the single-file DD-10 path: semantic regeneration, UUID lock and pending-barrier check, exact-byte CAS publication without a permanent multi-file journal, operation-ID replay disambiguation and no inherited writes.
- Justifies: `FR-08`, `FR-16`, `NFR-02`, `DD-10`, `AC-06`, `AC-09`, `AC-13`
- Depends on: `cross-store-selection-publication`
- Gate: tests — `TestForkSinglePublicationRoundTrip` in internal/decisionview/authority_write_test.go (satisfies persists-state); `TestForkSinglePublicationBarrierNoOpGuard` in internal/decisionview/authority_write_test.go (satisfies concurrent-access); `TestForkSinglePublicationDivergenceAndReplay` in internal/decisionview/authority_write_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/authority_write.go, internal/decisionview/authority_write_test.go
- Estimate: 3
- Observation: **pass** at seq 264 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### selector-recovery-capture

- Contract: Before the first config replacement, persist versioned exact original, intermediate and final selector bytes in the private journal, validating their binding to the approved envelope. Retain the stable writer lock and flushed same-directory temp plus atomic replace protocol; never delete the config first. Missing or corrupt capture cannot authorize guessed recovery bytes.
- Justifies: `FR-16`, `NFR-02`, `DD-10`, `AC-06`, `AC-13`
- Depends on: `cross-store-selection-publication`, `single-file-authority-publication`
- Gate: tests — `TestForkSelectorCapturePublicRoundTrip` in internal/decisionview/selector_capture_test.go (satisfies persists-state); `TestForkSelectorCaptureCrashBoundaries` in internal/decisionview/selector_capture_test.go; `TestForkSelectorCaptureRejectsUnboundBytes` in internal/decisionview/selector_capture_test.go
- Hazards: persists-state
- Artifacts: internal/decisionview/journal.go, internal/decisionview/transaction.go, internal/decisionview/selector_capture.go, internal/decisionview/selector_capture_test.go
- Estimate: 3
- History: User approved journal/publication scope expansion and clarified there must be no delete-first config update. Atomic replacement is retained. The capture repairs the crash window after a pending config replacement, especially rebind where the original envelope contains only a selector digest.
- Observation: **pass** at seq 265 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### explicit-recovery-completion

- Contract: Inspection is side-effect-free and approved recovery handles every interrupted publication/reply state without activating staged intent, erasing committed history or overwriting divergent files.
- Justifies: `FR-09`, `FR-16`, `NFR-02`, `DD-10`, `DD-12`, `AC-06`, `AC-13`
- Depends on: `selector-recovery-capture`
- Gate: tests — `TestForkRecoveryPublicRoundTrip` in internal/decisionview/recovery_test.go (satisfies persists-state); `TestForkRecoveryDivergenceNoOpGuard` in internal/decisionview/recovery_test.go (satisfies concurrent-access); `TestForkRecoveryCrashAndLostReply` in internal/decisionview/recovery_test.go
- Hazards: persists-state, concurrent-access
- Artifacts: internal/decisionview/recovery.go, internal/decisionview/recovery_test.go
- Estimate: 4
- Observation: **pass** at seq 266 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
