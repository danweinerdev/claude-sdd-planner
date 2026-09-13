package rules

// v1 validation coexisting with graph projections (Plans/SddGraph 5.6, filed
// from the self-hosting pilot's findings): compile renders phase-doc views
// and upserts a marker-delimited Graph View section into the plan README.
// Both are PROJECTIONS of the committed graph — not v1 lifecycle intent —
// so SDD163's phases[]-listing demand skips marker-carrying views, and
// SDD174's lifecycle normalization strips the README's generated section.

import (
	"os"
	"strings"
	"testing"
)

func planReadmeWithGraphView(withSection bool) string {
	src := `---
title: "Sample Plan"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
phases:
  - id: 1
    title: "One"
    status: complete
    doc: "01-One.md"
---

# Sample Plan

## Overview

The plan's own prose.
`
	if withSection {
		src += "\n" + GraphViewBegin + "\n\n## Graph View\n\n" +
			"<!-- GENERATED VIEW — source of truth: Sample-Graph.json. Regenerate with `sdd compile --plan Sample`. Edits here are overwritten. -->\n\n" +
			"| Phase | Nodes | Doc |\n|---|---|---|\n| 1: One | 3 | `01-x.md` |\n\n" +
			"3 node(s) total.\n\n" + GraphViewEnd + "\n"
	}
	return src
}

// TestLifecycleNormalizationStripsGraphViewSection: the README a frozen
// phase review pinned predates the graph-view upsert; the projection must
// not read as changed plan intent (the pilot's SDD174 x4).
func TestLifecycleNormalizationStripsGraphViewSection(t *testing.T) {
	before, err := lifecycleNormalizedArtifact(planReadmeWithGraphView(false), "plan")
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	after, err := lifecycleNormalizedArtifact(planReadmeWithGraphView(true), "plan")
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	if before != after {
		t.Errorf("the generated Graph View section changed canonical intent:\n--- without ---\n%s\n--- with ---\n%s", before, after)
	}

	// The counterpart: a real prose change still survives normalization.
	changed, err := lifecycleNormalizedArtifact(
		strings.Replace(planReadmeWithGraphView(true), "The plan's own prose.", "DIFFERENT prose.", 1), "plan")
	if err != nil {
		t.Fatal(err)
	}
	if changed == after {
		t.Error("a real prose change normalized away; SDD174 would miss changed intent")
	}
}

func viewPhaseDoc(num, title string, generated bool) string {
	marker := ""
	if generated {
		marker = "\n<!-- GENERATED VIEW — source of truth: Sample-Graph.json. Regenerate with `sdd compile --plan Sample`. Edits here are overwritten. -->\n"
	}
	return `---
title: "` + title + `"
type: phase
plan: "Sample"
phase: ` + num + `
status: planned
created: 2026-08-01
updated: 2026-08-01
deliverable: "Graph view"
tasks: []
---

# Phase ` + num + `: ` + title + `
` + marker + `
## Overview

Rendered.
`
}

// TestPhaseOwnershipExemptsGeneratedViews: rendered views are owned by the
// committed graph, not the README phases[] array v1 owns (the pilot's
// SDD163 x5) — while a rogue non-generated unlisted doc still fires.
func TestPhaseOwnershipExemptsGeneratedViews(t *testing.T) {
	r := rootFrom(t, map[string]string{
		"Plans/Sample/README.md":   planReadmeWithGraphView(true),
		"Plans/Sample/01-One.md":   viewPhaseDoc("1", "One", false),
		"Plans/Sample/02-view.md":  viewPhaseDoc("2", "view", true),
		"Plans/Sample/03-rogue.md": viewPhaseDoc("3", "rogue", false),
	})
	diags := Run(r)
	for _, d := range diags {
		if d.Code == "SDD163" && d.Path == "Plans/Sample/02-view.md" {
			t.Errorf("a GENERATED VIEW must be exempt from the phases[] listing demand: %s", d.Message)
		}
	}
	rogue := false
	for _, d := range diags {
		if d.Code == "SDD163" && d.Path == "Plans/Sample/03-rogue.md" {
			rogue = true
		}
	}
	if !rogue {
		t.Error("a non-generated unlisted phase doc must still fire SDD163")
	}
}

// Graph-plan traceability (verified defect, sdd <= 2.8.3): the SDD160/162
// harvest read only phase-doc tasks[] text — empty by design for graph
// plans — so an approved graph plan could NEVER pass validate even though
// compile enforced coverage on the same tree. The citations live in node
// justifies inside <Plan>-Graph.json; the harvest resolves them with the
// same CitationIndex opinion the compiler uses, per spec (a citation
// resolving to one spec never covers another spec's same-numbered id).

