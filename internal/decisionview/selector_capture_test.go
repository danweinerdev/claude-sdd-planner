package decisionview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const selectorCrashExit = 86

func selectorCaptureWant(t *testing.T, f *transactionFixture) *ForkSelectorCapture {
	t.Helper()
	original := append([]byte(nil), f.oldConfig...)
	final := append([]byte(nil), f.newConfig...)
	if f.request.Preview.Operation == "rebind" {
		final = append([]byte(nil), original...)
	}
	intermediate, err := transactionPendingConfig(final, f.request.Preview.OperationID, forkJournalPath(f.request.Collections[0], f.request.Preview.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	want := &ForkSelectorCapture{
		Version:      Version1,
		OperationID:  f.request.Preview.OperationID,
		SourceDigest: collectionDigest(original),
		Original:     string(original),
		Intermediate: string(intermediate),
		Final:        string(final),
	}
	if f.request.Preview.Operation == "rebind" {
		matches := 0
		for _, source := range f.request.Preview.Sources {
			if source.Root == SourceRootRepository && source.Path == "planning-config.json" {
				matches++
				if source.Digest != want.SourceDigest {
					t.Fatalf("rebind capture source boundary = %q, want %q", want.SourceDigest, source.Digest)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("rebind preview has %d config source boundaries, want 1", matches)
		}
	}
	return want
}

func assertSelectorCapture(t *testing.T, f *transactionFixture) {
	t.Helper()
	store, err := OpenLocalStore(f.repository, f.planning, "selector-capture-read")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.LoadForkSelectorCapture(f.request.Preview.OperationID)
	if err != nil {
		t.Fatalf("reload recovery-grade selector capture from the private journal: %v", err)
	}
	want := selectorCaptureWant(t, f)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exact selector bytes did not survive private-journal reload:\n got: %#v\nwant: %#v", got, want)
	}
	got.Original = "caller mutation"
	got.Intermediate = "caller mutation"
	got.Final = "caller mutation"
	again, err := store.LoadForkSelectorCapture(f.request.Preview.OperationID)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("recovery accessor retained aliases to caller-owned bytes: got=%#v err=%v", again, err)
	}
}

func TestForkSelectorCapturePublicRoundTrip(t *testing.T) {
	fixtures := map[string]func(*testing.T) *transactionFixture{
		"adopt": func(t *testing.T) *transactionFixture {
			return newTransactionFixture(t, "selector-capture-adopt-round-trip")
		},
		"detach": func(t *testing.T) *transactionFixture {
			f, _, _ := detachTransactionFixture(t)
			return f
		},
		"rebind-noncanonical-config": noncanonicalRebindFixture,
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			result, err := f.transaction(t).Apply(context.Background())
			if err != nil || result == nil || result.Outcome != ForkTransactionCommitted {
				t.Fatalf("publish approved %s transaction: result=%#v err=%v", name, result, err)
			}
			assertSelectorCapture(t, f)
		})
	}
}

