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
verdict: Amend
reviewed_planning_revision: "7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "drift-detector (agent, delta faaf81a..7ee8ef6: 18 files across 008c9c1, 46b8a56, 4d1f94a, db2e653, 7ee8ef6): every file maps to one revised contract or the disclosed inline tool fix — absentP4 at internal/vcs/p4.go:116-122 called from RevisionExists (:137), both FileAt branches (:172, :181) and ChangedPaths (:204) with TestP4OperationalErrorsAreNotAbsence at checked_test.go:97-119; TestGitSpawningTestPackagesInstallPolicy at internal/testenv/adoption_test.go:44 with the documented internal/testenv exemption (:33-42) and new TestMain shims in graph/ops, graph/provider, graph/sync, hook, provision; Makefile:146-158 vet target as a prerequisite of test with TestMakeTestRunsVet at tools/testgate/race_test.go:140-175; TestMutationNotRetried at procexec_test.go:158-181 with the mutate-then-fail helper (helper_test.go:77-97). The provision fixture change at git_post_rewrite_test.go:60-66 is the hermetic policy's empty GIT_TEMPLATE_DIR no longer seeding hooks. Six unrevised gate-set nodes have no code change. No missing work, creep or drift. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "quality-scanner (agent, delta faaf81a..7ee8ef6 on Makefile, CLAUDE.md, .gitignore, testgate, testenv, the new main_env_test.go files, hook, provision, procexec): adoption test uses a regex to flag git-spawning test files and a go/parser AST walk to confirm the TestMain declaration, cross-checked against an independent grep of spawning dirs with no divergence; the internal/testenv self-exemption's reason verified against TestHermeticGitPolicy's pre/post-Install assertions (testenv_test.go:159, :163); the provision fixture fix matches InstallPostRewrite's own MkdirAll; make -n test composes check-templates, vet, test-race, test. go vet ./... clean, staticcheck clean on all five touched packages, gofmt -l clean, go test -race -count=1 passes on procexec, testenv, testgate, provision, hook. TestMutationNotRetried is a regression tripwire (Run at procexec.go:95-175 has no loop). No findings. VERDICT: Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "spec-compliance (agent, delta faaf81a..7ee8ef6, three prior gaps re-checked by execution): FR-01/DD-1 — TestGitSpawningTestPackagesInstallPolicy (internal/testenv/adoption_test.go) passes with TestMain installers in internal/graph/{ops,provider,sync}, internal/hook and internal/provision; FR-13 and the fault-injection acceptance no-retry clause — TestMutationNotRetried (procexec_test.go) passes against the real Run, procexec.go comment states the runner never re-executes; NFR-05/AC-11 — Makefile:152 'test: vet' with vet at :156-157 running go vet ./..., TestMakeTestRunsVet passes, go vet ./... clean; full make test run green including template gate, vet and test-race. p4 absentP4 consistent with the operational-failure language and TestP4OperationalErrorsAreNotAbsence passes across all four query paths. No gaps, no contract violations. VERDICT: Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e"
    evidence: "blind-spot-finder (agent, diff only faaf81a..7ee8ef6 on Makefile, .gitignore, testgate, testenv, main_env_test.go files, hook, provision, procexec; go build, go vet, make vet, make test, go test -race ./internal/procexec pass; two scratch proof-of-concept packages built and removed, git status clean): Major — internal/testenv/adoption_test.go:19-24 gitSpawnPattern matches only literal call sites, so a package spawning git through a parameterized wrapper like internal/vcs/vcs_test.go:12's runOK(t, dir, name, args...) is not detected; a scratch package with only that shape and no TestMain passed the inventory. Minor — the regex also matches comment text (a scratch package whose doc comment quoted exec.Command(\"git\") was flagged), which is why adoption_test.go exempts itself. Question — internal/provision/git_post_rewrite_test.go:19-20 still sets GIT_CONFIG_GLOBAL/NOSYSTEM via t.Setenv, now redundant under the package TestMain. procexec exactly-once walked: single cmd.Start, only signal retries against spawned descendants. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: major
    title: "The git-spawn inventory misses spawns routed through a wrapper or helper package"
    status: open
    action: revise
    nodes: [child-validation-hermetic]
    revise:
      contract: "Under a hostile ambient Git configuration, in-process validation in internal/rules and tools/regression and a child sdd validate process leave the sentinel untouched and use only the fixture repository, because each package's TestMain installs the policy before parallel tests and child processes receive an explicit environment; fixture setup commands run by the rules harness inherit the policy's global configuration instead of overriding it, so a prepared fixture reads core.fsmonitor=false and commit.gpgsign=false; every test package in the module that spawns git (including internal/graph/sync, internal/graph/ops and internal/graph/provider) installs the same TestMain, and an inventory test refuses a package without it — the inventory parses sources with go/ast so string literals count and comments never do, treats a test package as git-spawning when any of its test sources carries the literal \"git\" or when it imports (transitively, within the module) a package whose production sources spawn git, and proves on a synthetic module that a wrapper-only test package and a helper-package spawner are both detected."
      gate:
        type: tests
        tests:
          - {id: TestInProcessValidationHermetic, file: internal/rules/hermetic_test.go}
          - {id: TestChildValidationHermetic, file: cmd/sdd/hermetic_test.go, satisfies: [user-entrypoint]}
          - {id: TestFixtureSetupInheritsPolicy, file: internal/rules/hermetic_test.go}
          - {id: TestCorpusSetupInheritsPolicy, file: tools/regression/corpus_test.go}
          - {id: TestGitSpawningTestPackagesInstallPolicy, file: internal/testenv/adoption_test.go}
          - {id: TestGitSpawnInventoryFollowsIndirection, file: internal/testenv/adoption_test.go}
  - id: F-02
    severity: minor
    title: "The inventory regex also matches comment text"
    status: answered
  - id: F-03
    severity: minor
    title: "provision's fixture still sets GIT_CONFIG_GLOBAL by hand under the package TestMain"
    status: rejected
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..7ee8ef6d9e2ddb4ac7490f635ad85fda1f9d3c6e`.

## Findings
### F-01 — The git-spawn inventory misses spawns routed through a wrapper or helper package

`internal/testenv/adoption_test.go:19-24` matches only literal call sites such as `exec.Command("git"`. Two lanes reproduced the gap independently: a scratch package whose test spawns through a `runOK(t, dir, name, args...)` wrapper (the shape `internal/vcs/vcs_test.go:12` already uses), and one whose test calls a `SpawnGit` helper defined in a sibling non-test file; both passed the inventory with no TestMain. No live gap exists today, but the gate's guarantee is weaker than green implies.

### F-02 — The inventory regex also matches comment text

A scratch package whose doc comment quoted `exec.Command("git")` was flagged; the file exempts itself for the same reason.

### F-03 — provision's fixture still sets GIT_CONFIG_GLOBAL by hand under the package TestMain

`internal/provision/git_post_rewrite_test.go:19-20` keeps `t.Setenv` calls the package-level policy now covers.

## Resolution Log
### F-02 — answered

2026-09-13: Subsumed by F-01's revise: parsing with go/ast makes string literals the only match source, so comments can no longer trigger the gate and the self-exemption goes away.

### F-03 — rejected

2026-09-13: Harmless belt-and-suspenders; the package has no t.Parallel so the calls cannot panic, and removing them is hygiene rather than a plan change. Recorded in SDD-DOGFOOD-NOTES.md follow-ups.
