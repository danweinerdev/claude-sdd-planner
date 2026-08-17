package main

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
)

// Every generated template must be free of SCHEMA-SHAPE violations. The
// starting point the tool hands an author was itself invalid: `waivers: ""`
// tripped SDD176 on five types and `template plan` omitted `phases`, which
// SDD052 requires — 10 violations on a fresh `sdd template plan --out`.
// Unfilled {{PLACEHOLDER}} values are expected and not checked here.
func TestGeneratedTemplatesDeclareListFieldsAsLists(t *testing.T) {
	// Keys the validators require to be lists wherever a schema declares them.
	// Naming them here rather than deriving from the schema is the point: a
	// schema that stops declaring `[]` for one of these is exactly the
	// regression this guards, so the expectation cannot come from the schema.
	listKeys := map[string]bool{
		"tags": true, "related": true, "waivers": true,
		"phases": true, "tasks": true, "findings": true,
		"followups": true, "decisions": true, "lane_results": true,
	}
	for _, typ := range schema.Types() {
		s, err := schema.Load(typ)
		if err != nil {
			t.Fatalf("load %s: %v", typ, err)
		}
		body, err := renderTemplateFor(typ, false)
		if err != nil {
			t.Fatalf("render %s: %v", typ, err)
		}
		for _, f := range s.Frontmatter {
			if !listKeys[f.Key] && f.Entry == nil {
				continue
			}
			if strings.Contains(body, "\n"+f.Key+`: ""`) {
				t.Errorf("%s template renders list field %q as \"\" — the validators require a list "+
					"(declare \"default\": \"[]\" in internal/schema/%s.json)", typ, f.Key, typ)
			}
		}
	}
}

// SDD052 requires every plan to carry `phases` as a list, so the plan schema
// must declare the field — it did not, and `sdd template plan` therefore
// emitted a plan that failed validation immediately.
func TestPlanSchemaDeclaresPhases(t *testing.T) {
	s, err := schema.Load("plan")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Frontmatter {
		if f.Key == "phases" {
			if f.Default != "[]" {
				t.Errorf("plan `phases` default = %q, want \"[]\"", f.Default)
			}
			return
		}
	}
	t.Fatal("the plan schema declares no `phases` field, but SDD052 requires one")
}
