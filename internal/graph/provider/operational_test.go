package provider

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// TestProviderDetectionFailureIsOperational (FR-16, AC-08, DD-10): when VCS
// detection cannot run, the workspace provider must refuse rather than fall
// through to the plain provider. A plain provider on a real git repository
// silently disables worktree isolation and digest-only provenance would be
// recorded for work that actually has a revision — an inability presented as
// a posture.
func TestProviderDetectionFailureIsOperational(t *testing.T) {
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("SDD_VCS_DISABLE_P4", "1")

	repoRoot := t.TempDir()
	if out, err := exec.Command(gitExe, "-C", repoRoot, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	planDir := filepath.Join(repoRoot, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Control: with git available, detection answers git.
	p, err := DetectChecked(repoRoot, planDir)
	if err != nil {
		t.Fatalf("control with git: %v", err)
	}
	if p.Kind() != "git" {
		t.Fatalf("control provider kind = %q, want git", p.Kind())
	}

	// git cannot run: detection is undecidable and must surface as such.
	t.Setenv("PATH", t.TempDir())
	p, err = DetectChecked(repoRoot, planDir)
	if err == nil {
		t.Fatalf("detection failure produced provider %v instead of an error", p)
	}
	if !errors.Is(err, vcs.ErrOperational) {
		t.Fatalf("detection failure must wrap vcs.ErrOperational; got %v", err)
	}
	if p != nil {
		t.Fatalf("an operational detection failure must return no provider; got %v (kind %q)", p, p.Kind())
	}

	// The unchecked entry point must not erase the failure into a plain
	// tree either: an Unavailable adapter is never a plain posture.
	if legacy := Detect(repoRoot, planDir); legacy.Kind() == "plain" {
		t.Errorf("Detect fell back to the plain provider when detection could not run")
	}
}

// TestExecRunnerOperationalCause (FR-16, DD-10): the provider's mutating
// runner goes through the bounded runner, so a missing executable is an
// operational failure with a named cause, while a command that ran and
// exited nonzero stays an ordinary error carrying its stderr.
func TestExecRunnerOperationalCause(t *testing.T) {
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	// Ran to completion, exited nonzero: an ordinary error, never operational.
	if out, err := execRunner(dir, gitExe, "rev-parse", "HEAD"); err == nil {
		t.Fatalf("expected a nonzero git exit in a non-repository; got %q", out)
	} else if errors.Is(err, vcs.ErrOperational) {
		t.Errorf("a nonzero exit must not be reported as operational: %v", err)
	} else if !strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
		t.Errorf("a nonzero exit must carry git's own stderr: %v", err)
	}

	// Could not run at all: operational, with the cause named.
	_, err = execRunner(dir, filepath.Join(t.TempDir(), "definitely-not-here"))
	if err == nil {
		t.Fatal("a missing executable must be an error")
	}
	if !errors.Is(err, vcs.ErrOperational) {
		t.Errorf("a missing executable must wrap vcs.ErrOperational: %v", err)
	}
	if !procexec.IsCause(err, procexec.CauseUnavailable) {
		t.Errorf("a missing executable must carry procexec's unavailable cause: %v", err)
	}
}
