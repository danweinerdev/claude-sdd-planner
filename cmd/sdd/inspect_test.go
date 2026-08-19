package main

import (
	"os"
	"path/filepath"
	"testing"
)

// B-3: `show --json` must surface structured-list frontmatter (a review's
// findings[]/followups[]) as real JSON structures, not "" — the flat
// line-scan model deliberately leaves block-mapping sequences to the YAML
// node tree, so show has to read the tree.
func TestShowArtifactRendersStructuredFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "02-sample-code-review.md")
	src := `---
title: "Sample Code Review"
type: review
status: open
created: 2026-08-01
updated: 2026-08-02
review_of: Plans/Sample/01-One.md
findings:
  - id: F-01
    severity: major
    status: open
followups:
  - id: FU-01
    tracked_in: ""
---

# Sample Code Review

## Findings
- F-01
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := showArtifact(path, "spec")
	if err != nil {
		t.Fatalf("showArtifact: %v", err)
	}
	findings, ok := out.Frontmatter["findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("findings = %#v, want a one-element []any", out.Frontmatter["findings"])
	}
	f0, ok := findings[0].(map[string]any)
	if !ok || f0["id"] != "F-01" || f0["severity"] != "major" {
		t.Errorf("findings[0] = %#v, want id F-01, severity major", findings[0])
	}
	followups, ok := out.Frontmatter["followups"].([]any)
	if !ok || len(followups) != 1 {
		t.Fatalf("followups = %#v, want a one-element []any", out.Frontmatter["followups"])
	}
	// Scalar discipline: dates keep their source spelling, quoted strings
	// lose their quotes, and the artifact's own type wins over the flag.
	if out.Frontmatter["created"] != "2026-08-01" {
		t.Errorf("created = %#v, want the source date spelling", out.Frontmatter["created"])
	}
	if out.Frontmatter["title"] != "Sample Code Review" {
		t.Errorf("title = %#v, want unquoted string", out.Frontmatter["title"])
	}
	if out.Type != "review" {
		t.Errorf("type = %q, want review (frontmatter wins over the flag)", out.Type)
	}
}

// Malformed frontmatter must still show what is in the file (flat fallback),
// not fail: show is the command someone runs to find the problem.
func TestShowArtifactFallsBackOnMalformedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.md")
	src := "---\ntitle: \"Broken\ntype: spec\nstatus: draft\n---\n\n# Broken\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := showArtifact(path, "spec")
	if err != nil {
		t.Fatalf("showArtifact: %v", err)
	}
	if len(out.Frontmatter) == 0 {
		t.Error("malformed frontmatter should fall back to the flat entries, not vanish")
	}
}
