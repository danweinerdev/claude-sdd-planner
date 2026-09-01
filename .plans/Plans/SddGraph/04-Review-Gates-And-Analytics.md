---
title: "Review Gates and Analytics"
type: phase
plan: "SddGraph"
phase: 4
status: in-progress
created: 2026-08-31
updated: 2026-09-01
deliverable: "Feature-scoped review gates with derived scope, the closed predicate, finding-driven demotion, and the graph analytics surface (path, risk, shape, export) plus fuzz hardening of external-input parsers"
tasks:
  - id: "4.1"
    title: "Review gates: derived scope, closed predicate, finding demotion"
    status: complete
    verification: "go test ./internal/graph/... -run TestReviewGate -count=1 — scope(A) derives as A's dependency closure minus nodes inside an earlier frozen full gate's scope (nested-gates fixture proves no diff is reviewed twice); a review gate greens only from a persisted review artifact that is resolved, frozen: true, and verdict Aligned (D-0020 wiring through the existing sdd review scaffold/resolve flow), bound to the digest of the scope's aggregate diff with VCS-native provenance; a drifted scope diff derives the gate STALE; recording a resolved review whose findings name scope nodes writes failing observations against each named node (they derive RED, re-enter workable) and dependants of the demoted nodes go STALE by seq after rework re-verifies; lanes: full vs named subset, only full carries closure; the closed predicate derives as GREEN and inside a GREEN frozen full gate's scope with matching diff digest; rendered views project assumed-closed vs closed distinctly and 2.4's frozen-view refusal now keys on the closed predicate."
    justifies: "DD-9 (feature-scoped tiered reviews, two-axis closure, mechanical reopen), D-0022 (completion-grade closure), D-0020 (freeze-at-resolve inherited unchanged). Prevents both failure modes the design names: uniform review weight at graph granularity, and a faulted node still reading GREEN between finding and fix."
  - id: "4.2"
    title: "Graph analytics: path, risk, shape, status, show, export"
    status: planned
    verification: "go test ./internal/graph/algorithms/... ./cmd/sdd/ -run TestGraphAnalytics -count=1 — path reports critical-path length, total estimate, and speedup ceiling on fixtures with known answers; risk reports cut vertices (articulation points) on a fixture with a known waist; shape classifies FLAT, CHAIN, FUNNEL, HOURGLASS, MIXED on one fixture each from the depth histogram; status summarizes state counts and per-node lines; show prints one node's full record; export emits mermaid, dot, plan (flat ordered reading view), and shape formats; every subcommand supports --json; algorithms package imports neither store nor states (pure structure); next's critical-path marking (3.2) now delegates to algorithms."
    justifies: "DD-14 (analytics as first-class review inputs: cut vertices aim review attention, silhouette diagnoses decomposition, ceiling prices parallelism). Prevents decomposition quality staying an aesthetic judgment."
    depends_on: ["4.1"]
  - id: "4.3"
    title: "Fuzz targets for payload decoder and report parsers"
    status: planned
    verification: "go test ./internal/graph/... -run TestFuzzCorpus -count=1 plus go test ./internal/graph/model/ -fuzz FuzzDecode -fuzztime 60s and go test ./internal/graph/sync/ -fuzz FuzzJUnit -fuzztime 60s -run xxx locally documented in the task notes: no panics, no hangs, structured errors only, on arbitrary payload JSON, JUnit XML, and go-test-json streams; discovered crashers added to testdata corpora and fixed within this task."
    justifies: "Design § Structural Verification names these parsers as hostile-external-input consumers by design. Prevents a malformed CI report or hand-mangled payload crashing the tool that owns the committed graph."
    depends_on: ["4.1"]
---

# Phase 4: Review Gates and Analytics

## Overview

Completes the honesty model and the navigation surface. Review gates make
heavyweight completion feature-scoped: scope derives from the graph, the
existing four-lane review flow populates the gate's observation, findings
demote scope nodes mechanically, and the *closed* predicate (GREEN + covered
by a GREEN frozen full gate) becomes the completion-grade truth that rendered
views project and frozen-view refusal keys on. Analytics turn graph structure
into printed diagnoses. Fuzzing hardens every parser that eats external
input.

## 4.1: Review gates: derived scope, closed predicate, finding demotion

### Subtasks
- [x] Scope derivation in `internal/graph/states` (or a sibling
      `internal/graph/review`): dependency closure minus earlier frozen full
      gates' scopes; property test on nested-gate fixtures (disjoint
      incremental scopes, union covers the closure).
- [x] Gate observation source: wire to the persisted review artifact
      produced by the existing `sdd review scaffold` → `resolve` flow —
      gate reads `resolved` + `frozen: true` + verdict `Aligned`; no new
      review mechanism.
- [x] Aggregate-diff digest: union of scope nodes' artifact changes, hashed
      with the DD-6 anchor rules; VCS-native reference (git range / p4 CL +
      opened files) recorded as provenance; drift ⇒ gate STALE via ordinary
      digest staleness.
- [x] Finding demotion: a resolution whose findings map names scope nodes
      records failing observations against them through the sync path
      (RED, workable again); demotion happens as part of recording the
      review, not as agent courtesy.
- [x] Lanes: `full` (all four) vs named subset; only `full` participates in
      the closed predicate; compile's coverage invariant (2.3) already
      requires full-gate coverage — extend its tests to lane awareness.
