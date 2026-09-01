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
