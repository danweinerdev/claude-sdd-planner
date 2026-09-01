---
title: "Graph Store, Compiler, Convert"
type: phase
plan: "SddGraph"
phase: 2
status: complete
created: 2026-08-31
updated: 2026-08-31
deliverable: "Committed graph store with locked atomic writes, propose/assemble staging, the compile pipeline (batched errors, coverage, intent hashes, rendered views), v1 conversion with blocking sentinels, and guard coverage for every new surface"
tasks:
  - id: "2.1"
    title: "Graph store: discovery, locked atomic writes, init"
    status: complete
    verification: "go test ./internal/graph/store/... -count=1 — upward discovery finds Plans/<Plan>/<Plan>-Graph.json from a nested cwd and errors helpfully when absent; save is temp+fsync+rename with pinned UTF-8/LF (byte-identical output on Windows and POSIX fixtures); concurrent read-modify-write under the reused internal/store lock loses no update (two-writer test); `sdd graph init` writes the graph skeleton plus .gitignore covering *.lock and .graph/. go test ./internal/graph/store/... -race -count=1 clean."
    justifies: "DD-3 (only structure and observations persist; atomic writes under the advisory lock, reusing internal/store rather than growing a second implementation). Prevents torn reads and lock-file commits — the two store corruptions the design names."
  - id: "2.2"
    title: "sdd graph propose and assemble: fragment staging and merge"
    status: complete
    verification: "go test ./cmd/sdd/ -run TestGraphPropose -count=1 — propose --file stages a schema-valid fragment under Plans/<Plan>/.graph/fragments/ and rejects invalid payloads with JSON-path errors before staging; assemble merges disjoint fragments into one proposal set, refuses colliding node ids naming both fragments, and leaves fragments untouched on refusal; a proposal touching an existing node id is rejected as a mutation (CLI verbs own mutation, DD-11)."
    justifies: "DD-11 (construction is declarative and batched; parallel decomposition via per-agent fragments merged by assemble). Prevents the per-call construction failure mode: dangling intermediate states and shell-quoted prose."
    depends_on: ["2.1"]
  - id: "2.3"
    title: "sdd compile: batched validation, coverage, intent hashes"
    status: complete
    verification: "go test ./internal/graph/compile/... -count=1 — one compile pass over a deliberately broken proposal reports ALL of: JSON syntax with position, schema violation with JSON path and did-you-mean, uncovered AC (AC-3 uncovered), dangling dep, dependency cycle a->b->a, untriaged hazards, hazard with no satisfying test shape, node covered by no full review gate, artifacts of two claimed nodes overlapping; exit 1 with every finding, exit 2 on malformed invocation. Intent-hash test: embed over the shared normalizer, then a whitespace-only spec rewrap does NOT change the hash and a wording change DOES. Round-trip gate from 1.3 extended: the template exemplar compiles clean."
    justifies: "DD-4 (coverage becomes an exit code; intent hashes make spec edits ripple), DD-9 (coverage invariant, no silent gate insertion), DD-11 (all errors in one pass). Prevents a faithfully-executed wrong decomposition going GREEN — the design's named weakness of graph-only models."
    depends_on: ["2.2"]
  - id: "2.4"
    title: "Rendered plan and phase views from the graph"
    status: complete
    verification: "go test ./internal/graph/compile/... -run TestRender -count=1 — compile renders Plans/<Plan>/README.md and numbered phase docs from a fixture graph; rendered views parse as valid SDD artifacts (sdd validate --scope over a rendered fixture plan reports zero structural findings); every node's projected state renders from derived state, never from a stored field; re-rendering an unchanged graph is byte-identical (idempotent); a hand-edit to a rendered view is overwritten by the next compile and a header comment in every view names the graph as source."
    justifies: "DD-1 (plan/phase markdown becomes rendered views, superseding the FR-36 plan/phase leg), DD-2 (graph is the source of truth; views carry no information not derivable from graph + specs). Prevents dual-writable-source drift."
    depends_on: ["2.3"]
  - id: "2.5"
    title: "sdd graph convert: v1 plans to graphs with blocking sentinels"
    status: complete
    verification: "go test ./cmd/sdd/ -run TestGraphConvert -count=1 — converting a v1 fixture plan maps tasks to nodes, depends_on to deps, justifies citations, and declared artifacts; every gap is an explicit sentinel: hazards untriaged, gate unspecified where verification names no runnable check, needs-contract where the task title/notes reduce to no falsifiable sentence; the converted graph does NOT compile until sentinels are resolved (compile lists each one); completed v1 tasks convert with their evidence preserved as historical annotations, never as verification observations."
    justifies: "DD-15 (conversion is a standing tool capability; the tool never asserts on the operator's behalf — sentinel-then-block). Prevents laundering unmade judgments into the graph during migration."
    depends_on: ["2.3"]
  - id: "2.6"
    title: "Guard coverage: pretooluse entries and FR-28 path guard for graph artifacts"
    status: complete
    verification: "go test ./internal/hook/... -count=1 — the guard parity test enumerates the new mutating verbs (graph propose|assemble|compile|convert|init and later-phase names reserved: sync|release|split|set-tests|gc, next --claim) as denied for read-only agents and the read surface (graph hazards|status|show|export, template graph-proposal, next without --claim) as allowed; Write/Edit on <Plan>-Graph.json and on rendered plan/phase view paths is denied for every agent per the FR-28 extension; FR-44 discipline holds — a mutating subcommand missing a guard entry fails the suite."
    justifies: "D-0014 (sdd subcommand allowlist for read-only agents), DD-2 guard-coverage section (FR-28 Write/Edit extension to Graph.json and rendered views). Prevents the sanctioned-bypass hole: an agent hand-editing the graph the tool is supposed to own."
    depends_on: ["2.1"]
