package main

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
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

// A design's DD declaration syntax must be discoverable from CLI output.
// The authored shared template and design skill already documented the
// top-level bold-bullet form, but schema-generated `sdd template design`
// emitted only a TODO — leaving users to try unsupported `### DD-N`
// subsection headings, which correctly are not apply slots and therefore do
// not register identifiers for SPK040 self-citation resolution.
func TestDesignTemplateShowsCanonicalDDDeclaration(t *testing.T) {
	for _, forApply := range []bool{false, true} {
		body, err := renderTemplateFor("design", forApply)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "\n- **DD-1**: [Title].") {
			t.Errorf("forApply=%v: design template must show the canonical DD bullet declaration:\n%s", forApply, body)
		}
		if !strings.Contains(body, "Heading-form decisions (`### DD-N`) are subsections, not declarations") {
			t.Errorf("forApply=%v: template must explicitly reject the tempting heading form", forApply)
		}
	}
}

// TemplateBody is template-only guidance. Migration must keep using the
// neutral DefaultBody — inserting a placeholder DD-1 into an existing design
// would assert that the artifact made a decision it never made.
func TestDesignDecisionMigrateDefaultDoesNotInventDecision(t *testing.T) {
	s, err := schema.Load("design")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range s.Headings {
		if h.Text != "## Design Decisions" {
			continue
		}
		if !strings.HasPrefix(h.DefaultBody, "TODO:") {
			t.Errorf("migrate default = %q, want neutral TODO", h.DefaultBody)
		}
		if !strings.Contains(h.TemplateBody, "- **DD-1**:") {
			t.Errorf("template body lacks DD exemplar: %q", h.TemplateBody)
		}
		return
	}
	t.Fatal("design schema has no Design Decisions heading")
}
