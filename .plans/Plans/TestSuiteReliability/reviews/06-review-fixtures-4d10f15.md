---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d10f1512f70b05c759139a48a2076aec7a8ae62"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "4d10f1512f70b05c759139a48a2076aec7a8ae62"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d10f1512f70b05c759139a48a2076aec7a8ae62"
    evidence: "Read the delta 421dd78..4d10f15 over the gate's scope (7 files, 201 insertions, 4 deletions) and spot-checked the full range against phase views 01/02/03 and the graph JSON. Both amendments are implemented as their revised contracts state: child-validation-hermetic rev 2 removes GIT_CONFIG_NOSYSTEM/GIT_CONFIG_GLOBAL from internal/rules/rules_test.go setupEnv (187-192) and the sibling tools/regression/corpus.go (233-239), gate test TestFixtureSetupInheritsPolicy at internal/rules/hermetic_test.go:134 passes under -race; single-pass-evaluation rev 2 adds bareMu on Root (root.go:89) locked in bareOnce (waivers.go:210-212), gate test TestWaiverMemoConcurrencySafe at evaluate_test.go:212 passes under -race. TestCorpusSetupInheritsPolicy is a companion proof on a declared artifact, not creep. go build, go vet, go test -race on internal/rules, tools/regression, cmd/sdd, internal/testenv all pass. Only note: rendered views 01/03 still show rev 1 text until sdd compile re-renders."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d10f1512f70b05c759139a48a2076aec7a8ae62"
    evidence: "Intent-blind review of the delta 421dd78..4d10f15 with full call-graph context: traced testenv.Policy.Apply into os.Environ() and both runSetup sites (internal/rules/harness_test.go:77, tools/regression/corpus.go:311) to confirm the last-duplicate-key override is gone once rules_test.go:191 and corpus.go:238 drop the GIT_CONFIG_* keys; the new tests probe the pins through runSetup itself, not a post-hoc config read, and TestFixtureSetupInheritsPolicy re-verifies the fixed identity/date invariant. bareMu (root.go) locked in bareOnce (waivers.go:244-249): checked reentrancy (evaluate excludes waiverRuleCodes so bareOnce cannot re-enter) and lock ordering (no lock held while taking another). go build, gofmt -l on nine changed files, go vet, staticcheck (one pre-existing out-of-delta U1000 at inventory_test.go:39) and go test -race on internal/rules, tools/regression, internal/testenv all pass; the three new tests were run individually first. No findings."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d10f1512f70b05c759139a48a2076aec7a8ae62"
    evidence: "Re-checked the delta 421dd78..4d10f15 in scope against FR-01/FR-03/AC-01 (DD-1) and DD-6/DD-7 with full-file context, tracing testenv.Main -> Install -> Apply into os.Environ() and runSetup's policy.Env composition: rules_test.go:187-191 and corpus.go:233-238 drop the two GIT_CONFIG_* keys so fixture setup inherits the policy; TestFixtureSetupInheritsPolicy (hermetic_test.go:103-148) and TestCorpusSetupInheritsPolicy (corpus_test.go:270-304) observe core.fsmonitor and commit.gpgsign false from inside the Setup command; root.go:89 bareMu and waivers.go:210-216 lock bareOnce, TestWaiverMemoConcurrencySafe (evaluate_test.go:23-72) drives 8 goroutines under -race. All three new tests pass with -race. Comments on setupEnv now state the append-order hazard. No coverage gap or contract violation in the delta; the remainder of A/B/C was assessed in the prior pass through 421dd78."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d10f1512f70b05c759139a48a2076aec7a8ae62"
    evidence: "Diff-only read of the delta 421dd78..4d10f15 and full files: root.go, waivers.go, evaluate_test.go, rules_test.go, harness_test.go, hermetic_test.go, corpus.go, corpus_test.go, testenv.go, Makefile. Validated F-01 by building a worktree at 421dd78, applying only the new test, and running it with and without -race (fails only with the detector; make test passes none) and confirming grep -n race Makefile is empty. Validated F-02 by comparing corpus_test.go:277-322 against hermetic_test.go:117-179 and corpus.go:294-316: the corpus identity check never runs git. Not flagged: bareMu placement (correct, matches its comment), the env-precedence rationale (verified against os/exec), Windows skips. Question: no direct CheckRoot caller outside evaluate() and bareOnce was found by grep."
findings:
  - id: F-01
    severity: major
    title: "make test never runs the race detector, so the race regression test is inert in the gate"
    status: open
    action: extend
    node:
      id: race-gate
      contract: "make test runs go test -race over the shared-state packages (internal/rules, internal/procexec, internal/vcs, internal/graph/sync, internal/graph/ops, internal/graph/provider, tools/regression) through a test-race target it depends on, so a removed memo or containment lock fails the standard gate; a gate test parses the Makefile and refuses a test recipe that does not invoke the race target over those packages."
      deps: [single-pass-evaluation]
      gate:
        type: tests
        tests:
          - {id: TestMakeTestRunsRaceDetector, file: tools/testgate/race_test.go}
      hazards: []
      artifacts: [Makefile, tools/testgate/race_test.go, CLAUDE.md]
  - id: F-02
    severity: minor
    title: "TestCorpusSetupInheritsPolicy's identity half only inspects its own literal"
    status: open
    action: revise
    nodes: [child-validation-hermetic]
    revise:
      gate:
        type: tests
        tests:
          - {id: TestInProcessValidationHermetic, file: internal/rules/hermetic_test.go}
          - {id: TestChildValidationHermetic, file: cmd/sdd/hermetic_test.go, satisfies: [user-entrypoint]}
          - {id: TestFixtureSetupInheritsPolicy, file: internal/rules/hermetic_test.go}
          - {id: TestCorpusSetupInheritsPolicy, file: tools/regression/corpus_test.go}
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..4d10f1512f70b05c759139a48a2076aec7a8ae62`.

## Findings
### F-01 — make test never runs the race detector, so the race regression test is inert in the gate

`Makefile` `test:` runs `go test -count=1 ./...` with no `-race`. `TestWaiverMemoConcurrencySafe`'s own comment says the unguarded memo is only detected under `-race`; a controlled run on the pre-fix commit confirmed it fails only with the detector and passes in 0.00s without it. The spec asks for race-enabled tests on shared state; today the standard gate this repository's maintenance rules prescribe cannot catch a removed lock.

### F-02 — TestCorpusSetupInheritsPolicy's identity half only inspects its own literal

`tools/regression/corpus_test.go:314-321` says "the fixed identity still governs, so the corpus's recorded SHAs hold" but only iterates the in-package `setupEnv` literal for `GIT_CONFIG_` keys; its sibling in `internal/rules/hermetic_test.go` runs `git log` against a real prepared fixture. Make the corpus half observe a real prepared corpus fixture's author, email and date, and name it in the node's gate so it is a proof, not a companion.

## Resolution Log
None.
