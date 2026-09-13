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
verdict: Aligned
reviewed_planning_revision: "56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "drift-detector (agent, whole-plan final gate, delta a06cc46..56815db of 20 files; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged throughout; built the node-to-artifact map from the graph JSON: only internal/graph/compile/compile.go (propagation-graph-callers) and internal/rules/evidence.go and phasereview.go (propagation-validator) among the changed files are node-owned, the other 17 are unowned; a Python sha256 sweep of all 223 recorded artifact digests across the 21 nodes found exactly three mismatches, all in review-execution's stale frozen snapshot of those three files, while both owning nodes' 23 artifacts match exactly; sdd graph status read-only reported GREEN 18, STALE 3, closed 0 of 21 with the three review gates stale at seq 431 provenance a06cc46, which is the expected mid-round state this review exists to close; read both owning nodes' full contracts; no version-bump files and no Windows, budget or telemetry files in the diff so Non-Goals hold; no prior debriefs). Findings: Major approach drift, the v1 rule exemptions in evidence.go and phasereview.go are covered by propagation-validator's digest but not named by its contract, and the compile.go repoRoot plumbing is likewise outside propagation-graph-callers' contract text; Question, the README phases list and the generated graph view disagree on phase count because 07-Ungrouped.md was rendered at completion; observation, the README says status complete while the graph is not closed. Verdict Needs amendment."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "quality-scanner (agent, intent-blind, whole-tree final gate on delta a06cc46..56815db; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; read the full 1873-line diff plus graph_complete.go, graph_complete_test.go, transition.go 1-100, render.go 375-460, evidence.go in full, compile_test.go 740-820, rules graphviews.go 1-90, root.go 1-190, headings.go 220-400, evidence.go 300-345 and graph.go 1100-1150 to compare graphDerive with graphNext; gofmt -l . empty; go vet ./... clean; staticcheck ./... with caches under TMPDIR reports only the pre-existing operational_test.go:795 dead store and two unused types at compile/evidence.go:28 and 137; GOOS=windows and darwin arm64 builds clean; make test green after a spurious stale entry in the shared Go build cache referencing a deleted scratch test was bypassed with a fresh GOCACHE; go test -race -count=2 -shuffle=on over internal/rules, internal/graph and cmd/sdd ok; new code reuses resolvePlanReadme, resolveRoots, RenderViews, refusedError and transitionResult rather than duplicating them). Findings: Major, isGraphPlan at graphviews.go:77-81 is an unmemoized os.Stat called from five rule sites over every artifact in the root against the package's documented repoCache and sectionCache convention; Minor, planWrite at render.go:407-427 checks evidence-section presence but never compares evidence bodies, so a changed or added review artifact rewrites a frozen view's cited review without refusal; Minor, two unused types; Minor, graphPhaseComplete refusal and dry-run branches untested. Verdict Needs amendment."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "spec-compliance (agent, whole-plan final gate against Specs/TestSuiteReliability and Designs/TestSuiteReliability; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged; git diff --stat confirms the delta touches no internal/procexec or internal/vcs file and tools/parity is unchanged; go vet ./... clean; go test ./... fully green; every touched test file's diff is a mechanical repoRoot argument or gofmt realignment with no assertion value changed; inventoried every os.Stat, os.ReadDir and os.ReadFile call in graphviews.go, compile/evidence.go, render.go and graph_complete.go with file, line and disposition: render.go planWrite 383-388 and refreshReadmePhaseStatuses 760-766 correctly propagate non-not-exist errors, while five new sites fold every error class into a negative result: isGraphPlan at graphviews.go:79-80, findPhaseReview ReadDir at evidence.go:86-89 and ReadFile at 99-102, graphPlanDir at graph_complete.go:42-44 and graphPhaseComplete at 178-180, the last two routing a graph plan down the v1 completion path on a permission error; grep for Permission, Chmod, EACCES and FileMode in the three new test files finds no fault-injection test). Findings: Critical at isGraphPlan, findPhaseReview ReadDir, graphPlanDir and graphPhaseComplete; Major at findPhaseReview ReadFile; AC-08 fault injection uncovered for all five. Verdict Needs amendment."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d"
    evidence: "blind-spot-finder (agent, diff only, whole tree a06cc46..56815db of 21 files; HEAD 56815db301e9c63ccbc5ae1dd314cd32384e9b9d unchanged before and after; read in full the seven changed rules files, compile/evidence.go and its test, render.go including renderPhaseDoc, planWrite, fillDates, renderViews, planReadmeUpdate, applyPlanEvidence and stripPhaseEvidenceSection, render_status_test.go, graph_complete.go and its test, transition.go and internal/store/store.go 1-140; go build and go vet clean; go test over cmd/sdd, internal/graph and internal/rules with -race -shuffle=on -count=2 ok; grep for acquireExclusive, flock, LockFile and processLock in store and cmd/sdd confirms locks are per call; two scratch probe tests were created, run and removed with the tree confirmed clean; the frozen-view reopen scenario was constructed and refused correctly because status, the frozen marker and the acceptance checkbox all sit outside the stripped section). Findings: Major, writeReadmeStatusComplete at graph_complete.go:184-186 and 229-231 performs an unconditional WriteAtomic on the README after RenderViews' CAS-protected write with no lock spanning the two, so a concurrent writer's change is silently discarded; Major, applyPlanEvidence at render.go:626-644 bakes time.Now into the plan evidence body while the phase renderer uses the stable updated stamp through fillDates, reproduced as a README rewrite on a day change with nothing else changed; Minor, isGraphPlan folds every os.Stat error into not-a-graph-plan. Questions on findPhaseReview error folding and phaseCheckpoint node choice. Verdict Needs amendment."
