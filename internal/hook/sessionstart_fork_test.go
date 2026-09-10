package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"gopkg.in/yaml.v3"
)

const (
	hookForkOwner  = decisionview.OwnerID("11111111-2222-3333-4444-555555555555")
	hookForkParent = decisionview.CollectionID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	hookForkLocal  = decisionview.CollectionID("cccccccc-dddd-eeee-ffff-000000000000")
)

func TestForkSessionStartWarningsAndBudget(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) string
	}{
		{"pending transaction", func(t *testing.T) string {
			root := hookForkFixture(t, false)
			hookWriteConfig(t, root, map[string]any{"version": 1, "mode": "fork", "path": "Decisions/fork.md", "ledgerId": hookForkLocal, "transaction": map[string]any{"id": "pending-hook", "journal": "Decisions/pending.json"}})
			return root
		}},
		{"stale authority", func(t *testing.T) string { return hookForkFixture(t, true) }},
		{"malformed declaration", func(t *testing.T) string {
			root := t.TempDir()
			hookWrite(t, filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":".","repositoryId":"11111111-2222-3333-4444-555555555555","decisionLog":{"version":1}}`))
			return root
		}},
		{"operational missing source", func(t *testing.T) string {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".plans", "Decisions"), 0o755); err != nil {
				t.Fatal(err)
			}
			hookWriteConfig(t, root, map[string]any{"version": 1, "mode": "fork", "path": "Decisions/missing.md", "ledgerId": hookForkLocal})
			return root
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.setup(t)
			before := hookTreeSnapshot(t, root)
			context := sessionStartContext(root, 96)
			lower := strings.ToLower(context)
			if !strings.Contains(lower, "warning") {
				t.Errorf("configured unresolved state lost its nonfatal warning: %q", context)
			}
			if !strings.Contains(lower, "full") || !strings.Contains(context, "sdd decide effective") {
				t.Errorf("tiny budget lost full-read guidance: %q", context)
			}
			if strings.Contains(context, "parent authority rule") || strings.Contains(context, "local authority rule") {
				t.Errorf("unresolved hook presented partial authority as instructions: %q", context)
			}
			if got := hookTreeSnapshot(t, root); strings.Join(before, "\n") != strings.Join(got, "\n") {
				t.Fatalf("hook read created support files or changed persisted bytes\nbefore=%q\nafter=%q", before, got)
			}
		})
	}

	for _, root := range []string{t.TempDir(), func() string {
		r := t.TempDir()
		hookWrite(t, filepath.Join(r, "planning-config.json"), []byte(`{"planningRoot":"."}`))
		return r
	}()} {
		if context := sessionStartContext(root, 96); context != "" {
			t.Errorf("legacy missing config/ledger must remain a silent no-op, got %q", context)
		}
	}

	t.Run("candidate diagnostic without binding entries", func(t *testing.T) {
		capture := &decisionview.ConsumerCapture{
			Declared: true,
			View: &decisionview.ResolvedView{
				Resolution: decisionview.ResolutionComplete,
				Records:    []decisionview.ResolvedDecision{},
			},
			Diagnostics: []decisionview.Diagnostic{{
				Code: "DLG060", Severity: decisionview.Candidate, Message: "Candidate decision requires judgment.",
			}},
		}
		context := forkSessionStartContext(capture, 4096)
		if !strings.Contains(context, "DLG060") || !strings.Contains(context, "Candidate decision requires judgment.") {
			t.Fatalf("candidate diagnostic was dropped when no binding entries existed: %q", context)
		}
	})

	t.Run("legacy truncation uses legacy read commands", func(t *testing.T) {
		root := t.TempDir()
		hookWrite(t, filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":".plans"}`))
		entries := make([]map[string]any, 0, maxLedgerEntries+1)
		for i := 0; i <= maxLedgerEntries; i++ {
			entries = append(entries, hookEntry("D-"+strconv.Itoa(1000+i), "legacy authority rule"))
		}
		hookWriteLedger(t, filepath.Join(root, ".plans", "Decisions", "decisions.md"), entries, nil)
		context := sessionStartContext(root, 16*1024)
		if !strings.Contains(context, "sdd decide list") || !strings.Contains(context, "sdd decide search") {
			t.Fatalf("legacy truncation omitted working legacy read commands: %q", context)
		}
		if strings.Contains(context, "sdd decide effective") {
			t.Fatalf("legacy truncation recommended fork-only effective read: %q", context)
		}
	})
}

func hookForkFixture(t *testing.T, stale bool) string {
	t.Helper()
	root := t.TempDir()
	planning := filepath.Join(root, ".plans")
	parentPath := filepath.Join(planning, "Decisions", "parent.md")
	hookWriteLedger(t, parentPath, []map[string]any{hookEntry("D-0001", "parent authority rule")}, nil)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: planning}, hookForkParent, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := decisionview.BindCollection("hook-parent", hookForkOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		hookWriteLedger(t, parentPath, []map[string]any{hookEntry("D-0001", "changed without approval")}, nil)
	}
	metadata := decisionview.ForkMetadata{Version: 1, LedgerID: hookForkLocal, RepositoryID: hookForkOwner, ParentBindingID: binding.ID, Bindings: []decisionview.Binding{binding}}
	hookWriteLedger(t, filepath.Join(planning, "Decisions", "fork.md"), []map[string]any{hookEntry("D-0100", "local authority rule")}, metadata)
	hookWriteConfig(t, root, map[string]any{"version": 1, "mode": "fork", "path": "Decisions/fork.md", "ledgerId": hookForkLocal})
	return root
}

func hookEntry(id, statement string) map[string]any {
	return map[string]any{"id": id, "kind": "decision", "status": "accepted", "date": "2026-09-10", "decided_by": "user", "statement": statement, "rationale": "fixture rationale", "confirmation": "fixture confirmation", "scope": []string{"internal/hook/"}, "tags": []string{"fixture"}, "rejected": []string{}, "reversibility": "two-way"}
}

func hookWriteLedger(t *testing.T, path string, decisions []map[string]any, metadata any) {
	t.Helper()
	frontmatter := map[string]any{"title": "Hook fixture", "type": "decision-log", "status": "active", "created": "2026-09-10", "updated": "2026-09-10", "tags": []string{}, "related": []string{}, "decisions": decisions}
	if metadata != nil {
		frontmatter["fork"] = metadata
	}
	raw, err := yaml.Marshal(frontmatter)
	if err != nil {
		t.Fatal(err)
	}
	hookWrite(t, path, append(append([]byte("---\n"), raw...), []byte("---\n\n# Hook fixture\n")...))
}

func hookWriteConfig(t *testing.T, root string, decisionLog map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"planningRoot": ".plans", "repositoryId": hookForkOwner, "decisionLog": decisionLog})
	if err != nil {
		t.Fatal(err)
	}
	hookWrite(t, filepath.Join(root, "planning-config.json"), raw)
}

func hookWrite(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hookTreeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var snapshot []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot = append(snapshot, filepath.ToSlash(rel)+"\x00"+string(raw))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
