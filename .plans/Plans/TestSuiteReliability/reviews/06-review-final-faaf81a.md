---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..faaf81a383bdd44734d9e45d168fa0bb7b8018c2"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "faaf81a383bdd44734d9e45d168fa0bb7b8018c2"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..faaf81a383bdd44734d9e45d168fa0bb7b8018c2"
    evidence: "drift-detector (agent, whole plan 04c1e61..faaf81a, 99 files +8943/-398 excluding .plans): all 20 verified graph nodes report pass at faaf81a and every recorded artifact digest was recomputed with sha256 against the working tree with zero mismatches; every gate test cited by posix-containment (rev 5) and provision-owned-execution (rev 3) grepped present at its declared file; go vet ./... and make test clean across 37 packages including the -race pass; the nine amendments (seq 47, 48, 70, 85, 86, 102, 103, 116, 117) all cite review artifacts present under reviews/; the commit log decomposes exactly into node commits plus the logged tool fixes 244d34b, 261176c, 63c942e, c9bf9f7 and cleanups 34d1af2..faaf81a; contain_other.go is a refusal stub citing DD-3/pd-52a6f11c, no budget or telemetry code (Non-Goals respected); the rules.test binary removal sits inside c5bf98e. Only housekeeping: README.md:125 node count stale, already P-11. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..faaf81a383bdd44734d9e45d168fa0bb7b8018c2"
    evidence: "quality-scanner (agent, whole plan 04c1e61..faaf81a): go vet ./..., gofmt, staticcheck ./..., GOOS=windows and GOOS=darwin go build ./..., and make test incl. -race all clean; two sub-reads over procexec/testenv/vcs/provision and rules/graph/Makefile/testgate with claims re-validated (the 'IsCause unused' claim was false — internal/graph/provider/operational_test.go:94 calls it). Findings: Critical internal/vcs/p4.go:126-128, :162-164, :171-173, :194-196 rewrap every runP4 failure as ErrNotFound although runP4 (:100-114) wraps non-exit failures in ErrOperational, violating vcs.go:223-227 and, via cache.go:145-152 cacheable(), memoizing a transient p4 failure as absence; no p4 test covers it. Major cmd/sdd/next.go:322-417 duplicates graph.go:1227-1305's payload rendering and swallows input-resolution errors where --claim is fatal. Major internal/provision/git_post_rewrite.go:209-215 classifies 'not a git repository' by rendered text instead of the typed exit cause git.go uses. Minor: seven bare 'review F-0N' comment citations; .gitignore lacks *.test after the 6.8MB rules.test removal. VERDICT: Needs changes."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..faaf81a383bdd44734d9e45d168fa0bb7b8018c2"
    evidence: "spec-compliance (agent, whole plan 04c1e61..faaf81a against the spec, design and pd-4c883a6f/pd-52a6f11c): FR-01..FR-09, FR-13/14 POSIX/15/16, NFR-01, AC-01..AC-04, AC-06, AC-07, AC-11 and DD-1/2/4/6/7/8/9/10 each mapped to code and tests with file:line (testenv.go:44-115, harness_test.go, inventory_test.go:73/:225, evaluate.go:41-71, appendonly_scan_test.go, cache.go:129-153 with TestOperationalFailureNotCached, contain_posix.go, policy.go:9-10, provider.go:83-160, sync.go:298-320, doctor.go:70-160); go build/vet/test and make test green; tools/parity/frozen-expectations.json has zero diff in range. Gaps: Major FR-01/DD-1 — internal/graph/provider/operational_test.go and internal/graph/sync/operational_test.go run real git via exec.Command with no TestMain installing testenv (grep 'func TestMain' finds only cmd/sdd, internal/vcs, internal/rules, internal/procexec, tools/regression); Major NFR-05/AC-11 — go vet and staticcheck appear in no Makefile target or test (grep 'go vet|staticcheck' Makefile empty); Major AC-08 — no TestMutationNotRetried equivalent exists (grep retry/Retried across internal, cmd, tools finds only the unrelated CAS-store retry). No contract violations, no cross-document inconsistencies. VERDICT: Needs changes."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..faaf81a383bdd44734d9e45d168fa0bb7b8018c2"
    evidence: "blind-spot-finder (agent, diff only, whole plan 04c1e61..faaf81a): read procexec.go, contain_posix.go, contain_other.go, wnowait_linux/other.go, vcs/cache.go, rules/evaluate.go, rules/root.go, testenv.go, the four main_env_test.go siblings, rules_test.go, tools/testgate/race_test.go and the fixture helpers in graph/ops ops_test.go, retirement_test.go, remap_test.go, graph/provider provider_test.go and graph/sync operational_test.go; ran go test on vcs, rules, procexec, graph/ops, graph/provider (pass). Major: internal/graph/ops/ops_test.go:330-347 gitOps and internal/graph/provider/provider_test.go:47-65 gitOK/gitFixture spawn git init/commit through raw exec.Command with the inherited environment and no TestMain — reproduced in an isolated dir that commit.gpgsign=true with gpg.program=/bin/false makes git commit exit 128, and that a real gpg is present here so pinentry could hang CI; these packages are named in tools/testgate/race_test.go:12-20 as -race gated. Minor: the bare git init calls in graph/provider/operational_test.go:29 and graph/sync/operational_test.go:57 inherit init.templateDir (hook seeding reproduced), latent today. cache.go and root.go locking checked and found correct. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: critical
    title: "Perforce adapter reports operational failures as ErrNotFound, which the cache then memoizes"
    status: open
    action: revise
    nodes: [checked-detection]
    revise:
      contract: "vcs exposes checked detection returning (Repo, error): a missing or unrunnable SCM binary during a required probe is an operational error, never NoRepo, while a directory that is genuinely not a repository still yields NoRepo; runGit and runP4 execute through procexec, and in both adapters a missing blob, path or revision returns ErrNotFound only after a successful query — an ErrOperational from the runner is never rewrapped as ErrNotFound by RevisionExists, FileAt or ChangedPaths, so the memoization decorator cannot cache a transient failure as absence."
      gate:
        type: tests
        tests:
          - {id: TestVCSOperationalErrors, file: internal/vcs/checked_test.go}
          - {id: TestAuthoritativeSCMAbsence, file: internal/vcs/checked_test.go}
          - {id: TestAbsenceMessagesAreNotDoubled, file: internal/vcs/checked_test.go}
          - {id: TestP4OperationalErrorsAreNotAbsence, file: internal/vcs/checked_test.go}
  - id: F-02
    severity: major
    title: "Graph packages that spawn git in tests do not install the hermetic policy"
    status: open
    action: revise
    nodes: [child-validation-hermetic]
    revise:
      contract: "Under a hostile ambient Git configuration, in-process validation in internal/rules and tools/regression and a child sdd validate process leave the sentinel untouched and use only the fixture repository, because each package's TestMain installs the policy before parallel tests and child processes receive an explicit environment; fixture setup commands run by the rules harness inherit the policy's global configuration instead of overriding it, so a prepared fixture reads core.fsmonitor=false and commit.gpgsign=false; every test package in the module that spawns git (including internal/graph/sync, internal/graph/ops and internal/graph/provider) installs the same TestMain, and an inventory test refuses a package that shells to git in its tests without it."
      gate:
        type: tests
        tests:
          - {id: TestInProcessValidationHermetic, file: internal/rules/hermetic_test.go}
          - {id: TestChildValidationHermetic, file: cmd/sdd/hermetic_test.go, satisfies: [user-entrypoint]}
          - {id: TestFixtureSetupInheritsPolicy, file: internal/rules/hermetic_test.go}
          - {id: TestCorpusSetupInheritsPolicy, file: tools/regression/corpus_test.go}
          - {id: TestGitSpawningTestPackagesInstallPolicy, file: internal/testenv/adoption_test.go}
  - id: F-03
    severity: major
    title: "go vet is not part of the standard gate although the structural-check requirement and full-gate name it"
    status: open
    action: revise
    nodes: [race-gate]
    revise:
      contract: "make test runs go vet ./... and go test -race over the shared-state packages (internal/rules, internal/procexec, internal/vcs, internal/graph/sync, internal/graph/ops, internal/graph/provider, tools/regression) through vet and test-race targets it depends on, so a vet finding, a removed memo or a containment lock fails the standard gate; gate tests parse the Makefile and refuse a test recipe that does not invoke the vet target or the race target over those packages."
      gate:
        type: tests
        tests:
          - {id: TestMakeTestRunsRaceDetector, file: tools/testgate/race_test.go}
          - {id: TestMakeTestRunsVet, file: tools/testgate/race_test.go}
  - id: F-04
    severity: major
    title: "No test asserts that a failed mutating command is not retried"
    status: open
    action: revise
    nodes: [bounded-runner]
    revise:
      contract: "internal/procexec runs a resolved argv without a shell under a context deadline (default 30s) plus a bounded cleanup/drain allowance (5s, clipped to the enclosing deadline), retains a 64 KiB diagnostic excerpt per stream with a truncation flag while draining, enforces a finite machine-output limit whose overflow returns an incomplete-result error never parsed as success, returns typed causes for unavailable executable, deadline, access failure, nonzero exit, overflow and drain failure, and never re-executes a command on its own: a mutating command that fails ambiguously runs exactly once per Run call."
      gate:
        type: tests
        tests:
          - {id: TestDeadlineIsOperational, file: internal/procexec/procexec_test.go}
          - {id: TestOutputOverflow, file: internal/procexec/procexec_test.go}
          - {id: TestLargeMachineOutput, file: internal/procexec/procexec_test.go}
          - {id: TestStderrDrain, file: internal/procexec/procexec_test.go}
          - {id: TestTypedCauses, file: internal/procexec/procexec_test.go}
          - {id: TestMutationNotRetried, file: internal/procexec/procexec_test.go}
  - id: F-05
    severity: major
    title: "provision classifies 'not a git repository' from rendered error text"
    status: rejected
  - id: F-06
    severity: major
    title: "next --show duplicates the --claim payload rendering and swallows input-resolution errors"
    status: rejected
  - id: F-07
    severity: minor
    title: "Source comments cite bare review finding ids"
    status: rejected
  - id: F-08
    severity: minor
    title: ".gitignore does not exclude compiled *.test binaries"
    status: answered
  - id: F-09
    severity: minor
    title: "Bare git init in graph operational tests inherits init.templateDir"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..faaf81a383bdd44734d9e45d168fa0bb7b8018c2`.

## Findings
### F-01 — Perforce adapter reports operational failures as ErrNotFound, which the cache then memoizes

`internal/vcs/p4.go:126-128`, `:162-164`, `:171-173` and `:194-196` rewrap every `runP4` failure as `ErrNotFound`, although `runP4` (`:100-114`) wraps a missing binary, timeout, permission failure or overflow in `ErrOperational`. `vcs.go:223-227` states an operational error must never be read as absence, `git.go:106-110` honours that through `absent()`, and `cache.go:145-152` treats `ErrNotFound` as cacheable — so one transient p4 hiccup is memoized as "revision does not exist" for the rest of the process. No p4 test covers these paths.

### F-02 — Graph packages that spawn git in tests do not install the hermetic policy

`internal/graph/ops/ops_test.go:330-347`, `internal/graph/provider/provider_test.go:47-65`, `internal/graph/provider/operational_test.go:29` and `internal/graph/sync/operational_test.go:57` run `git init`/`git commit` through raw `exec.Command` with the inherited environment; none of the three packages has a `TestMain`. Reproduced: `commit.gpgsign=true` with an unusable `gpg.program` makes the fixture commit exit 128, and a real gpg would hang CI on pinentry. These packages are the ones `tools/testgate/race_test.go:12-20` names as race-gated. Two lanes found this independently.

### F-03 — go vet is not part of the standard gate although the structural-check requirement and full-gate name it

`grep -n "go vet\|staticcheck" Makefile` is empty and no test wires vet; the full gate only passed vet because the walk ran `go vet ./...` by hand before `make test`.

### F-04 — No test asserts that a failed mutating command is not retried

The spec's acceptance row for fault injection names `TestMutationNotRetried`; `grep -rn retry internal/procexec` finds nothing, so the property holds by construction but nothing pins it.

### F-05 — provision classifies 'not a git repository' from rendered error text

`internal/provision/git_post_rewrite.go:209-215` matches `strings.Contains(err.Error(), "not a git repository")` while `internal/vcs/git.go` switches on the typed `procexec` cause.

### F-06 — next --show duplicates the --claim payload rendering and swallows input-resolution errors

`cmd/sdd/next.go:322-417` and `cmd/sdd/graph.go:1227-1305` declare the same local types and print the same payload; `next.go:352-361` ignores an input-resolution error that `graph.go:1249-1257` treats as fatal.

### F-07 — Source comments cite bare review finding ids

Seven comments in `cmd/sdd/root.go`, `internal/procexec/containment.go`, `contain_posix.go` and `procexec.go` say "review F-01/F-02" without naming the artifact.

### F-08 — .gitignore does not exclude compiled *.test binaries

The diff removes a committed 6.8 MB `rules.test`; nothing prevents the next `go test -c` output from being added again.

### F-09 — Bare git init in graph operational tests inherits init.templateDir

Latent today (no commit follows the init); resolved by the same TestMain adoption as F-02.

## Resolution Log
### F-05 — rejected

2026-09-13: The exit code alone (128) cannot distinguish "not a repository" from git's other fatal errors, so the stderr text is the discriminator and git has printed it unchanged for years; provision's caller only uses it to choose a friendlier message. Recorded in SDD-DOGFOOD-NOTES.md follow-ups to revisit if procexec ever carries a structured git classification.

### F-06 — rejected

2026-09-13: `cmd/sdd/next.go` is sdd tool code landed as the inline fix 261176c outside the graph's node set; the duplication and the show-vs-claim asymmetry are recorded in SDD-DOGFOOD-NOTES.md follow-ups for the next tool pass, not a plan revision.

### F-07 — rejected

2026-09-13: Comment style; every cited comment already states the reason in prose. Follow-up recorded in SDD-DOGFOOD-NOTES.md to cite artifact paths instead of bare ids.

### F-08 — answered

2026-09-13: Non-normative hygiene; `*.test` is added to `.gitignore` in the inline commit that accompanies this amendment's F-02 work.

### F-09 — answered

2026-09-13: Subsumed by F-02's revise of `child-validation-hermetic`: installing `testenv.Main` in `internal/graph/sync` and `internal/graph/provider` blanks `GIT_TEMPLATE_DIR` and pins `init.templateDir` for those inits too.
