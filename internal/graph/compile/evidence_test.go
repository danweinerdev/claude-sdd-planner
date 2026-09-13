package compile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// evidenceReview writes a minimal, resolved/frozen/Aligned phase review
// artifact into planDir/reviews, targeting phaseDoc (plan-dir-relative) with
// the given rev.
func evidenceReview(t *testing.T, planDir, plan, phaseDoc, name, rev string) {
	t.Helper()
	dir := filepath.Join(planDir, "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lane := func(l string) string {
		return "\n  - lane: " + l + "\n    result: PASS/Aligned\n    reviewed_identity: \"" + rev + "\"\n    evidence: Checked the diff for this scope directly.\n"
	}
	src := `---
title: "Phase review"
type: review
status: resolved
created: 2026-01-01
updated: 2026-01-01
tags: [review]
related: ["Plans/` + plan + `/` + phaseDoc + `"]
review_of: "Plans/` + plan + `/` + phaseDoc + `"
rev: "` + rev + `"
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "1111111111111111111111111111111111111111"
review_mode: independent
lane_results:` + lane("review_plan_drift") + lane("review_quality") + lane("review_spec_compliance") + lane("review_blind_spots") + `
findings: []
followups: []
tags: []
related: []
---

## Findings

None.

## Resolution Log

None.
`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func evidencePlanFixture(t *testing.T) (root, planDir string) {
	t.Helper()
	root = t.TempDir()
	planDir = filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\ntitle: \"P\"\ntype: plan\nstatus: active\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags: []\nrelated: []\nphases: []\n---\n\n# P\n\n## Overview\n\nKeep identity prose exactly.\n\n## Plan Completion Evidence\n\nPending — not complete.\n"
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

const evidenceRev = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func closedNode(id, phase string) model.Node {
	return model.Node{
		ID: id, Contract: "works", Phase: phase,
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1,
		Verification: &model.Verification{
			Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
			Provenance: &model.Provenance{Kind: "git", Revision: evidenceRev},
		},
	}
}

// TestPhaseEvidenceCoveredByReview covers the case the renderer can fully
// satisfy: a closed phase whose checkpoint is covered by an on-disk
// resolved/frozen/Aligned phase review naming this exact phase doc.
func TestPhaseEvidenceCoveredByReview(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
	evidenceReview(t, planDir, "P", "01-core.md", "review.md", evidenceRev)
	closed := map[string]bool{"work": true}

	if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(doc)

	for _, want := range []string{
		"- Verified: ",
		"- Repository: ",
		"- VCS: git\n",
		"- Revision / checkpoint: `" + evidenceRev + "`\n",
		"- Identity recheck: no recheck ran (no resolved target repository at render time)",
		"### Completed task identities",
		"- Final aligned review: `Plans/P/reviews/review.md`; frozen: " + evidenceRev + "\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("phase evidence missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Pending — not complete.") {
		t.Fatalf("phase evidence still pending:\n%s", body)
	}

	readme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	rbody := string(readme)
	if !strings.Contains(rbody, "### Completed phase identities") {
		t.Fatalf("README missing completed phase identities:\n%s", rbody)
	}
	if !strings.Contains(rbody, "- `1`: `"+evidenceRev+"`; review: `Plans/P/reviews/review.md`\n") {
		t.Fatalf("README missing phase identity entry:\n%s", rbody)
	}
}

// TestPhaseEvidenceWithoutCoveringReview covers the case the renderer cannot
// satisfy from rendered content alone: a closed phase with no on-disk review
// whose review_of names this exact phase doc. The renderer must still emit
// truthful derived evidence (never a fabricated Final aligned review line),
// and the README's plan-level evidence must stay untouched (Pending) since
// not every completed phase resolves a covering review.
func TestPhaseEvidenceWithoutCoveringReview(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
	closed := map[string]bool{"work": true}

	if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(doc)
	if strings.Contains(body, "Final aligned review") {
		t.Fatalf("phase evidence fabricated a Final aligned review with no covering review artifact:\n%s", body)
	}
	if !strings.Contains(body, "- Revision / checkpoint: `"+evidenceRev+"`\n") {
		t.Fatalf("phase evidence missing derived revision:\n%s", body)
	}

	readme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(readme), "### Completed phase identities") {
		t.Fatalf("README plan evidence completed without a covering review:\n%s", readme)
	}
}

// TestReadmePhaseEntryTitleMatchesDoc is SDD152's own invariant: the README
// phase entry's title tracks the phase doc's rendered (human) title, not a
// stale or raw phase-label spelling an earlier hand-authored README entry
// may have carried.
func TestReadmePhaseEntryTitleMatchesDoc(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	// Simulate a pre-existing README phase entry carrying the raw label
	// instead of the human title renderPhaseDoc/phaseTitle derive.
	src, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(src), "phases: []",
		"phases:\n  - id: 1\n    title: \"01-core\"\n    status: planned\n    doc: \"01-core.md\"", 1)
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "work", Contract: "works", Phase: "01-core",
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}}}
	// The phase doc does not exist yet on the first render (a brand new
	// phase never has a stale README entry to reconcile); a second render,
	// matching the real repro of a pre-existing generated phase doc against
	// a stale README-only title, is what exercises the title fix.
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := renderViews(root, "P", "", g, nil, nil); err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), `title: "Core"`) {
		t.Fatalf("README phase title was not reconciled to the doc's human title:\n%s", readme)
	}
}

