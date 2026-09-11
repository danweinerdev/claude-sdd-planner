package decisionview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const storageReviewAncestorCollection = CollectionID("70000000-0000-4000-8000-000000000007")

func TestForkPublicationCanonicalRootsAndSourceBarriers(t *testing.T) {
	t.Run("filesystem ancestor symlink is canonicalized", func(t *testing.T) {
		f := newTransactionFixture(t, "storage-review-canonical-root")
		parent := filepath.Dir(f.repository)
		if filepath.Dir(f.planning) != parent {
			t.Skip("fixture roots do not share a filesystem parent")
		}
		mount := filepath.Join(t.TempDir(), "mount")
		if err := os.Symlink(parent, mount); err != nil {
			t.Skipf("directory symlinks unavailable: %v", err)
		}
		f.request.Repository = filepath.Join(mount, filepath.Base(f.repository))
		f.request.Planning = filepath.Join(mount, filepath.Base(f.planning))

		result, err := f.transaction(t).Apply(context.Background())
		if err != nil || result == nil || result.Outcome != ForkTransactionCommitted {
			t.Fatalf("publication through a canonicalizable filesystem ancestor: result=%#v err=%v", result, err)
		}
		if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, f.newConfig) {
			t.Fatal("canonical-root publication did not reach the represented config")
		}
		if _, err := canonicalTransactionRoot(filepath.Join(mount, "..", "escape")); err == nil {
			t.Fatal("a path escaping the supplied filesystem alias was accepted as that alias's root")
		}
		if err := validateRelativeLocator("../escape.md"); err == nil {
			t.Fatal("a locator escaping its canonical root was accepted")
		}
	})

	t.Run("no source barrier control", func(t *testing.T) {
		f, _, _ := storageReviewAdoptionChain(t, "storage-review-source-control")
		result, err := f.transaction(t).Apply(context.Background())
		if err != nil || result == nil || result.Outcome != ForkTransactionCommitted {
			t.Fatalf("unblocked adoption control did not commit: result=%#v err=%v", result, err)
		}
	})

	for _, test := range []struct {
		name      string
		operation string
		id        CollectionID
	}{
		{name: "direct source barrier", operation: "storage-review-direct-source", id: transactionSourceCollection},
		{name: "source ancestor barrier", operation: "storage-review-source-ancestor", id: storageReviewAncestorCollection},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _, _ := storageReviewAdoptionChain(t, test.operation)
			store, err := OpenLocalStore(f.repository, f.planning, "storage-review-source-barrier")
			if err != nil {
				t.Fatal(err)
			}
			journal, err := NewForkJournal(journalOwnerB, []CollectionID{test.id}, journalPreview(t, "pending-"+string(test.id)))
			if err != nil {
				store.Close()
				t.Fatal(err)
			}
			if err := store.SaveForkJournal(context.Background(), journal); err != nil {
				store.Close()
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			result, applyErr := f.transaction(t).Apply(context.Background())
			if !errors.Is(applyErr, ErrForkBarrierPending) || result != nil {
				t.Fatalf("adoption crossed %s: result=%#v err=%v", test.name, result, applyErr)
			}
			assertTransactionAuthorityUnchanged(t, f)
		})
	}
}

