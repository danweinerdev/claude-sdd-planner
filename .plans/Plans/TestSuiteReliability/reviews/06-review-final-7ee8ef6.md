---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "drift-detector (agent, final round 2, delta faaf81a..7ee8ef6, 18 files +514/-13): the four amendment commits db2e653, 4d1f94a, 46b8a56, 7ee8ef6 are single clean commits implementing each finding's revise.contract verbatim (graph contract_rev checked-detection 3, child-validation-hermetic 4, race-gate 2, bounded-runner 2); every recorded artifact digest for those nodes recomputed with sha256sum against the working tree with zero mismatches and provenance.revision at 7ee8ef6; all cited gate tests run directly at HEAD and pass, go build ./... clean; .gitignore *.test and the provision MkdirAll fixture change are disclosed in the prior final review's F-08/F-09 resolution log; 008c9c1 is the disclosed inline tool fix (README.md:73 policy). Non-Goals respected: no budget, telemetry, SHA-256 Git, version bump or scheduler change. No unowned work. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "quality-scanner (agent, final round 2, delta faaf81a..7ee8ef6 plus whole-tree checks at HEAD): the Critical p4 collapse is fully fixed — RevisionExists, both FileAt sites and ChangedPaths route through absentP4 and the one remaining bare ErrNotFound at p4.go:140 follows a successful query whose output says no such changelist; TestP4OperationalErrorsAreNotAbsence and the full suite green. gofmt -l lists only the untouched internal/graph/algorithms/algorithms_test.go; go vet ./..., staticcheck ./..., GOOS=windows and GOOS=darwin/arm64 go build ./..., and make test (with the new vet prerequisite and test-race) all clean. One Minor: the new gate-type guard at internal/graph/review/amend.go:223-225 refusing review/command targets has no test (its error text appears only at the call site; TestPlanAmendmentsScopeVsReach never names inner-review or full-gate as a target). VERDICT: Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "spec-compliance (agent, final round 2 against spec, design and the decisions file): FR-01/DD-1 adoption — TestGitSpawningTestPackagesInstallPolicy passes and every git-spawning test dir (cmd/sdd, graph/ops, graph/provider, graph/sync, hook, provision, rules, vcs, tools/regression) has a TestMain with internal/testenv exempted for a documented reason; NFR-05 — Makefile test depends on vet, TestMakeTestRunsVet passes, make vet clean; FR-13 and the fault-injection acceptance row — TestMutationNotRetried passes, Run has no retry loop; FR-16/DD-10 — absentP4 at p4.go:116-123 used at :139-141, :172-174, :181-183, :204-206 with TestP4OperationalErrorsAreNotAbsence passing; DD-7 — cache.go:132 cacheable() can no longer memoize a transient p4 failure as absence; tools/parity/frozen-expectations.json unchanged since 04c1e61 (empty diff). No gaps, no violations. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "blind-spot-finder (agent, diff only faaf81a..7ee8ef6, full reads of testenv.go, adoption_test.go, review/amend.go, ops/amend.go, p4.go, checked_test.go, procexec.go and tests, git_post_rewrite_test.go, Makefile, race_test.go; go vet, go test -count=1 ./..., make vet run): Major — TestGitSpawningTestPackagesInstallPolicy regex-matches literal spawn call sites in _test.go only, so a test that spawns git through a helper in a sibling non-test file is invisible; reproduced in a scratch module copy with a synthetic package whose test calls SpawnGit from a non-test file and no TestMain — the inventory reported PASS. No live gap today (every package importing the four production git-spawning files already has a TestMain). Minor — Reach lets a top-level review name any node in its transitive closure; the claim check at ops/amend.go:182-184 still blocks concurrent-claim conflicts, so this is blast-radius clarity, not a hazard. Other suspicions (hooks-dir seeding in other fixtures, vet double-run, exactly-once) resolved as non-issues. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: major
    title: "The git-spawn inventory misses spawns routed through a wrapper or helper package"
    status: answered
  - id: F-02
    severity: minor
    title: "The amend gate-type guard has no test"
    status: answered
  - id: F-03
    severity: minor
    title: "Reach widens a review's amendable set without warning the operator"
    status: rejected
  - id: F-04
    severity: minor
    title: "Two propagation callers still swallow the operational error the p4 fix surfaces"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e`.

## Findings
### F-01 — The git-spawn inventory misses spawns routed through a wrapper or helper package

`internal/testenv/adoption_test.go` matches literal spawn call sites in test files only; a synthetic package whose test spawns git through a helper defined in a non-test file passed the inventory with no TestMain.

### F-02 — The amend gate-type guard has no test

`internal/graph/review/amend.go:223-225` refuses review or command nodes as revise/extend targets, but no test names such a node.

### F-03 — Reach widens a review's amendable set without warning the operator

A finding on a top-level review may now name any node in its transitive closure; the claim check at `internal/graph/ops/amend.go:182-184` still blocks concurrent-claim conflicts.

### F-04 — Two propagation callers still swallow the operational error the p4 fix surfaces

`internal/graph/ops/remap.go:177` and `internal/rules/evidence.go:598` collapse a failed `RevisionExists` query into absence.

## Resolution Log
### F-01 — answered

2026-09-13: Revised through the sibling fixtures-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-fixtures-7ee8ef6.md`, F-01), which owns `child-validation-hermetic`; lands as contract rev 5.

### F-02 — answered

2026-09-13: sdd tool code outside the graph; the missing case is added to `TestPlanAmendmentsScopeVsReach` in the inline tool commit that accompanies this round's amendments.

### F-03 — rejected

2026-09-13: The widened set is the point of the fix (a whole-plan review must be able to revise anything it covers), and the claim check remains the concurrency backstop. A non-refusing notice when a target lies outside the increment scope is recorded in SDD-DOGFOOD-NOTES.md follow-ups.

### F-04 — answered

2026-09-13: Revised through the sibling execution-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-execution-7ee8ef6.md`, F-01 and F-02), which owns `propagation-graph-callers` and `propagation-validator`.
