---
title: "Payload Schema and Templates"
type: phase
plan: "SddGraph"
phase: 1
status: complete
created: 2026-08-31
updated: 2026-08-31
deliverable: "Strictly-decoded graph/payload model, closed hazard vocabulary, and the graph-proposal template skeleton + JSON Schema generated from one source and CI-gated against drift"
tasks:
  - id: "1.1"
    title: "Graph and payload model with strict JSON decoding"
    status: complete
    verification: "go test ./internal/graph/model/... -count=1 — round-trip (to_dict(from_dict(x)) == x) on a full-featured fixture graph; unknown key rejected with did-you-mean naming the nearest field and a JSON path (nodes[2].gate.tets[0] -> did you mean 'tests'); version != 1 rejected naming the supported schema version (no migrate verb exists yet — v1 is the first schema; the error must not reference one); malformed verification.result rejected naming the two valid values. go vet ./internal/graph/... and staticcheck ./internal/graph/... clean."
    justifies: "DD-12 (strict decoding, JSON-path errors), DD-3 (structure-and-observations-only persisted shape). Prevents a hallucinated payload key being silently dropped and surfacing later as an uncovered AC blamed on the wrong node."
  - id: "1.2"
    title: "Closed hazard vocabulary with required test shapes"
    status: complete
    verification: "go test ./internal/graph/hazards/... -count=1 — every vocabulary entry carries a nonempty required-test-shape description; require_known rejects an unknown hazard naming the vocabulary; the untriaged sentinel round-trips as the literal string and is distinct from an explicit empty list; a node with hazards untriaged is reported as untriaged by the model layer."
    justifies: "DD-9 (gate vocabulary), DD-13 (hazard discharge requires a test of a specific shape). Prevents 'passing tests that guard nothing' — the defect class the vocabulary encodes."
    depends_on: ["1.1"]
  - id: "1.3"
    title: "sdd template graph-proposal: skeleton and schema from one source"
    status: complete
    verification: "go test ./cmd/sdd/ -run TestGraphProposalTemplate -count=1 — `sdd template graph-proposal` emits a placeholder-complete exemplar that validates against the schema emitted by `sdd template graph-proposal --schema` (the round-trip gate); the exemplar demonstrates a tests gate, a command gate, a review gate, a filled hazards list, an untriaged sentinel, and a terminal full review gate depending on every sink; the drift test fails if exemplar and schema are edited independently. Manual: `sdd template graph-proposal --schema | python -m json.tool` exits 0."
    justifies: "DD-12 (skeleton + schema from one source, CI-gated exactly like the existing markdown template gate; the exemplar is what makes LLM payloads fill-in rather than guess)."
    depends_on: ["1.1", "1.2"]
---

# Phase 1: Payload Schema and Templates

## Overview

Lands the data layer everything else consumes: the Go model for
`<Plan>-Graph.json` and the `graph propose` payload (strict decoding,
versioned, JSON-path errors), the closed hazard vocabulary with its
required-test-shape contract, and the `sdd template graph-proposal`
skeleton/schema pair generated from one source. No disk I/O, no CLI verbs
beyond `template` — pure shapes and their gates, so phases 2–4 build on a
frozen wire contract.

## 1.1: Graph and payload model with strict JSON decoding

### Subtasks
- [x] Create `internal/graph/model` with `Graph`, `Node`, `Gate`, `Test`,
      `Verification`, `Claim` types mirroring the design's data model
      (`version`, `seq_counter`, `nodes[]`; node: `id`, `contract`,
      `justifies`, `intent_hashes`, `deps`, `gate{type,tests|command|lanes}`,
      `hazards`, `artifacts`, `estimate`, `phase`, `claim?`, `verification?`
      with `result/seq/artifact_digests/report_digest/isolation/provenance`
      and per-test `red_seq`).
