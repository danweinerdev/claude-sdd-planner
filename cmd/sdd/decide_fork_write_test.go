package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

const (
	forkWriteOwner   = decisionview.OwnerID("10000000-0000-4000-8000-000000000001")
	forkWriteParentA = decisionview.CollectionID("20000000-0000-4000-8000-000000000002")
	forkWriteParentB = decisionview.CollectionID("30000000-0000-4000-8000-000000000003")
	forkWriteLocal   = decisionview.CollectionID("40000000-0000-4000-8000-000000000004")
)

type forkWriteFixture struct {
	root       string
	planning   string
	configPath string
	parentA    string
	parentB    string
	local      string
}

func TestForkWriteCLIRealEntryPoint(t *testing.T) {
	bin := stressBinary(t)

	t.Run("preview is a side-effect-free exact envelope", func(t *testing.T) {
		f := newForkWriteFixture(t)
		proposal := writeForkProposal(t, f.root, "adopt.json", adoptionCLIProposal("adopt", "cli-adopt-entrypoint", "binding-a", "", "Decisions/parent-a.md"))
		before := forkReadSnapshot(t, f.root)
		stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "adopt", "--file", proposal, "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		envelope := decodeCLIObject(t, stdout)
		if envelope["operation"] != "adopt" || envelope["status"] != "proposal" || envelope["requiresApproval"] != true || envelope["digest"] == "" {
			t.Errorf("preview is not an approval-bound adopt envelope: %s", stdout)
		}
		if !jsonContainsString(envelope, "planning-config.json") || !jsonContainsString(envelope, "Decisions/fork.md") {
			t.Errorf("preview omits exact config/local files: %s", stdout)
		}
		if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
			t.Fatal("preview changed config, ledger, or support files")
		}
		stdout, stderr, err = runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "adopt", "--file", proposal)
		requireCLIExit(t, err, 0, stdout, stderr)
		if !strings.Contains(stdout, "--json") || !strings.Contains(stdout, "complete exact bytes") {
			t.Errorf("summary preview does not direct exact-byte approval: %q", stdout)
		}
	})

	t.Run("unmigrated fork writers refuse with guidance", func(t *testing.T) {
		f := newForkWriteFixture(t)
		adoptForkCLI(t, bin, f)
		archive := writeForkProposal(t, f.root, "archive.json", map[string]any{"version": 1, "operation": "archive", "operationId": "cli-unmigrated-archive", "date": "2026-09-10"})
		before := forkReadSnapshot(t, f.root)
		commands := [][]string{
			{"decide", "add", "--statement", "must not use inherited legacy writer", "--accept", "--json"},
			{"decide", "accept", "D-0001", "--json"},
			{"decide", "supersede", "D-0001", "--json"},
		}
		for _, args := range commands {
			stdout, stderr, err := runSdd(bin, f.root, args...)
			requireCLIExit(t, err, 1, stdout, stderr)
			if !strings.Contains(strings.ToLower(stderr), "fork") || !strings.Contains(stderr, "decide fork") {
				t.Errorf("%v refusal lacks migration guidance: %q", args, stderr)
			}
		}
		stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "archive", "--file", archive, "--json")
		requireCLIExit(t, err, 1, stdout, stderr)
		if !strings.Contains(strings.ToLower(stderr), "not implemented") || !strings.Contains(stderr, "preserve") {
			t.Errorf("archive refusal does not explain that existing history is preserved: %q", stderr)
		}
		if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
			t.Fatal("an unmigrated writer refusal changed authority")
		}
	})

	t.Run("capabilities stay truthful", func(t *testing.T) {
		f := newForkWriteFixture(t)
		stdout, stderr, err := runSdd(bin, f.root, "decide", "capabilities", "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		value := decodeCLIJSON(t, stdout)
		forks, ok := value.(map[string]any)["decision_forks"].(map[string]any)
		if !ok || forks["schema"] != float64(1) || forks["transactions"] != float64(1) || forks["partial"] != true {
			t.Fatalf("capabilities omit partial decision-fork status: %s", stdout)
		}
		want := []any{"adopt", "rebind", "detach", "override", "reconcile", "restore"}
		if !reflect.DeepEqual(forks["write_operations"], want) {
			t.Errorf("supported write operations = %#v, want %#v", forks["write_operations"], want)
		}
		if jsonContainsString(forks["write_operations"], "archive") || jsonContainsString(forks["write_operations"], "add") {
			t.Errorf("capabilities advertise unsupported writes: %s", stdout)
		}
		if jsonContainsString(value, "release-complete") || jsonContainsString(value, "releaseComplete") {
			t.Errorf("write entry point advertises unimplemented release completeness: %s", stdout)
		}
		if !reflect.DeepEqual(forks["transaction_operations"], []any{"preview", "apply", "inspect", "recover"}) {
			t.Errorf("supported transaction operations = %#v, want preview/apply/inspect/recover", forks["transaction_operations"])
		}
		for _, command := range []string{"accept", "supersede"} {
			out, diagnostic, helpErr := runSdd(bin, f.root, "decide", command, "--help")
			requireCLIExit(t, helpErr, 0, out, diagnostic)
			if !strings.Contains(out, "Refuse until") || !strings.Contains(out, "migrated") {
				t.Errorf("%s help promises availability instead of refusal: %q", command, out)
			}
		}
	})
}

