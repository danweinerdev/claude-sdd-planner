package vcs

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// withMemoization runs fn with memoization enabled and restores the
// disabled-by-default state (and empty caches) afterwards, so no other test
// can observe stale repository answers.
func withMemoization(t *testing.T, fn func()) {
	t.Helper()
	memoEnabled = true
	defer func() {
		memoEnabled = false
		resetCaches()
	}()
	fn()
}

func TestMemoDisabledByDefault(t *testing.T) {
	calls := 0
	for range 3 {
		v, err := memo(memoKey("root", "op", "arg"), func() (int, error) {
			calls++
			return 42, nil
		})
		if v != 42 || err != nil {
			t.Fatalf("memo = %d, %v", v, err)
		}
	}
	if calls != 3 {
		t.Fatalf("disabled memo should call through every time; got %d calls", calls)
	}
}

func TestMemoCachesValuesAndErrors(t *testing.T) {
	withMemoization(t, func() {
		calls := 0
		for range 3 {
			v, err := memo(memoKey("root", "op", "arg"), func() (string, error) {
				calls++
				return "answer", nil
			})
			if v != "answer" || err != nil {
				t.Fatalf("memo = %q, %v", v, err)
			}
		}
		if calls != 1 {
			t.Fatalf("value should be computed once; got %d calls", calls)
		}

		// A determinate error (ErrNotFound) is an answer and is cached too.
		errCalls := 0
		for range 2 {
			_, err := memo(memoKey("root", "op", "missing"), func() (string, error) {
				errCalls++
				return "", ErrNotFound
			})
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		}
		if errCalls != 1 {
			t.Fatalf("error should be computed once; got %d calls", errCalls)
		}
	})
}

func TestMemoKeysDoNotCollide(t *testing.T) {
	withMemoization(t, func() {
		// The NUL separator means composed keys cannot collide even when the
		// parts' concatenation is identical.
		a, _ := memo(memoKey("ab", "c"), func() (string, error) { return "first", nil })
		b, _ := memo(memoKey("a", "bc"), func() (string, error) { return "second", nil })
		if a != "first" || b != "second" {
			t.Fatalf("keys collided: a=%q b=%q", a, b)
		}
	})
}

// stubRepo counts calls so decorator tests can distinguish cached from
// delegated operations. It stands in for any adapter (git, p4, future ones):
// the decorator is adapter-agnostic by construction.
type stubRepo struct {
	calls map[string]int
}

func (s *stubRepo) hit(op string) { s.calls[op]++ }

func (s *stubRepo) Kind() Kind                      { return Git }
func (s *stubRepo) Root() string                    { return "/stub" }
func (s *stubRepo) RevisionSyntaxValid(string) bool { return true }
func (s *stubRepo) RevisionExists(string) (bool, error) {
	s.hit("RevisionExists")
	return true, nil
}
func (s *stubRepo) Head() (string, error) { s.hit("Head"); return "abc", nil }
func (s *stubRepo) IsAncestor(string, string) (bool, error) {
	s.hit("IsAncestor")
	return true, nil
}
func (s *stubRepo) Parents(string) ([]string, error) {
	s.hit("Parents")
	return []string{"p1", "p2"}, nil
}
func (s *stubRepo) FileAt(string, string) ([]byte, error) {
	s.hit("FileAt")
	return []byte("content"), nil
}
func (s *stubRepo) ChangedPaths(string) ([]string, error) {
	s.hit("ChangedPaths")
	return []string{"a"}, nil
}
func (s *stubRepo) RevisionsAfter(string) ([]string, error) {
	s.hit("RevisionsAfter")
	return []string{"r"}, nil
}
func (s *stubRepo) Clean() (bool, []string, error) { s.hit("Clean"); return true, nil, nil }
func (s *stubRepo) TrackedPaths(string, []string) ([]string, error) {
	s.hit("TrackedPaths")
	return []string{"t"}, nil
}
func (s *stubRepo) FileInIndex(string) ([]byte, error) {
	s.hit("FileInIndex")
	return []byte("staged"), nil
}

