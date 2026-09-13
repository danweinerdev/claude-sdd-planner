package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// completeFixture builds a planning root with one committed graph plan:
// implementation node "a" feeding a full review node "review-a". Both are
// pre-observed GREEN/pass so they derive closed — the fixture is the
// smallest graph review.Closed will mark fully closed. When open is true,
// "a" carries no observation, so neither node closes.
func completeFixture(t *testing.T, open bool) (root, planDir string) {
	t.Helper()
	root = chdirTemp(t)
	writeArtifact(t, root, "Plans/Demo", "README.md", `---
title: "Demo"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
phases: []
---

# Demo
`)
	a := model.Node{ID: "a", Contract: "does a", Phase: "01-core",
		Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_a", File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1}
	if !open {
		a.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
	}
	reviewA := model.Node{ID: "review-a", Contract: "reviews a", Phase: "01-core",
		Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1}
	if !open {
		reviewA.Verification = &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}
	}
	g := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{a, reviewA}}
	planDir = filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

func TestGraphPlanCompleteRefusesOpenNode(t *testing.T) {
	_, planDir := completeFixture(t, true)
	readme := filepath.Join(planDir, "README.md")
	before, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme})
		return root.Execute()
	})
	if err == nil {
		t.Fatalf("expected refusal, got success:\n%s", out)
	}
	if re, ok := err.(*refusedError); !ok {
		t.Fatalf("expected *refusedError, got %T: %v", err, err)
	} else if !strings.Contains(re.Error(), "a") {
		t.Fatalf("refusal must name the open node:\n%s", re.Error())
	}

	after, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("refused plan complete must write nothing; README changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestGraphPlanCompleteClosedGraphWrites(t *testing.T) {
	_, planDir := completeFixture(t, false)
	readme := filepath.Join(planDir, "README.md")

	// --dry-run must write nothing.
	dryBefore, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme, "--dry-run"})
		return root.Execute()
	}); err != nil {
		t.Fatalf("dry-run on a closed graph must succeed: %v\n%s", err, out)
	}
	dryAfter, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if string(dryAfter) != string(dryBefore) {
		t.Fatalf("--dry-run must write nothing; README changed:\nbefore:\n%s\nafter:\n%s", dryBefore, dryAfter)
	}

	// Real run: README status flips to complete, phase doc(s) become complete.
	if out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme})
		return root.Execute()
	}); err != nil {
		t.Fatalf("plan complete on a closed graph: %v\n%s", err, out)
	}

	readmeBytes, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readmeBytes), "status: complete") {
		t.Fatalf("README status was not flipped to complete:\n%s", readmeBytes)
	}

	phaseDoc := filepath.Join(planDir, "01-core.md")
	phaseBytes, err := os.ReadFile(phaseDoc)
	if err != nil {
		t.Fatalf("expected rendered phase doc %s: %v", phaseDoc, err)
	}
	if !strings.Contains(string(phaseBytes), "status: complete") {
		t.Fatalf("phase doc status was not complete:\n%s", phaseBytes)
	}
}

func TestGraphPhaseCompleteRefreshesPhaseStatus(t *testing.T) {
	_, planDir := completeFixture(t, false)

	// Render the initial (planned) phase doc via plan complete's renderer
	// path indirectly is circular; drive `sdd compile`-equivalent refresh by
	// running phase complete directly against the not-yet-rendered doc path.
	phaseDoc := filepath.Join(planDir, "01-core.md")

	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"phase", "complete", phaseDoc})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("phase complete on a closed phase: %v\n%s", err, out)
	}

	phaseBytes, err := os.ReadFile(phaseDoc)
	if err != nil {
		t.Fatalf("expected rendered phase doc %s: %v", phaseDoc, err)
	}
	if !strings.Contains(string(phaseBytes), "status: complete") {
		t.Fatalf("phase doc status was not refreshed to complete:\n%s", phaseBytes)
	}
}
