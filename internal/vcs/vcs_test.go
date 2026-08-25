package vcs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runOK(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runOK(t, dir, "git", "init", "-q")
	return dir
}

func TestDetectNoRepo(t *testing.T) {
	dir := t.TempDir()
	r := Detect(dir)
	if r.Kind() != None {
		t.Fatalf("Kind() = %v, want None", r.Kind())
	}
	if _, err := r.RevisionExists("x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("RevisionExists: err = %v, want ErrUnsupported", err)
	}
	if _, err := r.Head(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Head: err = %v, want ErrUnsupported", err)
	}
	if _, err := r.IsAncestor("a", "b"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("IsAncestor: err = %v, want ErrUnsupported", err)
	}
	if _, err := r.Parents("a"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Parents: err = %v, want ErrUnsupported", err)
	}
	if _, err := r.FileAt("a", "x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("FileAt: err = %v, want ErrUnsupported", err)
	}
	if _, err := r.ChangedPaths("a"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ChangedPaths: err = %v, want ErrUnsupported", err)
	}
	if _, err := r.RevisionsAfter("a"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("RevisionsAfter: err = %v, want ErrUnsupported", err)
	}
	if _, _, err := r.Clean(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Clean: err = %v, want ErrUnsupported", err)
	}
	if r.RevisionSyntaxValid("deadbeef") {
		t.Errorf("RevisionSyntaxValid: want false for NoRepo")
	}
}

func TestGitDetection(t *testing.T) {
	dir := initRepo(t)
	r := Detect(dir)
	if r.Kind() != Git {
		t.Fatalf("Kind() = %v, want Git", r.Kind())
	}
	root, err := filepath.EvalSymlinks(r.Root())
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if root != want {
		t.Errorf("Root() = %s, want %s", root, want)
	}
}

func TestGitWorktreeDetection(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f"), []byte("a\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "init")
	wtDir := filepath.Join(t.TempDir(), "wt")
	runOK(t, dir, "git", "worktree", "add", "-q", wtDir, "-b", "feature")
	r := Detect(wtDir)
	if r.Kind() != GitWorktree {
		t.Fatalf("Kind() = %v, want GitWorktree", r.Kind())
	}
}

func TestRevisionSyntaxValid(t *testing.T) {
	dir := initRepo(t)
	r := Detect(dir)
	full := strings.Repeat("a", 40)
	if !r.RevisionSyntaxValid(full) {
		t.Errorf("want valid: %s", full)
	}
	upper := strings.Repeat("A", 40)
	if !r.RevisionSyntaxValid(upper) {
		t.Errorf("want valid (uppercase hex, matches Python's case-insensitive class): %s", upper)
	}
	for _, bad := range []string{
		strings.Repeat("a", 39),
		strings.Repeat("a", 41),
		strings.Repeat("g", 40),
		"",
		"HEAD",
	} {
		if r.RevisionSyntaxValid(bad) {
			t.Errorf("want invalid: %q", bad)
		}
	}
}

func TestParentsAndIsAncestor(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f"), []byte("a\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "c1")
	c1 := runOK(t, dir, "git", "rev-parse", "HEAD")

	os.WriteFile(filepath.Join(dir, "f"), []byte("b\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "c2")
	c2 := runOK(t, dir, "git", "rev-parse", "HEAD")

	r := Detect(dir)

	parents, err := r.Parents(c2)
	if err != nil {
		t.Fatal(err)
	}
	if len(parents) != 1 || parents[0] != c1 {
		t.Errorf("Parents(c2) = %v, want [%s]", parents, c1)
	}

	root, err := r.Parents(c1)
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 0 {
		t.Errorf("Parents(c1) = %v, want empty (root commit)", root)
	}

	ok, err := r.IsAncestor(c1, c2)
	if err != nil || !ok {
		t.Errorf("IsAncestor(c1, c2) = %v, %v, want true, nil", ok, err)
	}
	ok, err = r.IsAncestor(c2, c1)
	if err != nil || ok {
		t.Errorf("IsAncestor(c2, c1) = %v, %v, want false, nil", ok, err)
	}

	// Merge commit has two parents.
	runOK(t, dir, "git", "checkout", "-q", "-b", "side", c1)
	os.WriteFile(filepath.Join(dir, "g"), []byte("side\n"), 0o644)
	runOK(t, dir, "git", "add", "g")
	runOK(t, dir, "git", "commit", "-q", "-m", "side")
	side := runOK(t, dir, "git", "rev-parse", "HEAD")
	runOK(t, dir, "git", "checkout", "-q", c2)
	runOK(t, dir, "git", "checkout", "-q", "-B", "mainline", c2)
	runOK(t, dir, "git", "merge", "-q", "--no-ff", "-m", "merge", "side")
	mergeRev := runOK(t, dir, "git", "rev-parse", "HEAD")
	mergeParents, err := r.Parents(mergeRev)
	if err != nil {
		t.Fatal(err)
	}
	if len(mergeParents) != 2 {
		t.Errorf("Parents(merge) = %v, want 2 parents (c2=%s, side=%s)", mergeParents, c2, side)
	}
}

func TestFileAt(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f"), []byte("hello\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "c1")
	head := runOK(t, dir, "git", "rev-parse", "HEAD")

	r := Detect(dir)
	content, err := r.FileAt(head, "f")
	if err != nil || string(content) != "hello\n" {
		t.Errorf("FileAt(present) = %q, %v", content, err)
	}
	if _, err := r.FileAt(head, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FileAt(absent): err = %v, want ErrNotFound", err)
	}
}

