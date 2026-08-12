---
title: "Compiler Feel Spike"
type: phase
plan: "SDD-Toolchain"
phase: 1
status: complete
created: 2026-08-03
updated: 2026-08-11
deliverable: "A throwaway Go spike that parses a spec proposal and prints normalized output, plus a written go/no-go assessment of the compiler model"
tasks:
  - id: "1.1"
    title: "Go module skeleton and the spec schema as data"
    status: complete
    verification: "`go build ./...` succeeds; the schema loads and round-trips through its own loader; a unit test asserts the schema declares every heading, frontmatter field with ownership, and identifier namespace present in shared/templates/spec.md"
    justifies: "FR-14 requires each artifact type be declared in exactly one machine-readable schema; without a real schema for one type there is nothing for the parser or apply to be tested against."
  - id: "1.2"
    title: "Frontmatter and Markdown parsing into an inspectable model"
    status: complete
    verification: "Parsing every existing artifact under a copy of the planning root yields a model with correct source line positions for every heading and frontmatter key, asserted against golden files"
    justifies: "FR-29 requires line-accurate error messages and FR-19 requires per-section matching; both are impossible without an AST carrying source positions, which is the specific reason the spec names a CommonMark parser with position info as a dependency."
    depends_on: ["1.1"]
  - id: "1.3"
    title: "Section matcher with near-miss normalization and refusal"
    status: complete
    verification: "Table-driven tests cover every FR-19 auto-correction and every FR-19 refusal, each asserting the itemized correction list or the refusal message; a deliberately mangled spec produces all violations in one result"
    justifies: "FR-19 near-miss tolerance is the single largest determinant of whether the compiler is pleasant or infuriating to drive; this task is where that hypothesis is falsifiable."
    depends_on: ["1.2"]
  - id: "1.4"
    title: "apply --dry-run --diff over the round-trip contract"
    status: complete
    verification: "On a copied planning root: applying an already-normalized artifact is byte-idempotent with zero corrections; each FR-45 identifier case (preserved, unknown, new, omitted, --retire) produces the specified outcome; nothing is ever written outside the copy"
    justifies: "FR-45 and FR-24 were both written in response to review findings F-01 and F-03 and have never been executed; the round-trip contract is the compiler operation the plugin will use most and is the likeliest place the design is still wrong."
    depends_on: ["1.3"]
  - id: "1.5"
    title: "Feel assessment and go/no-go on the compiler model"
    status: complete
    verification: "A written assessment exists recording: tool-call count and token cost to author one spec via the compiler versus one Write, the measured stripped binary size, measured single-artifact timings, the corrections needed on each existing artifact, and an explicit recommendation to proceed, amend, or abandon"
    justifies: "Prevents the concrete failure of building Phases 3 through 7 on an unvalidated hypothesis; also supplies the measurements NFR-07 is provisional pending and that finding F-12 flagged as unsourced."
    depends_on: ["1.4"]
---

# Phase 1: Compiler Feel Spike

## Overview
Answers the only question that can invalidate the design before real money is spent:
does the artifact-compiler model actually feel right to author against? Everything
here is deliberately throwaway — no parity work, no hooks, no distribution, no
writes to the real planning root. It exists to produce evidence and a decision,
not shippable code. FR-33's ordering bars the production compiler until Python is
gone; that rule protects byte-level differential comparison, which a spike
operating on a copied tree cannot disturb.

## 1.1: Go module skeleton and the spec schema as data

### Subtasks
- [x] Create `go.mod` with the module path `github.com/danweinerdev/claude-sdd-planner` and a declared Go floor
- [x] Add `cmd/sdd` with a minimal subcommand dispatcher (stdlib `flag`, no cobra yet)
- [x] Declare the `spec` artifact type as data: ordered headings with depth, frontmatter fields with author/tool ownership, identifier namespaces and grammars, free-prose sections
- [x] Embed the schema with `go:embed` and add a loader with a table-driven test

### Notes
Revision boundary: The repository builds as a Go module and can load a declarative schema for the `spec` artifact type.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `c19be0fee155d9fe548714e671996b92066ba4ea`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `c19be0fee155d9fe548714e671996b92066ba4ea`
- Focused review: `git show c19be0fee155d9fe548714e671996b92066ba4ea`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `c19be0fee155d9fe548714e671996b92066ba4ea`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/schema/` | `.` | PASS (`exit 0`) | `ok internal/schema; 17 artifact types load and validate` |

## 1.2: Frontmatter and Markdown parsing into an inspectable model

### Subtasks
- [x] Select and pin the CommonMark and YAML libraries, recording version and as-of date (discharges the pinning obligation the Requirements preamble places on the design)
- [x] Parse frontmatter into a node model preserving line, column, and comments
- [x] Parse the body into sections keyed by heading, retaining source spans
- [x] Golden-file tests over a copied `.plans/` tree

### Notes
Revision boundary: Any existing SDD artifact parses into a positioned model without loss.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `3c6698eae10ff4fffd4f933960b15f1576c4a87f`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `3c6698eae10ff4fffd4f933960b15f1576c4a87f`
- Focused review: `git show 3c6698eae10ff4fffd4f933960b15f1576c4a87f`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `3c6698eae10ff4fffd4f933960b15f1576c4a87f`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/artifact/` | `.` | PASS (`exit 0`) | `ok internal/artifact; frontmatter and sections parse with positions` |

