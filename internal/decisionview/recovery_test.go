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
	"strings"
	"testing"
)

const recoveryCrashExit = 86
const recoveryRetryCrashExit = 85

type recoveryTreeEntry struct {
	Mode os.FileMode
	Data []byte
}

func recoveryTree(t *testing.T, roots ...string) map[string]recoveryTreeEntry {
	t.Helper()
	got := map[string]recoveryTreeEntry{}
	for rootIndex, root := range roots {
		err := filepath.Walk(root, func(name string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			key := fmt.Sprintf("%d:%s", rootIndex, filepath.ToSlash(rel))
			entry := recoveryTreeEntry{Mode: info.Mode()}
			if info.Mode().IsRegular() {
				entry.Data, err = os.ReadFile(name)
				if err != nil {
					return err
				}
			}
			got[key] = entry
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return got
}

func recoveryFixture(t *testing.T, operation, id string) *transactionFixture {
	t.Helper()
	switch operation {
	case "adopt":
		return newTransactionFixture(t, id)
	case "rebind":
		f, _, _ := rebindTransactionFixture(t)
		return f
	case "rebind-noncanonical":
		return noncanonicalRebindFixture(t)
	case "detach":
		f, _, _ := detachTransactionFixture(t)
		return f
	default:
		t.Fatalf("unknown recovery fixture operation %q", operation)
		return nil
	}
}

func runRecoveryCrash(t *testing.T, f *transactionFixture, point ForkTransactionPoint) {
	t.Helper()
	raw, err := json.Marshal(f.request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(requestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestForkRecoveryCrashAndLostReply$")
	cmd.Env = append(os.Environ(),
		"SDD_RECOVERY_CRASH_HELPER=1",
		"SDD_RECOVERY_REQUEST="+requestPath,
		"SDD_RECOVERY_POINT="+string(point),
	)
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != recoveryCrashExit {
		t.Fatalf("crash helper did not exit at %s: %v", point, err)
	}
}

func recoveryCrashChild() {
	raw, err := os.ReadFile(os.Getenv("SDD_RECOVERY_REQUEST"))
	if err != nil {
		os.Exit(87)
	}
	var request ForkTransactionRequest
	if json.Unmarshal(raw, &request) != nil {
		os.Exit(88)
	}
	tx, err := NewForkTransaction(request)
	if err != nil {
		os.Exit(89)
	}
	want := ForkTransactionPoint(os.Getenv("SDD_RECOVERY_POINT"))
	tx.failpoint = func(point ForkTransactionPoint) error {
		if point == want {
			os.Exit(recoveryCrashExit)
		}
		return nil
	}
	_, _ = tx.Apply(context.Background())
	os.Exit(90)
}

func runRecoveryRetryCrash(t *testing.T, f *transactionFixture, action ForkRecoveryAction, point forkRecoveryPoint, occurrence int) {
	t.Helper()
	preview := previewRecoveryUnchanged(t, f, action)
	request := ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: preview, ApprovalDigest: preview.Digest}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(t.TempDir(), "recovery-request.json")
	if err := os.WriteFile(requestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestForkRecoveryCrashAndLostReply$")
	cmd.Env = append(os.Environ(),
		"SDD_RECOVERY_RETRY_HELPER=1",
		"SDD_RECOVERY_RETRY_REQUEST="+requestPath,
		"SDD_RECOVERY_RETRY_POINT="+string(point),
		fmt.Sprintf("SDD_RECOVERY_RETRY_OCCURRENCE=%d", occurrence),
	)
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != recoveryRetryCrashExit {
		t.Fatalf("recovery helper did not crash at %s occurrence %d: %v", point, occurrence, err)
	}
}

func recoveryRetryCrashChild() {
	raw, err := os.ReadFile(os.Getenv("SDD_RECOVERY_RETRY_REQUEST"))
	if err != nil {
		os.Exit(81)
	}
	var request ForkRecoveryRequest
	if json.Unmarshal(raw, &request) != nil {
		os.Exit(82)
	}
	want := forkRecoveryPoint(os.Getenv("SDD_RECOVERY_RETRY_POINT"))
	occurrence := 0
	if _, err := fmt.Sscanf(os.Getenv("SDD_RECOVERY_RETRY_OCCURRENCE"), "%d", &occurrence); err != nil || occurrence < 1 {
		os.Exit(83)
	}
	seen := 0
	forkRecoveryFailpoint = func(point forkRecoveryPoint) error {
		if point == want {
			seen++
			if seen == occurrence {
				os.Exit(recoveryRetryCrashExit)
			}
		}
		return nil
	}
	_, _ = RecoverForkTransaction(context.Background(), request)
	os.Exit(84)
}

func previewRecoveryUnchanged(t *testing.T, f *transactionFixture, action ForkRecoveryAction) *ForkRecoveryPreview {
	t.Helper()
	before := recoveryTree(t, f.repository, f.planning)
	inspection, err := InspectForkRecovery(f.repository, f.planning, f.request.Preview.OperationID)
	if err != nil || inspection == nil {
		t.Fatalf("inspect persisted recovery state: inspection=%#v err=%v", inspection, err)
	}
	if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, before) {
		t.Fatal("recovery inspection changed repository or planning bytes")
	}
	preview, err := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, action)
	if err != nil || preview == nil || preview.Digest == "" || preview.CurrentStateDigest == "" || preview.OperationID != f.request.Preview.OperationID || preview.Action != action {
		t.Fatalf("preview %s recovery: preview=%#v err=%v", action, preview, err)
	}
	if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, before) {
		t.Fatal("recovery preview changed repository or planning bytes")
	}
	return preview
}

func applyRecovery(t *testing.T, f *transactionFixture, action ForkRecoveryAction) *ForkTransactionResult {
	t.Helper()
	preview := previewRecoveryUnchanged(t, f, action)
	result, err := RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: preview, ApprovalDigest: preview.Digest})
	if err != nil {
		t.Fatalf("apply approved %s recovery: %v", action, err)
	}
	return result
}

