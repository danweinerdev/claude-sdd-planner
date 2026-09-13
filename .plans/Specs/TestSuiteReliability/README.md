---
title: "Test Suite Reliability"
type: spec
status: approved
created: 2026-09-08
updated: 2026-09-13
tags: [testing, reliability, performance, git, graph]
related: [Specs/SDD-Toolchain, Designs/SddGraph, Designs/TestSuiteReliability]
---

# Test Suite Reliability

## Overview
Make the complete Go test suite fast enough for ordinary development and predictable on Git-configured Windows, macOS, and Linux hosts without reducing validation coverage. This initiative covers all five workstreams: hermetic Git execution; prepare-once determinism fixtures and pure-logic separation; elimination of redundant validation/history work; explicit concurrency budgets; and bounded, owned subprocess execution with useful failure telemetry.

This is proposed scope, not an implementation result. The user requested specification and design followed by graph-based execution. Approval of these documents precedes graph compilation; no implementation or graph completion is asserted here.

### Evidence and uncertainty
Evidence was collected on 2026-09-08 against baseline revision `7cf2572`. Paths below are repository-relative.

| Observation | Evidence | Limit of claim |
|---|---|---|
| Full suite can exceed forty minutes | User report; session `make test` attempts exceeded 10- and 20-minute command bounds | Not a completed timing sample or measured median |
| Rules tests wait on Git | Bounded `internal/rules` runs timed out in `TestRunIsDeterministic`; stacks included `internal/vcs.runGit` and `FileInIndex` | Does not attribute every stall to one cause |
| System fsmonitor enabled; 95 daemon processes observed | Read-only Git configuration and process inventory in the session | Individual daemon provenance was not established; not permission to kill them |
| Setup and validation environments differ | `internal/rules/rules_test.go:129-170`; `internal/vcs/git.go:70-80` | Setup supplies environment overrides; validation inherits the test process environment |
| Determinism repeats preparation four times per Bad example | `internal/rules/rules_test.go:106-127` calls `runExample`, which creates directories/files/history | Exact aggregate counts must be measured from the live registry, not grep syntax or stale comments |
| Shared work repeats | `internal/rules/rules.go:158-185`, `waivers.go:233-277`, `appendonly.go:363-376` | Existing `Root.repoCache` and `bareOnce` already remove some repetition; do not reimplement them blindly |
| Corpus and subtests have independent concurrency | `tools/regression/corpus.go:120-158`; `rules_test.go:65-127`; Go package scheduling | CPU count is not a suitable standalone Git process budget |

## Goals
- Isolate test-owned Git activity from workstation configuration and prevent new background-service leaks.
- Reduce fixture construction and Git process amplification while preserving real SCM boundary tests and full diagnostic semantics.
- Bound resource use and failure latency, distinguish inability to validate from an authoritative negative result, and report enough evidence to diagnose either.
- Execute this initiative through the existing observation-gated graph protocol with stable requirement and acceptance-criterion citations.

## Non-Goals
- No removal of the full `make test` gate, required Good/Bad examples, frozen corpus checks, template checks, portable drift/leak checks, or existing portability obligations.
- No regeneration of frozen expectations to hide changes; no diagnostic compatibility change for successful SCM operations.
- No workstation-wide configuration mutation, arbitrary daemon cleanup, external service dependency, or live Git/P4 server requirement.
- No blanket hermetic environment imposed on production commands; test isolation is test-owned. Shared validation execution/error handling changes are in scope where necessary to prevent hangs or false success.
- No replacement of SDD's graph scheduler, compiler, state derivation, or review protocol; no new external work tracker.
- No universal wall-clock promise across unspecified hardware, and no test-result cache hits counted as completed test execution.

## Requirements

### Functional Requirements
#### A. Hermetic Git environment

