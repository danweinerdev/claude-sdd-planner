package digest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileAndBytesAgree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile, err := File(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fromFile, "sha256:") || len(fromFile) != len("sha256:")+64 {
		t.Fatalf("digest spelling: %q", fromFile)
	}
	if fromFile != Bytes([]byte("payload")) {
		t.Fatal("File and Bytes must agree on identical content")
	}
	if _, err := File(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("a missing file is an error from File")
	}
}

func TestDigesterMemoizesWithinOneRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(dir)
	first := d.Artifact("a.txt")
	if first == "" {
		t.Fatal("readable artifact must digest")
	}
	// A mid-pass edit must NOT change the answer within the same run: every
	// node sees the same bytes during one derive.
	if err := os.WriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d.Artifact("a.txt") != first {
		t.Fatal("Digester must memoize within one run")
	}
	// A fresh run sees the new content.
	if New(dir).Artifact("a.txt") == first {
		t.Fatal("a new Digester must re-read")
	}
	// Missing artifacts are the distinct empty value.
	if New(dir).Artifact("nope.txt") != "" {
		t.Fatal(`missing artifacts digest to ""`)
	}
}

func TestDirDigestsDeterministically(t *testing.T) {
	build := func(order []string) string {
		dir := t.TempDir()
		for _, rel := range order {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("content of "+rel), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	a := build([]string{"crates/x/src/lib.rs", "Cargo.toml", "crates/y/Cargo.toml"})
	b := build([]string{"crates/y/Cargo.toml", "crates/x/src/lib.rs", "Cargo.toml"})
	da, err := Dir(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(da, "sha256:") || len(da) != len("sha256:")+64 {
		t.Fatalf("digest spelling: %q", da)
	}
	db, err := Dir(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatal("identical trees built in different order must digest identically")
	}
	// Content change changes the digest.
	if err := os.WriteFile(filepath.Join(a, "Cargo.toml"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	edited, err := Dir(a)
	if err != nil {
		t.Fatal(err)
	}
	if edited == da {
		t.Fatal("an edited file must change the directory digest")
	}
	// A new file changes the digest.
	if err := os.WriteFile(filepath.Join(b, "new.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	grown, err := Dir(b)
	if err != nil {
		t.Fatal(err)
	}
	if grown == db {
		t.Fatal("a new file must change the directory digest")
	}
	if _, err := Dir(filepath.Join(a, "missing")); err == nil {
		t.Fatal("a missing directory is an error from Dir")
	}
}

func TestArtifactDigestsDirectories(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "crates", "maestro-config", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "lib.rs"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Graphs declare directory artifacts with a trailing slash; Clean strips it,
	// so both spellings hit the same cache key and the same digest.
	d := New(root)
	withSlash := d.Artifact("crates/")
	if withSlash == "" {
		t.Fatal("a directory artifact must digest")
	}
	if New(root).Artifact("crates") != withSlash {
		t.Fatal("trailing-slash and bare spellings must agree")
	}
	// Same content digested from a different root (workspace vs mainline)
	// must agree.
	root2 := t.TempDir()
	sub2 := filepath.Join(root2, "crates", "maestro-config", "src")
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub2, "lib.rs"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if New(root2).Artifact("crates/") != withSlash {
		t.Fatal("identical trees under different roots must digest identically")
	}
	// Memoization holds for directories within one run.
	if err := os.WriteFile(filepath.Join(sub, "lib.rs"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d.Artifact("crates/") != withSlash {
		t.Fatal("Digester must memoize directory digests within one run")
	}
	if New(root).Artifact("crates/") == withSlash {
		t.Fatal("a new Digester must re-walk the directory")
	}
	// Missing directories stay the distinct empty value.
	if New(root).Artifact("nope/") != "" {
		t.Fatal(`missing directory artifacts digest to ""`)
	}
}
