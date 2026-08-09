---
title: "Write Guard And Rollout"
type: phase
plan: "SDD-Toolchain"
phase: 7
status: planned
created: 2026-08-03
updated: 2026-08-03
deliverable: "The Write/Edit guard enabled per type behind its migration, skills rewritten against the CLI, and remaining types onboarded"
tasks:
  - id: "7.1"
    title: "Normalization migration for the spec type"
    status: planned
    verification: "AC-43 passes: identical sdd validate diagnostic sets before and after, one revision touching no content, byte-idempotence under a second apply, and a deliberately introduced difference stopping the migration"
    justifies: "FR-46, FR-47, D-0008, AC-43. Review finding F-03 showed an incidental first apply would rewrite completed artifacts whose recorded revision identity is load-bearing."
  - id: "7.2"
    title: "Write/Edit guard for the spec type"
    status: planned
    verification: "AC-24 passes: Write denied on a spec artifact path, Read permitted, notes/ and planning-root README permitted, plugin source permitted, fail-open when the planning root cannot be resolved, and the denial message's suggested invocation succeeds when run"
    justifies: "FR-28, FR-36, AC-24. Without the mechanical guard the compiler is the polite path and Edit is the fast path, which makes every invariant advisory."
    depends_on: ["7.1"]
  - id: "7.3"
    title: "Rewrite /specify against the CLI and measure"
    status: planned
    verification: "AC-27 passes for authoring instructions; a recorded comparison shows skill line count and tool calls per artifact before and after, and spec-reviewer verdicts on a compiler-authored spec versus a hand-authored one"
    justifies: "FR-35, AC-27, and the spec goal that skills carry role and judgment instead of transcribed formatting procedure. Also tests whether the shorter skill actually produces equal or better artifacts."
    depends_on: ["7.2"]
  - id: "7.4"
    title: "Onboard the remaining artifact types"
    status: planned
    verification: "AC-28 and AC-46 pass: every artifact of every type dry-runs with normalization deltas only, each type behind its own completed migration and guard enablement"
    justifies: "FR-36, FR-34, AC-28, AC-46. Completes the rollout so no artifact type is left with two writers, which would leave the invariants advisory for that type."
    depends_on: ["7.3"]
---

# Phase 7: Write Guard And Rollout

## Overview
Makes the compiler the only writer, one artifact type at a time, each behind its
own normalization migration.

## 7.1: Normalization migration for the spec type

### Subtasks
- [ ] Implement the migration command with before/after validation
- [ ] Run it for the spec type as its own scoped lifecycle revision
- [ ] Re-anchor any identity found byte-sensitive

### Notes
Revision boundary: Every spec artifact is canonical, with validation proven unchanged across the migration.

### Completion Evidence

Pending — not complete.

## 7.2: Write/Edit guard for the spec type

### Subtasks
- [ ] Implement path-scoped Write/Edit denial for migrated types only
- [ ] Ensure Read is never denied and plugin source is never denied
- [ ] Make each denial message name a runnable invocation

### Notes
Revision boundary: Direct writes to spec artifacts are impossible; everything else is unaffected.

### Completion Evidence

Pending — not complete.

## 7.3: Rewrite /specify against the CLI and measure

### Subtasks
- [ ] Rewrite /specify to author through sdd
- [ ] Remove every Write/Edit authoring instruction for artifact paths
- [ ] Record the comparison

### Notes
Revision boundary: One lifecycle skill is fully CLI-driven, with the effect measured.

### Completion Evidence

Pending — not complete.

## 7.4: Onboard the remaining artifact types

### Subtasks
- [ ] Add schemas for plan, phase, design, research, brainstorm, debrief, review, and decision-log
- [ ] Run each type's migration and enable its guard
- [ ] Rewrite the remaining lifecycle skills against the CLI

### Notes
Revision boundary: Every artifact type is compiler-authored and guarded.

### Completion Evidence

Pending — not complete.

## Acceptance Criteria
- [ ] Each artifact type is normalized by its own migration with validation proven unchanged.
- [ ] Direct Write/Edit on any migrated artifact type is denied, with actionable messages.
- [ ] Every lifecycle skill authors through sdd, with no Write/Edit authoring instruction remaining.
- [ ] The measured effect on skill size, tool calls, and reviewer verdicts is recorded.

## Phase Completion Evidence

Pending — not complete.
