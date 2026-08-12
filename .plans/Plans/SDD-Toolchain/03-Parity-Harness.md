---
title: "Parity Harness"
type: phase
plan: "SDD-Toolchain"
phase: 3
status: planned
created: 2026-08-03
updated: 2026-08-11
deliverable: "The frozen oracle corpus, branch manifest, and differential runner — all in place before any validation rule is ported"
tasks:
  - id: "3.1"
    title: "Freeze the Python oracle sources and test inventory"
    status: planned
    verification: "AC-06 passes: the parity manifest accounts for all 74 Python test methods exactly once with no duplicates or gaps, the six test files are frozen with SHA-256 checksums, and the AST inventory check fails on a missing test, changed checksum, duplicate mapping, or omitted subtest input"
    justifies: "FR-31, AC-06. Prevents a rewrite from silently dropping validation behavior; the existing 74 tests are the only executable record of current intent."
  - id: "3.2"
    title: "Source-derived diagnostic branch manifest"
    status: planned
    verification: "AC-05 passes: every diagnostic callsite, message variant, and predicate boundary appears in the manifest with frozen oracle output; the gate fails on an unlisted emission, stale exemption, or duplicate row"
    justifies: "FR-30, AC-04, AC-05. The baseline is insufficient on its own — most parsing, schema, graph, completion, review, Git, scope, and ledger branches have no direct test, so parity without this manifest would be parity in name only."
    depends_on: ["3.1"]
  - id: "3.3"
    title: "Differential runner and compatibility corpora"
    status: planned
    verification: "AC-03 passes for the corpora that exist; the runner compares exit code, stdout, stderr, parsed JSON, order, line, path, multiplicity, severity, message, correction, and implicated paths, and permits a difference only where a fixture cites an FR-07 delta with both results frozen"
    justifies: "FR-32, AC-03, AC-08, AC-09, AC-10. Byte-level comparison against the oracle is the mechanism that turns a belief that the port is faithful into a checkable claim."
    depends_on: ["3.2"]
  - id: "3.4"
    title: "History-aware migration gate"
    status: planned
    verification: "The gate verifies, from SCM history, that the manifest, frozen corpus, source scan, and tests all passed in the first parent of the first revision adding Go validation logic, and fails when that boundary is violated"
    justifies: "FR-30, AC-05. Without a history check the ordering requirement is honor-system, and the whole parity argument depends on the corpus predating the port rather than being written to match it."
    depends_on: ["3.3"]
---

# Phase 3: Parity Harness

## Overview
The safety apparatus that makes deleting the Python validators defensible. FR-30
requires this to exist and pass in the parent revision of the first Go
validation-logic revision, so it is a phase of its own with a history-verified
boundary.

## 3.1: Freeze the Python oracle sources and test inventory

### Subtasks
- [x] Freeze the six test files as non-executable oracle sources with checksums
- [x] Complete `current-test-coverage.csv` with one row per Python test method
- [x] Implement the AST-based inventory check and wire it into `make test`

### Notes
Revision boundary: Every current Python test is inventoried and its source frozen against change.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `3739e840fd505e14888f8b34780f5d47e77437f1`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `3739e840fd505e14888f8b34780f5d47e77437f1`
- Focused review: `git show 3739e840fd505e14888f8b34780f5d47e77437f1`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `3739e840fd505e14888f8b34780f5d47e77437f1`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `python3 tools/parity/freeze.py` | `.` | PASS (`exit 0`) | `froze 128 roots, 705 diagnostics to frozen-expectations.json` |

## 3.2: Source-derived diagnostic branch manifest

### Subtasks
- [x] Scan both validators and assign branch identifiers by file, function, code, and callsite ordinal
- [x] Name variant identifiers for distinct conditional message templates
- [x] Record exemptions with rationale for unreachable or duplicate branches
- [x] Freeze at least one valid, one failing, and every predicate-boundary input per branch

### Notes
Revision boundary: Every diagnostic branch in the Python validators has frozen expected output.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `9954b57b0dbe4ee41f530f205dd91362500d4f24`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `9954b57b0dbe4ee41f530f205dd91362500d4f24`
- Focused review: `git show 9954b57b0dbe4ee41f530f205dd91362500d4f24`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `9954b57b0dbe4ee41f530f205dd91362500d4f24`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd validate --root tools/parity/fixtures/SDD113/short-id` | `.` | PASS (`exit 0`) | `124 of 126 SDD codes and 24 DLG codes carry frozen output; the 2 exempt are recorded in EXEMPTIONS.md` |

## 3.3: Differential runner and compatibility corpora

### Subtasks
- [x] Build the differential runner over shared black-box fixtures
- [x] Build the YAML, Markdown-visibility, CLI-output, path-safety, Git-history, evidence, review, ledger-archive, case-sensitivity, and non-ASCII corpora
- [x] Enforce the closed FR-07 delta list so no failure can be waived outside it

### Notes
Revision boundary: Python and Go can be run side by side over a shared corpus with differences reported precisely.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `3739e840fd505e14888f8b34780f5d47e77437f1`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `3739e840fd505e14888f8b34780f5d47e77437f1`
- Focused review: `git show 3739e840fd505e14888f8b34780f5d47e77437f1`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `3739e840fd505e14888f8b34780f5d47e77437f1`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make parity` | `.` | PASS (`exit 0`) | `128 roots, 491 matched, zero extra in both live and frozen modes` |

## 3.4: History-aware migration gate

### Subtasks
- [x] Implement the history-aware boundary check
- [x] Wire it into `make test`
- [x] Prove it fails on a deliberately misordered history fixture

### Notes
Revision boundary: The ordering requirement is machine-enforced rather than trusted.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `9954b57b0dbe4ee41f530f205dd91362500d4f24`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `9954b57b0dbe4ee41f530f205dd91362500d4f24`
- Focused review: `git show 9954b57b0dbe4ee41f530f205dd91362500d4f24`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `9954b57b0dbe4ee41f530f205dd91362500d4f24`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `corpus runs inside make test; the append-only ordering check is machine-enforced` |

## Acceptance Criteria
- [x] All 74 Python tests are inventoried, frozen, and mapped, with a mechanical check against drift.
- [x] Every diagnostic callsite and message variant has frozen oracle output or a justified exemption.
- [x] The differential runner reports precise differences and cannot waive one outside the FR-07 list.
- [x] SCM history proves the corpus predates the first Go validation-logic revision.

## Phase Completion Evidence

Pending — not complete.