func TestDecoratorMemoizesObjectStateOnly(t *testing.T) {
	withMemoization(t, func() {
		stub := &stubRepo{calls: map[string]int{}}
		repo := memoize(stub)

		for range 3 {
			repo.RevisionExists("deadbeef")
			repo.Head()
			repo.IsAncestor("a", "b")
			repo.Parents("deadbeef")
			repo.FileAt("HEAD", "f.md")
			repo.ChangedPaths("deadbeef")
			repo.RevisionsAfter("deadbeef")
			repo.TrackedPaths("HEAD", []string{"Plans"})
			repo.Clean()
			repo.FileInIndex("f.md")
		}
		for _, op := range []string{
			"RevisionExists", "Head", "IsAncestor", "Parents", "FileAt",
			"ChangedPaths", "RevisionsAfter", "TrackedPaths",
			// FileInIndex is working state, but the append-only rules query it
			// once per artifact, so an uncached call cost one git exec per
			// artifact per run. It is cached WITHIN a run and dropped by
			// InvalidateWorkingState, which the transition gate calls around
			// its candidate write — see the Clean check below for the
			// always-live case.
			"FileInIndex",
		} {
			if stub.calls[op] != 1 {
				t.Errorf("%s: %d underlying calls, want 1 (memoized)", op, stub.calls[op])
			}
		}
		// Clean() is never cached: it reports whole-worktree state that the
		// transition gate's temp write changes, and no key identifies it.
		if stub.calls["Clean"] != 3 {
			t.Errorf("Clean: %d underlying calls, want 3 (uncached)", stub.calls["Clean"])
		}
		// The invalidation contract FileInIndex's caching rests on: after it,
		// the next query hits the underlying repo again.
		InvalidateWorkingState()
		repo.FileInIndex("f.md")
		if stub.calls["FileInIndex"] != 2 {
			t.Errorf("FileInIndex after InvalidateWorkingState: %d underlying calls, want 2 — "+
				"the transition gate depends on this to observe its candidate write",
				stub.calls["FileInIndex"])
		}
	})
}

func TestDecoratorCopiesSliceResults(t *testing.T) {
	withMemoization(t, func() {
		repo := memoize(&stubRepo{calls: map[string]int{}})

		parents, _ := repo.Parents("deadbeef")
		parents[0] = "corrupted"
		again, _ := repo.Parents("deadbeef")
		if again[0] != "p1" {
			t.Fatalf("cached Parents corrupted by caller mutation: %v", again)
		}

		content, _ := repo.FileAt("HEAD", "f.md")
		content[0] = 'X'
		again2, _ := repo.FileAt("HEAD", "f.md")
		if string(again2) != "content" {
			t.Fatalf("cached FileAt corrupted by caller mutation: %q", again2)
		}
	})
}

func TestMemoizeDisabledReturnsBareAdapter(t *testing.T) {
	stub := &stubRepo{calls: map[string]int{}}
	if got := memoize(stub); got != Repo(stub) {
		t.Fatal("memoize should be an identity when disabled")
	}
}

func TestDetectMemoized(t *testing.T) {
	dir := t.TempDir()
	withMemoization(t, func() {
		r1 := Detect(dir)
		r2 := Detect(dir)
		if r1 != r2 {
			t.Fatalf("Detect should return the cached adapter for the same dir")
		}
	})
}

