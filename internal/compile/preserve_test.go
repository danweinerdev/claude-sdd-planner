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

// planPayloadNoTrio is what `sdd template plan --for-apply` style payloads
// carry: no tool-owned fields (SPK021 refuses them), nested phases[] intact.
const planPayloadNoTrio = `---
title: "Thing"
type: plan
tags: [x]
related: []
phases:
  - id: 1
    name: First
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

// TestPreserveCreateStampsToolOwnedFields (verified defect, sdd <= 2.8.2):
// the preserve branch discarded the compiled frontmatter wholesale, so a
// plan created through the tool's own write path carried no status, created,
// or updated — invalid by construction, refused by this binary's own
// validator (SDD010 x3, SDD012, SDD013 x2), unrepairable by migrate, and
// unexpressable in the payload (SPK021). Tool-owned fields must be stamped
// into the preserved block exactly as the managed path stamps them.
func TestPreserveCreateStampsToolOwnedFields(t *testing.T) {
	r := Compile(loadPlan(t), planPayloadNoTrio, Options{Today: "2026-09-02"})
	if !r.OK() {
		t.Fatalf("create must succeed: %+v", r.Refusals)
	}
	fm := frontmatterBlock(t, r.Output)
	for _, want := range []string{"status: draft", "created: 2026-09-02", "updated: 2026-09-02"} {
		if !strings.Contains(fm, want) {
			t.Errorf("created plan must carry %q; frontmatter:\n%s", want, fm)
		}
	}
	if !strings.Contains(fm, "phases:\n  - id: 1\n    name: First") {
		t.Errorf("nested frontmatter must stay verbatim:\n%s", fm)
	}
}

// TestPreserveEditRestampsToolOwnedFields: an edit whose payload (correctly)
// omits the tool-owned trio must carry the on-disk values through — the
// defect stripped them from previously valid files, byte-identical to the
// broken create output.
func TestPreserveEditRestampsToolOwnedFields(t *testing.T) {
	existing := artifact.Parse(planSrc)
	r := Compile(loadPlan(t), planPayloadNoTrio, Options{Today: "2026-09-02", Existing: existing})
	if !r.OK() {
		t.Fatalf("edit must succeed: %+v", r.Refusals)
	}
	fm := frontmatterBlock(t, r.Output)
	for _, want := range []string{
		"status: draft",       // carried from disk
		"created: 2026-08-01", // carried from disk, never restamped
		"updated: 2026-09-02", // restamped on every write
	} {
		if !strings.Contains(fm, want) {
			t.Errorf("edited plan must carry %q; frontmatter:\n%s", want, fm)
		}
	}
}

func frontmatterBlock(t *testing.T, output string) string {
	t.Helper()
	rest := strings.TrimPrefix(output, "---\n")
	end := strings.Index(rest, "\n---")
	if !strings.HasPrefix(output, "---\n") || end < 0 {
		t.Fatalf("output has no frontmatter block:\n%s", output)
	}
	return rest[:end]
}

// The canonical DD declaration form is a top-level bold bullet inside the
// fixed Design Decisions slot. collectFromMatched must register payload-owned
// ids before SPK040 checks self-citations elsewhere in the same design.
func TestDesignPayloadDeclaredDDResolvesSelfCitation(t *testing.T) {
	s, err := schema.Load("design")
	if err != nil {
		t.Fatal(err)
	}
	payload := `---
title: "D"
tags: []
related: []
---

# D

## Overview
The architecture follows DD-1.

## Non-Goals
None.

## Architecture
Architecture.

### Components
Components.

### Data Flow
Flow.

### Interfaces
Interfaces.

## Design Decisions

- **DD-1**: Choose the stable path.
  Context: c. Decision: x. Rationale: y.

## Error Handling
Errors.

## Testing Strategy
Tests.

### Structural Verification
Checks.

## Migration / Rollout
Rollout.
`
	r := Compile(s, payload, Options{Today: "2026-09-02"})
	if !r.OK() {
		t.Fatalf("canonical payload-owned DD declaration must resolve its self-citation: %+v", r.Refusals)
	}
	if !strings.Contains(r.Output, "The architecture follows DD-1.") ||
		!strings.Contains(r.Output, "- **DD-1**: Choose the stable path.") {
		t.Fatalf("compiled design lost declaration or citation:\n%s", r.Output)
	}
}
