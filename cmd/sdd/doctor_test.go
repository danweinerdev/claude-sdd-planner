package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClaudePluginRootSkipsPortableInstallation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows os.UserHomeDir
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))

	root := filepath.Join(home, ".agents", "plugins", "claude-sdd-planner")
	if err := os.MkdirAll(filepath.Join(root, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shared", "agent-runtime.md"), []byte("# runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"name":"sdd-planner"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	codexRoot := filepath.Join(home, ".codex", "plugins", "cache", "mkt", "sdd-planner", "2.8.0")
	if err := os.MkdirAll(filepath.Join(codexRoot, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexRoot, "shared", "agent-runtime.md"), []byte("# runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexRoot, "plugin.json"), []byte(`{"name":"sdd-planner"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, source := claudePluginRoot()
	if got != "" || source != "" {
		t.Errorf("claudePluginRoot() = (%q, %q), want portable installation ignored", got, source)
	}

	if path, problem := checkHookBinary(got, source); path != "" || problem != "" {
		t.Errorf("checkHookBinary on a portable root = (%q, %q), want silence", path, problem)
	}
	if path, problem := checkHooksFile(got, source, false); path != "" || problem != "" {
		t.Errorf("checkHooksFile on a portable root = (%q, %q), want silence", path, problem)
	}
}

func TestClaudePluginRootUsesRuntimeValue(t *testing.T) {
	want := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", want)

	got, source := claudePluginRoot()
	if got != want || source != "CLAUDE_PLUGIN_ROOT" {
		t.Errorf("claudePluginRoot() = (%q, %q), want (%q, CLAUDE_PLUGIN_ROOT)", got, source, want)
	}
}

func TestDoctorCheckIsNonWritingAndNormalModeRepairsGitHook(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := chdirTemp(t)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")

	var checkErr error
	checkOut, _ := captureStdout(t, func() error {
		checkErr = cmdDoctor(doctorOpts{Check: true})
		return checkErr
	})
	var refusal *refusedError
	if !errors.As(checkErr, &refusal) || exitCode(checkErr) != 1 {
		t.Fatalf("doctor --check error = %v (exit %d), want finding/exit 1", checkErr, exitCode(checkErr))
	}
	if !strings.Contains(checkOut, "Git post-rewrite hook") || !strings.Contains(checkOut, "missing") {
		t.Fatalf("check did not clearly report missing hook:\n%s", checkOut)
	}
	hookPath := strings.TrimSpace(runDoctorGit(t, repo, "rev-parse", "--git-path", "hooks/post-rewrite"))
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repo, hookPath)
	}
	if _, err := os.Lstat(hookPath); !os.IsNotExist(err) {
		t.Fatalf("doctor --check wrote %s: %v", hookPath, err)
	}

	if out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("doctor repair: %v\n%s", err, out)
	}
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("doctor did not install hook: %v", err)
	}
	if out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); err != nil {
		t.Fatalf("doctor --check after repair: %v\n%s", err, out)
	}
	if err := os.Remove(hookPath); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); exitCode(err) != 1 {
		t.Fatalf("check after deletion exit = %d, err %v", exitCode(err), err)
	}
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("doctor did not restore deleted managed hook: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\n# sdd managed post-rewrite hook v1\n# stale\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	staleBytes, _ := os.ReadFile(hookPath)
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); exitCode(err) != 1 {
		t.Fatalf("check on drift exit = %d, err %v", exitCode(err), err)
	}
	if after, _ := os.ReadFile(hookPath); string(after) != string(staleBytes) {
		t.Fatal("doctor --check rewrote drifted hook")
	}
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("doctor did not repair drifted hook: %v", err)
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("idempotent doctor: %v", err)
	}
	info, _ = os.Stat(hookPath)
	if !info.ModTime().Equal(mtime) {
		t.Fatal("idempotent doctor rewrote current hook")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(hookPath, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); exitCode(err) != 1 {
			t.Fatalf("check on non-executable hook exit = %d, err %v", exitCode(err), err)
		}
		if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
			t.Fatalf("doctor did not repair executable mode: %v", err)
		}
		info, _ = os.Stat(hookPath)
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("doctor left hook non-executable: %o", info.Mode().Perm())
		}
	}
}

func runDoctorGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestDoctorUnsafeGitHookIsOperationalFailure(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := chdirTemp(t)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	hookPath := strings.TrimSpace(runDoctorGit(t, repo, "rev-parse", "--git-path", "hooks/post-rewrite"))
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repo, hookPath)
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "user-hook")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, hookPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) })
	if exitCode(err) != 2 || !strings.Contains(out, "unsafe") {
		t.Fatalf("unsafe check exit=%d err=%v output:\n%s", exitCode(err), err, out)
	}
	if info, statErr := os.Lstat(hookPath); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("doctor changed unsafe symlink: %v, %v", info, statErr)
	}
}

func TestDoctorNoGitIsExplicitNoOp(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	chdirTemp(t)
	out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) })
	if err != nil {
		t.Fatalf("doctor in plain directory: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no-git") || !strings.Contains(out, "no repository hook") {
		t.Fatalf("doctor did not explain no-Git no-op:\n%s", out)
	}
}
