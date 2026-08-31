---
title: "Phase review: Payload Schema and Templates"
type: review
status: resolved
created: 2026-08-31
updated: 2026-08-31
tags: [review]
related: ["Plans/SddGraph/01-Payload-Schema-And-Templates.md", "Designs/SddGraph"]
review_of: "Plans/SddGraph/01-Payload-Schema-And-Templates.md"
rev: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..fcb35759af8c456ac3e8df43375ba38f9d9d94e6"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "6dc832a46d5bb5597c1817ad29e5d18ee015977e"
review_mode: single-agent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..fcb35759af8c456ac3e8df43375ba38f9d9d94e6"
    evidence: "Implementation commits e37df49 (1.1), ed53984 (1.2), 7483157 (1.3) traced subtask-by-subtask against the phase doc: all 13 subtasks implemented as written; verification commands in each task's evidence were actually run this session with recorded output; meta-registries (subcommand list, handler flag table) updated deliberately as the repo discipline requires; no scope beyond phase 1 — no store, compile, or walk code present."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..fcb35759af8c456ac3e8df43375ba38f9d9d94e6"
    evidence: "Read all three new packages and the CLI diff in full at c59170f..fcb3575 (single-agent mode: reviewer carries implementation context; intent-blindness degraded and disclosed). Error-path quality is the contract and holds: batched DecodeErrors with JSON paths and did-you-mean, deterministic map walks via sortedKeys, table-driven tests covering every rejection branch; Gate.MarshalJSON documents why omitempty cannot express the full-lanes form; no dead code, doc-comments state the design rules they enforce."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..fcb35759af8c456ac3e8df43375ba38f9d9d94e6"
    evidence: "Diff checked against Designs/SddGraph obligations: DD-3 (no derivable state persisted anywhere in the shapes), DD-12 (strict decoding with unknown-key rejection; skeleton and schema single-sourced and CI-gated: exemplar round-trips through DecodeProposal, schema cross-checked against model.KeySets, gate-type constants, sentinels, required lists), DD-9 (closed vocabulary of ten hazards, each with a required test shape), FR-18 posture mirrored (tool-owned payload fields refused loudly). No plan/phase markdown compiler work (FR-36 supersession respected)."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..fcb35759af8c456ac3e8df43375ba38f9d9d94e6"
    evidence: "Probed hostile-input surfaces: deep nesting (stdlib depth cap errors cleanly), trailing content rejected, rune-safe did-you-mean, exact-integer enforcement. Two real gaps found, both non-blocking at this layer and tracked as deferred findings F-01 and F-02 with followups FU-01 (compile-side duplicate-node-id check, task 2.3) and FU-02 (token-walk duplicate-key detection, task 4.3)."
findings:
  - id: F-01
    severity: minor
    title: "Model layer accepts duplicate node ids within one document; NodeByID is first-wins"
    status: deferred
  - id: F-02
    severity: minor
    title: "encoding/json duplicate-key last-wins silently weakens strict decoding for repeated keys in one object"
    status: deferred
followups:
  - id: FU-01
    finding: F-01
    summary: "Add duplicate-node-id detection to compile's batched semantic findings alongside dangling deps and cycles"
    tracked_in: "2.3"
  - id: FU-02
    finding: F-02
    summary: "Detect duplicate keys within one JSON object via a token-walk pass when hardening the decoder for hostile input"
    tracked_in: "4.3"
---

# Phase review: Payload Schema and Templates

Reviewed `Plans/SddGraph/01-Payload-Schema-And-Templates.md` at frozen identity `c59170f34737eb905844d5b88a1e4cb3e0a21aec..fcb35759af8c456ac3e8df43375ba38f9d9d94e6`.

## Findings

### F-01 — Model layer accepts duplicate node ids within one document

`DecodeGraph`/`DecodeProposal` accept a document declaring two nodes with the
same id; `NodeByID` returns the first. The model layer checks shape, not
graph semantics, so this is a boundary question rather than a defect — but
nothing downstream refuses it yet either. Surfaced by the blind-spots lane.

### F-02 — encoding/json duplicate-key last-wins weakens strict decoding

Go's `encoding/json` keeps the last value when one object repeats a key,
silently. A payload carrying two `contract` keys for one node decodes without
a finding, which weakens the strict-decoding guarantee DD-12 states.
Detecting it requires a token-walk duplicate check the current raw-map pass
cannot see. Surfaced by the blind-spots lane.

## Resolution Log

### F-01 — deferred

2026-08-31 — deferred to FU-01, tracked in plan task `2.3`: duplicate node
ids are a semantic finding and belong in compile's batched semantic set
(with dangling deps, cycles, untriaged hazards), where fragment-merge
collisions (2.2) are already specified. Non-blocking here: no committed
graph exists yet, and the compile gate lands before any payload reaches a
store.

### F-02 — deferred

2026-08-31 — deferred to FU-02, tracked in plan task `4.3`: the
duplicate-key check belongs with the decoder-hardening/fuzz work, which
already owns the hostile-input surface of the same code. Non-blocking here:
the failure mode requires a malformed hand-built payload, the
template/propose flow never produces one, and the consequence is a decode
the author can diff, not silent state corruption.
