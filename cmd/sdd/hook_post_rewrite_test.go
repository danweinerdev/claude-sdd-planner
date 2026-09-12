package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/ops"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestHookPostRewriteCommandCapturesAndOnlyHintsRemap(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	raw := strings.Repeat("1", 40) + " " + strings.Repeat("2", 40) + "\n"
	var out bytes.Buffer
	if err := cmdHookPostRewrite(repo, "amend", strings.NewReader(raw), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"captured", "graph remap-revisions", "--map"} {
		if !strings.Contains(strings.ToLower(out.String()), strings.ToLower(want)) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".plans")); !os.IsNotExist(err) {
		t.Fatalf("capture touched graph/planning state: %v", err)
	}
}

func TestHookPostRewriteIsRegisteredWithRewriteArg(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"hook", "post-rewrite"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil || cmd.Use != "post-rewrite <rebase|amend>" {
		t.Fatalf("registered command = %#v", cmd)
	}
}

func TestRealGitRebaseCapturesMapWithInstalledBinaryOnPATH(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	sddPath := filepath.Join(binDir, "sdd")
	if runtime.GOOS == "windows" {
		sddPath += ".exe"
	}
	build := exec.Command("go", "build", "-o", sddPath, ".")
	build.Dir = packageDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build test-installed sdd: %v\n%s", err, out)
	}

	repo := t.TempDir()
	runRebaseGit(t, repo, nil, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "planning-config.json"), []byte(`{"planningRoot":"."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	doctor := exec.Command(sddPath, "doctor")
	doctor.Dir, doctor.Env = repo, env
	if out, err := doctor.CombinedOutput(); err != nil {
		t.Fatalf("doctor did not establish capture: %v\n%s", err, out)
	}
	writeRebaseFile := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		runRebaseGit(t, repo, nil, "add", name)
	}
	writeRebaseFile("base", "base\n")
	runRebaseGit(t, repo, nil, "-c", "user.name=SDD", "-c", "user.email=sdd@example.invalid", "commit", "-q", "-m", "base")
	runRebaseGit(t, repo, nil, "checkout", "-q", "-b", "topic")
	writeRebaseFile("topic", "topic\n")
	runRebaseGit(t, repo, nil, "-c", "user.name=SDD", "-c", "user.email=sdd@example.invalid", "commit", "-q", "-m", "topic")
	oldRev := runRebaseGit(t, repo, nil, "rev-parse", "HEAD")
	runRebaseGit(t, repo, nil, "checkout", "-q", "main")
	writeRebaseFile("main", "main\n")
	runRebaseGit(t, repo, nil, "-c", "user.name=SDD", "-c", "user.email=sdd@example.invalid", "commit", "-q", "-m", "main")
	runRebaseGit(t, repo, nil, "checkout", "-q", "topic")
	out := runRebaseGit(t, repo, env, "rebase", "main")
	if !strings.Contains(out, "captured") || !strings.Contains(out, "graph remap-revisions") {
		t.Fatalf("real rebase did not surface capture/hint:\n%s", out)
	}
	newRev := runRebaseGit(t, repo, nil, "rev-parse", "HEAD")
	mapDir := runRebaseGit(t, repo, nil, "rev-parse", "--git-path", "sdd/rewrite-maps")
	if !filepath.IsAbs(mapDir) {
		mapDir = filepath.Join(repo, mapDir)
	}
	entries, err := os.ReadDir(mapDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("captured maps = %d, err %v", len(entries), err)
	}
	raw, err := os.ReadFile(filepath.Join(mapDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != oldRev+" "+newRev+"\n" {
		t.Fatalf("captured real Git map = %q, want %q", raw, oldRev+" "+newRev+"\n")
	}
	if _, err := os.Stat(filepath.Join(repo, ".plans")); !os.IsNotExist(err) {
		t.Fatalf("real capture touched graph/planning state: %v", err)
	}

	// Consume the actual captured stream, not a synthesized old/new pair.
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte("---\ntitle: Demo\ntype: plan\nstatus: active\nrelated: []\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, SeqCounter: 1, Nodes: []model.Node{{ID: "work", Contract: "recorded before rebase", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1, Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean, Provenance: &model.Provenance{Kind: "git", Revision: oldRev}}}}}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(g.Nodes)
	dry, err := ops.RemapRevisions(ops.RemapOptions{Root: root, RepoRoot: repo, Plan: "Demo", Mapping: raw, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.RemapRevisions(ops.RemapOptions{Root: root, RepoRoot: repo, Plan: "Demo", Mapping: raw, ExpectDigest: dry.ExpectDigest}); err != nil {
		t.Fatal(err)
	}
	remapped, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(remapped.Nodes)
	if remapped.RevisionLineage[oldRev] != newRev || !bytes.Equal(before, after) || remapped.SeqCounter != g.SeqCounter {
		t.Fatal("captured-map remapping did not preserve observation identity while recording lineage")
	}
}

func runRebaseGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
