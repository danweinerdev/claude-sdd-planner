---
title: "Phase review: Graph Store, Compiler, Convert"
type: review
status: resolved
created: 2026-08-31
updated: 2026-08-31
tags: [review]
related: ["Plans/SddGraph/02-Graph-Store-Compiler-Convert.md", "Designs/SddGraph"]
review_of: "Plans/SddGraph/02-Graph-Store-Compiler-Convert.md"
rev: "9c1fbdaba6e650df3fa937dfd2e57f8bb76675ef..39a31750362225d3ce885477f68621a70f470eac"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "39a31750362225d3ce885477f68621a70f470eac"
review_mode: single-agent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "9c1fbdaba6e650df3fa937dfd2e57f8bb76675ef..39a31750362225d3ce885477f68621a70f470eac"
    evidence: "Implementation commits 085f4f1 (2.1), b011057 (2.2), fe2b76e (2.3), 4f9e981 (2.4), 10db754 (2.5), b2281b8 (2.6) traced subtask-by-subtask: all 30 subtasks implemented; recorded deviations are deliberate and noted in the phase doc (goldens under package testdata; README surgical edits with mixed-plan merge deferred to conversion; convert gates universally unspecified per the task Trap; -race deferred to 3.6 for want of a cgo toolchain). FU-01 from the phase-1 review landed in 2.3 as promised (duplicate-node-id findings)."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "9c1fbdaba6e650df3fa937dfd2e57f8bb76675ef..39a31750362225d3ce885477f68621a70f470eac"
    evidence: "Read the full phase-2 range: store (CAS over WriteAtomicExpecting, locked reads), propose/assemble (atomic refusals, UUIDv7 staging order), compile (batched findings with deterministic ordering, fingerprint embedding, consume-after-durable-write), render (marker-guarded views, date-stable idempotence), convert (mechanical mapping, universal sentinels), guards (single-source classifications). Single-agent mode: reviewer carries implementation context, disclosed. Error text quality is consistent — every refusal names the fix; deterministic walks throughout; no dead code found."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "9c1fbdaba6e650df3fa937dfd2e57f8bb76675ef..39a31750362225d3ce885477f68621a70f470eac"
    evidence: "Range checked against Designs/SddGraph obligations: DD-3 (structure+observations only; store reuses internal/store, no second lock), DD-11 (declarative batched construction; mutations via locked verbs), DD-4 (coverage as exit code; defined hash input with one shared normalizer), DD-9 (coverage invariant, never auto-inserted), DD-15 (convert marks every judgment, defaults nothing, history never grants GREEN), DD-2 (views are projections; FR-28 extension denies hand edits to the committed graph), D-0014/FR-44 (deliberate classification enforced against the real command tree). No FR-36-violating plan/phase compiler work."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "9c1fbdaba6e650df3fa937dfd2e57f8bb76675ef..39a31750362225d3ce885477f68621a70f470eac"
    evidence: "Probed refusal atomicity (stage/assemble/compile/render all leave state untouched on refusal), guard bypass surfaces (bare and unknown graph sub-verbs deny; flag-stripped next --claim caught; Graph.json JSON path covered), and the value-model traps (string-typed frontmatter ints found and fixed in convert). One real wrinkle found and deferred as F-01: a crash in the window between compile's graph write and payload consumption leaves landed nodes plus a staged payload, and the next compile refuses per-node with already-exists findings requiring manual fragment removal — tracked into task 3.5 (gc)."
findings:
  - id: F-01
    severity: minor
    title: "A crash between compile's graph write and payload consumption strands a staged payload whose nodes already landed"
    status: deferred
followups:
  - id: FU-01
    finding: F-01
    summary: "Teach gc (or compile itself) to detect and reap a staged payload whose nodes all exist in the graph, naming what it removed"
    tracked_in: "3.5"
---

# Phase review: Graph Store, Compiler, Convert

Reviewed `Plans/SddGraph/02-Graph-Store-Compiler-Convert.md` at frozen identity `9c1fbdaba6e650df3fa937dfd2e57f8bb76675ef..39a31750362225d3ce885477f68621a70f470eac`.

## Findings

### F-01 — A crash between graph write and payload consumption strands the staged payload

Compile's success path is: append nodes under the store's compare-and-swap,
render views, then remove the staged payload. Render preflight runs before
the graph write, so the remaining failure window is I/O-only — but a crash
inside it leaves the nodes landed and the payload still staged, and the next
compile refuses per node with already-exists findings until the operator
removes the fragment by hand. Real, rare, and self-announcing rather than
silent. Blind-spots lane.

## Resolution Log

### F-01 — deferred

2026-08-31 — deferred to FU-01, tracked in plan task `3.5`: gc (or compile
itself) learns to detect a staged payload whose nodes all already exist in
the graph and reap it, naming what it removed. Non-blocking here: the state
is loud (every finding names the collision), the remedy is one file
deletion, and no data is lost — the graph write was durable by construction.
