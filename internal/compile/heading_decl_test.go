package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

// designPayload builds a design payload whose Design Decisions section is
// caller-supplied and whose Architecture prose carries the given citation
// sentence. Mirrors the field defect report: a real design declaring its
// decisions as `### DD-N — Title` subsections could not pass `sdd apply`
// (SPK040 "available DD identifiers: (empty)") even though `sdd validate`
// and graph compile resolve those same headings.
func designPayload(decisions, architectureProse string) string {
	return `---
title: "D"
tags: []
related: []
---

# D

## Overview
Overview.

## Non-Goals
None.

## Architecture
` + architectureProse + `

### Components
Components.

### Data Flow
Flow.

### Interfaces
Interfaces.

## Design Decisions

` + decisions + `

## Error Handling
Errors.

## Testing Strategy
Tests.

### Structural Verification
Checks.

## Migration / Rollout
Rollout.
`
}

func loadDesign(t *testing.T) *schema.Schema {
	t.Helper()
	s, err := schema.Load("design")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The reported failing case, reduced: `### DD-1 — X` under Design Decisions
// plus one prose DD-1 citation. The payload's own loose-form ids must be in
// SPK040's candidate table (they are hoisted out of the slot as additional
// sections, so the per-slot bullet scan never sees them).
func TestDesignHeadingDDResolvesSelfCitation(t *testing.T) {
	s := loadDesign(t)
	for name, decisions := range map[string]string{
		"em-dash":      "### DD-1 — First\n\nBody one.\n\n### DD-2 — Second\n\nBody two.",
		"colon":        "### DD-1: First\n\nBody one.\n\n### DD-2: Second\n\nBody two.",
		"loose-bullet": "- **DD-1 — First**: body one.\n- **DD-2 — Second**: body two.",
	} {
		t.Run(name, func(t *testing.T) {
			r := Compile(s, designPayload(decisions, "The architecture follows DD-1 and DD-2."), Options{Today: "2026-09-03"})
			if !r.OK() {
				t.Fatalf("payload-local loose-form DD declarations must be in the citation table: %+v", r.Refusals)
			}
			if !strings.Contains(r.Output, "The architecture follows DD-1 and DD-2.") {
				t.Fatalf("citation prose lost:\n%s", r.Output)
			}
		})
	}
}

// Heading-form DD sections are retained in source position, and editing such
// a design must not read its heading-declared ids as unintended retirements
// (SPK031) — the exact trap that made heading-form designs uneditable.
func TestDesignHeadingDDEditRoundTrip(t *testing.T) {
	s := loadDesign(t)
	payload := designPayload("### DD-1 — First\n\nBody one.\n\n### DD-2 — Second\n\nBody two.", "Follows DD-2.")
	created := Compile(s, payload, Options{Today: "2026-09-03"})
	if !created.OK() {
		t.Fatalf("create: %+v", created.Refusals)
	}
	if !strings.Contains(created.Output, "## Design Decisions") ||
		strings.Index(created.Output, "### DD-1 — First") < strings.Index(created.Output, "## Design Decisions") {
		t.Fatalf("heading decisions must stay below their parent slot:\n%s", created.Output)
	}
	existing := artifact.Parse(created.Output)
	edited := Compile(s, created.Output, Options{Existing: existing, Today: "2026-09-03"})
	if !edited.OK() {
		t.Fatalf("re-applying the same heading-form design must not retire its DDs: %+v", edited.Refusals)
	}
}

// Allocation must treat heading-declared numbers as claimed: a new unnumbered
// bullet decision in the slot gets the next free id, not a duplicate of an
// existing heading's.
func TestDesignHeadingDDAllocationSkipsClaimed(t *testing.T) {
	s := loadDesign(t)
	base := designPayload("### DD-1 — First\n\nBody one.\n\n### DD-2 — Second\n\nBody two.", "Follows DD-1.")
	created := Compile(s, base, Options{Today: "2026-09-03"})
	if !created.OK() {
		t.Fatalf("create: %+v", created.Refusals)
	}
	existing := artifact.Parse(created.Output)
	// An unnumbered bullet in an identifier-bearing section is a new item;
	// allocation must number it past the heading-claimed ids.
	edit := strings.Replace(created.Output,
		"## Design Decisions\n",
		"## Design Decisions\n\n- New bullet decision needing an id.\n", 1)
	r := Compile(s, edit, Options{Existing: existing, Today: "2026-09-03"})
	if !r.OK() {
		t.Fatalf("edit: %+v", r.Refusals)
	}
	if !strings.Contains(r.Output, "**DD-3**: New bullet decision needing an id.") {
		t.Fatalf("new item must be allocated DD-3 (DD-1/DD-2 are heading-claimed):\n%s", r.Output)
	}
}

// Coherence boundary: the validator accepts loose forms only for DD. A spec
// declaring `### FR-99` must NOT register it — otherwise apply would write
// documents `sdd validate` then refuses.
func TestSpecHeadingFRDoesNotDeclare(t *testing.T) {
	s, err := schema.Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	payload := `---
title: "S"
tags: []
related: []
---

# S

## Overview
Cites FR-99 which no bullet declares.

## Goals
Goals.

## Non-Goals
None.

## Requirements

- **FR-01**: Does a thing.

### FR-99

Not a declaration.

## User Stories
Stories.

## Acceptance Criteria

- [ ] **AC-01**: Verifies the thing.

## Constraints

- **NFR-01**: Is fast.

## Dependencies
None.

## Open Questions
None.
`
	r := Compile(s, payload, Options{Today: "2026-09-03"})
	if r.OK() {
		t.Fatal("a spec's heading-form FR must not register as a declaration")
	}
	found := false
	for _, ref := range r.Refusals {
		if ref.Code == "SPK040" && strings.Contains(ref.Message, "FR-99") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SPK040 for FR-99, got: %+v", r.Refusals)
	}
}