func traceSpec(name string) string {
	return `---
title: "` + name + `"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# ` + name + `

## Functional Requirements

- **FR-01**: ` + name + ` requirement one.

## Acceptance Criteria

- [ ] **AC-01**: ` + name + ` criterion one.
`
}

func traceGraph(justifies ...string) string {
	nodes := ""
	for i, j := range justifies {
		if i > 0 {
			nodes += ","
		}
		nodes += `{"id":"n` + string(rune('a'+i)) + `","contract":"c","justifies":["` + j + `"],"gate":{"type":"tests","tests":[{"id":"t","file":"f.ext"}]},"hazards":[],"estimate":1}`
	}
	return `{"version":1,"seq_counter":0,"nodes":[` + nodes + `]}`
}

const traceGraphPlan = `---
title: "P"
type: plan
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/A, Specs/B]
phases: []
---

# P

## Overview

x.
`

func TestGraphPlanTraceabilityResolvesJustifies(t *testing.T) {
	r := rootFrom(t, map[string]string{
		"Specs/A/README.md":    traceSpec("A"),
		"Specs/B/README.md":    traceSpec("B"),
		"Plans/P/README.md":    traceGraphPlan,
		"Plans/P/P-Graph.json": traceGraph("Specs/A:FR-01", "A:AC-01", "Specs/B:FR-01", "B:AC-01"),
	})
	for _, d := range Run(r) {
		if d.Code == "SDD160" || d.Code == "SDD162" {
			t.Errorf("full per-spec coverage via graph justifies must satisfy traceability: %s %s", d.Code, d.Message)
		}
	}
}

func TestGraphCoverageDoesNotDemandTransitiveSpecs(t *testing.T) {
	r := rootFrom(t, map[string]string{
		"Specs/A/README.md":        traceSpec("A"),
		"Specs/B/README.md":        traceSpec("B"),
		"Designs/Bridge/README.md": strings.Replace(strings.Replace(traceSpec("Bridge"), "type: spec", "type: design", 1), "related: []", "related: [Specs/B]", 1),
		"Plans/P/README.md":        strings.Replace(traceGraphPlan, "related: [Specs/A, Specs/B]", "related: [Specs/A, Designs/Bridge]", 1),
		"Plans/P/P-Graph.json":     traceGraph("Specs/A:FR-01", "Specs/A:AC-01"),
	})
	for _, d := range Run(r) {
		if d.Code == "SDD160" || d.Code == "SDD161" || d.Code == "SDD162" {
			t.Errorf("transitive sources are citable, not additional graph coverage obligations: %s %s", d.Code, d.Message)
		}
	}
}

func TestGraphPlanTraceabilityIsPerSpec(t *testing.T) {
	// Specs/B's criterion is uncovered; a BARE ambiguous justification of
	// AC-01 covers neither spec (never first-wins).
	r := rootFrom(t, map[string]string{
		"Specs/A/README.md":    traceSpec("A"),
		"Specs/B/README.md":    traceSpec("B"),
		"Plans/P/README.md":    traceGraphPlan,
		"Plans/P/P-Graph.json": traceGraph("Specs/A:FR-01", "Specs/A:AC-01", "Specs/B:FR-01", "AC-01"),
	})
	var hits []string
	for _, d := range Run(r) {
		if d.Code == "SDD162" {
			hits = append(hits, d.Message)
		}
	}
	joined := strings.Join(hits, "\n")
	if !strings.Contains(joined, "`AC-01` from `Specs/B/README.md`") {
		t.Errorf("the uncovered spec's criterion must still be reported:\n%s", joined)
	}
	if strings.Contains(joined, "Specs/A/README.md") {
		t.Errorf("the qualified-covered spec must be satisfied:\n%s", joined)
	}
}

