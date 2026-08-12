---
title: "Schema And Compiler"
type: phase
plan: "SDD-Toolchain"
phase: 6
status: planned
created: 2026-08-03
updated: 2026-08-11
deliverable: "Schema-as-data for the spec type, generated templates, and a production apply with the round-trip contract and isolation"
tasks:
  - id: "6.1"
    title: "Production schema loader with project overrides"
    status: complete
    verification: "AC-14 passes: an override adding an optional section loads, while overrides removing a required heading, relaxing a grammar, weakening a gate, claiming a tool-owned field, or altering path recognition each fail at load naming the rejected rule"
    justifies: "FR-14, FR-16, AC-14. Finding F-13 established that override-able path recognition would let a project disable the FR-28 write guard by configuration."
  - id: "6.2"
    title: "Generate templates from schema"
    status: complete
    verification: "AC-13 passes: every committed file in shared/templates/ is byte-identical to its schema-generated form, and a deliberate divergence in either direction fails `make test`"
    justifies: "FR-15, AC-13. Collapses the templates/schema/validator/CLAUDE.md sync burden the repository maintenance rules currently carry by hand."
    depends_on: ["6.1"]
  - id: "6.3"
    title: "apply with the round-trip contract"
    status: complete
    verification: "AC-15, AC-16, AC-17, AC-18, AC-19, AC-40, AC-47 pass, including byte-idempotence, whole-payload refusal, and every FR-45 identifier case"
    justifies: "FR-17, FR-18, FR-19, FR-20, FR-22, FR-23, FR-24, FR-45. Revision, not creation, is the dominant operation, and review finding F-01 showed the original spec could not express it at all."
    depends_on: ["6.2"]
  - id: "6.4"
    title: "Isolation for every mutating subcommand"
    status: complete
    verification: "AC-44 passes: two interleaved concurrent mutations produce a refusal with the re-read-and-retry diagnostic rather than a lost update, proven for both apply and evidence add against one phase document"
    justifies: "FR-48, AC-44. /implement launches concurrent code-implementer agents, which are not read-only, so lost updates are the expected case; atomicity alone permits silent evidence loss."
    depends_on: ["6.3"]
  - id: "6.5"
    title: "Lifecycle verbs and gates"
    status: complete
    verification: "AC-20, AC-21, AC-22, AC-42 pass: each gate refuses with the unmet gate named, gate verdicts match sdd validate, ledger collisions refuse and leave bytes unchanged, next returns runnable invocations, and complete/frozen artifacts are refused by apply"
    justifies: "FR-21, FR-25, FR-26, FR-46, D-0008, AC-20, AC-21, AC-22, AC-42. Makes evidence gating and the ledger collision rule mechanical rather than behavioral."
    depends_on: ["6.4"]
  - id: "6.6"
    title: "Error messages as the primary interface"
    status: complete
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
- [x] Promote the spike schema to production with the full field set
- [x] Implement override layering with the prohibition set
- [x] Fix path recognition as plugin-owned

### Notes
Revision boundary: Schemas load from embedded defaults plus safe project overrides.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Focused review: `git show 687104f1393bdcef72ae72b31f7ab8ec0629a290`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/schema/` | `.` | PASS (`exit 0`) | `17 artifact types load and validate from embedded JSON` |

## 6.2: Generate templates from schema

### Subtasks
- [x] Implement the generator
- [x] Prove the first generation is byte-identical to the committed templates
- [x] Add the drift check to `make test`

### Notes
Revision boundary: Templates are generated, and drift is mechanically impossible.

### Trap
Improving a template while implementing the generator. The first pass must reproduce today's bytes exactly — the frozen oracle fixtures were built from those templates, and a "better" template silently invalidates the safety argument for having deleted Python.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Focused review: `git show 687104f1393bdcef72ae72b31f7ab8ec0629a290`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd template --check` | `.` | PASS (`exit 0`) | `8 templates match their schemas; found 2 real schema gaps on first run` |

## 6.3: apply with the round-trip contract

### Subtasks
- [x] Implement the compile pipeline through atomic write
- [x] Implement FR-45 assertion semantics and `--retire`
- [x] Implement section set through the same parser and normalizer
- [x] Implement the payload lint

### Notes
Revision boundary: A spec can be created and revised entirely through the compiler.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- Focused review: `git show 7159b5b7dca76000c2f60d59d4132559e5fbe71f`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/compile/` | `.` | PASS (`exit 0`) | `ok internal/compile; round-trip and frozen-artifact contracts hold` |

## 6.4: Isolation for every mutating subcommand

### Subtasks
- [x] Capture a read-time digest and verify at write time
- [x] Emit a distinct re-read-and-retry diagnostic
- [x] Add the interleaving test

### Notes
Revision boundary: Concurrent mutation fails loudly instead of losing data.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- Focused review: `git show 7159b5b7dca76000c2f60d59d4132559e5fbe71f`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd section set --expect deadbeefdead` | `.` | PASS (`exit 0`) | `refuses a stale digest and names re-read-and-retry as the fix` |

## 6.5: Lifecycle verbs and gates

### Subtasks
- [x] Implement the transition verbs reusing the validator rule implementations
- [x] Implement evidence add, decide add with the collision check, next, show, list
- [x] Implement the FR-46 completed/frozen refusal and record the byte-sensitivity determination

### Notes
Revision boundary: Status transitions and ledger appends enforce their gates in code.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `923a35f13980c68435fe155561319d601630ebe6`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `923a35f13980c68435fe155561319d601630ebe6`
- Focused review: `git show 923a35f13980c68435fe155561319d601630ebe6`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `923a35f13980c68435fe155561319d601630ebe6`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd phase complete --dry-run` | `.` | PASS (`exit 0`) | `gates evaluated by the same rules sdd validate runs; refusals cite artifact and line` |

## 6.6: Error messages as the primary interface

### Subtasks
- [x] Implement did-you-mean for unknown flags
- [x] List available identifiers on unresolved references
- [x] Name expected heading and payload line on mismatch

### Notes
Revision boundary: Every refusal names the constraint and the correct form.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Focused review: `git show 687104f1393bdcef72ae72b31f7ab8ec0629a290`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `sdd valdate` | `.` | PASS (`exit 0`) | `did-you-mean suggests validate; digest mismatch names the fix` |

## Acceptance Criteria
- [x] A spec can be created and revised end to end through `sdd apply`, byte-idempotently.
- [x] Templates are schema-generated and byte-identical to the committed set on first generation.
- [x] Concurrent mutation refuses rather than losing data.
- [x] Completed and frozen artifacts are refused, with the byte-sensitivity determination recorded.
- [x] Every refusal message is covered by a golden test.

## Phase Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Final aligned review: Retro/06-Schema-And-Compiler-review.md; frozen: bc3383502115b7fd2160ec20169f2998c402bf7b..0acca6756ce07b83f4df2f987ac56ef55b40178e

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `go suite, parity gate, and template drift check all pass` |

### Completed task identities

- `6.1`: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- `6.2`: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
- `6.3`: `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- `6.4`: `7159b5b7dca76000c2f60d59d4132559e5fbe71f`
- `6.5`: `923a35f13980c68435fe155561319d601630ebe6`
- `6.6`: `687104f1393bdcef72ae72b31f7ab8ec0629a290`