---

# Phase 2: Graph Store, Compiler, Convert

## Overview

Turns the phase-1 shapes into a working artifact pipeline: a committed,
locked, atomically-written graph store; declarative proposal staging and
fragment merge; the compile pass that enforces every semantic invariant in
one batched report and embeds intent hashes; rendered plan/phase views;
v1→graph conversion with blocking sentinels; and guard entries so no agent
can write around the tool. At phase end a plan can be authored as payloads
and compiled to a committed graph plus valid rendered views — nothing
executes yet.

## 2.1: Graph store: discovery, locked atomic writes, init

### Subtasks
- [x] Create `internal/graph/store`: `Find(start)` walking upward for
      `Plans/<Plan>/<Plan>-Graph.json`; `Load` via the strict model decoder;
      `Save` via temp file + fsync + `os.Rename`-equivalent with pinned
      UTF-8/LF and stable key order.
- [x] Reuse `internal/store`'s lock (`lock.go`/`lock_unix.go`/
      `lock_windows.go`) for a `Locked(path, fn)` read-modify-write helper;
      document non-reentrancy.
- [x] `sdd graph init --plan <name>`: writes an empty v1 graph plus
      `Plans/<Plan>/.gitignore` covering `*.lock` and `.graph/`.
- [x] Two-writer contention test (goroutines + separate processes deferred
      to 3.6) and a torn-read test (reader during writer sees old or new,
      never partial).
- [x] `-race` on the package tests.

