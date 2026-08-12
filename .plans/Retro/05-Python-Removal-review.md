---
title: "Phase review: Python Removal"
type: review
status: resolved
created: 2026-08-11
updated: 2026-08-11
tags: [review]
related: ["Plans/SDD-Toolchain/05-Python-Removal.md"]
review_of: "Plans/SDD-Toolchain/05-Python-Removal.md"
rev: "bc3383502115b7fd2160ec20169f2998c402bf7b..ca638666693988006cd3303a8f3df4d2797f5f8b"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "ca638666693988006cd3303a8f3df4d2797f5f8b"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..ca638666693988006cd3303a8f3df4d2797f5f8b"
    evidence: "All four tasks map to 539432d (deletion) and d547943 (measurements). scripts/, tests/, and requirements.txt are gone; no Python remains outside tools/parity, bump-version.py, and the retained guard oracle."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..ca638666693988006cd3303a8f3df4d2797f5f8b"
    evidence: "Read the Makefile after the change. make test now runs parity and check-templates before the Go suite, so template and ordering drift fail the build. The venv bootstrap is gone; the harness is stdlib-only."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..ca638666693988006cd3303a8f3df4d2797f5f8b"
    evidence: "Checked NFR-07 against measurement: 20 ms median where the spec's provisional bound was 300 ms, now set to 50 ms with headroom. Payload arithmetic measured at 21.9 MB across five targets against a ~50 MB estimate."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..ca638666693988006cd3303a8f3df4d2797f5f8b"
    evidence: "Inspected without plan context. Deleting the oracle was gated on freezing it first, so the corpus still reports 491 matched with the validators gone. D-0015 was reaffirmed rather than reopened: the size measurement falsifies only the payload argument, and reversing an accepted entry needs user reconciliation."
findings:
  - id: F-01
    severity: major
    title: "Sibling phases closing on one shared range trip SDD173"
    status: rejected
followups: []
---

# Phase review: Python Removal

Reviewed `Plans/SDD-Toolchain/05-Python-Removal.md` at frozen identity `bc3383502115b7fd2160ec20169f2998c402bf7b..0acca6756ce07b83f4df2f987ac56ef55b40178e`.

## Findings

### F-01 — Sibling phases closing on one shared range trip SDD173

Closing seven phases against one frozen range makes each phase see the other
six phases' lifecycle documents as material change. `phaseLifecyclePaths`
permits only the phase's own doc, its review, the plan README, and its
debrief — a faithful port of the Python, which assumed phases close one at a
time.

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

