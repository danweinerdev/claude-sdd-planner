package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// TestForkGraphIntentRealEntryPoint exercises the installed command surface,
// not a hand-built states.Inputs value. The fixture persists the selector,
// bound parent, equal local ID, explicit legacy inventory, and graph before
// invoking `graph status`.
func TestForkGraphIntentRealEntryPoint(t *testing.T) {
	root := forkReadFixture(t, false, false, "effective local replacement")
	planning := filepath.Join(root, ".plans")
	capture := decisionview.CaptureForRepository(root)
	local := capture.Collections[forkReadLocal]
	if local == nil || local.Metadata == nil {
		t.Fatalf("fixture did not load selected fork context: %+v", capture.Diagnostics)
	}
	metadata := *local.Metadata
	metadata.LegacyContexts = append(metadata.LegacyContexts, decisionview.LegacyContext{
		Root: decisionview.SourceRootPlanning, Path: "Plans/Demo/README.md",
		Namespace: forkReadParent, LocalIDs: []string{"D-0001"},
	})
	parent := capture.Collections[forkReadParent]
	target := decisionview.QualifiedID(qualified(forkReadParent, "D-0001"))
	basis, err := decisionview.CreateBasis(metadata.ParentBindingID, parent, target, []string{metadata.ParentBindingID})
	if err != nil {
		t.Fatal(err)
	}
	replacement := forkReadEntry("D-0001", "accepted", "effective local replacement")
	relation, err := json.Marshal(decisionview.OverrideDeclaration{Target: target, Basis: basis})
	if err != nil {
		t.Fatal(err)
	}
	var override map[string]any
	if err := json.Unmarshal(relation, &override); err != nil {
		t.Fatal(err)
	}
	replacement["override"] = override
	forkReadWriteLedger(t, planning, "Decisions/fork.md", []map[string]any{
		replacement,
		forkReadEntry("D-0100", "accepted", "ordinary local rule"),
	}, metadata)
	forkReadWrite(t, planning, "Plans/Demo/README.md", []byte(`---
title: Demo
type: plan
status: draft
created: 2026-09-10
updated: 2026-09-10
tags: []
related: []
phases: []
---

# Demo
`))
	pass := func(id string, citation string) model.Node {
		return model.Node{ID: id, Contract: "retains " + id, Justifies: []string{citation},
			Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "Test" + id, File: "fixture_test.go"}}},
			Hazards: model.Hazards{}, Estimate: 1, Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}}
	}
	graph := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{
		pass("historical-original", qualified(forkReadParent, "D-0001")),
		pass("effective-replacement", qualified(forkReadLocal, "D-0001")),
		pass("captured-legacy", "D-0001"),
	}}
	planDir := filepath.Join(planning, "Plans", "Demo")
	if err := gstore.Save(gstore.PathFor(planDir), graph); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runSdd(stressBinary(t), root, "graph", "status", "--plan", "Demo", "--json")
	requireCLIExit(t, err, 0, stdout, stderr)
	var status struct {
		Nodes []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, stdout)
	}
	states := map[string]string{}
	for _, node := range status.Nodes {
		states[node.ID] = node.State
	}
	for _, id := range []string{"historical-original", "effective-replacement", "captured-legacy"} {
		if states[id] != "GREEN" {
			t.Errorf("%s = %q, want GREEN from its persisted qualified/legacy identity", id, states[id])
		}
	}
	// Read-only graph intent resolution must not rewrite historical citations.
	raw, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) || !containsJSONCitation(raw, qualified(forkReadParent, "D-0001")) {
		t.Fatal("graph status rewrote or removed the historical qualified citation")
	}
}

func containsJSONCitation(raw []byte, citation string) bool {
	var graph model.Graph
	if json.Unmarshal(raw, &graph) != nil {
		return false
	}
	for _, node := range graph.Nodes {
		for _, got := range node.Justifies {
			if got == citation {
				return true
			}
		}
	}
	return false
}
