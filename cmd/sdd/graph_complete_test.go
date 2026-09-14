package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// completeFixture builds a planning root with one committed graph plan:
// implementation node "a" feeding a full review node "review-a". Both are
// pre-observed GREEN/pass so they derive closed — the fixture is the
// smallest graph review.Closed will mark fully closed. When open is true,
// "a" carries no observation, so neither node closes.
func completeFixture(t *testing.T, open bool) (root, planDir string) {
	t.Helper()
	root = chdirTemp(t)
	writeArtifact(t, root, "Plans/Demo", "README.md", `---
title: "Demo"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
phases: []
---

# Demo
`)
	a := model.Node{ID: "a", Contract: "does a", Phase: "01-core",
		Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_a", File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1}
	if !open {
		a.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
	}
	reviewA := model.Node{ID: "review-a", Contract: "reviews a", Phase: "01-core",
		Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1}
	if !open {
		reviewA.Verification = &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}
	}
	g := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{a, reviewA}}
	planDir = filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

func TestGraphPlanCompleteRefusesOpenNode(t *testing.T) {
	_, planDir := completeFixture(t, true)
	readme := filepath.Join(planDir, "README.md")
	before, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme})
		return root.Execute()
	})
	if err == nil {
		t.Fatalf("expected refusal, got success:\n%s", out)
	}
	if re, ok := err.(*refusedError); !ok {
		t.Fatalf("expected *refusedError, got %T: %v", err, err)
	} else if !strings.Contains(re.Error(), "a") {
		t.Fatalf("refusal must name the open node:\n%s", re.Error())
	}

	after, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("refused plan complete must write nothing; README changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestGraphPlanCompleteClosedGraphWrites(t *testing.T) {
	_, planDir := completeFixture(t, false)
	readme := filepath.Join(planDir, "README.md")

	// --dry-run must write nothing.
	dryBefore, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme, "--dry-run"})
		return root.Execute()
	}); err != nil {
		t.Fatalf("dry-run on a closed graph must succeed: %v\n%s", err, out)
	}
	dryAfter, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if string(dryAfter) != string(dryBefore) {
		t.Fatalf("--dry-run must write nothing; README changed:\nbefore:\n%s\nafter:\n%s", dryBefore, dryAfter)
	}

	// Real run: README status flips to complete, phase doc(s) become complete.
	if out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme})
		return root.Execute()
	}); err != nil {
		t.Fatalf("plan complete on a closed graph: %v\n%s", err, out)
	}

	readmeBytes, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readmeBytes), "status: complete") {
		t.Fatalf("README status was not flipped to complete:\n%s", readmeBytes)
	}

	phaseDoc := filepath.Join(planDir, "01-core.md")
	phaseBytes, err := os.ReadFile(phaseDoc)
	if err != nil {
		t.Fatalf("expected rendered phase doc %s: %v", phaseDoc, err)
	}
	if !strings.Contains(string(phaseBytes), "status: complete") {
		t.Fatalf("phase doc status was not complete:\n%s", phaseBytes)
	}
}

func TestGraphPhaseCompleteRefreshesPhaseStatus(t *testing.T) {
	_, planDir := completeFixture(t, false)

	// Render the initial (planned) phase doc via plan complete's renderer
	// path indirectly is circular; drive `sdd compile`-equivalent refresh by
	// running phase complete directly against the not-yet-rendered doc path.
	phaseDoc := filepath.Join(planDir, "01-core.md")

	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"phase", "complete", phaseDoc})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("phase complete on a closed phase: %v\n%s", err, out)
	}

	phaseBytes, err := os.ReadFile(phaseDoc)
	if err != nil {
		t.Fatalf("expected rendered phase doc %s: %v", phaseDoc, err)
	}
	if !strings.Contains(string(phaseBytes), "status: complete") {
		t.Fatalf("phase doc status was not refreshed to complete:\n%s", phaseBytes)
	}
}

