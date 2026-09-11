---
title: "03-consumers"
type: phase
plan: "ForkAwareDecisionLedgers"
phase: 3
status: in-progress
created: 2026-09-08
updated: 2026-09-11
deliverable: "Graph view: 15 node(s) under phase label 03-consumers"
tasks: []
---

# Phase 3: 03-consumers

<!-- GENERATED VIEW — source of truth: ForkAwareDecisionLedgers-Graph.json. Regenerate with `sdd compile --plan ForkAwareDecisionLedgers`. Edits here are overwritten. -->

## Overview

Rendered view of 15 node(s) from the plan graph (schema v1, seq 283).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### decision-read-cli

- Contract: Real CLI effective/history/lookup/list/search reads share resolution, preserve legacy output and return complete diagnostics even under filters; unsupported capability is never advertised as full fork support.
- Justifies: `FR-13`, `FR-17`, `NFR-04`, `DD-9`, `DD-11`, `AC-01`, `AC-11`, `AC-14`
- Depends on: `citations-and-scopes`
- Gate: tests — `TestForkReadCLIRealEntryPoint` in cmd/sdd/decide_fork_read_test.go (satisfies user-entrypoint); `TestForkReadCLIHostileJSON` in cmd/sdd/decide_fork_read_test.go (satisfies external-format); `TestForkReadCLILegacyAndFilteredFailures` in cmd/sdd/decide_fork_read_test.go
- Hazards: user-entrypoint, external-format
- Artifacts: cmd/sdd/decide.go, cmd/sdd/decide_fork_read.go, cmd/sdd/decide_fork_read_test.go, cmd/sdd/root.go, cmd/sdd/root_test.go, internal/hook/guard.go
- Estimate: 3
- Observation: **pass** at seq 252 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### graph-intent-consumer

- Contract: Graph source/intent and decision-exemption reads resolve qualified and legacy-context decisions consistently, retaining historical identity and refusing unresolved authority without modifying graph completion semantics.
- Justifies: `FR-05`, `FR-12`, `FR-14`, `DD-8`, `AC-03`, `AC-10`, `AC-11`, `AC-14`
- Depends on: `shared-fork-validation-context`, `validation-entrypoint-integration`
- Gate: tests — `TestForkGraphIntentRealEntryPoint` in cmd/sdd/graph_fork_intent_test.go (satisfies user-entrypoint); `TestForkGraphIntentIndependentSets` in internal/graph/compile/fork_intent_test.go (satisfies derives-state)
- Hazards: user-entrypoint, derives-state
- Artifacts: internal/graph/compile/anchor.go, internal/graph/compile/compile.go, internal/graph/compile/fork_intent.go, internal/graph/compile/fork_intent_test.go, cmd/sdd/graph_fork_intent_test.go
- Estimate: 3
- Observation: **pass** at seq 273 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### hook-context-consumer

- Contract: SessionStart uses the shared effective view, preserves nonfatal legacy no-ops and retains unresolved/truncation warnings without injecting hidden originals as competing instructions.
- Justifies: `FR-13`, `FR-14`, `NFR-04`, `DD-11`, `AC-01`, `AC-11`, `AC-14`
- Depends on: `shared-fork-validation-context`, `validation-entrypoint-integration`
- Gate: tests — `TestForkSessionStartRealEntryPoint` in cmd/sdd/hook_fork_test.go (satisfies user-entrypoint); `TestForkSessionStartHostileJSON` in cmd/sdd/hook_fork_test.go (satisfies external-format); `TestForkSessionStartWarningsAndBudget` in internal/hook/sessionstart_fork_test.go
- Hazards: user-entrypoint, external-format
- Artifacts: internal/hook/sessionstart.go, internal/hook/sessionstart_fork_test.go, cmd/sdd/hook_fork_test.go
- Estimate: 3
- Observation: **pass** at seq 274 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### shared-fork-validation-context

