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

func collisionEntry(id, status, subject string) string {
	return `  - id: ` + id + `
    kind: decision
    status: ` + status + `
    date: 2026-07-01
    decided_by: user
    statement: "` + subject + ` storage uses the alpha approach for durable replicated persistence."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`
}

func ledgerBytes(t *testing.T) []byte {
	t.Helper()
	path, err := ledgerPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecideAddCLI_CompatibleWithParsesAndPreservesTarget(t *testing.T) {
	root := chdirTemp(t)
	target := collisionEntry("D-0001", "accepted", "Widget")
	writeLedger(t, root, target)
	c := newRootCmd()
	c.SetArgs([]string{"decide", "add", "--statement", "Widget storage uses the beta approach for durable replicated persistence.", "--compatible-with", "D-0001", "--compatible-with", "D-0001", "--accept"})
	if err := c.Execute(); err != nil {
		t.Fatalf("CLI decide add: %v", err)
	}
	got := string(ledgerBytes(t))
	if !strings.Contains(got, target) {
		t.Fatalf("compatible target bytes changed:\n%s", got)
	}
	if strings.Contains(got, "compatible_with") || strings.Contains(got, "compatible-with") {
		t.Fatalf("compatibility acknowledgement leaked into ledger metadata:\n%s", got)
	}
	if !strings.Contains(got, "id: D-0002") {
		t.Fatalf("new entry missing:\n%s", got)
	}
}

func TestDecideAddCLI_ExactScalarRoundTrip(t *testing.T) {
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
`)
	statement := "First approved paragraph has a comma, a \\n literal, quoted \"text\", and Unicode café 東京.\nSecond approved paragraph remains distinct."
	rationale := "Why \\paths stay literal.\nA second rationale paragraph with \"quotes\"."
	c := newRootCmd()
	c.SetArgs([]string{"decide", "add",
		"--statement", statement,
		"--rationale", rationale,
		"--rejected", "Reject alpha,Reject beta",
		"--scope", "Specs/Exact,Designs/Exact",
		"--tags", "roundtrip,unicode",
		"--accept"})
	if err := c.Execute(); err != nil {
		t.Fatalf("CLI decide add: %v", err)
	}
	doc, _, _, err := loadLedger()
	if err != nil {
		t.Fatal(err)
	}
	entries := loadEntries(doc)
	if len(entries) != 1 {
		t.Fatalf("got %d parsed entries", len(entries))
	}
	got := entries[0]
	if got.Statement != statement {
		t.Errorf("statement round trip mismatch:\nwant: %q\n got: %q", statement, got.Statement)
	}
	if got.Rationale != rationale {
		t.Errorf("rationale round trip mismatch:\nwant: %q\n got: %q", rationale, got.Rationale)
	}
	if strings.Join(got.Rejected, "|") != "Reject alpha|Reject beta" {
		t.Errorf("rejected round trip mismatch: %#v", got.Rejected)
	}
	if strings.Join(got.Scope, "|") != "Specs/Exact|Designs/Exact" {
		t.Errorf("scope round trip mismatch: %#v", got.Scope)
	}
	if strings.Join(got.Tags, "|") != "roundtrip|unicode" {
		t.Errorf("tags round trip mismatch: %#v", got.Tags)
	}
}

func TestDecideAddCLI_RejectedValuePreservesLiteralComma(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	want := []string{
		"Reject every second edit instead of accumulating compatible edits.",
		"Silently replace an edit with retirement, or retirement with an edit.",
	}
	c := newRootCmd()
	c.SetArgs([]string{"decide", "add",
		"--statement", "A separate approved statement about staged operation conflict handling.",
		"--rejected-value", want[0],
		"--rejected-value", want[1],
		"--accept"})
	if err := c.Execute(); err != nil {
		t.Fatalf("CLI decide add: %v", err)
	}
	doc, _, _, err := loadLedger()
	if err != nil {
		t.Fatal(err)
	}
	entries := loadEntries(doc)
	got := entries[len(entries)-1].Rejected
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("literal rejected values changed:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestDecideAddCLI_RejectedValuePreservesYAMLLikeStrings(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	want := []string{"null", "TRUE", "123", "", "2026-09-09", "literal, comma"}
	args := []string{"decide", "add", "--statement", "A separate approved truth about exact scalar values.", "--accept"}
	for _, value := range want {
		args = append(args, "--rejected-value", value)
	}
	c := newRootCmd()
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatalf("CLI decide add: %v", err)
	}
	doc, _, _, err := loadLedger()
	if err != nil {
		t.Fatal(err)
	}
	entries := loadEntries(doc)
	got := entries[len(entries)-1].Rejected
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("YAML-like rejected values changed:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestDecideAddCLI_RejectsMixedRejectedFlagsWithoutWrite(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	before := string(ledgerBytes(t))
	c := newRootCmd()
	c.SetArgs([]string{"decide", "add", "--statement", "Unrelated approved statement.",
		"--rejected", "old one,old two", "--rejected-value", "literal, comma", "--accept"})
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually-exclusive refusal, got %v", err)
	}
	if string(ledgerBytes(t)) != before {
		t.Fatal("mixed rejected flags changed ledger bytes")
	}
}

func TestDecideAdd_CompatibleWithInvalidTargetsDoNotWrite(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"unknown", "D-9999"}, {"proposed", "D-0002"}, {"superseded", "D-0003"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := chdirTemp(t)
			entries := collisionEntry("D-0001", "accepted", "Widget") + "\n" +
				collisionEntry("D-0002", "proposed", "Proposed") + "\n" +
				collisionEntry("D-0003", "superseded", "Retired")
			writeLedger(t, root, entries)
			before := string(ledgerBytes(t))
			err := cmdDecideAdd(decideAddOpts{Statement: "Widget storage uses the beta approach for durable replicated persistence.", CompatibleWith: []string{tc.id}, Accept: true})
			if err == nil {
				t.Fatal("expected invalid compatibility target refusal")
			}
			if got := string(ledgerBytes(t)); got != before {
				t.Fatal("refusal changed ledger bytes")
			}
		})
	}
}

func TestDecideAdd_CompatibleWithMustNameActualCandidate(t *testing.T) {
	root := chdirTemp(t)
	unrelated := `  - id: D-0002
    kind: decision
    status: accepted
    date: 2026-07-01
    decided_by: user
    statement: "Telescopes track distant galaxies through calibrated optical mirrors."
    rejected: []
    rationale: "Because."
    scope: []
    tags: []
    reversibility: two-way`
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget")+"\n"+unrelated)
	before := string(ledgerBytes(t))
	err := cmdDecideAdd(decideAddOpts{Statement: "Widget storage uses the beta approach for durable replicated persistence.", CompatibleWith: []string{"D-0002"}, Accept: true})
	if err == nil || !strings.Contains(err.Error(), "actual collision candidate") {
		t.Fatalf("expected stale acknowledgement refusal, got %v", err)
	}
	if string(ledgerBytes(t)) != before {
		t.Fatal("refusal changed ledger bytes")
	}
}

func TestDecideAdd_ResolvedAndUnresolvedCandidatesRefuse(t *testing.T) {
	for _, compatible := range [][]string{nil, {"D-0002"}} {
		root := chdirTemp(t)
		writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget")+"\n"+collisionEntry("D-0002", "accepted", "Widget")+"\n"+collisionEntry("D-0003", "accepted", "Widget"))
		before := string(ledgerBytes(t))
		err := cmdDecideAdd(decideAddOpts{Statement: "Widget storage uses the beta approach for durable replicated persistence.", Supersedes: "D-0001", CompatibleWith: compatible, Accept: true})
		if err == nil {
			t.Fatal("named plus unnamed collision must refuse")
		}
		if string(ledgerBytes(t)) != before {
			t.Fatal("refusal changed ledger bytes")
		}
	}
}

func TestDecideAdd_MixedDistinctResolutions(t *testing.T) {
	root := chdirTemp(t)
	two := collisionEntry("D-0002", "accepted", "Widget")
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget")+"\n"+two)
	if err := cmdDecideAdd(decideAddOpts{Statement: "Widget storage uses the beta approach for durable replicated persistence.", Supersedes: "D-0001", CompatibleWith: []string{"D-0002"}, Accept: true}); err != nil {
		t.Fatalf("mixed resolutions: %v", err)
	}
	got := string(ledgerBytes(t))
	if !strings.Contains(got, two) {
		t.Fatal("compatible target was mutated")
	}
	if !strings.Contains(got, "superseded_by: D-0003") {
		t.Fatal("superseded target was not linked")
	}
}

func TestDecideAdd_SameIDResolutionConflictRefuses(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	before := string(ledgerBytes(t))
	err := cmdDecideAdd(decideAddOpts{Statement: "Widget storage uses the beta approach for durable replicated persistence.", Supersedes: "D-0001", CompatibleWith: []string{"D-0001"}, Accept: true})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("expected same-id conflict, got %v", err)
	}
	if string(ledgerBytes(t)) != before {
		t.Fatal("refusal changed ledger bytes")
	}
}

func TestDecideAdd_CompatibleDryRunDoesNotWrite(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	before := string(ledgerBytes(t))
	if err := cmdDecideAdd(decideAddOpts{Statement: "Widget storage uses the beta approach for durable replicated persistence.", CompatibleWith: []string{"D-0001"}, Accept: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if string(ledgerBytes(t)) != before {
		t.Fatal("dry run changed ledger bytes")
	}
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

// TestQuoteYAML_RoundTripsEscapes: quoteYAML must produce a scalar the ledger
// parser reads back byte-for-byte. The old implementation escaped only the
// double quote, so a backslash or a raw newline in --statement produced a
// scalar that parsed to different bytes — or not at all. Every case here goes
// through renderEntry → ledger file → loadEntries, not a string compare.
func TestQuoteYAML_RoundTripsEscapes(t *testing.T) {
	cases := map[string]string{
		"backslash":       `C:\Users\path and a trailing \`,
		"literal-n":       `not a newline: \n stays two characters`,
		"raw-newline":     "first paragraph\n\nsecond paragraph",
		"tab-and-cr":      "col1\tcol2\r\nrow2",
		"control-char":    "bell\x07 and unit sep\x1f",
		"del":             "before\x7fafter",
		"quotes":          `she said "hi" and 'bye'`,
		"unicode":         "café 東京 — emoji 🎯",
		"yaml-indicators": "- not a list: {not: a map} # not a comment",
		"empty":           "",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			root := chdirTemp(t)
			lines := renderEntry(decisionEntry{
				ID: "D-0001", Kind: "decision", Status: "accepted", Date: "2026-07-01",
				DecidedBy: "user", Statement: want, Rationale: want, Reversibility: "two-way",
			})
			for _, l := range lines {
				if strings.Contains(l, "\n") {
					t.Fatalf("renderEntry emitted a multi-line YAML line: %q", l)
				}
			}
			writeLedger(t, root, strings.Join(lines, "\n"))
			doc, _, _, err := loadLedger()
			if err != nil {
				t.Fatal(err)
			}
			entries := loadEntries(doc)
			if len(entries) != 1 {
				t.Fatalf("got %d parsed entries from:\n%s", len(entries), strings.Join(lines, "\n"))
			}
			if entries[0].Statement != want {
				t.Errorf("statement:\nwant %q\n got %q", want, entries[0].Statement)
			}
			if entries[0].Rationale != want {
				t.Errorf("rationale:\nwant %q\n got %q", want, entries[0].Rationale)
			}
		})
	}
}

// TestDecideAdd_LegacyRejectedRendersPlainScalars: the comma-separated
// --rejected path keeps its plain flow rendering; only --rejected-value opts
// into quoting every element. Both must parse back to the same values.
func TestDecideAdd_LegacyRejectedRendersPlainScalars(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	if err := cmdDecideAdd(decideAddOpts{Statement: "Unrelated approved statement about rendering.", Rejected: "alpha,beta gamma", Accept: true}); err != nil {
		t.Fatal(err)
	}
	if got := string(ledgerBytes(t)); !strings.Contains(got, "    rejected: [alpha, beta gamma]\n") {
		t.Fatalf("legacy --rejected rendering changed:\n%s", got)
	}

	root = chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	if err := cmdDecideAdd(decideAddOpts{Statement: "Unrelated approved statement about rendering.", RejectedValues: []string{"alpha", "beta gamma"}, Accept: true}); err != nil {
		t.Fatal(err)
	}
	if got := string(ledgerBytes(t)); !strings.Contains(got, `    rejected: ["alpha", "beta gamma"]`+"\n") {
		t.Fatalf("--rejected-value rendering not quoted:\n%s", got)
	}
	doc, _, _, err := loadLedger()
	if err != nil {
		t.Fatal(err)
	}
	entries := loadEntries(doc)
	if got := strings.Join(entries[len(entries)-1].Rejected, "|"); got != "alpha|beta gamma" {
		t.Fatalf("quoted rejected values parsed as %q", got)
	}
}

// TestDecideAdd_CompatibleWithBlankIDRefuses: a blank acknowledgement (e.g.
// `--compatible-with ""` or a trailing comma) is an input error, never a
// silently-normalized no-op, and never writes.
func TestDecideAdd_CompatibleWithBlankIDRefuses(t *testing.T) {
	for _, ids := range [][]string{{""}, {"  "}, {"D-0001", ""}} {
		root := chdirTemp(t)
		writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
		before := string(ledgerBytes(t))
		err := cmdDecideAdd(decideAddOpts{Statement: "Widget storage uses the beta approach for durable replicated persistence.", CompatibleWith: ids, Accept: true})
		if err == nil || !strings.Contains(err.Error(), "requires a decision id") {
			t.Fatalf("ids %q: expected blank-id refusal, got %v", ids, err)
		}
		if string(ledgerBytes(t)) != before {
			t.Fatalf("ids %q: refusal changed ledger bytes", ids)
		}
	}
}

// TestDecideAdd_CompatibleWithNoCandidatesIsNotAFreePass: naming an accepted
// entry that the collision check did not flag is refused even when nothing
// else collides — an acknowledgement must correspond to a detected candidate
// so a stale flag copied from an earlier invocation cannot linger unnoticed.
func TestDecideAdd_CompatibleWithNoCandidatesIsNotAFreePass(t *testing.T) {
	root := chdirTemp(t)
	writeLedger(t, root, collisionEntry("D-0001", "accepted", "Widget"))
	before := string(ledgerBytes(t))
	err := cmdDecideAdd(decideAddOpts{Statement: "Telescopes track distant galaxies through calibrated optical mirrors.", CompatibleWith: []string{"D-0001"}, Accept: true})
	if err == nil || !strings.Contains(err.Error(), "actual collision candidate") {
		t.Fatalf("expected non-candidate refusal, got %v", err)
	}
	if string(ledgerBytes(t)) != before {
		t.Fatal("refusal changed ledger bytes")
	}
}