func noncanonicalRebindFixture(t *testing.T) *transactionFixture {
	t.Helper()
	f, _, _ := rebindTransactionFixture(t)
	var config map[string]json.RawMessage
	if err := json.Unmarshal(f.oldConfig, &config); err != nil {
		t.Fatal(err)
	}
	noncanonical := []byte(fmt.Sprintf("{\n  \"unrelated\" : %s,\n\t\"repositoryId\": %s, \"decisionLog\" : %s,\n  \"planningRoot\" : %s\n}\n", config["unrelated"], config["repositoryId"], config["decisionLog"], config["planningRoot"]))
	if bytes.Equal(noncanonical, f.oldConfig) {
		t.Fatal("rebind selector fixture is accidentally canonical")
	}
	if err := os.WriteFile(f.configPath, noncanonical, 0o644); err != nil {
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
	replacement, err := LoadCollection(Roots{Repository: f.repository, Planning: f.planning}, transactionReplacementCollection, SourceLocator{Root: SourceRootPlanning, Path: "Decisions/replacement.md"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewForkAdoption(ForkAdoptionSnapshot{ConfigBefore: noncanonical, Local: local, Source: replacement, Collections: map[CollectionID]*Collection{parent.ID: parent}}, f.request.Preview.Request)
	if err != nil {
		t.Fatal(err)
	}
	f.oldConfig = append([]byte(nil), noncanonical...)
	f.newConfig = append([]byte(nil), noncanonical...)
	f.request.Preview = preview.Envelope
	f.request.ApprovalDigest = preview.Envelope.Digest
	return f
}

func TestForkSelectorCaptureCrashBoundaries(t *testing.T) {
	points := []ForkTransactionPoint{
		ForkTransactionAfterJournal,
		ForkTransactionAfterBarriers,
		ForkTransactionAfterIntermediateConfig,
		ForkTransactionAfterFinalConfig,
	}
	fixtures := map[string]func(*testing.T) *transactionFixture{
		"adopt": func(t *testing.T) *transactionFixture {
			return newTransactionFixture(t, "selector-capture-crash-adopt")
		},
		"detach": func(t *testing.T) *transactionFixture {
			f, _, _ := detachTransactionFixture(t)
			return f
		},
		"rebind-noncanonical-config": noncanonicalRebindFixture,
	}
	for operation, fixture := range fixtures {
		for _, point := range points {
			t.Run(operation+"/"+string(point), func(t *testing.T) {
				f := fixture(t)
				requestPath := filepath.Join(t.TempDir(), "request.json")
				raw, err := json.Marshal(f.request)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(requestPath, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestSelectorCaptureCrashSubprocess$")
				cmd.Env = append(os.Environ(), "SDD_SELECTOR_CRASH_REQUEST="+requestPath, "SDD_SELECTOR_CRASH_POINT="+string(point))
				output, runErr := cmd.CombinedOutput()
				var exitErr *exec.ExitError
				if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != selectorCrashExit {
					t.Fatalf("subprocess did not crash at %s: err=%v output=%s", point, runErr, output)
				}
				wantConfig := f.oldConfig
				if point == ForkTransactionAfterIntermediateConfig {
					wantConfig = []byte(selectorCaptureWant(t, f).Intermediate)
				} else if point == ForkTransactionAfterFinalConfig {
					wantConfig = []byte(selectorCaptureWant(t, f).Final)
				}
				if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, wantConfig) {
					t.Fatalf("config bytes at crash boundary %s:\n got %q\nwant %q", point, got, wantConfig)
				}
				assertSelectorCapture(t, f)
			})
		}
	}
}

func TestForkSelectorCaptureLegacyJournalReadCompatibility(t *testing.T) {
	repo, planning := storeFixture(t)
	store, err := OpenLocalStore(repo, planning, "legacy-selector-journal-write")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewForkJournal(journalOwnerA, []CollectionID{journalLedger}, journalPreview(t, "legacy-selector-journal"))
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

	store, err = OpenLocalStore(repo, planning, "legacy-selector-journal-read")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.LoadForkJournal(journal.OperationID)
	if err != nil || loaded == nil || loaded.SelectorCapture != nil {
		t.Fatalf("capture-less legacy journal did not load through journal API: journal=%#v err=%v", loaded, err)
	}
	barrier, err := store.InspectForkBarrier(journalLedger)
	if err != nil || barrier == nil || barrier.OperationID != journal.OperationID {
		t.Fatalf("capture-less legacy journal did not load through barrier API: barrier=%#v err=%v", barrier, err)
	}
	capture, err := store.LoadForkSelectorCapture(journal.OperationID)
	if capture != nil || !errors.Is(err, ErrForkSelectorCaptureUnavailable) {
		t.Fatalf("capture-less legacy journal authorized recovery bytes: capture=%#v err=%v", capture, err)
	}
}

func TestSelectorCaptureCrashSubprocess(t *testing.T) {
	requestPath := os.Getenv("SDD_SELECTOR_CRASH_REQUEST")
	if requestPath == "" {
		t.Skip("subprocess helper")
	}
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request ForkTransactionRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	tx, err := NewForkTransaction(request)
	if err != nil {
		t.Fatal(err)
	}
	point := ForkTransactionPoint(os.Getenv("SDD_SELECTOR_CRASH_POINT"))
	tx.failpoint = func(got ForkTransactionPoint) error {
		if got == point {
			os.Exit(selectorCrashExit)
		}
		return nil
	}
	if _, err := tx.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("transaction passed requested crash point %s", point)
}

func TestForkSelectorCaptureRejectsUnboundBytes(t *testing.T) {
	f := newTransactionFixture(t, "selector-capture-reject-unbound")
	tx := f.transaction(t)
	tx.failpoint = func(point ForkTransactionPoint) error {
		if point == ForkTransactionAfterBarriers {
			return errors.New("retain pending journal for capture validation")
		}
		return nil
	}
	_, _ = tx.Apply(context.Background())

	journalPath := filepath.Join(f.planning, filepath.FromSlash(forkJournalPath(f.request.Collections[0], f.request.Preview.OperationID)))
	journalRaw := readTransactionBytes(t, journalPath)
	var journal map[string]json.RawMessage
	if err := json.Unmarshal(journalRaw, &journal); err != nil {
		t.Fatal(err)
	}
	want := selectorCaptureWant(t, f)
	captureRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	journal["selectorCapture"] = captureRaw
	validRaw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, validRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	assertSelectorCapture(t, f)

	mutations := map[string]func(map[string]json.RawMessage){
		"missing": func(fields map[string]json.RawMessage) { delete(fields, "selectorCapture") },
		"corrupt": func(fields map[string]json.RawMessage) { fields["selectorCapture"] = json.RawMessage(`"corrupt"`) },
		"unsupported-version": func(fields map[string]json.RawMessage) {
			changed := *want
			changed.Version = SchemaVersion(2)
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
		"unbound-operation": func(fields map[string]json.RawMessage) {
			changed := *want
			changed.OperationID = "different-approved-operation"
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
		"unbound-source-digest": func(fields map[string]json.RawMessage) {
			changed := *want
			changed.SourceDigest = collectionDigest([]byte("different source boundary\n"))
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
		"unbound-original": func(fields map[string]json.RawMessage) {
			changed := *want
			changed.Original = "guessed original bytes\n"
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
		"wrong-pending-marker": func(fields map[string]json.RawMessage) {
			changed := *want
			changed.Intermediate = want.Final
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
		"wrong-pending-operation": func(fields map[string]json.RawMessage) {
			changed := *want
			pending, _ := transactionPendingConfig([]byte(want.Final), "different-pending-operation", forkJournalPath(f.request.Collections[0], f.request.Preview.OperationID))
			changed.Intermediate = string(pending)
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
		"wrong-pending-journal": func(fields map[string]json.RawMessage) {
			changed := *want
			pending, _ := transactionPendingConfig([]byte(want.Final), want.OperationID, "Decisions/.fork-state/other/journals/wrong.json")
			changed.Intermediate = string(pending)
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
		"unbound-final": func(fields map[string]json.RawMessage) {
			changed := *want
			changed.Final = "guessed final bytes\n"
			fields["selectorCapture"], _ = json.Marshal(changed)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(validRaw, &fields); err != nil {
				t.Fatal(err)
			}
			mutate(fields)
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := OpenLocalStore(f.repository, f.planning, "selector-capture-reject")
			if err != nil {
				t.Fatal(err)
			}
			got, loadErr := store.LoadForkSelectorCapture(f.request.Preview.OperationID)
			store.Close()
			if loadErr == nil || got != nil {
				t.Fatalf("%s selector capture authorized recovery bytes: %#v, %v", name, got, loadErr)
			}
		})
	}
}
