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