// TestGraphCompleteReadFailuresAreOperational (review-execution 56815db-b
// F-01 items 1-2): a graph-store stat failure for any reason other than
// not-exist, and a reviews-directory read failure, must exit 2 with the
// cause — never fall through to the v1 completion path, and never write
// anything.
func TestGraphCompleteReadFailuresAreOperational(t *testing.T) {
	t.Run("graph-store stat failure", func(t *testing.T) {
		_, planDir := completeFixture(t, false)
		readme := filepath.Join(planDir, "README.md")
		before, err := os.ReadFile(readme)
		if err != nil {
			t.Fatal(err)
		}

		old := statGraphStore
		statGraphStore = func(name string) (os.FileInfo, error) {
			return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrPermission}
		}
		defer func() { statGraphStore = old }()

		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"plan", "complete", readme})
			return root.Execute()
		})
		if err == nil {
			t.Fatalf("expected an error, got success:\n%s", out)
		}
		if exitCode(err) != 2 {
			t.Fatalf("exit = %d, want 2 (operational): %v", exitCode(err), err)
		}
		if _, ok := err.(*refusedError); ok {
			t.Fatalf("a graph-store stat failure must not be a refusal: %v", err)
		}
		after, err := os.ReadFile(readme)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Fatal("an operational stat failure must write nothing")
		}
	})

	t.Run("reviews-directory read failure", func(t *testing.T) {
		_, planDir := completeFixture(t, false)
		readme := filepath.Join(planDir, "README.md")
		before, err := os.ReadFile(readme)
		if err != nil {
			t.Fatal(err)
		}
		reviews := filepath.Join(planDir, "reviews")
		if err := os.MkdirAll(reviews, 0o755); err != nil {
			t.Fatal(err)
		}
		obstructReviewsRead(t, reviews)
		if _, readErr := os.ReadDir(reviews); readErr == nil {
			t.Fatal("reviews-directory obstruction did not make os.ReadDir fail")
		} else if os.IsNotExist(readErr) {
			t.Fatalf("injected reviews read error must not be IsNotExist: %v", readErr)
		}

		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"plan", "complete", readme})
			return root.Execute()
		})
		if err == nil {
			t.Fatalf("expected an error, got success:\n%s", out)
		}
		if exitCode(err) != 2 {
			t.Fatalf("exit = %d, want 2 (operational): %v", exitCode(err), err)
		}
		after, err := os.ReadFile(readme)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Fatal("an operational reviews-directory read failure must write nothing")
		}
		phaseDoc := filepath.Join(planDir, "01-core.md")
		if _, err := os.Stat(phaseDoc); !os.IsNotExist(err) {
			t.Fatalf("an operational failure must not render a phase doc: %v", err)
		}
	})
}

// TestGraphPhaseCompleteRefusesAndDryRuns (contract rev 7 acceptance): an
// open node in the phase must refuse naming it, writing nothing, and
// --dry-run on a closed phase must write nothing while still reporting
// success.
func TestGraphPhaseCompleteRefusesAndDryRuns(t *testing.T) {
	t.Run("open node refuses and writes nothing", func(t *testing.T) {
		_, planDir := completeFixture(t, true)
		// The phase doc has never been rendered (the fixture's graph is not
		// closed), matching TestGraphPhaseCompleteRefreshesPhaseStatus's
		// convention of driving `phase complete` against the not-yet-rendered
		// doc path directly.
		phaseDoc := filepath.Join(planDir, "01-core.md")
		if _, err := os.Stat(phaseDoc); !os.IsNotExist(err) {
			t.Fatalf("fixture must not have a pre-rendered phase doc: %v", err)
		}

		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"phase", "complete", phaseDoc})
			return root.Execute()
		})
		if err == nil {
			t.Fatalf("expected refusal, got success:\n%s", out)
		}
		re, ok := err.(*refusedError)
		if !ok {
			t.Fatalf("expected *refusedError, got %T: %v", err, err)
		}
		if !strings.Contains(re.Error(), "a") {
			t.Fatalf("refusal must name the open node:\n%s", re.Error())
		}
		if _, statErr := os.Stat(phaseDoc); !os.IsNotExist(statErr) {
			t.Fatal("a refused phase complete must not render the phase doc")
		}
	})

	t.Run("dry-run on a closed phase writes nothing", func(t *testing.T) {
		_, planDir := completeFixture(t, false)
		phaseDoc := filepath.Join(planDir, "01-core.md")
		if _, err := os.Stat(phaseDoc); !os.IsNotExist(err) {
			t.Fatalf("fixture must not have a pre-rendered phase doc: %v", err)
		}

		out, err := captureStdout(t, func() error {
			root := newRootCmd()
			root.SetArgs([]string{"phase", "complete", phaseDoc, "--dry-run"})
			return root.Execute()
		})
		if err != nil {
			t.Fatalf("dry-run on a closed phase must succeed: %v\n%s", err, out)
		}
		if _, statErr := os.Stat(phaseDoc); !os.IsNotExist(statErr) {
			t.Fatal("--dry-run must not render the phase doc")
		}
	})
}