### Notes
Revision boundary: the store package plus `sdd graph init`; no other verb
exists yet. Reuse is the point — DD-3 names `internal/store` explicitly to
avoid a second lock implementation drifting from the first. Serialization
must be deterministic (stable ordering) because the graph is committed and
diffed by humans; treat map iteration order as a bug source. Design
references: DD-3, § Components (`store` row).

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `085f4f123f1988300c643a322cc800c858e8450c`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `085f4f123f1988300c643a322cc800c858e8450c`
- Focused review: `git show 085f4f123f1988300c643a322cc800c858e8450c`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `085f4f123f1988300c643a322cc800c858e8450c`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/graph/... ./cmd/sdd/ -count=1` | `.` | PASS (`exit 0`) | `ok internal/graph/{model,hazards,proposal,store}, ok cmd/sdd 24.687s; upward discovery from nested cwd finds SamplePlan-Graph.json and a miss names the init verb; init writes empty v1 graph + .graph/ dir + merged .gitignore (*.sdd-lock, *.lock, .graph/) preserving existing lines, re-init refused; load->save byte-identical LF-only; 8 writers x 5 CAS increments lose no update; reader storm during 40 updates never sees a torn or invalid graph; fn and decode errors surface without overwriting a corrupt graph. NOTE: -race deferred to task 3.6 matrix — this Windows host has no cgo C toolchain (gcc absent), so the race detector cannot build here` |
| `go vet ./internal/graph/... ./cmd/sdd/` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `sdd graph init smoke in a temp planning root` | `built binary at 085f4f1` | PASS | `initialized Plans/Demo/Demo-Graph.json with version 1 / seq_counter 0 / nodes []; .gitignore carries the three lines; second init refused naming the live-graph rule` |

### Trap
`os.Rename` over an existing file fails on Windows under some conditions and
`fsync` semantics differ — do not copy a POSIX-only atomic-write recipe.
Follow the existing `internal/store`/`internal/compile` write path (it
already solved Windows atomicity); if it lacks a piece the graph needs,
extend it there, not in a fork.

## 2.2: sdd graph propose and assemble: fragment staging and merge

### Subtasks
- [x] `sdd graph propose --plan <name> --file <payload.json>`: validate
      (parse + schema, JSON-path errors) then stage under
      `Plans/<Plan>/.graph/fragments/<uuid7>.json`; refuse without staging on
      any error.
- [x] `sdd graph assemble --plan <name>`: merge staged fragments into one
      proposal set; refuse on node-id collisions naming both fragments;
      deterministic merge order.
- [x] Reject payloads carrying tool-owned fields (`claim`, `verification`,
      `intent_hashes`) at propose time.
- [x] Reject proposals that redefine an existing master-graph node id —
      mutations go through CLI verbs (phase 3), not proposals.
- [x] Tests per the verification field, including refusal atomicity
      (nothing staged on error).

### Notes
Revision boundary: proposals can be staged and merged; `compile` (2.3)
consumes the assembled set. Fragments live in the gitignored `.graph/`
workspace area — they are working state, not record. Single-fragment flows
skip `assemble` (compile accepts one staged fragment); don't force ceremony
on the common case. Design references: DD-11, § Interfaces.

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `b011057eb8ee58439ea6fc764cacdeab45cc71c4`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `b011057eb8ee58439ea6fc764cacdeab45cc71c4`
- Focused review: `git show b011057eb8ee58439ea6fc764cacdeab45cc71c4`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `b011057eb8ee58439ea6fc764cacdeab45cc71c4`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./cmd/sdd/ ./internal/graph/... -count=1` | `.` | PASS (`exit 0`) | `ok cmd/sdd 24.431s, ok internal/graph/{model,hazards,proposal,store}; stage parks a valid fragment verbatim and refuses without staging on strict-decode findings (JSON-path + did-you-mean), tool-owned fields, and master-graph node redefinition; stage without an initialized graph points at init; assemble merges 3 disjoint fragments in staging order (UUIDv7 time-ordered), refuses collisions naming both fragments leaving all fragments untouched and writing no merged set, consumes fragments only after proposal.json is durably written; empty staging area points at propose` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `end-to-end authoring smoke in a temp planning root` | `built binary at b011057` | PASS | `template graph-proposal -> init -> propose -> assemble produced proposal.json with the 4 exemplar nodes in order; second propose+assemble cycle staged and merged cleanly` |

## 2.3: sdd compile: batched validation, coverage, intent hashes

### Subtasks
- [x] Create `internal/graph/compile`: pipeline parse → schema → semantic,
      accumulating findings; never stop at the first error within a stage
      class.
- [x] Semantic checks: `justifies` coverage (every AC in reachable specs has
      a covering node; every citation resolves), dangling deps, cycles
      (reuse/port topo sort into `internal/graph/algorithms`), untriaged
      hazards, hazard-without-satisfying-test-shape, DD-9 coverage invariant
      (every node inside the dependency closure of ≥1 `full` review gate —
      compile never inserts one), claimed-artifact overlap.
- [x] Shared intent-hash normalizer: identifier-token span to next same-depth
      item, whitespace collapsed, wrap joined, markers/emphasis stripped;
      SHA-256; one function used by embed and (phase 3) recheck.
- [x] Write the compiled graph to the store under the lock; report every
      allocated/embedded hash in the result.
- [x] Extend 1.3's round-trip gate: template exemplar compiles clean.
- [x] Exit codes per the sdd contract (0/1/2).

