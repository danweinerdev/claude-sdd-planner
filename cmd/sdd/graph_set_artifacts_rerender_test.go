// TestGraphSetArtifacts_RerendersViews: after a successful set-artifacts,
// the rendered phase view reflects the new declared write-set immediately,
// through the same RenderViews entry point a full compile uses — not only
// at the next compile.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func setArtifactsFixture(t *testing.T) (root, planDir string) {
	t.Helper()
	root = chdirTemp(t)
	planDir = filepath.Join(root, "Plans", "Demo")
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

## Overview

Keep identity prose exactly.
`)
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{{
		ID: "work", Contract: "does the thing", Justifies: []string{"D-0001"},
		Phase: "01-core", Gate: model.Gate{Type: model.GateTests,
			Tests: []model.Test{{ID: "test_work", File: "w_test.go"}}},
		Hazards: model.Hazards{}, Estimate: 1, Artifacts: []string{"src/old.ext"},
	}}}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(fresh *model.Graph) error {
		fresh.Nodes = g.Nodes
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Render the initial view the way `sdd compile` would, so set-artifacts
	// has something on disk to refresh.
	_, _, err := rerenderViewsAfterEdit(planDir, "Demo")
	if err != nil {
		t.Fatalf("initial render: %v", err)
	}
	return root, planDir
}

func TestGraphSetArtifacts_RerendersViews(t *testing.T) {
	root, planDir := setArtifactsFixture(t)
	phasePath := filepath.Join(planDir, "01-core.md")
	before, err := os.ReadFile(phasePath)
	if err != nil {
		t.Fatalf("phase view must exist after the initial render: %v", err)
	}
	if !strings.Contains(string(before), "src/old.ext") {
		t.Fatalf("the initial view must carry the original artifact, got:\n%s", before)
	}

	c := graphSetArtifactsCmd()
	c.SetArgs([]string{"--plan", "Demo", "--node", "work", "--file", writeJSONFile(t, root, []string{"src/new.ext"})})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err != nil {
		t.Fatalf("set-artifacts: %v\n%s", err, out)
	}

	after, err := os.ReadFile(phasePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "src/old.ext") {
		t.Fatalf("the re-rendered view must drop the old artifact, got:\n%s", after)
	}
	if !strings.Contains(string(after), "src/new.ext") {
		t.Fatalf("the re-rendered view must carry the new artifact, got:\n%s", after)
	}
	if !strings.Contains(out, "rendered views refreshed") {
		t.Fatalf("set-artifacts must say the views were refreshed, got: %q", out)
	}
}

// TestGraphSetArtifacts_NoRenderLeavesViews: --no-render applies the graph
// edit and leaves every rendered view byte-identical, so a caller holding a
// graph-only mutation boundary can edit the write-set without the command
// writing outside the graph.
func TestGraphSetArtifacts_NoRenderLeavesViews(t *testing.T) {
	root, planDir := setArtifactsFixture(t)
	phasePath := filepath.Join(planDir, "01-core.md")
	before, err := os.ReadFile(phasePath)
	if err != nil {
		t.Fatalf("phase view must exist after the initial render: %v", err)
	}

	c := graphSetArtifactsCmd()
	c.SetArgs([]string{"--plan", "Demo", "--node", "work", "--no-render",
		"--file", writeJSONFile(t, root, []string{"src/new.ext"})})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err != nil {
		t.Fatalf("set-artifacts --no-render: %v\n%s", err, out)
	}

	after, err := os.ReadFile(phasePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("--no-render must leave the view byte-identical, got:\n%s", after)
	}
	if !strings.Contains(out, "rendered views not refreshed") {
		t.Fatalf("--no-render must report the views are stale, got: %q", out)
	}

	// The graph edit itself still applied.
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID("work")
	if n == nil || len(n.Artifacts) != 1 || n.Artifacts[0] != "src/new.ext" {
		t.Fatalf("the graph edit must still apply under --no-render: %+v", n)
	}
}

func writeJSONFile(t *testing.T, root string, artifacts []string) string {
	t.Helper()
	path := filepath.Join(root, "artifacts.json")
	var b strings.Builder
	b.WriteString("[")
	for i, a := range artifacts {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`"` + a + `"`)
	}
	b.WriteString("]")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
