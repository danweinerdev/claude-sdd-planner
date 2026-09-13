package rules

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// fakeGitRepo is a Git-kind adapter whose RevisionExists/IsAncestor answers
// are scripted, so the identity checks below can be exercised without a
// live git binary. Every other operation reports ErrUnsupported via the
// embedded Unavailable.
type fakeGitRepo struct {
	vcs.Unavailable
	existsOK   bool
	existsErr  error
	ancestrOK  bool
	ancestrErr error
}

func (f fakeGitRepo) Kind() vcs.Kind { return vcs.Git }

func (f fakeGitRepo) RevisionExists(string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.existsOK, nil
}

func (f fakeGitRepo) IsAncestor(string, string) (bool, error) {
	if f.ancestrErr != nil {
		return false, f.ancestrErr
	}
	return f.ancestrOK, nil
}

// TestIdentityQueryFailuresAreOperational is the git-flavoured counterpart of
// TestP4IdentityQueryFailureIsOperational: every identity check in
// internal/rules that can emit an absence diagnostic must emit it only when
// the underlying query actually ran and came back negative (or, for the p4
// check, syntactically unsupported) — never when the query itself failed
// operationally. On an operational failure the collector must have recorded
// it and no absence diagnostic may be emitted.
func TestIdentityQueryFailuresAreOperational(t *testing.T) {
	rel := "Plans/P/README.md"
	opErr := fmt.Errorf("%w: git rev-parse: deadline exceeded", vcs.ErrOperational)

	t.Run("verifyCleanP4Identity ErrUnsupported still emits", func(t *testing.T) {
		_, root := materializeRoot(t, map[string]string{rel: researchClean})
		fake := fakeP4Repo{existsErr: fmt.Errorf("%w: not a changelist number: 123", vcs.ErrUnsupported)}
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
			t.Errorf("ErrUnsupported diags = %v, want an SDD072", codesOf(diags))
		}
	})

	t.Run("verifyCleanGitIdentity RevisionExists", func(t *testing.T) {
		_, root := materializeRoot(t, map[string]string{rel: researchClean})
		fake := fakeGitRepo{existsErr: opErr}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		a := root.ByPath[rel]
		var diags []Diagnostic
		verifyCleanGitIdentity(root, a, "0123456789abcdef0123456789abcdef01234567", "Revision / checkpoint", 1, false,
			func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("operational failure surfaced as SDD072: %+v", d)
			}
		}
	})

	t.Run("verifyCleanGitIdentity RevisionExists ErrNotFound still emits", func(t *testing.T) {
		_, root := materializeRoot(t, map[string]string{rel: researchClean})
		fake := fakeGitRepo{existsErr: fmt.Errorf("%w: rev", vcs.ErrNotFound)}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		a := root.ByPath[rel]
		var diags []Diagnostic
		verifyCleanGitIdentity(root, a, "0123456789abcdef0123456789abcdef01234567", "Revision / checkpoint", 1, false,
			func(d Diagnostic) { diags = append(diags, d) })

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

	t.Run("verifyCleanGitIdentity IsAncestor", func(t *testing.T) {
		_, root := materializeRoot(t, map[string]string{rel: researchClean})
		fake := fakeGitRepo{existsOK: true, ancestrErr: opErr}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		a := root.ByPath[rel]
		var diags []Diagnostic
		verifyCleanGitIdentity(root, a, "0123456789abcdef0123456789abcdef01234567", "Revision / checkpoint", 1, true,
			func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("operational failure surfaced as SDD072: %+v", d)
			}
		}
	})

	t.Run("validGitTaskReviewIdentity RevisionExists", func(t *testing.T) {
		_, root := materializeRoot(t, map[string]string{rel: researchClean})
		fake := fakeGitRepo{existsErr: opErr}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		a := root.ByPath[rel]
		rev := "0123456789abcdef0123456789abcdef01234567"
		var diags []Diagnostic
		validGitTaskReviewIdentity(root, a, "Reviewed", rev, rev, rev, 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD169" {
				t.Errorf("operational failure surfaced as SDD169: %+v", d)
			}
		}
	})

	t.Run("verifyPhaseReviewIdentity RevisionExists", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		fake := fakeGitRepo{existsErr: opErr}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found at Plans/Sample/01-One.md")
		}
		// verifyPhaseReviewIdentity needs a full-hex range to reach the
		// RevisionExists loop; its endpoint must match the checkpoint the
		// body records.
		checkpoint := "1123456789abcdef0123456789abcdef01234567"
		frozenHex := "0123456789abcdef0123456789abcdef01234567.." + checkpoint
		ctx := phaseGateContext{Phase: phase, Body: "- Revision / checkpoint: " + checkpoint + "\n", Line: 1}
		tasks := []taskIdentity{{ID: "1.1", Revision: checkpoint}}
		var diags []Diagnostic
		verifyPhaseReviewIdentity(root, ctx, frozenHex, tasks, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyPhaseReviewPlanningRevision RevisionExists", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		fake := fakeGitRepo{existsErr: opErr}
		root.repoCache = map[string]vcs.Repo{root.Dir: recordingRepo{Repo: fake, root: root}}

		ctxs := completePhasesWithEvidence(root)
		if len(ctxs) != 1 {
			t.Fatalf("completePhasesWithEvidence = %d contexts, want 1", len(ctxs))
		}
		review := root.ByPath["Retro/phase-review.md"]
		if review == nil {
			t.Fatal("fixture review not found at Retro/phase-review.md")
		}
		var diags []Diagnostic
		verifyPhaseReviewPlanningRevision(root, ctxs[0], review, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD174" {
				t.Errorf("operational failure surfaced as SDD174: %+v", d)
			}
		}
	})

	t.Run("verifyGitPlanPhaseCheckpoints RevisionExists", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		_, root := materializeRoot(t, files)
		fake := fakeGitRepo{existsErr: opErr}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		plan := root.ByPath["Plans/Sample/README.md"]
		if plan == nil {
			t.Fatal("fixture plan not found at Plans/Sample/README.md")
		}
		body := "- VCS: git\n- Revision / checkpoint: 0123456789abcdef0123456789abcdef01234567\n"
		var diags []Diagnostic
		verifyGitPlanPhaseCheckpoints(root, plan, body, 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD175" {
				t.Errorf("operational failure surfaced as SDD175: %+v", d)
			}
		}
	})
}

