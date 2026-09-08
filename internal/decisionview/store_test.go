package decisionview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func storeFixture(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	planning := filepath.Join(repo, ".plans")
	if err := os.MkdirAll(planning, 0o755); err != nil {
		t.Fatal(err)
	}
	return repo, planning
}
func TestForkStorePublicRoundTrip(t *testing.T) {
	repo, planning := storeFixture(t)
	s, err := OpenLocalStore(repo, planning, "ledger-1")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := s.Read("Decisions/fork.md")
	if err != nil || missing.Exists {
		t.Fatalf("missing read: %+v %v", missing, err)
	}
	data := []byte("versioned\n\"fork\" bytes 雪")
	if err := s.WriteExpected(context.Background(), "Decisions/fork.md", data, ""); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenLocalStore(repo, planning, "ledger-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Read("Decisions/fork.md")
	if err != nil || !got.Exists || !bytes.Equal(got.Bytes, data) {
		t.Fatalf("public save/load: %+v %v", got, err)
	}
	if err := s.WriteExpected(context.Background(), "Decisions/fork.md", []byte("lost update"), ""); !errors.Is(err, ErrStoreConflict) {
		t.Fatalf("stale expectation: %v", err)
	}
	if err := s.WriteExpected(context.Background(), "Decisions/fork.md", []byte("new"), got.Digest); err != nil {
		t.Fatal(err)
	}
}

// The second writer observes the first actual nonblocking lock attempt while
// writer one holds the critical section. This fails if locking becomes a no-op,
// without depending on sleeps to arrange both writers' read/check windows.
func storeGuardScenario(t *testing.T, noOp bool) error {
	t.Helper()
	repo, planning := storeFixture(t)
	if err := os.MkdirAll(filepath.Join(planning, "Decisions"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(planning, "Decisions", "fork.md"), []byte("old"), 0o644); err != nil {
		return err
	}
	a, err := OpenLocalStore(repo, planning, "ledger-1")
	if err != nil {
		return err
	}
	defer a.Close()
	b, err := OpenLocalStore(repo, planning, "ledger-1")
	if err != nil {
		return err
	}
	defer b.Close()
	old, err := a.Read("Decisions/fork.md")
	if err != nil {
		return err
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	attempt := make(chan error, 1)
	second := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.afterCheck = func() { close(entered); <-release }
	base := b.tryLock
	if noOp {
		a.tryLock = func(*os.File) error { return nil }
		base = func(*os.File) error { return nil }
	}
	var once sync.Once
	b.tryLock = func(f *os.File) error {
		var e error
		if base != nil {
			e = base(f)
		}
		once.Do(func() { attempt <- e })
		return e
	}
	go func() { first <- a.WriteExpected(ctx, "Decisions/fork.md", []byte("first"), old.Digest) }()
	select {
	case <-entered:
	case err := <-first:
		return fmt.Errorf("writer did not enter guarded check: %v", err)
	case <-ctx.Done():
		return ctx.Err()
	}
	go func() { second <- b.WriteExpected(ctx, "Decisions/fork.md", []byte("second"), old.Digest) }()
	var lockErr error
	select {
	case lockErr = <-attempt:
	case <-ctx.Done():
		close(release)
		return ctx.Err()
	}
	close(release)
	e1, e2 := <-first, <-second
	if lockErr == nil {
		return errors.New("guard did not exclude the second writer")
	}
	if e1 != nil || !errors.Is(e2, ErrStoreConflict) {
		return fmt.Errorf("guarded writer outcomes: %v / %v", e1, e2)
	}
	return nil
}
func TestForkStoreCASNoOpGuard(t *testing.T) {
	if err := storeGuardScenario(t, false); err != nil {
		t.Fatal(err)
	}
}
func TestForkStoreNoOpNegativeControl(t *testing.T) {
	if err := storeGuardScenario(t, true); err == nil {
		t.Fatal("the independent no-op guard control failed to detect exclusion loss")
	}
}

func TestForkStorePreservesRootFootprint(t *testing.T) {
	repo, planning := storeFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "unrelated.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenLocalStore(repo, planning, "ledger-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before, err := os.ReadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("missing/ledger.md"); err != nil {
		t.Fatal(err)
	}
	inside, err := os.ReadDir(planning)
	if err != nil || len(inside) != 0 {
		t.Fatal("read created support state")
	}
	if err := s.WriteExpected(context.Background(), "Decisions/fork.md", []byte("approved"), ""); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(repo)
	if err != nil || len(after) != len(before) {
		t.Fatal("write polluted represented repository root")
	}
	data, err := os.ReadFile(filepath.Join(repo, "unrelated.txt"))
	if err != nil || string(data) != "keep" {
		t.Fatal("unrelated work changed")
	}
	if err := s.WriteExpected(context.Background(), "../outside.md", []byte("escape"), ""); err == nil {
		t.Fatal("write escaped root")
	}
}
