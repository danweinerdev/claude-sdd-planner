---
title: "Python Removal"
type: phase
plan: "SDD-Toolchain"
phase: 5
status: complete
created: 2026-08-03
updated: 2026-08-11
deliverable: "Python and PyYAML gone from every user-facing path, with the frozen corpus retained as the regression suite"
tasks:
  - id: "5.1"
    title: "Delete the Python validators and their bootstrap"
    status: complete
    verification: "AC-27 passes: `make test` green from a clean checkout with both scripts, PyYAML, and the virtualenv bootstrap removed; repository search finds no Python validator invocation and no PyYAML dependency"
    justifies: "FR-33, FR-35, D-0012, AC-27. Removes a hard runtime dependency that currently has a documented degradation path in the validate skill, and deletes the second implementation of the same rules."
  - id: "5.2"
    title: "Reconcile the decision ledger and maintenance rules"
    status: complete
    verification: "`sdd decide validate` passes; CLAUDE.md, README.md, and both claude-md templates describe the Go toolchain accurately; no document instructs a removed entry point"
    justifies: "FR-35, D-0012, D-0013, D-0014, D-0015. Prevents the documentation drift the repository maintenance rules exist to catch, now that four accepted decisions have changed mechanism."
    depends_on: ["5.1"]
  - id: "5.3"
    title: "Measure and replace the provisional bounds"
    status: complete
    verification: "NFR-07 carries a measured figure on named hardware rather than a provisional target, and the recorded binary size replaces the unsourced estimate"
    justifies: "NFR-07, AC-31, and review finding F-12. Prevents an unsourced target from hardening into a requirement nobody has checked."
    depends_on: ["5.1"]
  - id: "5.4"
    title: "Re-evaluate prebuilt distribution against the measured binary size"
    status: complete
    verification: "A measurement of the real dependency-linked binary across all five NFR-03 targets is recorded, and a written determination either reaffirms D-0015 or opens a superseding decision for user reconciliation"
    justifies: "Review 02 finding F-05 and FU-01: the ~50 MB estimate behind rejecting prebuilt bundling measured 4x high at 2.40 MB per stdlib-only target, so the payload arithmetic in the spec's Open Questions is unsourced and wrong."
    depends_on: ["5.3"]
---

# Phase 5: Python Removal

## Overview
The irreversible step, deliberately isolated so it lands only when parity is
proven. FR-33 makes this a prerequisite for the compiler, because a normalizer
rewriting artifact bytes would invalidate the byte-level differential comparison
the parity gates depend on.

## 5.1: Delete the Python validators and their bootstrap

Implements `FR-33`, `FR-35`, `D-0012`, `AC-27`, `NFR-01`, `NFR-04`.

### Subtasks
- [x] Delete both scripts, `requirements.txt`, and the virtualenv test bootstrap
- [x] Retain frozen oracle outputs and black-box fixtures as the authoritative regression suite
- [x] Update every skill, shared convention, template, agent, Make target, and maintenance document that named them

### Notes
Revision boundary: No user-facing path invokes Python, and the regression suite still proves the rules.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `539432de2ea8745eb48e9cf4885c6174cde8c01d`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `539432de2ea8745eb48e9cf4885c6174cde8c01d`
- Focused review: `git show 539432de2ea8745eb48e9cf4885c6174cde8c01d`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `539432de2ea8745eb48e9cf4885c6174cde8c01d`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make parity` | `.` | PASS (`exit 0`) | `scripts/, tests/, requirements.txt deleted; corpus still 491 matched` |

## 5.2: Reconcile the decision ledger and maintenance rules

Implements `FR-35`, `D-0012`, `D-0013`, `D-0014`, `D-0015`.

### Subtasks
- [x] Update the maintenance rules for the Go toolchain and the schema-generated templates
- [x] Verify every superseded ledger entry links correctly to its successor
- [x] Refresh command and agent tables in README, CLAUDE.md, and both templates

### Notes
Revision boundary: Documentation and ledger match the shipped system.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `d547943485e68923a3812ea981961966d7f8f366`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `d547943485e68923a3812ea981961966d7f8f366`
- Focused review: `git show d547943485e68923a3812ea981961966d7f8f366`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `d547943485e68923a3812ea981961966d7f8f366`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd decide validate` | `.` | PASS (`exit 0`) | `ledger supersession links clean; maintenance rules updated for the Go toolchain` |

## 5.3: Measure and replace the provisional bounds

Implements `NFR-07`, `AC-31`.

### Subtasks
- [x] Benchmark single-artifact apply and validate on named hardware
- [x] Record stripped binary size
- [x] Amend the spec with measured figures

### Notes
Revision boundary: Every quantitative bound in the spec traces to a measurement.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `d547943485e68923a3812ea981961966d7f8f366`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `d547943485e68923a3812ea981961966d7f8f366`
- Focused review: `git show d547943485e68923a3812ea981961966d7f8f366`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `d547943485e68923a3812ea981961966d7f8f366`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd validate --root tools/parity/fixtures/SDD113/short-id` | `.` | PASS (`exit 0`) | `median 20ms on Threadripper 9970X; NFR-07 bound set to 50ms from a provisional 300ms` |

## 5.4: Re-evaluate prebuilt distribution against the measured binary size

### Subtasks
- [x] Build all five NFR-03 targets with the real CommonMark and YAML dependencies linked and record each stripped size
- [x] Compute the actual per-release payload and history growth against the recorded figure
- [x] Correct the unsourced estimate in the spec's Open Questions to the measured value
- [x] Write the determination: either reaffirm D-0015 or open a superseding decision entry, which stops for user reconciliation per D-0003

### Notes
Revision boundary: a recorded measurement and an explicit determination on prebuilt distribution, replacing the estimate the original decision cited.

### Trap
Treating the smaller measurement as automatically reopening D-0015. That decision rests on the judgment that the plugin should not be in the compiling business, which no size figure disturbs; only the payload-arithmetic argument is falsified, and reversing an accepted ledger entry requires user reconciliation, never a unilateral amendment.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `d547943485e68923a3812ea981961966d7f8f366`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `d547943485e68923a3812ea981961966d7f8f366`
- Focused review: `git show d547943485e68923a3812ea981961966d7f8f366`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `d547943485e68923a3812ea981961966d7f8f366`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make build-all VARIANTS=release` | `.` | PASS (`exit 0`) | `21.9 MB across five targets vs the spec's ~50 MB estimate; D-0015 reaffirmed` |

## Acceptance Criteria
- [x] `make test` passes from a clean checkout with no Python and no PyYAML.
- [x] The frozen corpus remains the authoritative regression suite after deletion.
- [x] No document references a removed entry point, and the ledger supersession chain is intact.
- [x] Every quantitative bound in the spec is measured rather than estimated.

## Phase Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Final aligned review: Retro/05-Python-Removal-review.md; frozen: bc3383502115b7fd2160ec20169f2998c402bf7b..0acca6756ce07b83f4df2f987ac56ef55b40178e

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `go suite, parity gate, and template drift check all pass` |

### Completed task identities

- `5.1`: `539432de2ea8745eb48e9cf4885c6174cde8c01d`
- `5.2`: `d547943485e68923a3812ea981961966d7f8f366`
- `5.3`: `d547943485e68923a3812ea981961966d7f8f366`
- `5.4`: `d547943485e68923a3812ea981961966d7f8f366`
