package decisionview

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func lockLocalFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}
func unlockLocalFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
func localLockBusy(err error) bool { return errors.Is(err, windows.ERROR_LOCK_VIOLATION) }

var replaceLocalFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func publishLocalFile(parent *os.Root, temp, base string, existing bool) error {
	from, err := windows.UTF16PtrFromString(filepath.Join(parent.Name(), temp))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(parent.Name(), base))
	if err != nil {
		return err
	}
	if !existing {
		return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
	}
	backupName := temp + ".old"
	if _, err := parent.Lstat(backupName); !os.IsNotExist(err) {
		if err == nil {
			return os.ErrExist
		}
		return err
	}
	backup, err := windows.UTF16PtrFromString(filepath.Join(parent.Name(), backupName))
	if err != nil {
		return err
	}
	result, _, callErr := replaceLocalFile.Call(uintptr(unsafe.Pointer(to)), uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(backup)), 0, 0, 0)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return callErr
	}
	// A failed cleanup is still a committed publication, not a rollback.
	return parent.Remove(backupName)
}
