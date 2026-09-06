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

// repairFixture builds a planning root with a spec (AC-01) and a committed
// graph carrying one unanchored node and one already-anchored node.
func repairFixture(t *testing.T) {
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
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	unanchored := model.Node{ID: "w", Contract: "works", Justifies: []string{"AC-01"},
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}
	anchored := model.Node{ID: "done", Contract: "done", Justifies: []string{"AC-01"},
		IntentHashes: map[string]string{"AC-01": "sha256:existing"},
		Gate:         model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}
	if err := gstore.Save(gstore.PathFor(planDir), &model.Graph{Version: model.SchemaVersion,
		Nodes: []model.Node{unanchored, anchored}}); err != nil {
		t.Fatal(err)
	}
}

func TestGraphRepairIntentCLI(t *testing.T) {
	repairFixture(t)

	// Whole-graph repair: only the unanchored node is backfilled; the
	// already-anchored node is left alone.
	var out struct {
		OK       bool     `json:"ok"`
		Repaired []string `json:"repaired"`
		Changes  []struct {
			Node  string `json:"node"`
			Cited string `json:"cited"`
			Hash  string `json:"hash"`
		} `json:"changes"`
		DryRun bool `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "repair-intent", "--plan", "Demo", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Repaired) != 1 || out.Repaired[0] != "w" {
		t.Fatalf("repair result: %+v", out)
	}
	if len(out.Changes) != 1 || out.Changes[0].Node != "w" || out.Changes[0].Cited != "AC-01" {
		t.Fatalf("changes must name exactly the repaired node/citation: %+v", out.Changes)
	}
	if out.Changes[0].Hash == "" || len(out.Changes[0].Hash) < len("sha256:") {
		t.Fatalf("the hash must be present for independent assertion: %+v", out.Changes)
	}
	if out.DryRun {
		t.Fatal("a real run must not report dry_run")
	}

	// Idempotence: a second run reports zero changes.
	var second struct {
		Repaired []string `json:"repaired"`
		Changes  []struct {
			Node string `json:"node"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "repair-intent", "--plan", "Demo", "--json")), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Changes) != 0 || len(second.Repaired) != 0 {
		t.Fatalf("idempotent re-run must report zero changes: %+v", second)
	}
}

func TestGraphRepairIntentDryRunLeavesGraphUnchanged(t *testing.T) {
	repairFixture(t)
	root, _, err := resolveRoots(".", "")
	if err != nil {
		t.Fatal(err)
	}
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "Demo"))
	before, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		OK      bool `json:"ok"`
		DryRun  bool `json:"dry_run"`
		Changes []struct {
			Node  string `json:"node"`
			Cited string `json:"cited"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "repair-intent", "--plan", "Demo", "--dry-run", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || !out.DryRun {
		t.Fatalf("dry-run must report dry_run=true and ok: %+v", out)
	}
	if len(out.Changes) != 1 || out.Changes[0].Node != "w" || out.Changes[0].Cited != "AC-01" {
		t.Fatalf("dry-run must plan the same change: %+v", out.Changes)
	}
	after, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a dry-run must leave the graph byte-identical")
	}
}

func TestGraphRepairIntentNamedNodeMustExist(t *testing.T) {
	repairFixture(t)
	_, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "repair-intent", "--plan", "Demo", "--node", "ghost"})
		return root.Execute()
	})
	if err == nil {
		t.Fatal("a named --node that does not exist must refuse")
	}
	// Operational refusal (the named node does not exist) is exit 2, not 1.
	if code := exitCode(err); code != 2 {
		t.Fatalf("a nonexistent --node is operational (exit 2), got exit %d", code)
	}
}

// TestGraphRepairIntentRefusalExitCode: an eligibility refusal (a claimed node
// with a missing hash) exits 1 with the reason list, and the graph stays
// byte-identical. --json carries ok:false with the same reasons.
func TestGraphRepairIntentRefusalExitCode(t *testing.T) {
	repairFixture(t)
	root, _, err := resolveRoots(".", "")
	if err != nil {
		t.Fatal(err)
	}
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "Demo"))
	// Claim the unanchored node so a whole-graph repair refuses on eligibility.
	if _, err := gstore.Update(graphPath, func(g *model.Graph) error {
		g.NodeByID("w").Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}

	// Text form: exit 1, reason names the claim.
	_, err = captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "repair-intent", "--plan", "Demo"})
		return root.Execute()
	})
	if code := exitCode(err); code != 1 {
		t.Fatalf("an eligibility refusal must exit 1, got exit %d (err=%v)", code, err)
	}
	if err == nil || !strings.Contains(err.Error(), "is claimed by") {
		t.Fatalf("the refusal must name the claim: %v", err)
	}

	after, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused repair must leave the graph byte-identical")
	}

	// JSON form: exit 1, ok:false, reasons present, graph still unchanged.
	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "repair-intent", "--plan", "Demo", "--json"})
		return root.Execute()
	})
	if code := exitCode(err); code != 1 {
		t.Fatalf("an eligibility refusal with --json must exit 1, got exit %d (err=%v)", code, err)
	}
	var res struct {
		OK      bool     `json:"ok"`
		Reasons []string `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("--json refusal output must be one JSON document: %v\n%s", err, out)
	}
	if res.OK {
		t.Fatalf("a refusal must report ok:false, got %s", out)
	}
	if len(res.Reasons) == 0 {
		t.Fatalf("a refusal must carry the reasons, got %s", out)
	}
	afterJSON, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(afterJSON) {
		t.Fatal("a refused repair must leave the graph byte-identical")
	}
}
