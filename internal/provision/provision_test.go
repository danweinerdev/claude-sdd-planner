package provision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeSdd writes an executable that reports the given version, standing in for
// a user-installed binary.
func fakeSdd(t *testing.T, dir, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "sdd")
	body := "#!/bin/sh\n[ \"$1\" = version ] && echo 'sdd " + version + "'\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func manifest(t *testing.T, root, floor string) {
	t.Helper()
	dir := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"sdd-planner","version":"9.9.9","minSddVersion":"` + floor + `"}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestProvisionPlacesPluginCopy: the copy is what hooks.json executes, so
// FR-37 requires it be placed or refreshed on every run.
func TestProvisionPlacesPluginCopy(t *testing.T) {
	root := t.TempDir()
	manifest(t, root, "1.0.0")
	binDir := t.TempDir()
	source := fakeSdd(t, binDir, "1.16.0")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	res, err := Provision(root, "1.0.0")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if res.Source != source {
		t.Errorf("source = %q, want %q", res.Source, source)
	}
	if !res.Refreshed {
		t.Error("first run must write the plugin copy")
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "sdd")); err != nil {
		t.Errorf("plugin copy absent: %v", err)
	}
}

// TestResolvePrefersPluginCopy pins FR-05's order. The plugin copy is what
// hooks run, so reporting on a different PATH binary would describe something
// the hooks never execute.
func TestResolvePrefersPluginCopy(t *testing.T) {
	root := t.TempDir()
	manifest(t, root, "1.0.0")
	pluginBin := fakeSdd(t, filepath.Join(root, "bin"), "2.0.0")

	other := t.TempDir()
	fakeSdd(t, other, "3.0.0")
	t.Setenv("PATH", other+string(os.PathListSeparator)+os.Getenv("PATH"))

	res, err := Resolve(root, "1.0.0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Source != pluginBin {
		t.Errorf("source = %q, want the plugin copy %q", res.Source, pluginBin)
	}
}

// TestResolveRejectsBelowFloor: a binary predating a schema or CLI contract
// change silently applies the wrong rules, so FR-38 rejects rather than uses it.
func TestResolveRejectsBelowFloor(t *testing.T) {
	root := t.TempDir()
	manifest(t, root, "5.0.0")
	binDir := t.TempDir()
	fakeSdd(t, binDir, "1.16.0")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	res, err := Resolve(root, "5.0.0")
	if err == nil {
		t.Fatal("a binary below the floor must not be admitted")
	}
	var sawReason bool
	for _, c := range res.Candidates {
		if c.Version == "1.16.0" && !c.Admitted && c.Reason != "" {
			sawReason = true
		}
	}
	if !sawReason {
		t.Errorf("rejection must name the detected version and floor; got %+v", res.Candidates)
	}
}

// TestProvisionWritesNothingWhenNoCandidate is FR-40's ordering requirement:
// provisioning is verified before any filesystem mutation.
func TestProvisionWritesNothingWhenNoCandidate(t *testing.T) {
	root := t.TempDir()
	manifest(t, root, "5.0.0")
	t.Setenv("PATH", t.TempDir()) // nothing installed

	if _, err := Provision(root, "5.0.0"); err == nil {
		t.Fatal("expected a failure with no candidate")
	}
	if _, err := os.Stat(filepath.Join(root, "bin")); !os.IsNotExist(err) {
		t.Error("provisioning wrote bin/ despite failing to resolve a binary")
	}
}

