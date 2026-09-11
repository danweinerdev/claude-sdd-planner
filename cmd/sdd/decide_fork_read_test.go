package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"gopkg.in/yaml.v3"
)

const (
	forkReadOwner  = decisionview.OwnerID("11111111-2222-3333-4444-555555555555")
	forkReadParent = decisionview.CollectionID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	forkReadLocal  = decisionview.CollectionID("cccccccc-dddd-eeee-ffff-000000000000")
)

func TestForkReadCLIRealEntryPoint(t *testing.T) {
	bin := stressBinary(t)
	root := forkReadFixture(t, false, false, "ordinary local rule")
	before := forkReadSnapshot(t, root)

	commands := []struct {
		name    string
		args    []string
		present []string
		absent  []string
	}{
		{"effective", []string{"decide", "effective", "--json"}, []string{qualified(forkReadParent, "D-0001"), qualified(forkReadLocal, "D-0100")}, []string{qualified(forkReadLocal, "D-0101")}},
		{"history", []string{"decide", "history", "--json"}, []string{qualified(forkReadParent, "D-0001"), qualified(forkReadLocal, "D-0099"), qualified(forkReadLocal, "D-0100"), qualified(forkReadLocal, "D-0101"), "proposed"}, nil},
		{"lookup", []string{"decide", "lookup", qualified(forkReadParent, "D-0001"), "--json"}, []string{qualified(forkReadParent, "D-0001"), "parent authority rule"}, nil},
		{"archive-lookup", []string{"decide", "lookup", qualified(forkReadLocal, "D-0099"), "--json"}, []string{qualified(forkReadLocal, "D-0099"), "archived local history", "historical"}, nil},
		{"list", []string{"decide", "list", "--json"}, []string{qualified(forkReadParent, "D-0001"), qualified(forkReadLocal, "D-0100")}, []string{qualified(forkReadLocal, "D-0101")}},
		{"search", []string{"decide", "search", "ordinary local", "--json"}, []string{qualified(forkReadLocal, "D-0100"), "ordinary local rule"}, []string{qualified(forkReadParent, "D-0001")}},
	}
	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runSdd(bin, root, tc.args...)
			requireCLIExit(t, err, 0, stdout, stderr)
			value := decodeCLIJSON(t, stdout)
			requireJSONVersion(t, value)
			for _, want := range tc.present {
				if !jsonContainsString(value, want) {
					t.Errorf("%s JSON does not contain %q: %s", tc.name, want, stdout)
				}
			}
			for _, unwanted := range tc.absent {
				if jsonContainsString(value, unwanted) {
					t.Errorf("%s JSON unexpectedly contains filtered/non-effective %q: %s", tc.name, unwanted, stdout)
				}
			}
		})
	}

	stdout, stderr, err := runSdd(bin, root, "decide", "capabilities", "--json")
	requireCLIExit(t, err, 0, stdout, stderr)
	capabilities := decodeCLIJSON(t, stdout)
	forks, ok := capabilities.(map[string]any)["decision_forks"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities omit partial decision_forks support: %s", stdout)
	}
	if forks["schema"] != float64(1) || forks["transactions"] != float64(1) || !jsonContainsString(forks["canonicalization"], "entry-v1") || forks["partial"] != true {
		t.Errorf("capabilities do not advertise the implemented read schema/canonicalization: %s", stdout)
	}
	transactionOperations := forks["transaction_operations"]
	if !jsonContainsString(transactionOperations, "preview") || !jsonContainsString(transactionOperations, "apply") || !jsonContainsString(transactionOperations, "inspect") || !jsonContainsString(transactionOperations, "recover") {
		t.Errorf("capabilities omit implemented versioned transaction operations: %s", stdout)
	}
	stdout, stderr, err = runSdd(bin, root, "decide", "lookup", qualified(forkReadParent, "D-0001"))
	requireCLIExit(t, err, 0, stdout, stderr)
	if !strings.Contains(stdout, qualified(forkReadParent, "D-0001")) || !strings.Contains(stdout, "current authority") {
		t.Errorf("text lookup omits identity/authority context: %q", stdout)
	}
	if !reflect.DeepEqual(before, forkReadSnapshot(t, root)) {
		t.Fatal("fork read commands changed ledger/config bytes or created support files")
	}
}

