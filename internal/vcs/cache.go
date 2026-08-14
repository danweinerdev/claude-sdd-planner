package vcs

import (
	"strings"
	"sync"
)

// Process-lifetime memoization for repository queries.
//
// One validation pass asks the same questions many times: every rule that
// touches evidence re-detects the repository for its directory, and every
// completed task's revision is re-checked for existence, ancestry, and
// committed content on every run. `sdd task complete` doubles all of it by
// validating the root before and after the candidate transition. Each of
// those questions is a subprocess (git) or a network RPC (p4), so an
// unmemoized run scales as O(completed tasks × checks) spawns and dominates
// wall-clock time on a mature planning root.
//
// Memoization is a decorator applied around whatever adapter Detect returns,
// so every VCS — git, Perforce, and any future adapter — is covered at the
// seam instead of hand-wrapping each adapter's methods.
//
// The cache is sound because sdd never mutates repository state: adapters
// are read-only by contract (Repo's doc), and no sdd subcommand commits,
// stages, or moves refs. Within one process, therefore:
//
//   - Object/ref state (revision existence, ancestry, parents, file content
//     at a revision, tracked/changed paths, HEAD) is immutable → cached.
//   - Detection (which VCS owns a directory, where its root is) is immutable
//     → cached.
//   - WORKING state is NOT: `sdd task complete` deliberately writes the
//     candidate artifact to disk between its before/after validation runs,
//     and the clean-worktree rules must observe that write (cmd/sdd's
//     splitCommitPending depends on it). Clean() and FileInIndex() therefore
//     pass through uncached. Adding a new working-state query to Repo? It
//     belongs on the uncached side of this decorator.
//
// Errors are cached too: ErrNotFound is a determinate answer, and an
// environmental failure (binary missing, not a repository) does not heal
// mid-process. Slice- and byte-valued results are copied on every return —
// hit or miss — so no caller can mutate the cached answer another caller
// will receive.
//
// Memoization is opt-in via EnableMemoization, called once from cmd/sdd's
// main. It is NOT enabled under `go test`: test suites and genfixtures build
// fixture repositories, commit, validate, then commit again within one
// process, which breaks the immutability assumption the cache rests on.

var (
	memoEnabled bool

	detectMu    sync.Mutex
	detectCache = map[string]Repo{}

	memoMu    sync.Mutex
	memoCache = map[string]memoEntry{}
)

type memoEntry struct {
	val any
	err error
}

// EnableMemoization turns on process-lifetime caching of repository object
// state and VCS detection. Call it once, at process start, from a short-lived
// command that will not mutate repository state — cmd/sdd's main. Never call
// it from tests or any long-lived process.
func EnableMemoization() { memoEnabled = true }

// memoize wraps a detected adapter in the caching decorator. NoRepo is left
// bare: its every operation is a constant answer already.
func memoize(r Repo) Repo {
	if !memoEnabled {
		return r
	}
	if _, bare := r.(NoRepo); bare {
		return r
	}
	return &memoRepo{r: r, prefix: string(r.Kind()) + "\x00" + r.Root()}
}

// memoKey joins key parts with NUL, which cannot appear in paths, revisions,
// or operation names, so distinct queries cannot collide.
func memoKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

// memo returns the cached answer for key, computing and storing it on first
// use. Concurrent callers may compute duplicates; last write wins, which is
// harmless because the computation is deterministic within a process.
func memo[T any](key string, fn func() (T, error)) (T, error) {
	if !memoEnabled {
		return fn()
	}
	memoMu.Lock()
	e, ok := memoCache[key]
	memoMu.Unlock()
	if ok {
		v, _ := e.val.(T)
		return v, e.err
	}
	v, err := fn()
	memoMu.Lock()
	memoCache[key] = memoEntry{val: v, err: err}
	memoMu.Unlock()
	return v, err
}

// memoSlice is memo for slice-valued results, returning a fresh copy on
// every call so callers cannot corrupt the cached value by mutating theirs.
func memoSlice[E any](key string, fn func() ([]E, error)) ([]E, error) {
	v, err := memo(key, fn)
	if v == nil {
		return nil, err
	}
	return append([]E(nil), v...), err
}

// memoRepo is the caching decorator around a real adapter.
type memoRepo struct {
	r      Repo
	prefix string
}

// Pure or working-state operations delegate directly.
func (m *memoRepo) Kind() Kind                        { return m.r.Kind() }
func (m *memoRepo) Root() string                      { return m.r.Root() }
func (m *memoRepo) RevisionSyntaxValid(s string) bool { return m.r.RevisionSyntaxValid(s) }
func (m *memoRepo) Clean() (bool, []string, error)    { return m.r.Clean() }
func (m *memoRepo) FileInIndex(relPath string) ([]byte, error) {
	return m.r.FileInIndex(relPath)
}

// Object/ref-state operations are memoized.
func (m *memoRepo) RevisionExists(rev string) (bool, error) {
	return memo(memoKey(m.prefix, "rev-exists", rev), func() (bool, error) {
		return m.r.RevisionExists(rev)
	})
}

func (m *memoRepo) Head() (string, error) {
	return memo(memoKey(m.prefix, "head"), m.r.Head)
}

func (m *memoRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	return memo(memoKey(m.prefix, "is-ancestor", ancestor, descendant), func() (bool, error) {
		return m.r.IsAncestor(ancestor, descendant)
	})
}

func (m *memoRepo) Parents(rev string) ([]string, error) {
	return memoSlice(memoKey(m.prefix, "parents", rev), func() ([]string, error) {
		return m.r.Parents(rev)
	})
}

func (m *memoRepo) FileAt(rev, relPath string) ([]byte, error) {
	return memoSlice(memoKey(m.prefix, "file-at", rev, relPath), func() ([]byte, error) {
		return m.r.FileAt(rev, relPath)
	})
}

func (m *memoRepo) ChangedPaths(rev string) ([]string, error) {
	return memoSlice(memoKey(m.prefix, "changed-paths", rev), func() ([]string, error) {
		return m.r.ChangedPaths(rev)
	})
}

func (m *memoRepo) RevisionsAfter(rev string) ([]string, error) {
	return memoSlice(memoKey(m.prefix, "revisions-after", rev), func() ([]string, error) {
		return m.r.RevisionsAfter(rev)
	})
}

func (m *memoRepo) TrackedPaths(rev string, prefixes []string) ([]string, error) {
	key := memoKey(append([]string{m.prefix, "tracked-paths", rev}, prefixes...)...)
	return memoSlice(key, func() ([]string, error) {
		return m.r.TrackedPaths(rev, prefixes)
	})
}

// resetCaches empties both caches. Tests only: production sdd commands are
// single short-lived processes whose repository object state never changes.
func resetCaches() {
	detectMu.Lock()
	detectCache = map[string]Repo{}
	detectMu.Unlock()
	memoMu.Lock()
	memoCache = map[string]memoEntry{}
	memoMu.Unlock()
}
