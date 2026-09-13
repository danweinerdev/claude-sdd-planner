---
title: "Propagation"
type: phase
plan: "TestSuiteReliability"
phase: 5
status: complete
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 4 node(s) under phase label Propagation"
tasks: []
---

# Phase 5: Propagation

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

<!-- FROZEN VIEW — every node in this phase is closed: GREEN and covered by a passing frozen full review gate. This projection is history; a render that would change it is refused. -->

## Overview

Rendered view of 4 node(s) from the plan graph (schema v1, seq 494).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### checked-detection

- Contract: vcs exposes checked detection returning (Repo, error): a missing or unrunnable SCM binary during a required probe is an operational error, never NoRepo, while a directory that is genuinely not a repository still yields NoRepo; runGit and runP4 execute through procexec, and in both adapters a missing blob, path or revision returns ErrNotFound only after a successful query — an ErrOperational from the runner is never rewrapped as ErrNotFound by RevisionExists, FileAt or ChangedPaths, so the memoization decorator cannot cache a transient failure as absence.
- Justifies: `Specs/TestSuiteReliability:FR-16`, `Specs/TestSuiteReliability:AC-08`, `Designs/TestSuiteReliability:DD-10`
- Depends on: `bounded-runner`
- Gate: tests — `TestVCSOperationalErrors` in internal/vcs/checked_test.go; `TestAuthoritativeSCMAbsence` in internal/vcs/checked_test.go; `TestAbsenceMessagesAreNotDoubled` in internal/vcs/checked_test.go; `TestP4OperationalErrorsAreNotAbsence` in internal/vcs/checked_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/vcs/vcs.go, internal/vcs/git.go, internal/vcs/p4.go, internal/vcs/checked_test.go, internal/vcs/vcs_test.go
- Estimate: 2
- Observation: **pass** at seq 476 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### cache-error-policy

- Contract: The vcs memoization decorator never caches an operational failure or a failed detection; a determinate ErrNotFound remains cacheable; a probe that fails is never stored as NoRepo.
- Justifies: `Specs/TestSuiteReliability:FR-09`, `Specs/TestSuiteReliability:AC-04`, `Designs/TestSuiteReliability:DD-7`
- Depends on: `checked-detection`
- Gate: tests — `TestOperationalFailureNotCached` in internal/vcs/cache_test.go; `TestDeterminateAbsenceCached` in internal/vcs/cache_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/vcs/cache.go, internal/vcs/cache_test.go
- Estimate: 1
- Observation: **pass** at seq 477 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### propagation-validator

- Contract: Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection, VerifyRetirementSource wraps a failed post-detection RevisionExists so ErrOperational survives errors.Is, the retirement rule records an operational VerifyRetirementSource failure on the Root and stays silent instead of emitting it as a retirement problem, and the exported retirement-problems entry point never discards an operational failure — it returns it as an error alongside the substantive problems so no caller can read an unanswered verification as a clean result; every repository query made from a rule callback in internal/rules that can emit or suppress a diagnostic — RevisionExists, IsAncestor, Clean, Head, RevisionsAfter, ChangedPaths and FileAt, including the review-committed check and the lifecycle-normalized content loads behind the intent comparison and the planning-revision load — emits an absence or state diagnostic only when the query succeeded and answered negatively (or the revision syntax is unsupported) and stays silent on ErrOperational, which the collector already holds; an operational plan lookup inside the evidence-committed checks suppresses only the plan-state contribution, never a content diagnostic computed without the repository, where content completeness includes the presence of the evidence heading; the v1 completion-evidence rules SDD059, SDD070, SDD157, SDD158, SDD166 and SDD167 do not apply to a graph plan README whose directory carries the plan graph nor to a phase document that is a generated view of that graph, while a hand-authored phase document beside a graph is still held to them, and the graph-plan predicate is evaluated once per plan directory per Root and treats a graph-file stat failure that is not not-exist as an operational failure recorded on the Root, never as absence; and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run.
- Justifies: `Specs/TestSuiteReliability:FR-16`, `Specs/TestSuiteReliability:AC-08`, `Designs/TestSuiteReliability:DD-10`
- Depends on: `checked-detection`, `single-pass-evaluation`
- Gate: tests — `TestIgnoredRepoErrorAbortsEvaluation` in internal/rules/operational_test.go; `TestRetirementDetectionFailureIsOperational` in internal/rules/operational_test.go; `TestValidationOperationalExit` in cmd/sdd/operational_test.go (satisfies user-entrypoint); `TestP4IdentityQueryFailureIsOperational` in internal/rules/operational_test.go; `TestIdentityQueryFailuresAreOperational` in internal/rules/operational_test.go; `TestRetirementProbeFailureIsOperational` in internal/rules/operational_test.go; `TestRepoQueryFailuresAreOperational` in internal/rules/operational_test.go; `TestContentQueryFailuresAreOperational` in internal/rules/operational_test.go; `TestMissingEvidenceHeadingSurvivesPlanLookupFailure` in internal/rules/operational_test.go; `TestRetirementRuleFailureIsOperational` in internal/rules/operational_test.go; `TestRetirementProblemsReturnsOperationalError` in internal/rules/operational_test.go; `TestGraphPlanExemptionRequiresGeneratedView` in internal/rules/graphviews_test.go; `TestGraphPlanStatFailureIsOperational` in internal/rules/operational_test.go
- Hazards: user-entrypoint
- Artifacts: internal/rules/root.go, internal/rules/evaluate.go, internal/rules/retirement.go, internal/rules/operational_test.go, cmd/sdd/validate.go, cmd/sdd/operational_test.go, internal/rules/evidence.go, internal/rules/phasereview.go, internal/rules/graphviews.go, internal/rules/headings.go, internal/rules/plan.go, internal/rules/fixtures.go, internal/rules/graphviews_test.go
- Estimate: 3
- Observation: **pass** at seq 481 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### propagation-graph-callers

