---
title: "Phase review: Execution Loop"
type: review
status: resolved
created: 2026-09-01
updated: 2026-09-01
tags: [review]
related: ["Plans/SddGraph/03-Execution-Loop.md", "Designs/SddGraph"]
review_of: "Plans/SddGraph/03-Execution-Loop.md"
rev: "55c51cbb76d40ff19f88b67f40f9dcd99a1c8867..55f24cf20c1bb2bb1911a0cf1cc38a84e31359e8"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "55f24cf20c1bb2bb1911a0cf1cc38a84e31359e8"
review_mode: single-agent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "55c51cbb76d40ff19f88b67f40f9dcd99a1c8867..55f24cf20c1bb2bb1911a0cf1cc38a84e31359e8"
    evidence: "Implementation commits e5e9ed8 (3.1), f6fe384 (3.2), 7629a68 (3.3), 0cec5cb (3.4), 027be4a (3.5), e5b0f92 (3.6), 55f24cf (phase-close AC-4 test) traced subtask-by-subtask: all 34 subtasks implemented. Recorded deviations are deliberate and noted in evidence: -race executed on Linux via WSL (Windows host has no cgo toolchain — the subtask's own 'CI-conditional where needed' posture); 3.6's four production fixes were pre-authorized by the task's fix-forward clause for bugs the stress harness finds; the forced-1 git test landed after 3.6 completion as test-only AC closure inside the phase range. FU-01 from review 05 landed in 3.5's gc as promised (stale-payload reaping, tested)."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "55c51cbb76d40ff19f88b67f40f9dcd99a1c8867..55f24cf20c1bb2bb1911a0cf1cc38a84e31359e8"
    evidence: "Read the full phase-3 range (~4.3k lines across states, digest, claims, provider, sync, ops, CLI verbs): states are pure derivation over persisted structure+observations (no stored state anywhere); claims confirm-then-allocate with exact-lease rollback so no concurrent expiry/reclaim is ever clobbered; sync's merge preconditions gate the RECORDING of a pass, so dependants never unblock on unproven work; split gates on an introduced-findings diff against the compiler itself; gc verifies directories are actually gone rather than trusting provider no-ops. Refusal texts uniformly name the fix (red-before-green names the unproven tests; dirty-worktree names the revision-anchor rule). Single-agent mode: reviewer carries implementation context, disclosed. Three staticcheck U1000s in cmd/sdd predate the range (untouched files) — logged as F-02/FU-02, not a range defect."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "55c51cbb76d40ff19f88b67f40f9dcd99a1c8867..55f24cf20c1bb2bb1911a0cf1cc38a84e31359e8"
    evidence: "Range checked against Designs/SddGraph obligations: DD-5/D-0022 (completion only via synced observations; red-before-green refuses a never-failed hazard-discharging test at merge; no assert-pass path — asserted isolation is refused by default and never merges), DD-6 (digest+seq anchoring load-bearing, VCS provenance supplementary; a pass from a dirty worktree refuses because the anchor must name the tested bytes), DD-7 (isolation = no unverified foreign edits; worktrees clean by construction; shared-dirty records provisionally and derives STALE, never GREEN), DD-8 (parallelism is provider capacity: git N-way and forced-1 floor both proven, p4 single-CL clean by construction, plain digest-only), DD-10 (claims/leases not locks: double-claim prevention structural via CAS, takeover CAS-serialized, late sync refused by claim discipline), DD-13 (split is the stopping rule's remedy, retired ids append-only and never reused), DD-3 (only structure and observations persist; every state derived at read)."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "55c51cbb76d40ff19f88b67f40f9dcd99a1c8867..55f24cf20c1bb2bb1911a0cf1cc38a84e31359e8"
    evidence: "The 3.6 stress harness was built as this lane's instrument and forced out four real bugs, all fixed in-range: the gc-vs-claim TOCTOU (allocation preceding the claim record — closed by confirm-then-allocate), gc never persisting lease expiry (dead claimants' workspaces unreapable forever), gc's reap silently no-op on plain trees (provider Release no-ops trusted), and deterministic claim branches colliding on legitimate post-gc reclaims. Remaining probed windows are documented and safe: a crash between claim confirm and allocation leaves a claim that expires naturally and a reap that no-ops; a crash between sync's merge write and workspace release leaves an orphan worktree gc reaps. Cross-process coverage on both flock semantics (Windows native x3, Linux/WSL x2 under -race); zero double-claims, zero torn reads. One wrinkle deferred as F-01: per-allocation claim branches accumulate unbounded."
findings:
  - id: F-01
    severity: minor
    title: "Per-allocation claim branches (graph/<node>-<hex4>) accumulate unbounded; nothing prunes them"
    status: deferred
  - id: F-02
    severity: minor
    title: "Three pre-existing dead usage consts in cmd/sdd flagged by staticcheck (predate this range)"
    status: deferred
followups:
  - id: FU-01
    finding: F-01
    summary: "Decide a branch retention/pruning policy once the self-hosting pilot shows real volume — candidates: gc prunes branches of merged nodes whose provenance revision is reachable from the mainline, or a plan-completion sweep"
    tracked_in: "5.4"
  - id: FU-02
    finding: F-02
    summary: "Delete the dead evidenceUsage/reviewUsage/transitionUsage consts (U1000) next time cmd/sdd is materially touched"
    tracked_in: "4.1"
---

# Phase review: Execution Loop

Reviewed `Plans/SddGraph/03-Execution-Loop.md` at frozen identity `55c51cbb76d40ff19f88b67f40f9dcd99a1c8867..55f24cf20c1bb2bb1911a0cf1cc38a84e31359e8`.

## Findings

### F-01 — Per-allocation claim branches accumulate unbounded

Every claim allocates a uniquely-suffixed branch (`graph/<node>-<hex4>`) so
merged or crashed work stays reachable after its worktree is released, and a
post-gc reclaim can never collide with a predecessor's surviving branch.
The flip side is that nothing ever deletes these branches: a long walk with
crashes and re-claims litters the repository with graph branches whose
commits are already reachable from the mainline after merge. Harmless to
correctness (append-only history is the design posture), but repository
hygiene degrades with walk length. Blind-spots lane.

### F-02 — Pre-existing dead usage consts in cmd/sdd

`staticcheck ./cmd/sdd/` reports U1000 for `evidenceUsage` (evidence.go),
`reviewUsage` (review.go), and `transitionUsage` (transition.go). All three
predate this phase's range — the files are untouched by it (verified against
the range diff) — so this is not a range defect, but the quality lane's
sweep surfaced it and it should not be lost. Quality lane.

## Resolution Log

### F-01 — deferred

2026-09-01 — deferred to FU-01, tracked in plan task `5.4`: the self-hosting
pilot converts Plans/SddGraph itself and will show the real branch volume of
a full walk; decide the retention policy there with data (gc pruning
branches whose provenance revision is mainline-reachable, or a one-shot
plan-completion sweep). Non-blocking here: correctness is unaffected, the
branches are findable by node id, and premature pruning risks deleting the
only reference to crashed-claim work — exactly what the branches exist to
preserve.

### F-02 — deferred

2026-09-01 — deferred to FU-02, tracked in plan task `4.1`: delete the three
dead consts the next time cmd/sdd is materially touched (4.1 adds review-gate
verbs there). Non-blocking here: the consts predate the range, are inert,
and folding an unrelated deletion into a frozen phase range after its review
would trade bisectability for trivia.
