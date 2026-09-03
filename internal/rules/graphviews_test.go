package rules

// v1 validation coexisting with graph projections (Plans/SddGraph 5.6, filed
// from the self-hosting pilot's findings): compile renders phase-doc views
// and upserts a marker-delimited Graph View section into the plan README.
// Both are PROJECTIONS of the committed graph — not v1 lifecycle intent —
// so SDD163's phases[]-listing demand skips marker-carrying views, and
// SDD174's lifecycle normalization strips the README's generated section.

import (
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
		"Plans/Sample/README.md":  planReadmeWithGraphView(true),
		"Plans/Sample/01-One.md":  viewPhaseDoc("1", "One", false),
		"Plans/Sample/02-view.md": viewPhaseDoc("2", "view", true),
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
		"Specs/A/README.md":  traceSpec("A"),
		"Specs/B/README.md":  traceSpec("B"),
		"Plans/P/README.md":  traceGraphPlan,
		"Plans/P/P-Graph.json": traceGraph("Specs/A:FR-01", "A:AC-01", "Specs/B:FR-01", "B:AC-01"),
	})
	for _, d := range Run(r) {
		if d.Code == "SDD160" || d.Code == "SDD162" {
			t.Errorf("full per-spec coverage via graph justifies must satisfy traceability: %s %s", d.Code, d.Message)
		}
	}
}

func TestGraphPlanTraceabilityIsPerSpec(t *testing.T) {
	// Specs/B's criterion is uncovered; a BARE ambiguous justification of
	// AC-01 covers neither spec (never first-wins).
	r := rootFrom(t, map[string]string{
		"Specs/A/README.md":  traceSpec("A"),
		"Specs/B/README.md":  traceSpec("B"),
		"Plans/P/README.md":  traceGraphPlan,
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