func assertRecoveryBytes(t *testing.T, f *transactionFixture, newAuthority bool) {
	t.Helper()
	wantConfig := f.oldConfig
	if newAuthority {
		wantConfig = f.newConfig
	}
	if got := readTransactionBytes(t, f.configPath); !bytes.Equal(got, wantConfig) {
		t.Fatalf("config authority mismatch after recovery\ngot:  %q\nwant: %q", got, wantConfig)
	}
	fork, exists, err := readOptionalTransactionBytes(f.forkPath)
	if err != nil {
		t.Fatal(err)
	}
	wantExists, wantFork := f.forkExists, f.oldFork
	if newAuthority {
		wantExists, wantFork = true, f.newFork
	}
	if exists != wantExists || (exists && !bytes.Equal(fork, wantFork)) {
		t.Fatalf("ledger authority mismatch after recovery: exists=%t want=%t", exists, wantExists)
	}
}

func TestForkRecoveryPublicRoundTrip(t *testing.T) {
	operations := []string{"adopt", "rebind", "rebind-noncanonical", "detach"}
	cases := []struct {
		name   string
		point  ForkTransactionPoint
		action ForkRecoveryAction
		new    bool
	}{
		{"journal-only-discard", ForkTransactionAfterJournal, ForkRecoveryDiscardStaging, false},
		{"barrier-discard", ForkTransactionAfterBarriers, ForkRecoveryDiscardStaging, false},
		{"pending-marker-rollback", ForkTransactionAfterIntermediateConfig, ForkRecoveryRollback, false},
		{"publication-window-rollback", ForkTransactionBeforePlanningPublish, ForkRecoveryRollback, false},
		{"published-ledger-rollback", ForkTransactionAfterPlanningPublish, ForkRecoveryRollback, false},
		{"published-ledger-finish", ForkTransactionAfterPlanningPublish, ForkRecoveryFinish, true},
		{"final-selector-finish", ForkTransactionAfterFinalConfig, ForkRecoveryFinish, true},
	}
	for _, operation := range operations {
		for _, test := range cases {
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				f := recoveryFixture(t, operation, "recovery-"+operation+"-"+test.name)
				id := f.request.Preview.OperationID
				runRecoveryCrash(t, f, test.point)
				result := applyRecovery(t, f, test.action)
				wantOutcome := ForkTransactionRolledBack
				if test.new {
					wantOutcome = ForkTransactionCommitted
				}
				if result == nil || result.OperationID != id || result.Outcome != wantOutcome {
					t.Fatalf("wrong recovery result: %#v", result)
				}
				assertRecoveryBytes(t, f, test.new)
				inspection, err := InspectForkTransaction(f.repository, f.planning, id, f.request.Collections[0])
				if err != nil || inspection == nil || inspection.Outcome != wantOutcome || inspection.Authority != ResolutionComplete {
					t.Fatalf("terminal recovery did not round trip: inspection=%#v err=%v", inspection, err)
				}
				journalBefore := recoveryTree(t, f.repository, f.planning)
				if replay, replayErr := f.transaction(t).Apply(context.Background()); replayErr == nil || replay != nil {
					t.Fatalf("recovered operation ID was reusable: result=%#v err=%v", replay, replayErr)
				}
				if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, journalBefore) {
					t.Fatal("reused operation ID changed terminal history")
				}
			})
		}
	}
}

