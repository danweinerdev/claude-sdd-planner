package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/spf13/cobra"
)

func TestDecidePlanRoutesRejectNonComponentNames(t *testing.T) {
	root := decideTestRoot(t)
	outside := filepath.Join(root, "victim")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	for _, plan := range []string{"../victim", `..\victim`, "nested/plan", root} {
		for _, route := range decidePlanRoutes(plan) {
			t.Run(strings.ReplaceAll(plan, string(filepath.Separator), "_")+"/"+route.name, func(t *testing.T) {
				err := route.run()
				if err == nil || !strings.Contains(err.Error(), "single directory name") {
					t.Fatalf("non-component plan %q was not rejected by %s: %v", plan, route.name, err)
				}
			})
		}
	}
}

func TestDecidePlanRoutesRejectSymlinkEscape(t *testing.T) {
	root := decideTestRoot(t)
	outside := t.TempDir()
	link := filepath.Join(root, "Plans", "Escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(root)

	for _, route := range decidePlanRoutes("Escape") {
		t.Run(route.name, func(t *testing.T) {
			err := route.run()
			if err == nil || !strings.Contains(err.Error(), "outside Plans") {
				t.Fatalf("symlink escape was not rejected by %s: %v", route.name, err)
			}
		})
	}
}

func TestDecidePlanRoutesRejectSymlinkAliasWithinPlans(t *testing.T) {
	root := decideTestRoot(t)
	real := filepath.Join(root, "Plans", "Real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "Plans", "Alias")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(root)

	for _, route := range decidePlanRoutes("Alias") {
		t.Run(route.name, func(t *testing.T) {
			err := route.run()
			if err == nil || !strings.Contains(err.Error(), "not a symlink") {
				t.Fatalf("symlink alias was not rejected by %s: %v", route.name, err)
			}
		})
	}
}

func TestDecidePlanRoutesRejectSymlinkedPlansDirectory(t *testing.T) {
	root := decideTestRoot(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "Target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "Plans")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Plans")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(root)

	for _, route := range decidePlanRoutes("Target") {
		t.Run(route.name, func(t *testing.T) {
			err := route.run()
			if err == nil || !strings.Contains(err.Error(), "Plans/ must be a real directory") {
				t.Fatalf("symlinked Plans directory was not rejected by %s: %v", route.name, err)
			}
		})
	}
}

func TestDecideAddRefusesIncompleteDecisionSnapshotBeforeWriting(t *testing.T) {
	root := decideTestRoot(t)
	target := filepath.Join(root, "Plans", "Target")
	broken := filepath.Join(root, "Plans", "Broken")
	for _, dir := range []string{target, broken} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(decisions.PathFor(broken), []byte("[{broken]"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	err := cmdDecideAdd(&cobra.Command{}, "Target", "approved statement", "", "", false)
	if err == nil || !strings.Contains(err.Error(), "snapshot is incomplete") {
		t.Fatalf("add did not refuse incomplete decision snapshot: %v", err)
	}
	if _, statErr := os.Stat(decisions.PathFor(target)); !os.IsNotExist(statErr) {
		t.Fatalf("add wrote target decisions before refusing snapshot: %v", statErr)
	}
}

type decidePlanRoute struct {
	name string
	run  func() error
}

func decidePlanRoutes(plan string) []decidePlanRoute {
	command := func() *cobra.Command {
		c := &cobra.Command{}
		c.SetOut(io.Discard)
		return c
	}
	return []decidePlanRoute{
		{name: "add", run: func() error { return cmdDecideAdd(command(), plan, "approved statement", "", "", false) }},
		{name: "list", run: func() error { return cmdDecideList(command(), plan, false) }},
		{name: "current", run: func() error { return cmdDecideCurrent(command(), plan, false) }},
		{name: "lookup", run: func() error { return cmdDecideLookup(command(), "pd-deadbeef", plan, false) }},
		{name: "render", run: func() error { return cmdDecideRender(command(), plan, false) }},
	}
}

func decideTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":"."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}
