package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// CanonPath must agree with what git reports for the same directory — that
// agreement is its entire purpose. On Windows this exercises 8.3 short-name
// expansion whenever TMP is spelled short (C:\Users\DANIEL~1\...); on macOS
// it exercises the /tmp -> /private/tmp symlink; on Linux it is typically an
// identity with symlinks resolved.
func TestCanonPathAgreesWithGitToplevel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	gitTop := filepath.Clean(filepath.FromSlash(strings.TrimSpace(string(out))))
	canon := CanonPath(dir)
	if !strings.EqualFold(canon, gitTop) {
		t.Errorf("CanonPath(%q) = %q, git reports %q — the two must name the directory identically", dir, canon, gitTop)
	}
}

// A nonexistent path is cleaned, never an error: canonicalization is a
// comparison aid.
func TestCanonPathNonexistentPassthrough(t *testing.T) {
	p := filepath.Join(t.TempDir(), "does", "not", "exist")
	if got := CanonPath(p); got != filepath.Clean(p) {
		t.Errorf("CanonPath(%q) = %q, want cleaned passthrough", p, got)
	}
}

// Canonicalization must be idempotent.
func TestCanonPathIdempotent(t *testing.T) {
	dir := t.TempDir()
	once := CanonPath(dir)
	if twice := CanonPath(once); twice != once {
		t.Errorf("CanonPath not idempotent: %q -> %q", once, twice)
	}
}

// os.MkdirTemp under a short-named TMP and the canonical spelling must
// resolve to the same directory. Windows-specific value; harmless elsewhere.
func TestCanonPathResolvesTempDirSpelling(t *testing.T) {
	dir := t.TempDir()
	canon := CanonPath(dir)
	a, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(canon)
	if err != nil {
		t.Fatalf("canonical spelling %q does not exist: %v", canon, err)
	}
	if !os.SameFile(a, b) {
		t.Errorf("%q and %q are not the same directory", dir, canon)
	}
}