func TestForkRecoveryDivergenceNoOpGuard(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		name := "no-op-control"
		if mutate {
			name = "changed-current-bytes"
		}
		t.Run(name, func(t *testing.T) {
			f := recoveryFixture(t, "adopt", "recovery-divergence-"+name)
			runRecoveryCrash(t, f, ForkTransactionAfterPlanningPublish)
			preview := previewRecoveryUnchanged(t, f, ForkRecoveryRollback)
			if mutate {
				external := []byte("externally changed current ledger bytes\n")
				if err := os.WriteFile(f.forkPath, external, 0o644); err != nil {
					t.Fatal(err)
				}
				before := recoveryTree(t, f.repository, f.planning)
				result, err := RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: preview, ApprovalDigest: preview.Digest})
				if err == nil || result != nil {
					t.Fatalf("stale current-state digest overwrote divergence: result=%#v err=%v", result, err)
				}
				if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, before) {
					t.Fatal("divergence refusal changed current bytes or recovery history")
				}
				return
			}
			result, err := RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: preview, ApprovalDigest: preview.Digest})
			if err != nil || result == nil || result.Outcome != ForkTransactionRolledBack {
				t.Fatalf("no-op divergence control did not recover: result=%#v err=%v", result, err)
			}
		})
	}

	t.Run("unapproved-staging-never-activated", func(t *testing.T) {
		f := recoveryFixture(t, "adopt", "recovery-unapproved-staging")
		runRecoveryCrash(t, f, ForkTransactionAfterPlanningPublish)
		preview := previewRecoveryUnchanged(t, f, ForkRecoveryFinish)
		before := recoveryTree(t, f.repository, f.planning)
		result, err := RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: preview, ApprovalDigest: "sha256:unapproved"})
		if err == nil || result != nil {
			t.Fatalf("unapproved finish activated staging: result=%#v err=%v", result, err)
		}
		if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, before) {
			t.Fatal("unapproved recovery changed staged or committed bytes")
		}
	})

	for _, corruption := range []string{"capture-less", "corrupt-capture"} {
		t.Run(corruption+"-fails-closed", func(t *testing.T) {
			f := recoveryFixture(t, "adopt", "recovery-"+corruption)
			runRecoveryCrash(t, f, ForkTransactionAfterJournal)
			journalPath := filepath.Join(f.planning, filepath.FromSlash(forkJournalPath(f.request.Collections[0], f.request.Preview.OperationID)))
			raw := readTransactionBytes(t, journalPath)
			var journal map[string]any
			if err := json.Unmarshal(raw, &journal); err != nil {
				t.Fatal(err)
			}
			if corruption == "capture-less" {
				delete(journal, "selectorCapture")
			} else {
				capture := journal["selectorCapture"].(map[string]any)
				capture["sourceDigest"] = "sha256:corrupt"
			}
			raw, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			before := recoveryTree(t, f.repository, f.planning)
			if inspection, err := InspectForkRecovery(f.repository, f.planning, f.request.Preview.OperationID); err == nil || inspection != nil {
				t.Fatalf("%s journal inspection guessed recovery authority: inspection=%#v err=%v", corruption, inspection, err)
			}
			if preview, err := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryRollback); err == nil || preview != nil {
				t.Fatalf("%s journal preview guessed selector bytes: preview=%#v err=%v", corruption, preview, err)
			}
			if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, before) {
				t.Fatal("failed-closed journal inspection changed bytes")
			}
		})
	}
}

