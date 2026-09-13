package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// AC-08 (user-entrypoint): the real binary, run as a subprocess against a
// planning root whose validation must consult git, exits 2 with the cause
// when git cannot run — never 0 (clean) and never 1 (authoritative
// findings). The same root with git available exits 0.
func TestValidationOperationalExit(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	doc := "---\n" +
		"title: \"Clean\"\ntype: research\nstatus: draft\n" +
		"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n---\n\n" +
		"# Clean\n\n## Summary\n\nText.\n\n## Context\n\nText.\n\n## Findings\n\nText.\n\n## Analysis\n\nText.\n\n## Open Questions\n\nText.\n"
	if err := os.MkdirAll(filepath.Join(root, "Research"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Research", "clean.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(gitExe, "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(path string) (int, string) {
		cmd := exec.Command(bin, "validate", "--root", root)
		cmd.Dir = root
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd: %v", err)
		}
		return code, string(out)
	}

	if code, out := run(filepath.Dir(gitExe)); code != 0 {
		t.Fatalf("control with git: exit %d\n%s", code, out)
	}
	code, out := run(t.TempDir())
	if code != 2 {
		t.Fatalf("validate without git: exit %d, want 2 (operational)\n%s", code, out)
	}
	if !strings.Contains(out, "git") || !strings.Contains(strings.ToLower(out), "could not") {
		t.Errorf("operational exit must name the cause; got:\n%s", out)
	}
	if strings.Contains(out, "0 error") || strings.Contains(out, "valid: true") {
		t.Errorf("operational failure reported as clean:\n%s", out)
	}
}

// lifecycleOperationalFixture builds a planning root whose plan targets a
// real Git repository, so every verb under test must consult git.
func lifecycleOperationalFixture(t *testing.T) (root, planningRev, mapPath string) {
	t.Helper()
	root = t.TempDir()
	target := t.TempDir()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(base, rel, body string) {
		t.Helper()
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(target, "init", "-q", "-b", "main")
	write(target, "base.txt", "base\n")
	git(target, "add", "base.txt")
	git(target, "commit", "-q", "-m", "base")
	rev := git(target, "rev-parse", "HEAD")
	write(target, "next.txt", "next\n")
	git(target, "add", "next.txt")
	git(target, "commit", "-q", "-m", "next")
	newRev := git(target, "rev-parse", "HEAD")

	write(root, "planning-config.json",
		fmt.Sprintf(`{"planningRoot":".","planMapping":{"Demo":"target"},"repositories":{"target":{"path":%q}}}`, target))
	write(root, "Plans/Demo/README.md",
		"---\ntitle: Demo\ntype: plan\nstatus: active\ncreated: 2026-09-12\nupdated: 2026-09-12\n"+
			"tags: []\nrelated: []\nphases: []\n---\n\n# Demo\n\n## Overview\n\nText.\n")

	g := &model.Graph{Version: 1, Nodes: []model.Node{{
		ID: "work", Contract: "works", Gate: model.Gate{Type: model.GateCommand, Command: "true"},
		Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{Result: model.ResultPass, Seq: 1,
			Isolation:  model.IsolationClean,
			Provenance: &model.Provenance{Kind: "git", Revision: rev}},
	}}}
	if err := gstore.Save(gstore.PathFor(filepath.Join(root, "Plans", "Demo")), g); err != nil {
		t.Fatal(err)
	}
	mapPath = filepath.Join(root, "rewrite-map.txt")
	write(root, "rewrite-map.txt", rev+" "+newRev+"\n")

	// `evidence add` resolves its repository by walking up from the artifact
	// to a .git marker, so the planning root is a committed repository too:
	// with git present the verb reaches a real HEAD and a clean tree.
	git(root, "init", "-q", "-b", "main")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "planning root")
	planningRev = git(root, "rev-parse", "HEAD")
	return root, planningRev, mapPath
}

// TestTransitionGateOperationalSweepExits (review-execution F-02): the
// lifecycle transition gate (gateDiagnostics) runs its before and after
// sweeps through the checked evaluation entry points and exits 2 with the
// cause when either sweep is operational. Today gateDiagnostics calls
// rules.RunWithWaivers (the unchecked form) for both sweeps and never
// inspects the operational outcome, so the same SDD198 finding in both
// sweeps dedups to an empty introduced set and `plan approve` proceeds —
// this test documents that defect red and must start failing once
// gateDiagnostics switches to the checked entry point.
//
// `plan approve` is the cheapest verb that reaches gateDiagnostics: it needs
// only a plan at status `draft` with no other blocking findings, no task
// IDs, and no candidate-artifact freezing pass.
func TestTransitionGateOperationalSweepExits(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("planning-config.json", `{"planningRoot":"."}`)
	write("Plans/Demo/README.md",
		"---\ntitle: Demo\ntype: plan\nstatus: draft\ncreated: 2026-09-12\nupdated: 2026-09-12\n"+
			"tags: []\nrelated: []\nphases: []\n---\n\n# Demo\n\n## Overview\n\nText.\n")

	// The planning root itself must be a committed repository: gateDiagnostics
	// scopes its rule sweeps against the repository the artifact lives in, and
	// several rules (e.g. the clean-worktree check) query it directly.
	git(root, "init", "-q", "-b", "main")
	git(root, "add", "-A")
	git(root, "commit", "-q", "-m", "planning root")

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(path string, extraArgs ...string) (int, string) {
		t.Helper()
		args := append([]string{"plan", "approve", "Plans/Demo/README.md"}, extraArgs...)
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd plan approve: %v", err)
		}
		return code, string(out)
	}

	// Control: git available, --dry-run so it never mutates the fixture. The
	// transition may refuse (exit 1) or succeed (exit 0) depending on
	// findings, but it must never be an operational exit — establishing that
	// the fixture itself is not the cause of an operational failure.
	if code, out := run(filepath.Dir(gitExe), "--dry-run"); code == 2 {
		t.Fatalf("control with git exited 2 (operational):\n%s", out)
	}

	statusBefore, err := os.ReadFile(filepath.Join(root, "Plans", "Demo", "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	code, out := run(t.TempDir())
	if code != 2 {
		t.Fatalf("without git: exit %d, want 2 (operational)\n%s", code, out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
		t.Errorf("the operational exit must name the cause; got:\n%s", out)
	}

	statusAfter, err := os.ReadFile(filepath.Join(root, "Plans", "Demo", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(statusAfter) != string(statusBefore) {
		t.Errorf("an operational sweep must not let the transition proceed; artifact changed:\nbefore:\n%s\nafter:\n%s",
			statusBefore, statusAfter)
	}
}

// TestLifecycleVerbsOperationalExit (FR-16, AC-08, DD-10): the lifecycle
// verbs that consult the VCS — `evidence add` and `graph remap-revisions` —
// exit 2 with the cause named when git cannot run, never 1 (an authoritative
// refusal about the repository's contents) and never 0. With git present the
// same invocations do not exit 2.
func TestLifecycleVerbsOperationalExit(t *testing.T) {
	bin := stressBinary(t)
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	root, planningRev, mapPath := lifecycleOperationalFixture(t)

	base := []string{"SDD_VCS_DISABLE_P4=1", "HOME=" + t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	run := func(path string, args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		cmd.Env = append(append([]string{}, base...), "PATH="+path)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running sdd %s: %v", strings.Join(args, " "), err)
		}
		return code, string(out)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"evidence add", []string{"evidence", "add", filepath.Join(root, "Plans", "Demo", "README.md"),
			"--plan", "--verified-by", "go test ./...", "--result", "ok",
			"--revision", planningRev, "--dry-run"}},
		{"graph remap-revisions", []string{"graph", "remap-revisions", "--plan", "Demo",
			"--map", mapPath, "--dry-run"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Control: git available. Whatever the verb decides, it is never
			// an operational exit.
			if code, out := run(filepath.Dir(gitExe), tc.args...); code == 2 {
				t.Fatalf("control with git exited 2 (operational):\n%s", out)
			}

			code, out := run(t.TempDir(), tc.args...)
			if code != 2 {
				t.Fatalf("without git: exit %d, want 2 (operational)\n%s", code, out)
			}
			low := strings.ToLower(out)
			if !strings.Contains(low, "git") || !strings.Contains(low, "could not") {
				t.Errorf("the operational exit must name the cause; got:\n%s", out)
			}
			// The inability must never be dressed as an authoritative answer
			// about the repository's contents.
			for _, forbidden := range []string{"does not exist", "not a Git repository",
				"uncommitted changes", "is not a full native revision identifier"} {
				if strings.Contains(out, forbidden) {
					t.Errorf("an operational failure was reported as %q:\n%s", forbidden, out)
				}
			}
		})
	}
}
