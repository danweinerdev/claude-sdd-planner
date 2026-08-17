package vcs

import (
	"errors"
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