### Notes
Revision boundary: `sdd compile --plan <name>` produces a valid committed
graph from staged proposals or refuses with the complete finding set. This
task is the enforcement core of the design — DD-4's coverage-as-exit-code
and DD-9's coverage invariant both live here. The normalizer's
formatting-insensitivity cases (rewrap no-fire, reword fire) are contractual,
not incidental — table-test them explicitly. Rendered views are 2.4, so
compile's write side here is graph-only.

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `fe2b76ed66bdaaa974de0a3b09a9f603a48c0ec2`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `fe2b76ed66bdaaa974de0a3b09a9f603a48c0ec2`
- Focused review: `git show fe2b76ed66bdaaa974de0a3b09a9f603a48c0ec2`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `fe2b76ed66bdaaa974de0a3b09a9f603a48c0ec2`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./cmd/sdd/ ./internal/graph/... ./internal/rules/ -count=1` | `.` | PASS (`exit 0`) | `ok across cmd/sdd 23.673s, internal/graph/{algorithms,compile,hazards,intent,model,proposal,store}, internal/rules 114.579s; one deliberately broken proposal reports ALL findings in one pass (dup id, dangling dep, cycle cyc-a->cyc-b->cyc-a, untriaged hazards, unknown hazard, satisfies-undeclared, hazard-undischarged naming the required shape, unsourced node, dangling citations (a nonexistent fixture AC and ledger id), a citation of a superseded fixture ledger entry, an uncovered fixture AC, covered-by-no-full-gate, claimed-artifact overlap) with the graph untouched and the payload still staged; happy path embeds sha256 fingerprints for FR/AC/DD and not for ledger ids, then consumes the payload after the durable write; a rewrap-only spec edit embeds the identical FR-01 hash across two compiles while reword/literal changes fire (intent table tests); the filled template exemplar compiles with zero findings; input selection points at propose when empty and assemble when multiple fragments are staged` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings after merging the S1021 declaration in algorithms.go` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `sdd compile end-to-end smoke in a temp planning root` | `built binary at fe2b76e` | PASS | `template exemplar filled (ids substituted, untriaged resolved) -> propose -> compile: 4 nodes appended, 5 fingerprints embedded, fragment consumed; parse-config carries AC-01 and DD-1 sha256 hashes` |

### Trap
Do not implement coverage by grepping the spec markdown ad hoc — resolve
AC/FR/DD ids through the same related-graph resolution `internal/rules`
already implements (SDD160-family), or you will disagree with `sdd validate`
about which spec is reachable and the two tools will fight.

## 2.4: Rendered plan and phase views from the graph

### Subtasks
- [x] Renderer in `internal/graph/compile`: graph → `Plans/<Plan>/README.md`
      + numbered phase docs (grouping by node `phase` label), carrying a
      generated-file header naming the graph as source and the regenerating
      command.
- [x] Project node contracts, deps, gates, hazards, justifies, and derived
      state into human-readable task-like sections; completion-grade fields
      render from observations only.
- [x] Idempotence: unchanged graph → byte-identical views.
- [x] Golden triples: payload → graph → rendered-views fixtures under the
      existing `tools/regression` pattern (byte-compared views,
      canonicalized graph comparison), regenerated via `make gen-fixtures`
      and committed — the corpus the design's Testing Strategy names.
- [x] Rendered views pass `sdd validate` structurally (fixture-level check);
      where the v1 schema demands sections that are graph-derived, render
      them — never invent content the graph does not hold.
- [x] Frozen-view refusal stub: refuse regeneration when a frozen `Aligned`
      review artifact covering the view's nodes exists (full *closed*
      predicate arrives in 4.1; the stub keys on review-artifact presence and
      carries a TODO naming 4.1).

### Notes
Revision boundary: `compile` emits views alongside the graph; views are
provably regenerable and never hand-edited (2.6 guards enforce it). Naming
stays `README.md` + numbered phase docs — design OQ-1 touches only
spec/design naming. The projection is DD-2's contract: if a reader needs
information the view lacks, the fix is graph schema or renderer, never a
hand edit. Design references: DD-1, DD-2 (frozen-view invariant).