// installOneShotGitShim puts a git shim ahead of the real git on PATH that
// answers exactly one invocation (delegating to the real binary) and then
// removes its own execute bit, so every subsequent "git" invocation fails
// to start at all. This reaches a genuine procexec-level operational
// failure — not merely a nonzero git exit — for a query issued after VCS
// detection's own git call has already succeeded. Ported from
// internal/graph/ops/operational_test.go's helper of the same name.
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

// TestRetirementProbeFailureIsOperational: VerifyRetirementSource's
// post-detection RevisionExists probe must wrap a failure so it survives
// errors.Is(err, vcs.ErrOperational), matching the detection-failure case
// TestRetirementDetectionFailureIsOperational already covers. The one-shot
// shim lets detection's own git call succeed and only the following
// RevisionExists call fail to start, so this actually exercises the probe
// — not detection — unlike scrubbing PATH outright.
func TestRetirementProbeFailureIsOperational(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "commit", "--allow-empty", "-q", "-m", "base").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	rev := strings.TrimSpace(string(head))

	installOneShotGitShim(t)

	src := model.RetirementSource{VCS: "git", Revision: rev, SourceID: "x", Path: "Plans/P/README.md"}
	_, err = VerifyRetirementSource(dir, src)
	if err == nil {
		t.Fatal("VerifyRetirementSource with the RevisionExists probe unrunnable = nil error, want one")
	}
	if !errors.Is(err, vcs.ErrOperational) {
		t.Fatalf("VerifyRetirementSource with the RevisionExists probe unrunnable = %v, want vcs.ErrOperational", err)
	}
	if strings.Contains(err.Error(), "may need fetching") {
		t.Fatalf("an operational query failure must not be reported as a commit absent from local history: %v", err)
	}
}
