---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Read the delta 4d10f15..facd924 over the gate's scope (only CLAUDE.md, Makefile, tools/regression/corpus_test.go and the new tools/testgate/race_test.go changed) against phase views 01/02/03 and the graph JSON. race-gate: Makefile adds test: test-race as an accumulated prerequisite and a test-race recipe running go test -race -count=1 over the seven declared packages (make -n test shows it executing first); TestMakeTestRunsRaceDetector parses the Makefile and passes; CLAUDE.md:207 documents it; the node's artifacts, gate test and verified digests match. child-validation-hermetic rev 3: corpus_test.go:313-368 prepares a committing corpus root through prepare() and asserts git log author/email/date against the fixture identity, keeping the literal guard; the node lists all four gate tests. Ran go build ./..., both gate tests and make test-race: all pass. bf004eb in the same range belongs to the execution gate's scope, not this one. No missing work, creep or drift."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Intent-blind review of the delta 4d10f15..facd924 (CLAUDE.md, Makefile, tools/regression/corpus_test.go, tools/testgate/race_test.go) with full-file context: verified make -n test runs test-race before the ./... recipe, that the accumulated test: test-race rule leaves TestMakeGateRunsFreshTests's literal intact (checked bytes with cat -A), that committingFixture deterministically selects fixtures/SDD154/spec-elements-removed whose SETUP really commits; ran go build ./..., go vet on both packages, go test ./tools/testgate (4/4), the corpus test, and make test-race (seven packages pass). One minor with no current impact: parseMakeRules in race_test.go checks for '=' before the first ':' so ':=' assignments (PYTHON, SDD, PLATFORMS, ...) are parsed as rules; harmless today because only the test and test-race keys are asserted."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Checked the delta 4d10f15..facd924 in scope (CLAUDE.md, Makefile, tools/regression/corpus_test.go, tools/testgate/race_test.go) against NFR-05 and FR-01/FR-03/AC-01/DD-1/DD-8 with full-file context: Makefile:146-181 test-race runs go test -race -count=1 over internal/rules, internal/procexec, internal/vcs, internal/graph/{sync,ops,provider}, tools/regression and is a prerequisite of test (matches the design's Structural Verification command plus the graph packages it anticipates; -p/-parallel belong to the deferred budget workstream), TestMakeTestRunsRaceDetector enforces it structurally; corpus_test.go:315-334 prepares a real corpus root through prepare() and asserts author, email and date against testenv.FixtureName/FixtureEmail and 2024-01-01T00:00:00Z. Ran go build ./..., the testgate test, and the corpus test under -race: all pass. No coverage gap, no contract violation."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc"
    evidence: "Diff-only read of the range 4d10f15..facd924: the filtered scope changed only CLAUDE.md, Makefile, corpus_test.go and race_test.go; the lane also read the range's other changed files (procexec containment.go/contain_*.go/procexec.go, doctor.go, main.go, root.go, vcs git.go/vcs.go) in full. Findings: F-01 major, internal/provision/git_post_rewrite.go gitOutput/gitConfigPath use raw os/exec (no procexec import) and are reached by doctor on every run via CheckPostRewrite/InstallPostRewrite, so the containment guarantee has a silent exemption in the command meant to report it; F-02 minor, diagnose duplicates the reason (reproduced through vcs.Detect().Head() with ContainmentProbe stubbed; scratch package removed); F-03 minor, CauseContainment doc omits the pre-flight case. Not flagged: the race Makefile mechanics (ran TestMakeTestRunsRaceDetector), the unchanged sweep logic, the %w changes, committingFixture."
findings:
  - id: F-01
    severity: major
    title: "internal/provision runs git through os/exec, outside the bounded runner"
    status: open
    action: extend
    node:
      id: provision-owned-execution
      contract: "Every git invocation in internal/provision (the post-rewrite hook resolution used by sdd doctor: gitOutput, gitConfigPath and any sibling) runs through procexec.Run under the default policy, so doctor never forks an uncontained or unbounded git child; a test proves the package has no direct os/exec git call and that a hanging git on the doctor path fails within the runner's bounds."
      deps: [bounded-runner]
      gate:
        type: tests
        tests:
          - {id: TestProvisionUsesOwnedRunner, file: internal/provision/owned_runner_test.go}
      hazards: []
      artifacts: [internal/provision/git_post_rewrite.go, internal/provision/owned_runner_test.go]
  - id: F-02
    severity: minor
    title: "diagnose() renders the unsupported-platform reason twice"
    status: rejected
  - id: F-03
    severity: minor
    title: "CauseContainment doc comment omits the no-adapter pre-flight case"
    status: rejected
  - id: F-04
    severity: minor
    title: "parseMakeRules treats := assignments as rules"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..facd924bd5e6f6001bb79578b6d1c78ad122e9cc`.

## Findings
### F-01 — internal/provision runs git through os/exec, outside the bounded runner

`internal/provision/git_post_rewrite.go:348-365` (`gitOutput`, `gitConfigPath`) shell out with raw `os/exec`, and `cmd/sdd/doctor.go` reaches them on every run through `CheckPostRewrite`/`InstallPostRewrite`, before the new containment blocker is printed. On a platform without an adapter, doctor itself forks uncontained, unbounded git children, which is the exact class the runner exists to prevent; `tools/regression/corpus.go` shows the intended pattern.

### F-02 — diagnose() renders the unsupported-platform reason twice

Duplicate of the execution gate's finding (06-review-execution-facd924 F-02), which revises unsupported-platform-diagnostic.

### F-03 — CauseContainment doc comment omits the no-adapter pre-flight case

`internal/procexec/errors.go:35-38` describes only runtime adapter failure; the pre-flight refusal at `procexec.go:41-43` shares the cause.

### F-04 — parseMakeRules treats := assignments as rules

`tools/testgate/race_test.go:183-198` checks for `=` before the first `:`, so `PYTHON := python3` parses as a rule; harmless today because only the test and test-race keys are asserted.

## Resolution Log
### F-02 — rejected

2026-09-13: Duplicate; handled by the execution gate's revise of unsupported-platform-diagnostic (TestDiagnoseRendersPlatformOnce).

### F-03 — rejected

2026-09-13: Documentation only; folded into the same unsupported-platform-diagnostic rework (the implementer updates the CauseContainment comment), not a separate node.

### F-04 — rejected

2026-09-13: No behavioral impact; the parser is local to one gate test and only asserts the test and test-race keys. Recorded in SDD-DOGFOOD-NOTES.md as a cleanup for when the parser grows.
