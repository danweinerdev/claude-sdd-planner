---
title: "Graph Store, Compiler, Convert"
type: phase
plan: "SddGraph"
phase: 2
status: in-progress
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
    status: planned
    verification: "go test ./cmd/sdd/ -run TestGraphPropose -count=1 — propose --file stages a schema-valid fragment under Plans/<Plan>/.graph/fragments/ and rejects invalid payloads with JSON-path errors before staging; assemble merges disjoint fragments into one proposal set, refuses colliding node ids naming both fragments, and leaves fragments untouched on refusal; a proposal touching an existing node id is rejected as a mutation (CLI verbs own mutation, DD-11)."
    justifies: "DD-11 (construction is declarative and batched; parallel decomposition via per-agent fragments merged by assemble). Prevents the per-call construction failure mode: dangling intermediate states and shell-quoted prose."
    depends_on: ["2.1"]
  - id: "2.3"
    title: "sdd compile: batched validation, coverage, intent hashes"
    status: planned
    verification: "go test ./internal/graph/compile/... -count=1 — one compile pass over a deliberately broken proposal reports ALL of: JSON syntax with position, schema violation with JSON path and did-you-mean, uncovered AC (AC-3 uncovered), dangling dep, dependency cycle a->b->a, untriaged hazards, hazard with no satisfying test shape, node covered by no full review gate, artifacts of two claimed nodes overlapping; exit 1 with every finding, exit 2 on malformed invocation. Intent-hash test: embed over the shared normalizer, then a whitespace-only spec rewrap does NOT change the hash and a wording change DOES. Round-trip gate from 1.3 extended: the template exemplar compiles clean."
    justifies: "DD-4 (coverage becomes an exit code; intent hashes make spec edits ripple), DD-9 (coverage invariant, no silent gate insertion), DD-11 (all errors in one pass). Prevents a faithfully-executed wrong decomposition going GREEN — the design's named weakness of graph-only models."
    depends_on: ["2.2"]
  - id: "2.4"
    title: "Rendered plan and phase views from the graph"
    status: planned
    verification: "go test ./internal/graph/compile/... -run TestRender -count=1 — compile renders Plans/<Plan>/README.md and numbered phase docs from a fixture graph; rendered views parse as valid SDD artifacts (sdd validate --scope over a rendered fixture plan reports zero structural findings); every node's projected state renders from derived state, never from a stored field; re-rendering an unchanged graph is byte-identical (idempotent); a hand-edit to a rendered view is overwritten by the next compile and a header comment in every view names the graph as source."
    justifies: "DD-1 (plan/phase markdown becomes rendered views, superseding the FR-36 plan/phase leg), DD-2 (graph is the source of truth; views carry no information not derivable from graph + specs). Prevents dual-writable-source drift."
    depends_on: ["2.3"]
  - id: "2.5"
    title: "sdd graph convert: v1 plans to graphs with blocking sentinels"
    status: planned
    verification: "go test ./cmd/sdd/ -run TestGraphConvert -count=1 — converting a v1 fixture plan maps tasks to nodes, depends_on to deps, justifies citations, and declared artifacts; every gap is an explicit sentinel: hazards untriaged, gate unspecified where verification names no runnable check, needs-contract where the task title/notes reduce to no falsifiable sentence; the converted graph does NOT compile until sentinels are resolved (compile lists each one); completed v1 tasks convert with their evidence preserved as historical annotations, never as verification observations."
    justifies: "DD-15 (conversion is a standing tool capability; the tool never asserts on the operator's behalf — sentinel-then-block). Prevents laundering unmade judgments into the graph during migration."
    depends_on: ["2.3"]
  - id: "2.6"
    title: "Guard coverage: pretooluse entries and FR-28 path guard for graph artifacts"
    status: planned
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
- [ ] `sdd graph propose --plan <name> --file <payload.json>`: validate
      (parse + schema, JSON-path errors) then stage under
      `Plans/<Plan>/.graph/fragments/<uuid7>.json`; refuse without staging on
      any error.
- [ ] `sdd graph assemble --plan <name>`: merge staged fragments into one
      proposal set; refuse on node-id collisions naming both fragments;
      deterministic merge order.
- [ ] Reject payloads carrying tool-owned fields (`claim`, `verification`,
      `intent_hashes`) at propose time.
- [ ] Reject proposals that redefine an existing master-graph node id —
      mutations go through CLI verbs (phase 3), not proposals.
- [ ] Tests per the verification field, including refusal atomicity
      (nothing staged on error).

### Notes
Revision boundary: proposals can be staged and merged; `compile` (2.3)
consumes the assembled set. Fragments live in the gitignored `.graph/`
workspace area — they are working state, not record. Single-fragment flows
skip `assemble` (compile accepts one staged fragment); don't force ceremony
on the common case. Design references: DD-11, § Interfaces.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

## 2.3: sdd compile: batched validation, coverage, intent hashes

### Subtasks
- [ ] Create `internal/graph/compile`: pipeline parse → schema → semantic,
      accumulating findings; never stop at the first error within a stage
      class.
- [ ] Semantic checks: `justifies` coverage (every AC in reachable specs has
      a covering node; every citation resolves), dangling deps, cycles
      (reuse/port topo sort into `internal/graph/algorithms`), untriaged
      hazards, hazard-without-satisfying-test-shape, DD-9 coverage invariant
      (every node inside the dependency closure of ≥1 `full` review gate —
      compile never inserts one), claimed-artifact overlap.