findings:
  - id: F-01
    severity: major
    title: "Owned artifacts now carry work their contracts do not name"
    status: answered
  - id: F-02
    severity: critical
    title: "Five new filesystem reads in the closing path fold failure into absence"
    status: answered
  - id: F-03
    severity: major
    title: "Unguarded README status write and wall-clock date in the plan evidence"
    status: answered
  - id: F-04
    severity: minor
    title: "Rendered views disagree with the graph mid-round"
    status: answered
followups: []
---

# Phase review: Acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..56815db301e9c63ccbc5ae1dd314cd32384e9b9d`.

## Findings
### F-01 — Owned artifacts now carry work their contracts do not name

`internal/rules/evidence.go` and `phasereview.go` (propagation-validator) and `internal/graph/compile/compile.go` (propagation-graph-callers) changed for graph-plan completion work that neither contract describes.

### F-02 — Five new filesystem reads in the closing path fold failure into absence

`internal/rules/graphviews.go:79-80`, `internal/graph/compile/evidence.go:86-89` and `99-102`, `cmd/sdd/graph_complete.go:42-44` and `178-180`; no fault-injection test covers any of them.

### F-03 — Unguarded README status write and wall-clock date in the plan evidence

`cmd/sdd/graph_complete.go:184-186,229-231` and `internal/graph/compile/render.go:626-644`; the frozen-view comparison at `render.go:407-427` also never compares evidence bodies.

### F-04 — Rendered views disagree with the graph mid-round

The README says complete and the frozen acceptance view says closed while the graph reports the three review gates stale at this identity.

## Resolution Log
### F-01 — answered

2026-09-13: Both contracts are revised through the sibling gate reviews at this identity (`Plans/TestSuiteReliability/reviews/06-review-fixtures-56815db.md` F-01 and `Plans/TestSuiteReliability/reviews/06-review-execution-56815db.md` F-01) so each names the closing-path work its artifacts carry.

### F-02 — answered

2026-09-13: The rules-side site is revised through the fixtures-gate F-01 amendment and the four renderer and cmd sites through the execution-gate F-01 amendment, each with fault-injection gate tests.

### F-03 — answered

2026-09-13: Revised through the execution-gate F-01 amendment at this identity.

### F-04 — answered

2026-09-13: Expected mid-round state: the gates are stale because the nodes were re-verified at this identity and this review is what closes them; the README and views are re-rendered by plan complete once the graph closes again. Logged as a tool observation (a completed README should not be allowed to disagree silently with an unclosed graph).