- Contract: A repository-root-bound shared decisionview capture supplies immutable qualified authority and diagnostics to rules.Root. Programmatic validation preserves citation comment/frontmatter/liveness semantics and collection-aware identity; source/scoping failures and operational severity cannot be hidden by CLI-only filtering.
- Justifies: `FR-12`, `FR-13`, `FR-14`, `FR-15`, `FR-17`, `NFR-05`, `AC-01`, `AC-03`, `AC-10`, `AC-11`, `AC-12`, `AC-14`, `AC-15`
- Depends on: `decision-read-cli`
- Gate: tests — `TestForkRulesAuthoritySets` in internal/rules/fork_decisions_test.go (satisfies derives-state); `TestForkRulesCitationCompatibility` in internal/rules/fork_decisions_test.go; `TestForkRulesOperationalAndOwnerIsolation` in internal/rules/fork_decisions_test.go
- Hazards: derives-state
- Artifacts: internal/decisionview/consumer.go, internal/rules/decisionlogs.go, internal/rules/decisions.go, internal/rules/root.go, internal/rules/rules.go, internal/rules/citations.go, internal/rules/index.go, internal/rules/graphapi.go, internal/rules/fork_decisions_test.go, internal/dlg/validate_all.go, internal/dlg/history.go, internal/dlg/gitutil.go, tools/regression/fixtures/
- Estimate: 4
- History: Split after review showed a CLI-only adapter did not migrate rules/compile/lifecycle consumers or preserve legacy citation semantics. The rejected candidate remains in a preserved workspace; do not inherit its diagnostic filtering workaround.
- Observation: **pass** at seq 257 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### validation-entrypoint-integration

- Contract: Focused/root CLI validation, compiler/apply and lifecycle gates consume the shared qualified authority context consistently, preserving legacy behavior and full diagnostics/exit severity. Explicit ledger arguments cannot bypass selected fork context; unsupported graph-intent use fails closed until its existing migration node lands.
- Justifies: `FR-12`, `FR-13`, `FR-14`, `FR-15`, `FR-17`, `NFR-05`, `AC-01`, `AC-03`, `AC-10`, `AC-11`, `AC-12`, `AC-14`, `AC-15`
- Depends on: `shared-fork-validation-context`
- Gate: tests — `TestForkValidationRealEntryPoint` in cmd/sdd/decide_fork_validate_test.go (satisfies user-entrypoint); `TestForkValidationIndependentSets` in cmd/sdd/decide_fork_validate_test.go (satisfies derives-state); `TestForkValidationLegacyCorpus` in cmd/sdd/decide_fork_validate_test.go
- Hazards: user-entrypoint, derives-state
- Artifacts: cmd/sdd/decide_validate.go, cmd/sdd/decide_fork_validate_test.go, cmd/sdd/decide_fork_read.go, cmd/sdd/validate.go, cmd/sdd/root.go, cmd/sdd/apply.go, cmd/sdd/transition.go, cmd/sdd/section.go, cmd/sdd/migrate.go, internal/compile/compile.go, internal/compile/fork_citations_test.go, tools/regression/corpus.go
- Estimate: 3
- Observation: **pass** at seq 271 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### simple-config-replacement

- Contract: For the rarely changed represented-repository planning-config.json only, stage and flush/close the complete new file, remove the old config, then rename the staged file. The user accepts the temporary missing-path and crash window. Keep errors truthful and preserve staged bytes after a failed post-removal rename; do not change ledger publication, approval checks, or source ownership protections.
- Justifies: `FR-16`, `DD-10`, `AC-13`
- Depends on: `decision-read-cli`, `explicit-recovery-completion`, `selector-recovery-capture`
- Gate: tests — `TestConfigReplacementStagedSequence` in internal/decisionview/config_replace_test.go; `TestConfigReplacementFailurePreservesStagedBytes` in internal/decisionview/config_replace_test.go; `TestConfigReplacementDoesNotChangeLedgerPublisher` in internal/decisionview/config_replace_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/decisionview/config_replace.go, internal/decisionview/config_replace_test.go, internal/decisionview/transaction.go, internal/decisionview/recovery.go
- Estimate: 2
- History: User explicitly accepted create-new/remove-old/rename for planning-config.json because it is expected to change rarely, superseding the earlier continuous-path requirement for that file only. Do not expand recovery machinery or weaken ordinary ledger writes.
- Observation: **pass** at seq 267 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### decision-write-cli-completion

