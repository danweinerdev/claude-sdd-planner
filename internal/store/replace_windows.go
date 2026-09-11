//go:build windows

package store

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	replaceRetryWindow   = 2 * time.Second
	replaceRetryInterval = 10 * time.Millisecond
)

func replaceFile(oldpath, newpath string, beforeRetry func() error) error {
	return replaceFileWith(oldpath, newpath, beforeRetry, os.Rename, time.Now, time.Sleep)
}

// replaceFileWith keeps retry dependencies local to a call so tests can drive
// error and clock behavior without mutable process-global hooks.
func replaceFileWith(
	oldpath, newpath string,
	beforeRetry func() error,
	rename func(string, string) error,
	now func() time.Time,
	sleep func(time.Duration),
) error {
	deadline := now().Add(replaceRetryWindow)
	for {
		err := rename(oldpath, newpath)
		if err == nil || !isRetryableReplaceError(err) {
			return err
		}

		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return err
		}
		pause := replaceRetryInterval
		if remaining < pause {
			pause = remaining
		}
		sleep(pause)
		// The clock seam may advance farther than requested. Never issue a
		// rename after the deadline, even when a sleep overshoots it.
		if !now().Before(deadline) {
			return err
		}
		if err := beforeRetry(); err != nil {
			return err
		}
		// Re-reading the expected digest can itself consume the remaining
		// budget. Do not begin another rename after that I/O crosses the
		// deadline.
		if !now().Before(deadline) {
			return err
		}
	}
}

func isRetryableReplaceError(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