- **FR-01**: Every test-owned Git fixture SHALL be created under an owned temporary root. A common test environment policy SHALL cover both fixture setup and Git launched by code under test, including in-process validator calls and child `sdd` processes. Production environment behavior outside tests SHALL remain unchanged.
- **FR-02**: The policy SHALL exclude ambient system/global Git configuration, inherited configuration injection and repository-routing variables, templates, hooks, signing, filters, credential/prompt helpers, automatic maintenance and fsmonitor activation. Required platform process-launch variables SHALL be retained. Fixture identity, timestamps, line-ending policy and initial-branch behavior SHALL be explicit and compatible with existing frozen fixtures. Null/config paths SHALL be platform-correct; tests SHALL not rely on an unverified assumption about `/dev/null` on Windows.
- **FR-03**: Environment-dependent tests SHALL use explicit temporary configuration and isolated child processes, never mutate process-wide environment while parallel tests run, and never edit the workstation's configuration. Non-P4 fixtures SHALL retain irrelevant-P4-probe suppression; the complete platform-independent suite SHALL remain offline. Intentional configuration behavior SHALL have separate positive tests so hermeticity does not hide it.

#### B. Fixture reuse and meaningful determinism

- **FR-04**: Within each Bad example's determinism case, file and Git setup SHALL run exactly once, followed by four evaluations using independently loaded `Root` objects. Cases SHALL not share mutable fixtures, loaded roots, or observations. Read-only validation SHALL leave fixture content, index, and history unchanged.
- **FR-05**: Repeatability SHALL compare the complete ordered diagnostic values, including code, severity, path, line, message, correction and any additional fields. Cross-root comparisons MAY normalize only enumerated temporary-root substitutions. Dedicated tests SHALL exercise evaluation-order independence and independent fixture reproducibility; neither shall be claimed from repeating the same ordered execution alone.
- **FR-06**: Pure rule transformations SHALL be testable without Git, P4, or subprocesses. A documented, non-vacuous targeted selection SHALL execute these tests. Real-SCM tests SHALL retain coverage of detection, staged/worktree/history differences, absence versus execution errors, linked worktrees, relevant path edge cases and mutation-between-validation behavior. A coverage inventory SHALL map moved/restructured assertions to their replacements; file renaming alone is not a test selector.

#### C. Eliminate redundant validation work

- **FR-07**: Each ordinary root check SHALL execute once per validation evaluation and each ordinary artifact check once per applicable artifact. Waiver bookkeeping SHALL consume those findings rather than trigger an additional ordinary sweep. Strict `Run` and reporting `RunWithWaivers` SHALL preserve their distinct current semantics and canonical diagnostic order.
- **FR-08**: Shared rule-family history scans, beginning with append-only history, SHALL execute once per evaluation scope and distribute their findings to the relevant rule codes. Subsequent validation of a newly loaded root after HEAD/index/worktree changes SHALL observe the new state. Existing detection and parsing caches SHALL be reused where sound rather than duplicated.
- **FR-09**: Mutable index/worktree observations SHALL never be cached beyond their validation scope. Immutable object caching, if introduced, SHALL key on resolved immutable object identity and repository identity rather than movable names such as HEAD. Transient operational errors SHALL not become cached negative facts. Reusing a loaded root after external mutation is outside its existing immutable-root contract; callers requiring freshness SHALL reload it.

#### D. Concurrency and process budgets

- **FR-10**: The authoritative full gate SHALL explicitly budget concurrently executing test packages, parallel cases, corpus workers and active owned SCM command trees. The initial proposed profile is two packages, four parallel cases and four corpus workers per package, with at most four active SCM command trees per package (aggregate ceiling eight for the full gate). Overrides SHALL be positive, validated, finite, recorded, and reflected in the aggregate ceiling. CPU count alone SHALL not determine these limits.
- **FR-11**: The budget SHALL include SCM activity launched through production code under test and nested helper/CLI processes, not only fixture helpers. Admission waiting SHALL be cancellable; failure and cancellation SHALL release capacity. The implementation SHALL prevent nested-execution deadlock. A per-process semaphore SHALL not be represented as a cross-process limiter; the design SHALL define accounting ownership and test its aggregate claims.
- **FR-12**: Direct `go test ./...` SHALL still select the complete suite and retain hermeticity, per-command deadlines and per-package limits. Its aggregate bound SHALL be documented in terms of the caller's package parallelism; the stronger fixed aggregate profile belongs to `make test`. Developer subsets MAY supplement, never replace, the full gate. Go toolchain/compiler processes SHALL be distinguished from the SCM command-tree budget.

