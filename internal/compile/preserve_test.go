package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

// planSrc is a minimally valid `plan` artifact whose frontmatter carries a
// nested block (phases[]) the flat field model cannot represent. It is
// intentionally self-consistent (tool-owned fields already match "on disk")
// so a round-trip compile against itself as Existing succeeds.
const planSrc = `---
title: "Thing"
type: plan
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: [x]
related: []
phases:
  - id: 1
    name: First
  - id: 2
    name: Second
---

# Thing

## Overview
A thing.

## Non-Goals
- not that

## Architecture
Some architecture.

## Key Decisions
- decided X

## Dependencies
- none

## Plan Completion Evidence
- pending
`

func loadPlan(t *testing.T) *schema.Schema {
	t.Helper()
	s, err := schema.Load("plan")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A preserve schema's nested frontmatter block round-trips byte-identical,
// and `updated` is restamped even though the tool never models phases[].
func TestPreserveSchemaRoundTripsNestedFrontmatter(t *testing.T) {
	existing := artifact.Parse(planSrc)
	r := Compile(loadPlan(t), planSrc, Options{Today: "2026-08-04", Existing: existing})
	mustOK(t, r)

	if !strings.Contains(r.Output, "phases:\n  - id: 1\n    name: First\n  - id: 2\n    name: Second\n") {
		t.Errorf("nested frontmatter block not preserved byte-identical:\n%s", r.Output)
	}
	if !strings.Contains(r.Output, "updated: 2026-08-04") {
		t.Errorf("updated not restamped:\n%s", r.Output)
	}
	if strings.Contains(r.Output, "updated: 2026-08-01") {
		t.Errorf("stale updated value survived compilation:\n%s", r.Output)
	}
}

// An undeclared frontmatter key is refused under a managed schema (SPK020)
// but tolerated verbatim under a preserve schema, because preserve exists
// precisely so nested/unmodeled structure doesn't make the artifact unwritable.
func TestUndeclaredFrontmatterKeyManagedVsPreserve(t *testing.T) {
	t.Run("managed refuses", func(t *testing.T) {
		p := strings.Replace(payload(nil), "tags: [x]", "customKey: nope\ntags: [x]", 1)
		r := compileNew(t, p)
		if r.OK() {
			t.Fatal("compile succeeded with an undeclared frontmatter key under a managed schema")
		}
		if !hasCode(r, "SPK020") {
			t.Errorf("codes = %v, want SPK020", codes(r))
		}
	})

	t.Run("preserve allows", func(t *testing.T) {
		src := strings.Replace(planSrc, "tags: [x]", "customKey: nope\ntags: [x]", 1)
		existing := artifact.Parse(src)
		r := Compile(loadPlan(t), src, Options{Today: "2026-08-01", Existing: existing})
		mustOK(t, r)
		if !strings.Contains(r.Output, "customKey: nope") {
			t.Errorf("undeclared key dropped under a preserve schema:\n%s", r.Output)
		}
	})
}