// TestFindPhaseReviewReadFailuresAreOperational (review-execution
// 56815db-b F-01 item 1): only a not-exist on the reviews directory or an
// individual review file may mean "no covering review" — any other read
// failure is an inability to answer and must surface as an error from
// RenderViews rather than being folded into an empty result.
func TestFindPhaseReviewReadFailuresAreOperational(t *testing.T) {
	t.Run("not-exist still renders honest no-covering-review", func(t *testing.T) {
		root, planDir := evidencePlanFixture(t)
		g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
		closed := map[string]bool{"work": true}
		if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
			t.Fatalf("not-exist reviews dir must not be operational: %v", err)
		}
		doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(doc), "Final aligned review") {
			t.Fatalf("no covering review must not fabricate one:\n%s", doc)
		}
	})

	t.Run("ReadDir failure is operational", func(t *testing.T) {
		root, _ := evidencePlanFixture(t)
		g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
		closed := map[string]bool{"work": true}

		old := readReviewDir
		readReviewDir = func(name string) ([]os.DirEntry, error) {
			return nil, &os.PathError{Op: "readdir", Path: name, Err: os.ErrPermission}
		}
		defer func() { readReviewDir = old }()

		if _, err := renderViews(root, "P", "", g, nil, closed); err == nil {
			t.Fatal("expected an operational error from a ReadDir failure that is not not-exist")
		} else if os.IsNotExist(err) {
			t.Fatalf("ReadDir permission failure must not be reported as not-exist: %v", err)
		}
	})

	t.Run("ReadFile failure is operational", func(t *testing.T) {
		root, planDir := evidencePlanFixture(t)
		g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
		evidenceReview(t, planDir, "P", "01-core.md", "review.md", evidenceRev)
		closed := map[string]bool{"work": true}

		old := readReviewFile
		readReviewFile = func(name string) ([]byte, error) {
			return nil, &os.PathError{Op: "read", Path: name, Err: os.ErrPermission}
		}
		defer func() { readReviewFile = old }()

		if _, err := renderViews(root, "P", "", g, nil, closed); err == nil {
			t.Fatal("expected an operational error from a ReadFile failure that is not not-exist")
		} else if os.IsNotExist(err) {
			t.Fatalf("ReadFile permission failure must not be reported as not-exist: %v", err)
		}
	})
}

// realGitRepo builds a minimal git repository at dir with one commit and
// returns its HEAD revision, for probing a real identity recheck.
func realGitRepo(t *testing.T) (dir, rev string) {
	t.Helper()
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid"}, args...)
		out, err := exec.Command(gitExe, argv...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "base")
	return dir, run("rev-parse", "HEAD")
}

// stubRevisionRepo is a minimal vcs.Repo whose RevisionExists is the only
// method under test; every other method panics if called (identity
// rechecking has no business calling them).
type stubRevisionRepo struct {
	exists bool
	err    error
}

func (s stubRevisionRepo) Kind() vcs.Kind                  { return vcs.Git }
func (s stubRevisionRepo) Root() string                    { return "" }
func (s stubRevisionRepo) RevisionSyntaxValid(string) bool { return true }
func (s stubRevisionRepo) RevisionExists(string) (bool, error) {
	return s.exists, s.err
}
func (s stubRevisionRepo) Head() (string, error)                   { panic("unused") }
func (s stubRevisionRepo) IsAncestor(string, string) (bool, error) { panic("unused") }
func (s stubRevisionRepo) Parents(string) ([]string, error)        { panic("unused") }
func (s stubRevisionRepo) FileAt(string, string) ([]byte, error)   { panic("unused") }
func (s stubRevisionRepo) ChangedPaths(string) ([]string, error)   { panic("unused") }
func (s stubRevisionRepo) RevisionsAfter(string) ([]string, error) { panic("unused") }
func (s stubRevisionRepo) Clean() (bool, []string, error)          { panic("unused") }
func (s stubRevisionRepo) TrackedPaths(string, []string) ([]string, error) {
	panic("unused")
}
func (s stubRevisionRepo) FileInIndex(string) ([]byte, error) { panic("unused") }