#### E. Deadlines, ownership, output and diagnostics

- **FR-13**: Test-owned command execution and shared read-only validation SCM execution SHALL have finite admission/execution deadlines and bounded pipe-drain/cleanup time. Proposed defaults are a 30-second command deadline and a 5-second cleanup/drain allowance, constrained by the enclosing test/request deadline. Long-running test tools require explicit finite policies rather than an indiscriminate five-second limit. Commands SHALL be started without a shell; mutations SHALL never be retried automatically after ambiguous failure.
- **FR-14**: The runner SHALL establish ownership before a command can create descendants and SHALL clean up owned processes on normal completion, failure, timeout and enclosing cancellation. Windows SHALL use a verified containment mechanism; a console process group alone is insufficient. POSIX cleanup SHALL target only an owned process group with an explicit supported-descendant contract. Escaping/background services SHALL be prevented in ordinary fixtures; unsupported containment SHALL fail explicitly rather than silently weaken the guarantee. No PID/name-based sweep over unrelated workstation processes is permitted.
- **FR-15**: Command output handling SHALL retain bounded diagnostic output, continue safe draining or cancel on overflow, and never parse truncated machine output as a successful result. A bounded reader alone SHALL not leave a child blocked on a full pipe. Machine-output and diagnostic-output policies SHALL be distinct so valid large repository data is not silently truncated.
- **FR-16**: Missing executables, denied access, corrupt/unreadable repositories, cancellation, timeout, output overflow, pipe-drain failure and containment failure SHALL propagate as operational failures through VCS adapters, validation and CLI boundaries. They SHALL not become missing files/revisions, a plain-tree fallback, a skipped check, a waived finding, or a successful validation. Authoritative absence and ordinary negative SCM predicates SHALL retain their existing behavior; existing CLI operational exit code 2 and authoritative-finding exit code 1 SHALL remain distinct.
- **FR-17**: Test diagnostics SHALL identify the owning test/fixture, sanitized command category/arguments, queue/execution/cleanup durations, exit/failure class, output truncation and ownership cleanup outcome. A local machine-readable report SHALL record process starts, peak admitted concurrency and durations by command category. Environment values, credentials and machine-specific absolute paths SHALL not be persisted in planning artifacts. Reports SHALL use explicit root aliases and field-level redaction, not discard arbitrary diagnostic differences. Production success output SHALL not gain unsolicited telemetry.

### Non-Functional Requirements
- **NFR-01**: The full correctness gate SHALL remain offline and complete on supported hosts. Frozen expectations and required rule examples SHALL remain unchanged in meaning; any intentional assertion relocation SHALL have an explicit equivalence record. No global Git configuration, user repositories, or unrelated services may be changed by tests.
- **NFR-02**: Proposed performance acceptance is a median below 60 seconds for the fixed targeted command and below 300 seconds for the full gate on reference host `windows-reference-01`. This denotes the current development workstation, not an invented hardware model. The measurement report SHALL capture its non-sensitive OS/CPU/RAM/storage/toolchain descriptors before baseline sampling and reuse the same host/profile for candidates. Approval accepts these targets, not a claim they have been achieved.
- **NFR-03**: Warm means Go build cache populated but test-result caching disabled for every timed sample (`-count=1`). Collect three baseline and three candidate samples with identical command, fixture/test inventory and explicit concurrency profile; report median and individual results. Cold-build samples are separate. If a baseline exceeds the fixed 15-minute sample bound, record a censored timeout rather than a fabricated duration or median; candidate absolute targets still apply. Performance is a dedicated acceptance report, not a flaky universal per-test wall-clock assertion.
- **NFR-04**: Deterministic gates SHALL assert zero surviving supported owned descendants, the declared concurrency ceilings, one fixture preparation per determinism case, one ordinary evaluation/shared scan per scope, and full diagnostic equivalence. Process-start counts SHALL be reported before/after and SHALL not grow on the fixed representative workload; wall time cannot substitute for these structural checks.
- **NFR-05**: Go structural checks SHALL include `go vet ./...`, race-enabled tests for shared state/concurrency on supported runners, existing portability checks, and `staticcheck` when available. A missing platform/race prerequisite SHALL be reported and cannot be presented as successful verification.

