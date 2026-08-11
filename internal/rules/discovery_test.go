package rules

import (
	"path/filepath"
	"testing"
)

// TestPlanningRootMustBeADirectory covers SDD001, whose failing condition the
// example harness cannot construct: it always creates a real directory to hold
// an example's Files, so "the root does not exist" is unreachable there.
func TestPlanningRootMustBeADirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created")
	root := &Root{Dir: missing, ByPath: map[string]*Artifact{}}

	var got []Diagnostic
	Get("SDD001").CheckRoot(root, func(d Diagnostic) { got = append(got, d) })

	if len(got) != 1 {
		t.Fatalf("expected exactly one SDD001, got %d", len(got))
	}
	if got[0].Code != "SDD001" {
		t.Errorf("code = %q, want SDD001", got[0].Code)
	}
	// Python reports the root path itself, there being no artifact to blame.
	if got[0].Path != missing {
		t.Errorf("path = %q, want the root %q", got[0].Path, missing)
	}
	if got[0].Message != "Planning root is not a directory." {
		t.Errorf("message = %q", got[0].Message)
	}
}

// TestPlanningRootThatExistsIsQuiet is the Good half.
func TestPlanningRootThatExistsIsQuiet(t *testing.T) {
	root := &Root{Dir: t.TempDir(), ByPath: map[string]*Artifact{}}
	var got []Diagnostic
	Get("SDD001").CheckRoot(root, func(d Diagnostic) { got = append(got, d) })
	if len(got) != 0 {
		t.Errorf("expected no diagnostic for an existing root, got %v", got)
	}
}
