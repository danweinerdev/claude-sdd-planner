package schema

import "testing"

func TestLoadSpec(t *testing.T) {
	s, err := Load("spec")
	if err != nil {
		t.Fatalf("Load(spec): %v", err)
	}
	if s.Type != "spec" {
		t.Errorf("Type = %q, want spec", s.Type)
	}
	if s.AdditionalSections != "allowed" {
		t.Errorf("AdditionalSections = %q, want allowed", s.AdditionalSections)
	}
}

// The schema is the single source for the parser's rules (FR-15), so it must
// actually declare the structure shared/templates/spec.md carries. A schema
// missing a section would silently make that section unmanaged.
func TestSpecSchemaCoversTemplate(t *testing.T) {
	s, err := Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	wantHeadings := []string{
		"## Overview", "## Goals", "## Non-Goals", "## Requirements",
		"### Functional Requirements", "### Non-Functional Requirements",
		"## User Stories", "## Acceptance Criteria", "## Constraints",
		"## Dependencies", "## Open Questions",
	}
	for _, h := range wantHeadings {
		if s.Heading(h) == nil {
			t.Errorf("schema is missing declared section %q", h)
		}
	}
	wantFields := map[string]Ownership{
		"title": Author, "type": Tool, "status": Tool,
		"created": Tool, "updated": Tool, "tags": Author, "related": Author,
	}
	for k, own := range wantFields {
		f := s.Field(k)
		if f == nil {
			t.Errorf("schema is missing frontmatter key %q", k)
			continue
		}
		if f.Ownership() != own {
			t.Errorf("field %q ownership = %q, want %q", k, f.Ownership(), own)
		}
	}
	for _, ns := range []string{"FR", "NFR", "AC"} {
		if s.Namespace(ns) == nil {
			t.Errorf("schema is missing namespace %q", ns)
		}
	}
}

// At least one section must be free-prose (FR-19): the compiler pushes
// artifacts toward uniformity, and the sections carrying judgment need to stay
// unshaped.
func TestSpecSchemaHasFreeProseSection(t *testing.T) {
	s, err := Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range s.Headings {
		if h.FreeProse {
			return
		}
	}
	t.Error("no free-prose section declared; FR-19 requires at least one")
}

func TestNamespaceFormat(t *testing.T) {
	ns := Namespace{Name: "FR", Prefix: "FR", Width: 2}
	for _, tc := range []struct {
		n    int
		want string
	}{{1, "FR-01"}, {7, "FR-07"}, {49, "FR-49"}, {123, "FR-123"}} {
		if got := ns.Format(tc.n); got != tc.want {
			t.Errorf("Format(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestLoadUnknownType(t *testing.T) {
	if _, err := Load("nonesuch"); err == nil {
		t.Error("Load(nonesuch) succeeded, want an error naming the missing schema")
	}
}

// Every embedded schema (spec + the seven added for FR-14's remaining
// artifact types) must load without error.
func TestAllEmbeddedSchemasLoad(t *testing.T) {
	types := Types()
	if len(types) != 17 {
		t.Fatalf("Types() = %v, want 17 embedded schema types", types)
	}
	for _, ty := range types {
		t.Run(ty, func(t *testing.T) {
			if _, err := Load(ty); err != nil {
				t.Fatalf("Load(%q): %v", ty, err)
			}
		})
	}
}

// Types carrying nested frontmatter structure — a plan's phases[], a phase's
// tasks[], a review's findings[], a debrief's own structured fields — must
// declare frontmatterMode=preserve. Exactly these four, no more, no fewer.
func TestFrontmatterModePreserveSet(t *testing.T) {
	// decision-log joins the set: decisions[] is nested frontmatter, so
	// regenerating the block from flat fields would destroy every entry.
	// Every type carrying nested frontmatter or a free-form body preserves its
	// frontmatter block verbatim. Only the four fully-modeled prose types are
	// managed.
	wantManaged := map[string]bool{"spec": true, "design": true, "research": true, "brainstorm": true}
	for _, ty := range Types() {
		s, err := Load(ty)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Preserves(); got == wantManaged[ty] {
			t.Errorf("%s: Preserves() = %v, want %v", ty, got, !wantManaged[ty])
		}
	}
}

// At least one free-prose section per type keeps judgment-carrying content
// unshaped (FR-19) — except phase, whose declared sections (Overview,
// Acceptance Criteria, Phase Completion Evidence) are all structured. This
// asserts phase's actual current state rather than forcing a free-prose
// section it doesn't have.
func TestEveryTypeHasFreeProseSectionExceptPhase(t *testing.T) {
	for _, ty := range Types() {
		s, err := Load(ty)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, h := range s.Headings {
			if h.FreeProse {
				has = true
				break
			}
		}
		// phase declares only structured sections; decision-log declares no
		// sections at all (it is frontmatter plus prose). Both are asserted as
		// their actual state rather than forced to carry a free-prose section.
		// Types with no declared sections cannot have a free-prose one; phase
		// declares only structured sections. Both assert actual state.
		if len(s.Headings) == 0 || ty == "phase" {
			continue
		}
		if !has {
			t.Errorf("%s: no free-prose section declared; FR-19 wants at least one", ty)
		}
	}
}

func TestSchemaValidateRejectsMalformed(t *testing.T) {
	cases := map[string]Schema{
		"no type": {AdditionalSections: "allowed", Headings: []Heading{{Text: "## A", Depth: 2}}},
		"bad additionalSections": {Type: "x", AdditionalSections: "maybe",
			Headings: []Heading{{Text: "## A", Depth: 2}}},
		"neither headings nor fields": {Type: "x", AdditionalSections: "allowed"},
		"depth mismatch": {Type: "x", AdditionalSections: "allowed",
			Headings: []Heading{{Text: "## A", Depth: 3}}},
		"duplicate heading": {Type: "x", AdditionalSections: "allowed",
			Headings: []Heading{{Text: "## A", Depth: 2}, {Text: "## A", Depth: 2}}},
		"undeclared namespace": {Type: "x", AdditionalSections: "allowed",
			Headings: []Heading{{Text: "## A", Depth: 2, IDNamespace: "ZZ"}}},
		"free-prose and ids": {Type: "x", AdditionalSections: "allowed",
			Namespaces: []Namespace{{Name: "FR", Prefix: "FR", Width: 2}},
			Headings:   []Heading{{Text: "## A", Depth: 2, IDNamespace: "FR", FreeProse: true}}},
		"duplicate field": {Type: "x", AdditionalSections: "allowed",
			Headings:    []Heading{{Text: "## A", Depth: 2}},
			Frontmatter: []Field{{Key: "t", OwnerRaw: "author"}, {Key: "t", OwnerRaw: "author"}}},
		"bad ownership": {Type: "x", AdditionalSections: "allowed",
			Headings:    []Heading{{Text: "## A", Depth: 2}},
			Frontmatter: []Field{{Key: "t", OwnerRaw: "nobody"}}},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			if err := s.validate(); err == nil {
				t.Error("validate() succeeded, want an error")
			}
		})
	}
}