## User Stories
- As a developer, I want a Git-configured workstation to run tests without starting a daemon per temporary repository or contacting my real SCM services.
- As a maintainer, I want repeated validation to prove stable complete diagnostics without reconstructing the same history four times.
- As an implementer, I want fast pure-logic feedback while the full gate still exercises real Git behavior.
- As a reviewer, I want hangs and missing infrastructure reported as operational failures, not mistaken for missing evidence or clean results.
- As a graph executor, I want each improvement linked to acceptance tests and observed red/green results, rather than prose claiming that performance is fixed.

## Acceptance Criteria
- [ ] **AC-01**: Hostile temporary system/global/config-injection settings enable a fsmonitor or hook sentinel; setup, in-process validation and child CLI validation leave the sentinel untouched, use only the fixture repository, preserve explicit fixture identity, and create no owned background service. Intentional-config tests prove their explicit fixture setting still takes effect. (FR-01, FR-02, FR-03)
- [ ] **AC-02**: Instrumented determinism cases record one setup and four fresh-root evaluations; a deliberately drifting message/line/correction fails comparison even when codes match. At least one independently constructed real-Git fixture pair and one explicit rule-order permutation are compared. Fixture bytes/index/history remain unchanged by validation. (FR-04, FR-05)
- [ ] **AC-03**: The pure selection executes a nonzero declared test inventory with SCM unavailable and zero SCM process starts; the full suite still executes all mapped real-SCM boundary assertions and every registered Good/Bad example. (FR-06, FR-12, NFR-01)
- [ ] **AC-04**: Invocation counters prove one ordinary check per applicable root/artifact and one append-only family scan per evaluation. Strict/reporting/waiver fixtures preserve complete expected diagnostics, severity and order. Reload-after-HEAD/index/worktree-mutation cases reject stale cache results; transient operational errors are not cached as absence. (FR-07, FR-08, FR-09)
- [ ] **AC-05**: Synchronized contention tests cover fixture, validator, nested CLI and corpus paths across at least two package-like test processes. Peak active SCM command trees stays within the declared profile, cancelled waiters start no command, nested activity cannot deadlock, and all slots are reusable after failure. Report distinguishes per-package and aggregate limits. (FR-10, FR-11, FR-12)
- [ ] **AC-06**: Helper processes exercise hang, early-parent-exit with inherited pipes, descendant creation and cleanup failure on Windows and POSIX. Operations return within their configured execution plus cleanup bounds, normal completion cleans owned descendants too, and an unrelated sentinel process remains alive. Unsupported containment fails explicitly; no broad daemon cleanup occurs. (FR-13, FR-14)
- [ ] **AC-07**: Infinite stdout/stderr and valid-large-machine-output fixtures prove bounded retention, continued draining/cancellation, no truncated successful parse, bounded completion and a specific overflow/drain error where applicable. (FR-15)
- [ ] **AC-08**: Fault injection for executable absence, permission/corruption, timeout and drain/containment failure reaches validation/CLI as an operational failure with exit 2, never clean/absent/waived/plain-tree fallback. Genuine missing index blobs/revisions and negative ancestry preserve their existing authoritative behavior. Mutation failure is not retried. (FR-13, FR-16)
- [ ] **AC-09**: Reports associate commands with the correct fixture, accurately count starts and peak admission, distinguish queue/run/cleanup time and errors, and redact seeded sensitive values and root paths without altering compared semantic fields. Concurrent reports remain parseable; production success output is unchanged. (FR-17)
- [ ] **AC-10**: On `windows-reference-01`, the documented fixed targeted and full commands meet NFR-02 using NFR-03, with unchanged required test inventory, no leaked supported owned descendants, no structural-budget violation, and reported process counts. Censored baseline data remains explicitly censored. (NFR-01, NFR-02, NFR-03, NFR-04)
- [ ] **AC-11**: `make test`, uncached complete Go suite, Go structural checks, template checks, portable gates and existing portability obligations pass, with Windows and POSIX process-lifecycle evidence captured separately. Missing evidence leaves closure blocked. (NFR-01, NFR-05)
- [ ] **AC-12**: Implementation uses a binary-compiled plan graph citing this spec and the related design. Hazard-discharging tests have observed failing baselines before passing observations count; completion derives from synced reports and a frozen Aligned four-lane review whose aggregate diff still matches, never manual graph/view edits or asserted status. (SddGraph:pd-b9031144, SddGraph:pd-312da877)