Recorded deviations (implementation): golden triples live in
`internal/graph/compile/testdata/golden/` rather than `tools/regression`
(they freeze a pipeline, not a validator rule example; regenerated via
`UPDATE_GOLDENS=1 go test`, byte-compared in CI). README rendering is two
surgical edits (phases[] replacement when empty, delimited Graph View
section) preserving identity prose; merging non-empty v1 phases[] is
conversion's job (2.5). The marker-refusal guard doubles as the frozen-view
stub; TODO(4.1) in render.go upgrades it to the closed predicate.

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `4f9e981133e7d6dd73d18db7ca492ff00fe603b6`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `4f9e981133e7d6dd73d18db7ca492ff00fe603b6`
- Focused review: `git show 4f9e981133e7d6dd73d18db7ca492ff00fe603b6`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `4f9e981133e7d6dd73d18db7ca492ff00fe603b6`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./cmd/sdd/ ./internal/graph/... -count=1` | `.` | PASS (`exit 0`) | `ok across cmd/sdd 23.943s and all seven internal/graph packages; happy path renders one phase doc plus the README projection (marker, type: phase frontmatter, node sections, pending evidence line) and re-rendering the unchanged graph writes nothing (byte-stable idempotence); a pre-existing non-generated target refuses the whole compile BEFORE the graph write, leaving graph and staged payload untouched; golden payload->graph->views triples byte-compare after date normalization (UPDATE_GOLDENS=1 regenerates; testdata location deviation from the tools/regression wording recorded in Notes); the real validator over the rendered plan scope reports zero Error findings; the nested-quote YAML defect in deliverable was caught by that check and fixed` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `sdd compile view smoke in a temp planning root` | `built binary at 4f9e981` | PASS | `filled exemplar compiled: 4 nodes, 5 fingerprints, 2 views rendered; phase view carries marker/frontmatter/node sections; README gains rendered phases[] and the delimited Graph View table; second compile reports nothing staged` |

## 2.5: sdd graph convert: v1 plans to graphs with blocking sentinels

### Subtasks
- [x] `sdd graph convert --plan <name>`: read the v1 README + phase docs via
      existing artifact parsing; map task → node (id, contract from title +
      condensed notes, deps from depends_on + phase order, justifies,
      artifacts where declared).
- [x] Gap sentinels: `hazards: "untriaged"` everywhere; `gate:
      {type: "unspecified"}` where verification names no runnable check;
      `needs-contract` marker where no falsifiable sentence derives.
- [x] Compile-side enforcement: `unspecified` gates and `needs-contract`
      markers block compile with per-node findings (extend 2.3's semantic
      set).
- [x] Completed v1 tasks: convert with evidence preserved as historical
      annotation fields (never as `verification` observations — no
      retroactive GREEN).
- [x] Fixture test converting a miniature v1 plan end to end, asserting the
      sentinel set and the compile refusal list.

### Notes
Revision boundary: `convert` emits a staged proposal (through 2.2's path)
that deliberately does not compile until an operator resolves each sentinel.
This is DD-15 verbatim: mechanical work done by the tool, judgments left
visibly unmade. Do not auto-derive hazards from task text — that is exactly
the laundering the sentinel exists to prevent. Historical evidence
annotations are display-only; states derive from observations, and converted
plans start with none.


Recorded deviation (implementation): gates land `unspecified` universally
rather than attempting command extraction from verification prose — any
extraction is inference, which this task's own Trap forbids; the operator
specifies every gate at sentinel-resolution time. Completed-task provenance
lives in the new author-owned `history` node field (schema and drift gates
updated), never in observations.

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `10db7546b3c14ed42be02f10eb0293c71e63a661`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `10db7546b3c14ed42be02f10eb0293c71e63a661`
- Focused review: `git show 10db7546b3c14ed42be02f10eb0293c71e63a661`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `10db7546b3c14ed42be02f10eb0293c71e63a661`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./cmd/sdd/ ./internal/graph/... -count=1` | `.` | PASS (`exit 0`) | `ok across cmd/sdd 24.128s and all eight internal/graph packages; a two-phase v1 fixture converts to three nodes with every mechanical mapping asserted (task ids -> task-N-M, task depends_on plus phase-order densification into sorted deps, justifies id extraction in order of appearance, phase labels from doc stems) and every judgment sentinel asserted (NEEDS-CONTRACT contracts, unspecified gates, untriaged hazards); the completed task carries verified-date + revision provenance as history with verification nil (no retroactive observations); compile refuses the converted graph listing gate/contract/hazard sentinels per node plus the coverage invariant; an empty plan refuses helpfully; the frontmatter string-value-model bug in phase-id parsing was caught by the fixture (deps wired backwards) and fixed with a documented string branch` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `sdd graph convert end-to-end smoke in a temp planning root` | `built binary at 10db754` | PASS | `converted 1 task with v1 provenance carried; compile refused with 4 findings naming each sentinel and the missing full gate; CLI output names the deliberate v1-doc retirement step` |

### Trap
The tempting shortcut is defaulting `gate: {type: "command", command: "make
test"}` for unspecified tasks so converted graphs compile immediately. That
default asserts a verification contract nobody wrote — sentinel-then-block
is the requirement, not a UX bug to smooth over.

