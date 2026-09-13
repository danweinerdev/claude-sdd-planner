---
title: "Phase review: Acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..3554448493ec74ae41630eb3f40727076c4224d9"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "3554448493ec74ae41630eb3f40727076c4224d9"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..3554448493ec74ae41630eb3f40727076c4224d9"
    evidence: "drift-detector (agent, whole-plan final gate, delta ef1962e..3554448, one commit over 15 files; HEAD 3554448493ec74ae41630eb3f40727076c4224d9 unchanged; each of the seven answered findings across the three frozen ef1962e reviews traced to its code change and its named regression test, all seven passing under an explicit -run with -count=1; go build and go vet clean; go test ./... -count=1 ok for every package; a Python walk over the graph JSON finds internal/rules/reviews.go and traceability.go undeclared by any node, both one-line call-site fixups for the graphviews.go signature change, while internal/graph/compile/compile.go is declared under propagation-graph-callers; the commit message discloses two extra code-only fixes inside the rev 7 closing-path contract, the phase-docs-before-README ordering and legacyGraphViewSpan, covered by TestRenderViewsReconcilesLegacyReadmeIdempotently; grep for windows, telemetry and budget over the diff empty and no version-bump files; sdd graph status read-only reported GREEN 18 with only the three review gates stale). No missing work, one Minor scope note for the two disclosed extra fixes, no approach drift; one question on the stray go.mod working-tree edit outside the reviewed range. Verdict Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..3554448493ec74ae41630eb3f40727076c4224d9"
    evidence: "quality-scanner (agent, intent-blind, whole-tree final gate on delta ef1962e..3554448 of 826 diff lines read in full; HEAD 3554448493ec74ae41630eb3f40727076c4224d9 unchanged; gofmt -l . empty; go vet ./... clean; staticcheck ./... clean with the cache under TMPDIR; GOOS=windows and darwin arm64 builds ok; make test exit 0 for every package; go test -race -count=2 -shuffle=on over internal/rules, internal/graph and cmd/sdd ok with no race warnings; full reads of render.go at 1195 lines and compile/evidence.go plus compile.go Run at 85-166, graphviews.go 1-230, plan.go 270-325, root.go 108-123, main.go 40-60, root.go 685-701 and graph_complete.go 260-325; grep confirms no t.Parallel in the seam-mutating test files; the trimmed-halves short-circuit anchors on the last evidence heading so Contract text never enters the compared body; go.mod diff empty at the end). Findings: Major, compile.Run at compile.go:138 resolves its own memo repo while renderViews at render.go:746 resolves another, so on the compile path a closing phase is still probed twice and no test goes through compile.Run; Minor, the probes counter at render.go:66 and 80 is written but never read; Minor, render.go has crossed 1000 lines and splits naturally at the phase-doc versus README seam. Verdict Needs amendment."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..3554448493ec74ae41630eb3f40727076c4224d9"
    evidence: "spec-compliance (agent, whole-plan final gate against Specs/TestSuiteReliability and Designs/TestSuiteReliability scoped to workstreams A, B, C and POSIX E; HEAD 3554448493ec74ae41630eb3f40727076c4224d9 unchanged; the 15-file delta listed from git diff --stat; tools/parity and tools/regression/fixtures diff empty twice; go vet ./... clean; go test ./... with -count=1 ok for all packages; every changed test assertion checked and none moved toward a relaxed implementation, the operational_test.go change being a scoping fix only; the DD-10 closing audit read graph_complete.go, root.go exitCode 692-701 with refusedError usage grepped across cmd/sdd, graphviews.go 840-960, root.go parsedGraphCache, plan.go SDD059 1097-1130, render.go in full, compile/evidence.go 75-138, sync.go 285-320, provider.go 83-137 and retirement.go 30-204, with grep for os.IsNotExist across the three packages yielding 10 sites all propagating non-not-exist errors and grep for discarded error returns finding only type-assertion idioms and the pre-existing artifact-discovery walk; each round-17 resolution mapped to its covering test at a named line). No coverage gaps, no contract violations, no weakened assertions, no cross-document inconsistencies; one question on planGraph's recordFailure being a parallel mechanism to the recording Repo decorator, judged compliant. Verdict Aligned."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..3554448493ec74ae41630eb3f40727076c4224d9"
    evidence: "blind-spot-finder (agent, diff only, whole tree ef1962e..3554448 of 15 files; HEAD 3554448493ec74ae41630eb3f40727076c4224d9 unchanged; read render.go, compile/evidence.go, compile.go and rules graphviews.go in full plus graph_complete.go 280-329, root.go 690-700 and store.go 70-130 and the new tests; go build and go vet clean; go test over cmd/sdd, internal/graph and internal/rules with -race -shuffle=on -count=2 ok for all 18 packages; reproduced the main finding by calling legacyGraphViewSpan from a scratch test placed in the package and removed afterwards; git status at the end shows no tracked change). Findings: Major, legacyGraphViewHeadingRe at render.go:498-511 has no fenced-code awareness, so a README whose prose carries a fenced example containing the Graph View heading is treated as an orphaned legacy section on a first render and the example text is replaced silently; Minor, compile.Run at compile.go:138 and renderViews at render.go:746 resolve two independent memo repos so a compile probes each frozen phase twice; Minor, isGraphPlanDir's stat-only exemption and planGraph's fold of an unmarshal failure into no-graph can disagree on a stat-able but malformed graph file within one run. Verdict Needs amendment."
findings:
  - id: F-01
    severity: major
    title: "The legacy graph-view detector matches a heading inside a fenced code block"
    status: answered
  - id: F-02
    severity: minor
    title: "Stat-only exemption and parse-based graph read can disagree on a malformed graph file"
    status: answered
  - id: F-03
    severity: minor
    title: "Unused probes counter, oversized render.go, and two undeclared call-site files"
    status: answered
followups: []
---

# Phase review: Acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..3554448493ec74ae41630eb3f40727076c4224d9`.

## Findings
### F-01 — The legacy graph-view detector matches a heading inside a fenced code block

`internal/graph/compile/render.go:498-511`: a README whose prose carries a fenced example of the generated heading is treated as an orphaned legacy section on a first render and the example is replaced (reproduced on a copy).

### F-02 — Stat-only exemption and parse-based graph read can disagree on a malformed graph file

`internal/rules/graphviews.go:89-107` versus `:168-201`: a stat-able but unparseable graph file exempts the views from the v1 rules while the citation and traceability paths fall back to v1 harvesting.

### F-03 — Unused probes counter, oversized render.go, and two undeclared call-site files

`render.go:66,80` probes is never read; render.go exceeds 1100 lines; `internal/rules/reviews.go` and `traceability.go` carry one-line call-site edits and are declared by no node.

## Resolution Log
### F-01 — answered

2026-09-13: Narrow first-render data-loss path requiring prose that reproduces the generated heading inside a fence; tracked as the first follow-up outside the graph, with a fence-aware or end-marker-requiring detector.

### F-02 — answered

2026-09-13: Tracked as a follow-up: treat an unmarshal failure on a stat-confirmed graph file as operational.

### F-03 — answered

2026-09-13: Tracked as follow-ups; the two call-site files are declared on propagation-validator when the node is next claimed.
