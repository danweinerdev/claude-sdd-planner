package intent

import (
	"strings"
	"testing"
)

const specBody = `
## Requirements

- **FR-01**: The watcher SHALL emit a change event within 500ms of an
  mtime change, and SHALL coalesce bursts into one event.
- **FR-02**: Unknown keys SHALL be rejected by name.

## Acceptance Criteria

- [ ] **AC-01**: A changed value is observable within ` + "`2s`" + ` of the write.
- [x] **AC-02**: An unknown key names itself in the error.
- [ ] ~~**AC-03**~~: *Retired 2026-08-01 — superseded.*

## Design Decisions

- **DD-1**: Title of the decision.
  Context: something. Decision: chosen. Rationale: because.

### DD-2 — Heading-form decision

Body of the heading-form decision.
`

func TestItemsFindsEveryLiveDefinition(t *testing.T) {
	items := Items(specBody)
	for _, id := range []string{"FR-01", "FR-02", "AC-01", "AC-02", "DD-1", "DD-2"} {
		if _, ok := items[id]; !ok {
			t.Fatalf("missing item %s; got %v", id, keys(items))
		}
	}
	if _, ok := items["AC-03"]; ok {
		t.Fatal("a retired (struck-through) id must not be extracted")
	}
	// Spans end at the next definition or heading: FR-01's text must not
	// bleed into FR-02's.
	if strings.Contains(items["FR-01"].Normalized, "Unknown keys") {
		t.Fatalf("FR-01 span bleeds into FR-02: %q", items["FR-01"].Normalized)
	}
	if !strings.Contains(items["FR-01"].Normalized, "coalesce bursts") {
		t.Fatalf("FR-01 span must include its wrapped continuation: %q", items["FR-01"].Normalized)
	}
	if strings.Contains(items["DD-2"].Normalized, "AC-01") {
		t.Fatalf("DD-2 span leaked: %q", items["DD-2"].Normalized)
	}
}

func keys(m map[string]Item) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRewrapAndMarkupDoNotFire(t *testing.T) {
	a := Normalize("- **FR-01**: The watcher SHALL emit a change event within 500ms of an\n  mtime change.")
	b := Normalize("- **FR-01**: The watcher SHALL emit a change event\n  within 500ms of an mtime change.")
	c := Normalize("* [ ] __FR-01__: The watcher SHALL emit a change event within 500ms of an mtime change.")
	if a != b {
		t.Fatalf("rewrap must not change the normalized form:\n%q\n%q", a, b)
	}
	if a != c {
		t.Fatalf("marker and emphasis changes must not change the normalized form:\n%q\n%q", a, c)
	}
	if Hash(a) != Hash(b) || Hash(a) != Hash(c) {
		t.Fatal("equal normalized forms must hash equal")
	}
}

func TestWordAndLiteralChangesFire(t *testing.T) {
	base := Normalize("- **AC-01**: A changed value is observable within `2s` of the write.")
	reworded := Normalize("- **AC-01**: A changed value becomes observable within `2s` of the write.")
	literal := Normalize("- **AC-01**: A changed value is observable within `3s` of the write.")
	if Hash(base) == Hash(reworded) {
		t.Fatal("a wording change must fire — synonymy is the LLM's judgment, not the hasher's")
	}
	if Hash(base) == Hash(literal) {
		t.Fatal("a literal-value change must fire (literal values count)")
	}
	// The backticks themselves are formatting: removing them without
	// changing the value must NOT fire.
	bare := Normalize("- **AC-01**: A changed value is observable within 2s of the write.")
	if Hash(base) != Hash(bare) {
		t.Fatalf("code-span markers are formatting; contents are the value:\n%q\n%q", base, bare)
	}
}

func TestItemsAgreeAcrossEquivalentBodies(t *testing.T) {
	rewrapped := strings.ReplaceAll(specBody,
		"emit a change event within 500ms of an\n  mtime change, and SHALL coalesce bursts into one event.",
		"emit a change event within 500ms of an mtime change,\n  and SHALL coalesce bursts into one event.")
	a, b := Items(specBody), Items(rewrapped)
	if a["FR-01"].Hash != b["FR-01"].Hash {
		t.Fatal("a rewrapped spec must fingerprint identically")
	}
	if a["FR-02"].Hash != b["FR-02"].Hash {
		t.Fatal("untouched items must fingerprint identically")
	}
}