- [x] Implement `FromDict`/`ToDict` (or `UnmarshalJSON`/`MarshalJSON`) with
      strict decoding: unknown keys are errors carrying a JSON path and a
      nearest-field did-you-mean (Levenshtein over the struct's known keys).
- [x] Enforce field invariants at decode: nonempty node id/contract,
      `estimate >= 1` defaulting to 1, `verification.result` ∈ {pass, fail},
      `isolation` ∈ {clean, shared-dirty, asserted}, gate `type` ∈ {tests,
      command, review}, `version == 1` with a version-mismatch error naming
      the supported version.
- [x] Separate payload shapes (proposal/fragment) from the master-graph
      shape — proposals carry no `claim`/`verification`/`intent_hashes`
      (tool-owned fields are rejected in payloads, same posture as FR-18).
- [x] Table-driven tests: round-trip fixture, every rejection case with its
      exact JSON path, tool-owned-field rejection.

### Notes
Revision boundary: `internal/graph/model` compiles, is fully unit-tested, and
is imported by nothing else yet. The wire contract (field names, sentinel
spellings, valid enums) is frozen here — phases 2–4 must not need to change
it. Design references: `Designs/SddGraph` § Data model, DD-12. Tool-owned
fields in payloads are rejected, not ignored, mirroring the spec's FR-18
posture for `apply`. Keep `hazards: null`-vs-`"untriaged"`-vs-`[]` handling
in this task's tests — it is the subtlest decode case (1.2 defines the
vocabulary; the sentinel plumbing lives here).

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `e37df494a163ea466fe71143727584d107684cc2`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `e37df494a163ea466fe71143727584d107684cc2`
- Focused review: `git show e37df494a163ea466fe71143727584d107684cc2`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `e37df494a163ea466fe71143727584d107684cc2`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/graph/model/ -count=1` | `.` | PASS (`exit 0`) | `ok internal/graph/model 0.275s; round-trip byte-identical on the full-featured fixture; unknown-key did-you-mean with JSON path (nodes[0].gate: unknown key "tets" — did you mean "tests"?); version 2 rejected naming supported version 1 with no migrate reference; result/isolation/gate-type enums rejected naming valid values; estimate 0 and 1.5 rejected; hazards untriaged/[]/list decode distinctly; 4+ findings batched in one pass; all four tool-owned fields rejected in proposals and accepted in graphs` |
| `go vet ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

### Trap
You will want to use plain `encoding/json` with struct tags and call it
strict. Don't — stock `json.Unmarshal` silently drops unknown keys, which is
precisely the failure DD-12 forbids. Decode via `json.Decoder` with
`DisallowUnknownFields` *plus* a raw-map pass to produce JSON paths and
did-you-mean, or token-walk; the error quality is the contract, not a
nicety.

## 1.2: Closed hazard vocabulary with required test shapes

### Subtasks
- [x] Create `internal/graph/hazards`: the vocabulary as data — each entry
      `{name, required test shape description}` seeded from the design's
      table (order-sensitive, computes-number, derives-state, persists-state,
      user-entrypoint, ships-prose, concurrent-access, external-format,
      deterministic-replay, frame-coupled).
- [x] `RequireKnown(name, where)` returning a vocabulary-naming error for
      unknown hazards; `Untriaged` sentinel constant.
- [x] `sdd graph hazards` read-only subcommand printing the vocabulary and
      each hazard's required test shape (`--json` supported).
- [x] Tests: unknown rejection, sentinel distinctness from empty list,
      vocabulary completeness (every entry has a nonempty shape description).

