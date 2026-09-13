package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// TestPrintUntrackedTruncatesByDefault: a whole-package report can carry
// hundreds of untracked ids (P-08) — the console line must stay a count
// plus the first five ids, with the full list gated behind --verbose (or
// --json, exercised at the Buckets/ReverifyResult level).
func TestPrintUntrackedTruncatesByDefault(t *testing.T) {
	var ids []string
	for i := 0; i < 400; i++ {
		ids = append(ids, "TestExample/case_"+strconv.Itoa(i))
	}

	var buf bytes.Buffer
	printUntracked(&buf, ids, false)
	out := buf.String()
	if !strings.HasPrefix(out, "untracked: TestExample/case_0, ") {
		t.Fatalf("truncated output must lead with the first ids: %q", out)
	}
	if !strings.Contains(out, "… (395 more)") {
		t.Fatalf("truncated output must name the remaining count: %q", out)
	}
	if strings.Count(out, "TestExample/case_") != 5 {
		t.Fatalf("truncated output must carry exactly 5 ids, got: %q", out)
	}
}

// TestPrintUntrackedVerboseCarriesFullList: --verbose prints every id.
func TestPrintUntrackedVerboseCarriesFullList(t *testing.T) {
	var ids []string
	for i := 0; i < 400; i++ {
		ids = append(ids, "TestExample/case_"+strconv.Itoa(i))
	}

	var buf bytes.Buffer
	printUntracked(&buf, ids, true)
	out := buf.String()
	if strings.Contains(out, "more)") {
		t.Fatalf("verbose output must not truncate: %q", out)
	}
	if strings.Count(out, "TestExample/case_") != 400 {
		t.Fatalf("verbose output must carry every id, got %d occurrences", strings.Count(out, "TestExample/case_"))
	}
}

// TestPrintUntrackedShortListNeverTruncates: five or fewer ids print in
// full even without --verbose — truncation only kicks in past the point it
// would actually save space.
func TestPrintUntrackedShortListNeverTruncates(t *testing.T) {
	ids := []string{"a", "b", "c"}
	var buf bytes.Buffer
	printUntracked(&buf, ids, false)
	out := buf.String()
	if out != "untracked: a, b, c\n" {
		t.Fatalf("short untracked list must print in full: %q", out)
	}
}

// TestPrintUntrackedEmptyPrintsNothing.
func TestPrintUntrackedEmptyPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	printUntracked(&buf, nil, false)
	if buf.Len() != 0 {
		t.Fatalf("an empty untracked bucket must print nothing: %q", buf.String())
	}
}
