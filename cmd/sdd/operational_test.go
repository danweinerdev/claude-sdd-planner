package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
