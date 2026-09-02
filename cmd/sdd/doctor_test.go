package main

import (
	"os"
	"path/filepath"
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
