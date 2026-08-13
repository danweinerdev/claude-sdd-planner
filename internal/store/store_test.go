package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Digest must be stable for identical content and differ for differing
// content, since FR-48's isolation precondition depends on both properties.
func TestDigestStableAndDistinct(t *testing.T) {
	a := Digest("hello")
	b := Digest("hello")
	if a != b {
		t.Errorf("Digest not stable: %q != %q", a, b)
	}
	c := Digest("goodbye")
	if a == c {
		t.Error("Digest did not differ for differing content")
	}
}

// WriteAtomic must produce exactly the requested bytes, preserve an existing
// file's permission mode across an overwrite (FR-24 mustn't silently loosen
// permissions), and leave no temp file behind on success.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.md")

	if err := WriteAtomic(path, "first content"); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "first content" {
		t.Errorf("content = %q, want %q", got, "first content")
	}

	// Give the file a distinctive mode, then overwrite and check it survives.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if err := WriteAtomic(path, "second content"); err != nil {
		t.Fatalf("WriteAtomic (overwrite): %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want %o (overwrite must preserve mode)", fi.Mode().Perm(), 0o640)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "second content" {
		t.Errorf("content after overwrite = %q, want %q", got, "second content")
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range ents {
		// The lock sidecar is expected to persist: its inode is the thing
		// concurrent sessions contend on, so deleting it after each write
		// would let two writers lock two different inodes and both proceed.
		if strings.HasSuffix(e.Name(), ".sdd-lock") {
			continue
		}
		if strings.Contains(e.Name(), ".sdd-") {
			t.Errorf("leftover temp file after successful write: %s", e.Name())
		}
	}
}

// FindPlanningRoot must find a config in the start directory itself, and also
// walk upward to find one declared by a parent when the start directory has
// none of its own.
func TestFindPlanningRootLocatesAndWalksUp(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"planningRoot": "."}`)

	got, err := FindPlanningRoot(root)
	if err != nil {
		t.Fatalf("FindPlanningRoot(start dir): %v", err)
	}
	if got != filepath.Clean(root) {
		t.Errorf("got %q, want %q", got, root)
	}

	// A subdirectory with no config of its own must find the parent's.
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = FindPlanningRoot(sub)
	if err != nil {
		t.Fatalf("FindPlanningRoot(nested start): %v", err)
	}
	if got != filepath.Clean(root) {
		t.Errorf("got %q, want %q", got, root)
	}
}

// planningRoot may be ".", a relative subdirectory, or an absolute path; all
// three forms must resolve correctly.
func TestFindPlanningRootResolvesEachForm(t *testing.T) {
	t.Run("dot", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, `{"planningRoot": "."}`)
		got, err := FindPlanningRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Clean(dir) {
			t.Errorf("got %q, want %q", got, dir)
		}
	})

	t.Run("relative subdir", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, `{"planningRoot": "Planning"}`)
		got, err := FindPlanningRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, "Planning")
		if got != filepath.Clean(want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("absolute", func(t *testing.T) {
		dir := t.TempDir()
		abs := t.TempDir()
		writeConfig(t, dir, `{"planningRoot": `+strconv.Quote(abs)+`}`)
		got, err := FindPlanningRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Clean(abs) {
			t.Errorf("got %q, want %q", got, abs)
		}
	})
}

// No config anywhere at or above start must be a clear error, not a silent
// fallback that would let a caller write into the wrong place.
func TestFindPlanningRootErrorsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindPlanningRoot(dir); err == nil {
		t.Fatal("want error when no planning-config.json exists at or above start")
	}
}

// Malformed JSON and an empty planningRoot are both configuration mistakes a
// caller must be told about explicitly rather than resolving to a guess.
func TestFindPlanningRootErrorsOnBadConfig(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, `{not valid json`)
		if _, err := FindPlanningRoot(dir); err == nil {
			t.Fatal("want error on malformed JSON")
		}
	})

	t.Run("empty planningRoot", func(t *testing.T) {
		dir := t.TempDir()
		writeConfig(t, dir, `{"planningRoot": ""}`)
		if _, err := FindPlanningRoot(dir); err == nil {
			t.Fatal("want error on empty planningRoot")
		}
	})
}

// List must render forward-slash, planning-root-relative paths for both
// nested types (spec/design/plan, which live at <Dir>/<Name>/README.md) and
// flat types (research, which is <Dir>/*.md), must return nil rather than an
// error when the type's directory doesn't exist yet (a fresh planning root
// with no specs authored is not a failure), and must reject an unknown type.
func TestList(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "Specs", "Widget", "README.md"), "spec")
	mustWriteFile(t, filepath.Join(root, "Specs", "Gadget", "README.md"), "spec")
	// A directory with no README.md is not a spec and must be skipped.
	if err := os.MkdirAll(filepath.Join(root, "Specs", "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := List(root, "spec")
	if err != nil {
		t.Fatalf("List(spec): %v", err)
	}
	want := []string{"Specs/Gadget/README.md", "Specs/Widget/README.md"}
	if !equalUnordered(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	mustWriteFile(t, filepath.Join(root, "Research", "topic-one.md"), "research")
	mustWriteFile(t, filepath.Join(root, "Research", "topic-two.md"), "research")

	got, err = List(root, "research")
	if err != nil {
		t.Fatalf("List(research): %v", err)
	}
	want = []string{"Research/topic-one.md", "Research/topic-two.md"}
	if !equalUnordered(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	got, err = List(root, "design")
	if err != nil {
		t.Fatalf("List(design) on absent directory: %v", err)
	}
	if got != nil {
		t.Errorf("List(design) on absent directory = %v, want nil", got)
	}

	if _, err := List(root, "bogus"); err == nil {
		t.Fatal("want error for an unknown artifact type")
	}
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "planning-config.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]int{}
	for _, s := range a {
		am[s]++
	}
	for _, s := range b {
		am[s]--
	}
	for _, n := range am {
		if n != 0 {
			return false
		}
	}
	return true
}
