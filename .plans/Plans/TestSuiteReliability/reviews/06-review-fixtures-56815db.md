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
    evidence: "drift-detector (agent, delta a06cc46..56815db, HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d confirmed before and after; the three commits db360c7, 1b0c932 and 56815db change 21 files, all graph-completion machinery in cmd/sdd/graph_complete.go, transition.go, internal/graph/compile/{compile,render,evidence}.go and internal/rules/{evidence,fixtures,graphviews,headings,phasereview,plan}.go plus the implement skill and its generated portable copies; parsed the graph JSON for the 10 reviewed nodes, hashed all 52 digest-listed files at HEAD with sha256sum and found zero mismatches and zero missing files; comm -12 between the delta file list and the gate file list is empty, so none of the reviewed nodes' verified artifacts or dependency files moved, including the rules files owned by single-pass-evaluation and append-only-scan-once which are rules.go, evaluate.go, waivers.go, root.go and appendonly.go; git diff on plugin.json and version.go is empty so no version bump). No missing work, no approach drift; the delta is inline tool work outside this gate's nodes, routed to the execution and final gates. Verdict Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "quality-scanner (agent, intent-blind, delta a06cc46..56815db restricted to internal/rules; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; read graphviews.go in full, evidence.go lines 1-1160, headings.go 1-120, phasereview.go 1-60 and 223-1130, plan.go 280-320 and fixtures.go 730-800; traced every caller of completePhasesWithEvidence, which is shared by SDD166, SDD167, SDD168, SDD170, SDD173, SDD174 and by SDD172 through phaseTaskGitIdentities, and confirmed the new isGraphPlan guard is behaviorally inert for the four undocumented codes because finalAlignedReviewOf returns not-ok on rendered views and rendered views carry an empty tasks list; gofmt -l . empty; go vet and staticcheck on internal/rules clean, staticcheck cache redirected under TMPDIR because the default cache dir is read-only in the sandbox; go test ./internal/rules/... -race -count=2 -shuffle=on ok twice; go test ./tools/... ok; targeted runs of the registry examples meta-test, TestPure selection, TestSCMBoundaryInventory and TestPureSelectionInventory all pass; git diff --stat on tools/parity empty). One Minor finding: the doc comment on completePhasesWithEvidence at phasereview.go:223-230 names only SDD166/167/168 while the guard also short-circuits SDD170, SDD172, SDD173 and SDD174, none of which are in the new test's exempted map. Verdict Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "spec-compliance (agent, delta a06cc46..56815db against Specs/TestSuiteReliability and Designs/TestSuiteReliability scoped to workstreams A, B and C; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; the delta is graph-completion infrastructure and a rules applicability predicate, not workstream work; git diff on rules_test.go, harness_test.go, inventory_test.go, evaluate_test.go and appendonly_scan_test.go is empty so the determinism, pure-selection, single-pass and scan-once contract tests are untouched; isGraphPlan at graphviews.go:77-79 is an os.Stat on the sibling Graph.json called inside five existing per-artifact rule callbacks at headings.go:245 and 336, phasereview.go:234, plan.go:294 and evidence.go:331, so it adds no root-level or family-wide scan and does not violate the once-per-artifact and scan-once contracts; git diff --stat on tools/parity and tools/regression/fixtures is empty; go build and go vet clean; go test -count=1 over internal/rules and tools reports ok including the registry examples meta-test and the new TestGraphPlanExemptsV1CompletionEvidenceRules at graphviews_test.go:288). No coverage gaps, no contract violations, no cross-document inconsistencies. Verdict Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "blind-spot-finder (agent, diff only a06cc46..56815db on internal/rules, internal/graph/algorithms and cmd/sdd/graph_complete_test.go; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; read graphviews.go, headings.go, phasereview.go 1-320, plan.go 280-325, root.go 240-420, internal/graph/compile/evidence.go and store.go 42-58; go build clean, go vet ./internal/rules/... clean, go test ./internal/rules/... -race -shuffle=on -count=2 ok; wrote two scratch repro tests in the rules package and ran them, then deleted them and confirmed git status clean for internal/rules). Two findings: Major, isGraphPlan at graphviews.go:77-81 is a directory-level fact used to exempt per-document gates SDD157 and SDD166/167/168 at headings.go:245 and 336 and phasereview.go:234, so a hand-authored phase doc registered in a graph plan's README with a bogus final-review citation passes with zero diagnostics, demonstrated by the second repro; the codebase already has the document-scoped IsGeneratedView test used at phaseownership.go:60. Minor, isGraphPlan folds any os.Stat error into false, so a transient non-ENOENT failure silently falls back to the stricter v1 rules, consistent with the pre-existing planGraphIDs convention in the same file. Verdict Needs amendment."