- Contract: Real fork CLI preview/apply/inspect/recover and existing local add paths preserve exact approval, local-only mutation and truthful transaction outcomes through a public create-save-load lifecycle. Config replacement follows the explicit user-approved rare-config risk exception; unsupported mutations refuse rather than using inherited defaults.
- Justifies: `FR-08`, `FR-09`, `FR-13`, `FR-16`, `DD-9`, `DD-10`, `DD-12`, `AC-02`, `AC-04`, `AC-06`, `AC-09`, `AC-13`, `AC-14`
- Depends on: `simple-config-replacement`
- Gate: tests — `TestForkWriteCLIRealEntryPoint` in cmd/sdd/decide_fork_write_test.go (satisfies user-entrypoint); `TestForkWriteCLIPublicRoundTrip` in cmd/sdd/decide_fork_write_test.go (satisfies persists-state); `TestForkWriteCLIApprovalAndRecovery` in cmd/sdd/decide_fork_write_test.go
- Hazards: user-entrypoint, persists-state
- Artifacts: cmd/sdd/decide.go, cmd/sdd/decide_fork_read.go, cmd/sdd/decide_fork_write.go, cmd/sdd/decide_fork_write_test.go, cmd/sdd/root.go, cmd/sdd/root_test.go, internal/hook/guard.go
- Estimate: 4
- Observation: **pass** at seq 268 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### core-context-review-fixes

- Contract: Preserve shared fork authority and global failures through scoped roots, apply owner-aware scope validation and explicit legacy-root citation context, and refuse known missing-selector staging/history without activating staged authority.
- Justifies: `FR-05`, `FR-12`, `FR-15`, `AC-03`, `AC-12`, `AC-13`
- Depends on: `validation-entrypoint-integration`
- Gate: tests — `TestForkScopedAuthorityPreservation` in internal/rules/fork_review_test.go (satisfies derives-state); `TestForkOwnerAwareScopeAndLegacyContext` in internal/rules/fork_review_test.go; `TestForkKnownMissingSelectorRefusal` in internal/decisionview/consumer_review_test.go
- Hazards: derives-state
- Artifacts: internal/rules/scope.go, internal/rules/decisions.go, internal/rules/citations.go, internal/rules/root.go, internal/rules/fork_review_test.go, internal/decisionview/consumer.go, internal/decisionview/selection.go, internal/decisionview/consumer_review_test.go, cmd/sdd/decide_fork_validate_test.go
- Estimate: 3
- Observation: **pass** at seq 272 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### core-storage-review-fixes

- Contract: Harden publication and recovery root normalization, adoption source-barrier checks, readable journal size bounds and committed barrier cleanup while preserving target ownership, no-overwrite intent and the accepted config-only remove/rename policy.
- Justifies: `FR-16`, `NFR-03`, `AC-13`
- Depends on: `decision-write-cli-completion`, `simple-config-replacement`
- Gate: tests — `TestForkPublicationCanonicalRootsAndSourceBarriers` in internal/decisionview/storage_review_test.go (satisfies concurrent-access); `TestForkJournalReadableBoundsAndCommittedCleanup` in internal/decisionview/storage_review_test.go (satisfies persists-state)
- Hazards: concurrent-access, persists-state
- Artifacts: internal/decisionview/transaction.go, internal/decisionview/journal.go, internal/decisionview/recovery.go, internal/decisionview/store_windows.go, internal/decisionview/store_posix.go, internal/decisionview/storage_review_test.go
- Estimate: 3
- Observation: **pass** at seq 270 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### core-cli-review-fixes

- Contract: The CLI accepts both declared source roots and legitimate stale-override restoration, reports canonical capabilities honestly, and uses compatible decision-ID allocation without weakening approval, source or scope checks.
- Justifies: `FR-03`, `FR-08`, `FR-13`, `FR-17`, `AC-10`, `AC-14`
- Depends on: `decision-write-cli-completion`, `simple-config-replacement`
- Gate: tests — `TestForkCLIReviewSourceRestoreCapabilities` in cmd/sdd/decide_fork_review_test.go (satisfies user-entrypoint); `TestForkOverrideExtendedIDs` in internal/decisionview/lifecycle_review_test.go
- Hazards: user-entrypoint
- Artifacts: cmd/sdd/decide_fork_read.go, cmd/sdd/decide_fork_write.go, cmd/sdd/decide_fork_read_test.go, cmd/sdd/decide_fork_write_test.go, cmd/sdd/decide_fork_review_test.go, internal/decisionview/lifecycle.go, internal/decisionview/lifecycle_review_test.go
- Estimate: 2
- Observation: **pass** at seq 269 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### legacy-adoption-recovery

