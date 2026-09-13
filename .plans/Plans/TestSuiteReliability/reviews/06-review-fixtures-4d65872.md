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
verdict: Amend
reviewed_planning_revision: "4d6587238de8b72301d3bef22c242d9b56f0da84"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "drift-detector (agent, delta 7ee8ef6..4d65872, 11 files, four commits): only f9be2ec belongs to this gate's set and it implements child-validation-hermetic rev 5 clause by clause in internal/testenv/adoption_test.go — parser.ParseFile replaces the regex, fileHasGitLiteral matches only ast.BasicLit so comments never match, fileCallsGitSpawner checks exec.Command/CommandContext/LookPath and procexec.Run/LookPath, reachesSpawner is a memoized DFS over module imports; TestGitSpawnInventoryFollowsIndirection asserts the synthetic offender set is exactly {a, b}; both inventory tests pass. The five new main_env_test.go files differ from internal/vcs's only in the package clause; compile, convert, intent and review transitively import internal/rules, and model is flagged by the \"git\" literal in model_test.go:33,168,252 fixture JSON — the contract's literal rule working as written. Graph shows contract_rev 5 with pass at 4d65872. Phases 2 and 3 have no touched files. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "quality-scanner (agent, delta 7ee8ef6..4d65872 on testenv, the five graph main_env_test.go files and rules; go vet, gofmt -l, staticcheck, go test -race clean on testenv (1.4s) and rules (6.9s)): the memoized reachability walk (visiting cycle guard, reaches memo) traced sound; both inventory tests exercise wrapper, helper-package, comment-only and TestMain-suppressed cases; recordingRepo.note returns the original error so errors.Is sees absentP4's chain. Minor: internal/rules/evidence.go:599-604 suppresses the diagnostic for any error other than ErrNotFound, so vcs.ErrUnsupported from p4.go:131-134 (RevisionSyntaxValid) would now be swallowed silently — unreachable today only because the caller's p4RevisionRe (evidence.go:69) mirrors the adapter's regex; suppress on ErrOperational only. Minor: adoption_test.go:42-51 and :289-325 match spawn calls by identifier name without resolving import aliases and parse build-tagged files unconditionally; no aliased import or tagged spawner exists in the module today. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "spec-compliance (agent, delta 7ee8ef6..4d65872, commits 82bc0f1, ab07682, f9be2ec, 4d65872): FR-01/DD-1 — internal/testenv/adoption_test.go now scans with go/ast and an import-graph walk (gitSpawnOffenders, fileHasGitLiteral, fileCallsGitSpawner, reachesSpawner) and TestGitSpawnInventoryFollowsIndirection (:754-855) proves wrapper, helper-package, comment-only and TestMain-suppressed cases on a synthetic five-package module; DD-10/FR-16 — internal/rules/evidence.go:599-604 returns without the diagnostic when RevisionExists fails other than ErrNotFound (the recordingRepo at root.go:208-217 already recorded the failure) with TestP4IdentityQueryFailureIsOperational (:307-351) covering both branches; remap.go:177-187 split confirmed with TestRemapRevisionsQueryFailureIsOperational. go build ./... and go test on graph, rules, testenv pass. Noted: verifyCleanGitIdentity at internal/rules/evidence.go:570 still reads 'ok, _ := repo.RevisionExists' — the Git twin of the fixed check, outside the two confirmed items. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84"
    evidence: "blind-spot-finder (agent, diff only 7ee8ef6..4d65872; ran the real gitSpawnOffenders against three synthetic attack packages under t.TempDir via a scratch TestSynthAttackVectors, scratch removed, tree clean): Major — fileHasGitLiteral (adoption_test.go:262-283) sees only an unsplit \"git\" BasicLit, so const gitBin = \"g\" + \"it\" with exec.Command(gitBin, ...) and no TestMain yields offenders = []; Major — fileCallsGitSpawner (:289-325) requires a bare exec/procexec identifier and a literal argument, so a production helper with var run = exec.Command (or an import alias) never seeds prodSpawns and its importers are not flagged (offenders = []). No current file uses either pattern. verifyCleanGitIdentity's discards at evidence.go:570,578 traced as unreachable false positives because recordingRepo (root.go:215-218) records first and evaluate.go:51-75 discards the sweep. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: major
    title: "The git-spawn inventory can still be bypassed by concatenated names, aliased spawners and wrapper functions"
    status: open
    action: revise
    nodes: [child-validation-hermetic]
    revise:
      contract: "Under a hostile ambient Git configuration, in-process validation in internal/rules and tools/regression and a child sdd validate process leave the sentinel untouched and use only the fixture repository, because each package's TestMain installs the policy before parallel tests and child processes receive an explicit environment; fixture setup commands run by the rules harness inherit the policy's global configuration instead of overriding it, so a prepared fixture reads core.fsmonitor=false and commit.gpgsign=false; every test package in the module that can spawn a process installs the same TestMain, and an inventory test refuses a package without it — the inventory resolves packages by import path only: a test package is spawning when any of its test sources import os/exec or the module's procexec package, or when the package or its test sources transitively import (within the module) a package whose production sources import either, so identifier names, string literals and comments play no part and wrappers, import aliases, concatenated binary names, variable-named spawners and helper packages cannot escape; a synthetic-module proof covers each of those shapes."
      gate:
        type: tests
        tests:
          - {id: TestInProcessValidationHermetic, file: internal/rules/hermetic_test.go}
          - {id: TestChildValidationHermetic, file: cmd/sdd/hermetic_test.go, satisfies: [user-entrypoint]}
          - {id: TestFixtureSetupInheritsPolicy, file: internal/rules/hermetic_test.go}
          - {id: TestCorpusSetupInheritsPolicy, file: tools/regression/corpus_test.go}
          - {id: TestGitSpawningTestPackagesInstallPolicy, file: internal/testenv/adoption_test.go}
          - {id: TestGitSpawnInventoryFollowsIndirection, file: internal/testenv/adoption_test.go}
          - {id: TestSpawnInventoryIsImportBased, file: internal/testenv/adoption_test.go}
  - id: F-02
    severity: major
    title: "A second evaluation from a fresh root in the same process loses traceability findings"
    status: open
    action: revise
    nodes: [prepare-once-determinism]
    revise:
      contract: "Each Bad example's determinism case prepares files and Git history exactly once, evaluates four independently loaded roots, compares complete ordered diagnostics (code, severity, path, line, message, correction, implicated, waived reason) so a drifting message fails even when codes match, and leaves fixture bytes, index and HEAD unchanged; two evaluations of distinct fresh roots inside one process, in either order, produce identical diagnostics because no memo keyed by a root-relative path or artifact identity outlives its Root."
      gate:
        type: tests
        tests:
          - {id: TestDeterminismPreparationCount, file: internal/rules/harness_test.go}
          - {id: TestCompleteDiagnosticComparison, file: internal/rules/harness_test.go, satisfies: [deterministic-replay]}
          - {id: TestValidationLeavesFixtureUnchanged, file: internal/rules/harness_test.go}
          - {id: TestRunIsDeterministic, file: internal/rules/rules_test.go}
          - {id: TestFreshRootsAreIndependentInProcess, file: internal/rules/harness_test.go}
  - id: F-03
    severity: minor
    title: "The Perforce identity check now swallows ErrUnsupported along with operational errors"
    status: answered
  - id: F-04
    severity: minor
    title: "The inventory matches spawn calls by identifier name and ignores build tags"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..4d6587238de8b72301d3bef22c242d9b56f0da84`.

## Findings
### F-01 — The git-spawn inventory can still be bypassed by concatenated names, aliased spawners and wrapper functions

Three lanes defeated the rev 5 detector with real `gitSpawnOffenders` runs on synthetic modules: `const gitBin = "g" + "it"` in a test file, `var run = exec.Command` in a helper package, and the `execRunner(dir, name, ...)` shape `internal/graph/provider/provider.go:89-98` already uses, all returned an empty offender set. Any rule that inspects names or literals has a hole; resolving by import path (`os/exec`, `internal/procexec`) does not.

### F-02 — A second evaluation from a fresh root in the same process loses traceability findings

`go test -count=2 ./internal/rules` fails `TestExamplesBehaveAsDeclared/SDD161/bad/design-conflates-same-numbered-id` and `TestSDD161ConflationRefusedPerSpec` on the second run (findings empty), reproduced by the orchestrator at HEAD and by a lane at the prior identity. A memo keyed by something that repeats across fresh roots survives its Root.

### F-03 — The Perforce identity check now swallows ErrUnsupported along with operational errors

`internal/rules/evidence.go:599-604` returns silently for any error other than `ErrNotFound`, so `vcs.ErrUnsupported` from `p4.go:131-134` would lose its diagnostic; unreachable today only because the caller's regex mirrors the adapter's.

### F-04 — The inventory matches spawn calls by identifier name and ignores build tags

`internal/testenv/adoption_test.go:42-51` and `:289-325`.

## Resolution Log
### F-03 — answered

2026-09-13: Folded into the sibling execution-gate review at this identity (`Plans/TestSuiteReliability/reviews/06-review-execution-4d65872.md`, F-01), whose revise of `propagation-validator` makes every identity check suppress only `ErrOperational`.

### F-04 — answered

2026-09-13: Subsumed by F-01: an import-path rule has no identifier matching to alias, and files excluded by build tags still import the package, which is the conservative answer.
