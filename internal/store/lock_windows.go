//go:build windows

package store

import (
	"os"

	"golang.org/x/sys/windows"
)

// LockFileEx is Windows' advisory byte-range lock. It is taken over the whole
// possible range so the semantics match flock's whole-file locking on Unix:
// the sidecar carries no data, only lock state, so the range is arbitrary as
// long as every participant agrees on it.
//
// LOCKFILE_FAIL_IMMEDIATELY mirrors LOCK_NB — the wait loop stays in Go, where
// the deadline is bounded and testable, rather than blocking in the kernel.

const (
	// Lock the entire 64-bit range, the conventional "whole file" spelling.
	lockRangeLow  = ^uint32(0)
	lockRangeHigh = ^uint32(0)
)

func lockFileRange(f *os.File, flags uint32) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0,
		lockRangeLow, lockRangeHigh, ol)
}

func lockShared(f *os.File) error {
	return lockFileRange(f, windows.LOCKFILE_FAIL_IMMEDIATELY)
}

func lockExclusive(f *os.File) error {
	return lockFileRange(f, windows.LOCKFILE_FAIL_IMMEDIATELY|windows.LOCKFILE_EXCLUSIVE_LOCK)
}

func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0,
		lockRangeLow, lockRangeHigh, ol)
}