// TestGraphCompleteStatusFlipIsCompareAndSwap (review-execution 56815db-b
// F-01 item 6): the README status flip that follows RenderViews is a
// compare-and-swap against exactly the bytes RenderViews just wrote. A
// concurrent writer that changes the README in the window between the
// render and the flip must cause the flip to be refused (an
// *store.ErrConcurrentWrite, surfaced through plan complete's operational
// error), and the concurrent writer's content must survive untouched —
// never silently overwritten by the flip.
func TestGraphCompleteStatusFlipIsCompareAndSwap(t *testing.T) {
	_, planDir := completeFixture(t, false)
	readme := filepath.Join(planDir, "README.md")

	var concurrentContent string
	beforeStatusFlip = func(readme string) {
		src, err := os.ReadFile(readme)
		if err != nil {
			t.Fatal(err)
		}
		// A concurrent writer changes the README (e.g. an unrelated
		// frontmatter edit) after RenderViews wrote its views and before
		// the status flip runs.
		concurrentContent = string(src) + "\n<!-- concurrent writer -->\n"
		if err := os.WriteFile(readme, []byte(concurrentContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeStatusFlip = nil })

	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme})
		return root.Execute()
	})
	if err == nil {
		t.Fatalf("expected the status flip to be refused, got success:\n%s", out)
	}
	if !strings.Contains(err.Error(), "already") {
		t.Fatalf("refusal must say the views were already rendered when the flip was refused: %v", err)
	}
	// A compare-and-swap conflict is a refused mutation (FR-03: exit 1), not
	// an inability to run the operation (exit 2).
	if code := exitCode(err); code != 1 {
		t.Fatalf("exitCode(err) = %d, want 1 (refused mutation, not exit 2)", code)
	}

	after, readErr := os.ReadFile(readme)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != concurrentContent {
		t.Fatalf("the concurrent writer's content must survive untouched:\nwant:\n%s\ngot:\n%s", concurrentContent, after)
	}
	if strings.Contains(string(after), "\nstatus: complete\n") {
		t.Fatal("a refused status flip must not have marked the plan complete")
	}
	if !strings.Contains(string(after), "\nstatus: active\n") {
		t.Fatalf("the plan's top-level status must remain untouched by the refused flip:\n%s", after)
	}
}