findings:
  - id: F-01
    severity: major
    title: "The graph-plan exemption is directory-scoped and folds a stat failure into absence"
    status: open
    action: revise
    nodes: [propagation-validator]
    revise:
      contract: "Validation owns an operational-failure collector: Root.Repo records a failed checked detection before returning an unavailable adapter, the evaluator aborts after the first callback that recorded a failure and discards partial findings, retirement uses checked detection, VerifyRetirementSource wraps a failed post-detection RevisionExists so ErrOperational survives errors.Is, the retirement rule records an operational VerifyRetirementSource failure on the Root and stays silent instead of emitting it as a retirement problem, and the exported retirement-problems entry point never discards an operational failure — it returns it as an error alongside the substantive problems so no caller can read an unanswered verification as a clean result; every repository query made from a rule callback in internal/rules that can emit or suppress a diagnostic — RevisionExists, IsAncestor, Clean, Head, RevisionsAfter, ChangedPaths and FileAt, including the review-committed check and the lifecycle-normalized content loads behind the intent comparison and the planning-revision load — emits an absence or state diagnostic only when the query succeeded and answered negatively (or the revision syntax is unsupported) and stays silent on ErrOperational, which the collector already holds; an operational plan lookup inside the evidence-committed checks suppresses only the plan-state contribution, never a content diagnostic computed without the repository, where content completeness includes the presence of the evidence heading; the v1 completion-evidence rules SDD059, SDD070, SDD157, SDD158, SDD166 and SDD167 do not apply to a graph plan README whose directory carries the plan graph nor to a phase document that is a generated view of that graph, while a hand-authored phase document beside a graph is still held to them, and the graph-plan predicate is evaluated once per plan directory per Root and treats a graph-file stat failure that is not not-exist as an operational failure recorded on the Root, never as absence; and `sdd validate` exits 2 with the cause instead of 0 or 1 when git or p4 cannot run."
      gate:
        type: tests
        tests:
          - {id: TestIgnoredRepoErrorAbortsEvaluation, file: internal/rules/operational_test.go}
          - {id: TestRetirementDetectionFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestValidationOperationalExit, file: cmd/sdd/operational_test.go, satisfies: [user-entrypoint]}
          - {id: TestP4IdentityQueryFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestIdentityQueryFailuresAreOperational, file: internal/rules/operational_test.go}
          - {id: TestRetirementProbeFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestRepoQueryFailuresAreOperational, file: internal/rules/operational_test.go}
          - {id: TestContentQueryFailuresAreOperational, file: internal/rules/operational_test.go}
          - {id: TestMissingEvidenceHeadingSurvivesPlanLookupFailure, file: internal/rules/operational_test.go}
          - {id: TestRetirementRuleFailureIsOperational, file: internal/rules/operational_test.go}
          - {id: TestRetirementProblemsReturnsOperationalError, file: internal/rules/operational_test.go}
          - {id: TestGraphPlanExemptionRequiresGeneratedView, file: internal/rules/graphviews_test.go}
          - {id: TestGraphPlanStatFailureIsOperational, file: internal/rules/operational_test.go}
  - id: F-02
    severity: minor
    title: "The shared completion-evidence helper's guard affects more rule codes than its comment and test name"
    status: answered
followups: []
---

# Phase review: Acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d`.

## Findings
### F-01 — The graph-plan exemption is directory-scoped and folds a stat failure into absence

`internal/rules/graphviews.go:77-81` answers a directory-level question but gates per-document exemptions at `headings.go:245,336` and `phasereview.go:234`; a hand-authored phase doc registered beside a graph passes with zero diagnostics (reproduced). The same predicate returns false on any stat error, and it is an unmemoized stat from five rule sites over every artifact in the root.

### F-02 — The shared completion-evidence helper's guard affects more rule codes than its comment and test name

`phasereview.go:223-230` names SDD166/167/168 while the guard also short-circuits SDD170, SDD172, SDD173 and SDD174, none in the exemption test's map.

## Resolution Log
### F-02 — answered

2026-09-13: Corrected in the propagation-validator rev 8 commit alongside F-01: the comment names every affected code and the exemption test's map lists them.
