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
    evidence: "drift-detector (agent, final round 4, delta 4d65872..b4a8456: 377372e, c976667, b4a8456 plus inline 7d8f13d; 11 files, each commit single-concern per git show --stat): child-validation-hermetic rev 6 (25 artifacts), prepare-once-determinism rev 2 and propagation-validator rev 3 (8 artifacts) have every recorded artifact digest recomputed against the tree with zero mismatches; go build, go vet and the named gate tests pass; whole-plan reconciliation of git diff --name-only 04c1e61..HEAD against the union of node artifacts leaves 34 files, every one traced to a tool-fix commit logged in SDD-DOGFOOD-NOTES.md under the plan's inline-tool-fix carve-out and reviewed Aligned in earlier rounds; plugin.json still 2.10.0, no telemetry, budget, SHA-256 or Job Object code. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "quality-scanner (agent, final round 4, delta 4d65872..b4a8456 plus whole-tree checks): gofmt -l (only the untouched algorithms_test.go), go vet ./..., staticcheck ./..., GOOS=windows and GOOS=darwin go build ./..., make test, and go test -race -count=2 on rules, testenv, procexec (this round's determinism acceptance) all clean; every non-test RevisionExists/IsAncestor caller in internal/ and cmd/ enumerated and classified. Major: internal/rules/phasereview.go:684, :911, :926, :931, :1159 keep the pre-fix IsAncestor pattern and can turn a transient ErrOperational (reachable because r.Repo always wraps a recordingRepo, root.go:180-195) into a false SDD173/SDD175; none is covered by TestIdentityQueryFailuresAreOperational; :816 only skips emission. Question: Clean() at :657 and Head() at :663 share the shape. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "spec-compliance (agent, final round 4, definitive DD-10 audit of every RevisionExists, IsAncestor, FileAt, ChangedPaths and vcs Clean() caller in internal/ and cmd/, 30-plus sites listed with file:line): no non-compliant caller — each either discriminates ErrOperational/ErrNotFound/ErrUnsupported explicitly (evidence.go:570, :584, :609, :962; phasereview.go:866, :897, :917, :979, :986, :1093; retirement.go:53; cache.go:145 cacheable; remap.go:181; sync.go:303; cmd/sdd/evidence.go:102, :118; cmd/sdd/review.go:105, :113) or is structurally protected by recordingRepo (root.go:216-252) plus evaluate.go:52,60,69,101 discarding the sweep, proven generically by TestIgnoredRepoErrorAbortsEvaluation. Minor coverage gap: phasereview.go:658, :684, :690, :698, :816, :911, :926, :931, :1159 and evidence.go:653, :676, :706, :724 rely on the structural guarantee with no call-site test. FR-01/DD-1 import-based inventory confirmed (spawnImportPaths at adoption_test.go:824; graph/proposal and testgate really import os/exec); DD-8 registry immutability covered; tools/parity/frozen-expectations.json byte-identical to 04c1e61. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3"
    evidence: "blind-spot-finder (agent, final round 4, diff only 4d65872..b4a8456; full reads of phasereview.go, evaluate.go, rules.go, testenv.go, procexec/helper_test.go, adoption_test.go; grepped every IsAncestor, RevisionExists and OperationalFailure caller; go build clean; go test -shuffle=on on rules, testenv, cmd/sdd and go test -race -count=2 -shuffle=on on rules, testenv, procexec, graph/proposal, testgate all pass): Major — phasereview.go:684, :911, :926, :931, :1159 keep the old IsAncestor pattern and would emit a false SDD173/SDD175 on ErrOperational, masked today only by evaluate.go:51-76 discarding the sweep output; fakeGitRepo.ancestrErr is exercised once (verifyCleanGitIdentity). Question — procexec helper_test.go's spawn-descendant child builds an env from scratch without PATH or the policy vars (pre-existing, no git involved). cmd/sdd subprocess tests inherit the hermetic env via nil Cmd.Env. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: major
    title: "Remaining repo queries in rule callbacks still fold operational failures into diagnostics"
    status: answered
  - id: F-02
    severity: minor
    title: "All() deep-copies examples on the validate hot path"
    status: answered
  - id: F-03
    severity: minor
    title: "procexec's spawn-descendant helper builds a child environment without the policy"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..b4a8456d8c17acc83ad2488cccd389d37b20d2d3`.

## Findings
### F-01 — Remaining repo queries in rule callbacks still fold operational failures into diagnostics

`internal/rules/phasereview.go:684`, `:911`, `:926`, `:931`, `:1159` and siblings; structurally masked by the evaluator's discard.

### F-02 — All() deep-copies examples on the validate hot path

`internal/rules/rules.go:176-201`.

### F-03 — procexec's spawn-descendant helper builds a child environment without the policy

`internal/procexec/helper_test.go`'s descendant child gets `Env = []string{helperEnv + "=sleep"}`; pre-existing and git-free.

## Resolution Log
### F-01 — answered

2026-09-13: Revised through the sibling execution-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-execution-b4a8456.md`, F-01), contract rev 4 of `propagation-validator`.

### F-02 — answered

2026-09-13: Landed inline as 4eaa3cf.

### F-03 — rejected

2026-09-13: The descendant is a sleep that never touches git; the from-scratch environment is what the containment test needs. Recorded in SDD-DOGFOOD-NOTES.md follow-ups as a note for future helper modes.
