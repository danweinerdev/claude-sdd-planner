package rules

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// noGit points PATH at an empty directory for the test's duration; the
// tests here run serially because t.Setenv forbids t.Parallel.
func noGit(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SDD_VCS_DISABLE_P4", "1")
}

// FR-16 / DD-10: a rule that consults the repository and ignores the error
// cannot turn an operational failure into findings. The evaluator records
// the failure at the adapter, aborts before the next callback, and returns
// no partial findings; the unchecked entry points surface it as an
// invalidating operational diagnostic rather than a clean result.
func TestIgnoredRepoErrorAbortsEvaluation(t *testing.T) {
	_, root := materializeRoot(t, map[string]string{"Research/clean.md": researchClean})
	noGit(t)

	var sloppyRan, laterRan bool
	sloppy := &Rule{
		Code: "SDD997", Severity: Error,
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			sloppyRan = true
			repo := r.Repo(r.Dir)
			if _, err := repo.Head(); err != nil {
				// The classic bug: treat any error as "no history here".
				emit(Diagnostic{Code: "SDD997", Severity: Error, Path: "Research/clean.md", Line: 1, Message: "no history"})
				return
			}
		},
	}
	later := &Rule{
		Code: "SDD998", Severity: Error,
		CheckRoot: func(*Root, func(Diagnostic)) { laterRan = true },
		Check:     func(*Artifact, func(Diagnostic)) { laterRan = true },
	}
	rules := []*Rule{sloppy, later, ruleByCode(t, "SDD020")}

	for _, mode := range []struct {
		name string
		run  func(*Root, []*Rule) ([]Diagnostic, error)
	}{{"strict", runWith}, {"reporting", runWithWaiversWith}} {
		t.Run(mode.name, func(t *testing.T) {
			sloppyRan, laterRan = false, false
			diags, err := mode.run(freshRoot(t, root.Dir), rules)
			if !errors.Is(err, vcs.ErrOperational) {
				t.Fatalf("err = %v, want vcs.ErrOperational", err)
			}
			if len(diags) != 0 {
				t.Errorf("partial findings leaked past an operational failure: %v", codesOf(diags))
			}
			if !sloppyRan {
				t.Fatal("the consulting rule never ran; the test proves nothing")
			}
			if laterRan {
				t.Error("evaluation continued into the next callback after the failure was recorded")
			}
		})
	}

	// Unchecked entry points: the registry sweep consults the repository
	// through the append-only family, so a root with no runnable git must
	// come back as one invalidating operational diagnostic, never as clean.
	for _, mode := range []struct {
		name string
		run  func(*Root) []Diagnostic
	}{{"Run", Run}, {"RunWithWaivers", RunWithWaivers}} {
		t.Run(mode.name, func(t *testing.T) {
			diags := mode.run(freshRoot(t, root.Dir))
			if len(diags) != 1 || diags[0].Severity != Operational || !diags[0].Severity.Invalidating() {
				t.Fatalf("%s without git = %+v, want exactly one invalidating Operational diagnostic", mode.name, diags)
			}
		})
	}

	// Positive control: with git available the same local rules produce the
	// sloppy rule's finding as an ordinary result.
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+gitDir(t))
	sloppyRan, laterRan = false, false
	diags, err := runWith(freshRoot(t, root.Dir), rules)
	if err != nil {
		t.Fatalf("control with git: %v", err)
	}
	if !laterRan || len(diags) == 0 {
		t.Errorf("control with git: later=%v diags=%v", laterRan, codesOf(diags))
	}
}

// gitDir returns the directory holding the git executable found before the
// test scrubbed PATH (captured at package init, when PATH was intact).
func gitDir(t *testing.T) string {
	t.Helper()
	if gitExe == "" {
		t.Skip("git not found on the original PATH")
	}
	return filepath.Dir(gitExe)
}

var gitExe, _ = exec.LookPath("git")

// FR-16: retirement verification uses checked detection; an unrunnable git
// is reported as operational, not as "no Git repository contains the plan".
func TestRetirementDetectionFailureIsOperational(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	noGit(t)
	src := model.RetirementSource{VCS: "git", Revision: "0123456789abcdef0123456789abcdef01234567", SourceID: "x", Path: "Plans/P/README.md"}
	_, err := VerifyRetirementSource(dir, src)
	if !errors.Is(err, vcs.ErrOperational) {
		t.Fatalf("VerifyRetirementSource without git = %v, want vcs.ErrOperational", err)
	}
}

// fakeP4Repo is a Perforce-kind adapter whose RevisionExists answer is
// scripted, so verifyCleanP4Identity can be exercised without a live p4
// server. Every other operation reports ErrUnsupported via the embedded
// Unavailable, which this rule never calls.
type fakeP4Repo struct {
	vcs.Unavailable
	existsErr error
}

func (f fakeP4Repo) Kind() vcs.Kind { return vcs.Perforce }

func (f fakeP4Repo) RevisionExists(string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return true, nil
}

// TestP4IdentityQueryFailureIsOperational: a p4 identity query that fails
// operationally (the server or client could not be consulted) must be
// recorded on the evaluation's collector, not reported as "not a submitted
// changelist" — that diagnostic is reserved for a query that actually ran
// and came back with ErrNotFound.
func TestP4IdentityQueryFailureIsOperational(t *testing.T) {
	rel := "Plans/P/README.md"

	t.Run("operational failure is recorded, not reported as absence", func(t *testing.T) {
		_, root := materializeRoot(t, map[string]string{rel: researchClean})
		fake := fakeP4Repo{existsErr: fmt.Errorf("%w: p4 describe: deadline exceeded", vcs.ErrOperational)}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		a := root.ByPath[rel]
		var diags []Diagnostic
		verifyCleanP4Identity(root, a, "123", "Revision / checkpoint", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("operational failure surfaced as SDD072: %+v", d)
			}
		}
	})

	t.Run("ErrNotFound still produces the diagnostic", func(t *testing.T) {
		_, root := materializeRoot(t, map[string]string{rel: researchClean})
		fake := fakeP4Repo{existsErr: fmt.Errorf("%w: 123", vcs.ErrNotFound)}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		a := root.ByPath[rel]
		var diags []Diagnostic
		verifyCleanP4Identity(root, a, "123", "Revision / checkpoint", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD072" {
				found = true
			}
		}
		if !found {
			t.Errorf("ErrNotFound diags = %v, want an SDD072", codesOf(diags))
		}
	})
}
