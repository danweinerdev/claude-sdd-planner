//go:build windows

package store

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAtomicReplaceTransientSharing(t *testing.T) {
	t.Run("retries both transient Windows errors with the same staged file", func(t *testing.T) {
		for _, errno := range []syscall.Errno{syscall.ERROR_ACCESS_DENIED, windows.ERROR_SHARING_VIOLATION} {
			now := time.Unix(0, 0)
			var calls int
			var staged string
			err := replaceFileWith("staged", "target", func() error { return nil }, func(oldpath, newpath string) error {
				calls++
				if staged == "" {
					staged = oldpath
				} else if oldpath != staged {
					t.Fatalf("retry changed staged file from %q to %q", staged, oldpath)
				}
				if calls < 3 {
					return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errno}
				}
				return nil
			}, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
			if err != nil {
				t.Fatalf("transient errno %d was not retried: %v", errno, err)
			}
			if calls != 3 {
				t.Errorf("rename calls = %d, want 3", calls)
			}
		}
	})

	t.Run("publishes after a real held reader releases the destination", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "artifact.md")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		h, err := windows.CreateFile(name, windows.GENERIC_READ,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			t.Fatalf("holding destination open: %v", err)
		}
		released := make(chan struct{})
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = windows.CloseHandle(h)
			close(released)
		}()
		if err := WriteAtomicExpecting(path, "new", Digest("old")); err != nil {
			<-released
			t.Fatalf("write did not survive transient held reader: %v", err)
		}
		<-released
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "new" {
			t.Fatalf("published bytes = %q, err = %v; want new", got, err)
		}
	})
}

func TestAtomicReplaceFailurePreservesState(t *testing.T) {
	t.Run("changed target during retry is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "artifact.md")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		now := time.Unix(0, 0)
		calls := 0
		replace := func(oldpath, newpath string, beforeRetry func() error) error {
			return replaceFileWith(oldpath, newpath, beforeRetry, func(string, string) error {
				calls++
				if calls == 1 {
					if err := os.WriteFile(path, []byte("other writer"), 0o644); err != nil {
						t.Fatal(err)
					}
					return windows.ERROR_SHARING_VIOLATION
				}
				return nil
			}, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
		}
		err := writeAtomicCheckedWith(path, "ours", Digest("old"), true, replace)
		var conflict *ErrConcurrentWrite
		if !errors.As(err, &conflict) {
			t.Fatalf("changed target error = %T %v, want *ErrConcurrentWrite", err, err)
		}
		if calls != 1 {
			t.Errorf("rename called %d times after CAS changed, want 1", calls)
		}
		assertFileContent(t, path, "other writer")
		assertNoOwnedTemps(t, path)
	})

	t.Run("persistent sharing failure is bounded and preserves destination", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "artifact.md")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		now := time.Unix(0, 0)
		start := now
		calls := 0
		replace := func(oldpath, newpath string, beforeRetry func() error) error {
			return replaceFileWith(oldpath, newpath, beforeRetry, func(string, string) error {
				calls++
				return syscall.ERROR_ACCESS_DENIED
			}, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
		}
		err := writeAtomicCheckedWith(path, "ours", Digest("old"), true, replace)
		if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			t.Fatalf("error = %v, want original access-denied cause", err)
		}
		if calls < 2 {
			t.Fatalf("rename calls = %d, want retries", calls)
		}
		if elapsed := now.Sub(start); elapsed > 2*time.Second {
			t.Fatalf("retry elapsed = %s, exceeds 2s budget", elapsed)
		}
		assertFileContent(t, path, "old")
		assertNoOwnedTemps(t, path)
	})

	t.Run("new target appearing during retry is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "artifact.md")
		now := time.Unix(0, 0)
		calls := 0
		replace := func(oldpath, newpath string, beforeRetry func() error) error {
			return replaceFileWith(oldpath, newpath, beforeRetry, func(string, string) error {
				calls++
				if calls == 1 {
					if err := os.WriteFile(path, []byte("appeared"), 0o644); err != nil {
						t.Fatal(err)
					}
					return syscall.ERROR_ACCESS_DENIED
				}
				return nil
			}, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
		}
		err := writeAtomicCheckedWith(path, "ours", "", true, replace)
		var conflict *ErrConcurrentWrite
		if !errors.As(err, &conflict) {
			t.Fatalf("appeared target error = %T %v, want *ErrConcurrentWrite", err, err)
		}
		if calls != 1 {
			t.Errorf("rename called %d times after target appeared, want 1", calls)
		}
		assertFileContent(t, path, "appeared")
		assertNoOwnedTemps(t, path)
	})

	t.Run("sleep overshoot cannot rename after deadline", func(t *testing.T) {
		now := time.Unix(0, 0)
		calls := 0
		original := &os.LinkError{
			Op:  "rename",
			Old: "staged",
			New: "target",
			Err: windows.ERROR_SHARING_VIOLATION,
		}
		err := replaceFileWith("staged", "target", func() error {
			t.Fatal("CAS check ran after deadline")
			return nil
		}, func(string, string) error {
			calls++
			return original
		}, func() time.Time { return now }, func(time.Duration) { now = now.Add(3 * time.Second) })
		if err != original {
			t.Fatalf("error = %v, want original wrapped rename error %v", err, original)
		}
		if calls != 1 {
			t.Fatalf("rename calls = %d, want no retry after deadline", calls)
		}
	})

	t.Run("digest reread crossing deadline cannot rename again", func(t *testing.T) {
		now := time.Unix(0, 0)
		calls := 0
		original := &os.LinkError{
			Op:  "rename",
			Old: "staged",
			New: "target",
			Err: syscall.ERROR_ACCESS_DENIED,
		}
		err := replaceFileWith("staged", "target", func() error {
			now = now.Add(replaceRetryWindow)
			return nil
		}, func(string, string) error {
			calls++
			return original
		}, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
		if err != original {
			t.Fatalf("error = %v, want original wrapped rename error %v", err, original)
		}
		if calls != 1 {
			t.Fatalf("rename calls = %d, want no retry after digest reread crossed deadline", calls)
		}
	})

	t.Run("unrelated errors are not retried", func(t *testing.T) {
		calls := 0
		unrelated := syscall.Errno(87) // ERROR_INVALID_PARAMETER
		err := replaceFileWith("staged", "target", func() error { return nil }, func(string, string) error {
			calls++
			return unrelated
		}, time.Now, func(time.Duration) { t.Fatal("unrelated error slept") })
		if !errors.Is(err, unrelated) {
			t.Fatalf("error = %v, want %v", err, unrelated)
		}
		if calls != 1 {
			t.Fatalf("rename calls = %d, want 1", calls)
		}
	})

	t.Run("pre-retry read failure is propagated", func(t *testing.T) {
		readErr := errors.New("digest reread failed")
		calls := 0
		now := time.Unix(0, 0)
		err := replaceFileWith("staged", "target", func() error { return readErr }, func(string, string) error {
			calls++
			return windows.ERROR_SHARING_VIOLATION
		}, func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) })
		if !errors.Is(err, readErr) {
			t.Fatalf("error = %v, want reread failure", err)
		}
		if calls != 1 {
			t.Fatalf("rename calls = %d after reread failure, want 1", calls)
		}
	})
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("content = %q, err = %v; want %q", got, err, want)
	}
}

func assertNoOwnedTemps(t *testing.T, path string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".sdd-*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		if match != lockPath(path) {
			t.Fatalf("operation-owned temp file remains: %s", match)
		}
	}
}