// TestRenderedIdentityLineReportsRealCheck (review-execution 56815db-b F-01
// item 3): the phase evidence's Identity recheck line must report the
// outcome of a real revision-exists probe run at render time — matched when
// the recorded revision exists in the resolved repo, not matched when it
// does not, no recheck ran when no repo was resolved, and an operational
// probe failure surfaces as an error rather than a fabricated "matched".
func TestRenderedIdentityLineReportsRealCheck(t *testing.T) {
	t.Run("repo has the revision: matched", func(t *testing.T) {
		dir, rev := realGitRepo(t)
		root, planDir := evidencePlanFixture(t)
		g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
		g.Nodes[0].Verification.Provenance.Revision = rev
		closed := map[string]bool{"work": true}
		if _, err := renderViews(root, "P", dir, g, nil, closed); err != nil {
			t.Fatal(err)
		}
		doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(doc), "Identity recheck: revision-exists probe") || !strings.Contains(string(doc), "— matched") {
			t.Fatalf("expected a real matched probe line:\n%s", doc)
		}
	})

	t.Run("repo lacks the revision: not matched", func(t *testing.T) {
		dir, _ := realGitRepo(t)
		root, planDir := evidencePlanFixture(t)
		g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
		closed := map[string]bool{"work": true} // evidenceRev does not exist in dir
		if _, err := renderViews(root, "P", dir, g, nil, closed); err != nil {
			t.Fatal(err)
		}
		doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(doc), "— not matched") {
			t.Fatalf("expected a not-matched probe line:\n%s", doc)
		}
	})

	t.Run("no repo resolved: no recheck ran", func(t *testing.T) {
		root, planDir := evidencePlanFixture(t)
		g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
		closed := map[string]bool{"work": true}
		if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
			t.Fatal(err)
		}
		doc, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(doc), "no recheck ran") {
			t.Fatalf("expected an honest no-recheck-ran line when no repo was resolved:\n%s", doc)
		}
	})

	t.Run("operational probe failure propagates as an error", func(t *testing.T) {
		wantErr := errors.New("git could not run")
		_, err := renderPhaseEvidence("/plan", "P",
			phaseGroup{Ordinal: 1, Title: "Core", Doc: "01-core.md",
				Nodes: []*model.Node{{ID: "work", Verification: &model.Verification{
					Provenance: &model.Provenance{Kind: "git", Revision: evidenceRev}}}}},
			"/repo", "2026-01-01", stubRevisionRepo{err: wantErr})
		if err == nil {
			t.Fatal("expected an error from an operational probe failure")
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("error must wrap the probe failure: %v", err)
		}
	})
}

// TestPlanEvidenceIsDayStable (review-execution 56815db-b F-01 item 5): the
// plan README's evidence Verified date derives from the README's own
// stable `updated` stamp, not the render-time clock, so re-rendering an
// unchanged closed plan on a later day produces byte-identical output.
func TestPlanEvidenceIsDayStable(t *testing.T) {
	root, planDir := evidencePlanFixture(t)
	g := &model.Graph{Version: 1, Nodes: []model.Node{closedNode("work", "01-core")}}
	evidenceReview(t, planDir, "P", "01-core.md", "review.md", evidenceRev)
	closed := map[string]bool{"work": true}

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return start }
	t.Cleanup(func() { now = time.Now })

	if _, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	}
	firstReadme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	firstPhase, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Re-render after genuinely crossing a day boundary (the clock is
	// advanced 48 hours, not merely re-read at the same instant): nothing
	// in the graph or reviews changed, so this must be a no-op — no write
	// at all, and certainly no different Verified date, regardless of what
	// the render-time clock now reads.
	now = func() time.Time { return start.Add(48 * time.Hour) }
	if files, err := renderViews(root, "P", "", g, nil, closed); err != nil {
		t.Fatal(err)
	} else if len(files) != 0 {
		t.Fatalf("unchanged closed plan re-render must write nothing after the clock advanced; wrote %v", files)
	}

	secondReadme, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstReadme) != string(secondReadme) {
		t.Fatalf("README changed across a day boundary:\nfirst:\n%s\nsecond:\n%s", firstReadme, secondReadme)
	}
	secondPhase, err := os.ReadFile(filepath.Join(planDir, "01-core.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstPhase) != string(secondPhase) {
		t.Fatalf("phase doc changed across a day boundary:\nfirst:\n%s\nsecond:\n%s", firstPhase, secondPhase)
	}
}
