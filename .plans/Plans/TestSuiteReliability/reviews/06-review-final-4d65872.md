---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "4d6587238de8b72301d3bef22c242d9b56f0da84"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "drift-detector (agent, final round 3, delta 7ee8ef6..4d65872, 11 files, four disjoint single-concern commits per git show --stat): child-validation-hermetic rev 5, propagation-validator rev 2 and propagation-graph-callers rev 2 have contract_rev, artifact digests (every listed file re-hashed with sha256sum) and provenance.revision matching HEAD 4d65872; no work outside the four commits plus graph bookkeeping; Non-Goals untouched. Minor bookkeeping: child-validation-hermetic's artifacts array omits the five main_env_test.go files f9be2ec created under internal/graph/{compile,convert,intent,model,review} (a walk of every node's artifacts finds none of them), so their digests are untracked. The Git-side verifyCleanGitIdentity at evidence.go:570 still discards its error — pre-existing, outside rev 2's Perforce-only wording. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "quality-scanner (agent, final round 3, delta 7ee8ef6..4d65872 plus whole-tree checks): staticcheck ./..., go vet ./..., gofmt -l (only the untouched algorithms_test.go), GOOS=windows and GOOS=darwin go build ./..., and make test (36 packages incl. -race) all clean. Whole-plan caller classification of RevisionExists/FileAt/ChangedPaths: recordingRepo (root.go:216,232,244) and memoRepo (cache.go:190,212,218) pass errors through; cmd/sdd/evidence.go:118, cmd/sdd/review.go:105 and remap.go:181 split ErrNotFound correctly; appendonly.go:186 best-effort baseline is not misleading; retirement.go:53 folds the cause into text (question). Major: evidence.go:570 verifyCleanGitIdentity still discards the error beside the fixed Perforce sibling; same class pre-existing at evidence.go:952 and phasereview.go:815, :862, :964, :1071, and generic-text FileAt sites at evidence.go:643,666,696,714 and phasereview.go:412,697,773. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "spec-compliance (agent, final round 3, walked DD-10's full audit list plus every RevisionExists/FileAt/ChangedPaths caller): provider.go:89-98 execRunner wraps ErrOperational with %w; doctor.go:304-336 reports inability; sync.go:293-320 distinguishes ErrUnsupported from operational; gitCapable at evidence.go:565 gates on Kind only; bareOnce (waivers.go:233-244) shares the Root collector; remap.go:174-184 and evidence.go:599-608 fixed with live-passing tests; the five TestMain adoptions close the FR-01/DD-1 gap and TestGitSpawnInventoryFollowsIndirection passes; tools/parity/frozen-expectations.json has zero diff and zero commits since 04c1e61. Minor: internal/rules/retirement.go:54 wraps a post-detection RevisionExists error with %v so errors.Is(ErrOperational) is lost, and TestRetirementDetectionFailureIsOperational only exercises the detection path. Minor: evidence.go:570 verifyCleanGitIdentity lacks the sibling's split and a direct-call test (collector-protected today). VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "blind-spot-finder (agent, final round 3, diff only 7ee8ef6..4d65872; read every changed file, walked callers of RemapRevisions, verifyCleanP4Identity and gitSpawnOffenders; go build ./... and go test ./... green on a host with no p4; -trimpath and symlinked-checkout traced through os.Getwd, no failure mode): Major — fileCallsGitSpawner (adoption_test.go:673-709) recognises only a direct exec/procexec selector with a literal \"git\" argument, so internal/graph/provider/provider.go's execRunner(dir, name, ...) → procexec.Run(name, ...) with the literal at g.run call sites (:216-342) is invisible; provider is flagged today only because its test files carry their own literal; a synthetic two-package module with helper.Run(name) wrapping exec.Command(name) and a caller passing \"git\" produced offenders = []; a temporary deletion of provider's main_env_test.go was restored and the tree confirmed clean. Question — TestGitSpawnInventoryFollowsIndirection's package a case is caught by the test-file literal rule, not by production call-shape matching. Not flagged: vendor/ walk (inert), t.Setenv without t.Parallel. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: major
    title: "The git-spawn inventory can be bypassed by indirection through variable-named spawners"
    status: answered
  - id: F-02
    severity: major
    title: "Git-side identity checks still fold operational failures into absence"
    status: answered
  - id: F-03
    severity: minor
    title: "VerifyRetirementSource wraps a probe failure with %v"
    status: answered
  - id: F-04
    severity: minor
    title: "child-validation-hermetic's artifact list omits the five new TestMain files"
    status: answered
  - id: F-05
    severity: major
    title: "A second in-process evaluation from a fresh root loses traceability findings"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84`.

## Findings
### F-01 — The git-spawn inventory can be bypassed by indirection through variable-named spawners

`internal/graph/provider/provider.go:89-98`'s `execRunner(dir, name, ...)` shape is invisible to `fileCallsGitSpawner`; a synthetic helper module produced an empty offender set.

### F-02 — Git-side identity checks still fold operational failures into absence

`internal/rules/evidence.go:570`, `:952` and four `phasereview.go` sites.

### F-03 — VerifyRetirementSource wraps a probe failure with %v

`internal/rules/retirement.go:54`.

### F-04 — child-validation-hermetic's artifact list omits the five new TestMain files

`internal/graph/{compile,convert,intent,model,review}/main_env_test.go` are declared by no node.

### F-05 — A second in-process evaluation from a fresh root loses traceability findings

`go test -count=2 ./internal/rules` fails two SDD161 tests on the second run.

## Resolution Log
### F-01 — answered

2026-09-13: Revised through the sibling fixtures-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-fixtures-4d65872.md`, F-01): the inventory becomes import-path based, contract rev 6 of `child-validation-hermetic`.

### F-02 — answered

2026-09-13: Revised through the sibling execution-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-execution-4d65872.md`, F-01), contract rev 3 of `propagation-validator`.

### F-03 — answered

2026-09-13: Same execution-gate revise as F-02.

### F-04 — answered

2026-09-13: Fixed with `sdd graph set-artifacts --add` on `child-validation-hermetic` alongside this round's amendments.

### F-05 — answered

2026-09-13: Revised through the sibling fixtures-gate review at this identity (F-02), contract rev 2 of `prepare-once-determinism`.
