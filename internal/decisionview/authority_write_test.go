package decisionview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type singlePublicationFixture struct {
	tx      *transactionFixture
	local   *Collection
	parent  *Collection
	preview *PreviewEnvelope
}

func newSinglePublicationFixture(t *testing.T, operation, operationID, statement string) *singlePublicationFixture {
	t.Helper()
	f := newTransactionFixture(t, "single-publication-adoption-"+operationID)
	local, parent := persistAdoptedTransactionFixture(t, f)
	proposal, err := json.Marshal(map[string]any{
		"version":       1,
		"operation":     operation,
		"operationId":   operationID,
		"date":          "2026-09-09",
		"target":        "ledger:" + string(parent.ID) + ":D-0001",
		"replacement":   "",
		"kind":          "decision",
		"decidedBy":     "user-approved",
		"statement":     statement,
		"rationale":     "approved exact single-file publication",
		"scope":         []string{"internal/decisionview"},
		"confirmation":  "publish these exact complete ledger bytes",
		"rejected":      []string{},
		"tags":          []string{"fork"},
		"reversibility": "two-way",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewForkOverride(ForkOverrideSnapshot{Local: local, Collections: map[CollectionID]*Collection{local.ID: local, parent.ID: parent}}, proposal)
	if err != nil {
		t.Fatalf("build real %s preview: %v", operation, err)
	}
	return &singlePublicationFixture{tx: f, local: local, parent: parent, preview: preview.Envelope}
}

func (f *singlePublicationFixture) publication(t *testing.T) *ForkAuthorityPublication {
	t.Helper()
	p, err := NewForkAuthorityPublication(ForkAuthorityPublicationRequest{
		Repository: f.tx.repository, Planning: f.tx.planning, OwnerID: journalOwnerA,
		Collection: f.local.ID, Preview: f.preview, ApprovalDigest: f.preview.Digest,
	})
	if err != nil {
		t.Fatalf("construct publication: %v", err)
	}
	return p
}

func persistSinglePreview(t *testing.T, f *singlePublicationFixture) {
	t.Helper()
	if err := os.WriteFile(f.tx.forkPath, []byte(f.preview.Changes[0].After), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := LoadCollection(Roots{Repository: f.tx.repository, Planning: f.tx.planning}, f.local.ID, f.local.Locator)
	if err != nil {
		t.Fatalf("reload preview-generated collection: %v", err)
	}
	f.local = local
}

func assertSinglePublication(t *testing.T, f *singlePublicationFixture) {
	t.Helper()
	parentBefore := append([]byte(nil), f.parent.Files[0].Source...)
	configBefore := readTransactionBytes(t, f.tx.configPath)
	result, err := f.publication(t).Apply(context.Background())
	if err != nil {
		t.Errorf("publish exact approved %s envelope: %v", f.preview.Operation, err)
		return
	}
	if result == nil || result.OperationID != f.preview.OperationID || result.Replayed || result.Outcome != ForkAuthorityCommitted {
		t.Errorf("first publication result = %#v", result)
		return
	}
	if got := readTransactionBytes(t, f.tx.forkPath); !bytes.Equal(got, []byte(f.preview.Changes[0].After)) {
		t.Errorf("persisted local bytes differ from approved envelope")
	}
	loaded, loadErr := LoadCollection(Roots{Repository: f.tx.repository, Planning: f.tx.planning}, f.local.ID, f.local.Locator)
	if loadErr != nil || loaded == nil || !bytes.Equal(loaded.Files[0].Source, []byte(f.preview.Changes[0].After)) {
		t.Errorf("fresh public load did not round trip valid published collection: collection=%#v err=%v", loaded, loadErr)
	}
	if got := readTransactionBytes(t, f.tx.sourcePath); !bytes.Equal(got, parentBefore) {
		t.Errorf("single-file publication wrote inherited authority")
	}
	if got := readTransactionBytes(t, f.tx.configPath); !bytes.Equal(got, configBefore) {
		t.Errorf("single-file publication mutated repository config")
	}
	store, openErr := OpenLocalStore(f.tx.repository, f.tx.planning, "single-publication-inspection")
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer store.Close()
	barrier, barrierErr := store.InspectForkBarrier(f.local.ID)
	if barrierErr != nil || barrier != nil {
		t.Errorf("single-file publication left a transaction barrier: %#v, %v", barrier, barrierErr)
	}
	if _, statErr := os.Stat(filepath.Join(f.tx.planning, filepath.FromSlash(forkJournalPath(f.local.ID, f.preview.OperationID)))); !os.IsNotExist(statErr) {
		t.Errorf("single-file publication left a permanent multi-file journal: %v", statErr)
	}
}

func TestForkSinglePublicationRoundTrip(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		assertSinglePublication(t, newSinglePublicationFixture(t, "override", "single-roundtrip-override", "approved replacement authority"))
	})
	t.Run("reconcile", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-reconcile-basis", "first replacement authority")
		persistSinglePreview(t, f)
		proposal := json.RawMessage(`{"version":1,"operation":"reconcile","operationId":"single-roundtrip-reconcile","date":"2026-09-09","target":"ledger:` + string(f.parent.ID) + `:D-0001","replacement":"ledger:` + string(f.local.ID) + `:D-0001","kind":"decision","decidedBy":"user-approved","statement":"reconciled replacement authority","rationale":"approved exact reconciliation","scope":["internal/decisionview"],"confirmation":"publish these exact complete ledger bytes","rejected":[],"tags":["fork"],"reversibility":"two-way"}`)
		preview, err := PreviewForkOverride(ForkOverrideSnapshot{Local: f.local, Collections: map[CollectionID]*Collection{f.local.ID: f.local, f.parent.ID: f.parent}}, proposal)
		if err != nil {
			t.Fatal(err)
		}
		f.preview = preview.Envelope
		assertSinglePublication(t, f)
	})
	t.Run("restore", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-restore-basis", "restorable replacement authority")
		persistSinglePreview(t, f)
		proposal := json.RawMessage(`{"version":1,"operation":"restore","operationId":"single-roundtrip-restore","date":"2026-09-09","decidedBy":"user-approved","confirmation":"restore this exact retained override","replacement":"ledger:` + string(f.local.ID) + `:D-0001"}`)
		preview, err := PreviewForkRestoration(ForkRestorationSnapshot{ConfigBefore: readTransactionBytes(t, f.tx.configPath), Local: f.local, Collections: map[CollectionID]*Collection{f.local.ID: f.local, f.parent.ID: f.parent}}, proposal)
		if err != nil {
			t.Fatal(err)
		}
		f.preview = preview.Envelope
		assertSinglePublication(t, f)
	})
	t.Run("archive participates in allocation and persisted validation", func(t *testing.T) {
		f := newTransactionFixture(t, "single-publication-archive-adoption")
		local, parent := persistAdoptedTransactionFixture(t, f)
		current := readTransactionBytes(t, f.forkPath)
		withArchive := bytes.Replace(current, []byte(`"parentBindingId":`), []byte(`"archives": ["Decisions/fork-archive-*.md"], "parentBindingId":`), 1)
		if bytes.Equal(withArchive, current) {
			t.Fatal("adopted fixture metadata did not expose parentBindingId")
		}
		if err := os.WriteFile(f.forkPath, withArchive, 0o644); err != nil {
			t.Fatal(err)
		}
		archivePath := filepath.Join(f.planning, "Decisions", "fork-archive-2025.md")
		archive := transactionLedger("retained archived local history")
		archive = bytes.Replace(archive, []byte("status: active"), []byte("status: archived"), 1)
		archive = bytes.Replace(archive, []byte("id: D-0001"), []byte("id: D-0099"), 1)
		archive = bytes.Replace(archive, []byte("status: accepted"), []byte("status: rejected"), 1)
		if err := os.WriteFile(archivePath, archive, 0o644); err != nil {
			t.Fatal(err)
		}
		selection, err := ReadSelection(f.repository)
		if err != nil {
			t.Fatal(err)
		}
		local, err = LoadSelectedCollection(selection)
		if err != nil {
			t.Fatalf("load real selected archive collection: %v", err)
		}
		proposal := json.RawMessage(`{"version":1,"operation":"override","operationId":"single-archive-override","date":"2026-09-09","target":"ledger:` + string(parent.ID) + `:D-0001","replacement":"","kind":"decision","decidedBy":"user-approved","statement":"archive-aware replacement","rationale":"approved exact archive-aware publication","scope":["internal/decisionview"],"confirmation":"publish these exact complete ledger bytes","rejected":[],"tags":["fork"],"reversibility":"two-way"}`)
		preview, err := PreviewForkOverride(ForkOverrideSnapshot{Local: local, Collections: map[CollectionID]*Collection{local.ID: local, parent.ID: parent}}, proposal)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Decision["id"] != "D-0100" {
			t.Fatalf("archive ID was reused: allocated %#v, want D-0100", preview.Decision["id"])
		}
		fixture := &singlePublicationFixture{tx: f, local: local, parent: parent, preview: preview.Envelope}
		assertSinglePublication(t, fixture)
		selection, err = ReadSelection(f.repository)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadSelectedCollection(selection)
		if err != nil {
			t.Fatalf("public selected collection rejected archive-aware publication: %v", err)
		}
		if loaded.Entries["D-0099"] == nil || loaded.Entries["D-0100"] == nil || len(loaded.Files) != 2 {
			t.Errorf("post-write collection lost archive history: entries=%v files=%v", loaded.Entries, loaded.Files)
		}
		if got := readTransactionBytes(t, archivePath); !bytes.Equal(got, archive) {
			t.Errorf("single-file publication mutated archive bytes")
		}
	})
}