func TestForkRecoveryCrashAndLostReply(t *testing.T) {
	if os.Getenv("SDD_RECOVERY_CRASH_HELPER") == "1" {
		recoveryCrashChild()
		return
	}
	if os.Getenv("SDD_RECOVERY_RETRY_HELPER") == "1" {
		recoveryRetryCrashChild()
		return
	}

	t.Run("crash-bypasses-graceful-rollback", func(t *testing.T) {
		f := recoveryFixture(t, "adopt", "recovery-real-crash")
		runRecoveryCrash(t, f, ForkTransactionAfterPlanningPublish)
		if bytes.Equal(readTransactionBytes(t, f.configPath), f.oldConfig) {
			t.Fatal("crash helper performed graceful rollback instead of leaving the exact persisted boundary")
		}
		result := applyRecovery(t, f, ForkRecoveryFinish)
		if result == nil || result.Outcome != ForkTransactionCommitted {
			t.Fatalf("crash recovery did not finish: %#v", result)
		}
		assertRecoveryBytes(t, f, true)
	})

	for _, point := range []ForkTransactionPoint{ForkTransactionAfterCommitRecord, ForkTransactionAfterBarrierRelease} {
		t.Run("lost-reply/"+string(point), func(t *testing.T) {
			f := recoveryFixture(t, "adopt", "recovery-lost-reply-"+string(point))
			runRecoveryCrash(t, f, point)
			assertRecoveryBytes(t, f, true)
			result := applyRecovery(t, f, ForkRecoveryFinish)
			if result == nil || result.Outcome != ForkTransactionCommitted {
				t.Fatalf("lost committed reply was not recognized: %#v", result)
			}
			committed := recoveryTree(t, f.repository, f.planning)
			rollback, previewErr := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryRollback)
			if previewErr == nil {
				got, err := RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: rollback, ApprovalDigest: rollback.Digest})
				if err == nil || got != nil {
					t.Fatalf("committed history was rolled back: result=%#v err=%v", got, err)
				}
			}
			if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, committed) {
				t.Fatal("committed rollback refusal erased or rewrote history")
			}
		})
	}
}

