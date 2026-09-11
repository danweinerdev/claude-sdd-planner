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

func legacyAdoptionRecoveryFixture(t *testing.T, operation string) *transactionFixture {
	t.Helper()
	f := newTransactionFixture(t, operation)
	f.oldConfig = []byte("{\n  \"planningRoot\": " + mustJSONRecoveryTest(t, f.planning) + ",\n  \"unrelated\": {\"keep\": true}\n}\n")
	if err := os.WriteFile(f.configPath, f.oldConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionSourceCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := json.Marshal(forkAdoptionRequest{
		Version: Version1, Operation: "adopt", OperationID: operation, Date: "2026-09-09",
		RepositoryID: journalOwnerA, LedgerID: transactionCollection, Path: "Decisions/fork.md",
		BindingID: "binding-initial", SourceOwnerID: journalOwnerB,
		Source: SourceLocator{Root: SourceRootPlanning, Path: "Decisions/parent.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewForkAdoption(ForkAdoptionSnapshot{ConfigBefore: f.oldConfig, Source: source}, proposal)
	if err != nil {
		t.Fatal(err)
	}
	f.newConfig, f.newFork = nil, nil
	for _, change := range preview.Envelope.Changes {
		if change.Root == SourceRootRepository {
			f.newConfig = []byte(change.After)
		} else {
			f.newFork = []byte(change.After)
		}
	}
	f.request = ForkTransactionRequest{
		Repository: f.repository, Planning: f.planning, OwnerID: journalOwnerA,
		Collections: []CollectionID{transactionCollection}, Preview: preview.Envelope,
		ApprovalDigest: preview.Envelope.Digest,
	}
	return f
}

func mustJSONRecoveryTest(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestForkLegacyAdoptionRecovery(t *testing.T) {
	for _, test := range []struct {
		name    string
		action  ForkRecoveryAction
		outcome ForkTransactionOutcome
		wantNew bool
	}{
		{name: "finish", action: ForkRecoveryFinish, outcome: ForkTransactionCommitted, wantNew: true},
		{name: "rollback", action: ForkRecoveryRollback, outcome: ForkTransactionRolledBack},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := legacyAdoptionRecoveryFixture(t, "legacy-adoption-recovery-"+test.name)
			runRecoveryCrash(t, f, ForkTransactionAfterIntermediateConfig)

			store, err := OpenLocalStore(f.repository, f.planning, "legacy-recovery-test")
			if err != nil {
				t.Fatal(err)
			}
			capture, err := store.LoadForkSelectorCapture(f.request.Preview.OperationID)
			if closeErr := store.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(readTransactionBytes(t, f.configPath), []byte(capture.Intermediate)) {
				t.Fatal("interruption did not leave the exact intermediate selector bytes")
			}

			preview := previewRecoveryUnchanged(t, f, test.action)
			result, err := RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: preview, ApprovalDigest: preview.Digest})
			if err != nil {
				t.Fatalf("explicit %s recovery of legacy adoption: %v", test.action, err)
			}
			if result == nil || result.Outcome != test.outcome {
				t.Fatalf("legacy adoption recovery outcome = %#v, want %s", result, test.outcome)
			}
			assertRecoveryBytes(t, f, test.wantNew)
		})
	}

	t.Run("discard refuses activated intermediate selector", func(t *testing.T) {
		f := legacyAdoptionRecoveryFixture(t, "legacy-adoption-recovery-discard")
		runRecoveryCrash(t, f, ForkTransactionAfterIntermediateConfig)
		before := recoveryTree(t, f.repository, f.planning)
		preview, err := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryDiscardStaging)
		if err == nil || preview != nil {
			t.Fatalf("discard accepted an activated intermediate selector: preview=%#v err=%v", preview, err)
		}
		if after := recoveryTree(t, f.repository, f.planning); !recoveryTreesEqual(after, before) {
			t.Fatal("refused discard changed recovery or authority bytes")
		}
	})
}

func recoveryTreesEqual(left, right map[string]recoveryTreeEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for name, want := range right {
		got, ok := left[name]
		if !ok || got.Mode != want.Mode || !bytes.Equal(got.Data, want.Data) {
			return false
		}
	}
	return true
}

func TestForkRecoveryOriginalOwnerGuard(t *testing.T) {
	owned := `{"planningRoot":".plans","repositoryId":"10000000-0000-4000-8000-000000000001"}`
	selectedWithoutOwner := `{"planningRoot":".plans","decisionLog":{"version":1,"mode":"fork","path":"Decisions/fork.md","ledgerId":"40000000-0000-4000-8000-000000000004"}}`
	empty := `{"planningRoot":".plans","repositoryId":""}`
	foreign := `{"planningRoot":".plans","repositoryId":"20000000-0000-4000-8000-000000000002"}`
	for _, test := range []struct {
		name    string
		capture *ForkSelectorCapture
	}{
		{name: "malformed original", capture: &ForkSelectorCapture{Original: `[]`, Intermediate: owned, Final: owned}},
		{name: "empty original owner", capture: &ForkSelectorCapture{Original: empty, Intermediate: owned, Final: owned}},
		{name: "mismatched original", capture: &ForkSelectorCapture{Original: foreign, Intermediate: owned, Final: owned}},
		{name: "selected authority cannot omit owner", capture: &ForkSelectorCapture{Original: selectedWithoutOwner, Intermediate: owned, Final: owned}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateRecoveryOwner(journalOwnerA, test.capture); err == nil {
				t.Fatal("recovery accepted malformed, foreign, or absent ownership for existing selected authority")
			}
		})
	}

	t.Run("selector digest remains bound", func(t *testing.T) {
		f := legacyAdoptionRecoveryFixture(t, "legacy-adoption-recovery-digest")
		runRecoveryCrash(t, f, ForkTransactionAfterIntermediateConfig)
		journalPath := filepath.Join(f.planning, filepath.FromSlash(forkJournalPath(transactionCollection, f.request.Preview.OperationID)))
		raw, err := os.ReadFile(journalPath)
		if err != nil {
			t.Fatal(err)
		}
		var journal ForkJournal
		if err := json.Unmarshal(raw, &journal); err != nil {
			t.Fatal(err)
		}
		journal.SelectorCapture.SourceDigest = collectionDigest([]byte("different original bytes"))
		raw, err = json.Marshal(&journal)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journalPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		preview, err := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryFinish)
		if err == nil || preview != nil || !errors.Is(err, ErrForkSelectorCaptureUnavailable) {
			t.Fatalf("recovery accepted an unbound selector digest: preview=%#v err=%v", preview, err)
		}
	})
}
