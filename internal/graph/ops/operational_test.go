package ops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// installOneShotGitShim puts a git shim ahead of the real git on PATH that
// answers exactly one invocation (delegating to the real binary) and then
// removes its own execute bit, so every subsequent "git" invocation fails
// to start at all. In this test the first invocation the shim answers is
// VCS detection's own `git rev-parse --show-toplevel`; it disables itself
// immediately after, so the later RevisionExists `cat-file` call is the one
// that fails to start. That failure is a genuine procexec-level operational
// failure — not merely a nonzero git exit. This ordering is load-bearing:
// if detection ever issued a second git call before RevisionExists ran, the
// shim would already be disabled by then and the test would silently shift
// to exercising a detection failure instead of a RevisionExists query
// failure.
func installOneShotGitShim(t *testing.T) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	chmod, err := exec.LookPath("chmod")
	if err != nil {
		t.Skip("chmod not installed")
	}
	shimDir := t.TempDir()
	script := "#!/bin/sh\n" +
		chmod + " -x \"$0\"\n" +
		"exec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir)
}

// TestRemapRevisionsQueryFailureIsOperational: a RevisionExists query that
// cannot run at all (the git subprocess itself unavailable) is a different
// fact from the revision not existing, and must not be folded into the "not
// an available commit" message — that would send the operator hunting a
// commit that is actually there but simply could not be probed.
func TestRemapRevisionsQueryFailureIsOperational(t *testing.T) {
	f := newRemapFixture(t)
	mapping := []byte(f.old + " " + f.rewritten)

	// Control: with git fully available, a genuinely nonexistent revision is
	// reported as not available, and the error is not operational.
	bogus := "0000000000000000000000000000000000000000"
	badMapping := []byte(f.old + " " + bogus)
	if _, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: badMapping, DryRun: true}); err == nil {
		t.Fatal("expected an error for a nonexistent revision")
	} else if !strings.Contains(err.Error(), "is not an available commit") {
		t.Fatalf("control: err = %v, want \"is not an available commit\"", err)
	} else if errors.Is(err, vcs.ErrOperational) {
		t.Fatalf("control: err must not be operational: %v", err)
	}

	// The revision-existence query itself cannot run: the failure must
	// surface as operational, never as "not an available commit".
	installOneShotGitShim(t)
	_, err := RemapRevisions(RemapOptions{Root: f.root, RepoRoot: f.root, Plan: "Demo", Mapping: mapping, DryRun: true})
	if err == nil {
		t.Fatal("expected an error when the revision query cannot run")
	}
	if !errors.Is(err, vcs.ErrOperational) {
		t.Fatalf("query failure must wrap vcs.ErrOperational: %v", err)
	}
	if strings.Contains(err.Error(), "is not an available commit") {
		t.Fatalf("an operational query failure must not be reported as an unavailable commit: %v", err)
	}
}
