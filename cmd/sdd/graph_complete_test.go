package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		if err := os.Chmod(reviews, 0o000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(reviews, 0o755)

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
// failure that is NOT a *store.ErrConcurrentWrite (here,
// WriteAtomicExpecting's temp-file create failing because the plan
// directory is unwritable) must surface as a plain error — never wrapped in
// *refusedError — so it exits 2 (an inability to run the operation), not 1
// (a refused mutation). The expectDigest passed matches the file's current
// digest exactly, so store.WriteAtomicExpecting's own digest re-check
// cannot itself produce the conflict this test must NOT exercise; the exit-1
// conflict case is TestGraphCompleteStatusFlipIsCompareAndSwap.
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

	planDir := filepath.Dir(readme)
	if err := os.Chmod(planDir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(planDir, 0o755)

	err = writeReadmeStatusComplete(readme, expectDigest)
	if err == nil {
		t.Fatal("expected an operational error from the unwritable plan directory")
	}
	var conflict *store.ErrConcurrentWrite
	if errors.As(err, &conflict) {
		t.Fatalf("an unwritable-directory failure must not be reported as a concurrent-write conflict: %v", err)
	}
	if _, ok := err.(*refusedError); ok {
		t.Fatalf("a non-conflict write failure must not be wrapped in *refusedError: %v", err)
	}
	wrapped := fmt.Errorf("plan complete: %w", err)
	if code := exitCode(wrapped); code != 2 {
		t.Fatalf("exitCode = %d, want 2 (operational, not a refused mutation): %v", code, wrapped)
	}
}