// FR-09 / AC-04 / DD-7: a transient operational failure must never become a
// cached fact. Once git is back on PATH the next call answers from the
// repository, and a detection probe that failed leaves no NoRepo behind.
func TestOperationalFailureNotCached(t *testing.T) {
	t.Setenv("SDD_VCS_DISABLE_P4", "1")
	realPath := os.Getenv("PATH")
	repo := initCheckedRepo(t)
	emptyDir := t.TempDir()

	withMemoization(t, func() {
		live, err := DetectChecked(repo)
		if err != nil {
			t.Fatalf("baseline detection: %v", err)
		}
		head, err := live.Head()
		if err != nil {
			t.Fatalf("baseline Head: %v", err)
		}
		resetCaches()

		// Operation-level: the first call fails operationally while git is
		// off PATH; the cache must not serve that failure afterwards.
		t.Setenv("PATH", emptyDir)
		if _, err := live.RevisionExists(head); !errors.Is(err, ErrOperational) {
			t.Fatalf("RevisionExists without git: %v, want ErrOperational", err)
		}
		if _, err := live.Head(); !errors.Is(err, ErrOperational) {
			t.Fatalf("Head without git: %v, want ErrOperational", err)
		}
		if _, err := live.FileAt(head, "a.txt"); !errors.Is(err, ErrOperational) {
			t.Fatalf("FileAt without git: %v, want ErrOperational", err)
		}

		t.Setenv("PATH", realPath)
		if ok, err := live.RevisionExists(head); !ok || err != nil {
			t.Errorf("RevisionExists after git returned = %v, %v; want true, nil "+
				"— the operational failure was served from cache", ok, err)
		}
		if got, err := live.Head(); err != nil || got != head {
			t.Errorf("Head after git returned = %q, %v; want %q, nil "+
				"— the operational failure was served from cache", got, err, head)
		}
		if content, err := live.FileAt(head, "a.txt"); err != nil || string(content) != "a\n" {
			t.Errorf("FileAt after git returned = %q, %v; want \"a\\n\", nil "+
				"— the operational failure was served from cache", content, err)
		}

		// Detection-level: a probe that could not run must leave nothing
		// cached, least of all a NoRepo/Unavailable verdict for a real repo.
		resetCaches()
		t.Setenv("PATH", emptyDir)
		if r, err := DetectChecked(repo); !errors.Is(err, ErrOperational) || r != nil {
			t.Fatalf("DetectChecked without git = %v, %v; want nil, ErrOperational", r, err)
		}
		t.Setenv("PATH", realPath)
		r, err := DetectChecked(repo)
		if err != nil {
			t.Fatalf("DetectChecked after git returned: %v "+
				"— the failed probe was served from cache", err)
		}
		if r.Kind() != Git {
			t.Errorf("DetectChecked after git returned Kind() = %q, want %q "+
				"— a failed probe was stored as a non-git verdict", r.Kind(), Git)
		}
	})
}

// FR-09 / DD-7: a determinate ErrNotFound stays cacheable — it is an answer,
// not a failure — while whole-worktree state (Clean) stays uncached.
func TestDeterminateAbsenceCached(t *testing.T) {
	t.Setenv("SDD_VCS_DISABLE_P4", "1")
	realPath := os.Getenv("PATH")
	repo := initCheckedRepo(t)
	emptyDir := t.TempDir()
	bogus := strings.Repeat("b", 40)

	withMemoization(t, func() {
		live, err := DetectChecked(repo)
		if err != nil {
			t.Fatalf("baseline detection: %v", err)
		}
		if ok, err := live.RevisionExists(bogus); ok || !errors.Is(err, ErrNotFound) {
			t.Fatalf("RevisionExists(bogus) = %v, %v; want false, ErrNotFound", ok, err)
		}

		// Removing git proves the second answer came from the cache: an
		// uncached call would have to exec git and would fail operationally.
		t.Setenv("PATH", emptyDir)
		ok, err := live.RevisionExists(bogus)
		if ok || !errors.Is(err, ErrNotFound) || errors.Is(err, ErrOperational) {
			t.Errorf("cached RevisionExists(bogus) = %v, %v; want false, ErrNotFound "+
				"— the determinate absence was not cached", ok, err)
		}

		// Clean() reports whole-worktree state and is never cached: with git
		// gone it must fail operationally rather than replay an answer.
		t.Setenv("PATH", realPath)
		if clean, _, err := live.Clean(); err != nil || !clean {
			t.Fatalf("baseline Clean() = %v, %v; want true, nil", clean, err)
		}
		t.Setenv("PATH", emptyDir)
		if _, _, err := live.Clean(); !errors.Is(err, ErrOperational) {
			t.Errorf("Clean() without git = %v; want ErrOperational — Clean must stay uncached", err)
		}
	})
}
