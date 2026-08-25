package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Advisory file locking, so several sessions can work in one planning root
// without corrupting each other's writes.
//
// # Why a sidecar lock file
//
// Locks are taken on a sidecar (`.<artifact>.sdd-lock`), not on the artifact itself. WriteAtomic
// writes a temp file and renames it over the target (FR-24), which replaces the
// inode — a lock held on the artifact's own descriptor would be attached to the
// file that just got replaced, protecting nothing. The sidecar's inode is
// stable across the rename, so it is what every participant agrees to contend
// on. The sidecar is never read or written; only its lock state matters.
//
// # The concurrency model
//
// Readers take a shared lock: any number may hold it at once, and each sees a
// consistent snapshot because no writer can be mid-rename while they hold it.
// Writers take an exclusive lock, which excludes every reader and every other
// writer.
//
// Waiting is deliberately asymmetric:
//
//   - A reader that cannot lock immediately retries briefly and then proceeds.
//     A write is short (one rename), so contention is transient, and a read is
//     safe to repeat.
//
//   - A writer waits for the exclusive lock, and then *fails* if the artifact
//     changed while it waited. It does not silently write on top of the other
//     writer's result. The caller must re-read and re-apply — the same
//     compare-and-swap discipline `--expect` already gives an explicit caller,
//     applied automatically to concurrent ones. Blindly writing after waiting
//     would make the last writer win and silently discard the first, which is
//     exactly the corruption the lock exists to prevent.
//
// This is a compare-and-swap, not a mutex: acquiring the lock is not permission
// to write, it is permission to *check whether writing is still valid*.
//
// # Advisory, not mandatory
//
// flock/LockFileEx are advisory: they bind processes that ask, which is every
// path through this package, and do not stop an unrelated editor from writing
// the file. That is the right trade — the goal is cooperating `sdd` sessions,
// not a filesystem-level mutex — but it means the digest precondition below,
// not the lock, is what actually guarantees a write is not lost.

// lockRetryWindow bounds how long a reader retries before giving up on the
// lock and reading anyway. A writer holds the exclusive lock only across a
// rename, so anything longer than this indicates a stuck or crashed holder
// rather than ordinary contention — and blocking a read indefinitely on a
// stale lock is worse than reading bytes that are, by construction of the
// atomic write, always a complete previous version.
const lockRetryWindow = 2 * time.Second

// lockRetryInterval is the poll gap while waiting. Short enough that ordinary
// contention costs nothing perceptible, long enough not to spin a core.
const lockRetryInterval = 10 * time.Millisecond

// writeLockTimeout bounds a writer's wait for the exclusive lock. A writer
// that cannot acquire in this window reports the contention rather than
// hanging: an agent session waiting forever on a lock looks identical to a
// hung tool.
const writeLockTimeout = 10 * time.Second

// ErrLockTimeout reports that an exclusive lock could not be acquired.
var ErrLockTimeout = errors.New("timed out waiting for the file lock")

// ErrConcurrentWrite reports that the artifact changed while this writer was
// waiting for the lock, so the write it prepared is based on stale content.
// The caller must re-read and re-apply its edit.
type ErrConcurrentWrite struct {
	Path     string
	Expected string // digest the caller's content was derived from
	Found    string // digest actually on disk once the lock was held
}

func (e *ErrConcurrentWrite) Error() string {
	return fmt.Sprintf(
		"%s changed while waiting for the write lock (expected digest %s, found %s)\n"+
			"    fix: another session wrote this artifact first. Re-read it, reapply "+
			"your edit to the current content, and retry",
		e.Path, shortDigest(e.Expected), shortDigest(e.Found))
}

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	if d == "" {
		return "<absent>"
	}
	return d
}

// lockPath is the sidecar whose lock state guards one artifact.
//
// The dot prefix and the shared suffix matter: the sidecar sits next to the
// artifact (so it shares its filesystem, which flock and LockFileEx require),
// but must never look like an artifact. Artifact discovery only walks `.md`
// files, and `.sdd-lock` is neither a Markdown file nor a name a user would
// author, so a stray sidecar cannot be mistaken for planning content.
func lockPath(path string) string {
	dir, base := filepath.Split(path)
	return filepath.Join(dir, "."+base+".sdd-lock")
}

// fileLock is one held advisory lock. Release is idempotent.
type fileLock struct {
	f        *os.File
	released bool
}

func (l *fileLock) Release() {
	if l == nil || l.released {
		return
	}
	l.released = true
	_ = unlockFile(l.f)
	_ = l.f.Close()
}

// openLockFile opens (creating if needed) the sidecar for path. It never
// creates directories: a MkdirAll here ran on READ locks too, so merely
// probing a nonexistent path (`store.Read` on a create target, a refused
// apply) manufactured the target's whole directory chain as a side effect —
// including at wrong, unresolved locations. A directory that does not exist
// is the writer's to create (acquireExclusive), and for a reader it simply
// means there is nothing to lock.
func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(lockPath(path), os.O_CREATE|os.O_RDWR, 0o644)
}

// acquireShared takes a read lock, retrying briefly under contention. It
// returns a nil lock (and no error) when the lock could not be taken within
// the retry window: a read proceeds regardless, because an atomic write leaves
// only complete versions on disk, and refusing to read would turn one stuck
// writer into a planning root nobody can inspect.
func acquireShared(path string) *fileLock {
	f, err := openLockFile(path)
	if err != nil {
		return nil // unlockable location; read unguarded rather than fail
	}
	deadline := time.Now().Add(lockRetryWindow)
	for {
		if err := lockShared(f); err == nil {
			return &fileLock{f: f}
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil
		}
		time.Sleep(lockRetryInterval)
	}
}

// acquireExclusive takes a write lock, waiting up to writeLockTimeout. The
// writer is the one party entitled to create the target's directory (it is
// about to create the artifact itself), so the MkdirAll lives here rather
// than in openLockFile where reads would inherit it.
func acquireExclusive(path string) (*fileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for %s: %w", path, err)
	}
	f, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("opening lock for %s: %w", path, err)
	}
	deadline := time.Now().Add(writeLockTimeout)
	for {
		if err := lockExclusive(f); err == nil {
			return &fileLock{f: f}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%s: %w after %s", path, ErrLockTimeout, writeLockTimeout)
		}
		time.Sleep(lockRetryInterval)
	}
}