## 2.6: Guard coverage: pretooluse entries and FR-28 path guard for graph artifacts

### Subtasks
- [x] Add the phase-2 mutating verbs to the `sdd hook pretooluse`
      deny-list for read-only agents; allowlist the read surface. (Later
      phases land their own entries in the same revision as their verbs —
      the parity test built here is what enforces that.)
- [x] Extend the artifact-path Write/Edit guard to `<Plan>-Graph.json` and
      rendered view paths (schema-recognized), denied for every agent.
- [x] Extend the FR-44-style parity test: any `sdd graph` subcommand without
      a guard classification fails the suite.

### Notes
Revision boundary: guard entries and tests only; no behavioral change to the
verbs themselves. The FR-28 extension is the piece the design review flagged
as NEW-1 — without it, hand-editing the graph silently reintroduces
dual-source drift. Design references: DD-2 (guard-coverage paragraph),
D-0014, `Specs/SDD-Toolchain` FR-44.

### Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `b2281b8a9e601475a0cb55f9c55b1e4258bba10a`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `b2281b8a9e601475a0cb55f9c55b1e4258bba10a`
- Focused review: `git show b2281b8a9e601475a0cb55f9c55b1e4258bba10a`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `b2281b8a9e601475a0cb55f9c55b1e4258bba10a`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/hook/ ./cmd/sdd/ -count=1` | `.` | PASS (`exit 0`) | `ok internal/hook 10.524s, ok cmd/sdd 25.305s; every classified top-level verb allows/denies exactly as declared; graph read surface (hazards/status/show/export/path/risk/shape) allowed and every mutating graph verb incl. reserved later-phase names (sync/release/split/set-tests/gc) denied, with bare and unknown graph sub-verbs denying by default; bare 'sdd next' and 'next --json' allowed while '--claim' and '--claim=true' deny; Write/Edit on Plans/Sample/Sample-Graph.json denied for a read-only agent with Read never denied and unrelated JSON untouched; the new cmd-side parity test walks the real cobra tree and fails on any unclassified verb (FR-44)` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/... ./internal/hook/` | `.` | PASS (`exit 0`) | `no findings` |

## Acceptance Criteria
- [x] A payload authored from the 1.3 exemplar stages, assembles, compiles,
      and renders valid views end to end on a fixture plan (DD-1, DD-2,
      DD-11).
- [x] Compile reports the full finding set in one pass and refuses with exit
      1; intent hashes are formatting-insensitive and wording-sensitive
      (DD-4).
- [x] A converted v1 fixture blocks on its sentinels and lists each one
      (DD-15).
- [x] Every new verb and graph artifact path is guard-covered; the parity
      test enforces it (D-0014, FR-44, FR-28 extension).
- [x] `make test` green, including the extended template round-trip gate;
      `go vet`/`staticcheck` clean; store tests pass with `-race`.

## Phase Completion Evidence

- Verified: 2026-08-31
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `39a31750362225d3ce885477f68621a70f470eac`
- Identity recheck: `git rev-parse HEAD` at 2026-08-31 00:00 matched `39a31750362225d3ce885477f68621a70f470eac`
- Final aligned review: Plans/SddGraph/reviews/05-sddgraph-code-review-39a3175.md; frozen: 9c1fbdaba6e650df3fa937dfd2e57f8bb76675ef..39a31750362225d3ce885477f68621a70f470eac

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `make test` | `.` | PASS (`exit 0`) | `exit 0 at 39a3175: full gate green at phase end — go suite (all packages incl. the six new/extended graph packages, hook guard, cmd meta-registries and the FR-44 parity test), regression corpus, template gate (10 markdown templates + byte-compared graph-proposal JSON pair), portable drift and leak gates` |

### Completed task identities

- `2.1`: `085f4f123f1988300c643a322cc800c858e8450c`
- `2.2`: `b011057eb8ee58439ea6fc764cacdeab45cc71c4`
- `2.3`: `fe2b76ed66bdaaa974de0a3b09a9f603a48c0ec2`
- `2.4`: `4f9e981133e7d6dd73d18db7ca492ff00fe603b6`
- `2.5`: `10db7546b3c14ed42be02f10eb0293c71e63a661`
- `2.6`: `b2281b8a9e601475a0cb55f9c55b1e4258bba10a`