// SDD096 graph fallback (verified defect, sdd <= 2.8.3): a frozen —
// immutable — review whose follow-up tracked a v1 task that an in-place
// graph rebuild retired left the root PERMANENTLY invalid: tracked_in
// resolved only against v1 phase-doc tasks, reviews declare no waivers, and
// the artifact cannot be edited. The sanctioned exit: tracked_in also
// resolves against the plan's committed graph — live node ids and the
// append-only retired register (the tool's own tombstone place), with the
// convert spelling (3.3 <-> task-3-3) accepted.
func TestFollowupTracksGraphNodesAndRetiredIds(t *testing.T) {
	reviewFor := func(tracked string) string {
		return replaceFirst(
			reviewWithBlocks(
				"\n  - id: F-01\n    severity: major\n    title: One\n    status: open\n",
				"\n  - id: FU-01\n    finding: F-01\n    summary: S.\n    tracked_in: \""+tracked+"\"\n",
				"### F-01 — one\n\nText.\n", ""),
			`review_of: "Specs/Sample/README.md"`, `review_of: "Plans/Sample/README.md"`)
	}
	graph := `{"version":1,"seq_counter":0,"nodes":[{"id":"node-x","contract":"c","justifies":["FR-01"],"gate":{"type":"tests","tests":[{"id":"t","file":"f.ext"}]},"hazards":[],"estimate":1}],"retired":["task-3-3"]}`

	for _, tc := range []struct {
		name, tracked string
		fires         bool
	}{
		{"retired-tombstone-convert-spelling", "3.3", false},
		{"retired-tombstone-exact", "task-3-3", false},
		{"live-graph-node", "node-x", false},
		{"nowhere", "9.9", true},
	} {
		r := rootFrom(t, map[string]string{
			"Retro/sample-review.md":         reviewFor(tc.tracked),
			"Plans/Sample/README.md":         validPlan(false),
			"Plans/Sample/Sample-Graph.json": graph,
		})
		fired := false
		for _, d := range Run(r) {
			if d.Code == "SDD096" {
				fired = true
			}
		}
		if fired != tc.fires {
			t.Errorf("%s: SDD096 fired=%v, want %v", tc.name, fired, tc.fires)
		}
	}
}

// TestGraphPlanExemptsV1CompletionEvidenceRules runs the full rule set over
// a minimal closed graph plan (a `status: complete` README and phase view
// beside a committed Graph.json, carrying none of the v1 completion-evidence
// apparatus those rules demand) and asserts none of the v1 rules exempted
// for graph plans fire — their completion record is the graph, not this
// markdown (CLAUDE.md "Completion Evidence").
func TestGraphPlanExemptsV1CompletionEvidenceRules(t *testing.T) {
	r := rootFrom(t, map[string]string{
		"Plans/Sample/README.md":         completeGraphPlanReadme("Sample", "01-core.md", "One"),
		"Plans/Sample/01-core.md":        completeGeneratedPhaseView("Sample", "1", "One"),
		"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1"}, nil),
	})
	exempted := map[string]bool{
		"SDD059": true, "SDD070": true, "SDD157": true, "SDD158": true,
		"SDD166": true, "SDD167": true, "SDD168": true, "SDD170": true,
		"SDD172": true, "SDD173": true, "SDD174": true,
	}
	for _, d := range Run(r) {
		if d.Severity != Error {
			continue
		}
		if exempted[d.Code] {
			t.Errorf("graph plan: %s fired and should not have: %s", d.Code, d.Message)
		}
	}
}

// handAuthoredCompletePhase renders a `status: complete` phase document that
// carries NONE of the rendered-view marker (IsGeneratedView) and none of the
// v1 completion-evidence apparatus (Completed task identities, a filled
// Phase Completion Evidence section) — a plan author's own markdown sitting
// beside a graph, not a projection of it. The graph-plan exemption keys on
// the generated-view marker per document, not on plan directory alone, so
// this document must still be held to the v1 rules.
func handAuthoredCompletePhase(planName, phaseOrdinal, title, doc string) string {
	return `---
title: "` + title + `"
type: phase
plan: "` + planName + `"
phase: ` + phaseOrdinal + `
status: complete
created: 2024-01-01
updated: 2024-01-01
deliverable: "Hand-authored."
tasks: []
---

# Phase ` + phaseOrdinal + `: ` + title + `

## Overview

Hand-written, not rendered.

## Acceptance Criteria

- [ ] Works.

## Phase Completion Evidence

Pending — not complete.
`
}

// handAuthoredPlannedPhase renders a `status: planned` phase document —
// genuinely incomplete, and not a rendered projection (no IsGeneratedView
// marker) — so SDD059 (a complete plan containing an incomplete phase) must
// still catch it beside a graph plan (review-execution ef1962e F-01).
func handAuthoredPlannedPhase(planName, phaseOrdinal, title string) string {
	return `---
title: "` + title + `"
type: phase
plan: "` + planName + `"
phase: ` + phaseOrdinal + `
status: planned
created: 2024-01-01
updated: 2024-01-01
deliverable: "Hand-authored."
tasks: []
---

# Phase ` + phaseOrdinal + `: ` + title + `

## Overview

Hand-written, not rendered.

## Acceptance Criteria

- [ ] Works.
`
}

