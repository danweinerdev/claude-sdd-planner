package ymlite

import "testing"

// The real shape this parser exists for: a plan README's phases[].
const phases = `title: "Some Plan"
status: draft
phases:
  - id: 1
    title: "First Phase"
    status: planned
    doc: "01-First-Phase.md"
    depends_on: []
  - id: 2
    title: "Second Phase"
    status: in-progress
    doc: "02-Second-Phase.md"
    depends_on: [1]
`

func TestSequenceOfFlatMaps(t *testing.T) {
	items := Sequence(phases, "phases")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	if got := items[0].Str("title"); got != "First Phase" {
		t.Errorf("items[0].title = %q, want First Phase (quotes stripped)", got)
	}
	if got := items[1].Str("status"); got != "in-progress" {
		t.Errorf("items[1].status = %q", got)
	}
	if got := items[1].Str("id"); got != "2" {
		t.Errorf("items[1].id = %q, want 2", got)
	}
}

// depends_on drives the plan dependency graph, so an empty list and a populated
// one must be distinguishable — a nil-vs-[1] confusion would invent or erase
// edges.
func TestFlowListDecoding(t *testing.T) {
	items := Sequence(phases, "phases")
	if got := items[0].List("depends_on"); len(got) != 0 {
		t.Errorf("empty flow list decoded as %v, want none", got)
	}
	got := items[1].List("depends_on")
	if len(got) != 1 || got[0] != "1" {
		t.Errorf("depends_on = %v, want [1]", got)
	}
}

func TestListHandlesMultipleAndQuoted(t *testing.T) {
	src := "x:\n  - a: [one, \"two\", 'three']\n"
	got := Sequence(src, "x")[0].List("a")
	want := []string{"one", "two", "three"}
	if len(got) != 3 {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("element %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A bare scalar where a list was expected is tolerated as a one-element list, so
// an author writing `depends_on: 1` is not silently read as no dependency.
func TestScalarReadsAsSingleElementList(t *testing.T) {
	src := "x:\n  - a: 1\n"
	got := Sequence(src, "x")[0].List("a")
	if len(got) != 1 || got[0] != "1" {
		t.Errorf("scalar-as-list = %v, want [1]", got)
	}
}

func TestAbsentKeyAndAbsentField(t *testing.T) {
	if items := Sequence(phases, "tasks"); len(items) != 0 {
		t.Errorf("Sequence for an absent key = %+v, want none", items)
	}
	it := Sequence(phases, "phases")[0]
	if got := it.Str("nonesuch"); got != "" {
		t.Errorf("Str for an absent field = %q, want empty", got)
	}
	if got := it.List("nonesuch"); got != nil {
		t.Errorf("List for an absent field = %v, want nil", got)
	}
}

// The sequence must stop at the next top-level key. Reading past it would fold
// unrelated frontmatter into the last item.
func TestSequenceStopsAtNextTopLevelKey(t *testing.T) {
	src := `phases:
  - id: 1
    title: "One"
tags: [a, b]
related: [Specs/Thing]
`
	items := Sequence(src, "phases")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	for _, leaked := range []string{"tags", "related"} {
		if _, ok := items[0][leaked]; ok {
			t.Errorf("top-level key %q leaked into the sequence item", leaked)
		}
	}
}

// Comment and blank lines inside a sequence are common in real artifacts (the
// templates ship commented example entries) and must not become fields.
func TestCommentsAndBlanksIgnored(t *testing.T) {
	src := `phases:
  # this is an example entry
  - id: 1

    title: "One"
    # trailing note
`
	items := Sequence(src, "phases")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	if got := items[0].Str("title"); got != "One" {
		t.Errorf("title = %q, want One", got)
	}
	if len(items[0]) != 2 {
		t.Errorf("item has %d fields (%v), want exactly id and title", len(items[0]), items[0])
	}
}

// A value containing a colon (a statement, a URL) must not be truncated at the
// first colon — ledger statements routinely contain them.
func TestValueContainingColon(t *testing.T) {
	src := "decisions:\n  - id: D-0001\n    statement: \"Ships as three layers: a doc, a skill, a command.\"\n"
	got := Sequence(src, "decisions")[0].Str("statement")
	want := "Ships as three layers: a doc, a skill, a command."
	if got != want {
		t.Errorf("statement = %q, want %q", got, want)
	}
}

func TestEmptySequence(t *testing.T) {
	if items := Sequence("phases: []\n", "phases"); len(items) != 0 {
		t.Errorf("empty inline sequence = %+v, want none", items)
	}
}