- [x] Closed predicate + projections: rendered views distinguish
      assumed-closed from closed; 2.4's frozen-view refusal stub upgraded to
      key on closed (TODO from 2.4 resolved).
- [x] Guard entries for any new verb surface (e.g., the review-recording
      hook) in the same revision.

### Notes
Revision boundary: review gates green mechanically from frozen Aligned
review artifacts, findings reopen work mechanically, and *closed* exists as
a derived predicate consumed by views and refusal — the full DD-9 mechanics
in one revision because they are one contract (scope, evidence, demotion,
closure are mutually defining). Design references: DD-9, D-0020, D-0022;
§ Node states (closure axis); § Error Handling (INTENT-STALE vs demotion
distinction). The p4 review surface is the CL diff — provenance capture from
3.3 already supplies it.

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `6e5ae294e0a63a34d764fb3527dcf80f262a25db`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `6e5ae294e0a63a34d764fb3527dcf80f262a25db`
- Focused review: `git show 6e5ae294e0a63a34d764fb3527dcf80f262a25db`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `6e5ae294e0a63a34d764fb3527dcf80f262a25db`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./... -count=1` | `.` | PASS (`exit 0`) | `full sweep green including the new internal/graph/review package (scope disjointness/union property on nested-gate fixtures incl. three-level transitivity; three-signal refusal batched and naming each failing signal; lane conformance full vs named subset; aggregate scope-artifact digests on the gate observation with drift deriving STALE; demotion seq-stamped below the gate obs, RED+workable, gate seq-stale on rework, out-of-scope names refused citing the finding id; closed predicate incl. gate self-closure and stale-gate closure withdrawal; claim discipline with merge-on-record), states review-gate digest extension table test, compile lane-vocabulary refusal + subset-no-coverage test, frozen-view lifecycle test (freeze, no-op, refusal, explicit-delete escape), goldens regenerated with closure projections; go vet and staticcheck clean` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `CLI smoke in a temp git repo` | `built binary at 6e5ae29: compile impl+fgate, green impl, then the review verb` | PASS | `unfrozen artifact refused naming the frozen signal; frozen Aligned recorded — gate pass seq 2, scope [impl], states GREEN:2; finding naming impl demoted it (fail seq 3 < gate seq 4) with aggregate digests recorded` |

### Trap
You will want the gate to green from the review's `status: resolved` alone.
D-0020's whole point is that freezing happens at resolve *atomically with
the SDD167 check* — read all three signals (resolved, frozen, Aligned) from
the artifact and bind to the diff digest, or a re-opened review would leave
a stale gate GREEN.

## 4.2: Graph analytics: path, risk, shape, status, show, export

### Subtasks
- [ ] `internal/graph/algorithms`: longest path with estimate weights
      (critical path + total estimate + speedup ceiling), articulation
      points (cut vertices), depth histogram + silhouette classification
      (FLAT / CHAIN / FUNNEL / HOURGLASS / MIXED) — pure functions over
      structure, no store/state imports.
- [ ] CLI verbs: `graph path`, `graph risk`, `graph shape`, `graph status`,
      `graph show <id>`, `graph export --format mermaid|dot|plan|shape`,
      all with `--json`.
- [ ] Wire `next`'s critical-path-first ordering (3.2's minimal helper) to
      the real implementation.
- [ ] Known-answer fixtures per algorithm and per silhouette class.
- [ ] Guard entries: all analytics verbs read-only-allowlisted.

### Notes
Revision boundary: the complete read-only analytics surface. Keep
`algorithms` dependency-free (the design's component table: "knows nothing
of state or disk") so 2.3's cycle detection and this task share one
implementation. Export formats are presentation only — no information
beyond graph + derived state. Design references: DD-14, § Interfaces.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

## 4.3: Fuzz targets for payload decoder and report parsers

### Subtasks
- [ ] `FuzzDecode` over the strict model decoder (arbitrary JSON bytes).
- [ ] `FuzzJUnit` and `FuzzGoTestJSON` over the sync parsers (arbitrary
      XML/JSON-stream bytes).
- [ ] Seed corpora from real reports (this repo's own `go test -json`
      output; a JUnit sample from a common emitter).
- [ ] CI-friendly regression mode: corpus replay as ordinary tests; the
      `-fuzz` exploration commands documented here for local runs.
- [ ] Fix every discovered crasher in this task; add each to the corpus.

### Notes
Revision boundary: fuzz harnesses + corpus + any crash fixes; no functional
change otherwise. Structured errors are the contract — a parser may reject,
never panic, on any input, because sync consumes files produced by arbitrary
CI systems. Design references: § Structural Verification.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

## Acceptance Criteria
- [ ] A two-gate nested fixture reviews incrementally (no re-reviewed diff),
      greens only from frozen Aligned artifacts, and demotes named nodes to
      RED on findings (DD-9, D-0020).
- [ ] *Closed* derives correctly and drives both view projection and
      frozen-view refusal (D-0022).
- [ ] `path`/`risk`/`shape` return known answers on fixtures; every
      silhouette class classified; exports render (DD-14).
- [ ] Corpus-replay fuzz tests green; no parser panics on hostile input.
- [ ] Guard entries cover the phase's verbs; `make test` green; `go vet`/
      `staticcheck` clean.

## Phase Completion Evidence

<!-- Keep the exact `Pending — not complete.` line until completion. -->
Pending — not complete.
