//go:build !windows

package decisionview

import (
	"errors"
	"os"
	"syscall"
)

func lockLocalFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
func unlockLocalFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func localLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
func publishLocalFile(parent *os.Root, temp, base string, existing bool) error {
	if err := parent.Rename(temp, base); err != nil {
		return err
	}
	dir, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
