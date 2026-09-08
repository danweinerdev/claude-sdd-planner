---
title: "03-consumers"
type: phase
plan: "ForkAwareDecisionLedgers"
phase: 3
status: planned
created: 2026-09-08
updated: 2026-09-08
deliverable: "Graph view: 6 node(s) under phase label 03-consumers"
tasks: []
---

# Phase 3: 03-consumers

<!-- GENERATED VIEW — source of truth: ForkAwareDecisionLedgers-Graph.json. Regenerate with `sdd compile --plan ForkAwareDecisionLedgers`. Edits here are overwritten. -->

## Overview

Rendered view of 6 node(s) from the plan graph (schema v1, seq 0).
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
- Observation: none yet
- Closure: open — state BLOCKED

### decision-write-cli

- Contract: Real fork CLI preview/apply/inspect/recover and existing local add paths preserve exact approval, local-only mutation and truthful transaction outcomes through a public create-save-load lifecycle.
- Justifies: `FR-08`, `FR-09`, `FR-13`, `FR-16`, `DD-9`, `DD-10`, `DD-12`, `AC-02`, `AC-04`, `AC-06`, `AC-09`, `AC-13`, `AC-14`
- Depends on: `explicit-recovery`, `decision-read-cli`
- Gate: tests — `TestForkWriteCLIRealEntryPoint` in cmd/sdd/decide_fork_write_test.go (satisfies user-entrypoint); `TestForkWriteCLIPublicRoundTrip` in cmd/sdd/decide_fork_write_test.go (satisfies persists-state); `TestForkWriteCLIApprovalAndRecovery` in cmd/sdd/decide_fork_write_test.go
- Hazards: user-entrypoint, persists-state
- Artifacts: cmd/sdd/decide.go, cmd/sdd/decide_fork_write.go, cmd/sdd/decide_fork_write_test.go, cmd/sdd/root.go, cmd/sdd/root_test.go, internal/hook/guard.go
- Estimate: 4
- Observation: none yet
- Closure: open — state BLOCKED

### authority-core-review

- Contract: The integrated authority, approval and transaction core has a frozen Aligned full four-lane review covering its aggregate implementation diff.
- Justifies: `D-0022`, `AC-06`, `AC-13`
- Depends on: `decision-write-cli`
- Gate: review — full (carries completion-grade closure)
- Hazards: none (explicit claim)
- Estimate: 1
- Observation: none yet
- Closure: open — state BLOCKED

### validation-consumers

- Contract: Focused and root validation consume the same independent effective/diagnostic sets as the CLI, preserve legacy verdicts and propagate structural/stale versus could-not-run failures with the required exits.
- Justifies: `FR-12`, `FR-13`, `FR-14`, `FR-15`, `FR-17`, `NFR-05`, `AC-01`, `AC-03`, `AC-10`, `AC-11`, `AC-12`, `AC-14`, `AC-15`
- Depends on: `decision-read-cli`
- Gate: tests — `TestForkValidationRealEntryPoint` in cmd/sdd/decide_fork_validate_test.go (satisfies user-entrypoint); `TestForkValidationIndependentSets` in cmd/sdd/decide_fork_validate_test.go (satisfies derives-state); `TestForkValidationLegacyCorpus` in cmd/sdd/decide_fork_validate_test.go
- Hazards: user-entrypoint, derives-state
- Artifacts: cmd/sdd/decide_validate.go, cmd/sdd/decide_fork_validate_test.go, cmd/sdd/root.go, internal/dlg/validate_all.go, internal/dlg/history.go, internal/dlg/gitutil.go, internal/rules/decisionlogs.go, internal/rules/decisions.go, internal/rules/root.go, internal/rules/fork_decisions_test.go, internal/compile/compile.go, internal/compile/fork_citations_test.go, tools/regression/fixtures/
- Estimate: 5
- Observation: none yet
- Closure: open — state BLOCKED

### graph-intent-consumer

- Contract: Graph source/intent and decision-exemption reads resolve qualified and legacy-context decisions consistently, retaining historical identity and refusing unresolved authority without modifying graph completion semantics.
- Justifies: `FR-05`, `FR-12`, `FR-14`, `DD-8`, `AC-03`, `AC-10`, `AC-11`, `AC-14`
- Depends on: `validation-consumers`
- Gate: tests — `TestForkGraphIntentRealEntryPoint` in cmd/sdd/graph_fork_intent_test.go (satisfies user-entrypoint); `TestForkGraphIntentIndependentSets` in internal/graph/compile/fork_intent_test.go (satisfies derives-state)
- Hazards: user-entrypoint, derives-state
- Artifacts: internal/graph/compile/anchor.go, internal/graph/compile/compile.go, internal/graph/compile/fork_intent.go, internal/graph/compile/fork_intent_test.go, cmd/sdd/graph_fork_intent_test.go
- Estimate: 3
- Observation: none yet
- Closure: open — state BLOCKED

### hook-context-consumer

- Contract: SessionStart uses the shared effective view, preserves nonfatal legacy no-ops and retains unresolved/truncation warnings without injecting hidden originals as competing instructions.
- Justifies: `FR-13`, `FR-14`, `NFR-04`, `DD-11`, `AC-01`, `AC-11`, `AC-14`
- Depends on: `validation-consumers`
- Gate: tests — `TestForkSessionStartRealEntryPoint` in cmd/sdd/hook_fork_test.go (satisfies user-entrypoint); `TestForkSessionStartHostileJSON` in cmd/sdd/hook_fork_test.go (satisfies external-format); `TestForkSessionStartWarningsAndBudget` in internal/hook/sessionstart_fork_test.go
- Hazards: user-entrypoint, external-format
- Artifacts: internal/hook/sessionstart.go, internal/hook/sessionstart_fork_test.go, cmd/sdd/hook_fork_test.go
- Estimate: 3
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
