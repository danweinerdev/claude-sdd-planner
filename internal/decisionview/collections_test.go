package decisionview_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"gopkg.in/yaml.v3"
)

func collectionEntry(id, status, statement string) map[string]any {
	return map[string]any{"id": id, "kind": "decision", "status": status, "date": "2026-09-08", "decided_by": "user", "statement": statement, "rationale": "explicit test requirement", "scope": []string{}, "tags": []string{}, "rejected": []string{}}
}
func collectionLedger(t *testing.T, root, path, status string, entries []map[string]any, metadata any) []byte {
	t.Helper()
	m := map[string]any{"title": "Fixture ledger", "type": "decision-log", "status": status, "created": "2026-09-08", "updated": "2026-09-08", "tags": []string{}, "related": []string{}, "decisions": entries}
	if metadata != nil {
		m["fork"] = metadata
	}
	b, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	b = append(append([]byte("---\n"), b...), []byte("---\n\n# Fixture ledger\n")...)
	path = filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestForkCollectionsHostileSources(t *testing.T) {
	root := t.TempDir()
	statement := "yes no true false null\n\"quoted\" 雪"
	original := collectionLedger(t, root, "Decisions/decisions.md", "active", []map[string]any{collectionEntry("D-0001", "accepted", statement)}, nil)
	c, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: root}, selectionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Entries["D-0001"]["statement"] != statement {
		t.Fatalf("hostile statement lost: %#v", c.Entries)
	}
	if len(c.Files) != 1 || !bytes.Equal(c.Files[0].Source, original) {
		t.Fatal("loader rewrote source")
	}
	raw, err := yaml.Marshal(c.Entries["D-0001"])
	if err != nil {
		t.Fatal(err)
	}
	var reparsed map[string]any
	if err := yaml.Unmarshal(raw, &reparsed); err != nil {
		t.Fatal(err)
	}
	if reparsed["statement"] != statement {
		t.Fatal("hostile record did not reparse")
	}
	for _, source := range []string{"../outside.md", "/outside.md", "C:/outside.md", "Decisions\\decisions.md"} {
		if _, err := decisionview.LoadCollection(decisionview.Roots{Planning: root}, selectionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: source}); err == nil {
			t.Errorf("unsafe source accepted: %q", source)
		}
	}
}

func collectionSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, item os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() || item.Type()&os.ModeSymlink != 0 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestForkCollectionsContainedReadOnly(t *testing.T) {
	base := t.TempDir()
	planning := filepath.Join(base, "planning")
	repository := filepath.Join(base, "repository")
	collectionLedger(t, planning, "Decisions/decisions.md", "active", []map[string]any{collectionEntry("D-0001", "accepted", "planning rule")}, nil)
	collectionLedger(t, repository, "DECISIONS.md", "active", []map[string]any{collectionEntry("D-0001", "accepted", "repository rule")}, nil)
	before := collectionSnapshot(t, base)
	for _, loc := range []decisionview.SourceLocator{{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md"}, {Root: decisionview.SourceRootRepository, Path: "DECISIONS.md"}} {
		if _, err := decisionview.LoadCollection(decisionview.Roots{Repository: repository, Planning: planning}, selectionLedger, loc); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, collectionSnapshot(t, base)) {
		t.Fatal("reads changed tracked/untracked source bytes or created support files")
	}
	t.Run("link-escape", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(repository, "DECISIONS.md"), filepath.Join(planning, "escape.md")); err != nil {
			t.Skipf("native symlink creation unavailable: %v", err)
		}
		if _, err := decisionview.LoadCollection(decisionview.Roots{Planning: planning}, selectionLedger, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "escape.md"}); err == nil {
			t.Fatal("symlink escaped declared root")
		}
	})
}

func TestForkCollectionsOwnArchivesAndIDs(t *testing.T) {
	root := t.TempDir()
	collectionLedger(t, root, "Decisions/decisions.md", "active", []map[string]any{collectionEntry("D-0001", "accepted", "parent rule")}, nil)
	collectionLedger(t, root, "Decisions/archive-2026.md", "archived", []map[string]any{collectionEntry("D-0002", "rejected", "historical rule")}, nil)
	metadata := map[string]any{"version": 1, "ledgerId": selectionLedger, "repositoryId": selectionOwner, "archives": []string{"Decisions/fork-archive-*.md"}}
	collectionLedger(t, root, "Decisions/fork.md", "active", []map[string]any{collectionEntry("D-0001", "accepted", "local rule")}, metadata)
	collectionLedger(t, root, "Decisions/fork-archive-2026.md", "archived", []map[string]any{collectionEntry("D-0002", "rejected", "local history")}, nil)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Planning: root}, "bbbbbbbb-cccc-dddd-eeee-ffffffffffff", decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md", Archives: []string{"Decisions/archive-*.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.Entries) != 2 || parent.Entries["D-0001"]["statement"] != "parent rule" {
		t.Fatalf("parent membership: %+v", parent.Entries)
	}
	writeSelectionConfig(t, root, selectionConfig(".", "Decisions/fork.md"))
	selected, err := decisionview.ReadSelection(root)
	if err != nil {
		t.Fatal(err)
	}
	local, err := decisionview.LoadSelectedCollection(selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(local.Entries) != 2 || local.Entries["D-0001"]["statement"] != "local rule" || local.ID == parent.ID {
		t.Fatal("parent/local namespaces or archives were mixed")
	}
	collectionLedger(t, root, "Decisions/fork-archive-2026.md", "archived", []map[string]any{collectionEntry("D-0001", "rejected", "duplicate local ID")}, nil)
	if _, err := decisionview.LoadSelectedCollection(selected); err == nil {
		t.Fatal("duplicate IDs within one collection accepted")
	}
}
