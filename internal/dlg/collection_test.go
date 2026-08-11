package dlg

import "testing"

func entry(fields map[string]any) map[string]any { return fields }

func ledgerWith(entries ...map[string]any) ([]*Ledger, map[string][]map[string]any) {
	l := &Ledger{Path: "Decisions/decisions.md", Source: "", Meta: map[string]any{}}
	return []*Ledger{l}, map[string][]map[string]any{l.Path: entries}
}

func codes(ds []Diagnostic) map[string]int {
	out := map[string]int{}
	for _, d := range ds {
		out[d.Code]++
	}
	return out
}

// TestDLG066DanglingChain pins the while/else polarity that a first port got
// backwards. Python's `else` runs when the loop ends WITHOUT break and its
// body is `continue`, so the terminal check runs only after a break. A lone
// superseded entry with no replacement breaks immediately (its
// `superseded_by` is not a string) and must therefore be reported.
func TestDLG066DanglingChain(t *testing.T) {
	ledgers, entries := ledgerWith(entry(map[string]any{
		"id": "D-0001", "status": "superseded",
	}))
	got := codes(ValidateCollection(ledgers, entries))
	if got["DLG066"] != 1 {
		t.Errorf("a superseded entry with no replacement must report DLG066; got %v", got)
	}
}

// TestDLG066AcceptedTerminalIsQuiet is the other side: a chain ending at an
// accepted replacement also breaks, but its terminal is accepted, so nothing
// is reported.
func TestDLG066AcceptedTerminalIsQuiet(t *testing.T) {
	ledgers, entries := ledgerWith(
		entry(map[string]any{"id": "D-0001", "status": "superseded", "superseded_by": "D-0002"}),
		entry(map[string]any{"id": "D-0002", "status": "accepted", "supersedes": "D-0001"}),
	)
	got := codes(ValidateCollection(ledgers, entries))
	if got["DLG066"] != 0 {
		t.Errorf("a chain reaching an accepted replacement must not report DLG066; got %v", got)
	}
}

// TestDLG059CycleIsNotDLG066 covers the loop's other exit: a supersession
// cycle exhausts the walk rather than breaking, so it is DLG059's finding and
// must not also be reported as a dangling chain.
func TestDLG059CycleIsNotDLG066(t *testing.T) {
	ledgers, entries := ledgerWith(
		entry(map[string]any{"id": "D-0001", "status": "superseded", "superseded_by": "D-0002", "supersedes": "D-0002"}),
		entry(map[string]any{"id": "D-0002", "status": "superseded", "superseded_by": "D-0001", "supersedes": "D-0001"}),
	)
	got := codes(ValidateCollection(ledgers, entries))
	if got["DLG059"] == 0 {
		t.Errorf("a supersession cycle must report DLG059; got %v", got)
	}
	if got["DLG066"] != 0 {
		t.Errorf("a cycle exhausts the walk and must not report DLG066; got %v", got)
	}
}
