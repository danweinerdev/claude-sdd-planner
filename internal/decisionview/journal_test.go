package decisionview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

const (
	journalOwnerA = OwnerID("10000000-0000-4000-8000-000000000001")
	journalOwnerB = OwnerID("20000000-0000-4000-8000-000000000002")
	journalLedger = CollectionID("30000000-0000-4000-8000-000000000003")
)

func journalPreview(t *testing.T, operation string) *PreviewEnvelope {
	t.Helper()
	before := &ResolvedView{Version: 1, OwnerID: journalOwnerA, LocalID: journalLedger, Resolution: ResolutionComplete}
	after := &ResolvedView{Version: 1, OwnerID: journalOwnerA, LocalID: journalLedger, Resolution: ResolutionComplete}
	p, err := NewPreviewEnvelope(
		"adopt",
		operation,
		"2026-09-08",
		json.RawMessage(`{"mode":"fork"}`),
		[]PreviewFileChange{{
			Root:  SourceRootPlanning,
			Path:  "Decisions/fork.md",
			After: "approved fork bytes\n",
		}},
		nil,
		before,
		after,
	)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestForkJournalPublicRoundTrip(t *testing.T) {
	repo, planning := storeFixture(t)
	store, err := OpenLocalStore(repo, planning, "shared-collection")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	journal, err := NewForkJournal(journalOwnerA, []CollectionID{journalLedger}, journalPreview(t, "operation-round-trip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveForkJournal(context.Background(), journal); err != nil {
		t.Fatalf("save planning-root journal and barrier: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenLocalStore(repo, planning, "shared-collection")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.LoadForkJournal(journal.OperationID)
	if err != nil {
		t.Fatalf("load journal through public API: %v", err)
	}
	if !reflect.DeepEqual(got, journal) {
		t.Fatalf("journal round trip changed operation state:\nwant: %#v\n got: %#v", journal, got)
	}
	barrier, err := store.InspectForkBarrier(journalLedger)
	if err != nil || barrier == nil || barrier.OperationID != journal.OperationID || barrier.OwnerID != journalOwnerA {
		t.Fatalf("shared pending barrier did not round trip: %#v, %v", barrier, err)
	}
	journalPath := filepath.Join(planning, "Decisions", ".fork-state", string(journalLedger), "journals", journal.OperationID+".json")
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("journal was not private planning-root state at %s: %v", journalPath, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "Decisions", ".fork-state")); !os.IsNotExist(err) {
		t.Fatalf("journal support escaped the planning root: %v", err)
	}
}

func TestForkJournalSharedBarrierNoOpGuard(t *testing.T) {
	repo, planning := storeFixture(t)
	first, err := OpenLocalStore(repo, planning, "owner-a-checkout")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenLocalStore(repo, planning, "owner-b-checkout")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	a, err := NewForkJournal(journalOwnerA, []CollectionID{journalLedger}, journalPreview(t, "operation-a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewForkJournal(journalOwnerB, []CollectionID{journalLedger}, journalPreview(t, "operation-b"))
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)
	lockAttempt := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var enterOnce sync.Once
	first.afterCheck = func() {
		blocked := false
		enterOnce.Do(func() {
			blocked = true
			close(entered)
		})
		if blocked {
			<-release
		}
	}
	baseTryLock := second.tryLock
	var attemptOnce sync.Once
	second.tryLock = func(f *os.File) error {
		err := baseTryLock(f)
		attemptOnce.Do(func() { lockAttempt <- err })
		return err
	}
	go func() { firstResult <- first.SaveForkJournal(ctx, a) }()
	select {
	case <-entered:
	case err := <-firstResult:
		t.Fatalf("first owner did not enter the shared journal guard: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() { secondResult <- second.SaveForkJournal(ctx, b) }()
	var lockErr error
	select {
	case lockErr = <-lockAttempt:
	case <-ctx.Done():
		close(release)
		t.Fatal(ctx.Err())
	}
	close(release)
	firstErr, secondErr := <-firstResult, <-secondResult
	if lockErr == nil {
		t.Fatal("competing owner entered while the shared barrier guard was held; exclusion is a no-op")
	}
	if firstErr != nil || !errors.Is(secondErr, ErrForkBarrierPending) {
		t.Fatalf("serialized competing owner outcomes: %v / %v", firstErr, secondErr)
	}
	barrier, err := second.InspectForkBarrier(journalLedger)
	if err != nil || barrier == nil || barrier.OwnerID != journalOwnerA || barrier.OperationID != a.OperationID {
		t.Fatalf("competing checkout did not observe first owner's authority barrier: %#v, %v", barrier, err)
	}
}

func TestForkJournalReadOnlyAndPrivate(t *testing.T) {
	repo, planning := storeFixture(t)
	store, err := OpenLocalStore(repo, planning, "read-only-checkout")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoBefore, err := os.ReadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	planningBefore, err := os.ReadDir(planning)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadForkJournal("missing-operation")
	if err != nil || journal != nil {
		t.Fatalf("read-only missing journal inspection should be absent without error: %#v, %v", journal, err)
	}
	barrier, err := store.InspectForkBarrier(journalLedger)
	if err != nil || barrier != nil {
		t.Fatalf("read-only missing barrier inspection should be absent without error: %#v, %v", barrier, err)
	}
	repoAfter, err := os.ReadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	planningAfter, err := os.ReadDir(planning)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repoAfter, repoBefore) || !reflect.DeepEqual(planningAfter, planningBefore) {
		t.Fatalf("read-only inspection created support files: repo %v -> %v; planning %v -> %v", repoBefore, repoAfter, planningBefore, planningAfter)
	}
}

func TestForkJournalRejectsCorruptOrPrematureBarrier(t *testing.T) {
	repo, planning := storeFixture(t)
	s, err := OpenLocalStore(repo, planning, "writer")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := journalPreview(t, "operation-integrity")
	journal, err := NewForkJournal(journalOwnerA, []CollectionID{journalLedger}, p)
	if err != nil {
		t.Fatal(err)
	}
	p.Changes[0].After = "caller mutation"
	if journal.Preview.Changes[0].After == p.Changes[0].After {
		t.Fatal("journal did not capture independent preview bytes")
	}
	if err := s.SaveForkJournal(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	barrierPath := filepath.Join(planning, filepath.FromSlash(forkBarrierPath(journalLedger)))
	raw, err := os.ReadFile(barrierPath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["status"] = "committed"
	changed, _ := json.Marshal(fields)
	if err := os.WriteFile(barrierPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InspectForkBarrier(journalLedger); err == nil {
		t.Fatal("barrier claimed commit while journal remained pending")
	}
	fields["status"] = "pending"
	delete(fields, "version")
	changed, _ = json.Marshal(fields)
	if err := os.WriteFile(barrierPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InspectForkBarrier(journalLedger); err == nil {
		t.Fatal("missing barrier version accepted")
	}
	if _, err := s.LoadForkJournal("../escape"); err == nil {
		t.Fatal("operation identity escaped journal storage")
	}
}