func TestForkRecoveryRecoveryCrashRetry(t *testing.T) {
	type crashCase struct {
		point      forkRecoveryPoint
		occurrence int
	}
	operations := []string{"adopt", "rebind", "detach"}
	for _, operation := range operations {
		probe := recoveryFixture(t, operation, "recovery-crash-count-"+operation)
		barriers := len(probe.request.Collections)
		actions := []struct {
			name        string
			action      ForkRecoveryAction
			original    ForkTransactionPoint
			wantNew     bool
			crashPoints []crashCase
		}{
			{
				name: "finish", action: ForkRecoveryFinish, original: ForkTransactionAfterJournal, wantNew: true,
				crashPoints: []crashCase{{forkRecoveryAfterIntermediateConfig, 1}, {forkRecoveryAfterLedgerPublication, 1}, {forkRecoveryAfterFinalConfig, 1}, {forkRecoveryAfterJournalWrite, 1}},
			},
			{
				name: "rollback", action: ForkRecoveryRollback, original: ForkTransactionAfterPlanningPublish,
				crashPoints: []crashCase{{forkRecoveryAfterIntermediateConfig, 1}, {forkRecoveryAfterLedgerPublication, 1}, {forkRecoveryAfterJournalWrite, 1}},
			},
			{
				name: "discard", action: ForkRecoveryDiscardStaging, original: ForkTransactionAfterJournal,
				crashPoints: []crashCase{{forkRecoveryAfterJournalWrite, 1}},
			},
		}
		for actionIndex := range actions {
			action := &actions[actionIndex]
			barrierWrites := barriers
			if action.action == ForkRecoveryFinish {
				barrierWrites *= 2
			}
			for occurrence := 1; occurrence <= barrierWrites; occurrence++ {
				action.crashPoints = append(action.crashPoints, crashCase{forkRecoveryAfterBarrierPublication, occurrence})
			}
			for _, crash := range action.crashPoints {
				name := fmt.Sprintf("%s/%s/%s-%d", operation, action.name, crash.point, crash.occurrence)
				t.Run(name, func(t *testing.T) {
					f := recoveryFixture(t, operation, "recovery-self-crash-"+strings.ReplaceAll(name, "/", "-"))
					runRecoveryCrash(t, f, action.original)
					runRecoveryRetryCrash(t, f, action.action, crash.point, crash.occurrence)
					inspection, err := InspectForkRecovery(f.repository, f.planning, f.request.Preview.OperationID)
					if err != nil || inspection == nil {
						t.Fatalf("inspect interrupted recovery: inspection=%#v err=%v", inspection, err)
					}
					result := applyRecovery(t, f, action.action)
					wantOutcome := ForkTransactionRolledBack
					if action.wantNew {
						wantOutcome = ForkTransactionCommitted
					}
					if result == nil || result.Outcome != wantOutcome {
						t.Fatalf("retry did not converge: %#v", result)
					}
					assertRecoveryBytes(t, f, action.wantNew)
					terminal, err := InspectForkTransaction(f.repository, f.planning, f.request.Preview.OperationID, f.request.Collections[0])
					if err != nil || terminal == nil || terminal.Outcome != wantOutcome || terminal.Authority != ResolutionComplete {
						t.Fatalf("retry left incomplete authority: inspection=%#v err=%v", terminal, err)
					}
				})
			}
		}
	}
}