### Notes
Revision boundary: the vocabulary package plus one read-only CLI verb; no
compile-time enforcement yet (that is 2.3's coverage checks). The vocabulary
is closed and extended only by evidence — the package doc-comment must say
so. Design references: `Designs/SddGraph` § Data model (hazard vocabulary),
DD-9, DD-13. `sdd graph hazards` is read-only and gets allowlisted for
read-only agents when 2.6 lands the guard entries; note that forward
dependency in the code comment, not as a hidden coupling.

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `ed539842fd2c45838fa44d4b0de75fae9bc1e807`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `ed539842fd2c45838fa44d4b0de75fae9bc1e807`
- Focused review: `git show ed539842fd2c45838fa44d4b0de75fae9bc1e807`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `ed539842fd2c45838fa44d4b0de75fae9bc1e807`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/graph/hazards/ ./cmd/sdd/ -count=1` | `.` | PASS (`exit 0`) | `ok internal/graph/hazards 0.069s, ok cmd/sdd 17.151s; vocabulary complete (10 entries, all with nonempty required shapes, canonical order enforced, All() returns a copy); RequireKnown rejects unknown hazards naming the caller location and the full vocabulary; RequireKnownAll batches deterministically; subcommand and handler-flag meta-registries updated deliberately` |
| `go vet ./internal/graph/... ./cmd/sdd/` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings; pre-existing cmd/sdd U1000 findings (evidenceUsage, reviewUsage, transitionUsage unused consts) confirmed present at HEAD before this task via stash probe — untouched, out of task scope` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `sdd graph hazards / --json smoke` | `built binary at ed53984` | PASS | `text table lists 10 hazards with required shapes plus the explicit-empty-list note; --json parses, 10 entries, first computes-number` |

## 1.3: sdd template graph-proposal: skeleton and schema from one source

### Subtasks
- [x] Define the proposal JSON Schema (payload `version`, `nodes[]`,
      fragment metadata) generated from — or CI-checked against — the same
      source that renders the skeleton exemplar (follow the existing
      `internal/schema` spec-schema-covers-template test pattern).
- [x] Extend `cmd/sdd/template.go` with the `graph-proposal` template:
      default output is the placeholder-complete exemplar; `--schema` emits
      the JSON Schema.
- [x] Exemplar content: one node per gate type, a filled hazard list and an
      `"untriaged"` node, `justifies` citing placeholder `AC-NN`/`DD-N`,
      `deps`, `artifacts`, `estimate`, and a terminal `full` review gate
      depending on every sink node (the DD-9 coverage backstop).
- [x] Round-trip gate test: the emitted exemplar validates against the
      emitted schema; wire into the existing `sdd template --check` path so
      `make test` fails on drift.

### Notes
Revision boundary: `sdd template graph-proposal [--schema]` works end to end
and is drift-gated; nothing consumes proposals yet. The exemplar is the
authoring contract for every LLM that will ever propose a graph — favor
demonstrative values over terse ones (DD-12: models replicate exemplars, they
don't satisfy specifications). When 2.3 lands, extend the round-trip test to
"exemplar compiles clean" — leave a TODO naming that test by name. Schema
files live beside the existing ones in `internal/schema/` unless size argues
for `internal/graph/schema/`; either way one source, two outputs.

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `7483157e34d20584796bc2fa1ecc6f7eaf693aa4`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `7483157e34d20584796bc2fa1ecc6f7eaf693aa4`
- Focused review: `git show 7483157e34d20584796bc2fa1ecc6f7eaf693aa4`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `7483157e34d20584796bc2fa1ecc6f7eaf693aa4`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./cmd/sdd/ ./internal/graph/... -count=1` | `.` | PASS (`exit 0`) | `ok cmd/sdd 17.275s, ok internal/graph/{hazards,model,proposal}; exemplar decodes clean via DecodeProposal (round-trip gate); byte-deterministic LF-only render; demonstrates tests/command/review gates, untriaged/filled/explicit-empty hazards, AC-NN/FR-NN/DD-N placeholders, terminal full review gate covering every sink; schema properties == model.KeySets() with additionalProperties:false everywhere; gate.type enum == model constants; version const == SchemaVersion; required lists == decoder requirements` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `sdd template graph-proposal / --schema / template --check smoke` | `built binary at 7483157` | PASS | `exemplar emits 4 nodes (define-schema, parse-config, build-gate, feature-review); --schema emits defs {gate,node,test}; committed shared/templates copies written; 'sdd template --check' reports 10 templates match plus byte-compared JSON pair; make plugins + plugins-check in sync` |

### Trap
Do not hand-write the skeleton and the schema as two artifacts that a test
merely compares loosely (e.g., key-name presence). The gate must be the real
validator run on the real exemplar — anything weaker recreates the
template-drift problem this task exists to prevent.

## Acceptance Criteria
- [x] `internal/graph/model` round-trips the full-featured fixture and
      rejects every malformed case with a JSON-path error (DD-12).
- [x] Hazard vocabulary is closed, described, and CLI-inspectable
      (`sdd graph hazards`).
- [x] `sdd template graph-proposal` exemplar validates against
      `--schema` output in CI; independent edits fail `make test`.
- [x] `go vet` and `staticcheck` clean over `internal/graph/...`;
      `make test` green.

## Phase Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `edee5d595b069bb5a76505c9fc611bfac2df1efd`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `edee5d595b069bb5a76505c9fc611bfac2df1efd`
- Final aligned review: Plans/SddGraph/reviews/04-sddgraph-code-review-edee5d5.md; frozen: c59170f34737eb905844d5b88a1e4cb3e0a21aec..edee5d595b069bb5a76505c9fc611bfac2df1efd

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `exit 0 at edee5d5: full gate green — go suite (internal/graph/{model,hazards,proposal}, cmd/sdd incl. the resolve freeze gate and hermetic review fixtures), regression corpus, template gate (10 markdown templates + byte-compared graph-proposal JSON pair), portable drift and leak gates` |

### Completed task identities

- `1.1`: `e37df494a163ea466fe71143727584d107684cc2`
- `1.2`: `ed539842fd2c45838fa44d4b0de75fae9bc1e807`
- `1.3`: `7483157e34d20584796bc2fa1ecc6f7eaf693aa4`