// TestGraphPlanExemptionRequiresGeneratedView is the review-execution F-02
// regression: the v1 completion-evidence exemption must key on the PER
// DOCUMENT generated-view marker (IsGeneratedView), not on "this plan
// directory carries a Graph.json" alone. A hand-authored phase doc sitting
// beside a graph plan's Graph.json, registered in the README's `phases:`
// list, is still plan-author markdown — not a rendered projection — and
// must still be held to the v1 rules the generated view is exempt from.
func TestGraphPlanExemptionRequiresGeneratedView(t *testing.T) {
	// The README registers all three phases, so SDD059/SDD158's plan-level
	// phase-entry checks see phases 2 and 3 (the hand-authored docs) too.
	// Phase 3's doc is genuinely incomplete (`status: planned`) and is not a
	// generated view, so SDD059 must still catch it beside a graph plan.
	threePhaseReadme := strings.Replace(completeGraphPlanReadme("Sample", "01-core.md", "One"),
		`phases:
  - id: 1
    title: "One"
    status: complete
    doc: "01-core.md"`,
		`phases:
  - id: 1
    title: "One"
    status: complete
    doc: "01-core.md"
  - id: 2
    title: "Two"
    status: complete
    doc: "02-hand.md"
  - id: 3
    title: "Three"
    status: planned
    doc: "03-hand.md"`, 1)
	r := rootFrom(t, map[string]string{
		"Plans/Sample/README.md":         threePhaseReadme,
		"Plans/Sample/01-core.md":        completeGeneratedPhaseView("Sample", "1", "One"),
		"Plans/Sample/02-hand.md":        handAuthoredCompletePhase("Sample", "2", "Two", "02-hand.md"),
		"Plans/Sample/03-hand.md":        handAuthoredPlannedPhase("Sample", "3", "Three"),
		"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1"}, nil),
	})

	diags := Run(r)
	byCode := map[string][]Diagnostic{}
	for _, d := range diags {
		byCode[d.Code] = append(byCode[d.Code], d)
	}

	// The generated view (01-core.md) must still fire NONE of the exempted
	// codes.
	for _, code := range []string{"SDD059", "SDD070", "SDD157", "SDD158", "SDD166", "SDD167"} {
		for _, d := range byCode[code] {
			if d.Path == "Plans/Sample/01-core.md" {
				t.Errorf("generated view: %s fired and should not have: %s", code, d.Message)
			}
		}
	}

	// The hand-authored doc (02-hand.md) must still be held to SDD070 and
	// SDD157 (and at least one of the phase-review family).
	for _, code := range []string{"SDD070", "SDD157"} {
		found := false
		for _, d := range byCode[code] {
			if d.Path == "Plans/Sample/02-hand.md" {
				found = true
			}
		}
		if !found {
			t.Errorf("hand-authored phase doc beside a graph plan: %s did not fire (all diagnostics: %v)", code, codesOf(diags))
		}
	}

	// The README-versus-phase-doc status cross-check (SDD059) must fire for
	// the hand-authored phase's disagreement even though the plan overall
	// is a graph plan — only the generated view (01-core.md) is exempt.
	found := false
	for _, d := range byCode["SDD059"] {
		if d.Path == "Plans/Sample/README.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("hand-authored phase doc beside a graph plan: SDD059 did not fire (all diagnostics: %v)", codesOf(diags))
	}
}

// TestIsGraphPlanMemoizesStat is the review-execution F-02 memoization
// regression: isGraphPlan is called from five rule sites over every artifact
// in a plan directory (headings.go x2, phasereview.go, plan.go, evidence.go)
// and today re-stats the filesystem on every single call — the package's
// established convention (repoCache, sectionCache in root.go) is to memoize
// per-Root instead. With several phase docs and the full rule set running,
// the underlying stat must execute at most once per plan directory per Root.
func TestIsGraphPlanMemoizesStat(t *testing.T) {
	r := rootFrom(t, map[string]string{
		"Plans/Sample/README.md":         completeGraphPlanReadme("Sample", "01-core.md", "One"),
		"Plans/Sample/01-core.md":        completeGeneratedPhaseView("Sample", "1", "One"),
		"Plans/Sample/02-more.md":        completeGeneratedPhaseView("Sample", "2", "Two"),
		"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1"}, nil),
	})

	var calls int
	orig := statGraphFile
	statGraphFile = func(name string) (os.FileInfo, error) {
		calls++
		return orig(name)
	}
	t.Cleanup(func() { statGraphFile = orig })

	Run(r)

	if calls > 1 {
		t.Errorf("statGraphFile called %d times for one plan directory in one Root; want at most 1 (memoized)", calls)
	}
}
