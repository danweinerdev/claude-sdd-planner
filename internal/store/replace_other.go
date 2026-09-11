//go:build !windows

package store

import "os"

func replaceFile(oldpath, newpath string, beforeRetry func() error) error {
	return os.Rename(oldpath, newpath)
}
