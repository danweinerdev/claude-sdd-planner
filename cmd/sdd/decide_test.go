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

	if err := cmdDecide([]string{"add", "--statement", "A brand new unrelated fact about widgets.", "--accept"}); err != nil {
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

	if err := cmdDecide([]string{"add", "--statement", "A brand new unrelated fact about widgets.", "--accept"}); err != nil {
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

	if err := cmdDecide([]string{"add", "--statement", "A brand new unrelated fact.", "--accept"}); err != nil {
		t.Fatalf("decide add: %v", err)
	}

	path, _ := ledgerPath()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "updated: 2026-07-01") {
		t.Errorf("updated was not advanced:\n%s", b)
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

	err := cmdDecide([]string{"add", "--statement",
		"SessionStart ledger injection moves into the Go binary as a hook, injecting accepted ledger entries as additional context at session start."})
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

	if err := cmdDecide([]string{"add", "--statement", "The new approach for widget assembly.",
		"--supersedes", "D-0001", "--accept"}); err != nil {
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

	if err := cmdDecide([]string{"add", "--statement", "A brand new unrelated fact about widgets."}); err != nil {
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
