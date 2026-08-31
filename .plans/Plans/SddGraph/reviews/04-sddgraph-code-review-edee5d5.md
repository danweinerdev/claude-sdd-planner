---
title: "Phase review: Payload Schema and Templates"
type: review
status: resolved
created: 2026-08-31
updated: 2026-08-31
tags: [review]
related: ["Plans/SddGraph/01-Payload-Schema-And-Templates.md", "Designs/SddGraph"]
review_of: "Plans/SddGraph/01-Payload-Schema-And-Templates.md"
rev: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..edee5d595b069bb5a76505c9fc611bfac2df1efd"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "edee5d595b069bb5a76505c9fc611bfac2df1efd"
review_mode: single-agent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..edee5d595b069bb5a76505c9fc611bfac2df1efd"
    evidence: "Commits e37df49, ed53984, 7483157 traced subtask-by-subtask against phase-1 tasks 1.1-1.3: all 13 subtasks implemented as written with recorded verification output. Post-review commits 31199e7 and edee5d5 are gate fixes and review-artifact remediation discovered while closing this phase — in-scope corrective work for the phase gate itself, not silent scope growth; no store, compile, or walk code exists in the range."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..edee5d595b069bb5a76505c9fc611bfac2df1efd"
    evidence: "Read the full range c59170f..edee5d5: internal/graph/{model,hazards,proposal}, cmd/sdd graph/template wiring, and the post-review gate fixes (candidateArtifactErrors, the resolve freeze gate, hermetic review fixtures). Single-agent mode: reviewer carries implementation context, intent-blindness degraded and disclosed. Error-path quality holds throughout: batched JSON-path findings with did-you-mean, deterministic walks, table-driven rejection coverage; the freeze gate reuses RunWithWaivers rather than growing a second validity opinion."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..edee5d595b069bb5a76505c9fc611bfac2df1efd"
    evidence: "Range checked against Designs/SddGraph obligations: DD-3 (no derivable state persisted), DD-12 (strict decoding; skeleton and schema single-sourced, cross-checked against model.KeySets, constants, sentinels, required lists), DD-9 (closed ten-hazard vocabulary with required test shapes), FR-18 posture (tool-owned payload fields refused). The gate fixes strengthen D-0020 conformance: a phase review can no longer freeze while the validator rejects it. No FR-36-violating plan/phase compiler work anywhere in the range."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "c59170f34737eb905844d5b88a1e4cb3e0a21aec..edee5d595b069bb5a76505c9fc611bfac2df1efd"
    evidence: "Probed hostile-input surfaces (deep nesting errors cleanly, trailing content rejected, rune-safe did-you-mean, exact-integer enforcement) and the new gate paths (refusal leaves the artifact unwritten; ordinary reviews exempt from the freeze gate because they stay repairable). Two gaps remain tracked as deferred findings F-01 and F-02 with followups FU-01 (compile-side duplicate-node-id check, task 2.3) and FU-02 (token-walk duplicate-key detection, task 4.3)."
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

Reviewed `Plans/SddGraph/01-Payload-Schema-And-Templates.md` at frozen identity `c59170f34737eb905844d5b88a1e4cb3e0a21aec..edee5d595b069bb5a76505c9fc611bfac2df1efd`.

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
