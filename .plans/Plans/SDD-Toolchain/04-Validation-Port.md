---
title: "Validation Port"
type: phase
plan: "SDD-Toolchain"
phase: 4
status: planned
created: 2026-08-03
updated: 2026-08-03
deliverable: "sdd validate and sdd decide validate at proven parity with the Python oracle"
tasks:
  - id: "4.1"
    title: "Port full-artifact validation to parity"
    status: planned
    verification: "AC-03, AC-04, AC-07 pass: byte-equivalent stdout/stderr and identical exit codes against the oracle for every unchanged fixture, with every SDD code and message branch covered"
    justifies: "FR-06, FR-08, FR-09, FR-10, AC-03, AC-04, AC-07. Delivers the deterministic layer that six skill files and the completion gates depend on, without Python."
  - id: "4.2"
    title: "Port focused-ledger validation to parity"
    status: planned
    verification: "AC-03 and AC-10 pass for the ledger mode, including the per-mode duplicate-key difference and all four diagnostic path alias forms with no machine-specific absolute path"
    justifies: "FR-07, FR-08, FR-09, FR-10, AC-03, AC-10. The ledger validator has no direct test coverage today, so parity here is entirely dependent on the Phase 3 corpora."
    depends_on: ["4.1"]
  - id: "4.3"
    title: "The two intentional corrections"
    status: planned
    verification: "AC-11 and AC-12 pass: the four Resources paths validate without SDD041 from every planning-root form, and the suffixed task identifier grammar accepts 3.1/3.1a/3.2b while rejecting the four named invalid forms with updated correction text"
    justifies: "FR-11, FR-12, FR-13, FR-18, AC-11, AC-12. Fixes four reproducible false positives that make existing artifacts appear invalid, and unblocks splitting a task into independently verifiable parts."
    depends_on: ["4.2"]
  - id: "4.4"
    title: "Cross-platform and read-only guarantees"
    status: planned
    verification: "AC-29, AC-30, AC-36 pass: read-only guard tests show no byte, index, worktree, or config change; Windows and Linux authoring produce byte-identical artifacts; all five targets compile"
    justifies: "NFR-02, NFR-05, NFR-06, AC-29, AC-30, AC-36. Prevents the cross-platform diff churn that would destroy the semantic-diff benefit, and proves the read-only claim that the reviewer agents depend on."
    depends_on: ["4.3"]
---

# Phase 4: Validation Port

## Overview
Port the rules themselves, gated behind Phase 3. Includes the two intentional
corrections and the task-identifier grammar change, each with its own regression
fixture.

## 4.1: Port full-artifact validation to parity

### Subtasks
- [ ] Port discovery, parsing, schema, heading, identifier, hierarchy, citation, graph, traceability, evidence, review-gate, and durability rules
- [ ] Preserve diagnostic identity, ordering, and output shape exactly
- [ ] Drive to green against the frozen corpus

### Notes
Revision boundary: `sdd validate` is a faithful replacement for sdd_validate.py on every unchanged fixture.

### Completion Evidence

Pending — not complete.

## 4.2: Port focused-ledger validation to parity

### Subtasks
- [ ] Port ledger discovery, schema, sequencing, supersession, collision, immutability, and concurrent-edit rules
- [ ] Implement the @repo and @ledger path aliases
- [ ] Preserve duplicate-key rejection in ledger mode only

### Notes
Revision boundary: `sdd decide validate` is a faithful replacement for sdd_decision_validate.py.

### Completion Evidence

Pending — not complete.

## 4.3: The two intentional corrections

### Subtasks
- [ ] Implement planning-root-relative related resolution with the scrubbed regression fixture
- [ ] Implement the `<phase>.<digits>[a-z]?` grammar as opaque exact identifiers
- [ ] Update /plan, the phase template, the schema, and plan-reviewer to document the grammar

### Notes
Revision boundary: Both reported bugs are fixed with regression fixtures, and the grammar is documented everywhere it is authored.

### Completion Evidence

Pending — not complete.

## 4.4: Cross-platform and read-only guarantees

### Subtasks
- [ ] Enforce LF output and forward-slash frontmatter paths; accept CRLF and BOM input
- [ ] Handle NFC/NFD in path, heading, and identifier matching
- [ ] Add read-only guard tests and the cross-compilation portability check

### Notes
Revision boundary: Validation is byte-stable across platforms and provably mutates nothing.

### Completion Evidence

Pending — not complete.

## Acceptance Criteria
- [ ] `sdd validate` and `sdd decide validate` are byte-equivalent to the oracle on every unchanged fixture.
- [ ] Every SDD and DLG code and message branch has passing coverage.
- [ ] The two reported bugs are fixed with the concrete regression fixtures named in the spec.
- [ ] The task-identifier grammar is consistent across /plan, the template, the schema, and plan-reviewer.
- [ ] Artifacts authored on Windows and Linux are byte-identical, and validation mutates nothing.

## Phase Completion Evidence

Pending — not complete.
