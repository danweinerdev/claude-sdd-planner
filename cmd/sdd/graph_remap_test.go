package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/ops"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func cliRemapGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := append([]string{"-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid", "-c", "core.hooksPath=" + filepath.Join(dir, "no-hooks")}, args...)
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func remapCLIFixture(t *testing.T) (root, oldRev, newRev, mapPath string) {
	t.Helper()
	root = chdirTemp(t)
	target := t.TempDir()
	cliRemapGit(t, target, "init", "-q", "-b", "main")
	write := func(base, rel, body string) {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(target, "base", "base\n")
	cliRemapGit(t, target, "add", "base")
	cliRemapGit(t, target, "commit", "-q", "-m", "base")
	cliRemapGit(t, target, "checkout", "-q", "-b", "topic")
	write(target, "topic", "topic\n")
	cliRemapGit(t, target, "add", "topic")
	cliRemapGit(t, target, "commit", "-q", "-m", "topic")
	oldRev = cliRemapGit(t, target, "rev-parse", "HEAD")
	cliRemapGit(t, target, "checkout", "-q", "main")
	write(target, "main", "main\n")
	cliRemapGit(t, target, "add", "main")
	cliRemapGit(t, target, "commit", "-q", "-m", "main")
	cliRemapGit(t, target, "checkout", "-q", "topic")
	cliRemapGit(t, target, "rebase", "main")
	newRev = cliRemapGit(t, target, "rev-parse", "HEAD")

	write(root, "planning-config.json", fmt.Sprintf(`{"planningRoot":".","planMapping":{"Demo":"target"},"repositories":{"target":{"path":%q}}}`, target))
	write(root, "Plans/Demo/README.md", "---\ntitle: Demo\ntype: plan\nstatus: active\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags: []\nrelated: []\nphases: []\n---\n\n# Demo\n")
	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "work", Contract: "works", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1, Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean, Provenance: &model.Provenance{Kind: "git", Revision: oldRev}}}}}
	if err := gstore.Save(gstore.PathFor(filepath.Join(root, "Plans", "Demo")), g); err != nil {
		t.Fatal(err)
	}
	mapPath = filepath.Join(root, "rewrite-map.txt")
	write(root, "rewrite-map.txt", oldRev+" "+newRev+"\n")
	return root, oldRev, newRev, mapPath
}

func TestGraphRemapRevisionsCLIUsesMappedTargetAndPrintsResults(t *testing.T) {
	_, oldRev, newRev, mapPath := remapCLIFixture(t)
	human := runGraphVerb(t, "graph", "remap-revisions", "--plan", "Demo", "--map", mapPath, "--dry-run")
	for _, want := range []string{"would record", oldRev, newRev, "expect-digest:", "does not prove the rewritten code"} {
		if !strings.Contains(human, want) {
			t.Fatalf("human output missing %q:\n%s", want, human)
		}
	}
	var dry ops.RemapResult
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "remap-revisions", "--plan", "Demo", "--map", mapPath, "--dry-run", "--json")), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.ExpectDigest == "" || len(dry.Outcomes) != 1 || dry.Outcomes[0].Status != ops.RemapRecorded {
		t.Fatalf("dry JSON: %+v", dry)
	}
	var applied ops.RemapResult
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "remap-revisions", "--plan", "Demo", "--map", mapPath, "--expect-digest", dry.ExpectDigest, "--json")), &applied); err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Lineage[oldRev] != newRev {
		t.Fatalf("apply JSON: %+v", applied)
	}
}

func TestGraphRemapRevisionsCLIRequiresFlags(t *testing.T) {
	for _, args := range [][]string{{"graph", "remap-revisions"}, {"graph", "remap-revisions", "--plan", "Demo"}} {
		_, err := captureStdout(t, func() error { root := newRootCmd(); root.SetArgs(args); return root.Execute() })
		if err == nil || !strings.Contains(err.Error(), "--plan, --map, and --expect-digest") {
			t.Fatalf("args %v: %v", args, err)
		}
	}
}

func TestGraphRemapStaleFenceIsRefusedMutation(t *testing.T) {
	_, _, _, mapPath := remapCLIFixture(t)
	_, err := captureStdout(t, func() error {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"graph", "remap-revisions", "--plan", "Demo", "--map", mapPath, "--expect-digest", strings.Repeat("0", 64)})
		return cmd.Execute()
	})
	if code := exitCode(err); code != 1 {
		t.Fatalf("stale graph fence must exit 1 (refused mutation), got %d: %v", code, err)
	}
}