// TestWriteReadmeStatusCompleteNonConflictErrorExitsTwo isolates
// writeReadmeStatusComplete's OTHER error branch directly: an operational
// failure that is NOT a *store.ErrConcurrentWrite (here, a deterministic
// injected temp-file create failure from WriteAtomicExpecting) must surface
// as a plain error — never wrapped in
// *refusedError — so it exits 2 (an inability to run the operation), not 1
// (a refused mutation). The expectDigest passed matches the file's current
// digest exactly; the exit-1 conflict case is
// TestGraphCompleteStatusFlipIsCompareAndSwap.
func TestWriteReadmeStatusCompleteNonConflictErrorExitsTwo(t *testing.T) {
	root := chdirTemp(t)
	src := `---
title: "Demo"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
phases: []
---

# Demo
`
	readme := writeArtifact(t, root, "Plans/Demo", "README.md", src)
	current, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	expectDigest := store.Digest(string(current))

	lines := strings.Split(src, "\n")
	if !setTopLevelStatus(lines, "complete") {
		t.Fatal("fixture has no top-level status")
	}
	wantUpdated := restampUpdated(strings.Join(lines, "\n"), time.Now().Format("2006-01-02"))

	called := false
	old := writeGraphReadmeExpecting
	writeGraphReadmeExpecting = func(path, content, digest string) error {
		called = true
		if path != readme {
			t.Errorf("write path = %q, want %q", path, readme)
		}
		if content != wantUpdated {
			t.Errorf("write candidate mismatch:\nwant:\n%s\ngot:\n%s", wantUpdated, content)
		}
		if digest != expectDigest {
			t.Errorf("write digest = %q, want %q", digest, expectDigest)
		}
		return &os.PathError{Op: "create", Path: path, Err: os.ErrPermission}
	}
	defer func() { writeGraphReadmeExpecting = old }()

	err = writeReadmeStatusComplete(readme, expectDigest)
	if err == nil {
		t.Fatal("expected an injected operational write error")
	}
	if !called {
		t.Fatal("writeReadmeStatusComplete did not reach WriteAtomicExpecting")
	}
	var conflict *store.ErrConcurrentWrite
	if errors.As(err, &conflict) {
		t.Fatalf("an injected create failure must not be reported as a concurrent-write conflict: %v", err)
	}
	if _, ok := err.(*refusedError); ok {
		t.Fatalf("a non-conflict write failure must not be wrapped in *refusedError: %v", err)
	}
	wrapped := fmt.Errorf("plan complete: %w", err)
	if code := exitCode(wrapped); code != 2 {
		t.Fatalf("exitCode = %d, want 2 (operational, not a refused mutation): %v", code, wrapped)
	}
	after, readErr := os.ReadFile(readme)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(current) {
		t.Fatalf("failed write must leave README unchanged:\nbefore:\n%s\nafter:\n%s", current, after)
	}
}

// initGitRepo turns dir into a minimal git repo with one commit, so
// vcs.DetectChecked resolves a real HEAD for the completed_at capture.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "initial", "--allow-empty")
}

// TestGraphPlanCompleteRecordsCompletedAt: `sdd plan complete` on a closed
// graph in a real git repo records CompletedAt (HEAD revision + seq) in the
// committed graph, and `sdd graph status` surfaces it.
func TestGraphPlanCompleteRecordsCompletedAt(t *testing.T) {
	root, planDir := completeFixture(t, false)
	initGitRepo(t, root)
	readme := filepath.Join(planDir, "README.md")

	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantRev := strings.TrimSpace(string(head))

	if out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"plan", "complete", readme})
		return root.Execute()
	}); err != nil {
		t.Fatalf("plan complete: %v\n%s", err, out)
	}

	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.CompletedAt == nil {
		t.Fatalf("expected CompletedAt to be recorded")
	}
	if g.CompletedAt.Revision != wantRev {
		t.Fatalf("CompletedAt.Revision = %q, want %q", g.CompletedAt.Revision, wantRev)
	}
	if g.CompletedAt.Seq != g.SeqCounter {
		t.Fatalf("CompletedAt.Seq = %d, want SeqCounter %d", g.CompletedAt.Seq, g.SeqCounter)
	}

	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"graph", "status", "--plan", "Demo"})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("graph status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "completed_at: revision="+wantRev) {
		t.Fatalf("expected completed_at line in graph status output:\n%s", out)
	}
}
