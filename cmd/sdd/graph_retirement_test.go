package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestHistoricalRetirementCLI(t *testing.T) {
	root := chdirTemp(t)
	writeArtifact(t, root, "Plans/Demo", "README.md", "---\ntitle: Demo\ntype: plan\nstatus: draft\nrelated: []\nphases: []\n---\n# Demo\n")
	writeArtifact(t, root, "Plans/Demo", "01 Old.md", "---\ntype: phase\nplan: Demo\nphase: 1\ntasks:\n  - id: '1.1'\n---\n# Old\n")
	git := func(args ...string) string {
		t.Helper()
		argv := append([]string{"-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid", "-c", "core.hooksPath=" + filepath.Join(root, "no-hooks")}, args...)
		cmd := exec.Command("git", argv...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "Plans/Demo")
	git("commit", "-qm", "history")
	rev := git("rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(root, "Plans/Demo/01 Old.md")); err != nil {
		t.Fatal(err)
	}
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "Demo"))
	if err := gstore.Save(graphPath, &model.Graph{Version: 1, Nodes: []model.Node{{ID: "replacement", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(graphPath)
	args := []string{"graph", "retire", "--plan", "Demo", "--id", "task-1-1", "--source-rev", "HEAD", "--source-path", "Plans/Demo/01 Old.md", "--source-id", "1.1", "--replaced-by", "replacement", "--json"}
	var result struct {
		OK     bool                   `json:"ok"`
		Record model.RetirementRecord `json:"record"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, append(args, "--dry-run")...)), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Record.Source.Revision != rev {
		t.Fatalf("not pinned: %+v", result)
	}
	after, _ := os.ReadFile(graphPath)
	if string(before) != string(after) {
		t.Fatal("dry run changed graph")
	}
	runGraphVerb(t, args...)
	g, err := gstore.Load(graphPath)
	if err != nil || g.RetirementSources["task-1-1"].Source.Revision != rev {
		t.Fatalf("record not persisted: %+v %v", g, err)
	}
}
