package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestForkWorkflowCommandsAndReferences(t *testing.T) {
	root := forkWorkflowRoot(t)
	bin := stressBinary(t)
	stdout, stderr, err := runSdd(bin, root, "decide", "capabilities", "--json")
	requireCLIExit(t, err, 0, stdout, stderr)
	capabilities := decodeCLIObject(t, stdout)
	forks, ok := capabilities["decision_forks"].(map[string]any)
	if !ok {
		t.Errorf("capabilities must expose the canonical decision_forks key: %s", stdout)
		forks = map[string]any{}
	}
	if forks["schema"] != float64(1) || forks["transactions"] != float64(1) ||
		!reflect.DeepEqual(forks["canonicalization"], []any{"entry-v1"}) {
		t.Errorf("decision_forks capability does not advertise the schema/canonicalization/transaction contract: %s", stdout)
	}
	if _, obsolete := capabilities["decisionforks"]; obsolete {
		t.Errorf("capabilities still expose the obsolete decisionforks producer key: %s", stdout)
	}

	for _, args := range [][]string{
		{"decide", "capabilities", "--help"},
		{"decide", "effective", "--help"},
		{"decide", "history", "--help"},
		{"decide", "lookup", "--help"},
		{"decide", "validate", "--help"},
		{"decide", "fork", "preview", "--help"},
		{"decide", "fork", "apply", "--help"},
		{"decide", "fork", "inspect", "--help"},
		{"decide", "fork", "recover", "--help"},
	} {
		out, diagnostic, runErr := runSdd(bin, root, args...)
		requireCLIExit(t, runErr, 0, out, diagnostic)
	}

	legacy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(legacy, ".plans", "Decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "planning-config.json"), []byte(`{"planningRoot":".plans"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := "---\ntitle: Decisions\ntype: decision-log\nstatus: active\ncreated: 2026-09-10\nupdated: 2026-09-10\ntags: []\nrelated: []\ndecisions:\n  - id: D-0001\n    kind: decision\n    status: accepted\n    date: 2026-09-10\n    decided_by: user-approved\n    statement: legacy branch remains available\n    rejected: []\n    rationale: exercises the conventional reader\n    scope: []\n    tags: [legacy]\n---\n"
	if err := os.WriteFile(filepath.Join(legacy, ".plans", "Decisions", "decisions.md"), []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"decide", "list", "--status", "accepted", "--json"},
		{"decide", "search", "legacy", "--json"},
	} {
		out, diagnostic, runErr := runSdd(bin, legacy, args...)
		requireCLIExit(t, runErr, 0, out, diagnostic)
		if !strings.Contains(out, "legacy branch remains available") {
			t.Errorf("legacy command %v did not read conventional authority: %s", args, out)
		}
	}

	assertWorkflowFile(t, root, "commands/decide/SKILL.md",
		"sdd decide capabilities --json", "decision_forks", "sdd decide effective --json",
		"sdd decide history", "sdd decide lookup", "sdd decide fork preview --operation",
		"sdd decide fork apply --file", "--approval-digest", "sdd decide fork inspect --operation",
		"sdd decide fork recover --operation <operation-id> --action <finish|rollback|discard-staging> --approval-digest",
		"/decide check", "add", "archive", "hygiene",
		"refus", "full", "exact", "json", "direct", "inherited",
		"sdd decide list --status accepted --json", "sdd decide search")
	assertWorkflowFile(t, root, "skills/decision-log/SKILL.md",
		"sdd decide capabilities --json", "decision_forks", "sdd decide effective --json",
		"sdd decide fork preview", "sdd decide fork apply", "full", "exact", "json",
		"approval", "direct", "inherited")
	assertWorkflowFile(t, root, "commands/setup/SKILL.md",
		"sdd decide capabilities --json", "decision_forks", "planning-config.json",
		"planning root", "repository", "remove", "rename", "manual restor")
	assertWorkflowFile(t, root, "shared/decision-log.md",
		"sdd decide capabilities --json", "decision_forks", "sdd decide effective --json",
		"planning-config.json", "planning root", "repository", "full", "exact", "json",
		"approval", "remove", "rename", "manual restor", "add", "archive", "hygiene",
		"refus", "direct", "inherited",
		"sdd decide fork recover --operation <operation-id> --action <finish|rollback|discard-staging> --approval-digest")
	assertWorkflowFile(t, root, "shared/path-resolution.md",
		"planning-config.json", "decisionlog", "planning root", "repository", "decision_forks",
		"repositoryid", "sibling", "ledgerid")
	assertWorkflowFile(t, root, "shared/orchestration.md",
		"sdd decide capabilities --json", "sdd decide effective --json", "diagnostic",
		"sdd decide list --status accepted --json", "decisionlog")

	for _, rel := range []string{
		"shared/templates/claude-md-full.md", "shared/templates/claude-md-snippet.md",
		"shared/templates/agents-md-full.md", "shared/templates/agents-md-snippet.md",
	} {
		assertWorkflowFile(t, root, rel, "decisionlog", "decision_forks", "sdd decide effective --json",
			"sdd decide list --status accepted --json", "legacy")
	}
	for _, rel := range []string{"agents/researcher.md", "agents/plan-reviewer.md", "agents/spec-reviewer.md"} {
		assertWorkflowFile(t, root, rel, "sdd decide capabilities --json", "sdd decide effective --json", "diagnostic",
			"sdd decide list --status accepted --json", "decisionlog")
	}
	for _, rel := range []string{
		"agents/code-implementer.md", "commands/implement/SKILL.portable.md",
		"commands/code-review/SKILL.md", "commands/code-review/SKILL.portable.md",
		"commands/validate/SKILL.md", "skills/decision-log/SKILL.md", "skills/sdd-cli/SKILL.md",
	} {
		assertWorkflowFile(t, root, rel, "decisionlog", "fork", "detached",
			"sdd decide effective --json", "sdd decide list --status accepted --json")
	}
	assertWorkflowFile(t, root, "shared/frontmatter-schema.md",
		"fork:", "ledgerid", "repositoryid", "parentbindingid", "bindings", "events",
		"legacycontexts", "operationids", "decisionlog", "transaction")
}

func forkWorkflowRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller information")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func assertWorkflowFile(t *testing.T, root, rel string, terms ...string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	content := strings.ToLower(string(raw))
	for _, term := range terms {
		if !strings.Contains(content, strings.ToLower(term)) {
			t.Errorf("%s does not document %q", rel, term)
		}
	}
}