func TestForkWriteCLIPublicRoundTrip(t *testing.T) {
	bin := stressBinary(t)
	f := newForkWriteFixture(t)
	parentABefore := mustForkWriteRead(t, f.parentA)

	adoptForkCLI(t, bin, f)
	assertForkWriteRead(t, bin, f, qualified(forkWriteParentA, "D-0001"), "parent A authority")

	rebind := adoptionCLIProposal("rebind", "cli-rebind-roundtrip", "binding-b", "binding-a", "Decisions/parent-b.md")
	previewApplyForkCLI(t, bin, f, "rebind", rebind)
	assertForkWriteRead(t, bin, f, qualified(forkWriteParentB, "D-0001"), "parent B authority")

	configBeforeSingles := mustForkWriteRead(t, f.configPath)
	override := overrideCLIProposal("override", "cli-override-roundtrip", qualified(forkWriteParentB, "D-0001"), "", "approved local replacement")
	previewApplyForkCLI(t, bin, f, "override", override)
	assertForkWriteRead(t, bin, f, qualified(forkWriteLocal, "D-0001"), "approved local replacement")

	oldParent := forkReadEntry("D-0001", "superseded", "parent B authority")
	oldParent["superseded_by"] = "D-0002"
	newParent := forkReadEntry("D-0002", "accepted", "parent B authority revised upstream")
	newParent["supersedes"] = "D-0001"
	writeForkSourceEntries(t, f.planning, "Decisions/parent-b.md", forkWriteParentB, []map[string]any{oldParent, newParent})
	parentBChanged := mustForkWriteRead(t, f.parentB)
	reconcile := overrideCLIProposal("reconcile", "cli-reconcile-roundtrip", qualified(forkWriteParentB, "D-0002"), qualified(forkWriteLocal, "D-0001"), "approved reconciled replacement")
	previewApplyForkCLI(t, bin, f, "reconcile", reconcile)
	assertForkWriteRead(t, bin, f, qualified(forkWriteLocal, "D-0002"), "approved reconciled replacement")

	restore := map[string]any{"version": 1, "operation": "restore", "operationId": "cli-restore-roundtrip", "date": "2026-09-10", "decidedBy": "user-approved", "confirmation": "restore this exact retained override", "replacement": qualified(forkWriteLocal, "D-0002")}
	previewApplyForkCLI(t, bin, f, "restore", restore)
	assertForkWriteRead(t, bin, f, qualified(forkWriteParentB, "D-0002"), "parent B authority revised upstream")
	if got := mustForkWriteRead(t, f.configPath); !reflect.DeepEqual(got, configBeforeSingles) {
		t.Fatal("single-file override/reconcile/restore changed planning configuration")
	}

	detach := map[string]any{"version": 1, "operation": "detach", "operationId": "cli-detach-roundtrip", "date": "2026-09-10", "decidedBy": "user-approved", "confirmation": "detach this exact selected fork and retain local history"}
	previewApplyForkCLI(t, bin, f, "detach", detach)
	config := mustForkWriteRead(t, f.configPath)
	if !strings.Contains(string(config), `"mode":"detached"`) || !strings.Contains(string(config), string(forkWriteLocal)) {
		t.Errorf("detachment erased selected local identity: %s", config)
	}
	if !strings.Contains(string(config), `"unrelated":{"keep":true}`) {
		t.Errorf("config update did not preserve unrelated fields: %s", config)
	}
	if got := mustForkWriteRead(t, f.parentA); !reflect.DeepEqual(got, parentABefore) {
		t.Fatal("CLI edited inherited parent A bytes")
	}
	if got := mustForkWriteRead(t, f.parentB); !reflect.DeepEqual(got, parentBChanged) {
		t.Fatal("CLI edited inherited parent B bytes")
	}
}

