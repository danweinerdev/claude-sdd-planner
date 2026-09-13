---
title: "Phase review: Acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "drift-detector (agent, delta a06cc46..56815db, HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; three commits db360c7, 1b0c932, 56815db over 21 files; a Python walk over the graph JSON recomputed sha256 of all 56 digest-listed artifacts for the 11 reviewed nodes via git show HEAD and found zero mismatches; membership check shows internal/rules/evidence.go and phasereview.go belong to propagation-validator at contract_rev 7 and internal/graph/compile/compile.go to propagation-graph-callers at contract_rev 6, and the other 14 changed code files belong to no node in the whole graph; full diffs of the three owned files read: the rules change is a presence-gated additive exemption keyed on the sibling Graph.json that touches no checked-detection, ErrOperational or exit-2 path, and the compile.go change only threads repoRoot through two call sites with no change to validation or write ordering; render.go's frozen-view relaxation at lines 381-429 read in full and noted as a self-documented behavioral change in an unowned file; go build clean; go test over internal/rules and internal/graph/compile ok; no prior debriefs). No missing work, no scope creep beyond accepted inline tool fixes, no approach drift. Verdict Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "quality-scanner (agent, intent-blind, delta a06cc46..56815db; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; read in full cmd/sdd/graph_complete.go, graph_complete_test.go, transition.go, internal/graph/compile/render.go, evidence.go, evidence_test.go, compile.go and the seven changed internal/rules files; gofmt -l . empty; go vet ./... clean; staticcheck over cmd/sdd, internal/graph and internal/rules with the cache under TMPDIR reports two unused types at internal/graph/compile/evidence.go:28 and 137 plus the pre-existing dead store at operational_test.go:795; GOOS=windows and GOOS=darwin arm64 builds clean; go test ./cmd/sdd/... ./internal/graph/... -race -count=2 -shuffle=on ok; refusal-before-write ordering and dry-run purity confirmed by reading the branches; retry safety of RenderViews confirmed through the byte-identical no-op check at render.go:404; grep confirms vcs RevisionExists at internal/vcs/git.go:118 is available and used by ops/remap.go:181). Findings: Major, renderPhaseEvidence at evidence.go:152 renders an Identity recheck line claiming matched without running any check, and the new isGraphPlan guard at rules/evidence.go:331 removes graph plans from the SDD072/073/075 family so nothing validates the claim; Minor, two unused types; Minor, graphPhaseComplete has no refusal, dry-run or phase-scoping test; Minor, RenderViews and writeReadmeStatusComplete at graph_complete.go:159-165 are two non-atomic writes with an error message that hides which half landed. Verdict Needs amendment."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "spec-compliance (agent, delta a06cc46..56815db against Specs/TestSuiteReliability and Designs/TestSuiteReliability scoped to workstream E POSIX; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; git diff --stat on internal/procexec and internal/vcs is empty so the dispatch premise that this delta touched them was wrong and the DD-10 audit was applied to the new renderer and lifecycle code instead; read in full internal/rules/{evidence,headings,phasereview,plan,graphviews}.go, cmd/sdd/{graph_complete,graph_complete_test,transition}.go, internal/graph/compile/{evidence,render}.go, plus main.go 1-80 and root.go 692-707; graph closure stays mechanically enforced by greview.Closed in graph_complete.go:106-165 and 171-240 before any write; refusals return refusedError for exit 1 and derive or render failures return plain errors for exit 2; tools/parity diff empty; go build, go vet and go test -count=1 over internal/rules, internal/graph and cmd/sdd all ok). One Major finding: findPhaseReview at internal/graph/compile/evidence.go:84-90 returns not-found on any os.ReadDir error, so a permission or I/O failure on the reviews directory renders a pending evidence section and plan complete still reports success; one linked Minor: applyPlanEvidence at render.go:626-630 turns that not-ok into silence. No test covers the directory read failure path. Verdict Needs amendment."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "blind-spot-finder (agent, diff only a06cc46..56815db on cmd/sdd and internal/graph/compile, 8 files; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; read in full graph_complete.go, compile.go, render.go and evidence.go plus model/decode.go for the Contract constraints; reproduced with three temporary tests under the compile package that were run and deleted leaving the tree clean; go build, go vet over cmd/sdd and internal/graph, and go test ./cmd/sdd/... ./internal/graph/compile/... -race -shuffle=on -count=2 all pass; verified dry-run returns before any write, review filenames come from os.ReadDir under the plan's reviews directory and are sorted so no traversal or ordering issue, and repoRoot flows through the single resolveRoots path). One Major finding: the frozen-view relaxation at render.go:407-428 strips the evidence section at the first regex match of a Phase Completion Evidence heading, but node Contract text is rendered verbatim ahead of that heading at render.go:227 with no content restriction, so a contract containing such a line truncates the comparison early and planWrite returned write true with no error while closure content past the injected heading differed; no existing test covers heading-shaped contract text. Verdict Needs amendment."
findings:
  - id: F-01
    severity: critical
    title: "The graph-plan closing path folds read failures into absence and asserts checks it never ran"
    status: open
    action: revise
    nodes: [propagation-graph-callers]
    revise:
      contract: "Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check; remap-revisions treats a RevisionExists error that is not ErrNotFound as an operational failure and never reports it as a revision that is not an available commit; every rules sweep run by a cmd/sdd verb — the lifecycle transition gate's before and after sweeps and the review-resolve freeze gate — goes through the checked evaluation entry points and exits 2 with the cause when the sweep is operational, so an operational diagnostic is never deduplicated or path-filtered into an empty result and no status or frozen flag is written; graph compile and graph retire consume the retirement-problems entry point's operational error, so an unanswered retirement-source verification refuses the compile or the retire with exit 2 and writes nothing; the shared graph validation behind graph split, graph amend and graph audit returns that operational error rather than discarding it, so split and amend refuse with exit 2 before their before/after comparison and audit reports the operational failure instead of a clean result; and the graph-plan closing path — plan complete, phase complete and the view renderer they share — keeps the same discipline: a graph-store or reviews-directory read that fails for any reason other than not-exist is an operational exit 2 and never routes a graph plan down the v1 completion path or renders a missing review, the rendered identity line states only what was actually checked (a revision-exists probe run at render time, or that no recheck ran), the plan-level evidence date derives from the stable updated stamp so an unchanged closed plan re-renders byte-identically across days, the frozen-view comparison anchors the evidence section on the writer-controlled boundary rather than the first heading-shaped line and refuses when an already-rendered evidence body would change, and the README status flip is a compare-and-swap against the bytes the renderer just wrote so a concurrent writer is refused rather than overwritten."
      gate:
        type: tests
        tests:
          - {id: TestSyncCleanFailureIsOperational, file: internal/graph/sync/operational_test.go}
          - {id: TestProviderDetectionFailureIsOperational, file: internal/graph/provider/operational_test.go}
          - {id: TestLifecycleVerbsOperationalExit, file: cmd/sdd/operational_test.go}
          - {id: TestRemapRevisionsQueryFailureIsOperational, file: internal/graph/ops/operational_test.go}
          - {id: TestTransitionGateOperationalSweepExits, file: cmd/sdd/operational_test.go}
          - {id: TestReviewResolveOperationalSweepExits, file: cmd/sdd/operational_test.go}
          - {id: TestRetireAndCompileOperationalSourceExit, file: cmd/sdd/operational_test.go}
          - {id: TestSplitAmendAuditOperationalSourceExit, file: cmd/sdd/operational_test.go}
          - {id: TestGraphCompleteReadFailuresAreOperational, file: cmd/sdd/graph_complete_test.go}
          - {id: TestGraphPhaseCompleteRefusesAndDryRuns, file: cmd/sdd/graph_complete_test.go}
          - {id: TestGraphCompleteStatusFlipIsCompareAndSwap, file: cmd/sdd/graph_complete_test.go}
          - {id: TestFindPhaseReviewReadFailuresAreOperational, file: internal/graph/compile/evidence_test.go}
          - {id: TestRenderedIdentityLineReportsRealCheck, file: internal/graph/compile/evidence_test.go}
          - {id: TestPlanEvidenceIsDayStable, file: internal/graph/compile/evidence_test.go}
          - {id: TestFrozenViewComparisonIgnoresHeadingShapedContract, file: internal/graph/compile/render_status_test.go}
  - id: F-02
    severity: minor
    title: "Unused types in the evidence renderer"
    status: answered
followups: []
---

# Phase review: Acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d`.

## Findings
### F-01 — The graph-plan closing path folds read failures into absence and asserts checks it never ran

Six sites in the new closing path break the operational-versus-absence discipline this node exists to enforce: `internal/graph/compile/evidence.go:86-89` and `99-102` fold a reviews-directory or review-file read failure into no covering review; `cmd/sdd/graph_complete.go:42-44` and `178-180` fold a graph-store stat failure into not-a-graph-plan and fall through to the v1 completion path; `evidence.go:152` renders an identity recheck as matched without running one; `render.go:407-428` anchors the frozen-view comparison on the first heading-shaped line, which a node contract can contain, and never compares an already-rendered evidence body; `render.go:626-644` bakes the wall clock into the plan evidence so an unchanged closed plan rewrites daily; `graph_complete.go:184-186,229-231` flip the README status with an unconditional write after the renderer's compare-and-swap. `graphPhaseComplete`'s refusal and dry-run branches have no test.

### F-02 — Unused types in the evidence renderer

`internal/graph/compile/evidence.go:28,137` define phaseIdentity and phaseEvidence, referenced nowhere.

## Resolution Log
### F-02 — answered

2026-09-13: Removed in the propagation-graph-callers rev 7 commit alongside F-01.
