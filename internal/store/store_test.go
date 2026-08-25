package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// modeBitsSupported reports whether the platform can represent POSIX
// permission bits at all. On Windows os.Chmod expresses only the read-only
// flag, so assertions about distinctive modes are meaningless there — the
// gated behavior is a platform no-op, not a missing feature.
func modeBitsSupported() bool {
	return runtime.GOOS != "windows"
}

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
	// Unix-only: Windows has no POSIX permission bits — os.Chmod there can
	// express only the read-only flag, so a "distinctive mode" cannot exist
	// and the preservation branch in WriteAtomic is a platform no-op.
	if modeBitsSupported() {
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
	} else if err := WriteAtomic(path, "second content"); err != nil {
		t.Fatalf("WriteAtomic (overwrite): %v", err)
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

// G-4: a JSON syntax error in planning-config.json must name the line and
// column of the offending byte — a bare "invalid character '}'" hard-fails
// every sdd command while pointing at nothing.
func TestDescribeJSONErrorNamesLineAndColumn(t *testing.T) {
	raw := []byte("{\n  \"planningRoot\": \".\",\n}\n")
	var c Config
	err := json.Unmarshal(raw, &c)
	if err == nil {
		t.Fatal("expected a syntax error from the trailing comma")
	}
	got := DescribeJSONError(raw, err)
	if !strings.Contains(got, "line 3") {
		t.Errorf("message lacks the offending line: %q", got)
	}
	if !strings.Contains(got, "invalid character") {
		t.Errorf("message lost the underlying error: %q", got)
	}
}

// A positionless error passes through untouched.
func TestDescribeJSONErrorPassthrough(t *testing.T) {
	err := errors.New("something else")
	if got := DescribeJSONError([]byte("{}"), err); got != "something else" {
		t.Errorf("got %q, want passthrough", got)
	}
}

// FindPlanningRoot surfaces the position in its own error.
func TestFindPlanningRootReportsParsePosition(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "{\n  \"planningRoot\": \".\",\n}\n")
	_, err := FindPlanningRoot(dir)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error lacks position: %v", err)
	}
}

// chdirTemp switches the working directory to a fresh temp dir for the test's
// duration and returns it. ResolveArtifactPath and CheckCreatePath resolve
// the planning root from ".", so these tests are cwd-sensitive by nature.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A create (path existing nowhere) must be anchored to the planning root:
// returning the literal spelling is how `apply --create Specs/X/README.md`
// manufactured ./Specs in the caller's working directory.
func TestResolveArtifactPathAnchorsCreatesToPlanningRoot(t *testing.T) {
	root := chdirTemp(t)
	writeConfig(t, root, `{"planningRoot": ".plans"}`)
	if err := os.MkdirAll(filepath.Join(root, ".plans"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := ResolveArtifactPath("Specs/New/README.md")
	if filepath.ToSlash(got) != ".plans/Specs/New/README.md" {
		t.Errorf("create was not anchored to the planning root: %q", got)
	}

	// An in-root spelling of a create is kept as given.
	inRoot := filepath.Join(".plans", "Specs", "New", "README.md")
	if got := ResolveArtifactPath(inRoot); got != inRoot {
		t.Errorf("in-root create spelling was rewritten: %q", got)
	}

	// Resolution must not create anything on disk.
	if _, err := os.Stat(filepath.Join(root, "Specs")); !os.IsNotExist(err) {
		t.Error("resolution created ./Specs as a side effect")
	}
}

// CheckCreatePath refuses creates outside the planning root (absolute paths
// and `..` escapes), allows in-root creates and existing files anywhere, and
// fails open when no planning root is resolvable.
func TestCheckCreatePath(t *testing.T) {
	root := chdirTemp(t)
	writeConfig(t, root, `{"planningRoot": ".plans"}`)
	if err := os.MkdirAll(filepath.Join(root, ".plans"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CheckCreatePath(filepath.Join(".plans", "Specs", "X", "README.md")); err != nil {
		t.Errorf("in-root create refused: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "Specs", "X", "README.md")
	if err := CheckCreatePath(outside); err == nil {
		t.Error("absolute out-of-root create was not refused")
	}

	if err := CheckCreatePath(filepath.Join("..", "escape.md")); err == nil {
		t.Error("`..` escape was not refused")
	}

	// An existing file outside the root may still be written (the
	// literal-path-wins contract for edits).
	existing := filepath.Join(root, "notes.md")
	mustWriteFile(t, existing, "# notes\n")
	if err := CheckCreatePath(existing); err != nil {
		t.Errorf("existing out-of-root file refused: %v", err)
	}
}

// CheckCreatePath must not block work outside any planning workspace.
func TestCheckCreatePathFailsOpenWithoutConfig(t *testing.T) {
	chdirTemp(t)
	if err := CheckCreatePath("anything.md"); err != nil {
		t.Errorf("no-config case must fail open, got: %v", err)
	}
}

// Reading a nonexistent path must not create its directory chain: the shared
// lock's MkdirAll manufactured ./Specs/Demo (plus a lock sidecar) even when
// the command that probed the path was refused and wrote nothing.
func TestReadDoesNotCreateDirectories(t *testing.T) {
	dir := chdirTemp(t)
	art, err := Read(filepath.Join("Specs", "Demo", "README.md"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if art.Exists {
		t.Fatal("artifact should not exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "Specs")); !os.IsNotExist(err) {
		t.Error("Read created the target's directory chain as a side effect")
	}
}
