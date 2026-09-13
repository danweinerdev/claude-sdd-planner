---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "drift-detector (agent, delta 4d65872..b4a8456, 11 files): child-validation-hermetic rev 6 — adoption_test.go's spawnImportPaths map and parser.ImportsOnly scan replace the deleted fileHasGitLiteral/fileCallsGitSpawner, TestSpawnInventoryIsImportBased asserts the six-package synthetic offender set is exactly {r, u, w}, all seven gate tests present and passing; prepare-once-determinism rev 2 — TestFreshRootsAreIndependentInProcess at harness_test.go:289 passes and go test -count=2 ./internal/rules no longer fails SDD161; the root-cause fix (clone/cloneExamples/exportRule in rules.go) lands in single-pass-evaluation's artifact, consistent with the finding's cause. The merged internal/procexec/helper_test.go TestMain runs the helper interception before testenv.Main, all 13 procexec tests pass under -race so bounded-runner and posix-containment are unaffected. Other touched files belong to propagation-validator rev 3 or the inline 7d8f13d docstring. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "quality-scanner (agent, delta 4d65872..b4a8456 on testenv, procexec/helper_test.go, graph/proposal, testgate, rules.go, harness_test.go; gofmt, go vet, staticcheck clean; go test -race -count=2 ok on testenv 1.4s, procexec 23.1s, rules 11.8s, testgate 1.0s; go build ./... clean; exportRule's shared Check/CheckRoot closures verified stateless): import-based scan, TestMain wiring and the procexec re-exec merge correct. Minor — rules.go:176-201 All() deep-copies all 115 rules' Good/Bad on every call, and evaluate.go:51-72 never reads them, yet runWith, runWithWaiversWith, bareOnce (waivers.go:248) and knownCode (waivers.go:152, once per waiver entry) call All() on the real sdd validate path (cmd/sdd/transition.go:251,343); only cmd/sdd/hermetic_test.go:100 and tools/genfixtures/main.go:64 need the copies. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "spec-compliance (agent, delta 4d65872..b4a8456; go build, go vet and the design's -race -p=2 -parallel=4 command pass across every touched package): FR-04/FR-05/DD-8 — rules.go clone/cloneExamples/exportRule with TestFreshRootsAreIndependentInProcess (harness_test.go) passing; FR-01/DD-1 — spawnImportPaths and the parser.ImportsOnly rewrite with TestSpawnInventoryIsImportBased passing, TestMain installs in graph/proposal, tools/testgate and procexec's helper. DD-10/FR-16 partial: RevisionExists sites in evidence.go:570-612, :962-966, phasereview.go and retirement.go are guarded, but IsAncestor at phasereview.go:684 (verifyGitPhasePostReviewState, also Clean() at :658), :911, :926 (verifyPhaseReviewIdentity) and :1159 (verifyGitPlanPhaseCheckpoints) still report an operational failure as not-an-ancestor; :816 silently skips. Major. VERDICT: Needs changes."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "blind-spot-finder (agent, diff only 4d65872..b4a8456 on testenv, procexec/helper_test.go, graph/proposal, testgate, rules.go, harness_test.go; read every touched file in full, repo-wide greps for syscall.ForkExec, os.StartProcess, cgo, go:linkname, external _test packages and build-tag exclusions found none; go build and targeted tests pass): no live bypass of the import-based inventory. Two Minor maintenance notes: spawnImportPaths (adoption_test.go:75-87) is a closed two-entry enumeration with nothing asserting it stays exhaustive if a third process-launch wrapper appears; exportRule (rules.go:176-204) deep-copies every rule's Good/Bad on each All()/Get() call including the validate hot path that never reads them (negligible today). helper TestMain ordering and Example.clone field completeness checked and sound; the internal/testenv exemption still holds. VERDICT: Aligned."
findings:
  - id: F-01
    severity: minor
    title: "All() deep-copies every rule's examples on the validate hot path"
    status: answered
  - id: F-02
    severity: minor
    title: "spawnImportPaths is a closed two-entry enumeration"
    status: rejected
  - id: F-03
    severity: major
    title: "IsAncestor and other repo queries in phase-review rules still fold operational failures into diagnostics"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3`.

## Findings
### F-01 — All() deep-copies every rule's examples on the validate hot path

`internal/rules/rules.go:176-201`: `Run`, `RunWithWaivers`, `bareOnce` and `knownCode` (once per waiver) called `All()` although evaluation never reads `Good`/`Bad`.

### F-02 — spawnImportPaths is a closed two-entry enumeration

`internal/testenv/adoption_test.go:75-87` lists `os/exec` and the module's procexec; a future third process-launch wrapper would need adding.

### F-03 — IsAncestor and other repo queries in phase-review rules still fold operational failures into diagnostics

`internal/rules/phasereview.go:684`, `:911`, `:926`, `:931`, `:1159` (`IsAncestor`), `:658` (`Clean`), `:816` (gate) — masked today by the evaluator's sweep-level discard.

## Resolution Log
### F-01 — answered

2026-09-13: Code-only; landed inline as 4eaa3cf (`allRules` for the four internal callers, `All`/`Get` keep the copy contract, `TestAllReturnsDistinctExampleMaps` pins it).

### F-02 — rejected

2026-09-13: The enumeration is deliberate and any new wrapper must itself import one of the two to spawn anything; recorded in SDD-DOGFOOD-NOTES.md follow-ups to add a one-line comment naming the closed set.

### F-03 — answered

2026-09-13: Revised through the sibling execution-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-execution-b4a8456.md`, F-01), contract rev 4 of `propagation-validator`, which owns those sites.