func TestForkReadCLIHostileJSON(t *testing.T) {
	const hostile = "yes no true false null\n\"quoted\" \\ slash 雪"
	root := forkReadFixture(t, false, false, hostile)
	before := forkReadSnapshot(t, root)
	stdout, stderr, err := runSdd(stressBinary(t), root, "decide", "effective", "--json")
	requireCLIExit(t, err, 0, stdout, stderr)
	value := decodeCLIJSON(t, stdout)
	if !jsonContainsString(value, hostile) {
		t.Fatalf("hostile statement did not survive real CLI JSON exactly: %s", stdout)
	}
	if !reflect.DeepEqual(before, forkReadSnapshot(t, root)) {
		t.Fatal("hostile JSON read changed source bytes or created support files")
	}
}

func TestForkReadCLILegacyAndFilteredFailures(t *testing.T) {
	bin := stressBinary(t)
	t.Run("legacy", func(t *testing.T) {
		root := t.TempDir()
		forkReadWrite(t, root, "planning-config.json", []byte(`{"planningRoot":".plans"}`))
		forkReadWriteLedger(t, filepath.Join(root, ".plans"), "Decisions/decisions.md", []map[string]any{
			forkReadEntry("D-0001", "accepted", "legacy stable output"),
		}, nil)
		before := forkReadSnapshot(t, root)
		stdout, stderr, err := runSdd(bin, root, "decide", "list", "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		var got struct {
			Decisions []decisionEntry `json:"decisions"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("legacy list JSON: %v: %s", err, stdout)
		}
		if len(got.Decisions) != 1 || got.Decisions[0].ID != "D-0001" || got.Decisions[0].Statement != "legacy stable output" {
			t.Fatalf("legacy list contract changed: %+v", got)
		}
		stdout, stderr, err = runSdd(bin, root, "decide", "search", "stable", "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		if !jsonContainsString(decodeCLIJSON(t, stdout), "legacy stable output") {
			t.Fatalf("legacy search contract changed: %s", stdout)
		}
		after := forkReadSnapshot(t, root)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("legacy reads created fork state or changed files\nbefore: %v\nafter:  %v", before, after)
		}
	})

	t.Run("filters-retain-ordered-errors", func(t *testing.T) {
		root := forkReadFixture(t, true, false, "ordinary local rule")
		before := forkReadSnapshot(t, root)
		for _, args := range [][]string{
			{"decide", "list", "--status", "rejected", "--json"},
			{"decide", "search", "definitely-absent", "--json"},
		} {
			stdout, stderr, err := runSdd(bin, root, args...)
			requireCLIExit(t, err, 1, stdout, stderr)
			value := decodeCLIJSON(t, stdout)
			messages := diagnosticMessages(t, value)
			want := []string{
				"Accepted decision D-0001 changed immutable field rationale",
				"Accepted decision D-0001 changed immutable field statement",
			}
			if !orderedStrings(messages, want) {
				t.Errorf("filtered read lost or reordered authority failures; got %q, want ordered %q", messages, want)
			}
		}
		if !reflect.DeepEqual(before, forkReadSnapshot(t, root)) {
			t.Fatal("failed filtered reads changed files")
		}
	})

	t.Run("malformed-is-could-not-run", func(t *testing.T) {
		root := t.TempDir()
		forkReadWrite(t, root, "planning-config.json", []byte(`{"planningRoot":".","decisionLog":{"version":1}}`))
		stdout, stderr, err := runSdd(bin, root, "decide", "effective", "--json")
		requireCLIExit(t, err, 2, stdout, stderr)
	})

	t.Run("failed-authority-lookup-keeps-diagnostics", func(t *testing.T) {
		root := t.TempDir()
		forkReadWrite(t, root, "planning-config.json", []byte(`{"planningRoot":".","repositoryId":"11111111-2222-3333-4444-555555555555"}`))
		stdout, stderr, err := runSdd(bin, root, "decide", "lookup", qualified(forkReadLocal, "D-0100"), "--json")
		requireCLIExit(t, err, 1, stdout, stderr)
		value := decodeCLIJSON(t, stdout)
		if !jsonContainsString(value, "FDL020") || !jsonContainsSubstring(value, "selection was removed") {
			t.Fatalf("failed lookup lost removed-authority diagnostics: %s", stdout)
		}
	})

	t.Run("legacy-explicit-reads-are-actionable", func(t *testing.T) {
		root := t.TempDir()
		forkReadWrite(t, root, "planning-config.json", []byte(`{"planningRoot":"."}`))
		for _, verb := range []string{"effective", "history", "lookup"} {
			args := []string{"decide", verb}
			if verb == "lookup" {
				args = append(args, qualified(forkReadLocal, "D-0100"))
			}
			stdout, stderr, err := runSdd(bin, root, args...)
			requireCLIExit(t, err, 2, stdout, stderr)
			if strings.Contains(stderr, "legacy decision read") || !strings.Contains(stderr, "use `sdd decide list` or `sdd decide search`") {
				t.Errorf("legacy %s guidance is opaque: %q", verb, stderr)
			}
		}
	})

	t.Run("non-effective-list-status-refuses", func(t *testing.T) {
		root := forkReadFixture(t, false, false, "ordinary local rule")
		stdout, stderr, err := runSdd(bin, root, "decide", "list", "--status", "proposed", "--json")
		requireCLIExit(t, err, 1, stdout, stderr)
		decodeCLIJSON(t, stdout)
		if !strings.Contains(stderr, "decide history") {
			t.Fatalf("non-effective list filter lacks history guidance: %q", stderr)
		}
	})

	t.Run("candidates-are-guidance", func(t *testing.T) {
		root := forkReadFixture(t, false, true, "local storage answer")
		stdout, stderr, err := runSdd(bin, root, "decide", "effective", "--json")
		requireCLIExit(t, err, 0, stdout, stderr)
		value := decodeCLIJSON(t, stdout)
		if !jsonContainsString(value, "DLG060") || !jsonContainsSubstring(value, "Judge whether") {
			t.Fatalf("candidate-only success omitted judgment guidance: %s", stdout)
		}
	})
}

func forkReadFixture(t *testing.T, stale, candidate bool, localStatement string) string {
	t.Helper()
	root := t.TempDir()
	planning := filepath.Join(root, ".plans")
	parentEntry := forkReadEntry("D-0001", "accepted", "parent authority rule")
	localEntry := forkReadEntry("D-0100", "accepted", localStatement)
	if candidate {
		parentEntry["question"] = "Which storage is authoritative?"
		localEntry["question"] = "Which storage is authoritative?"
	}
	forkReadWriteLedger(t, planning, "Decisions/parent.md", []map[string]any{parentEntry}, nil)
	parent, err := decisionview.LoadCollection(
		decisionview.Roots{Repository: root, Planning: planning},
		forkReadParent,
		decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md"},
	)
	if err != nil {
		t.Fatalf("load parent fixture: %v", err)
	}
	binding, err := decisionview.BindCollection("parent-binding", forkReadOwner, parent)
	if err != nil {
		t.Fatalf("bind parent fixture: %v", err)
	}
	if stale {
		parentEntry["statement"] = "unapproved changed parent rule"
		parentEntry["rationale"] = "unapproved changed rationale"
		forkReadWriteLedger(t, planning, "Decisions/parent.md", []map[string]any{parentEntry}, nil)
	}
	metadata := decisionview.ForkMetadata{
		Version: decisionview.Version1, LedgerID: forkReadLocal, RepositoryID: forkReadOwner,
		Archives: []string{"Decisions/fork-archive-*.md"}, ParentBindingID: binding.ID, Bindings: []decisionview.Binding{binding},
	}
	forkReadWriteLedgerStatus(t, planning, "Decisions/fork-archive-2025.md", "archived", []map[string]any{
		forkReadEntry("D-0099", "rejected", "archived local history"),
	}, nil)
	forkReadWriteLedger(t, planning, "Decisions/fork.md", []map[string]any{
		localEntry,
		forkReadEntry("D-0101", "proposed", "future local proposal"),
		forkReadEntry("D-0102", "rejected", "rejected local history"),
	}, metadata)
	config, err := json.Marshal(map[string]any{
		"planningRoot": ".plans", "repositoryId": forkReadOwner,
		"decisionLog": map[string]any{"version": 1, "mode": "fork", "path": "Decisions/fork.md", "ledgerId": forkReadLocal},
	})
	if err != nil {
		t.Fatal(err)
	}
	forkReadWrite(t, root, "planning-config.json", config)
	return root
}

func forkReadEntry(id, status, statement string) map[string]any {
	return map[string]any{
		"id": id, "kind": "decision", "status": status, "date": "2026-09-09", "decided_by": "user",
		"statement": statement, "rationale": "fixture rationale", "confirmation": "fixture confirmation",
		"rejected": []string{}, "scope": []string{"cmd/sdd/"}, "tags": []string{"fixture"}, "reversibility": "two-way",
	}
}

func forkReadWriteLedger(t *testing.T, root, relative string, entries []map[string]any, metadata any) {
	t.Helper()
	forkReadWriteLedgerStatus(t, root, relative, "active", entries, metadata)
}

func forkReadWriteLedgerStatus(t *testing.T, root, relative, status string, entries []map[string]any, metadata any) {
	t.Helper()
	frontmatter := map[string]any{
		"title": "Fork read fixture", "type": "decision-log", "status": status,
		"created": "2026-09-09", "updated": "2026-09-09", "tags": []string{}, "related": []string{}, "decisions": entries,
	}
	if metadata != nil {
		frontmatter["fork"] = metadata
	}
	raw, err := yaml.Marshal(frontmatter)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(append([]byte("---\n"), raw...), []byte("---\n\n# Fork read fixture\n")...)
	forkReadWrite(t, root, relative, raw)
}

func forkReadWrite(t *testing.T, root, relative string, raw []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func forkReadSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, item os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
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
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(raw))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func qualified(collection decisionview.CollectionID, id string) string {
	return "ledger:" + string(collection) + ":" + id
}

func requireCLIExit(t *testing.T, err error, want int, stdout, stderr string) {
	t.Helper()
	got := 0
	if err != nil {
		type exitCoder interface{ ExitCode() int }
		if exit, ok := err.(exitCoder); ok {
			got = exit.ExitCode()
		} else {
			t.Fatalf("CLI did not report a process exit: %v", err)
		}
	}
	if got != want {
		t.Fatalf("CLI exit = %d, want %d\nstdout: %s\nstderr: %s", got, want, stdout, stderr)
	}
}

func decodeCLIJSON(t *testing.T, stdout string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(stdout), &value); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return value
}

func requireJSONVersion(t *testing.T, value any) {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok || object["version"] != float64(1) {
		t.Errorf("fork read output is not version 1: %#v", value)
	}
}

func jsonContainsString(value any, want string) bool {
	switch value := value.(type) {
	case string:
		return value == want
	case []any:
		for _, item := range value {
			if jsonContainsString(item, want) {
				return true
			}
		}
	case map[string]any:
		for _, item := range value {
			if jsonContainsString(item, want) {
				return true
			}
		}
	}
	return false
}

func jsonContainsSubstring(value any, want string) bool {
	switch value := value.(type) {
	case string:
		return strings.Contains(value, want)
	case []any:
		for _, item := range value {
			if jsonContainsSubstring(item, want) {
				return true
			}
		}
	case map[string]any:
		for _, item := range value {
			if jsonContainsSubstring(item, want) {
				return true
			}
		}
	}
	return false
}

func diagnosticMessages(t *testing.T, value any) []string {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("diagnostic output is not an object: %#v", value)
	}
	raw, ok := object["diagnostics"].([]any)
	if !ok {
		t.Fatalf("diagnostic output omits diagnostics: %#v", value)
	}
	messages := make([]string, 0, len(raw))
	for _, item := range raw {
		diagnostic, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("diagnostic is not an object: %#v", item)
		}
		message, _ := diagnostic["message"].(string)
		messages = append(messages, message)
	}
	return messages
}

func orderedStrings(have, want []string) bool {
	at := 0
	for _, value := range have {
		if at < len(want) && value == want[at] {
			at++
		}
	}
	return at == len(want)
}