func TestClean(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f"), []byte("a\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "c1")

	r := Detect(dir)
	clean, dirty, err := r.Clean()
	if err != nil || !clean || len(dirty) != 0 {
		t.Errorf("Clean() on clean tree = %v, %v, %v", clean, dirty, err)
	}

	os.WriteFile(filepath.Join(dir, "f"), []byte("b\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "untracked"), []byte("x\n"), 0o644)
	clean, dirty, err = r.Clean()
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Errorf("Clean() on dirty tree = true, want false")
	}
	if len(dirty) != 2 {
		t.Errorf("Clean() dirty paths = %v, want 2 entries", dirty)
	}
}

func TestChangedPaths(t *testing.T) {
	dir := initRepo(t)
	// diff-tree without --root shows nothing for a root commit, so the second
	// (non-root) commit is the one under test.
	os.WriteFile(filepath.Join(dir, "a"), []byte("1\n"), 0o644)
	runOK(t, dir, "git", "add", "a")
	runOK(t, dir, "git", "commit", "-q", "-m", "c1")

	os.WriteFile(filepath.Join(dir, "a"), []byte("2\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b"), []byte("1\n"), 0o644)
	runOK(t, dir, "git", "add", "a", "b")
	runOK(t, dir, "git", "commit", "-q", "-m", "c2")
	head := runOK(t, dir, "git", "rev-parse", "HEAD")

	r := Detect(dir)
	paths, err := r.ChangedPaths(head)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Errorf("ChangedPaths = %v, want 2 entries", paths)
	}
}

func TestRevisionsAfter(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f"), []byte("a\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "c1")
	c1 := runOK(t, dir, "git", "rev-parse", "HEAD")

	os.WriteFile(filepath.Join(dir, "f"), []byte("b\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "c2")
	c2 := runOK(t, dir, "git", "rev-parse", "HEAD")

	r := Detect(dir)
	after, err := r.RevisionsAfter(c1)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0] != c2 {
		t.Errorf("RevisionsAfter(c1) = %v, want [%s]", after, c2)
	}

	after, err = r.RevisionsAfter(c2)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("RevisionsAfter(c2) = %v, want empty", after)
	}
}

func TestRevisionExists(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "f"), []byte("a\n"), 0o644)
	runOK(t, dir, "git", "add", "f")
	runOK(t, dir, "git", "commit", "-q", "-m", "c1")
	head := runOK(t, dir, "git", "rev-parse", "HEAD")

	r := Detect(dir)
	ok, err := r.RevisionExists(head)
	if err != nil || !ok {
		t.Errorf("RevisionExists(head) = %v, %v, want true, nil", ok, err)
	}
	ok, err = r.RevisionExists(strings.Repeat("f", 40))
	if !errors.Is(err, ErrNotFound) || ok {
		t.Errorf("RevisionExists(missing) = %v, %v, want false, ErrNotFound", ok, err)
	}
}

// --- Perforce: detection/syntax only, no server available in CI. ---

func hasP4(t *testing.T) bool {
	_, err := exec.LookPath("p4")
	if err != nil {
		t.Skip("p4 not installed; skipping Perforce integration checks")
	}
	return true
}

func TestP4Detection(t *testing.T) {
	hasP4(t)
	// Without a real workspace mapping, detection must fall through to None,
	// not fabricate a Perforce repo.
	dir := t.TempDir()
	r := Detect(dir)
	if r.Kind() == Perforce {
		t.Fatalf("unexpected Perforce detection in an unmapped directory")
	}
}

func TestP4RevisionSyntax(t *testing.T) {
	p := &p4Repo{root: "/nonexistent"}
	if !p.RevisionSyntaxValid("12345") {
		t.Errorf("want valid changelist number")
	}
	for _, bad := range []string{"default", "", "abc", strings.Repeat("a", 40)} {
		if p.RevisionSyntaxValid(bad) {
			t.Errorf("want invalid changelist: %q", bad)
		}
	}
}

func TestP4UnsupportedOperations(t *testing.T) {
	p := &p4Repo{root: "/nonexistent"}
	if _, err := p.Head(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Head: err = %v, want ErrUnsupported", err)
	}
	if _, err := p.IsAncestor("1", "2"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("IsAncestor: err = %v, want ErrUnsupported", err)
	}
	if _, err := p.Parents("1"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Parents: err = %v, want ErrUnsupported", err)
	}
	if _, err := p.RevisionsAfter("1"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("RevisionsAfter: err = %v, want ErrUnsupported", err)
	}
	if _, err := p.FileAt("notanumber", "path"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("FileAt(bad syntax): err = %v, want ErrUnsupported", err)
	}
	if _, err := p.ChangedPaths("notanumber"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ChangedPaths(bad syntax): err = %v, want ErrUnsupported", err)
	}
}

// SDD_VCS_DISABLE_P4 must short-circuit the probe before any subprocess or
// network activity: the test suites set it because `p4 info` is a network
// RPC paid per detection of every non-git fixture directory.
func TestP4ProbeDisabledByEnv(t *testing.T) {
	t.Setenv("SDD_VCS_DISABLE_P4", "1")
	if r := probeP4(t.TempDir()); r != nil {
		t.Fatalf("probeP4 must return nil when disabled, got %v", r.Kind())
	}
	if r := Detect(t.TempDir()); r.Kind() == Perforce {
		t.Fatal("Detect fabricated a Perforce repo with the probe disabled")
	}
}
