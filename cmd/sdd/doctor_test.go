package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// G-3: doctor must resolve the plugin root without CLAUDE_PLUGIN_ROOT by
// probing the portable installation locations agent-runtime.md documents.
func TestDiscoverPluginRootFallsBackToAgentsPlugins(t *testing.T) {
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

	got, source := discoverPluginRoot()
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
	if source != "~/.agents/plugins" {
		t.Errorf("source = %q, want ~/.agents/plugins", source)
	}

	// Portable root: hook checks are not applicable, not errors.
	if path, problem := checkHookBinary(got, source); path != "" || problem != "" {
		t.Errorf("checkHookBinary on a portable root = (%q, %q), want silence", path, problem)
	}
	if path, problem := checkHooksFile(got, source, false); path != "" || problem != "" {
		t.Errorf("checkHooksFile on a portable root = (%q, %q), want silence", path, problem)
	}
}

// The Codex cache wins over ~/.agents/plugins and validates the manifest name.
func TestDiscoverPluginRootPrefersCodexCacheAndValidates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))

	// An imposter plugin in the cache must be skipped.
	imposter := filepath.Join(home, ".codex", "plugins", "cache", "mkt", "other-plugin", "1.0.0")
	if err := os.MkdirAll(filepath.Join(imposter, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(imposter, "shared", "agent-runtime.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(imposter, "plugin.json"), []byte(`{"name":"other"}`), 0o644)

	real := filepath.Join(home, ".codex", "plugins", "cache", "mkt", "claude-sdd-planner", "2.3.4")
	if err := os.MkdirAll(filepath.Join(real, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(real, "shared", "agent-runtime.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(real, "plugin.json"), []byte(`{"name":"sdd-planner"}`), 0o644)

	got, source := discoverPluginRoot()
	if got != real {
		t.Errorf("root = %q, want %q", got, real)
	}
	if source != "codex plugin cache" {
		t.Errorf("source = %q, want codex plugin cache", source)
	}
}

// With nothing installed anywhere, the hook-binary check names every probed
// location instead of blaming only CLAUDE_PLUGIN_ROOT.
func TestCheckHookBinaryNamesAllProbedLocations(t *testing.T) {
	_, problem := checkHookBinary("", "")
	for _, want := range []string{"CLAUDE_PLUGIN_ROOT", ".codex", ".agents"} {
		if !strings.Contains(problem, want) {
			t.Errorf("problem %q does not mention %s", problem, want)
		}
	}
}