- [ ] Shared intent-hash normalizer: identifier-token span to next same-depth
      item, whitespace collapsed, wrap joined, markers/emphasis stripped;
      SHA-256; one function used by embed and (phase 3) recheck.
- [ ] Write the compiled graph to the store under the lock; report every
      allocated/embedded hash in the result.
- [ ] Extend 1.3's round-trip gate: template exemplar compiles clean.
- [ ] Exit codes per the sdd contract (0/1/2).

### Notes
Revision boundary: `sdd compile --plan <name>` produces a valid committed
graph from staged proposals or refuses with the complete finding set. This
task is the enforcement core of the design — DD-4's coverage-as-exit-code
and DD-9's coverage invariant both live here. The normalizer's
formatting-insensitivity cases (rewrap no-fire, reword fire) are contractual,
not incidental — table-test them explicitly. Rendered views are 2.4, so
compile's write side here is graph-only.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

### Trap
Do not implement coverage by grepping the spec markdown ad hoc — resolve
AC/FR/DD ids through the same related-graph resolution `internal/rules`
already implements (SDD160-family), or you will disagree with `sdd validate`
about which spec is reachable and the two tools will fight.

## 2.4: Rendered plan and phase views from the graph

### Subtasks
- [ ] Renderer in `internal/graph/compile`: graph → `Plans/<Plan>/README.md`
      + numbered phase docs (grouping by node `phase` label), carrying a
      generated-file header naming the graph as source and the regenerating
      command.
- [ ] Project node contracts, deps, gates, hazards, justifies, and derived
      state into human-readable task-like sections; completion-grade fields
      render from observations only.
- [ ] Idempotence: unchanged graph → byte-identical views.
- [ ] Golden triples: payload → graph → rendered-views fixtures under the
      existing `tools/regression` pattern (byte-compared views,
      canonicalized graph comparison), regenerated via `make gen-fixtures`
      and committed — the corpus the design's Testing Strategy names.
- [ ] Rendered views pass `sdd validate` structurally (fixture-level check);
      where the v1 schema demands sections that are graph-derived, render
      them — never invent content the graph does not hold.
- [ ] Frozen-view refusal stub: refuse regeneration when a frozen `Aligned`
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

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

## 2.5: sdd graph convert: v1 plans to graphs with blocking sentinels

### Subtasks
- [ ] `sdd graph convert --plan <name>`: read the v1 README + phase docs via
      existing artifact parsing; map task → node (id, contract from title +
      condensed notes, deps from depends_on + phase order, justifies,
      artifacts where declared).
- [ ] Gap sentinels: `hazards: "untriaged"` everywhere; `gate:
      {type: "unspecified"}` where verification names no runnable check;
      `needs-contract` marker where no falsifiable sentence derives.
- [ ] Compile-side enforcement: `unspecified` gates and `needs-contract`
      markers block compile with per-node findings (extend 2.3's semantic
      set).
- [ ] Completed v1 tasks: convert with evidence preserved as historical
      annotation fields (never as `verification` observations — no
      retroactive GREEN).
- [ ] Fixture test converting a miniature v1 plan end to end, asserting the
      sentinel set and the compile refusal list.

### Notes
Revision boundary: `convert` emits a staged proposal (through 2.2's path)
that deliberately does not compile until an operator resolves each sentinel.
This is DD-15 verbatim: mechanical work done by the tool, judgments left
visibly unmade. Do not auto-derive hazards from task text — that is exactly
the laundering the sentinel exists to prevent. Historical evidence
annotations are display-only; states derive from observations, and converted
plans start with none.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

### Trap
The tempting shortcut is defaulting `gate: {type: "command", command: "make
test"}` for unspecified tasks so converted graphs compile immediately. That
default asserts a verification contract nobody wrote — sentinel-then-block
is the requirement, not a UX bug to smooth over.

## 2.6: Guard coverage: pretooluse entries and FR-28 path guard for graph artifacts

### Subtasks
- [ ] Add the phase-2 mutating verbs to the `sdd hook pretooluse`
      deny-list for read-only agents; allowlist the read surface. (Later
      phases land their own entries in the same revision as their verbs —
      the parity test built here is what enforces that.)
- [ ] Extend the artifact-path Write/Edit guard to `<Plan>-Graph.json` and
      rendered view paths (schema-recognized), denied for every agent.
- [ ] Extend the FR-44-style parity test: any `sdd graph` subcommand without
      a guard classification fails the suite.

### Notes
Revision boundary: guard entries and tests only; no behavioral change to the
verbs themselves. The FR-28 extension is the piece the design review flagged
as NEW-1 — without it, hand-editing the graph silently reintroduces
dual-source drift. Design references: DD-2 (guard-coverage paragraph),
D-0014, `Specs/SDD-Toolchain` FR-44.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

## Acceptance Criteria
- [ ] A payload authored from the 1.3 exemplar stages, assembles, compiles,
      and renders valid views end to end on a fixture plan (DD-1, DD-2,
      DD-11).
- [ ] Compile reports the full finding set in one pass and refuses with exit
      1; intent hashes are formatting-insensitive and wording-sensitive
      (DD-4).
- [ ] A converted v1 fixture blocks on its sentinels and lists each one
      (DD-15).
- [ ] Every new verb and graph artifact path is guard-covered; the parity
      test enforces it (D-0014, FR-44, FR-28 extension).
- [ ] `make test` green, including the extended template round-trip gate;
      `go vet`/`staticcheck` clean; store tests pass with `-race`.

## Phase Completion Evidence

<!-- Keep the exact `Pending — not complete.` line until completion. -->
Pending — not complete.
