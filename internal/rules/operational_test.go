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

// fakeGitRepo is a Git-kind adapter whose query answers are scripted, so the
// identity and post-review-state checks below can be exercised without a
// live git binary. Every other operation reports ErrUnsupported via the
// embedded Unavailable.
type fakeGitRepo struct {
	vcs.Unavailable
	existsOK   bool
	existsErr  error
	ancestrOK  bool
	ancestrErr error

	cleanOK  bool
	cleanErr error
	headVal  string
	headErr  error

	revisionsAfterVal []string
	revisionsAfterErr error

	changedPathsVal map[string][]string
	changedPathsErr error

	fileAtVal map[string][]byte
	fileAtErr error
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

func (f fakeGitRepo) Clean() (bool, []string, error) {
	if f.cleanErr != nil {
		return false, nil, f.cleanErr
	}
	return f.cleanOK, nil, nil
}

func (f fakeGitRepo) Head() (string, error) {
	if f.headErr != nil {
		return "", f.headErr
	}
	return f.headVal, nil
}

func (f fakeGitRepo) RevisionsAfter(string) ([]string, error) {
	if f.revisionsAfterErr != nil {
		return nil, f.revisionsAfterErr
	}
	return f.revisionsAfterVal, nil
}

func (f fakeGitRepo) ChangedPaths(rev string) ([]string, error) {
	if f.changedPathsErr != nil {
		return nil, f.changedPathsErr
	}
	return f.changedPathsVal[rev], nil
}

func (f fakeGitRepo) FileAt(rev, rel string) ([]byte, error) {
	if f.fileAtErr != nil {
		return nil, f.fileAtErr
	}
	return f.fileAtVal[rev+":"+rel], nil
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

// TestRepoQueryFailuresAreOperational is the SDD173/evidence-committed
// counterpart of TestIdentityQueryFailuresAreOperational: every repository
// query a rule callback makes that can emit or suppress a diagnostic must
// emit an absence/state diagnostic only when the query actually ran and
// answered negatively, and must stay silent — with the failure already
// recorded on the collector — when the query itself failed operationally.
func TestRepoQueryFailuresAreOperational(t *testing.T) {
	opErr := fmt.Errorf("%w: git: deadline exceeded", vcs.ErrOperational)
	endpoint := "1123456789abcdef0123456789abcdef01234567"
	base := "0123456789abcdef0123456789abcdef01234567"

	newCtx := func(root *Root) (phaseGateContext, *Artifact) {
		phase := root.ByPath["Plans/Sample/01-One.md"]
		review := root.ByPath["Retro/phase-review.md"]
		if phase == nil || review == nil {
			t.Fatal("fixture phase or review not found")
		}
		ctx := phaseGateContext{
			Phase: phase,
			Body:  "- Final aligned review: Retro/phase-review.md; frozen: " + base + ".." + endpoint + "\n",
			Line:  1,
		}
		return ctx, review
	}

	t.Run("verifyGitPhasePostReviewState Clean", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{cleanErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyGitPhasePostReviewState Clean negative control still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{cleanOK: false, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("dirty-worktree diags = %v, want an SDD173", codesOf(diags))
		}
	})

	t.Run("verifyGitPhasePostReviewState Head", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{cleanOK: true, headErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyGitPhasePostReviewState Head negative control still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{cleanOK: true, headVal: "", Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("no-HEAD diags = %v, want an SDD173", codesOf(diags))
		}
	})

	t.Run("verifyGitPhasePostReviewState IsAncestor", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{cleanOK: true, headVal: "current-head", ancestrErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyGitPhasePostReviewState IsAncestor negative control still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{cleanOK: true, headVal: "current-head", ancestrOK: false, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("non-ancestor diags = %v, want an SDD173", codesOf(diags))
		}
	})

	t.Run("verifyGitPhasePostReviewState RevisionsAfter", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{cleanOK: true, headVal: "current-head", ancestrOK: true, revisionsAfterErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyGitPhasePostReviewState RevisionsAfter negative control (inspection failure) still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{
			cleanOK: true, headVal: "current-head", ancestrOK: true,
			revisionsAfterErr: fmt.Errorf("%w: cannot inspect", vcs.ErrNotFound),
			Unavailable:       vcs.Unavailable{Dir: root.Dir},
		}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("RevisionsAfter-failure diags = %v, want an SDD173", codesOf(diags))
		}
	})

	t.Run("verifyGitPhasePostReviewState ChangedPaths", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{
			cleanOK: true, headVal: "current-head", ancestrOK: true,
			revisionsAfterVal: []string{"commit-1"},
			changedPathsErr:   opErr,
			Unavailable:       vcs.Unavailable{Dir: root.Dir},
		}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyGitPhasePostReviewState ChangedPaths negative control (inspection failure) still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		ctx, review := newCtx(root)
		fake := fakeGitRepo{
			cleanOK: true, headVal: "current-head", ancestrOK: true,
			revisionsAfterVal: []string{"commit-1"},
			changedPathsErr:   fmt.Errorf("%w: cannot inspect", vcs.ErrNotFound),
			Unavailable:       vcs.Unavailable{Dir: root.Dir},
		}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("ChangedPaths-failure diags = %v, want an SDD173", codesOf(diags))
		}
	})

	// gateLoopFiles is phaseGateRangeFiles rebuilt with a distinct, non-
	// degenerate base/endpoint range and a matching `Revision / checkpoint`
	// line, so verifyPhaseReviewIdentity's own checks pass cleanly (every
	// identity here is answered by the fake, never by real git) and the
	// CheckRoot loop reaches the RevisionExists gate that decides whether to
	// run verifyGitPhasePostReviewState.
	gateBase := "2223456789abcdef0123456789abcdef01234567"
	gateLoopFiles := func() map[string]string {
		rangeID := gateBase + ".." + fixtureBaseCommit
		files := map[string]string{
			"code.txt":              "code\n",
			"Retro/phase-review.md": phaseGateReview(rangeID, true),
		}
		files["Plans/Sample/01-One.md"] = replaceFirst(
			replaceFirst(
				checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
				"## Phase Completion Evidence\n\nPending — not complete.",
				"## Phase Completion Evidence\n\n- Revision / checkpoint: "+fixtureBaseCommit+
					"\n- Final aligned review: Retro/phase-review.md; frozen: "+rangeID+"\n"),
			"", "")
		return files
	}

	t.Run("SDD173 RevisionExists gate loop", func(t *testing.T) {
		files := withPlanReadme(gateLoopFiles())
		_, root := materializeRoot(t, files)
		// existsOK=true lets verifyPhaseReviewIdentity's own RevisionExists
		// calls pass cleanly, so only the gate loop's later RevisionExists
		// call (which decides whether to run the post-review state gate) is
		// exercised: it must record the operational failure and skip the
		// gate silently rather than treat "unanswered" as "missing".
		fake := &revisionExistsThenFails{fakeGitRepo: fakeGitRepo{existsOK: true, ancestrOK: true, Unavailable: vcs.Unavailable{Dir: root.Dir}}, failAfter: 2, err: opErr}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		rule := ruleByCode(t, "SDD173")
		rule.CheckRoot(root, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("SDD173 RevisionExists gate loop negative control (genuinely absent skips the gate too, no diagnostic either way)", func(t *testing.T) {
		files := withPlanReadme(gateLoopFiles())
		_, root := materializeRoot(t, files)
		fake := &revisionExistsThenFails{
			fakeGitRepo: fakeGitRepo{existsOK: true, ancestrOK: true, Unavailable: vcs.Unavailable{Dir: root.Dir}},
			failAfter:   2, err: fmt.Errorf("%w: rev", vcs.ErrNotFound),
		}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		rule := ruleByCode(t, "SDD173")
		rule.CheckRoot(root, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("a genuinely absent gate-loop identity must skip the post-review gate silently too (SDD172 owns range-identity absence), not emit SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyPhaseReviewIdentity base/endpoint IsAncestor", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		checkpoint := "1123456789abcdef0123456789abcdef01234567"
		frozenHex := base + ".." + checkpoint
		fake := fakeGitRepo{existsOK: true, ancestrErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}
		ctx := phaseGateContext{Phase: phase, Body: "- Revision / checkpoint: " + checkpoint + "\n", Line: 1}

		var diags []Diagnostic
		verifyPhaseReviewIdentity(root, ctx, frozenHex, nil, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyPhaseReviewIdentity base/endpoint IsAncestor negative control still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		checkpoint := "1123456789abcdef0123456789abcdef01234567"
		frozenHex := base + ".." + checkpoint
		fake := fakeGitRepo{existsOK: true, ancestrOK: false, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}
		ctx := phaseGateContext{Phase: phase, Body: "- Revision / checkpoint: " + checkpoint + "\n", Line: 1}

		var diags []Diagnostic
		verifyPhaseReviewIdentity(root, ctx, frozenHex, nil, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("non-ancestor diags = %v, want an SDD173", codesOf(diags))
		}
	})

	t.Run("verifyPhaseReviewIdentity task-vs-endpoint IsAncestor", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		checkpoint := "1123456789abcdef0123456789abcdef01234567"
		frozenHex := base + ".." + checkpoint
		fake := fakeGitRepo{existsOK: true, ancestrErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}
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

	t.Run("verifyPhaseReviewIdentity task-vs-endpoint IsAncestor negative control still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		checkpoint := "1123456789abcdef0123456789abcdef01234567"
		frozenHex := base + ".." + checkpoint
		fake := fakeGitRepo{existsOK: true, ancestrOK: false, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}
		ctx := phaseGateContext{Phase: phase, Body: "- Revision / checkpoint: " + checkpoint + "\n", Line: 1}
		tasks := []taskIdentity{{ID: "1.1", Revision: checkpoint}}

		var diags []Diagnostic
		verifyPhaseReviewIdentity(root, ctx, frozenHex, tasks, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("non-ancestor diags = %v, want an SDD173", codesOf(diags))
		}
	})

	t.Run("verifyPhaseReviewIdentity task-vs-base IsAncestor", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		checkpoint := "1123456789abcdef0123456789abcdef01234567"
		frozenHex := base + ".." + checkpoint
		// ancestrOK=true satisfies the task-vs-endpoint check above it
		// (line 926, an "is descendant" gate that must pass to reach the
		// task-vs-base check); ancestrErr then fires on every IsAncestor
		// call, including this one, which is the one under test.
		fake := fakeGitRepo{existsOK: true, ancestrOK: true, ancestrErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}
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

	t.Run("verifyPhaseReviewIdentity task-vs-base IsAncestor negative control (task at range base) still emits", func(t *testing.T) {
		_, root := materializeRoot(t, withPlanReadme(phaseGateFiles(true, true)))
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		checkpoint := "1123456789abcdef0123456789abcdef01234567"
		frozenHex := base + ".." + checkpoint
		// The task revision equals the range base itself: task-vs-endpoint
		// IsAncestor(base, endpoint) must answer true (the base is always
		// an ancestor of the endpoint in a forward range), and
		// task-vs-base IsAncestor(base, base) must also answer true,
		// which is the genuine "at or before the range base" absence this
		// site's diagnostic exists to catch.
		tasks := []taskIdentity{{ID: "1.1", Revision: base}}
		fake := fakeGitRepo{existsOK: true, ancestrOK: true, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}
		ctx := phaseGateContext{Phase: phase, Body: "- Revision / checkpoint: " + checkpoint + "\n", Line: 1}

		var diags []Diagnostic
		verifyPhaseReviewIdentity(root, ctx, frozenHex, tasks, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("task-at-base diags = %v, want an SDD173", codesOf(diags))
		}
	})

	// planWithCompletePhase is phaseGateFiles with a plan README whose
	// `phases:` names the completed phase doc, and a phase evidence VCS/
	// checkpoint line added, so verifyGitPlanPhaseCheckpoints's per-phase
	// loop actually iterates and reaches the phase/plan IsAncestor call
	// instead of stopping at the (otherwise empty) `phases: []` in
	// validPlan or the missing per-phase VCS/checkpoint evidence.
	phaseGateCheckpoint := "1123456789abcdef0123456789abcdef01234567"
	planWithCompletePhase := func() map[string]string {
		files := phaseGateFiles(true, true)
		files["Plans/Sample/README.md"] = planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: complete
    doc: 01-One.md
`)
		files["Plans/Sample/01-One.md"] = strings.Replace(files["Plans/Sample/01-One.md"],
			"## Phase Completion Evidence\n\n- Final aligned review:",
			"## Phase Completion Evidence\n\n- VCS: git\n- Revision / checkpoint: "+phaseGateCheckpoint+"\n- Final aligned review:",
			1)
		return files
	}

	t.Run("verifyGitPlanPhaseCheckpoints phase/plan IsAncestor", func(t *testing.T) {
		files := planWithCompletePhase()
		_, root := materializeRoot(t, files)
		plan := root.ByPath["Plans/Sample/README.md"]
		if plan == nil {
			t.Fatal("fixture plan not found")
		}
		planCheckpoint := "0123456789abcdef0123456789abcdef01234567"
		body := "- VCS: git\n- Revision / checkpoint: " + planCheckpoint + "\n"
		fake := fakeGitRepo{existsOK: true, ancestrErr: opErr, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

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

	t.Run("verifyGitPlanPhaseCheckpoints phase/plan IsAncestor negative control still emits", func(t *testing.T) {
		files := planWithCompletePhase()
		_, root := materializeRoot(t, files)
		plan := root.ByPath["Plans/Sample/README.md"]
		if plan == nil {
			t.Fatal("fixture plan not found")
		}
		planCheckpoint := "0123456789abcdef0123456789abcdef01234567"
		body := "- VCS: git\n- Revision / checkpoint: " + planCheckpoint + "\n"
		fake := fakeGitRepo{existsOK: true, ancestrOK: false, Unavailable: vcs.Unavailable{Dir: root.Dir}}
		root.repoCache = map[string]vcs.Repo{root.RepoRoot: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPlanPhaseCheckpoints(root, plan, body, 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD175" {
				found = true
			}
		}
		if !found {
			t.Errorf("non-ancestor diags = %v, want an SDD175", codesOf(diags))
		}
	})

	t.Run("verifyGitEvidenceCommitted own FileAt", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		fake := fakeGitRepo{fileAtErr: opErr, Unavailable: vcs.Unavailable{Dir: dir}}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitEvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("operational failure surfaced as SDD072: %+v", d)
			}
		}
	})

	t.Run("verifyGitEvidenceCommitted own FileAt negative control still emits", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		fake := fakeGitRepo{fileAtErr: fmt.Errorf("%w: HEAD:x", vcs.ErrNotFound), Unavailable: vcs.Unavailable{Dir: dir}}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitEvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

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
			t.Errorf("not-committed diags = %v, want an SDD072", codesOf(diags))
		}
	})

	t.Run("verifyGitEvidenceCommitted plan FileAt", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeGitRepo{
			fileAtVal: map[string][]byte{"HEAD:Plans/Sample/01-One.md": committedPhase},
			// The plan's own FileAt (planCommitted) is scripted to fail
			// operationally by returning a distinct error only for that key
			// via a wrapping repo below.
			Unavailable: vcs.Unavailable{Dir: dir},
		}
		wrapped := planFileAtFails{fakeGitRepo: fake, planKey: "HEAD:Plans/Sample/README.md", err: opErr}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: wrapped, root: root}}

		var diags []Diagnostic
		verifyGitEvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("operational failure surfaced as SDD072: %+v", d)
			}
		}
	})

	t.Run("verifyGitEvidenceCommitted plan FileAt negative control still emits", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeGitRepo{
			fileAtVal:   map[string][]byte{"HEAD:Plans/Sample/01-One.md": committedPhase},
			Unavailable: vcs.Unavailable{Dir: dir},
		}
		wrapped := planFileAtFails{fakeGitRepo: fake, planKey: "HEAD:Plans/Sample/README.md", err: fmt.Errorf("%w: HEAD:Plans/Sample/README.md", vcs.ErrNotFound)}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: wrapped, root: root}}

		var diags []Diagnostic
		verifyGitEvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

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
			t.Errorf("plan-absent diags = %v, want an SDD072", codesOf(diags))
		}
	})

	t.Run("verifyP4EvidenceCommitted own FileAt", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		_, root := materializeRoot(t, files)
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		fake := fakeP4RepoFileAt{fileAtErr: opErr, dir: root.Dir}
		root.repoCache = map[string]vcs.Repo{root.Dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyP4EvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("operational failure surfaced as SDD072: %+v", d)
			}
		}
	})

	t.Run("verifyP4EvidenceCommitted own FileAt negative control still emits", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		_, root := materializeRoot(t, files)
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		fake := fakeP4RepoFileAt{fileAtErr: fmt.Errorf("%w: have:x", vcs.ErrNotFound), dir: root.Dir}
		root.repoCache = map[string]vcs.Repo{root.Dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyP4EvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

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
			t.Errorf("not-submitted diags = %v, want an SDD072", codesOf(diags))
		}
	})

	t.Run("verifyP4EvidenceCommitted plan FileAt", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		_, root := materializeRoot(t, files)
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeP4RepoFileAt{
			fileAtVal: map[string][]byte{"have:Plans/Sample/01-One.md": committedPhase},
			dir:       root.Dir,
			planKey:   "have:Plans/Sample/README.md",
			planErr:   opErr,
		}
		root.repoCache = map[string]vcs.Repo{root.Dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyP4EvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("operational failure surfaced as SDD072: %+v", d)
			}
		}
	})

	t.Run("verifyP4EvidenceCommitted plan FileAt negative control still emits", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		_, root := materializeRoot(t, files)
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeP4RepoFileAt{
			fileAtVal: map[string][]byte{"have:Plans/Sample/01-One.md": committedPhase},
			dir:       root.Dir,
			planKey:   "have:Plans/Sample/README.md",
			planErr:   fmt.Errorf("%w: have:Plans/Sample/README.md", vcs.ErrNotFound),
		}
		root.repoCache = map[string]vcs.Repo{root.Dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyP4EvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

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
			t.Errorf("plan-absent diags = %v, want an SDD072", codesOf(diags))
		}
	})
}

// revisionExistsThenFails wraps fakeGitRepo so the first failAfter
// RevisionExists calls answer from the embedded fake and every call after
// that returns err instead — a pointer receiver because the fake is shared
// across every RevisionExists call the rule under test makes, and the count
// must persist across those calls.
type revisionExistsThenFails struct {
	fakeGitRepo
	failAfter int
	err       error
	calls     int
}

func (f *revisionExistsThenFails) RevisionExists(rev string) (bool, error) {
	f.calls++
	if f.calls > f.failAfter {
		return false, f.err
	}
	return f.fakeGitRepo.RevisionExists(rev)
}

// planFileAtFails wraps fakeGitRepo so a FileAt call for planKey fails with
// err while every other FileAt call answers from fakeGitRepo.fileAtVal, so
// the artifact's own FileAt (verifyGitEvidenceCommitted's first call) can
// succeed while its planCommitted closure's FileAt (the second call) is the
// one exercised.
type planFileAtFails struct {
	fakeGitRepo
	planKey string
	err     error
}

func (p planFileAtFails) FileAt(rev, rel string) ([]byte, error) {
	key := rev + ":" + rel
	if key == p.planKey {
		return nil, p.err
	}
	return p.fakeGitRepo.FileAt(rev, rel)
}

// fakeP4RepoFileAt is a Perforce-kind adapter whose FileAt answer is
// scripted per key, so verifyP4EvidenceCommitted's two FileAt call sites
// (the artifact's own `have` copy and, via planCommitted, the plan
// README's `have` copy) can be exercised independently without a live p4
// server.
type fakeP4RepoFileAt struct {
	vcs.Unavailable
	dir       string
	fileAtVal map[string][]byte
	fileAtErr error
	planKey   string
	planErr   error
}

func (f fakeP4RepoFileAt) Kind() vcs.Kind { return vcs.Perforce }
func (f fakeP4RepoFileAt) Root() string   { return f.dir }

func (f fakeP4RepoFileAt) FileAt(rev, rel string) ([]byte, error) {
	key := rev + ":" + rel
	if key == f.planKey && f.planErr != nil {
		return nil, f.planErr
	}
	if f.fileAtErr != nil {
		return nil, f.fileAtErr
	}
	return f.fileAtVal[key], nil
}

// TestContentQueryFailuresAreOperational is the gate-test for review F-01's
// remaining sites: every content/comparison load that a rule callback makes
// (as opposed to an identity/existence probe, already covered above) must
// stay silent on an operational failure rather than turning "the query never
// answered" into a content diagnostic, EXCEPT verifyGitEvidenceCommitted's
// (and verifyP4EvidenceCommitted's) evidence-committed check, whose
// plan-lookup failure must suppress only the plan-state contribution to
// lifecycleComplete — a content diagnostic computed without needing the
// plan (e.g. genuinely unchecked acceptance criteria) must still emit.
func TestContentQueryFailuresAreOperational(t *testing.T) {
	opErr := fmt.Errorf("%w: git: deadline exceeded", vcs.ErrOperational)

	// (a) site 1: verifyGitPhaseReviewCommitted's own FileAt (phasereview.go:413,
	// SDD170) — the review-committed check.
	t.Run("verifyGitPhaseReviewCommitted FileAt", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		review := root.ByPath["Retro/phase-review.md"]
		if phase == nil || review == nil {
			t.Fatal("fixture phase or review not found")
		}
		fake := fakeGitRepo{fileAtErr: opErr, Unavailable: vcs.Unavailable{Dir: dir}}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		ctx := phaseGateContext{Phase: phase, Line: 1}
		var diags []Diagnostic
		verifyGitPhaseReviewCommitted(root, ctx, review, "r-2024-01-01-01", func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD170" {
				t.Errorf("operational failure surfaced as SDD170: %+v", d)
			}
		}
	})

	t.Run("verifyGitPhaseReviewCommitted FileAt negative control still emits", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		review := root.ByPath["Retro/phase-review.md"]
		if phase == nil || review == nil {
			t.Fatal("fixture phase or review not found")
		}
		fake := fakeGitRepo{fileAtErr: fmt.Errorf("%w: HEAD:x", vcs.ErrNotFound), Unavailable: vcs.Unavailable{Dir: dir}}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		ctx := phaseGateContext{Phase: phase, Line: 1}
		var diags []Diagnostic
		verifyGitPhaseReviewCommitted(root, ctx, review, "r-2024-01-01-01", func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD170" {
				found = true
			}
		}
		if !found {
			t.Errorf("not-committed diags = %v, want an SDD170", codesOf(diags))
		}
	})

	// (a) site 2: verifyGitPhasePostReviewState's two gitLifecycleNormalized
	// calls (phasereview.go:745-746, SDD173 "cannot compare canonical intent").
	newLifecycleCtx := func(root *Root) (phaseGateContext, *Artifact, string) {
		phase := root.ByPath["Plans/Sample/01-One.md"]
		review := root.ByPath["Retro/phase-review.md"]
		if phase == nil || review == nil {
			t.Fatal("fixture phase or review not found")
		}
		endpoint := fixtureBaseCommit
		return phaseGateContext{
			Phase: phase,
			Body:  "- Final aligned review: Retro/phase-review.md; frozen: " + endpoint + ".." + endpoint + "\n",
			Line:  1,
		}, review, endpoint
	}

	t.Run("verifyGitPhasePostReviewState gitLifecycleNormalized frozen", func(t *testing.T) {
		files := withPlanReadme(phaseGateRangeFiles())
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		ctx, review, endpoint := newLifecycleCtx(root)
		// current HEAD differs from endpoint but the only commit after it
		// touches nothing (no material change), so the intent-comparison
		// loop is reached instead of returning early at current==endpoint
		// or failing on a material change.
		fake := fakeGitRepo{
			cleanOK: true, headVal: "current-head", ancestrOK: true,
			revisionsAfterVal: []string{"commit-1"},
			changedPathsVal:   map[string][]string{"commit-1": nil},
			fileAtErr:         opErr,
			Unavailable:       vcs.Unavailable{Dir: dir},
		}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD173" {
				t.Errorf("operational failure surfaced as SDD173: %+v", d)
			}
		}
	})

	t.Run("verifyGitPhasePostReviewState gitLifecycleNormalized negative control (genuinely absent) still emits", func(t *testing.T) {
		files := withPlanReadme(phaseGateRangeFiles())
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		ctx, review, endpoint := newLifecycleCtx(root)
		fake := fakeGitRepo{
			cleanOK: true, headVal: "current-head", ancestrOK: true,
			revisionsAfterVal: []string{"commit-1"},
			changedPathsVal:   map[string][]string{"commit-1": nil},
			fileAtErr:         fmt.Errorf("%w: path absent at revision", vcs.ErrNotFound),
			Unavailable:       vcs.Unavailable{Dir: dir},
		}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyGitPhasePostReviewState(root, ctx, review, endpoint, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD173" {
				found = true
			}
		}
		if !found {
			t.Errorf("absent-path diags = %v, want an SDD173", codesOf(diags))
		}
	})

	// (a) site 4: verifyPhaseReviewPlanningRevision's gitLifecycleNormalized
	// calls (phasereview.go:1034, SDD174).
	t.Run("verifyPhaseReviewPlanningRevision gitLifecycleNormalized", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		ctxs := completePhasesWithEvidence(root)
		if len(ctxs) != 1 {
			t.Fatalf("completePhasesWithEvidence = %d contexts, want 1", len(ctxs))
		}
		review := root.ByPath["Retro/phase-review.md"]
		if review == nil {
			t.Fatal("fixture review not found at Retro/phase-review.md")
		}
		rev := "1111111111111111111111111111111111111111"
		fake := fakeGitRepo{existsOK: true, ancestrOK: true, fileAtErr: opErr, Unavailable: vcs.Unavailable{Dir: dir}}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyPhaseReviewPlanningRevision(root, ctxs[0], review, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational for planning revision %s", err, rev)
		}
		for _, d := range diags {
			if d.Code == "SDD174" {
				t.Errorf("operational failure surfaced as SDD174: %+v", d)
			}
		}
	})

	t.Run("verifyPhaseReviewPlanningRevision gitLifecycleNormalized negative control (genuinely absent) still emits", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		ctxs := completePhasesWithEvidence(root)
		if len(ctxs) != 1 {
			t.Fatalf("completePhasesWithEvidence = %d contexts, want 1", len(ctxs))
		}
		review := root.ByPath["Retro/phase-review.md"]
		if review == nil {
			t.Fatal("fixture review not found at Retro/phase-review.md")
		}
		fake := fakeGitRepo{existsOK: true, ancestrOK: true, fileAtErr: fmt.Errorf("%w: path absent at revision", vcs.ErrNotFound), Unavailable: vcs.Unavailable{Dir: dir}}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyPhaseReviewPlanningRevision(root, ctxs[0], review, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); err != nil {
			t.Fatalf("OperationalFailure() = %v, want nil", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD174" {
				found = true
			}
		}
		if !found {
			t.Errorf("absent-path diags = %v, want an SDD174", codesOf(diags))
		}
	})

	// (b) verifyGitEvidenceCommitted / verifyP4EvidenceCommitted: today the
	// `operational` flag blanket-suppresses every emit from
	// verifyCommittedLifecycle, not just the plan-state contribution to
	// lifecycleComplete. A phase with genuinely unchecked acceptance criteria
	// plus a transient plan FileAt failure must still emit the content SDD072
	// (computed without the plan); a phase whose ONLY problem is incomplete
	// plan state, combined with the same plan FileAt failure, must stay
	// silent.
	contentBrokenPhase := replaceFirst(
		phaseStatus("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
		"## Phase Completion Evidence\n\nPending — not complete.",
		"## Phase Completion Evidence\n\n- Final aligned review: Retro/phase-review.md; frozen: r-2024-01-01-01\n")

	t.Run("verifyGitEvidenceCommitted plan FileAt operational failure still emits the content diagnostic", func(t *testing.T) {
		files := withPlanReadme(map[string]string{
			"Plans/Sample/01-One.md": contentBrokenPhase,
			"Retro/phase-review.md":  phaseGateReview("r-2024-01-01-01", true),
		})
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeGitRepo{
			fileAtVal:   map[string][]byte{"HEAD:Plans/Sample/01-One.md": committedPhase},
			Unavailable: vcs.Unavailable{Dir: dir},
		}
		wrapped := planFileAtFails{fakeGitRepo: fake, planKey: "HEAD:Plans/Sample/README.md", err: opErr}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: wrapped, root: root}}

		var diags []Diagnostic
		verifyGitEvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD072" {
				found = true
			}
		}
		if !found {
			t.Errorf("unchecked-acceptance-criteria diags = %v, want an SDD072 even though the plan lookup failed operationally", codesOf(diags))
		}
	})

	t.Run("verifyGitEvidenceCommitted plan FileAt operational failure suppresses plan-only incompleteness", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		dir, root := materializeRoot(t, files)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeGitRepo{
			fileAtVal:   map[string][]byte{"HEAD:Plans/Sample/01-One.md": committedPhase},
			Unavailable: vcs.Unavailable{Dir: dir},
		}
		wrapped := planFileAtFails{fakeGitRepo: fake, planKey: "HEAD:Plans/Sample/README.md", err: opErr}
		root.repoCache = map[string]vcs.Repo{dir: recordingRepo{Repo: wrapped, root: root}}

		var diags []Diagnostic
		verifyGitEvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("plan-only incompleteness surfaced as SDD072 despite the plan lookup failing operationally: %+v", d)
			}
		}
	})

	t.Run("verifyP4EvidenceCommitted plan FileAt operational failure still emits the content diagnostic", func(t *testing.T) {
		files := withPlanReadme(map[string]string{
			"Plans/Sample/01-One.md": contentBrokenPhase,
			"Retro/phase-review.md":  phaseGateReview("r-2024-01-01-01", true),
		})
		_, root := materializeRoot(t, files)
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeP4RepoFileAt{
			fileAtVal: map[string][]byte{"have:Plans/Sample/01-One.md": committedPhase},
			dir:       root.Dir,
			planKey:   "have:Plans/Sample/README.md",
			planErr:   opErr,
		}
		root.repoCache = map[string]vcs.Repo{root.Dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyP4EvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		found := false
		for _, d := range diags {
			if d.Code == "SDD072" {
				found = true
			}
		}
		if !found {
			t.Errorf("unchecked-acceptance-criteria diags = %v, want an SDD072 even though the plan lookup failed operationally", codesOf(diags))
		}
	})

	t.Run("verifyP4EvidenceCommitted plan FileAt operational failure suppresses plan-only incompleteness", func(t *testing.T) {
		files := withPlanReadme(phaseGateFiles(true, true))
		_, root := materializeRoot(t, files)
		phase := root.ByPath["Plans/Sample/01-One.md"]
		if phase == nil {
			t.Fatal("fixture phase not found")
		}
		committedPhase := []byte(files["Plans/Sample/01-One.md"])
		fake := fakeP4RepoFileAt{
			fileAtVal: map[string][]byte{"have:Plans/Sample/01-One.md": committedPhase},
			dir:       root.Dir,
			planKey:   "have:Plans/Sample/README.md",
			planErr:   opErr,
		}
		root.repoCache = map[string]vcs.Repo{root.Dir: recordingRepo{Repo: fake, root: root}}

		var diags []Diagnostic
		verifyP4EvidenceCommitted(root, phase, "Phase Completion Evidence", "", 1, func(d Diagnostic) { diags = append(diags, d) })

		if err := root.OperationalFailure(); !errors.Is(err, vcs.ErrOperational) {
			t.Fatalf("OperationalFailure() = %v, want vcs.ErrOperational", err)
		}
		for _, d := range diags {
			if d.Code == "SDD072" {
				t.Errorf("plan-only incompleteness surfaced as SDD072 despite the plan lookup failing operationally: %+v", d)
			}
		}
	})
}
