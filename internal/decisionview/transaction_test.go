package decisionview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const transactionCollection = CollectionID("40000000-0000-4000-8000-000000000004")
const transactionSourceCollection = CollectionID("50000000-0000-4000-8000-000000000005")
const transactionReplacementCollection = CollectionID("60000000-0000-4000-8000-000000000006")

type transactionFixture struct {
	repository string
	planning   string
	configPath string
	forkPath   string
	sourcePath string
	oldConfig  []byte
	newConfig  []byte
	oldFork    []byte
	newFork    []byte
	forkExists bool
	request    ForkTransactionRequest
}

func transactionLedger(statement string) []byte {
	return []byte(fmt.Sprintf("---\ntitle: Parent decisions\ntype: decision-log\nstatus: active\ncreated: 2026-09-01\nupdated: 2026-09-01\ntags: []\nrelated: []\ndecisions:\n  - id: D-0001\n    kind: decision\n    status: accepted\n    date: 2026-09-01\n    decided_by: user\n    statement: %s\n    rationale: approved parent requirement\n    scope: []\n    tags: []\n    rejected: []\n---\n\n# Parent decisions\n", statement))
}

func newTransactionFixture(t *testing.T, operation string) *transactionFixture {
	t.Helper()
	f := &transactionFixture{repository: t.TempDir(), planning: t.TempDir()}
	config, err := json.Marshal(map[string]any{"planningRoot": f.planning, "repositoryId": journalOwnerA, "unrelated": map[string]any{"keep": true}})
	if err != nil {
		t.Fatal(err)
	}
	f.oldConfig = append(config, '\n')
	f.configPath = filepath.Join(f.repository, "planning-config.json")
	f.forkPath = filepath.Join(f.planning, "Decisions", "fork.md")
	f.sourcePath = filepath.Join(f.planning, "Decisions", "parent.md")
	if err := os.MkdirAll(filepath.Dir(f.forkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{f.configPath: f.oldConfig, f.sourcePath: transactionLedger("inherited authority")} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionSourceCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := json.Marshal(forkAdoptionRequest{Version: Version1, Operation: "adopt", OperationID: operation, Date: "2026-09-09", RepositoryID: journalOwnerA, LedgerID: transactionCollection, Path: "Decisions/fork.md", BindingID: "binding-initial", SourceOwnerID: journalOwnerB, Source: SourceLocator{Root: SourceRootPlanning, Path: "Decisions/parent.md"}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewForkAdoption(ForkAdoptionSnapshot{ConfigBefore: f.oldConfig, Source: source}, proposal)
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
	f.request = ForkTransactionRequest{Repository: f.repository, Planning: f.planning, OwnerID: journalOwnerA, Collections: []CollectionID{transactionCollection}, Preview: preview.Envelope, ApprovalDigest: preview.Envelope.Digest}
	return f
}

func (f *transactionFixture) transaction(t *testing.T) *ForkTransaction {
	t.Helper()
	tx, err := NewForkTransaction(f.request)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func readTransactionBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestForkTransactionPublicRoundTrip(t *testing.T) {
	f := newTransactionFixture(t, "transaction-round-trip")
	result, err := f.transaction(t).Apply(context.Background())
	if err != nil {
		t.Fatalf("apply exact approved cross-store transaction through public API: %v", err)
	}
	if result == nil || result.Outcome != ForkTransactionCommitted || result.OperationID != f.request.Preview.OperationID {
		t.Fatalf("public apply did not report the durable commit: %#v", result)
	}
	inspection, err := InspectForkTransaction(f.repository, f.planning, result.OperationID, transactionCollection)
	if err != nil {
		t.Fatalf("inspect transaction through a fresh public read boundary: %v", err)
	}
	if inspection == nil || inspection.Outcome != ForkTransactionCommitted || inspection.Authority != ResolutionComplete {
		t.Fatalf("committed transaction did not round trip as complete authority: %#v", inspection)
	}
	if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, f.newConfig) {
		t.Fatalf("repository config did not cross the public write/read boundary: %q", got)
	}
	if got := readTransactionBytes(t, f.forkPath); !bytes.Equal(got, f.newFork) {
		t.Fatalf("planning ledger did not cross the public write/read boundary: %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.repository, "Decisions", ".fork-state")); !os.IsNotExist(err) {
		t.Fatalf("transaction support escaped the distinct planning root: %v", err)
	}
	selection, err := ReadSelection(f.repository)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := LoadSelectedCollection(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := selection.ValidateOwner(selected.Metadata); err != nil {
		t.Fatalf("committed public selection/ledger ownership disagrees: %v", err)
	}
}

func transactionSourceRaceScenario(t *testing.T, noOpRecheck bool) (bool, error) {
	t.Helper()
	f := newTransactionFixture(t, fmt.Sprintf("transaction-source-race-%t", noOpRecheck))
	tx := f.transaction(t)
	reachedPublication := false
	tx.failpoint = func(point ForkTransactionPoint) error {
		if point == ForkTransactionBeforePlanningPublish {
			reachedPublication = true
		}
		return nil
	}
	checked, release := make(chan struct{}), make(chan struct{})
	tx.afterInitialCheck = func() { close(checked); <-release }
	if noOpRecheck {
		tx.recheckSources = func() error { return nil }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct {
		result *ForkTransactionResult
		err    error
	}, 1)
	go func() {
		result, err := tx.Apply(ctx)
		done <- struct {
			result *ForkTransactionResult
			err    error
		}{result, err}
	}()
	select {
	case <-checked:
	case got := <-done:
		return reachedPublication, fmt.Errorf("transaction never exposed its guarded source-check window: %v / %#v", got.err, got.result)
	case <-ctx.Done():
		return reachedPublication, ctx.Err()
	}
	if err := os.WriteFile(f.sourcePath, []byte("external source divergence\n"), 0o644); err != nil {
		close(release)
		return reachedPublication, err
	}
	close(release)
	got := <-done
	if got.err == nil && got.result != nil && got.result.Outcome == ForkTransactionCommitted {
		return reachedPublication, errors.New("transaction committed after its approved source basis changed")
	}
	if bytes.Equal(readTransactionBytes(t, f.configPath), f.newConfig) || transactionFileEquals(f.forkPath, f.newFork) {
		return reachedPublication, errors.New("source race exposed new authority instead of refusal or recovery-required")
	}
	return reachedPublication, nil
}

func TestForkTransactionSourceRaceNoOpGuard(t *testing.T) {
	reached, err := transactionSourceRaceScenario(t, false)
	if err != nil {
		t.Fatal(err)
	}
	if reached {
		t.Fatal("real source guard reached publication after early source divergence")
	}
	reached, err = transactionSourceRaceScenario(t, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reached {
		t.Fatal("independent no-op guard control did not reach the protected publication boundary")
	}
}

func assertTransactionFailureState(t *testing.T, f *transactionFixture, result *ForkTransactionResult, applyErr error) {
	t.Helper()
	if applyErr == nil {
		t.Fatal("injected publication failure was reported as an ordinary success")
	}
	inspection, err := InspectForkTransaction(f.repository, f.planning, f.request.Preview.OperationID, transactionCollection)
	if err != nil {
		t.Fatalf("failure state was not readable through the public inspection boundary: %v", err)
	}
	config := readTransactionBytes(t, f.configPath)
	fork, forkExists, forkErr := readOptionalTransactionBytes(f.forkPath)
	if forkErr != nil {
		t.Fatal(forkErr)
	}
	oldComplete := bytes.Equal(config, f.oldConfig) && forkExists == f.forkExists && (!forkExists || bytes.Equal(fork, f.oldFork))
	newComplete := bytes.Equal(config, f.newConfig) && forkExists && bytes.Equal(fork, f.newFork)
	if oldComplete {
		if result == nil || result.Outcome != ForkTransactionRolledBack || inspection == nil || inspection.Outcome != ForkTransactionRolledBack || inspection.Authority != ResolutionComplete {
			t.Fatalf("old bytes were not reported as a verified clean rollback: result=%#v inspection=%#v", result, inspection)
		}
		return
	}
	if newComplete {
		committed := result != nil && result.Outcome == ForkTransactionCommitted && inspection != nil && inspection.Outcome == ForkTransactionCommitted && inspection.Authority == ResolutionComplete
		blocked := result != nil && result.Outcome == ForkTransactionRecoveryRequired && inspection != nil && inspection.Outcome == ForkTransactionRecoveryRequired && inspection.Authority == ResolutionRecoveryNeeded
		if !committed && !blocked {
			t.Fatalf("new bytes were neither a committed lost reply nor blocked by an incomplete barrier release: result=%#v inspection=%#v", result, inspection)
		}
		return
	}
	if result == nil || result.Outcome != ForkTransactionRecoveryRequired || inspection == nil || inspection.Outcome != ForkTransactionRecoveryRequired || inspection.Authority != ResolutionRecoveryNeeded {
		t.Fatalf("partial/divergent authority was mislabeled clean instead of recovery-required: config=%q fork=%q result=%#v inspection=%#v", config, fork, result, inspection)
	}
}

func TestForkTransactionCrossStoreFailpoints(t *testing.T) {
	points := []ForkTransactionPoint{ForkTransactionAfterJournal, ForkTransactionAfterBarriers, ForkTransactionAfterIntermediateConfig, ForkTransactionAfterPlanningPublish, ForkTransactionAfterFinalConfig, ForkTransactionAfterCommitRecord, ForkTransactionAfterBarrierRelease}
	for _, point := range points {
		t.Run(string(point), func(t *testing.T) {
			f := newTransactionFixture(t, "transaction-failpoint-"+string(point))
			tx := f.transaction(t)
			tx.failpoint = func(got ForkTransactionPoint) error {
				if got == point {
					return fmt.Errorf("injected failure at %s", point)
				}
				return nil
			}
			result, err := tx.Apply(context.Background())
			assertTransactionFailureState(t, f, result, err)
		})
	}
	t.Run("external-divergence", func(t *testing.T) {
		f := newTransactionFixture(t, "transaction-external-divergence")
		tx := f.transaction(t)
		external := []byte("external unapproved fork divergence\n")
		reached := false
		tx.failpoint = func(point ForkTransactionPoint) error {
			if point == ForkTransactionBeforePlanningPublish {
				reached = true
				return os.WriteFile(f.forkPath, external, 0o644)
			}
			return nil
		}
		result, err := tx.Apply(context.Background())
		if !reached {
			t.Fatalf("transaction never reached the external-divergence publication boundary: %v", err)
		}
		if err == nil {
			t.Fatal("transaction ignored external divergence at a publication boundary")
		}
		if got := readTransactionBytes(t, f.forkPath); !bytes.Equal(got, external) {
			t.Fatalf("transaction overwrote external divergence: %q", got)
		}
		inspection, inspectErr := InspectForkTransaction(f.repository, f.planning, f.request.Preview.OperationID, transactionCollection)
		if inspectErr != nil || result == nil || result.Outcome != ForkTransactionRecoveryRequired || inspection == nil || inspection.Outcome != ForkTransactionRecoveryRequired || inspection.Authority != ResolutionRecoveryNeeded {
			t.Fatalf("external divergence was not retained as recovery-required: result=%#v inspection=%#v inspectErr=%v", result, inspection, inspectErr)
		}
	})
}

func selectionPublicationFixtures() map[string]func(*testing.T) *transactionFixture {
	return map[string]func(*testing.T) *transactionFixture{
		"adopt": func(t *testing.T) *transactionFixture {
			return newTransactionFixture(t, "transaction-operation-adopt")
		},
		"rebind": func(t *testing.T) *transactionFixture {
			f, _, _ := rebindTransactionFixture(t)
			return f
		},
		"detach": func(t *testing.T) *transactionFixture {
			f, _, _ := detachTransactionFixture(t)
			return f
		},
	}
}

func TestForkTransactionSelectionOperationRollbackAndDivergence(t *testing.T) {
	for operation, fixture := range selectionPublicationFixtures() {
		t.Run(operation+"-handled-rollback", func(t *testing.T) {
			f := fixture(t)
			tx := f.transaction(t)
			tx.failpoint = func(point ForkTransactionPoint) error {
				if point == ForkTransactionAfterPlanningPublish {
					return errors.New("injected handled publication failure")
				}
				return nil
			}
			result, err := tx.Apply(context.Background())
			assertTransactionFailureState(t, f, result, err)
		})

		t.Run(operation+"-external-divergence", func(t *testing.T) {
			f := fixture(t)
			tx := f.transaction(t)
			external := []byte("external unapproved selection divergence\n")
			reached := false
			tx.failpoint = func(point ForkTransactionPoint) error {
				if point == ForkTransactionBeforePlanningPublish {
					reached = true
					return os.WriteFile(f.forkPath, external, 0o644)
				}
				return nil
			}
			result, err := tx.Apply(context.Background())
			if !reached || err == nil {
				t.Fatalf("%s did not refuse divergence at publication: reached=%t result=%#v err=%v", operation, reached, result, err)
			}
			if got := readTransactionBytes(t, f.forkPath); !bytes.Equal(got, external) {
				t.Fatalf("%s overwrote external divergence: %q", operation, got)
			}
			if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, f.oldConfig) {
				t.Fatalf("%s divergence did not restore operation-owned config: %q", operation, got)
			}
			inspection, inspectErr := InspectForkTransaction(f.repository, f.planning, f.request.Preview.OperationID, transactionCollection)
			if inspectErr != nil || result == nil || result.Outcome != ForkTransactionRecoveryRequired || inspection == nil || inspection.Authority != ResolutionRecoveryNeeded {
				t.Fatalf("%s divergence was not recovery-required: result=%#v inspection=%#v err=%v", operation, result, inspection, inspectErr)
			}
		})
	}
}

func readOptionalTransactionBytes(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return b, err == nil, err
}

func transactionFileEquals(path string, want []byte) bool {
	got, exists, err := readOptionalTransactionBytes(path)
	return err == nil && exists && bytes.Equal(got, want)
}

func cloneTransactionPreview(t *testing.T, preview *PreviewEnvelope) *PreviewEnvelope {
	t.Helper()
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := DecodePreviewEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	return clone
}

func resignTransactionPreview(t *testing.T, preview *PreviewEnvelope) {
	t.Helper()
	digest, err := previewDigest(preview)
	if err != nil {
		t.Fatal(err)
	}
	preview.Digest = digest
}

func TestForkTransactionRejectsSemanticallyUnauthorizedEnvelope(t *testing.T) {
	tests := map[string]func(*transactionFixture){
		"owner mismatch": func(f *transactionFixture) { f.request.OwnerID = journalOwnerB },
		"collection mismatch": func(f *transactionFixture) { f.request.Collections = []CollectionID{transactionReplacementCollection} },
		"arbitrary destination": func(f *transactionFixture) {
			f.request.Preview = cloneTransactionPreview(t, f.request.Preview)
			f.request.Preview.Changes[0].Path = "unrelated.json"
			resignTransactionPreview(t, f.request.Preview)
			f.request.ApprovalDigest = f.request.Preview.Digest
		},
		"tampered authority delta": func(f *transactionFixture) {
			f.request.Preview = cloneTransactionPreview(t, f.request.Preview)
			f.request.Preview.Delta.Before = append([]QualifiedID(nil), f.request.Preview.Delta.After...)
			f.request.Preview.Delta.Removed = nil
			resignTransactionPreview(t, f.request.Preview)
			f.request.ApprovalDigest = f.request.Preview.Digest
		},
		"unsupported operation request": func(f *transactionFixture) {
			f.request.Preview = cloneTransactionPreview(t, f.request.Preview)
			f.request.Preview.Operation = "archive"
			resignTransactionPreview(t, f.request.Preview)
			f.request.ApprovalDigest = f.request.Preview.Digest
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := newTransactionFixture(t, "transaction-reject-"+name)
			mutate(f)
			tx, err := NewForkTransaction(f.request)
			if err == nil {
				_, err = tx.Apply(context.Background())
			}
			if err == nil {
				t.Fatal("semantically unauthorized envelope was accepted")
			}
			if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, f.oldConfig) {
				t.Fatalf("rejected envelope changed config: %q", got)
			}
			if _, exists, readErr := readOptionalTransactionBytes(f.forkPath); readErr != nil || exists {
				t.Fatalf("rejected envelope created local authority: exists=%v err=%v", exists, readErr)
			}
		})
	}
}

func TestForkTransactionRejectsReservedLedgerDestinationBeforeSupportMutation(t *testing.T) {
	f := newTransactionFixture(t, "transaction-reserved-destination")
	f.request.Preview = cloneTransactionPreview(t, f.request.Preview)
	for i := range f.request.Preview.Changes {
		if f.request.Preview.Changes[i].Root == SourceRootPlanning {
			f.request.Preview.Changes[i].Path = "dEcIsIoNs/.FoRk-StAtE/canonical.md"
		}
	}
	resignTransactionPreview(t, f.request.Preview)
	f.request.ApprovalDigest = f.request.Preview.Digest
	if _, err := NewForkTransaction(f.request); err == nil {
		t.Fatal("reserved support namespace was accepted as a canonical ledger destination")
	}
	if _, err := os.Stat(filepath.Join(f.planning, "Decisions", ".fork-state")); !os.IsNotExist(err) {
		t.Fatalf("reserved-destination refusal mutated support storage: %v", err)
	}
}

func persistAdoptedTransactionFixture(t *testing.T, f *transactionFixture) (*Collection, *Collection) {
	t.Helper()
	if err := os.WriteFile(f.configPath, f.newConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.forkPath, f.newFork, 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/fork.md"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionSourceCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	return local, parent
}

func rebindTransactionFixture(t *testing.T) (*transactionFixture, []byte, []byte) {
	t.Helper()
	f := newTransactionFixture(t, "selection-initial-adopt-rebind")
	local, parent := persistAdoptedTransactionFixture(t, f)
	replacementPath := filepath.Join(f.planning, "Decisions", "replacement.md")
	if err := os.WriteFile(replacementPath, transactionLedger("replacement inherited authority"), 0o644); err != nil {
		t.Fatal(err)
	}
	replacement, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionReplacementCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/replacement.md"})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := json.Marshal(forkAdoptionRequest{Version: Version1, Operation: "rebind", OperationID: "selection-rebind-operation", Date: "2026-09-09", RepositoryID: journalOwnerA, LedgerID: transactionCollection, Path: "Decisions/fork.md", BindingID: "binding-replacement", ParentBindingID: "binding-initial", SourceOwnerID: journalOwnerB, Source: SourceLocator{Root: SourceRootPlanning, Path: "Decisions/replacement.md"}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewForkAdoption(ForkAdoptionSnapshot{ConfigBefore: f.newConfig, Local: local, Source: replacement, Collections: map[CollectionID]*Collection{parent.ID: parent}}, proposal)
	if err != nil {
		t.Fatal(err)
	}
	f.oldConfig = append([]byte(nil), f.newConfig...)
	oldFork := append([]byte(nil), f.newFork...)
	f.oldFork, f.forkExists = oldFork, true
	for _, change := range preview.Envelope.Changes {
		if change.Root == SourceRootPlanning {
			f.newFork = []byte(change.After)
		}
	}
	f.request = ForkTransactionRequest{Repository: f.repository, Planning: f.planning, OwnerID: journalOwnerA, Collections: []CollectionID{transactionCollection, transactionSourceCollection, transactionReplacementCollection}, Preview: preview.Envelope, ApprovalDigest: preview.Envelope.Digest}
	return f, oldFork, f.newFork
}

func detachTransactionFixture(t *testing.T) (*transactionFixture, []byte, []byte) {
	t.Helper()
	f := newTransactionFixture(t, "selection-initial-adopt-detach")
	local, parent := persistAdoptedTransactionFixture(t, f)
	localDecision := []byte("decisions:\n  - id: D-0100\n    kind: decision\n    status: accepted\n    date: 2026-09-01\n    decided_by: user\n    statement: retained local authority\n    rationale: approved local requirement\n    scope: []\n    tags: []\n    rejected: []")
	f.newFork = bytes.Replace(f.newFork, []byte("decisions: []"), localDecision, 1)
	if err := os.WriteFile(f.forkPath, f.newFork, 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/fork.md"})
	if err != nil {
		t.Fatal(err)
	}
	proposal := json.RawMessage(`{"version":1,"operation":"detach","operationId":"selection-detach-operation","date":"2026-09-09","decidedBy":"user-approved","confirmation":"detach this exact selected fork and retain local history"}`)
	preview, err := PreviewForkRestoration(ForkRestorationSnapshot{ConfigBefore: f.newConfig, Local: local, Collections: map[CollectionID]*Collection{parent.ID: parent, local.ID: local}}, proposal)
	if err != nil {
		t.Fatal(err)
	}
	f.oldConfig = append([]byte(nil), f.newConfig...)
	oldFork := append([]byte(nil), f.newFork...)
	f.oldFork, f.forkExists = oldFork, true
	for _, change := range preview.Envelope.Changes {
		if change.Root == SourceRootRepository {
			f.newConfig = []byte(change.After)
		} else if change.Root == SourceRootPlanning {
			f.newFork = []byte(change.After)
		}
	}
	f.request = ForkTransactionRequest{Repository: f.repository, Planning: f.planning, OwnerID: journalOwnerA, Collections: []CollectionID{transactionCollection, transactionSourceCollection}, Preview: preview.Envelope, ApprovalDigest: preview.Envelope.Digest}
	return f, oldFork, f.newFork
}

func TestForkSelectionPublicationOperations(t *testing.T) {
	tests := []struct {
		name     string
		fixture  func(*testing.T) (*transactionFixture, []byte, []byte)
		wantMode string
	}{
		{name: "rebind", fixture: rebindTransactionFixture, wantMode: "fork"},
		{name: "detach", fixture: detachTransactionFixture, wantMode: "detached"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, oldFork, wantFork := test.fixture(t)
			if bytes.Equal(oldFork, wantFork) {
				t.Fatal("real preview builder did not produce a publication change")
			}
			result, err := f.transaction(t).Apply(context.Background())
			if err != nil {
				t.Fatalf("apply exact approved %s selection publication: %v", test.name, err)
			}
			if result == nil || result.Outcome != ForkTransactionCommitted || result.OperationID != f.request.Preview.OperationID {
				t.Fatalf("%s publication did not report durable commit: %#v", test.name, result)
			}
			for _, collection := range f.request.Collections {
				inspection, inspectErr := InspectForkTransaction(f.repository, f.planning, result.OperationID, collection)
				if inspectErr != nil || inspection == nil || inspection.Outcome != ForkTransactionCommitted || inspection.Authority != ResolutionComplete {
					t.Fatalf("%s collection %s did not round trip as complete: inspection=%#v err=%v", test.name, collection, inspection, inspectErr)
				}
			}
			if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, f.newConfig) {
				t.Fatalf("%s config publication mismatch", test.name)
			}
			if got := readTransactionBytes(t, f.forkPath); !bytes.Equal(got, f.newFork) {
				t.Fatalf("%s local-ledger publication mismatch", test.name)
			}
			selection, readErr := ReadSelection(f.repository)
			if readErr != nil || selection.Config.Mode != test.wantMode {
				t.Fatalf("%s selection did not round trip with mode %q: selection=%#v err=%v", test.name, test.wantMode, selection, readErr)
			}
			if test.name == "detach" {
				local, loadErr := LoadSelectedCollection(selection)
				if loadErr != nil {
					t.Fatalf("load detached collection: %v", loadErr)
				}
				parent, loadErr := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionSourceCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/parent.md"})
				if loadErr != nil {
					t.Fatalf("load detached parent: %v", loadErr)
				}
				view, composeErr := Compose(local.ID, "detached", map[CollectionID]*Collection{local.ID: local, parent.ID: parent})
				if composeErr != nil || view.Resolution != ResolutionComplete {
					t.Fatalf("compose detached authority: view=%#v err=%v", view, composeErr)
				}
				inherited := QualifiedID("ledger:" + string(parent.ID) + ":D-0001")
				for _, record := range view.Records {
					if record.ID == inherited && record.Applicability == "binding" {
						t.Fatalf("detached Compose still bound inherited target %s", inherited)
					}
				}
			}
		})
	}
}

func TestForkTransactionForeignBarrierAndReplay(t *testing.T) {
	t.Run("foreign barrier", func(t *testing.T) {
		f := newTransactionFixture(t, "transaction-foreign-barrier")
		store, err := OpenLocalStore(f.repository, f.planning, "foreign-barrier")
		if err != nil {
			t.Fatal(err)
		}
		journal, err := NewForkJournal(journalOwnerB, []CollectionID{transactionCollection}, journalPreview(t, "foreign-pending-operation"))
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
		result, err := f.transaction(t).Apply(context.Background())
		if !errors.Is(err, ErrForkBarrierPending) || result != nil {
			t.Fatalf("foreign pending barrier was not refused: result=%#v err=%v", result, err)
		}
		if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, f.oldConfig) {
			t.Fatalf("foreign-barrier refusal changed config: %q", got)
		}
		if _, exists, readErr := readOptionalTransactionBytes(f.forkPath); readErr != nil || exists {
			t.Fatalf("foreign-barrier refusal created local authority: exists=%v err=%v", exists, readErr)
		}
	})

	t.Run("pending journal replay", func(t *testing.T) {
		f := newTransactionFixture(t, "transaction-pending-replay")
		store, err := OpenLocalStore(f.repository, f.planning, "pending-replay")
		if err != nil {
			t.Fatal(err)
		}
		journal, err := NewForkJournal(f.request.OwnerID, f.request.Collections, f.request.Preview)
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
		journalPath := filepath.Join(f.planning, filepath.FromSlash(forkJournalPath(transactionCollection, journal.OperationID)))
		journalBefore := readTransactionBytes(t, journalPath)
		result, replayErr := f.transaction(t).Apply(context.Background())
		if replayErr == nil || result != nil {
			t.Fatalf("pending journal operation was reused: result=%#v err=%v", result, replayErr)
		}
		if got := readTransactionBytes(t, journalPath); !bytes.Equal(got, journalBefore) {
			t.Fatalf("pending replay modified existing journal:\n got %s\nwant %s", got, journalBefore)
		}
		assertTransactionAuthorityUnchanged(t, f)
	})

	t.Run("clean rollback replay", func(t *testing.T) {
		f := newTransactionFixture(t, "transaction-rollback-replay")
		tx := f.transaction(t)
		tx.failpoint = func(point ForkTransactionPoint) error {
			if point == ForkTransactionAfterJournal {
				return errors.New("injected clean rollback")
			}
			return nil
		}
		result, err := tx.Apply(context.Background())
		if err == nil || result == nil || result.Outcome != ForkTransactionRolledBack {
			t.Fatalf("setup did not produce clean rollback: result=%#v err=%v", result, err)
		}
		journalPath := filepath.Join(f.planning, filepath.FromSlash(forkJournalPath(transactionCollection, f.request.Preview.OperationID)))
		journalBefore := readTransactionBytes(t, journalPath)
		replay, replayErr := f.transaction(t).Apply(context.Background())
		if replayErr == nil || replay != nil {
			t.Fatalf("rolled-back journal operation was reused: result=%#v err=%v", replay, replayErr)
		}
		if got := readTransactionBytes(t, journalPath); !bytes.Equal(got, journalBefore) {
			t.Fatalf("rollback replay modified existing journal:\n got %s\nwant %s", got, journalBefore)
		}
		assertTransactionAuthorityUnchanged(t, f)
	})

	t.Run("malformed journal replay", func(t *testing.T) {
		f := newTransactionFixture(t, "transaction-malformed-replay")
		journalPath := filepath.Join(f.planning, filepath.FromSlash(forkJournalPath(transactionCollection, f.request.Preview.OperationID)))
		if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
			t.Fatal(err)
		}
		malformed := []byte(`{"status":"truncated"`)
		if err := os.WriteFile(journalPath, malformed, 0o600); err != nil {
			t.Fatal(err)
		}
		result, replayErr := f.transaction(t).Apply(context.Background())
		if replayErr == nil || result != nil {
			t.Fatalf("malformed existing journal was replaced: result=%#v err=%v", result, replayErr)
		}
		if got := readTransactionBytes(t, journalPath); !bytes.Equal(got, malformed) {
			t.Fatalf("malformed replay modified existing journal: %q", got)
		}
		assertTransactionAuthorityUnchanged(t, f)
	})

	t.Run("refused replay", func(t *testing.T) {
		f := newTransactionFixture(t, "transaction-refused-replay")
		first, err := f.transaction(t).Apply(context.Background())
		if err != nil || first == nil || first.Outcome != ForkTransactionCommitted {
			t.Fatalf("initial transaction did not commit: result=%#v err=%v", first, err)
		}
		configBefore := readTransactionBytes(t, f.configPath)
		forkBefore := readTransactionBytes(t, f.forkPath)
		replay, replayErr := f.transaction(t).Apply(context.Background())
		if replayErr == nil || replay != nil {
			t.Fatalf("approved operation identity was replayed: result=%#v err=%v", replay, replayErr)
		}
		if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, configBefore) {
			t.Fatalf("refused replay changed committed config: %q", got)
		}
		if got := readTransactionBytes(t, f.forkPath); !bytes.Equal(got, forkBefore) {
			t.Fatalf("refused replay changed committed ledger: %q", got)
		}
	})
}

func assertTransactionAuthorityUnchanged(t *testing.T, f *transactionFixture) {
	t.Helper()
	if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, f.oldConfig) {
		t.Fatalf("replay refusal changed config: %q", got)
	}
	if _, exists, err := readOptionalTransactionBytes(f.forkPath); err != nil || exists {
		t.Fatalf("replay refusal changed local ledger: exists=%t err=%v", exists, err)
	}
}
