package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// Artifacts of a retired type (retro, diagram) are ignored: never validated —
// no SDD011 "Unknown type", no field or status checks — while remaining
// discoverable so legacy `related:` references still resolve.
func TestRetiredTypesAreIgnoredNotValidated(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		// Deliberately incomplete frontmatter: were these validated, SDD010
		// (missing fields) would fire alongside SDD011 (unknown type).
		"Retro/2025-01-01-old-retro.md": "---\ntitle: Old Retro\ntype: retro\n---\n\n# Old Retro\n",
		"Diagrams/system-overview.md":   "---\ntitle: Overview\ntype: diagram\n---\n\n# Overview\n",
		"Research/topic.md": `---
title: Sample Research
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: [Retro/2025-01-01-old-retro.md, Diagrams/system-overview.md]
---

# Sample Research

## Summary
Body.

## Findings
Body.

## Sources
- none
`,
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	root, err := LoadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, a := range root.Artifacts {
		if retiredTypes[a.Kind()] {
			t.Errorf("retired-type artifact %s was loaded for validation", a.Rel)
		}
	}
	for _, rel := range []string{"Retro/2025-01-01-old-retro.md", "Diagrams/system-overview.md"} {
		if _, ok := root.ByPath[rel]; !ok {
			t.Errorf("retired-type artifact %s is not resolvable as a reference target", rel)
		}
	}
	for _, d := range Run(root) {
		if d.Path == "Retro/2025-01-01-old-retro.md" || d.Path == "Diagrams/system-overview.md" {
			t.Errorf("diagnostic on an ignored retired-type artifact: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}
}
