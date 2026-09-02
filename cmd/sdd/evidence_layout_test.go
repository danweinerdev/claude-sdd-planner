package main

// Layout fixes for the evidence writer (SddGraph phase-5 debrief, skill
// opportunity #1): the writer lives in the same binary as the rules it must
// satisfy, so emitting a layout SDD157/SDD158/SDD166 then reject is a
// defect, not an authoring problem. Three gaps, each hit repeatedly during
// the SddGraph plan's own closes:
//
//  1. The Tool/inspection table rendered AFTER the identities section,
//     which SDD157/SDD158 require to contain only identity entries.
//  2. The final-review line needs `; frozen: <range>` (SDD166), but the
//     flag accepted a bare path and wrote it verbatim.
//  3. The review path must be planning-root-relative; a CWD-relative
//     spelling (`.plans/Plans/...`) was written verbatim and refused.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceToolTableRendersBeforeIdentities(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   evidenceInput
		ids  string
	}{
		{"phase", evidenceInput{
			Date: "2026-09-01", Repository: ".", VCS: "git",
			Revision: strings.Repeat("a", 40), VerifiedBy: "make test",
			WorkingDir: ".", Result: "green", Tool: "inspection",
			ToolContext: "ctx", ToolResult: "seen",
			TaskIdentities: []taskIdentityLine{{ID: "1.1", Revision: strings.Repeat("b", 40)}},
		}, "### Completed task identities"},
		{"plan", evidenceInput{
			Date: "2026-09-01", Repository: ".", VCS: "git",
			Revision: strings.Repeat("a", 40), VerifiedBy: "make test",
			WorkingDir: ".", Result: "green", Tool: "inspection",
			ToolContext: "ctx", ToolResult: "seen",
			PhaseIdentities: []phaseIdentityLine{{ID: "1", Revision: strings.Repeat("b", 40), Review: "Plans/P/reviews/01-r.md"}},
		}, "### Completed phase identities"},
	} {
		out := renderEvidence(tc.in)
		tool := strings.Index(out, "| Tool / inspection |")
		ids := strings.Index(out, tc.ids)
		if tool < 0 || ids < 0 {
			t.Fatalf("%s: both blocks must render:\n%s", tc.name, out)
		}
		if tool > ids {
			t.Errorf("%s: the Tool/inspection table must render BEFORE the identities section (SDD157/SDD158 demand identities-only content there):\n%s", tc.name, out)
		}
	}
}

func TestComposeFinalReviewLine(t *testing.T) {
	root := t.TempDir()
	rel := "Plans/P/reviews/08-r.md"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("review"), 0o644); err != nil {
		t.Fatal(err)
	}
	frozen := strings.Repeat("a", 40) + ".." + strings.Repeat("b", 40)

	// Planning-root-relative input passes through with the suffix appended.
	got, err := composeFinalReview(root, rel, frozen)
	if err != nil {
		t.Fatalf("root-relative: %v", err)
	}
	want := rel + "; frozen: " + frozen
	if got != want {
		t.Errorf("root-relative: got %q, want %q", got, want)
	}

	// An absolute spelling normalizes to planning-root-relative.
	if got, err = composeFinalReview(root, abs, frozen); err != nil || got != want {
		t.Errorf("absolute: got %q (%v), want %q", got, err, got)
	}

	// A caller who already composed the suffix is not double-suffixed.
	pre := rel + "; frozen: " + frozen
	if got, err = composeFinalReview(root, pre, ""); err != nil || got != pre {
		t.Errorf("pre-composed: got %q (%v), want %q", got, err, pre)
	}

	// A bare path with no range anywhere refuses, naming the requirement —
	// writing a line SDD166 will reject is worse than refusing now.
	if _, err = composeFinalReview(root, rel, ""); err == nil ||
		!strings.Contains(err.Error(), "frozen") {
		t.Errorf("bare path must refuse naming the frozen requirement: %v", err)
	}

	// A path that resolves nowhere refuses rather than recording a dangling
	// reference.
	if _, err = composeFinalReview(root, "Plans/P/reviews/missing.md", frozen); err == nil {
		t.Error("a nonexistent review artifact must refuse")
	}
}
