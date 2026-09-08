package decisionview_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

const selectionOwner = "11111111-2222-3333-4444-555555555555"
const selectionLedger = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func selectionConfig(planning, ledger string) []byte {
	v := map[string]any{
		"planningRoot": planning, "repositoryId": selectionOwner,
		"decisionLog": map[string]any{"version": 1, "mode": "fork", "path": ledger, "ledgerId": selectionLedger},
		"unrelated":   map[string]any{"text": "yes true null \"quote\"\n雪", "number": 1.5},
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func writeSelectionConfig(t *testing.T, root string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "planning-config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestForkSelectionHostileConfig(t *testing.T) {
	root := t.TempDir()
	planning := filepath.Join(root, ".plans")
	if err := os.MkdirAll(planning, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := selectionConfig(".plans", "Decisions/fork 雪.md")
	writeSelectionConfig(t, root, raw)
	selected, err := decisionview.ReadSelection(root)
	if err != nil {
		t.Fatal(err)
	}
	if !selected.Explicit || selected.Config == nil {
		t.Fatal("explicit fork declaration was silently ignored")
	}
	if !bytes.Equal(selected.RawConfig, raw) {
		t.Fatal("unknown config keys or hostile text changed")
	}
	var reparsed map[string]any
	if err := json.Unmarshal(selected.RawConfig, &reparsed); err != nil {
		t.Fatal(err)
	}
	if reparsed["unrelated"].(map[string]any)["text"] != "yes true null \"quote\"\n雪" {
		t.Fatal("hostile data did not reparse losslessly")
	}
	if selected.Config.Path != "Decisions/fork 雪.md" {
		t.Fatalf("selected path: %q", selected.Config.Path)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"decisionLog":`, `"decisionLog":null,"decisionLog":`, 1),
		strings.Replace(string(raw), `"repositoryId":`, `"repositoryId":"00000000-0000-0000-0000-000000000000","repositoryId":`, 1),
		strings.Replace(string(raw), `"decisionLog":`, `"DecisionLog":`, 1),
		strings.Replace(string(raw), `"ledgerId":`, `"ledgerId":null,"ledgerId":`, 1),
		strings.Replace(string(raw), `"repositoryId":`, `"RepositoryId":"00000000-0000-0000-0000-000000000000","repositoryId":`, 1),
	} {
		writeSelectionConfig(t, root, []byte(bad))
		if _, err := decisionview.ReadSelection(root); err == nil {
			t.Errorf("accepted ambiguous/duplicate fork config: %s", bad)
		}
	}
}

func TestForkSelectionInternalExternalOwners(t *testing.T) {
	for _, external := range []bool{false, true} {
		name := "internal"
		if external {
			name = "external"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "source")
			planning := filepath.Join(root, ".plans")
			relative := ".plans"
			if external {
				planning = filepath.Join(base, "planning")
				relative = "../planning"
			}
			if err := os.MkdirAll(planning, 0o755); err != nil {
				t.Fatal(err)
			}
			raw := selectionConfig(relative, "Decisions/owner/fork.md")
			writeSelectionConfig(t, root, raw)
			before, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := decisionview.ReadSelection(root)
			if err != nil {
				t.Fatal(err)
			}
			if !selected.Explicit {
				t.Fatal("fork not selected")
			}
			// Windows TempDir may use an 8.3 spelling; compare the physical
			// root, not that incidental alias.
			physicalPlanning, err := filepath.EvalSymlinks(planning)
			if err != nil {
				t.Fatal(err)
			}
			if selected.PlanningRoot != physicalPlanning || selected.LedgerPath != filepath.Join(physicalPlanning, "Decisions", "owner", "fork.md") {
				t.Fatalf("wrong roots: %+v", selected)
			}
			meta := decisionview.ForkMetadata{Version: decisionview.Version1, LedgerID: selectionLedger, RepositoryID: selectionOwner}
			if err := selected.ValidateOwner(&meta); err != nil {
				t.Fatalf("matching owner rejected: %v", err)
			}
			meta.RepositoryID = "22222222-3333-4444-5555-666666666666"
			if err := selected.ValidateOwner(&meta); err == nil {
				t.Fatal("unrelated owner captured local authority")
			}
			meta.RepositoryID = selectionOwner
			meta.LedgerID = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
			if err := selected.ValidateOwner(&meta); err == nil {
				t.Fatal("wrong collection captured local authority")
			}
			if err := selected.ValidateOwner(nil); err == nil {
				t.Fatal("missing fork metadata accepted")
			}
			after, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(before) != len(after) {
				t.Fatal("selection added repo-root files")
			}
			current, err := os.ReadFile(filepath.Join(root, "planning-config.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(current, raw) {
				t.Fatal("selection rewrote config")
			}
		})
	}
}

func TestForkSelectionLegacyAndMalformed(t *testing.T) {
	root := t.TempDir()
	selected, err := decisionview.ReadSelection(root)
	if err != nil || selected.Explicit {
		t.Fatalf("missing config changed legacy behavior: %v %+v", err, selected)
	}
	legacy := []byte(`{"planningRoot":".plans","unrelated":1.5}`)
	writeSelectionConfig(t, root, legacy)
	if err := os.MkdirAll(filepath.Join(root, ".plans", "Decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".plans", "Decisions", "fork.md"), []byte("filename is not opt-in"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, err = decisionview.ReadSelection(root)
	if err != nil || selected.Explicit || selected.LedgerPath != "" {
		t.Fatalf("filename inferred fork mode: %v %+v", err, selected)
	}
	if err := selected.ValidateOwner(&decisionview.ForkMetadata{}); err == nil {
		t.Fatal("legacy descriptor was treated as fork authority")
	}
	for _, bad := range []string{
		`{"planningRoot":".plans","decisionLog":null}`,
		`{"planningRoot":".plans","decisionLog":[]}`,
		`{"planningRoot":".plans","decisionLog":{"version":99}}`,
		`{"planningRoot":".plans","decisionLog":`,
		strings.Replace(string(selectionConfig(".plans", "Decisions/fork.md")), selectionOwner, "not-an-owner", 1),
		string(selectionConfig(".plans", "../fork.md")),
		string(selectionConfig(".plans", "C:/fork.md")),
		string(selectionConfig(".plans", "/fork.md")),
		string(selectionConfig(".plans", "Decisions\\fork.md")),
	} {
		writeSelectionConfig(t, root, []byte(bad))
		if _, err := decisionview.ReadSelection(root); err == nil {
			t.Errorf("malformed fork fell back: %s", bad)
		}
	}
}

func TestForkSelectionPendingAndConfigPreservation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := selectionConfig(".plans", "Decisions/fork.md")
	writeSelectionConfig(t, root, raw)
	cfg, err := store.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cfg.RepositoryID, fields["repositoryId"]) || !bytes.Equal(cfg.DecisionLog, fields["decisionLog"]) {
		t.Fatal("store config did not retain raw selector fields")
	}
	fields["decisionLog"] = json.RawMessage(`{"version":1,"mode":"fork","path":"Decisions/fork.md","ledgerId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","transaction":{"id":"operation-1","journal":"Decisions/.fork-state/pending.json"}}`)
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	writeSelectionConfig(t, root, raw)
	selected, err := decisionview.ReadSelection(root)
	if !errors.Is(err, decisionview.ErrSelectionPending) || selected == nil || !selected.Explicit {
		t.Fatalf("pending selection lost recovery state: %+v %v", selected, err)
	}
	if err := selected.ValidateOwner(&decisionview.ForkMetadata{Version: 1, LedgerID: selectionLedger, RepositoryID: selectionOwner}); !errors.Is(err, decisionview.ErrSelectionPending) {
		t.Fatalf("pending authority was usable: %v", err)
	}
}