func TestForkRecoveryRolledBackBarrierCompletion(t *testing.T) {
	for _, state := range []string{"missing", "pending", "partially-terminal"} {
		for _, action := range []ForkRecoveryAction{ForkRecoveryRollback, ForkRecoveryDiscardStaging} {
			t.Run(state+"/"+string(action), func(t *testing.T) {
				f := recoveryFixture(t, "detach", "recovery-rolled-back-"+state+"-"+string(action))
				runRecoveryCrash(t, f, ForkTransactionAfterJournal)
				journalPath := filepath.Join(f.planning, filepath.FromSlash(forkJournalPath(f.request.Collections[0], f.request.Preview.OperationID)))
				var journal ForkJournal
				if err := json.Unmarshal(readTransactionBytes(t, journalPath), &journal); err != nil {
					t.Fatal(err)
				}
				journal.Status = "rolled-back"
				raw, err := json.Marshal(&journal)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(journalPath, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				if state != "missing" {
					for index, id := range f.request.Collections {
						status := "pending"
						if state == "partially-terminal" && index == 0 {
							status = "rolled-back"
						}
						barrier := ForkBarrier{Version: Version1, OperationID: journal.OperationID, OwnerID: journal.OwnerID, Collection: id, Journal: forkJournalPath(journal.Collections[0], journal.OperationID), Status: status}
						raw, err := json.Marshal(&barrier)
						if err != nil {
							t.Fatal(err)
						}
						barrierPath := filepath.Join(f.planning, filepath.FromSlash(forkBarrierPath(id)))
						if err := os.MkdirAll(filepath.Dir(barrierPath), 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(barrierPath, raw, 0o600); err != nil {
							t.Fatal(err)
						}
					}
				}
				result := applyRecovery(t, f, action)
				if result == nil || result.Outcome != ForkTransactionRolledBack {
					t.Fatalf("rolled-back cleanup did not converge: %#v", result)
				}
				assertRecoveryBytes(t, f, false)
				for _, id := range f.request.Collections {
					inspection, err := InspectForkTransaction(f.repository, f.planning, journal.OperationID, id)
					if err != nil || inspection == nil || inspection.Outcome != ForkTransactionRolledBack || inspection.Authority != ResolutionComplete {
						t.Fatalf("collection %s did not receive terminal rollback barrier: inspection=%#v err=%v", id, inspection, err)
					}
				}
			})
		}
	}
}

func TestForkRecoveryForeignBarrierRefusal(t *testing.T) {
	f := recoveryFixture(t, "adopt", "recovery-operation-one")
	runRecoveryCrash(t, f, ForkTransactionAfterJournal)
	const foreignOperation = "recovery-operation-two"
	foreignPreview := *f.request.Preview
	foreignPreview.OperationID = foreignOperation
	foreignPreview.Digest = ""
	digest, err := previewDigest(&foreignPreview)
	if err != nil {
		t.Fatal(err)
	}
	foreignPreview.Digest = digest
	journal, err := NewForkJournal(f.request.OwnerID, f.request.Collections, &foreignPreview)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenLocalStore(f.repository, f.planning, "foreign-recovery-barrier")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveForkJournal(context.Background(), journal); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	before := recoveryTree(t, f.repository, f.planning)
	preview, err := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryFinish)
	if err == nil || preview != nil || !strings.Contains(err.Error(), foreignOperation) {
		t.Fatalf("foreign barrier refusal omitted operation identity: preview=%#v err=%v", preview, err)
	}
	if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, before) {
		t.Fatal("op1 recovery refusal changed op2 pending journal or barrier bytes")
	}
}

func TestForkRecoveryCommittedExternalRevertRefusal(t *testing.T) {
	f := recoveryFixture(t, "adopt", "recovery-committed-external-revert")
	runRecoveryCrash(t, f, ForkTransactionAfterCommitRecord)
	approved := previewRecoveryUnchanged(t, f, ForkRecoveryFinish)
	if err := os.WriteFile(f.configPath, f.oldConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.forkPath); err != nil {
		t.Fatal(err)
	}
	before := recoveryTree(t, f.repository, f.planning)
	preview, err := PreviewForkRecovery(f.repository, f.planning, f.request.Preview.OperationID, ForkRecoveryFinish)
	if err == nil || preview != nil || !errors.Is(err, ErrForkRecoveryConflict) {
		t.Fatalf("committed journal preview accepted externally reverted authority: preview=%#v err=%v", preview, err)
	}
	result, err := RecoverForkTransaction(context.Background(), ForkRecoveryRequest{Repository: f.repository, Planning: f.planning, Preview: approved, ApprovalDigest: approved.Digest})
	if err == nil || result != nil || !errors.Is(err, ErrForkRecoveryConflict) {
		t.Fatalf("committed journal apply restored or reported reverted authority: result=%#v err=%v", result, err)
	}
	if after := recoveryTree(t, f.repository, f.planning); !reflect.DeepEqual(after, before) {
		t.Fatal("committed external-revert refusal changed authoritative or recovery bytes")
	}
}