func storageReviewAdoptionChain(t *testing.T, operation string) (*transactionFixture, *Collection, *Collection) {
	t.Helper()
	f := newTransactionFixture(t, operation)
	ancestorPath := filepath.Join(f.planning, "Decisions", "ancestor.md")
	if err := os.WriteFile(ancestorPath, transactionLedger("ancestor authority"), 0o644); err != nil {
		t.Fatal(err)
	}
	ancestor, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, storageReviewAncestorCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/ancestor.md"})
	if err != nil {
		t.Fatal(err)
	}
	parentConfig, err := json.Marshal(map[string]any{"planningRoot": f.planning, "repositoryId": journalOwnerB})
	if err != nil {
		t.Fatal(err)
	}
	parentProposal, err := json.Marshal(forkAdoptionRequest{
		Version: Version1, Operation: "adopt", OperationID: operation + "-parent", Date: "2026-09-10",
		RepositoryID: journalOwnerB, LedgerID: transactionSourceCollection, Path: "Decisions/parent.md",
		BindingID: "binding-ancestor", SourceOwnerID: journalOwnerA,
		Source: SourceLocator{Root: SourceRootPlanning, Path: "Decisions/ancestor.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	parentPreview, err := PreviewForkAdoption(ForkAdoptionSnapshot{ConfigBefore: parentConfig, Source: ancestor}, parentProposal)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range parentPreview.Envelope.Changes {
		if change.Root == SourceRootPlanning {
			if err := os.WriteFile(f.sourcePath, []byte(change.After), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	parent, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionSourceCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewForkAdoption(ForkAdoptionSnapshot{ConfigBefore: f.oldConfig, Source: parent, Collections: map[CollectionID]*Collection{ancestor.ID: ancestor}}, f.request.Preview.Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range preview.Envelope.Changes {
		if change.Root == SourceRootRepository {
			f.newConfig = []byte(change.After)
		} else {
			f.newFork = []byte(change.After)
		}
	}
	f.request.Preview = preview.Envelope
	f.request.ApprovalDigest = preview.Envelope.Digest
	return f, parent, ancestor
}

func TestForkJournalReadableBoundsAndCommittedCleanup(t *testing.T) {
	t.Run("journal write respects reader bound", func(t *testing.T) {
		f := newTransactionFixture(t, "storage-review-journal-bound")
		store, err := OpenLocalStore(f.repository, f.planning, "storage-review-journal-bound")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		relative := forkJournalPath(transactionCollection, "oversize")
		if err := store.writePrivateJournalFile(relative, make([]byte, maxCollectionFileBytes+1), false); err == nil {
			t.Fatal("journal writer persisted bytes that its corresponding reader must refuse")
		}
		if _, err := store.root.Stat(relative); !os.IsNotExist(err) {
			t.Fatalf("oversize journal refusal left a published target: %v", err)
		}
	})

	t.Run("committed history releases barriers after source advances", func(t *testing.T) {
		f := newTransactionFixture(t, "storage-review-committed-cleanup")
		tx := f.transaction(t)
		tx.failpoint = func(point ForkTransactionPoint) error {
			if point == ForkTransactionAfterCommitRecord {
				return errors.New("lost reply before committed barrier release")
			}
			return nil
		}
		result, err := tx.Apply(context.Background())
		if err == nil || result == nil || result.Outcome != ForkTransactionRecoveryRequired {
			t.Fatalf("fixture did not persist committed history with pending barriers: result=%#v err=%v", result, err)
		}
		assertRecoveryBytes(t, f, true)
		if err := os.WriteFile(f.sourcePath, transactionLedger("compatible later source authority"), 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(f.sourcePath, 0o644) })

		if rollback, rollbackErr := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryRollback); rollbackErr == nil || rollback != nil {
			t.Fatalf("committed history offered rollback after its source advanced: preview=%#v err=%v", rollback, rollbackErr)
		}
		preview, previewErr := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryFinish)
		if previewErr != nil || preview == nil {
			t.Fatalf("exact committed targets could not preview barrier cleanup after source advance: preview=%#v err=%v", preview, previewErr)
		}
		result, err = RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: preview, ApprovalDigest: preview.Digest})
		if err != nil || result == nil || result.Outcome != ForkTransactionCommitted {
			t.Fatalf("committed barrier cleanup failed: result=%#v err=%v", result, err)
		}
		assertRecoveryBytes(t, f, true)
		store, openErr := OpenLocalStore(f.repository, f.planning, "storage-review-committed-inspection")
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer store.Close()
		barrier, inspectErr := store.InspectForkBarrier(transactionCollection)
		if inspectErr != nil || barrier == nil || barrier.Status != "committed" {
			t.Fatalf("committed barrier was not released: barrier=%#v err=%v", barrier, inspectErr)
		}
	})
}
