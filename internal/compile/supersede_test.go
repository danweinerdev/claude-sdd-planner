package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
)

// specWith builds a minimal spec whose Functional Requirements are the given
// lines, so a test can state exactly the identifier situation it cares about.
func specWith(requirements ...string) string {
	return "---\ntitle: \"T\"\ntype: spec\nstatus: draft\ncreated: 2026-01-01\n" +
		"updated: 2026-01-01\ntags: []\nrelated: []\n---\n\n" +
		"## Overview\nx\n\n## Goals\n- g\n\n## Non-Goals\n- n\n\n" +
		"## Requirements\n### Functional Requirements\n" + strings.Join(requirements, "\n") +
		"\n\n### Non-Functional Requirements\n- **NFR-01**: perf\n\n" +
		"## User Stories\nu\n\n## Constraints\nc\n\n## Dependencies\nd\n\n" +
		"## Acceptance Criteria\n- **AC-01**: a\n\n## Open Questions\nNone.\n"
}

func compileSpec(t *testing.T, existing, payload string, opts Options) *Result {
	t.Helper()
	s, err := schema.Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	if existing != "" {
		opts.Existing = artifact.Parse(existing)
	}
	if opts.Retire == nil {
		opts.Retire = map[string]bool{}
	}
	opts.Today = "2026-01-01"
	return Compile(s, payload, opts)
}

// TestSupersedeCarriesIdentifiersForward is the core of the reported problem:
// rewriting a spec's requirements made every item look new and every existing
// identifier look deleted, so apply both renumbered the requirements AND
// refused for mass retirement — two outputs that contradicted each other.
func TestSupersedeCarriesIdentifiersForward(t *testing.T) {
	existing := specWith("- **FR-01**: original one", "- **FR-02**: original two")
	payload := specWith("- rewritten one", "- rewritten two")

	// Without --supersede: the rewrite is read as deletion plus new items.
	plain := compileSpec(t, existing, payload, Options{})
	if plain.OK() {
		t.Error("a full rewrite without --supersede should refuse; it looks like mass deletion")
	}

	res := compileSpec(t, existing, payload, Options{Supersede: true})
	if !res.OK() {
		t.Fatalf("supersede refused a clean rewrite: %v", res.Refusals)
	}
	if len(res.Carried) != 2 {
		t.Fatalf("want 2 carried identifiers, got %d: %v", len(res.Carried), res.Carried)
	}
	// Positional inheritance: the Nth rewritten item keeps the Nth identifier.
	if !strings.Contains(res.Output, "**FR-01**: rewritten one") {
		t.Errorf("FR-01 did not carry onto the first rewritten item:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "**FR-02**: rewritten two") {
		t.Errorf("FR-02 did not carry onto the second rewritten item:\n%s", res.Output)
	}
	// Nothing was renumbered.
	if strings.Contains(res.Output, "FR-03") {
		t.Errorf("supersede allocated a new identifier instead of carrying forward:\n%s", res.Output)
	}
}

// A supersede that drops content retires the surplus identifier rather than
// refusing: dropping requirements is the ordinary meaning of a rewrite, and
// reporting it keeps the removal visible.
func TestSupersedeRetiresSurplusIdentifiers(t *testing.T) {
	existing := specWith("- **FR-01**: one", "- **FR-02**: two")
	payload := specWith("- only one now")

	res := compileSpec(t, existing, payload, Options{Supersede: true})
	if !res.OK() {
		t.Fatalf("supersede refused a shrinking rewrite: %v", res.Refusals)
	}
	if len(res.Retired) != 1 || res.Retired[0] != "FR-02" {
		t.Errorf("want FR-02 retired, got %v", res.Retired)
	}
}

// Growing a rewrite carries what it can and allocates the rest.
func TestSupersedeAllocatesBeyondCarriedIdentifiers(t *testing.T) {
	existing := specWith("- **FR-01**: one")
	payload := specWith("- one rewritten", "- brand new second")

	res := compileSpec(t, existing, payload, Options{Supersede: true})
	if !res.OK() {
		t.Fatalf("supersede refused a growing rewrite: %v", res.Refusals)
	}
	if len(res.Carried) != 1 || !strings.Contains(res.Carried[0], "FR-01") {
		t.Errorf("want FR-01 carried, got %v", res.Carried)
	}
	if len(res.Allocations) != 1 || !strings.Contains(res.Allocations[0], "FR-02") {
		t.Errorf("want FR-02 allocated for the new item, got %v", res.Allocations)
	}
}

// TestPayloadMayIntroduceNextIdentifier covers the separate report that adding
// a new stable id was rejected as "does not exist". A namespace with no live
// identifiers could not be started at all: the refusal's correction printed
// "live NFR identifiers:" with nothing after it, naming no usable option.
func TestPayloadMayIntroduceNextIdentifier(t *testing.T) {
	// AC-02 is the next identifier after the fixture's AC-01.
	existing := specWith("- **FR-01**: one")
	payload := strings.Replace(specWith("- **FR-01**: one"),
		"- **AC-01**: a", "- **AC-01**: a\n- **AC-02**: second criterion", 1)

	res := compileSpec(t, existing, payload, Options{})
	if !res.OK() {
		t.Fatalf("declaring the next identifier was refused: %v", res.Refusals)
	}
	if !strings.Contains(res.Output, "**AC-02**: second criterion") {
		t.Errorf("AC-02 is missing from the output:\n%s", res.Output)
	}
}

// A gap is still an authoring mistake, not an intent — and the refusal must
// name the identifier to use instead.
func TestPayloadMayNotSkipIdentifiers(t *testing.T) {
	existing := specWith("- **FR-01**: one")
	payload := strings.Replace(specWith("- **FR-01**: one"),
		"- **AC-01**: a", "- **AC-01**: a\n- **AC-07**: skipped ahead", 1)

	res := compileSpec(t, existing, payload, Options{})
	if res.OK() {
		t.Fatal("declaring AC-07 after AC-01 should refuse: it leaves a gap")
	}
	var found bool
	for _, r := range res.Refusals {
		if r.Code == "SPK030" && strings.Contains(r.Correction, "AC-02") {
			found = true
		}
	}
	if !found {
		t.Errorf("the refusal should name AC-02 as the identifier to use: %v", res.Refusals)
	}
}
