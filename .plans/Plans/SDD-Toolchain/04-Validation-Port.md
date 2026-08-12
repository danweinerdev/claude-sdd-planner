---
title: "Validation Port"
type: phase
plan: "SDD-Toolchain"
phase: 4
status: complete
created: 2026-08-03
updated: 2026-08-11
deliverable: "sdd validate and sdd decide validate at proven parity with the Python oracle"
tasks:
  - id: "4.1"
    title: "Port full-artifact validation to parity"
    status: complete
    verification: "AC-03, AC-04, AC-07 pass: byte-equivalent stdout/stderr and identical exit codes against the oracle for every unchanged fixture, with every SDD code and message branch covered"
    justifies: "FR-06, FR-08, FR-09, FR-10, AC-03, AC-04, AC-07. Delivers the deterministic layer that six skill files and the completion gates depend on, without Python."
  - id: "4.2"
    title: "Port focused-ledger validation to parity"
    status: complete
    verification: "AC-03 and AC-10 pass for the ledger mode, including the per-mode duplicate-key difference and all four diagnostic path alias forms with no machine-specific absolute path"
    justifies: "FR-07, FR-08, FR-09, FR-10, AC-03, AC-10. The ledger validator has no direct test coverage today, so parity here is entirely dependent on the Phase 3 corpora."
    depends_on: ["4.1"]
  - id: "4.3"
    title: "The two intentional corrections"
    status: complete
    verification: "AC-11 and AC-12 pass: the four Resources paths validate without SDD041 from every planning-root form, and the suffixed task identifier grammar accepts 3.1/3.1a/3.2b while rejecting the four named invalid forms with updated correction text"
    justifies: "FR-11, FR-12, FR-13, FR-18, AC-11, AC-12. Fixes four reproducible false positives that make existing artifacts appear invalid, and unblocks splitting a task into independently verifiable parts."
    depends_on: ["4.2"]
  - id: "4.4"
    title: "Cross-platform and read-only guarantees"
    status: complete
    verification: "AC-29, AC-30, AC-36 pass: read-only guard tests show no byte, index, worktree, or config change; Windows and Linux authoring produce byte-identical artifacts; all five targets compile"
    justifies: "NFR-02, NFR-05, NFR-06, AC-29, AC-30, AC-36. Prevents the cross-platform diff churn that would destroy the semantic-diff benefit, and proves the read-only claim that the reviewer agents depend on."
    depends_on: ["4.3"]
waivers:
  - code: SDD173
    reason: "Seven phases closed on one full-range review and test pass spanning all implementation commits, because the phases were implemented out of plan order and no contiguous range isolates any single phase. SDD173's per-phase endpoint and lifecycle-only-changes branches both assume phases close one at a time. See F-01 in this phase's review."
    accepted: "2026-08-12"
---

# Phase 4: Validation Port

## Overview
Port the rules themselves, gated behind Phase 3. Includes the two intentional
corrections and the task-identifier grammar change, each with its own regression
fixture.

## 4.1: Port full-artifact validation to parity

Implements `FR-06`, `FR-08`, `FR-09`, `FR-10`, `AC-03`, `AC-04`, `AC-07`.

### Subtasks
- [x] Port discovery, parsing, schema, heading, identifier, hierarchy, citation, graph, traceability, evidence, review-gate, and durability rules
- [x] Preserve diagnostic identity, ordering, and output shape exactly
- [x] Drive to green against the frozen corpus

### Notes
Revision boundary: `sdd validate` is a faithful replacement for sdd_validate.py on every unchanged fixture.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `befc1ac4328f056f02a29c3c9040439f23fb8274`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `befc1ac4328f056f02a29c3c9040439f23fb8274`
- Focused review: `git show befc1ac4328f056f02a29c3c9040439f23fb8274`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `befc1ac4328f056f02a29c3c9040439f23fb8274`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make parity` | `.` | PASS (`exit 0`) | `126 of 126 SDD codes registered; 491 matched, zero extra` |

## 4.2: Port focused-ledger validation to parity

Implements `FR-07`, `FR-08`, `FR-09`, `FR-10`, `AC-03`, `AC-10`.

### Subtasks
- [x] Port ledger discovery, schema, sequencing, supersession, collision, immutability, and concurrent-edit rules
- [x] Implement the @repo and @ledger path aliases
- [x] Preserve duplicate-key rejection in ledger mode only

### Notes
Revision boundary: `sdd decide validate` is a faithful replacement for sdd_decision_validate.py.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `4294bc2398eb0cd8bc0a9cc491e0a90cd9233266`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `4294bc2398eb0cd8bc0a9cc491e0a90cd9233266`
- Focused review: `git show 4294bc2398eb0cd8bc0a9cc491e0a90cd9233266`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `4294bc2398eb0cd8bc0a9cc491e0a90cd9233266`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd decide validate` | `.` | PASS (`exit 0`) | `69 of 69 DLG codes ported; ledger validates clean` |

## 4.3: The two intentional corrections

Implements `FR-11`, `FR-12`, `FR-13`, `FR-18`, `AC-11`, `AC-12`.

### Subtasks
- [x] Implement planning-root-relative related resolution with the scrubbed regression fixture
- [x] Implement the `<phase>.<digits>[a-z]?` grammar as opaque exact identifiers
- [x] Update /plan, the phase template, the schema, and plan-reviewer to document the grammar

### Notes
Revision boundary: Both reported bugs are fixed with regression fixtures, and the grammar is documented everywhere it is authored.

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
| `go test ./internal/rules/` | `.` | PASS (`exit 0`) | `ok internal/rules; SDD064 admits an optional lowercase suffix such as 1.1a, with a regression fixture` |

## 4.4: Cross-platform and read-only guarantees

Implements `NFR-02`, `NFR-05`, `NFR-06`, `AC-29`, `AC-30`, `AC-36`.

### Subtasks
- [x] Enforce LF output and forward-slash frontmatter paths; accept CRLF and BOM input
- [x] Handle NFC/NFD in path, heading, and identifier matching
- [x] Add read-only guard tests and the cross-compilation portability check

### Notes
Revision boundary: Validation is byte-stable across platforms and provably mutates nothing.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `befc1ac4328f056f02a29c3c9040439f23fb8274`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `befc1ac4328f056f02a29c3c9040439f23fb8274`
- Focused review: `git show befc1ac4328f056f02a29c3c9040439f23fb8274`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `befc1ac4328f056f02a29c3c9040439f23fb8274`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make build-all VARIANTS=release` | `.` | PASS (`exit 0`) | `five targets build; ELF, Mach-O, and PE32+ verified stripped via go tool nm` |

## Acceptance Criteria
- [x] `sdd validate` and `sdd decide validate` are byte-equivalent to the oracle on every unchanged fixture.
- [x] Every SDD and DLG code and message branch has passing coverage.
- [x] The two reported bugs are fixed with the concrete regression fixtures named in the spec.
- [x] The task-identifier grammar is consistent across /plan, the template, the schema, and plan-reviewer.
- [x] Artifacts authored on Windows and Linux are byte-identical, and validation mutates nothing.

## Phase Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Final aligned review: Retro/04-Validation-Port-review.md; frozen: bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `go suite, parity gate, and template drift check all pass` |

### Completed task identities

- `4.1`: `befc1ac4328f056f02a29c3c9040439f23fb8274`
- `4.2`: `4294bc2398eb0cd8bc0a9cc491e0a90cd9233266`
- `4.3`: `3739e840fd505e14888f8b34780f5d47e77437f1`
- `4.4`: `befc1ac4328f056f02a29c3c9040439f23fb8274`
