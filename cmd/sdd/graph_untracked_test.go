package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// TestPrintUntrackedCountsByDefault: a whole-package report can carry
// hundreds — sometimes thousands — of untracked ids (P-08) — the console
// line must stay a one-line count, with the full list gated behind
// --verbose (or --json, exercised at the Buckets/ReverifyResult level).
func TestPrintUntrackedCountsByDefault(t *testing.T) {
	var ids []string
	for i := 0; i < 400; i++ {
		ids = append(ids, "TestExample/case_"+strconv.Itoa(i))
	}

	var buf bytes.Buffer
	printUntracked(&buf, ids, false)
	out := buf.String()
	if out != "untracked: 400 test id(s) not declared by any node\n" {
		t.Fatalf("default output must be a one-line count, got: %q", out)
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
	if strings.Contains(out, "not declared by any node") {
		t.Fatalf("verbose output must not collapse to a count: %q", out)
	}
	if strings.Count(out, "TestExample/case_") != 400 {
		t.Fatalf("verbose output must carry every id, got %d occurrences", strings.Count(out, "TestExample/case_"))
	}
}

// TestPrintUntrackedShortListStillCounts: a short list still collapses to
// the one-line count by default — the count line, not the raw list, is
// what stays quiet; --verbose is the only way to see ids.
func TestPrintUntrackedShortListStillCounts(t *testing.T) {
	ids := []string{"a", "b", "c"}
	var buf bytes.Buffer
	printUntracked(&buf, ids, false)
	out := buf.String()
	if out != "untracked: 3 test id(s) not declared by any node\n" {
		t.Fatalf("short untracked list must still print the one-line count: %q", out)
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
