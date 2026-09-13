package vcs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitOff points PATH at an empty directory so no git (or p4) can be found by
// the code under test. It must run serially: t.Setenv forbids t.Parallel.
func gitOff(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SDD_VCS_DISABLE_P4", "1")
}

func initCheckedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "one")
	return dir
}

// FR-16 / DD-10: an unrunnable git is an operational failure at detection
// and at every operation, never NoRepo and never ErrNotFound.
func TestVCSOperationalErrors(t *testing.T) {
	repo := initCheckedRepo(t)
	plain := t.TempDir()
	t.Setenv("SDD_VCS_DISABLE_P4", "1")

	// Detected while git is available, then git disappears: every history
	// query reports inability, not absence.
	live, err := DetectChecked(repo)
	if err != nil || live.Kind() != Git {
		t.Fatalf("baseline detection: %v / %v", live, err)
	}
	gitOff(t)
	if _, err := live.RevisionExists(strings.Repeat("a", 40)); !errors.Is(err, ErrOperational) || errors.Is(err, ErrNotFound) {
		t.Errorf("RevisionExists without git: %v, want ErrOperational and not ErrNotFound", err)
	}
	if _, err := live.Head(); !errors.Is(err, ErrOperational) {
		t.Errorf("Head without git: %v, want ErrOperational", err)
	}
	if _, err := live.FileAt("HEAD", "a.txt"); !errors.Is(err, ErrOperational) {
		t.Errorf("FileAt without git: %v, want ErrOperational", err)
	}
	if _, err := live.IsAncestor("HEAD", "HEAD"); !errors.Is(err, ErrOperational) {
		t.Errorf("IsAncestor without git: %v, want ErrOperational", err)
	}
	if _, _, err := live.Clean(); !errors.Is(err, ErrOperational) {
		t.Errorf("Clean without git: %v, want ErrOperational", err)
	}
	if _, err := live.FileInIndex("a.txt"); !errors.Is(err, ErrOperational) {
		t.Errorf("FileInIndex without git: %v, want ErrOperational", err)
	}

	// Detection itself, with git gone: a real repository and a plain
	// directory are both undecidable, so both are operational.
	for name, dir := range map[string]string{"repository": repo, "plain-dir": plain} {
		r, err := DetectChecked(dir)
		if !errors.Is(err, ErrOperational) {
			t.Errorf("DetectChecked(%s) without git: repo=%v err=%v, want ErrOperational", name, r, err)
		}
		if r != nil {
			t.Errorf("DetectChecked(%s) without git returned an adapter alongside the error", name)
		}
		// The unchecked entry point must not erase the failure into NoRepo.
		legacy := Detect(dir)
		if legacy.Kind() == None {
			t.Errorf("Detect(%s) without git reported None (a plain tree); the failure was erased", name)
		}
		if _, err := legacy.Head(); !errors.Is(err, ErrOperational) {
			t.Errorf("Detect(%s) without git: Head() = %v, want ErrOperational", name, err)
		}
	}
}

// FR-16: with git available, genuine absence keeps its authoritative answer
// and a non-repository is still NoRepo — a successful query is required
// before either is claimed.
func TestAuthoritativeSCMAbsence(t *testing.T) {
	t.Setenv("SDD_VCS_DISABLE_P4", "1")
	repo := initCheckedRepo(t)
	r, err := DetectChecked(repo)
	if err != nil {
		t.Fatal(err)
	}
	bogus := strings.Repeat("b", 40)
	if ok, err := r.RevisionExists(bogus); ok || !errors.Is(err, ErrNotFound) || errors.Is(err, ErrOperational) {
		t.Errorf("RevisionExists(bogus) = %v, %v; want false, ErrNotFound", ok, err)
	}
	if _, err := r.FileAt("HEAD", "missing.txt"); !errors.Is(err, ErrNotFound) || errors.Is(err, ErrOperational) {
		t.Errorf("FileAt(missing) = %v; want ErrNotFound", err)
	}
	if _, err := r.FileInIndex("missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FileInIndex(missing) = %v; want ErrNotFound", err)
	}
	if _, err := r.Parents(bogus); !errors.Is(err, ErrNotFound) {
		t.Errorf("Parents(bogus) = %v; want ErrNotFound", err)
	}
	head, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := r.IsAncestor(bogus, head); ok || err == nil || errors.Is(err, ErrOperational) {
		// An unknown revision is an error from git, but not an operational one.
		t.Errorf("IsAncestor(bogus, head) = %v, %v; want false with a non-operational error", ok, err)
	}
	if ok, err := r.IsAncestor(head, head); !ok || err != nil {
		t.Errorf("IsAncestor(head, head) = %v, %v; want true", ok, err)
	}

	plain := t.TempDir()
	n, err := DetectChecked(plain)
	if err != nil || n.Kind() != None {
		t.Errorf("DetectChecked(plain) = %v, %v; want NoRepo, nil", n, err)
	}
	if _, err := n.Head(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("NoRepo.Head() = %v; want ErrUnsupported", err)
	}
}