func TestForkWriteCLIApprovalAndRecovery(t *testing.T) {
	bin := stressBinary(t)

	t.Run("approval failures and non-local proposals are no-ops", func(t *testing.T) {
		f := newForkWriteFixture(t)
		proposal := writeForkProposal(t, f.root, "adopt.json", adoptionCLIProposal("adopt", "cli-approval-adopt", "binding-a", "", "Decisions/parent-a.md"))
		stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "adopt", "--file", proposal, "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		envelope := decodeCLIObject(t, stdout)
		digest, _ := envelope["digest"].(string)
		envelopePath := filepath.Join(f.root, "approved-envelope.json")
		if err := os.WriteFile(envelopePath, []byte(stdout), 0o600); err != nil {
			t.Fatal(err)
		}
		before := forkReadSnapshot(t, f.root)
		for name, args := range map[string][]string{
			"missing": {"decide", "fork", "apply", "--file", envelopePath, "--json"},
			"stale":   {"decide", "fork", "apply", "--file", envelopePath, "--approval-digest", "sha256:0000000000000000000000000000000000000000000000000000000000000000", "--json"},
		} {
			t.Run(name, func(t *testing.T) {
				out, diagnostic, applyErr := runSdd(bin, f.root, args...)
				requireCLIExit(t, applyErr, 1, out, diagnostic)
				if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
					t.Fatalf("%s approval refusal changed files", name)
				}
			})
		}
		var tampered map[string]any
		if err := json.Unmarshal([]byte(stdout), &tampered); err != nil {
			t.Fatal(err)
		}
		tampered["date"] = "2026-09-11"
		tamperedPath := writeForkProposal(t, f.root, "tampered-envelope.json", tampered)
		tamperedBefore := forkReadSnapshot(t, f.root)
		out, diagnostic, applyErr := runSdd(bin, f.root, "decide", "fork", "apply", "--file", tamperedPath, "--approval-digest", digest, "--json")
		requireCLIExit(t, applyErr, 1, out, diagnostic)
		if !reflect.DeepEqual(tamperedBefore, forkReadSnapshot(t, f.root)) {
			t.Fatal("tampered approval refusal changed files")
		}

		nonLocal := adoptionCLIProposal("adopt", "cli-non-local", "binding-a", "", "Decisions/parent-a.md")
		nonLocal["path"] = "Decisions/parent-a.md"
		nonLocalPath := writeForkProposal(t, f.root, "non-local.json", nonLocal)
		nonLocalBefore := forkReadSnapshot(t, f.root)
		out, diagnostic, applyErr = runSdd(bin, f.root, "decide", "fork", "preview", "--operation", "adopt", "--file", nonLocalPath, "--json")
		requireCLIExit(t, applyErr, 1, out, diagnostic)
		if !reflect.DeepEqual(nonLocalBefore, forkReadSnapshot(t, f.root)) {
			t.Fatal("non-local refusal changed authority")
		}
	})

	t.Run("inspect and recovery require their own exact preview", func(t *testing.T) {
		f := newForkWriteFixture(t)
		adoptForkCLI(t, bin, f)
		before := forkReadSnapshot(t, f.root)
		stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "inspect", "--operation", "cli-adopt-roundtrip", "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		inspection := decodeCLIObject(t, stdout)
		if inspection["outcome"] != "committed" || inspection["authority"] != "complete" {
			t.Errorf("inspection obscures durable transaction outcome: %s", stdout)
		}
		stdout, stderr, err = runSdd(bin, f.root, "decide", "fork", "recover", "--operation", "cli-adopt-roundtrip", "--action", "finish", "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		recovery := decodeCLIObject(t, stdout)
		digest, _ := recovery["digest"].(string)
		if recovery["action"] != "finish" || digest == "" {
			t.Fatalf("recovery preview omitted exact action/digest: %s", stdout)
		}
		text, textDiagnostic, textErr := runSdd(bin, f.root, "decide", "fork", "recover", "--operation", "cli-adopt-roundtrip", "--action", "finish")
		requireCLIExit(t, textErr, 0, text, textDiagnostic)
		if !strings.Contains(text, "--json") || !strings.Contains(text, "complete exact bytes") {
			t.Errorf("summary recovery does not direct exact-byte approval: %q", text)
		}
		out, diagnostic, applyErr := runSdd(bin, f.root, "decide", "fork", "recover", "--operation", "cli-adopt-roundtrip", "--action", "finish", "--approval-digest", "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "--json")
		requireCLIExit(t, applyErr, 1, out, diagnostic)
		if !reflect.DeepEqual(before, forkReadSnapshot(t, f.root)) {
			t.Fatal("stale recovery approval changed committed state")
		}
		out, diagnostic, applyErr = runSdd(bin, f.root, "decide", "fork", "recover", "--operation", "cli-adopt-roundtrip", "--action", "finish", "--approval-digest", digest, "--json")
		requireCLIExit(t, applyErr, 0, out, diagnostic)
		result := decodeCLIObject(t, out)
		if result["outcome"] != "committed" {
			t.Errorf("lost-reply recovery is not explicitly committed: %s", out)
		}
	})
}

func TestForkWriteCLIOutcomeErrorsPreserveOperationAndEncodingFailures(t *testing.T) {
	operationErr := errors.New("original apply failure")
	encodingErr := errors.New("stdout encoding failure")
	out := &forkApplyOutput{Version: 1, OperationID: "operation-with-unknown-outcome", Outcome: "outcome-unknown"}
	err := joinForkOutcomeErrors("decide fork apply", out, operationErr, encodingErr)
	if !errors.Is(err, operationErr) || !errors.Is(err, encodingErr) {
		t.Fatalf("joined error does not preserve both causes: %v", err)
	}
	if message := err.Error(); !strings.Contains(message, out.OperationID) || !strings.Contains(message, out.Outcome) {
		t.Errorf("joined error omits operation identity/outcome: %v", err)
	}
	out.Outcome = string(decisionview.ForkTransactionRolledBack)
	err = joinForkOutcomeErrors("decide fork apply", out, decisionview.ErrForkTransactionConflict, nil)
	if exitCode(err) != 1 || !errors.Is(err, decisionview.ErrForkTransactionConflict) {
		t.Fatalf("clean precondition rollback must retain its cause and exit 1: %v", err)
	}
	err = joinForkOutcomeErrors("decide fork apply", out, decisionview.ErrForkTransactionConflict, encodingErr)
	if exitCode(err) != 2 || !errors.Is(err, encodingErr) {
		t.Fatalf("outcome encoding failure must remain operational: %v", err)
	}
}

func newForkWriteFixture(t *testing.T) forkWriteFixture {
	t.Helper()
	root := t.TempDir()
	planning := filepath.Join(root, ".plans")
	config := []byte(`{"planningRoot":".plans","repositoryId":"10000000-0000-4000-8000-000000000001","unrelated":{"keep":true}}`)
	forkReadWrite(t, root, "planning-config.json", config)
	writeForkSource(t, planning, "Decisions/parent-a.md", forkWriteParentA, "parent A authority")
	writeForkSource(t, planning, "Decisions/parent-b.md", forkWriteParentB, "parent B authority")
	return forkWriteFixture{root: root, planning: planning, configPath: filepath.Join(root, "planning-config.json"), parentA: filepath.Join(planning, "Decisions", "parent-a.md"), parentB: filepath.Join(planning, "Decisions", "parent-b.md"), local: filepath.Join(planning, "Decisions", "fork.md")}
}

func writeForkSource(t *testing.T, planning, relative string, id decisionview.CollectionID, statement string) {
	t.Helper()
	writeForkSourceEntries(t, planning, relative, id, []map[string]any{forkReadEntry("D-0001", "accepted", statement)})
}

func writeForkSourceEntries(t *testing.T, planning, relative string, id decisionview.CollectionID, entries []map[string]any) {
	t.Helper()
	metadata := decisionview.ForkMetadata{Version: decisionview.Version1, LedgerID: id, RepositoryID: forkWriteOwner, Events: []decisionview.AuthorityEvent{{Version: decisionview.Version1, ID: "source-detached-" + string(id)[0:8], Kind: decisionview.EventDetach, Date: "2026-09-09", DecidedBy: "user-approved"}}}
	forkReadWriteLedger(t, planning, relative, entries, metadata)
}

func adoptionCLIProposal(operation, operationID, bindingID, parentBindingID, sourcePath string) map[string]any {
	return map[string]any{"version": 1, "operation": operation, "operationId": operationID, "date": "2026-09-10", "repositoryId": forkWriteOwner, "ledgerId": forkWriteLocal, "path": "Decisions/fork.md", "bindingId": bindingID, "parentBindingId": parentBindingID, "sourceOwnerId": forkWriteOwner, "source": map[string]any{"root": "planning", "path": sourcePath}}
}

func overrideCLIProposal(operation, operationID, target, replacement, statement string) map[string]any {
	return map[string]any{"version": 1, "operation": operation, "operationId": operationID, "date": "2026-09-10", "target": target, "replacement": replacement, "kind": "decision", "decidedBy": "user-approved", "statement": statement, "rationale": "approved exact CLI round trip", "scope": []string{"cmd/sdd"}, "confirmation": "publish these exact complete ledger bytes", "rejected": []string{}, "tags": []string{"fork"}, "reversibility": "two-way"}
}

func adoptForkCLI(t *testing.T, bin string, f forkWriteFixture) {
	t.Helper()
	previewApplyForkCLI(t, bin, f, "adopt", adoptionCLIProposal("adopt", "cli-adopt-roundtrip", "binding-a", "", "Decisions/parent-a.md"))
}

func previewApplyForkCLI(t *testing.T, bin string, f forkWriteFixture, operation string, proposal map[string]any) map[string]any {
	t.Helper()
	proposalPath := writeForkProposal(t, f.root, operation+"-proposal.json", proposal)
	stdout, stderr, err := runSdd(bin, f.root, "decide", "fork", "preview", "--operation", operation, "--file", proposalPath, "--json")
	requireCLIExit(t, err, 0, stdout, stderr)
	envelope := decodeCLIObject(t, stdout)
	digest, ok := envelope["digest"].(string)
	if !ok || digest == "" {
		t.Fatalf("%s preview omitted digest: %s", operation, stdout)
	}
	envelopePath := filepath.Join(f.root, operation+"-envelope.json")
	if err := os.WriteFile(envelopePath, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runSdd(bin, f.root, "decide", "fork", "apply", "--file", envelopePath, "--approval-digest", digest, "--json")
	requireCLIExit(t, err, 0, stdout, stderr)
	result := decodeCLIObject(t, stdout)
	if result["outcome"] != "committed" && result["outcome"] != "replayed" {
		t.Errorf("%s apply did not report an unambiguous durable outcome: %s", operation, stdout)
	}
	return envelope
}

func assertForkWriteRead(t *testing.T, bin string, f forkWriteFixture, id, statement string) {
	t.Helper()
	stdout, stderr, err := runSdd(bin, f.root, "decide", "effective", "--json")
	requireCLIExit(t, err, 0, stdout, stderr)
	value := decodeCLIJSON(t, stdout)
	if !jsonContainsString(value, id) || !jsonContainsString(value, statement) {
		t.Errorf("fresh effective read omits %q / %q: %s", id, statement, stdout)
	}
}

func writeForkProposal(t *testing.T, root, name string, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeCLIObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	value := decodeCLIJSON(t, raw)
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("CLI JSON is not an object: %s", raw)
	}
	return object
}

func mustForkWriteRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
