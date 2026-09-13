// TestGraphShow_IsolationStaleNamesCause: `graph show`'s ISOLATION-STALE
// line names the untracked/modified paths behind a shared-dirty
// observation, not just that isolation was dirty.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func isolationShowFixture(t *testing.T, dirtyPaths []string) string {
	t.Helper()
	root := chdirTemp(t)
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeArtifact(t, root, "Plans/Demo", "README.md", `---
title: "Demo"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: [x]
related: []
phases: []
---

# Demo
`)
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = []model.Node{{
			ID: "a", Contract: "does the thing", Justifies: []string{"D-0001"},
			Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_a", File: "a_test.go"}}},
			Hazards: model.Hazards{}, Estimate: 1,
			Verification: &model.Verification{
				Result: model.ResultPass, Seq: 1,
				Isolation:           model.IsolationSharedDirty,
				IsolationDirtyPaths: dirtyPaths,
			},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGraphShow_IsolationStaleNamesCause(t *testing.T) {
	isolationShowFixture(t, []string{"stray.txt", "other.txt"})

	c := graphShowCmd()
	c.SetArgs([]string{"a", "--plan", "Demo"})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ISOLATION-STALE") {
		t.Fatalf("expected an ISOLATION-STALE line, got: %q", out)
	}
	if !strings.Contains(out, "stray.txt") || !strings.Contains(out, "other.txt") {
		t.Fatalf("the ISOLATION-STALE line must name the dirty paths, got: %q", out)
	}
}

func TestGraphShow_IsolationStaleTruncatesPastFive(t *testing.T) {
	isolationShowFixture(t, []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt", "f.txt", "g.txt"})

	c := graphShowCmd()
	c.SetArgs([]string{"a", "--plan", "Demo"})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "… (2 more)") {
		t.Fatalf("more than five dirty paths must truncate to a count, got: %q", out)
	}
	if strings.Contains(out, "g.txt") {
		t.Fatalf("the sixth+ path must not print, got: %q", out)
	}
}
