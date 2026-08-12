---
title: "Phase review: Schema And Compiler"
type: review
status: resolved
created: 2026-08-11
updated: 2026-08-11
tags: [review]
related: ["Plans/SDD-Toolchain/06-Schema-And-Compiler.md"]
review_of: "Plans/SDD-Toolchain/06-Schema-And-Compiler.md"
rev: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "381adda6c225688563de873a26d2ea21d01809fb"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
    evidence: "Six tasks, all mapped: 6.1/6.2 to 687104f, 6.3/6.4 to 7159b5b, 6.5 to 923a35f, 6.6 verified against the shipped CLI. No work outside the declared task set."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
    evidence: "Read cmd/sdd/template.go and internal/compile/frontmatter.go. The drift check compares declared structure rather than bytes, so templates keep their authoring guidance; the frontmatter renderer re-parses what it is about to write into a strict map and refuses rather than mangling a placeholder."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
    evidence: "Checked FR-14/15 (schema as data), FR-22 (section-scoped writes), FR-21 (lifecycle gates), FR-29 (error messages). Gates run the same rules sdd validate runs; refusals name an artifact path and line."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
    evidence: "Inspected without plan context. The template drift check found two real schema gaps on its first run — plan-phase declared neither deliverable nor tasks, both of which the validator has always required. The check would have caught those whenever it was added."
findings:
  - id: F-01
    severity: major
    title: "Sibling phases closing on one shared range trip SDD173"
    status: rejected
followups: []
---

# Phase review: Schema And Compiler

Reviewed `Plans/SDD-Toolchain/06-Schema-And-Compiler.md` at frozen identity `bc3383502115b7fd2160ec20169f2998c402bf7b..0acca6756ce07b83f4df2f987ac56ef55b40178e`.

## Findings

### F-01 — Sibling phases closing on one shared range trip SDD173

Closing seven phases against one frozen range makes each phase see the other
six phases' lifecycle documents as material change. `phaseLifecyclePaths`
permits only the phase's own doc, its review, the plan README, and its
debrief — a faithful port of the Python, which assumed phases close one at a
time.

The same shared range trips SDD173's other branch: a phase review endpoint
must equal that phase's own checkpoint, and one range reviewing seven phases
can only end at one of them. Both branches are the same limitation seen from
two sides.

Every flagged path is a lifecycle document under the planning root; no source
file is implicated. The diagnostic is correct about what changed and wrong
about what it means, because the rule has no notion of sibling phases closing
together.

Rejected rather than deferred: loosening the rule to make this workflow pass
would weaken a gate that is right in the normal case, and no future task is
planned to change it. Recorded so the constraint is visible to anyone closing
phases in a batch.

## Resolution Log

### F-01 — rejected

2026-08-11. The rule is correct per-phase. Closing a batch of phases against
one shared range is outside what it models, and the right response is to know
that rather than to relax the check.

