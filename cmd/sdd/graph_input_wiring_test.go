package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestStatusAndNextIgnoreInputContentEdits(t *testing.T) {
	root := dispositionFixture(t)
	writeArtifact(t, root, "docs/PRDs", "wire.md", "# Wire\n## Selected\nOriginal.\n## Other\nUnrelated.\n")
	decl := model.Input{Root: model.InputRootRepository, Path: "docs/PRDs/wire.md", Section: &model.InputSection{HeadingPath: []string{"Selected"}}}
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "Demo"))
	if _, err := gstore.Update(graphPath, func(g *model.Graph) error {
		n := g.NodeByID("decision")
		n.Inputs = []model.Input{decl}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		for _, args := range [][]string{{"graph", "status", "--plan", "Demo", "--json"}, {"next", "Plans/Demo", "--json"}} {
			var result struct {
				States map[string]int `json:"states"`
			}
			if err := json.Unmarshal([]byte(runGraphVerb(t, args...)), &result); err != nil {
				t.Fatal(err)
			}
			if result.States["STALE"] != want {
				t.Fatalf("%v changed state from input content: %+v, want %d stale", args, result.States, want)
			}
		}
	}
	check(0)
	writeArtifact(t, root, "docs/PRDs", "wire.md", "# Wire\n## Selected\nOriginal.\n## Other\nEdited only here.\n")
	check(0)
	writeArtifact(t, root, "docs/PRDs", "wire.md", "# Wire\n## Selected\nChanged contract.\n## Other\nEdited only here.\n")
	check(0)
	if err := os.Remove(filepath.Join(root, "docs/PRDs/wire.md")); err != nil {
		t.Fatal(err)
	}
	check(0)
}

func TestNextRefusesMissingInputBeforeClaim(t *testing.T) {
	root := dispositionFixture(t)
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "Demo"))
	if _, err := gstore.Update(graphPath, func(g *model.Graph) error {
		g.Nodes = []model.Node{{ID: "candidate", Contract: "c", Justifies: []string{"D-0001"},
			Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1,
			Inputs: []model.Input{{Root: model.InputRootRepository, Path: "docs/PRDs/missing.md"}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = graphNext("Plans/Demo", true, "tester", "", true)
	if exitCode(err) != 1 {
		t.Fatalf("missing required input must refuse with exit 1, got %v", err)
	}
	after, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("refused input changed the graph/claim")
	}
}
