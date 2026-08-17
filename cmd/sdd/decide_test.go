package main

import (
	"os"
	"strings"
	"testing"
)

func ledgerDoc(entriesYAML string) string {
	return `---
title: "Decision Ledger"
type: decision-log
status: active
created: 2026-07-01
updated: 2026-07-01
tags: [decisions]
related: []
decisions:
` + entriesYAML + `
---

# Decision Ledger

Machine-readable record of decided truths.
`
}

func writeLedger(t *testing.T, root, entriesYAML string) string {
	t.Helper()
	return writeArtifact(t, root, "Decisions", "decisions.md", ledgerDoc(entriesYAML))
}

// TestDecideAdd_AllocatesAboveHighWaterMark: the next id must be one past the
// highest existing number, never filling a gap left by a prior retirement.
func TestDecideAdd_AllocatesAboveHighWaterMark(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, `  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "First decision."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way
  - id: D-0005
    kind: decision
    status: accepted
    date: 2026-07-02
    decided_by: user
    statement: "Fifth decision, after some were retired."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`)

	if err := cmdDecideAdd(decideAddOpts{Statement: "A brand new unrelated fact about widgets.", Accept: true}); err != nil {
		t.Fatalf("decide add: %v", err)
	}

	path, err := ledgerPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "id: D-0006") {
		t.Errorf("expected the new entry to be D-0006 (above the high-water mark), got:\n%s", b)
	}
	if strings.Contains(string(b), "id: D-0002") {
		t.Errorf("must never fill a gap left by retirement")
	}
}

// TestDecideAdd_PreservesPriorEntriesByteForByte: appending must not alter
// any existing entry's bytes, only insert the new one and restamp `updated`.
func TestDecideAdd_PreservesPriorEntriesByteForByte(t *testing.T) {
	root := chdirTemp(t)
	entry := `  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "First decision."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`
	writeLedger(t, root, entry)

	if err := cmdDecideAdd(decideAddOpts{Statement: "A brand new unrelated fact about widgets.", Accept: true}); err != nil {
		t.Fatalf("decide add: %v", err)
	}

	path, _ := ledgerPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(entry, "\n") {
		if !strings.Contains(string(b), line) {
			t.Errorf("original entry line lost: %q", line)
		}
	}
}

// TestDecideAdd_AdvancesUpdated: the ledger's own `updated` must move to
// today whenever a new decision is appended.
func TestDecideAdd_AdvancesUpdated(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, `  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "First decision."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`)

	if err := cmdDecideAdd(decideAddOpts{Statement: "A brand new unrelated fact.", Accept: true}); err != nil {
		t.Fatalf("decide add: %v", err)
	}

	path, _ := ledgerPath()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "updated: 2026-07-01") {
		t.Errorf("updated was not advanced:\n%s", b)
	}
}

// A fresh ledger declares `decisions: []` — the list's empty form, which is
// what the template and schema emit. The splice logic recognized only a bare
// `decisions:` block header, so the first add appended a SECOND `decisions:`
// key and the duplicate made the ledger unparseable YAML from then on.
func TestDecideAdd_EmptyListLedgerAcceptsFirstEntry(t *testing.T) {
	root := chdirTemp(t)
	writeArtifact(t, root, "Decisions", "decisions.md", `---
title: "Decision Ledger"
type: decision-log
status: active
created: 2026-07-01
updated: 2026-07-01
tags: [decisions]
related: []
decisions: []
---

# Decision Ledger

Machine-readable record of decided truths.
`)

	if err := cmdDecideAdd(decideAddOpts{
		Statement: "Widget storage uses the alpha approach for durability",
		Rationale: "Because.", Accept: true,
	}); err != nil {
		t.Fatalf("first add on an empty-list ledger: %v", err)
	}

	path, _ := ledgerPath()
	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "\ndecisions:"); n != 1 {
		t.Fatalf("ledger must carry exactly one `decisions:` key, found %d:\n%s", n, b)
	}
	if !strings.Contains(string(b), "id: D-0001") {
		t.Fatalf("the entry was not written:\n%s", b)
	}
}