- Contract: Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check; remap-revisions treats a RevisionExists error that is not ErrNotFound as an operational failure and never reports it as a revision that is not an available commit; every rules sweep run by a cmd/sdd verb — the lifecycle transition gate's before and after sweeps and the review-resolve freeze gate — goes through the checked evaluation entry points and exits 2 with the cause when the sweep is operational, so an operational diagnostic is never deduplicated or path-filtered into an empty result and no status or frozen flag is written; graph compile and graph retire consume the retirement-problems entry point's operational error, so an unanswered retirement-source verification refuses the compile or the retire with exit 2 and writes nothing; the shared graph validation behind graph split, graph amend and graph audit returns that operational error rather than discarding it, so split and amend refuse with exit 2 before their before/after comparison and audit reports the operational failure instead of a clean result; and the graph-plan closing path — plan complete, phase complete and the view renderer they share — keeps the same discipline: a graph-store or reviews-directory read that fails for any reason other than not-exist is an operational exit 2 and never routes a graph plan down the v1 completion path or renders a missing review, the rendered identity line states only what was actually checked (a revision-exists probe run at render time, or that no recheck ran), the plan-level evidence date derives from the stable updated stamp so an unchanged closed plan re-renders byte-identically across days, the frozen-view comparison anchors the evidence section on the writer-controlled boundary rather than the first heading-shaped line and refuses when an already-rendered evidence body would change, and the README status flip is a compare-and-swap against the bytes the renderer just wrote so a concurrent writer is refused rather than overwritten.
- Justifies: `Specs/TestSuiteReliability:FR-16`, `Specs/TestSuiteReliability:AC-08`, `Designs/TestSuiteReliability:DD-10`
- Depends on: `propagation-validator`
- Gate: tests — `TestSyncCleanFailureIsOperational` in internal/graph/sync/operational_test.go; `TestProviderDetectionFailureIsOperational` in internal/graph/provider/operational_test.go; `TestLifecycleVerbsOperationalExit` in cmd/sdd/operational_test.go; `TestRemapRevisionsQueryFailureIsOperational` in internal/graph/ops/operational_test.go; `TestTransitionGateOperationalSweepExits` in cmd/sdd/operational_test.go; `TestReviewResolveOperationalSweepExits` in cmd/sdd/operational_test.go; `TestRetireAndCompileOperationalSourceExit` in cmd/sdd/operational_test.go; `TestSplitAmendAuditOperationalSourceExit` in cmd/sdd/operational_test.go; `TestGraphCompleteReadFailuresAreOperational` in cmd/sdd/graph_complete_test.go; `TestGraphPhaseCompleteRefusesAndDryRuns` in cmd/sdd/graph_complete_test.go; `TestGraphCompleteStatusFlipIsCompareAndSwap` in cmd/sdd/graph_complete_test.go; `TestFindPhaseReviewReadFailuresAreOperational` in internal/graph/compile/evidence_test.go; `TestRenderedIdentityLineReportsRealCheck` in internal/graph/compile/evidence_test.go; `TestPlanEvidenceIsDayStable` in internal/graph/compile/evidence_test.go; `TestFrozenViewComparisonIgnoresHeadingShapedContract` in internal/graph/compile/render_status_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/graph/sync/sync.go, internal/graph/sync/operational_test.go, internal/graph/provider/provider.go, internal/graph/provider/operational_test.go, internal/graph/ops/remap.go, cmd/sdd/review.go, cmd/sdd/evidence.go, cmd/sdd/operational_test.go, internal/graph/ops/ops.go, internal/graph/review/review.go, cmd/sdd/graph.go, internal/graph/compile/compile.go, internal/graph/compile/anchor.go, internal/graph/ops/retirement.go, internal/graph/compile/audit.go, internal/graph/ops/amend.go, cmd/sdd/graph_complete.go, cmd/sdd/graph_complete_test.go, cmd/sdd/transition.go, internal/graph/compile/render.go, internal/graph/compile/evidence.go, internal/graph/compile/evidence_test.go, internal/graph/compile/render_status_test.go
- Estimate: 2
- Observation: **pass** at seq 482 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

## Acceptance Criteria

- [x] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

- Verified: 2026-09-13
- Repository: /home/daniel/Development/Code/claude-sdd-planner
- VCS: git
- Revision / checkpoint: `3554448493ec74ae41630eb3f40727076c4224d9`
- Identity recheck: revision-exists probe for `3554448493ec74ae41630eb3f40727076c4224d9` at 2026-09-13T00:00:00 — matched

| Command | Working directory | Result | Observable evidence |
| --- | --- | --- | --- |
| `sdd graph status --plan TestSuiteReliability` | . | PASS (exit 0) | phase 5: 4/4 node(s) closed (GREEN, covered by a passing frozen full review gate) |

### Completed task identities
