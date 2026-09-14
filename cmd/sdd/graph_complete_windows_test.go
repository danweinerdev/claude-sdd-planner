//go:build windows

package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

func obstructReviewsRead(t *testing.T, reviews string) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(reviews)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("holding reviews directory open: %v", err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(h); err != nil {
			t.Errorf("closing reviews directory handle: %v", err)
		}
	})
}