// Supersession is one-step: superseding an already-superseded entry forked the
// chain and stamped a second `superseded_by` onto the target — duplicate-key
// YAML that made every later add fail.
func TestDecideAdd_RefusesSupersedingASupersededEntry(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, `  - id: D-0001
    kind: decision
    status: superseded
    date: 2026-07-01
    decided_by: user
    statement: "Widget storage uses the alpha approach for durability."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way
    superseded_by: D-0002
  - id: D-0002
    kind: decision
    status: accepted
    date: 2026-07-02
    decided_by: user
    statement: "Widget storage uses the beta approach for durability instead."
    rejected: []
    rationale: "Better."
    scope: []
    tags: []
    reversibility: two-way
    supersedes: D-0001`)

	err := cmdDecideAdd(decideAddOpts{
		Statement:  "Widget storage moves to the gamma approach for durability outright",
		Rationale:  "Newer.",
		Supersedes: "D-0001",
		Accept:     true,
	})
	if err == nil {
		t.Fatal("superseding an already-superseded entry must be refused")
	}
	if !strings.Contains(err.Error(), "D-0002") {
		t.Errorf("the refusal must name the live successor; got %v", err)
	}

	path, _ := ledgerPath()
	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "superseded_by:"); n != 1 {
		t.Fatalf("a refused add must not stamp a second superseded_by (found %d):\n%s", n, b)
	}
	if strings.Contains(string(b), "D-0003") {
		t.Errorf("a refused add must not have written anything: %s", b)
	}
}

// TestDecideAdd_CollisionRefusesWithoutSupersedes: a statement that
// substantially overlaps an accepted entry's statement must refuse, exit
// non-nil, and never silently proceed (D-0003).
func TestDecideAdd_CollisionRefusesWithoutSupersedes(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, `  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "The plugin ships a SessionStart hook that injects accepted ledger entries as additional context at session start."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`)

	err := cmdDecideAdd(decideAddOpts{Statement: "SessionStart ledger injection moves into the Go binary as a hook, injecting accepted ledger entries as additional context at session start."})
	if err == nil {
		t.Fatal("expected a refusal for the colliding statement")
	}
	if _, ok := err.(*refusedError); !ok {
		t.Errorf("expected *refusedError, got %v (%T)", err, err)
	}

	path, _ := ledgerPath()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "D-0002") {
		t.Errorf("a refused add must not have written anything: %s", b)
	}
}

// TestDecideAdd_SupersedesMarksOldEntry: --supersedes must set the new
// entry's supersedes and flip the named entry to status: superseded plus
// superseded_by — the only mutation an accepted entry permits.
func TestDecideAdd_SupersedesMarksOldEntry(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, `  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "The old approach for widget assembly."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`)

	if err := cmdDecideAdd(decideAddOpts{Statement: "The new approach for widget assembly.", Supersedes: "D-0001", Accept: true}); err != nil {
		t.Fatalf("decide add: %v", err)
	}

	path, _ := ledgerPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "status: superseded") {
		t.Errorf("old entry was not marked superseded:\n%s", content)
	}
	if !strings.Contains(content, "superseded_by: D-0002") {
		t.Errorf("old entry missing superseded_by: D-0002:\n%s", content)
	}
	if !strings.Contains(content, "supersedes: D-0001") {
		t.Errorf("new entry missing supersedes: D-0001:\n%s", content)
	}
}

// TestDecideAdd_DefaultsToProposed: an entry the tool writes must not
// silently become accepted truth unless --accept was passed.
func TestDecideAdd_DefaultsToProposed(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, `  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "First decision."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`)

	if err := cmdDecideAdd(decideAddOpts{Statement: "A brand new unrelated fact about widgets."}); err != nil {
		t.Fatalf("decide add: %v", err)
	}

	path, _ := ledgerPath()
	b, _ := os.ReadFile(path)
	lines := strings.Split(string(b), "\n")
	found := false
	for i, l := range lines {
		if strings.TrimSpace(l) == "- id: D-0002" {
			for j := i; j < len(lines) && j < i+5; j++ {
				if strings.TrimSpace(lines[j]) == "status: proposed" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("new entry did not default to status: proposed:\n%s", b)
	}
}

// TestDecideAdd_SupersedesIgnoresFieldOrder: an author may write an entry's
// keys in any order — YAML gives `id` no positional significance. An earlier
// implementation located the entry by matching the literal text "- id: <id>"
// on the dash line, so a ledger whose entries began with `kind:` silently
// failed to mark the superseding target: the tool reported success, the old
// entry stayed `accepted`, and the ledger was left holding two contradictory
// accepted entries.
func TestDecideAdd_SupersedesIgnoresFieldOrder(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, `  - kind: decision
    id: D-0001
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "The old approach, written kind-first."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`)

	if err := cmdDecideAdd(decideAddOpts{Statement: "The new approach.", Supersedes: "D-0001", Accept: true}); err != nil {
		t.Fatalf("decide add: %v", err)
	}

	path, _ := ledgerPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "status: superseded") {
		t.Errorf("kind-first entry was not marked superseded:\n%s", content)
	}
	if !strings.Contains(content, "superseded_by: D-0002") {
		t.Errorf("kind-first entry missing superseded_by: D-0002:\n%s", content)
	}
	if strings.Contains(content, "status: accepted\n    date: 2026-07-01") {
		t.Errorf("old entry is still accepted — the ledger now holds two contradictory truths:\n%s", content)
	}
}
