---
title: "Schema And Compiler"
type: phase
plan: "SDD-Toolchain"
phase: 6
status: planned
created: 2026-08-03
updated: 2026-08-03
deliverable: "Schema-as-data for the spec type, generated templates, and a production apply with the round-trip contract and isolation"
tasks:
  - id: "6.1"
    title: "Production schema loader with project overrides"
    status: planned
    verification: "AC-14 passes: an override adding an optional section loads, while overrides removing a required heading, relaxing a grammar, weakening a gate, claiming a tool-owned field, or altering path recognition each fail at load naming the rejected rule"
    justifies: "FR-14, FR-16, AC-14. Finding F-13 established that override-able path recognition would let a project disable the FR-28 write guard by configuration."
  - id: "6.2"
    title: "Generate templates from schema"
    status: planned
    verification: "AC-13 passes: every committed file in shared/templates/ is byte-identical to its schema-generated form, and a deliberate divergence in either direction fails `make test`"
    justifies: "FR-15, AC-13. Collapses the templates/schema/validator/CLAUDE.md sync burden the repository maintenance rules currently carry by hand."
    depends_on: ["6.1"]
  - id: "6.3"
    title: "apply with the round-trip contract"
    status: planned
    verification: "AC-15, AC-16, AC-17, AC-18, AC-19, AC-40, AC-47 pass, including byte-idempotence, whole-payload refusal, and every FR-45 identifier case"
    justifies: "FR-17, FR-18, FR-19, FR-20, FR-22, FR-23, FR-24, FR-45. Revision, not creation, is the dominant operation, and review finding F-01 showed the original spec could not express it at all."
    depends_on: ["6.2"]
  - id: "6.4"
    title: "Isolation for every mutating subcommand"
    status: planned
    verification: "AC-44 passes: two interleaved concurrent mutations produce a refusal with the re-read-and-retry diagnostic rather than a lost update, proven for both apply and evidence add against one phase document"
    justifies: "FR-48, AC-44. /implement launches concurrent code-implementer agents, which are not read-only, so lost updates are the expected case; atomicity alone permits silent evidence loss."
    depends_on: ["6.3"]
  - id: "6.5"
    title: "Lifecycle verbs and gates"
    status: planned
    verification: "AC-20, AC-21, AC-22, AC-42 pass: each gate refuses with the unmet gate named, gate verdicts match sdd validate, ledger collisions refuse and leave bytes unchanged, next returns runnable invocations, and complete/frozen artifacts are refused by apply"
    justifies: "FR-21, FR-25, FR-26, FR-46, D-0008, AC-20, AC-21, AC-22, AC-42. Makes evidence gating and the ledger collision rule mechanical rather than behavioral."
    depends_on: ["6.4"]
  - id: "6.6"
    title: "Error messages as the primary interface"
    status: planned
    verification: "AC-26 passes: golden tests prove nearest-match flag suggestions, available-identifier lists on unresolved references, and expected-heading-plus-payload-line on schema mismatch"
    justifies: "FR-29, AC-26. A refusal an automated caller cannot act on converts a one-turn correction into a hallucinated workaround; these messages are what make construction-time refusal cheaper than post-hoc detection."
    depends_on: ["6.5"]
---

# Phase 6: Schema And Compiler

## Overview
The production compiler, informed by whatever Phase 1 learned. Scoped to the
`spec` type only, per FR-36's one-type-at-a-time rollout.

## 6.1: Production schema loader with project overrides

### Subtasks
- [ ] Promote the spike schema to production with the full field set
- [ ] Implement override layering with the prohibition set
- [ ] Fix path recognition as plugin-owned

### Notes
Revision boundary: Schemas load from embedded defaults plus safe project overrides.

### Completion Evidence

Pending — not complete.

## 6.2: Generate templates from schema

### Subtasks
- [ ] Implement the generator
- [ ] Prove the first generation is byte-identical to the committed templates
- [ ] Add the drift check to `make test`

### Notes
Revision boundary: Templates are generated, and drift is mechanically impossible.

### Trap
Improving a template while implementing the generator. The first pass must reproduce today's bytes exactly — the frozen oracle fixtures were built from those templates, and a "better" template silently invalidates the safety argument for having deleted Python.

### Completion Evidence

Pending — not complete.

## 6.3: apply with the round-trip contract

### Subtasks
- [ ] Implement the compile pipeline through atomic write
- [ ] Implement FR-45 assertion semantics and `--retire`
- [ ] Implement section set through the same parser and normalizer
- [ ] Implement the payload lint

### Notes
Revision boundary: A spec can be created and revised entirely through the compiler.

### Completion Evidence

Pending — not complete.

## 6.4: Isolation for every mutating subcommand

### Subtasks
- [ ] Capture a read-time digest and verify at write time
- [ ] Emit a distinct re-read-and-retry diagnostic
- [ ] Add the interleaving test

### Notes
Revision boundary: Concurrent mutation fails loudly instead of losing data.

### Completion Evidence

Pending — not complete.

## 6.5: Lifecycle verbs and gates

### Subtasks
- [ ] Implement the transition verbs reusing the validator rule implementations
- [ ] Implement evidence add, decide add with the collision check, next, show, list
- [ ] Implement the FR-46 completed/frozen refusal and record the byte-sensitivity determination

### Notes
Revision boundary: Status transitions and ledger appends enforce their gates in code.

### Completion Evidence

Pending — not complete.

## 6.6: Error messages as the primary interface

### Subtasks
- [ ] Implement did-you-mean for unknown flags
- [ ] List available identifiers on unresolved references
- [ ] Name expected heading and payload line on mismatch

### Notes
Revision boundary: Every refusal names the constraint and the correct form.

### Completion Evidence

Pending — not complete.

## Acceptance Criteria
- [ ] A spec can be created and revised end to end through `sdd apply`, byte-idempotently.
- [ ] Templates are schema-generated and byte-identical to the committed set on first generation.
- [ ] Concurrent mutation refuses rather than losing data.
- [ ] Completed and frozen artifacts are refused, with the byte-sensitivity determination recorded.
- [ ] Every refusal message is covered by a golden test.

## Phase Completion Evidence

Pending — not complete.