## 1.3: Section matcher with near-miss normalization and refusal

### Subtasks
- [x] Fuzzy-match payload headings to schema slots (case, trailing punctuation, emphasis-as-heading, off-by-one depth)
- [x] Refuse the unambiguous cases: missing required section, duplicate slot mapping, unknown section
- [x] Itemize every auto-correction in the result
- [x] Implement the FR-23 lexical identifier check with its code-span, retired-identifier, and free-prose exemptions

### Notes
Revision boundary: A payload is either normalized with an itemized correction list or refused whole with every violation named.

### Trap
Making the matcher strict because strictness is easier to implement and test. A matcher that refuses `**Non-Goals**` instead of correcting it will produce retry loops that cost more than the errors they prevent — the spike exists to find where that line actually sits, so both branches must be built.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `ee1066ab158eed2031d814328d9fc7cb8d50f13c`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `ee1066ab158eed2031d814328d9fc7cb8d50f13c`
- Focused review: `git show ee1066ab158eed2031d814328d9fc7cb8d50f13c`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `ee1066ab158eed2031d814328d9fc7cb8d50f13c`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/compile/` | `.` | PASS (`exit 0`) | `ok internal/compile; near-miss headings refuse with SPK codes` |

## 1.4: apply --dry-run --diff over the round-trip contract

### Subtasks
- [x] Emit normalized output: LF, canonical heading order, forward-slash frontmatter paths, stamped `updated`
- [x] Implement identifier allocation from the FR-20 high-water mark, never filling gaps
- [x] Implement the FR-45 assertion semantics including `--retire`
- [x] Print the diff, the allocations, and the corrections; write nothing

### Notes
Revision boundary: A spec proposal can be dry-run compiled end to end, showing exactly what would be written.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `48d9aece80cd287771e5628b82f4b0a1ff8a7c35`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `48d9aece80cd287771e5628b82f4b0a1ff8a7c35`
- Focused review: `git show 48d9aece80cd287771e5628b82f4b0a1ff8a7c35`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `48d9aece80cd287771e5628b82f4b0a1ff8a7c35`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/store/` | `.` | PASS (`exit 0`) | `ok internal/store; atomic write and digest round-trip hold` |

## 1.5: Feel assessment and go/no-go on the compiler model

### Subtasks
- [x] Author a real spec end to end through the spike and record tool calls and token cost against the Write baseline
- [x] Dry-run every artifact in a copied planning root; record refusals and correction counts per artifact (evidence for FR-34)
- [x] Measure stripped binary size and single-artifact apply/validate timings on named hardware
- [x] Write the assessment with an explicit recommendation, and amend `Specs/SDD-Toolchain` or record new decisions where the spike contradicts it

### Notes
Revision boundary: A decision with evidence behind it: proceed, amend the spec, or abandon the compiler model.

### Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `85f019ec021636fe7cee094c717178ed19db5bac`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `85f019ec021636fe7cee094c717178ed19db5bac`
- Focused review: `git show 85f019ec021636fe7cee094c717178ed19db5bac`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `85f019ec021636fe7cee094c717178ed19db5bac`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./cmd/sdd/` | `.` | PASS (`exit 0`) | `ok cmd/sdd; apply/show/schema exercised end to end` |

## Acceptance Criteria
- [x] Compiling a spec proposal end to end is demonstrated on a copied planning root, with normalized output, itemized corrections, and allocations shown and nothing written to the real `.plans/`.
- [x] Every FR-19 auto-correction and refusal case, and every FR-45 identifier case, has a passing test.
- [x] The written feel assessment exists and carries an explicit proceed/amend/abandon recommendation with measurements, not impressions.
- [x] The YAML and CommonMark libraries are pinned with version and as-of date, discharging the obligation the spec places on the design.
- [x] No file outside the spike tree and the copied planning root is modified.

## Phase Completion Evidence

- Verified: 2026-08-11
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Identity recheck: `git rev-parse HEAD` at 2026-08-11 00:00 matched `0acca6756ce07b83f4df2f987ac56ef55b40178e`
- Final aligned review: Retro/01-Compiler-Feel-Spike-review.md; frozen: bc3383502115b7fd2160ec20169f2998c402bf7b..0acca6756ce07b83f4df2f987ac56ef55b40178e

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `go suite, parity gate, and template drift check all pass` |

### Completed task identities

- `1.1`: `c19be0fee155d9fe548714e671996b92066ba4ea`
- `1.2`: `3c6698eae10ff4fffd4f933960b15f1576c4a87f`
- `1.3`: `ee1066ab158eed2031d814328d9fc7cb8d50f13c`
- `1.4`: `48d9aece80cd287771e5628b82f4b0a1ff8a7c35`
- `1.5`: `85f019ec021636fe7cee094c717178ed19db5bac`
