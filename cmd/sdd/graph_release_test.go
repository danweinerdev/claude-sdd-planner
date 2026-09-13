// TestGraphRelease* exercise `sdd graph release` against real git worktrees:
// an idle workspace (no commits beyond its base, no uncommitted changes)
// must be reaped the same way `graph gc` reaps one; a workspace carrying
// real work must be kept, explicitly, alongside its branch.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func releaseGitOK(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// releaseFixture builds a real git repo (the planning root and repo root
// coincide) with one claimed node backed by a git worktree, and returns the
// plan dir, the node id, and the claim's workspace handle.
func releaseFixture(t *testing.T) (planDir, nodeID, workspace string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := chdirTemp(t)
	releaseGitOK(t, root, "init", "-q")
	releaseGitOK(t, root, "config", "user.email", "t@example.com")
	releaseGitOK(t, root, "config", "user.name", "t")

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
`)
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	nodeID = "candidate"
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = []model.Node{{
			ID: nodeID, Contract: "does the thing", Justifies: []string{"D-0001"},
			Gate:    model.Gate{Type: model.GateCommand, Command: "true"},
			Hazards: model.Hazards{}, Estimate: 1,
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	releaseGitOK(t, root, "add", ".")
	releaseGitOK(t, root, "commit", "-q", "-m", "base")

	if _, err := captureStdout(t, func() error {
		handled, err := graphNext(planDir, true, "holder", "", false)
		if !handled {
			t.Fatal("claim must handle a plan with a committed graph")
		}
		return err
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID(nodeID)
	if n == nil || n.Claim == nil {
		t.Fatalf("node must be claimed: %+v", n)
	}
	return planDir, nodeID, n.Claim.Workspace
}

// TestGraphRelease_ReapsIdleWorkspace: no commits beyond base, no
// uncommitted changes — release must remove the worktree and delete the
// branch, the same as `graph gc` would.
func TestGraphRelease_ReapsIdleWorkspace(t *testing.T) {
	planDir, nodeID, workspace := releaseFixture(t)
	if workspace == "" {
		t.Fatal("fixture must produce a real worktree handle")
	}
	repoRoot := filepath.Dir(filepath.Dir(planDir))
	wsDir := filepath.Join(repoRoot, filepath.FromSlash(workspace))
	if _, err := os.Stat(wsDir); err != nil {
		t.Fatalf("worktree must exist before release: %v", err)
	}
	branches := releaseGitOK(t, repoRoot, "branch", "--list", "graph/"+nodeID+"-*", "--format=%(refname:short)")
	if branches == "" {
		t.Fatal("claim branch must exist before release")
	}

	c := graphReleaseCmd()
	c.SetArgs([]string{nodeID, "--plan", "Demo", "--by", "holder"})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err != nil {
		t.Fatalf("release: %v\n%s", err, out)
	}
	if !strings.Contains(out, "workspace removed") {
		t.Fatalf("idle workspace must be reported removed, got: %q", out)
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Fatal("an idle workspace's worktree must be removed by release")
	}
	after := releaseGitOK(t, repoRoot, "branch", "--list", "graph/"+nodeID+"-*", "--format=%(refname:short)")
	if after != "" {
		t.Fatalf("an idle workspace's claim branch must be deleted by release, found: %q", after)
	}
}

// TestGraphRelease_KeepsWorkspaceWithUncommittedWork: an edited-but-
// uncommitted worktree must survive release, reported explicitly with its
// path and branch, rather than being force-removed.
func TestGraphRelease_KeepsWorkspaceWithUncommittedWork(t *testing.T) {
	planDir, nodeID, workspace := releaseFixture(t)
	if workspace == "" {
		t.Fatal("fixture must produce a real worktree handle")
	}
	repoRoot := filepath.Dir(filepath.Dir(planDir))
	wsDir := filepath.Join(repoRoot, filepath.FromSlash(workspace))
	if err := os.WriteFile(filepath.Join(wsDir, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := graphReleaseCmd()
	c.SetArgs([]string{nodeID, "--plan", "Demo", "--by", "holder"})
	out, err := captureStdout(t, func() error { return c.Execute() })
	if err != nil {
		t.Fatalf("release: %v\n%s", err, out)
	}
	if !strings.Contains(out, "kept") || !strings.Contains(out, workspace) {
		t.Fatalf("a workspace with real work must be kept and named explicitly, got: %q", out)
	}
	branches := releaseGitOK(t, repoRoot, "branch", "--list", "graph/"+nodeID+"-*", "--format=%(refname:short)")
	if !strings.Contains(out, strings.TrimSpace(branches)) {
		t.Fatalf("the surviving branch must be named in the output, got: %q (branch %q)", out, branches)
	}
	if _, err := os.Stat(wsDir); err != nil {
		t.Fatalf("a workspace with real work must survive release: %v", err)
	}
	if branches == "" {
		t.Fatal("the claim branch must survive release when the workspace carries real work")
	}
}
