package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// analyticsFixture builds a planning root with a committed waist-shaped
// graph (two sources feeding one node that feeds two sinks) whose analytics
// have known answers:
//
//	a(1)  b(1)          depth 0, width 2
//	   \  /
//	   m(2)             depth 1, width 1  <- the waist (cut vertex)
//	   /  \
//	 x(1)  y(3)         depth 2, width 2
//
// critical path a->m->y (6 of 8, ceiling 1.33); histogram [2 1 2] HOURGLASS.
func analyticsFixture(t *testing.T) {
	t.Helper()
	root := chdirTemp(t)
	writeArtifact(t, root, "Specs/Sample", "README.md", `---
title: "S"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# S

## Acceptance Criteria

- [ ] **AC-01**: The API answers.
`)
	writeArtifact(t, root, "Plans/Demo", "README.md", `---
title: "Demo"
type: plan
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/Sample]
phases: []
---

# Demo
`)
	node := func(id string, est int, deps ...string) model.Node {
		return model.Node{ID: id, Contract: "does " + id, Justifies: []string{"AC-01"},
			Deps: deps, Gate: model.Gate{Type: model.GateTests,
				Tests: []model.Test{{ID: "test_" + id, File: "t.ext"}}},
			Hazards: model.Hazards{}, Estimate: est}
	}
	g := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{
		node("a", 1), node("b", 1), node("m", 2, "a", "b"),
		node("x", 1, "m"), node("y", 3, "m"),
	}}
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
}

// runGraphVerb executes one CLI invocation and returns captured stdout.
func runGraphVerb(t *testing.T, args ...string) string {
	t.Helper()
	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs(args)
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("sdd %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func TestGraphAnalyticsPathRiskShape(t *testing.T) {
	analyticsFixture(t)

	var path struct {
		OK      bool     `json:"ok"`
		Path    []string `json:"path"`
		Length  int      `json:"length"`
		Total   int      `json:"total"`
		Ceiling float64  `json:"ceiling"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "path", "--plan", "Demo", "--json")), &path); err != nil {
		t.Fatal(err)
	}
	if strings.Join(path.Path, ">") != "a>m>y" || path.Length != 6 || path.Total != 8 {
		t.Fatalf("path: %+v", path)
	}
	if path.Ceiling < 1.33 || path.Ceiling > 1.34 {
		t.Fatalf("ceiling: %v", path.Ceiling)
	}
	human := runGraphVerb(t, "graph", "path", "--plan", "Demo")
	if !strings.Contains(human, "a -> m -> y") || !strings.Contains(human, "6 of 8") {
		t.Fatalf("human path output:\n%s", human)
	}

	var risk struct {
		CutVertices []struct {
			ID     string `json:"id"`
			State  string `json:"state"`
			Weight int    `json:"critical_weight"`
		} `json:"cut_vertices"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "risk", "--plan", "Demo", "--json")), &risk); err != nil {
		t.Fatal(err)
	}
	if len(risk.CutVertices) != 1 || risk.CutVertices[0].ID != "m" || risk.CutVertices[0].Weight != 5 {
		t.Fatalf("risk: %+v", risk)
	}

	var shape struct {
		Histogram []int  `json:"histogram"`
		Class     string `json:"class"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "shape", "--plan", "Demo", "--json")), &shape); err != nil {
		t.Fatal(err)
	}
	if len(shape.Histogram) != 3 || shape.Histogram[0] != 2 || shape.Histogram[1] != 1 || shape.Histogram[2] != 2 {
		t.Fatalf("histogram: %v", shape.Histogram)
	}
	if shape.Class != "HOURGLASS" {
		t.Fatalf("class: %s", shape.Class)
	}
	if human := runGraphVerb(t, "graph", "shape", "--plan", "Demo"); !strings.Contains(human, "silhouette: HOURGLASS") ||
		!strings.Contains(human, "`sdd graph risk` names it") {
		t.Fatalf("human shape output:\n%s", human)
	}
}

func TestGraphAnalyticsStatusShowExport(t *testing.T) {
	analyticsFixture(t)

	var status struct {
		States map[string]int `json:"states"`
		Closed int            `json:"closed"`
		Nodes  []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "status", "--plan", "Demo", "--json")), &status); err != nil {
		t.Fatal(err)
	}
	if status.States["READY"] != 2 || status.States["BLOCKED"] != 3 || status.Closed != 0 {
		t.Fatalf("status: %+v", status)
	}
	if len(status.Nodes) != 5 || status.Nodes[0].ID != "a" {
		t.Fatalf("status nodes: %+v", status.Nodes)
	}

	var show struct {
		Node  *model.Node `json:"node"`
		State string      `json:"state"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "show", "m", "--plan", "Demo", "--json")), &show); err != nil {
		t.Fatal(err)
	}
	if show.Node == nil || show.Node.ID != "m" || show.State != "BLOCKED" {
		t.Fatalf("show: %+v", show)
	}
	if human := runGraphVerb(t, "graph", "show", "m", "--plan", "Demo"); !strings.Contains(human, "m  [BLOCKED]") ||
		!strings.Contains(human, "deps: a, b") {
		t.Fatalf("human show output:\n%s", human)
	}

	// Exports render; each format carries its signature syntax.
	for _, tc := range []struct{ format, want string }{
		{"mermaid", "flowchart TD"},
		{"mermaid", "a --> m"},
		{"dot", "digraph plan"},
		{"dot", `"m" -> "y";`},
		{"plan", "1. a — does a [READY, estimate 1]"},
		{"shape", "silhouette: HOURGLASS"},
	} {
		if out := runGraphVerb(t, "graph", "export", "--plan", "Demo", "--format", tc.format); !strings.Contains(out, tc.want) {
			t.Errorf("export %s missing %q:\n%s", tc.format, tc.want, out)
		}
	}
	var exp struct {
		Format string `json:"format"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "export", "--plan", "Demo", "--format", "dot", "--json")), &exp); err != nil {
		t.Fatal(err)
	}
	if exp.Format != "dot" || !strings.Contains(exp.Body, "digraph") {
		t.Fatalf("export json: %+v", exp)
	}

	// An unknown format refuses naming the vocabulary.
	_, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "export", "--plan", "Demo", "--format", "png"})
		return root.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "mermaid, dot, plan, shape") {
		t.Fatalf("unknown format must refuse naming the formats: %v", err)
	}
}
