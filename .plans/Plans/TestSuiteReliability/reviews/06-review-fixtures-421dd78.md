---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Read every hunk of the range restricted to the gate's 22 declared artifacts (3669 lines, 21 files) against phase views 01-hermetic-git, 02-determinism, 03-single-pass and the graph. All seven in-scope nodes (test-git-policy, child-validation-hermetic, prepare-once-determinism, fixture-reproducibility, pure-selection, single-pass-evaluation, append-only-scan-once) have code mapping to their contracts; every named gate test exists in its declared file and passes; no missing work and no scope creep (make test-pure and the CLAUDE.md bullet trace to pure-selection's artifacts; harness_test.go's runSetup is fixture-runners-owned's declared dual ownership). Observation: because internal/rules/root.go and evaluate.go are also artifacts of propagation-validator, the file-scoped diff carries that node's operational-failure collector (commit 9676f8e); that work is in review-execution's declared scope, which lists both files, so it is reviewed there rather than here. staticcheck nit outside the drift lens: unused const pureSelectionPattern in internal/rules/inventory_test.go (U1000); tracked for cleanup before review-final. go build/vet/test/-race green on internal/rules, internal/testenv, cmd/sdd, internal/vcs, tools/regression."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Intent-blind review of the range restricted to the gate's 22 declared artifacts: inspected internal/testenv/testenv.go (policy vars, dropped keys, Apply/Install restore), internal/rules/evaluate.go and root.go (runWith/evaluate seam, opFailures and append-only memo under mutex, defensive copies), harness_test.go/inventory_test.go/pure_test.go/reproducibility_test.go, the four TestMain files, cmd/sdd/hermetic_test.go and internal/rules/hermetic_test.go. go build, go vet, gofmt -l, full go test with -race and make test-pure all pass. One minor maintainability note only: hostileGitHost is duplicated verbatim between cmd/sdd/hermetic_test.go and internal/rules/hermetic_test.go (two copies; consolidate if a third appears). No correctness, safety or testing defects."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Checklist of FR-01..FR-09, AC-01..AC-04 and DD-1/DD-6/DD-7/DD-8 mapped to the diff with search trails: FR-01..03/AC-01 to internal/testenv/testenv.go (Policy, Install, Env, Apply, dropped keys, os.DevNull) and the four testenv.Main TestMains plus hermetic child-process tests; FR-04/05/AC-02 to harness_test.go prepareExample/diagnosticsEqual and reproducibility_test.go; FR-06/AC-03 to pure_test.go (14 TestPure cases), Makefile test-pure, inventory_test.go child run with empty PATH; FR-07/08/09/AC-04 to evaluate.go, appendonly.go per-Root memo, root.go evaluation-local collector and the vcs cache commit 4bd627a in range. Ran go build, go vet, go test on internal/rules, internal/testenv, cmd/sdd TestChildValidationHermetic and the pure selection: all pass. No coverage gap, no contract violation; Run/RunWithWaivers signatures unchanged, RunChecked additive per design Interfaces."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4"
    evidence: "Diff-only read of every changed file in the 22-artifact scope with call-chain tracing: testenv.go/testenv_test.go, root.go, evaluate.go, appendonly.go, waivers.go, rules.go, rules_test.go, harness_test.go, both hermetic_test.go files, inventory_test.go, pure_test.go, reproducibility_test.go, Makefile, CLAUDE.md. Two findings, both validated: F-01 (major) rules_test.go setupEnv appends GIT_CONFIG_GLOBAL=/dev/null after os.Environ() so fixture Setup git commands escape the policy's global config; confirmed empirically with a temporary probe test under the real TestMain (core.fsmonitor and commit.gpgsign unset in a prepared fixture, then removed the probe and confirmed a clean tree) and with a minimal Go program showing exec.Cmd takes the last duplicate env key. F-02 (minor) Root.bareComputed/bareDiagnostics memo is unguarded while the sibling appendMu/opMu memos are locked; only sequential callers today. Not flagged: atomic counters, Windows skips, the abort-on-operational-failure design."
findings:
  - id: F-01
    severity: major
    title: "Fixture setup overrides the hermetic policy's global config"
    status: open
    action: revise
    nodes: [child-validation-hermetic]
    revise:
      contract: "Under a hostile ambient Git configuration, in-process validation in internal/rules and tools/regression and a child sdd validate process leave the sentinel untouched and use only the fixture repository, because each package's TestMain installs the policy before parallel tests and child processes receive an explicit environment; fixture setup commands run by the rules harness inherit the policy's global configuration instead of overriding it, so a prepared fixture reads core.fsmonitor=false and commit.gpgsign=false."
      gate:
        type: tests
        tests:
          - {id: TestInProcessValidationHermetic, file: internal/rules/hermetic_test.go}
          - {id: TestChildValidationHermetic, file: cmd/sdd/hermetic_test.go, satisfies: [user-entrypoint]}
          - {id: TestFixtureSetupInheritsPolicy, file: internal/rules/hermetic_test.go}
  - id: F-02
    severity: minor
    title: "Waiver bookkeeping memo on Root is unguarded next to locked memos"
    status: open
    action: revise
    nodes: [single-pass-evaluation]
    revise:
      gate:
        type: tests
        tests:
          - {id: TestOrdinaryEvaluationOnce, file: internal/rules/evaluate_test.go}
          - {id: TestStrictAndReportingSemanticsPreserved, file: internal/rules/evaluate_test.go, satisfies: [derives-state]}
          - {id: TestWaiverMemoConcurrencySafe, file: internal/rules/evaluate_test.go}
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..421dd78c25c15f92ffdd04c64d12ca42ed80b2e4`.

## Findings
### F-01 — Fixture setup overrides the hermetic policy's global config

`internal/rules/rules_test.go` `setupEnv` still carries `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`, appended after `os.Environ()` in `harness_test.go` `runSetup` and `snapshotFixture`. Go's `exec.Cmd` takes the last duplicate key, so every fixture `Setup` git command runs with the policy's global config (fsmonitor, signing, maintenance, gc pins) replaced by an empty one. A probe under the real package TestMain showed `git config core.fsmonitor` unset in a prepared fixture while the process's `GIT_CONFIG_GLOBAL` pointed at the policy file that sets it to `false`. Only the identity and timestamp variables belong in `setupEnv`.

### F-02 — Waiver bookkeeping memo on Root is unguarded next to locked memos

`Root.bareComputed`/`bareDiagnostics` (used by `bareOnce` for the direct-invocation path of SDD176/SDD177) is a plain read-check-write while the sibling `appendMu` and `opMu` memos added in this range are locked. Not exploitable today (the only production caller is the sequential sweep), but the asymmetry is a maintenance trap; guard it the same way and prove it under `-race`.

## Resolution Log
None.