func TestForkSinglePublicationBarrierNoOpGuard(t *testing.T) {
	t.Run("pending barrier", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-pending-refusal", "barrier-protected authority")
		before := readTransactionBytes(t, f.tx.forkPath)
		store, err := OpenLocalStore(f.tx.repository, f.tx.planning, "single-pending-setup")
		if err != nil {
			t.Fatal(err)
		}
		journal, err := NewForkJournal(journalOwnerB, []CollectionID{f.local.ID}, journalPreview(t, "foreign-single-pending"))
		if err == nil {
			err = store.SaveForkJournal(context.Background(), journal)
		}
		_ = store.Close()
		if err != nil {
			t.Fatal(err)
		}
		result, applyErr := f.publication(t).Apply(context.Background())
		if !errors.Is(applyErr, ErrForkBarrierPending) || result != nil {
			t.Errorf("pending multi-file barrier was not refused: result=%#v err=%v", result, applyErr)
		}
		if got := readTransactionBytes(t, f.tx.forkPath); !bytes.Equal(got, before) {
			t.Errorf("barrier refusal changed local authority")
		}
	})

	t.Run("writer lock is effective", func(t *testing.T) {
		firstFixture := newSinglePublicationFixture(t, "override", "single-lock-first", "first serialized authority")
		secondProposal := bytes.Replace(firstFixture.preview.Request, []byte("single-lock-first"), []byte("single-lock-second"), 1)
		secondProposal = bytes.Replace(secondProposal, []byte("first serialized authority"), []byte("second serialized authority"), 1)
		secondPreview, err := PreviewForkOverride(ForkOverrideSnapshot{Local: firstFixture.local, Collections: map[CollectionID]*Collection{firstFixture.local.ID: firstFixture.local, firstFixture.parent.ID: firstFixture.parent}}, secondProposal)
		if err != nil {
			t.Fatal(err)
		}
		secondFixture := *firstFixture
		secondFixture.preview = secondPreview.Envelope
		first, second := firstFixture.publication(t), secondFixture.publication(t)
		entered, release := make(chan struct{}), make(chan struct{})
		firstDone, secondDone := make(chan error, 1), make(chan error, 1)
		lockAttempt := make(chan error, 1)
		first.afterInitialCheck = func() { close(entered); <-release }
		baseTryLock := second.tryLock
		var once sync.Once
		second.tryLock = func(file *os.File) error {
			err := baseTryLock(file)
			once.Do(func() { lockAttempt <- err })
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go func() { _, err := first.Apply(ctx); firstDone <- err }()
		select {
		case <-entered:
		case err := <-firstDone:
			t.Errorf("first publication never entered its guarded CAS window: %v", err)
			return
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		go func() { _, err := second.Apply(ctx); secondDone <- err }()
		var lockErr error
		select {
		case lockErr = <-lockAttempt:
		case <-ctx.Done():
			close(release)
			t.Fatal(ctx.Err())
		}
		close(release)
		firstErr, secondErr := <-firstDone, <-secondDone
		if lockErr == nil {
			t.Errorf("competing writer entered while UUID collection lock was held; lock is a no-op")
		}
		if firstErr != nil || !errors.Is(secondErr, ErrForkAuthorityConflict) {
			t.Errorf("serialized publication outcomes = %v / %v", firstErr, secondErr)
		}
	})
}

func TestForkSinglePublicationDivergenceAndReplay(t *testing.T) {
	t.Run("post-publication publisher ambiguity", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-publisher-ambiguity", "publisher ambiguity authority")
		publication := f.publication(t)
		basePublish := publication.publish
		publication.publish = func(root *os.Root, path string, content []byte, existing bool) error {
			if err := basePublish(root, path, content, existing); err != nil {
				return err
			}
			return errors.New("injected cleanup failure after replacement")
		}
		result, err := publication.Apply(context.Background())
		if !errors.Is(err, ErrForkAuthorityUnknown) || errors.Is(err, ErrForkAuthorityConflict) {
			t.Errorf("post-replacement publisher error was not explicit outcome-unknown: result=%#v err=%v", result, err)
		}
		if result == nil || result.OperationID != f.preview.OperationID || result.Outcome != ForkAuthorityOutcomeUnknown {
			t.Errorf("ambiguous publisher result omitted operation identity/outcome: %#v", result)
		}
		if got := readTransactionBytes(t, f.tx.forkPath); !bytes.Equal(got, []byte(f.preview.Changes[0].After)) {
			t.Errorf("injected publisher ambiguity did not exercise bytes already landed")
		}
	})

	t.Run("post-publication verification divergence", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-verification-ambiguity", "verification ambiguity authority")
		publication := f.publication(t)
		publication.afterPublish = func() error {
			return os.WriteFile(f.tx.forkPath, append([]byte(f.preview.Changes[0].After), '\n'), 0o644)
		}
		result, err := publication.Apply(context.Background())
		if !errors.Is(err, ErrForkAuthorityUnknown) || errors.Is(err, ErrForkAuthorityConflict) {
			t.Errorf("post-publication divergence was not explicit outcome-unknown: result=%#v err=%v", result, err)
		}
		if result == nil || result.OperationID != f.preview.OperationID || result.Outcome != ForkAuthorityOutcomeUnknown {
			t.Errorf("verification ambiguity omitted operation identity/outcome: %#v", result)
		}
	})

	t.Run("current config owner validation", func(t *testing.T) {
		cases := map[string]func([]byte) []byte{
			"malformed": func([]byte) []byte { return []byte("[]\n") },
			"missing": func(raw []byte) []byte {
				var fields map[string]any
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				delete(fields, "repositoryId")
				out, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				return out
			},
			"foreign": func(raw []byte) []byte {
				var fields map[string]any
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				fields["repositoryId"] = string(journalOwnerB)
				out, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				return out
			},
		}
		for name, mutate := range cases {
			t.Run(name, func(t *testing.T) {
				f := newSinglePublicationFixture(t, "override", "single-config-owner-"+name, "owner-checked authority")
				localBefore := readTransactionBytes(t, f.tx.forkPath)
				config := mutate(readTransactionBytes(t, f.tx.configPath))
				if err := os.WriteFile(f.tx.configPath, config, 0o644); err != nil {
					t.Fatal(err)
				}
				result, err := f.publication(t).Apply(context.Background())
				if !errors.Is(err, ErrForkAuthorityConflict) || result != nil {
					t.Errorf("%s current repository owner was not refused before write: result=%#v err=%v", name, result, err)
				}
				if got := readTransactionBytes(t, f.tx.forkPath); !bytes.Equal(got, localBefore) {
					t.Errorf("%s owner refusal changed local authority", name)
				}
				if got := readTransactionBytes(t, f.tx.configPath); !bytes.Equal(got, config) {
					t.Errorf("%s owner refusal rewrote current config", name)
				}
			})
		}
	})

	t.Run("forged approved envelope", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-forged-envelope", "unforged authority")
		before := readTransactionBytes(t, f.tx.forkPath)
		raw, err := json.Marshal(f.preview)
		if err != nil {
			t.Fatal(err)
		}
		var forged PreviewEnvelope
		if err := json.Unmarshal(raw, &forged); err != nil {
			t.Fatal(err)
		}
		forged.Changes[0].After += "\n"
		forged.Digest, err = previewDigest(&forged)
		if err != nil {
			t.Fatal(err)
		}
		f.preview = &forged
		result, applyErr := f.publication(t).Apply(context.Background())
		if !errors.Is(applyErr, ErrForkAuthorityConflict) || result != nil {
			t.Errorf("digest-valid forged envelope was not semantically refused: result=%#v err=%v", result, applyErr)
		}
		if got := readTransactionBytes(t, f.tx.forkPath); !bytes.Equal(got, before) {
			t.Errorf("forged-envelope refusal changed local authority")
		}
	})

	t.Run("stale selected config", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-stale-config", "config-sensitive authority")
		before := readTransactionBytes(t, f.tx.forkPath)
		config := readTransactionBytes(t, f.tx.configPath)
		stale := bytes.Replace(config, []byte(`"path":"Decisions/fork.md"`), []byte(`"path":"Decisions/other.md"`), 1)
		if bytes.Equal(stale, config) {
			t.Fatal("fixture config did not contain selected canonical path")
		}
		if err := os.WriteFile(f.tx.configPath, stale, 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := f.publication(t).Apply(context.Background())
		if !errors.Is(err, ErrForkAuthorityConflict) || result != nil {
			t.Errorf("stale selected config was not refused: result=%#v err=%v", result, err)
		}
		if got := readTransactionBytes(t, f.tx.forkPath); !bytes.Equal(got, before) {
			t.Errorf("stale-config refusal changed local authority")
		}
		if got := readTransactionBytes(t, f.tx.configPath); !bytes.Equal(got, stale) {
			t.Errorf("stale-config refusal rewrote configuration")
		}
	})

	t.Run("reserved support destination", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-reserved-path", "reserved-path authority")
		raw, err := json.Marshal(f.preview)
		if err != nil {
			t.Fatal(err)
		}
		var forged PreviewEnvelope
		if err := json.Unmarshal(raw, &forged); err != nil {
			t.Fatal(err)
		}
		forged.Changes[0].Path = "Decisions/.fork-state/authority.md"
		forged.Digest, err = previewDigest(&forged)
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewForkAuthorityPublication(ForkAuthorityPublicationRequest{Repository: f.tx.repository, Planning: f.tx.planning, OwnerID: journalOwnerA, Collection: f.local.ID, Preview: &forged, ApprovalDigest: forged.Digest})
		if err == nil {
			t.Errorf("reserved fork-state destination was accepted")
		}
	})

	t.Run("exact-byte divergence", func(t *testing.T) {
		f := newSinglePublicationFixture(t, "override", "single-divergence", "divergence-sensitive authority")
		diverged := append(readTransactionBytes(t, f.tx.forkPath), '\n')
		if err := os.WriteFile(f.tx.forkPath, diverged, 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := f.publication(t).Apply(context.Background())
		if !errors.Is(err, ErrForkAuthorityConflict) || result != nil {
			t.Errorf("exact-byte CAS did not refuse divergent authority: result=%#v err=%v", result, err)
		}
		if got := readTransactionBytes(t, f.tx.forkPath); !bytes.Equal(got, diverged) {
			t.Errorf("divergence refusal overwrote unrelated current bytes")
		}
	})

	t.Run("recorded operation superseded by later publication", func(t *testing.T) {
		first := newSinglePublicationFixture(t, "override", "single-recorded-first", "first recorded authority")
		result, err := first.publication(t).Apply(context.Background())
		if err != nil || result == nil {
			t.Fatalf("publish first operation: result=%#v err=%v", result, err)
		}
		local, err := LoadCollection(Roots{Repository: first.tx.repository, Planning: first.tx.planning}, first.local.ID, first.local.Locator)
		if err != nil {
			t.Fatal(err)
		}
		proposal := json.RawMessage(`{"version":1,"operation":"reconcile","operationId":"single-recorded-later","date":"2026-09-09","target":"ledger:` + string(first.parent.ID) + `:D-0001","replacement":"ledger:` + string(first.local.ID) + `:D-0001","kind":"decision","decidedBy":"user-approved","statement":"later approved authority","rationale":"approved later mutation","scope":["internal/decisionview"],"confirmation":"publish these exact complete ledger bytes","rejected":[],"tags":["fork"],"reversibility":"two-way"}`)
		laterPreview, err := PreviewForkOverride(ForkOverrideSnapshot{Local: local, Collections: map[CollectionID]*Collection{local.ID: local, first.parent.ID: first.parent}}, proposal)
		if err != nil {
			t.Fatal(err)
		}
		later := *first
		later.local, later.preview = local, laterPreview.Envelope
		result, err = later.publication(t).Apply(context.Background())
		if err != nil || result == nil {
			t.Fatalf("publish later operation: result=%#v err=%v", result, err)
		}
		result, err = first.publication(t).Apply(context.Background())
		if !errors.Is(err, ErrForkAuthorityReplay) || result != nil {
			t.Errorf("non-current retained operation was not refused: result=%#v err=%v", result, err)
		}
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "reus") {
			t.Errorf("honest historical replay was diagnosed as identity reuse: %v", err)
		}
	})

	t.Run("lost reply and operation collision", func(t *testing.T) {
		first := newSinglePublicationFixture(t, "override", "single-replay-operation", "first replay identity meaning")
		secondProposal := bytes.Replace(first.preview.Request, []byte("first replay identity meaning"), []byte("different replay identity meaning"), 1)
		secondPreview, err := PreviewForkOverride(ForkOverrideSnapshot{Local: first.local, Collections: map[CollectionID]*Collection{first.local.ID: first.local, first.parent.ID: first.parent}}, secondProposal)
		if err != nil {
			t.Fatal(err)
		}
		second := *first
		second.preview = secondPreview.Envelope
		initial, err := first.publication(t).Apply(context.Background())
		if err != nil || initial == nil || initial.Replayed {
			t.Errorf("initial operation publication = %#v, %v", initial, err)
			return
		}
		published := readTransactionBytes(t, first.tx.forkPath)
		if err := os.Remove(first.tx.sourcePath); err != nil {
			t.Fatal(err)
		}
		replay, replayErr := first.publication(t).Apply(context.Background())
		if replayErr != nil || replay == nil || !replay.Replayed || replay.OperationID != first.preview.OperationID {
			t.Errorf("identical lost-reply replay was not disambiguated: result=%#v err=%v", replay, replayErr)
		}
		collision, collisionErr := second.publication(t).Apply(context.Background())
		if !errors.Is(collisionErr, ErrForkAuthorityReplay) || collision != nil {
			t.Errorf("same operation ID with different exact envelope was not refused: result=%#v err=%v", collision, collisionErr)
		}
		if got := readTransactionBytes(t, first.tx.forkPath); !bytes.Equal(got, published) {
			t.Errorf("replay handling changed already committed authority")
		}
	})
}