- Contract: Initial adoption from a legacy selector without repositoryId remains explicitly recoverable after interrupted publication; absent original ownership is admitted only for genuine initial adoption, while mismatched or malformed ownership and selector digests remain refused.
- Justifies: `FR-16`, `AC-13`
- Depends on: `core-context-review-fixes`, `core-storage-review-fixes`, `core-cli-review-fixes`
- Gate: tests — `TestForkLegacyAdoptionRecovery` in internal/decisionview/recovery_legacy_test.go (satisfies persists-state); `TestForkRecoveryOriginalOwnerGuard` in internal/decisionview/recovery_legacy_test.go
- Hazards: persists-state
- Artifacts: internal/decisionview/recovery.go, internal/decisionview/recovery_legacy_test.go
- Estimate: 2
- Observation: **pass** at seq 275 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### root-qualified-consumer-diagnostics

- Contract: Fork validation resolves collection and decision source locators against their declared planning or repository roots, never aliases a different same-relative-path artifact, and reports diagnostics against the actual source file.
- Justifies: `FR-05`, `FR-12`, `FR-15`
- Depends on: `core-context-review-fixes`, `core-storage-review-fixes`, `core-cli-review-fixes`
- Gate: tests — `TestForkRepositorySourcePathCollision` in internal/rules/fork_locator_test.go (satisfies derives-state); `TestForkRepositorySourceDiagnosticPath` in internal/rules/fork_locator_test.go
- Hazards: derives-state
- Artifacts: internal/rules/index.go, internal/rules/decisions.go, internal/rules/fork_locator_test.go
- Estimate: 2
- Observation: **pass** at seq 277 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### legacy-root-resolution-compatibility

- Contract: Preserve baseline VCS-root semantics for legacy explicit planning roots and nested planning configs, while explicit fork selectors retain their represented configuration-owner root and fail closed on malformed authority.
- Justifies: `FR-01`, `FR-15`, `AC-12`
- Depends on: `core-context-review-fixes`, `core-storage-review-fixes`, `core-cli-review-fixes`
- Gate: tests — `TestForkLegacyRootResolutionCompatibility` in cmd/sdd/fork_root_compat_test.go (satisfies user-entrypoint); `TestForkExplicitOwnerRootResolution` in cmd/sdd/fork_root_compat_test.go
- Hazards: user-entrypoint
- Artifacts: cmd/sdd/validate.go, cmd/sdd/fork_root_compat_test.go
- Estimate: 2
- Observation: **pass** at seq 276 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### hostile-authority-decoding

- Contract: Malformed selector JSON, including escaped declaration keys, never silently falls back to legacy authority; YAML alias expansion is bounded with explicit errors while ordinary aliases, depth/cycle guards and deterministic valid decoding remain supported.
- Justifies: `FR-01`, `FR-12`, `NFR-01`, `AC-10`, `AC-14`
- Depends on: `legacy-adoption-recovery`, `root-qualified-consumer-diagnostics`, `legacy-root-resolution-compatibility`
- Gate: tests — `TestForkEscapedMalformedDeclaration` in cmd/sdd/fork_decoder_guard_test.go (satisfies external-format, user-entrypoint); `TestForkBoundedYAMLAliasExpansion` in internal/decisionview/decoder_guard_test.go (satisfies external-format)
- Hazards: external-format, user-entrypoint
- Artifacts: internal/decisionview/consumer.go, internal/decisionview/model.go, internal/decisionview/collections.go, internal/decisionview/decoder_guard.go, internal/decisionview/decoder_guard_test.go, cmd/sdd/validate.go, cmd/sdd/fork_decoder_guard_test.go
- Estimate: 3
- Observation: **pass** at seq 278 — isolation clean, provenance git 28f6e921fbc0
- Closure: assumed-closed — GREEN, not yet covered by a passing full review gate (sufficient to build on, not completion-grade)

### authority-core-closure-review

- Contract: All integrated authority core and decoder remediations have a fresh frozen Aligned full four-lane review with concrete finding dispositions.
- Justifies: `AC-10`, `AC-12`, `AC-13`, `AC-14`
- Depends on: `hostile-authority-decoding`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 2
- Observation: none yet
- Closure: open — state READY
- Claim: fork-ledgers-finish (lease expires 2026-09-11T20:55:25Z)

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