## Constraints
- SDD-Toolchain:pd-ddb1f0bb and `SDD-Toolchain:NFR-04` preserve Go validation and the complete offline test contract. SDD-Toolchain:pd-27391740 preserves reviewer read-only boundaries. SDD-Toolchain:pd-5ea9a4ae preserves canonical/generated portability boundaries. SDD-Toolchain:pd-f4a8d1a2 preserves user-owned binary installation; this feature does not provision binaries.
- SddGraph:pd-b9031144 governs red-before-green observations and frozen four-lane review closure; SddGraph:pd-312da877 keeps work tracking in the planning root. SddGraph:pd-2c12dec8 governs lifecycle bookkeeping boundaries. SDD-Toolchain:pd-bac0b279 and SDD-Toolchain:pd-eed0dbed forbid silent decision collisions or ledger writes without exact-text approval.
- This work does not change the compact skill roster, artifact storage model, setup, review freezing or other unrelated accepted decisions. No new ledger entry is required merely to restate these component-local proposals.
- The authoritative frozen expectations are never regenerated for this optimization. `make gen-fixtures` is not permission to rewrite expectation verdicts.

## Dependencies
- Related `Specs/SDD-Toolchain`, `Designs/SddGraph` and the accompanying `Designs/TestSuiteReliability`; current Go module/toolchain and local Git executable. No external SCM service is required.
- External contract pins, retrieved 2026-09-08: [Go os/exec, go1.26.0](https://pkg.go.dev/os/exec@go1.26.0), specifically `CommandContext`, `Cancel`, `WaitDelay` and output ownership; [Git environment, 2.53.0](https://git-scm.com/docs/git/2.53.0); [Win32 Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects) and [Process Creation Flags](https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags), both source revision `3aee48ac3f48e187d17bdfd56951741737331749` (2025-07-14 documentation); [POSIX kill, Issue 8 / IEEE 1003.1-2024](https://pubs.opengroup.org/onlinepubs/9799919799/functions/kill.html). These pin semantics, not an unapproved minimum Git/Windows version increase.
- In particular, Go `CommandContext` defaults to killing the direct child and leaves `WaitDelay` unset; it does not promise descendant cleanup. Windows console process groups are not Job Object containment. POSIX group signalling does not include descendants that leave the group.

## Open Questions
- Is another finite concurrency profile faster on the reference host? — **non-blocking** — the initial profile and acceptance targets are fixed proposed defaults; measurement may recommend a separately reviewed change, never silently widen a passing gate.
- Which of the 95 observed daemons came from earlier tests? — **non-blocking** — no requirement depends on attribution: prevention and owned-process lifecycle tests are mandatory, and unrelated daemons are never swept.
- Which workstreams execute on a non-Windows host first? — **non-blocking** — `Plans/TestSuiteReliability` (2026-09-13) builds workstreams A-C and the POSIX parts of E; workstream D, Windows containment, telemetry and the `windows-reference-01` protocol are a follow-on plan, so the uncovered criteria are deferred by design, not forgotten.
