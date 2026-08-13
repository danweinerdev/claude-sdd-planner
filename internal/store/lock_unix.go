//go:build !windows

package store

import (
	"os"

	"golang.org/x/sys/unix"
)

// flock is used rather than POSIX record locks (fcntl/F_SETLK) deliberately.
// Record locks are per-process and released when *any* descriptor on the file
// is closed, which makes them unusable in a library: an unrelated Read of the
// same path elsewhere in the process would drop a lock this package is holding.
// flock is per-descriptor, so each lock's lifetime is exactly the lifetime of
// the fileLock that owns it.
//
// LOCK_NB on every attempt keeps the wait loop in Go, where the deadline and
// retry interval are visible and testable, rather than blocking in the kernel
// where a caller cannot bound it.

func lockShared(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
}

func lockExclusive(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
