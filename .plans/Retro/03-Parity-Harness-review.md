---
title: "Phase review: Parity Harness"
type: review
status: resolved
created: 2026-08-11
updated: 2026-08-11
tags: [review]
related: ["Plans/SDD-Toolchain/03-Parity-Harness.md"]
review_of: "Plans/SDD-Toolchain/03-Parity-Harness.md"
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
    evidence: "All four tasks map to commits in range: 3739e84 froze the oracle (3.1, 3.3), 9954b57 recorded exemptions and wired the gate into make test (3.2, 3.4). 3.2's AST-scan subtask was reached by a different route — frozen output per code — because the validators it would scan were deleted first."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
    evidence: "Read tools/parity/freeze.py and parity.py. Freeze reuses parity's own prepare() so SETUP and {{REPO}} roots freeze identically to how they validate; frozen entries record identity and severity but not message text, since two codes interpolate CPython and PyYAML strings."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
    evidence: "Checked FR-06/07/32 (differential oracle as the acceptance test). 128 roots, 491 matched diagnostics, zero extra in both live and frozen modes; allow-missing.txt and allow-message-drift.txt carry no SDD or DLG code."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "bc3383502115b7fd2160ec20169f2998c402bf7b..381adda6c225688563de873a26d2ea21d01809fb"
    evidence: "Inspected without plan context. The frozen lookup keys on the exact manifest-relative path after an earlier basename fallback silently matched roots against other rules' expectations, producing 11 phantom extras. EXEMPTIONS.md names the two codes with no frozen output rather than leaving the gap unstated."
findings:
  - id: F-01
    severity: major
    title: "Sibling phases closing on one shared range trip SDD173"
    status: rejected
followups: []
---

# Phase review: Parity Harness

Reviewed `Plans/SDD-Toolchain/03-Parity-Harness.md` at frozen identity `bc3383502115b7fd2160ec20169f2998c402bf7b..0acca6756ce07b83f4df2f987ac56ef55b40178e`.

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

