package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The append-only graph-conversion exemption is subtle enough — and its
// worktree-vs-index split important enough — that the registry examples alone
// do not exercise it. These tests drive a real git repository directly so they
// can control exactly what is staged versus left unstaged, the one thing the
// example harness's argv-only Setup cannot express.

// graphConversionRoot writes files into a fresh git repository, commits them
// as the baseline, then runs after (which may stage removals or write
// unstaged files) before loading the root.
func graphConversionRoot(t *testing.T, files map[string]string, after func(dir string, run func(args ...string))) *Root {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), setupEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "baseline")
	if after != nil {
		after(dir, run)
	}
	r, err := LoadRoot(dir)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	return r
}

func countCode(ds []Diagnostic, code string) int {
	n := 0
	for _, d := range ds {
		if d.Code == code {
			n++
		}
	}
	return n
}

// TestGraphConversionExcusesStagedDeletion: removing a v1 phase document whose
// task ids all survive as graph nodes, with a generated replacement view
// declared by the plan README, is a deliberate conversion — SDD156/SDD164 stay
// quiet for both the worktree and the index.
func TestGraphConversionExcusesStagedDeletion(t *testing.T) {
	r := graphConversionRoot(t, map[string]string{
		"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
		"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
		"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
		"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil),
	}, func(dir string, run func(...string)) {
		run("rm", "-q", "Plans/Sample/01-One.md")
	})
	for _, d := range Run(r) {
		if d.Code == "SDD156" || d.Code == "SDD164" {
			t.Errorf("a deliberate graph conversion must not report %s: %s", d.Code, d.Message)
		}
	}
}

// TestGraphConversionExcusesRetiredTaskIDs: a task id superseded by an
// in-place rebuild is recorded in the retired register, and that tombstone
// still accounts for it.
func TestGraphConversionExcusesRetiredTaskIDs(t *testing.T) {
	r := graphConversionRoot(t, map[string]string{
		"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
		"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
		"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
		"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1"}, []string{"task-1-2"}),
	}, func(dir string, run func(...string)) {
		run("rm", "-q", "Plans/Sample/01-One.md")
	})
	for _, d := range Run(r) {
		if d.Code == "SDD156" || d.Code == "SDD164" {
			t.Errorf("a retired task id must still account for its v1 task: %s %s", d.Code, d.Message)
		}
	}
}

// TestUnstagedGraphDoesNotExcuseStagedDeletion: the graph is written to the
// worktree AFTER the baseline commit and never staged, while the phase doc's
// deletion IS staged. The worktree is excused, but the index must not be —
// an unstaged graph cannot excuse a staged deletion.
func TestUnstagedGraphDoesNotExcuseStagedDeletion(t *testing.T) {
	r := graphConversionRoot(t, map[string]string{
		"Plans/Sample/README.md":  planReadmeWithPhaseDoc("01-core.md"),
		"Plans/Sample/01-One.md":  v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
		"Plans/Sample/01-core.md": generatedPhaseView("Sample", "1", "One"),
	}, func(dir string, run func(...string)) {
		run("rm", "-q", "Plans/Sample/01-One.md")
		// Unstaged graph: written to disk, never `git add`ed, so the index
		// state has no graph to excuse the staged deletion.
		graphPath := filepath.Join(dir, "Plans", "Sample", "Sample-Graph.json")
		if err := os.WriteFile(graphPath, []byte(graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil)), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	diags := Run(r)
	if got := countCode(diags, "SDD156"); got != 2 {
		t.Errorf("index must report 2 SDD156 (tasks 1.1 and 1.2), got %d", got)
	}
	if got := countCode(diags, "SDD164"); got != 1 {
		t.Errorf("index must report 1 SDD164 (phase disappeared), got %d", got)
	}
}
