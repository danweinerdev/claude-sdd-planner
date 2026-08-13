package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSharedLocksDoNotExcludeEachOther pins the property that makes reads
// cheap: any number of readers hold the lock at once, so a planning root being
// inspected by several sessions never serializes on itself.
func TestSharedLocksDoNotExcludeEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := acquireShared(path)
	if a == nil {
		t.Fatal("first shared lock was not acquired")
	}
	defer a.Release()

	b := acquireShared(path)
	if b == nil {
		t.Fatal("a second reader was blocked by the first; shared locks must stack")
	}
	b.Release()
}

// TestExclusiveLockExcludesReaders is the other half: while a writer holds the
// lock, a reader cannot take it and falls back after its retry window rather
// than blocking forever.
func TestExclusiveLockExcludesReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := acquireExclusive(path)
	if err != nil {
		t.Fatalf("exclusive lock: %v", err)
	}
	defer w.Release()

	start := time.Now()
	if r := acquireShared(path); r != nil {
		r.Release()
		t.Error("a reader acquired a shared lock while an exclusive lock was held")
	}
	// It must have actually waited — an immediate give-up would mean the
	// retry loop is not running.
	if waited := time.Since(start); waited < lockRetryInterval {
		t.Errorf("reader gave up after %s without retrying", waited)
	}
}

// TestExclusiveLocksSerialize proves two writers cannot hold the lock at once.
func TestExclusiveLocksSerialize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := acquireExclusive(path)
	if err != nil {
		t.Fatalf("first exclusive lock: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		second, err := acquireExclusive(path)
		if err == nil {
			second.Release()
			close(acquired)
		}
	}()

	select {
	case <-acquired:
		t.Fatal("a second writer acquired the exclusive lock while the first held it")
	case <-time.After(50 * time.Millisecond):
		// Correct: still waiting.
	}
	first.Release()

	select {
	case <-acquired:
		// Correct: the wait completed once the holder released.
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting writer never acquired the lock after it was released")
	}
}

// TestWriteAtomicExpectingRefusesStaleContent is the core safety property: a
// writer whose content was derived from a version that has since been replaced
// must be refused, not silently applied on top. Without this, the second
// writer to arrive wins and the first writer's work vanishes with no error
// anywhere.
func TestWriteAtomicExpectingRefusesStaleContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := Digest("original")

	// Another session writes first.
	if err := WriteAtomicExpecting(path, "from session A", stale); err != nil {
		t.Fatalf("the first writer should succeed: %v", err)
	}

	// This session still holds the digest it read before A wrote.
	err := WriteAtomicExpecting(path, "from session B", stale)
	if err == nil {
		t.Fatal("a write based on superseded content was accepted; session A's write is lost")
	}
	var conflict *ErrConcurrentWrite
	if !asConcurrentWrite(err, &conflict) {
		t.Fatalf("want *ErrConcurrentWrite, got %T: %v", err, err)
	}
	if !strings.Contains(conflict.Error(), "Re-read it") &&
		!strings.Contains(conflict.Error(), "re-read") {
		t.Errorf("the error must tell the caller to re-read and retry: %v", conflict)
	}
	// A's content survives; B's was refused, not merged or clobbered.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from session A" {
		t.Errorf("content = %q, want session A's write preserved", got)
	}
}

// TestWriteAtomicExpectingAcceptsUnchangedContent confirms the check is a
// compare-and-swap and not merely a refusal: an uncontended write proceeds.
func TestWriteAtomicExpectingAcceptsUnchangedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.md")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicExpecting(path, "updated", Digest("original")); err != nil {
		t.Fatalf("an uncontended write was refused: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "updated" {
		t.Errorf("content = %q, want %q", got, "updated")
	}
	// Creating a new artifact: the expected digest is the empty string.
	fresh := filepath.Join(t.TempDir(), "new.md")
	if err := WriteAtomicExpecting(fresh, "created", ""); err != nil {
		t.Errorf("creating a new artifact was refused: %v", err)
	}
}

// TestConcurrentWritersSerializeAndOnlyOneWins is the end-to-end statement of
// the model: many sessions racing on one artifact must produce exactly one
// winner per generation, and every loser must be told to retry rather than
// having its write silently dropped. A last-writer-wins implementation passes
// none of these assertions — it would report N successes and keep one result.
func TestConcurrentWritersSerializeAndOnlyOneWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.md")
	if err := os.WriteFile(path, []byte("gen0"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := Digest("gen0")

	const writers = 8
	var wg sync.WaitGroup
	results := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = WriteAtomicExpecting(path, fmt.Sprintf("from writer %d", i), base)
		}(i)
	}
	wg.Wait()

	wins, conflicts := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			wins++
		case isConcurrentWrite(err):
			conflicts++
		default:
			t.Errorf("writer %d failed for an unexpected reason: %v", i, err)
		}
	}
	if wins != 1 {
		t.Errorf("want exactly one winner, got %d (the others silently overwrote each other)", wins)
	}
	if conflicts != writers-1 {
		t.Errorf("want %d writers told to retry, got %d", writers-1, conflicts)
	}
	// The surviving content is one writer's, intact — never a blend.
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "from writer ") {
		t.Errorf("content = %q, want exactly one writer's complete output", got)
	}
}

// TestReadsSeeCompleteVersionsUnderConcurrentWrites checks the reader side of
// the same race: a reader must never observe a partial or empty artifact while
// writers are replacing it.
func TestReadsSeeCompleteVersionsUnderConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.md")
	full := strings.Repeat("content line\n", 500)
	if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Unchecked writes, to model a writer that always wins.
			_ = WriteAtomic(path, full)
		}
	}()

	for i := 0; i < 200; i++ {
		art, err := Read(path)
		if err != nil {
			t.Fatalf("read %d failed: %v", i, err)
		}
		if !art.Exists {
			t.Fatalf("read %d saw the artifact as absent mid-write", i)
		}
		if art.Source != full {
			t.Fatalf("read %d saw a partial artifact (%d of %d bytes)",
				i, len(art.Source), len(full))
		}
	}
	close(stop)
	wg.Wait()
}

func asConcurrentWrite(err error, target **ErrConcurrentWrite) bool {
	c, ok := err.(*ErrConcurrentWrite)
	if ok {
		*target = c
	}
	return ok
}

func isConcurrentWrite(err error) bool {
	_, ok := err.(*ErrConcurrentWrite)
	return ok
}