// TestCompareVersionsIgnoresPrerelease: the floor is about the CLI contract a
// binary implements, and a prerelease of a version implements it.
func TestCompareVersionsIgnoresPrerelease(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.16.0", "1.16.0", 0},
		{"1.17.0", "1.16.0", 1},
		{"1.15.9", "1.16.0", -1},
		{"1.16.0-rc1", "1.16.0", 0},
		{"v1.16.0", "1.16.0", 0},
		{"2.0.0", "1.99.99", 1},
		// Build metadata is ignored for ordering, like a prerelease.
		{"1.16.0+build.5", "1.16.0", 0},
		// Prerelease on the floor side: still core-only.
		{"1.16.0", "1.16.0-rc1", 0},
		{"1.16.1-rc1", "1.16.0", 1},
	}
	for _, c := range cases {
		got, err := compareVersions(c.a, c.b)
		if err != nil {
			t.Errorf("compare(%q,%q): %v", c.a, c.b, err)
			continue
		}
		if got != c.want {
			t.Errorf("compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := compareVersions("not-a-version", "1.0.0"); err == nil {
		t.Error("an unparseable version must be an error, not silently ordered")
	}
}

// TestInstallCommandNamesTheFloor: FR-38 requires the rejection to carry the
// exact command that satisfies it.
func TestInstallCommandNamesTheFloor(t *testing.T) {
	got := InstallCommand("1.16.0")
	want := "go install github.com/danweinerdev/claude-sdd-planner/cmd/sdd@v1.16.0"
	if got != want {
		t.Errorf("InstallCommand = %q, want %q", got, want)
	}
}

// TestCompareVersionsRejectsMalformed pins the inputs the floor must refuse
// rather than guess at. `v1` and `1.16` are valid or coercible to some semver
// parsers; admitting a binary on that guess would silently bypass the floor.
func TestCompareVersionsRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "v1", "1.16", "1.16.x", "latest", "1.16.0.1", "not-a-version"} {
		if _, err := compareVersions(bad, "1.16.0"); err == nil {
			t.Errorf("compareVersions(%q, floor) accepted a malformed version", bad)
		}
		if _, err := compareVersions("1.16.0", bad); err == nil {
			t.Errorf("compareVersions(version, %q) accepted a malformed floor", bad)
		}
	}
}

// TestInstallHooksWritesOnePlatformCommand pins the fix for a hook error that
// appeared on every tool call: hooks.json has no OS conditional, so a file
// listing both a POSIX and a PowerShell command guarantees one of them fails
// on every platform. Linux users saw `powershell: command not found`; the
// previous shape produced `sdd.exe: No such file or directory` for the same
// reason. Exactly one command per event must be written, naming an
// interpreter that exists here.
func TestInstallHooksWritesOnePlatformCommand(t *testing.T) {
	root := t.TempDir()
	path, wrote, err := InstallHooks(root)
	if err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	if !wrote {
		t.Error("the first install reported no write")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("generated hooks.json is not valid JSON: %v\n%s", err, raw)
	}
	for _, event := range []string{"SessionStart", "PreToolUse"} {
		matchers, ok := doc.Hooks[event]
		if !ok || len(matchers) != 1 {
			t.Fatalf("%s: want one matcher, got %d", event, len(matchers))
		}
		cmds := matchers[0].Hooks
		if len(cmds) != 1 {
			t.Errorf("%s: want exactly one command for this platform, got %d — "+
				"a command for another platform fails on every tool call", event, len(cmds))
			continue
		}
		got := cmds[0].Command
		wantWrapper := "sdd-hook.sh"
		unwanted := "powershell"
		if runtime.GOOS == "windows" {
			wantWrapper, unwanted = "sdd-hook.ps1", "sh \""
		}
		if !strings.Contains(got, wantWrapper) {
			t.Errorf("%s: command does not use this platform's wrapper: %q", event, got)
		}
		if strings.Contains(got, unwanted) {
			t.Errorf("%s: command references another platform's interpreter: %q", event, got)
		}
		// The plugin root must stay a variable Claude Code expands at hook
		// time; baking in an absolute path re-creates the staleness across
		// upgrades that this whole mechanism removes.
		if !strings.Contains(got, "${CLAUDE_PLUGIN_ROOT}") {
			t.Errorf("%s: command does not defer to ${CLAUDE_PLUGIN_ROOT}: %q", event, got)
		}
	}

	// Idempotent: an unchanged file is left alone.
	if _, wrote, err := InstallHooks(root); err != nil || wrote {
		t.Errorf("second install rewrote an identical file (wrote=%v, err=%v)", wrote, err)
	}
}
