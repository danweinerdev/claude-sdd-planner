package main

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

const sectionSrc = `---
title: "Thing"
type: spec
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: [x]
related: []
---

# Thing

## Overview
Old overview text.

## Goals
- do it
`

func loadSpecSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, err := schema.Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSetSectionReplacesOnlyTargetBody(t *testing.T) {
	doc := artifact.Parse(sectionSrc)
	out, refs := setSection(doc, loadSpecSchema(t), "## Overview", "New overview text.\n", "2026-08-04")
	if len(refs) != 0 {
		t.Fatalf("setSection refused: %v", refs)
	}
	if !strings.Contains(out, "## Overview\nNew overview text.\n") {
		t.Errorf("target section not replaced:\n%s", out)
	}
	if !strings.Contains(out, "## Goals\n- do it\n") {
		t.Errorf("untouched section changed:\n%s", out)
	}
	if !strings.Contains(out, "updated: 2026-08-04") {
		t.Errorf("updated not restamped:\n%s", out)
	}
	if strings.Contains(out, "updated: 2026-08-01") {
		t.Errorf("stale updated survived:\n%s", out)
	}
	if !strings.Contains(out, "title: \"Thing\"") || !strings.Contains(out, "tags: [x]") {
		t.Errorf("frontmatter not preserved verbatim:\n%s", out)
	}
}

func TestSetSectionRefusesAbsentHeading(t *testing.T) {
	doc := artifact.Parse(sectionSrc)
	_, refs := setSection(doc, loadSpecSchema(t), "## Nonexistent", "body\n", "2026-08-04")
	if len(refs) == 0 {
		t.Fatal("setSection succeeded targeting a heading absent from the artifact")
	}
	if refs[0].Code != "SEC010" {
		t.Errorf("code = %q, want SEC010", refs[0].Code)
	}
	if !strings.Contains(refs[0].Correction, "## Overview") || !strings.Contains(refs[0].Correction, "## Goals") {
		t.Errorf("refusal does not list the actual headings: %s", refs[0].Correction)
	}
}

func TestSetSectionRefusesFrontmatterDelimiterInPayload(t *testing.T) {
	doc := artifact.Parse(sectionSrc)
	_, refs := setSection(doc, loadSpecSchema(t), "## Overview", "line one\n---\nline two\n", "2026-08-04")
	if len(refs) == 0 {
		t.Fatal("setSection succeeded with a --- delimiter in the payload")
	}
	if refs[0].Code != "SEC020" {
		t.Errorf("code = %q, want SEC020", refs[0].Code)
	}
}

func TestSetSectionRefusesShallowerHeadingInPayload(t *testing.T) {
	doc := artifact.Parse(sectionSrc)
	_, refs := setSection(doc, loadSpecSchema(t), "## Overview", "text\n## Not Allowed\nmore\n", "2026-08-04")
	if len(refs) == 0 {
		t.Fatal("setSection succeeded with a same-depth heading in the payload")
	}
	if refs[0].Code != "SEC021" {
		t.Errorf("code = %q, want SEC021", refs[0].Code)
	}
}

func TestSetSectionAllowsDeeperSubheadingInPayload(t *testing.T) {
	doc := artifact.Parse(sectionSrc)
	out, refs := setSection(doc, loadSpecSchema(t), "## Overview", "text\n### A Deeper Subheading\nmore\n", "2026-08-04")
	if len(refs) != 0 {
		t.Fatalf("setSection refused a deeper subheading: %v", refs)
	}
	if !strings.Contains(out, "### A Deeper Subheading") {
		t.Errorf("deeper subheading dropped:\n%s", out)
	}
}

func TestFindSectionExactMatch(t *testing.T) {
	doc := artifact.Parse(sectionSrc)
	sec, actual := findSection(doc, "## Goals")
	if sec == nil {
		t.Fatal("findSection did not find an existing heading")
	}
	if actual != nil {
		t.Errorf("actual = %v, want nil on a successful match", actual)
	}
	if sec.Heading != "## Goals" {
		t.Errorf("Heading = %q, want ## Goals", sec.Heading)
	}
}

func TestLintSectionBodyAllowsPlainProse(t *testing.T) {
	refs := lintSectionBody([]string{"just some text", "- a list item"}, 2)
	if len(refs) != 0 {
		t.Errorf("lintSectionBody refused plain prose: %v", refs)
	}
}
