package rules

import (
	"os/exec"
	"testing"
)

func appendOnlyGoodFixture(t *testing.T) *prepared {
	t.Helper()
	r := ruleByCode(t, "SDD154")
	for _, ex := range r.Good {
		if ex.Name == "spec-elements-retained" {
			return prepareExample(t, ex)
		}
	}
	t.Fatal("SDD154 good example missing")
	return nil
}

// FR-08 / DD-6: one evaluation runs the append-only history scan exactly
// once and distributes it to SDD154/155/156/164; a second evaluation on a
// fresh root scans again, because family results are evaluation-local, not
// process-global (DD-7).
func TestAppendOnlyScanOnce(t *testing.T) {
	p := appendOnlyGoodFixture(t)
	for i := 0; i < 2; i++ {
		root := freshRoot(t, p.dir)
		before := appendOnlyScans.Load()
		if diags := Run(root); hasCode(diags, "SDD154") {
			t.Fatalf("good fixture reported SDD154: %v", codesOf(diags))
		}
		if got := appendOnlyScans.Load() - before; got != 1 {
			t.Errorf("evaluation %d ran the append-only scan %d times, want exactly 1", i, got)
		}
		before = appendOnlyScans.Load()
		if _, err := RunWithWaiversChecked(root); err != nil {
			t.Fatal(err)
		}
		if got := appendOnlyScans.Load() - before; got != 0 {
			t.Errorf("a second evaluation of the SAME root rescanned %d time(s); the root's family result is memoized", got)
		}
	}
}

// FR-09 / DD-7: a newly loaded root after a worktree, index or HEAD change
// observes the new state; the previously loaded root keeps its own result
// (callers reload for freshness, per the immutable-root contract).
func TestReloadSeesSCMMutation(t *testing.T) {
	p := appendOnlyGoodFixture(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", p.dir}, args...)...)
		cmd.Env = append(cmd.Environ(), setupEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	first := freshRoot(t, p.dir)
	if diags := Run(first); hasCode(diags, "SDD154") {
		t.Fatalf("baseline reported SDD154: %v", codesOf(diags))
	}

	// Index + worktree mutation: the tracked spec is removed.
	git("rm", "-q", "Specs/Sample/README.md")
	afterRm := freshRoot(t, p.dir)
	if diags := Run(afterRm); !hasCode(diags, "SDD154") {
		t.Fatalf("a fresh root after `git rm` did not observe the removal: %v", codesOf(diags))
	}
	if diags := Run(first); hasCode(diags, "SDD154") {
		t.Error("the previously loaded root changed its answer; family results must be root-local")
	}

	// HEAD mutation: the removal is committed, so HEAD no longer carries
	// the baseline and a fresh root sees no retained-id violation.
	git("commit", "-q", "-m", "remove spec")
	afterCommit := freshRoot(t, p.dir)
	if diags := Run(afterCommit); hasCode(diags, "SDD154") {
		t.Fatalf("a fresh root after the commit still compares against the old HEAD: %v", codesOf(diags))
	}
	if diags := Run(afterRm); !hasCode(diags, "SDD154") {
		t.Error("the root loaded before the commit changed its answer")
	}
}
