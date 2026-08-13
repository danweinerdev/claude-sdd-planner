package provision

import (
	"os"
	"path/filepath"
	"runtime"
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
