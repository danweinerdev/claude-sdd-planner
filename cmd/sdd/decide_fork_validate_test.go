package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// These tests intentionally use the built executable. The contract is about
// the authority context observed by real validation, apply, and lifecycle
// entry points, not merely the already-covered rules.Root adapter.
func TestForkValidationRealEntryPoint(t *testing.T) {
	bin := stressBinary(t)
	root := forkValidationFixture(t, true)
	wantIDs := []string{qualified(forkReadLocal, "D-0100"), qualified(forkReadParent, "D-0001")}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"focused", []string{"decide", "validate", "--json"}},
		{"root", []string{"validate", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := forkValidationRun(t, bin, root, tc.args, 0)
			requireForkValidationIDs(t, got, wantIDs)
			requireForkValidationDiagnostic(t, got, "DLG064", "warning", "Source history "+string(forkReadLocal))
		})
	}

	t.Run("explicit-ledger-cannot-bypass-selected-context", func(t *testing.T) {
		root := forkValidationFixture(t, false)
		forkValidationReplaceOwner(t, filepath.Join(root, ".plans", "Decisions", "fork.md"))
		parent := filepath.Join(root, ".plans", "Decisions", "parent.md")
		got := forkValidationRun(t, bin, root, []string{"decide", "validate", parent, "--no-history", "--json"}, 1)
		requireForkValidationDiagnostic(t, got, "FDL020", "error", "represented repository")
	})

	t.Run("compile-apply-and-lifecycle-share-invalid-authority", func(t *testing.T) {
		root := forkValidationFixture(t, false)
		forkValidationReplaceOwner(t, filepath.Join(root, ".plans", "Decisions", "fork.md"))
		spec := strings.Replace(validSpec, "status: draft", "status: review", 1)
		forkReadWrite(t, filepath.Join(root, ".plans"), "Specs/Thing/README.md", []byte(spec))
		forkReadWrite(t, filepath.Join(root, ".plans"), "Research/apply.md", []byte(forkValidationCompileResearch))
		proposal := strings.Replace(forkValidationCompileResearch,
			"type: research\nstatus: draft\ncreated: 2024-01-01\nupdated: 2024-01-01\n", "", 1)

		t.Run("apply", func(t *testing.T) {
			apply := forkValidationRunInput(t, bin, root,
				[]string{"apply", "Research/apply.md", "--dry-run", "--json"}, proposal, 1)
			requireForkValidationFinding(t, apply, "FDL020", "represented repository")
		})
		t.Run("lifecycle", func(t *testing.T) {
			lifecycle := forkValidationRun(t, bin, root,
				[]string{"spec", "approve", "Specs/Thing/README.md", "--dry-run", "--json"}, 1)
			requireForkValidationFinding(t, lifecycle, "FDL020", "represented repository")
		})
	})

	t.Run("nearest-nested-repository-context", func(t *testing.T) {
		root := forkValidationFixture(t, false)
		outerGit := filepath.Join(filepath.Dir(root), ".git")
		if err := os.Mkdir(outerGit, 0o755); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(outerGit) })
		forkReadWrite(t, filepath.Join(root, ".plans"), "Research/apply.md", []byte(forkValidationCompileResearch))
		proposal := strings.Replace(forkValidationCompileResearch,
			"type: research\nstatus: draft\ncreated: 2024-01-01\nupdated: 2024-01-01\n", "", 1)
		spec := strings.Replace(validSpec, "status: draft", "status: review", 1)
		forkReadWrite(t, filepath.Join(root, ".plans"), "Specs/Thing/README.md", []byte(spec))
		for _, args := range [][]string{{"decide", "validate", "--json"}, {"validate", "--json"}} {
			got := forkValidationRun(t, bin, root, args, 0)
			requireForkValidationIDs(t, got, wantIDs)
		}
		forkValidationRunInput(t, bin, root,
			[]string{"apply", "Research/apply.md", "--dry-run", "--json"}, proposal, 0)
		forkValidationRun(t, bin, root,
			[]string{"spec", "approve", "Specs/Thing/README.md", "--dry-run", "--json"}, 0)
	})

	t.Run("section-and-migrate-refuse-unknown-fork-citations", func(t *testing.T) {
		root := forkValidationFixture(t, false)
		unknown := "ledger:99999999-9999-9999-9999-999999999999:D-00001"
		section := forkValidationRunInput(t, bin, root,
			[]string{"section", "set", "Research/in-scope.md", "--heading", "## Context", "--dry-run", "--json"},
			"Unknown "+unknown+".", 1)
		requireForkValidationFinding(t, section, "SPK040", unknown)
		path := filepath.Join(root, ".plans", "Research", "in-scope.md")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "## Context\n", "## Context\n\nUnknown "+unknown+".\n", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		migrate := forkValidationRun(t, bin, root,
			[]string{"migrate", "Research/in-scope.md", "--dry-run", "--json"}, 1)
		if !jsonContainsSubstring(migrate.Value, "SPK040") || !jsonContainsSubstring(migrate.Value, unknown) {
			t.Fatalf("migrate omitted fork citation refusal: %#v", migrate.Value)
		}
	})

	t.Run("absolute-apply-rejects-unrepresented-cross-repository-target", func(t *testing.T) {
		cwdFork := forkValidationFixture(t, false)
		external := t.TempDir()
		target := filepath.Join(external, "Research", "apply.md")
		forkReadWrite(t, external, "Research/apply.md", []byte(forkValidationCompileResearch))
		before, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		proposal := strings.Replace(forkValidationCompileResearch,
			"type: research\nstatus: draft\ncreated: 2024-01-01\nupdated: 2024-01-01\n", "", 1)
		stdout, stderr, runErr := forkValidationRunInputRaw(bin, cwdFork,
			[]string{"apply", target, "--json"}, proposal)
		requireCLIExit(t, runErr, 2, stdout, stderr)
		after, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("cross-repository apply changed the target")
		}
	})

	t.Run("legacy-existing-target-outside-planning-root-remains-supported", func(t *testing.T) {
		root := t.TempDir()
		forkReadWrite(t, root, "planning-config.json", []byte(`{"planningRoot":".plans"}`))
		target := filepath.Join(root, "legacy-research.md")
		if err := os.WriteFile(target, []byte(forkValidationCompileResearch), 0o644); err != nil {
			t.Fatal(err)
		}
		proposal := strings.Replace(forkValidationCompileResearch,
			"type: research\nstatus: draft\ncreated: 2024-01-01\nupdated: 2024-01-01\n", "", 1)
		got := forkValidationRunInput(t, bin, root, []string{"apply", target, "--dry-run", "--json"}, proposal, 0)
		object, _ := got.Value.(map[string]any)
		if object["ok"] != true {
			t.Fatalf("legacy out-of-root apply no longer follows its prior compile path: %#v", got.Value)
		}
	})
}

const forkValidationCompileResearch = `---
title: Apply Research
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

# Apply Research

## Context
Determine whether authority is safe to consume.

## Findings

### Key Insights
- The selected context is required.

### Sources
- Repository fixture, as of 2026-09-10.

## Analysis

### Implications
Writes must use the same authority as validation.

### Recommendations
Refuse when selected authority is invalid.

## Open Questions
None.
`

func TestForkValidationIndependentSets(t *testing.T) {
	bin := stressBinary(t)

	t.Run("qualified-citations-frontmatter-comments-liveness-and-archive-ids", func(t *testing.T) {
		root := forkValidationFixture(t, false)
		planning := filepath.Join(root, ".plans")
		forkValidationWriteResearch(t, planning, "Research/accepted.md",
			"uses "+qualified(forkReadParent, "D-0001")+" and legacy-context D-0100", "draft", nil)
		forkValidationWriteResearch(t, planning, "Research/comment.md",
			"<!-- ledger:99999999-9999-9999-9999-999999999999:D-9999 -->", "draft",
			[]string{"ledger:99999999-9999-9999-9999-999999999999:D-9998"})
		forkValidationWriteResearch(t, planning, "Research/historical.md",
			"uses "+qualified(forkReadLocal, "D-0099"), "draft", nil)
		forkValidationWriteResearch(t, planning, "Research/archived.md",
			"uses "+qualified(forkReadLocal, "D-0099"), "archived", nil)

		got := forkValidationRun(t, bin, root, []string{"validate", "--json"}, 1)
		paths120 := forkValidationDiagnosticPaths(got.Value, "SDD120")
		paths121 := forkValidationDiagnosticPaths(got.Value, "SDD121")
		if paths120["Research/comment.md"] || paths120["Research/accepted.md"] || paths121["Research/accepted.md"] {
			t.Fatalf("comment/frontmatter or live qualified citation behavior changed: SDD120=%v SDD121=%v", paths120, paths121)
		}
		if !paths121["Research/historical.md"] || paths121["Research/archived.md"] {
			t.Fatalf("historical decision liveness differs for live/archived artifacts: %v", paths121)
		}
		if forkValidationContainsID(got, qualified(forkReadLocal, "D-0099")) {
			t.Fatal("archived rejected local ID became effective")
		}
		requireForkValidationIDs(t, got, []string{qualified(forkReadLocal, "D-0100"), qualified(forkReadParent, "D-0001")})
	})

	t.Run("scope-filter-retains-ancestry-and-original-scope", func(t *testing.T) {
		root := forkReadFixture(t, true, false, "ordinary local rule")
		forkValidationWriteQualifiedCitation(t, filepath.Join(root, ".plans"))
		got := forkValidationRun(t, bin, root,
			[]string{"validate", "--scope", "Research/in-scope.md", "--json"}, 1)
		requireForkValidationDiagnostic(t, got, "FDL010", "error", "immutable field statement")
		requireForkValidationIDs(t, got, []string{qualified(forkReadLocal, "D-0100"), qualified(forkReadParent, "D-0001")})
	})

	t.Run("error-and-operational-are-invalidating-json", func(t *testing.T) {
		t.Run("error", func(t *testing.T) {
			root := forkValidationFixture(t, false)
			forkValidationReplaceOwner(t, filepath.Join(root, ".plans", "Decisions", "fork.md"))
			got := forkValidationRun(t, bin, root, []string{"validate", "--json"}, 1)
			requireForkValidationValid(t, got, false)
			requireForkValidationDiagnostic(t, got, "FDL020", "error", "represented repository")
		})
		t.Run("operational", func(t *testing.T) {
			if !rules.Operational.Invalidating() {
				t.Fatal("rules.Operational must block reliance")
			}
			if got := countErrorsOut([]outDiagnostic{{Code: "FDL022", Severity: string(rules.Operational)}}); got != 1 {
				t.Fatalf("root JSON invalidating count for operational authority = %d, want 1", got)
			}
			if got := exitCode(&refusedError{n: 1}); got != 1 {
				t.Fatalf("invalidating operational diagnostic exit = %d, want 1", got)
			}
		})
	})

	t.Run("candidate-guidance-is-valid-success", func(t *testing.T) {
		root := forkReadFixture(t, false, true, "local storage answer")
		if err := os.MkdirAll(filepath.Join(root, "cmd", "sdd"), 0o755); err != nil {
			t.Fatal(err)
		}
		forkValidationWriteQualifiedCitation(t, filepath.Join(root, ".plans"))
		for _, args := range [][]string{{"decide", "validate", "--json"}, {"validate", "--json"}} {
			got := forkValidationRun(t, bin, root, args, 0)
			requireForkValidationDiagnostic(t, got, "DLG060", "candidate", "Judge whether")
		}
	})

	t.Run("real-operational-path-error-has-exit-two-precedence", func(t *testing.T) {
		root := forkValidationFixture(t, false)
		operation := "pending-validation"
		journal := "Decisions/.fork-state/" + string(forkReadLocal) + "/journals/" + operation + ".json"
		barrier, err := json.Marshal(map[string]any{
			"version": 1, "operationId": operation, "ownerId": forkReadOwner,
			"collectionId": forkReadLocal, "journal": journal, "status": "pending",
		})
		if err != nil {
			t.Fatal(err)
		}
		forkReadWrite(t, filepath.Join(root, ".plans"),
			"Decisions/.fork-state/"+string(forkReadLocal)+"/pending.json", barrier)
		journals := filepath.Join(root, ".plans", "Decisions", ".fork-state", string(forkReadLocal), "journals")
		if err := os.WriteFile(journals, []byte("journal path component is a file"), 0o644); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(root, ".plans", "Research", "in-scope.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"decide", "validate", "--json"}, {"validate", "--json"}} {
			got := forkValidationRun(t, bin, root, args, 2)
			requireForkValidationValid(t, got, false)
			requireForkValidationDiagnostic(t, got, "FDL022", "operational", "barrier")
		}
		proposal := strings.Replace(string(before),
			"type: research\nstatus: draft\ncreated: 2024-01-01\nupdated: 2024-01-01\n", "", 1)
		forkValidationRunInput(t, bin, root,
			[]string{"apply", "Research/in-scope.md", "--json"}, proposal, 2)
		after, err := os.ReadFile(filepath.Join(root, ".plans", "Research", "in-scope.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatal("operational authority failure allowed mutation")
		}
	})

	t.Run("repository-resolution-error-does-not-fall-back", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "planning-config.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		ledgerRoot := t.TempDir()
		forkReadWriteLedger(t, ledgerRoot, "Decisions/decisions.md", []map[string]any{
			forkReadEntry("D-0001", "accepted", "must not be validated by fallback"),
		}, nil)
		stdout, stderr, err := runSdd(bin, root, "decide", "validate", filepath.Join(ledgerRoot, "Decisions", "decisions.md"), "--json")
		requireCLIExit(t, err, 2, stdout, stderr)
		if strings.Contains(stdout, "diagnostics") {
			t.Fatalf("repository resolution error fell through to explicit-ledger validation: %s", stdout)
		}
	})

	t.Run("configless-git-explicit-root-isolated-from-cwd-authority", func(t *testing.T) {
		external := t.TempDir()
		if err := os.Mkdir(filepath.Join(external, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		forkReadWrite(t, external, "Research/note.md", []byte(validResearchDoc))
		repo, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(repo, "tools", "regression", "fixtures", "SDD112", "missing-rationale", "Decisions", "decisions.md"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(external, "DECISIONS.md"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		gitStress(t, external, "init", "-q")
		gitStress(t, external, "add", ".")
		gitStress(t, external, "-c", "user.name=SDD Test", "-c", "user.email=sdd@example.invalid", "commit", "-qm", "fixture")
		cwdFork := forkValidationFixture(t, false)
		for _, cwd := range []string{external, cwdFork} {
			got := forkValidationRun(t, bin, cwd, []string{"validate", "--root", external, "--json"}, 1)
			requireForkValidationDiagnostic(t, got, "DLG022", "error", "rationale")
			if len(forkValidationIDs(got.Value)) != 0 || jsonContainsString(got.Value, "FDL020") {
				t.Fatalf("explicit config-less root consumed unrelated cwd fork authority: %#v", got.Value)
			}
		}
	})
}

func TestForkValidationLegacyCorpus(t *testing.T) {
	bin := stressBinary(t)
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, fixture, code, severity string
		exit                          int
	}{
		{"missing-rationale", "tools/regression/fixtures/SDD112/missing-rationale", "DLG022", "error", 1},
		{"collision-candidate", "tools/regression/fixtures/SDD147/same-question-different-answers", "DLG060", "candidate", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(repo, filepath.FromSlash(tc.fixture))
			ledger := filepath.Join(root, "Decisions", "decisions.md")
			focused := forkValidationRun(t, bin, root,
				[]string{"decide", "validate", ledger, "--no-history", "--json"}, tc.exit)
			requireForkValidationDiagnostic(t, focused, tc.code, tc.severity, "")
			whole := forkValidationRun(t, bin, root, []string{"validate", "--root", root, "--json"}, tc.exit)
			if !reflect.DeepEqual(forkValidationDLGDiagnostics(whole.Value), forkValidationDLGDiagnostics(focused.Value)) {
				t.Fatalf("legacy focused/root DLG verdict drift\nfocused: %#v\nroot: %#v",
					forkValidationDLGDiagnostics(focused.Value), forkValidationDLGDiagnostics(whole.Value))
			}
		})
	}
}

func forkValidationFixture(t *testing.T, external bool) string {
	t.Helper()
	root := t.TempDir()
	planning := filepath.Join(root, ".plans")
	if external {
		planning = t.TempDir()
	}
	parentEntry := forkReadEntry("D-0001", "accepted", "parent authority rule")
	parentEntry["scope"] = []string{"Research/in-scope.md"}
	forkReadWriteLedger(t, planning, "Decisions/parent.md", []map[string]any{parentEntry}, nil)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: planning}, forkReadParent,
		decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatalf("load parent fixture: %v", err)
	}
	binding, err := decisionview.BindCollection("parent-binding", forkReadOwner, parent)
	if err != nil {
		t.Fatalf("bind parent fixture: %v", err)
	}
	metadata := decisionview.ForkMetadata{
		Version: decisionview.Version1, LedgerID: forkReadLocal, RepositoryID: forkReadOwner,
		Archives: []string{"Decisions/fork-archive-*.md"}, ParentBindingID: binding.ID,
		Bindings: []decisionview.Binding{binding},
		LegacyContexts: []decisionview.LegacyContext{{Root: decisionview.SourceRootPlanning,
			Path: "Research/in-scope.md", Namespace: forkReadLocal, LocalIDs: []string{"D-0100"}}},
	}
	archived := forkReadEntry("D-0099", "rejected", "archived local history")
	archived["scope"] = []string{"Research/in-scope.md"}
	local := forkReadEntry("D-0100", "accepted", "local authority rule")
	local["scope"] = []string{"Research/in-scope.md"}
	forkReadWriteLedgerStatus(t, planning, "Decisions/fork-archive-2025.md", "archived", []map[string]any{archived}, nil)
	forkReadWriteLedger(t, planning, "Decisions/fork.md", []map[string]any{local}, metadata)
	forkValidationWriteQualifiedCitation(t, planning)
	config := map[string]any{"planningRoot": planning, "repositoryId": forkReadOwner,
		"decisionLog": map[string]any{"version": 1, "mode": "fork", "path": "Decisions/fork.md", "ledgerId": forkReadLocal}}
	if !external {
		config["planningRoot"] = ".plans"
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	forkReadWrite(t, root, "planning-config.json", raw)
	return root
}

func forkValidationWriteQualifiedCitation(t *testing.T, planning string) {
	t.Helper()
	doc := strings.Replace(validResearchDoc, "## Context\n\nText.",
		"## Context\n\nInherited authority "+qualified(forkReadParent, "D-0001")+
			" and local authority D-0100 apply.", 1)
	forkReadWrite(t, planning, "Research/in-scope.md", []byte(doc))
}

func forkValidationWriteResearch(t *testing.T, planning, rel, body, status string, related []string) {
	t.Helper()
	doc := strings.Replace(validResearchDoc, "status: draft", "status: "+status, 1)
	if related != nil {
		raw, err := json.Marshal(related)
		if err != nil {
			t.Fatal(err)
		}
		doc = strings.Replace(doc, "related: []", "related: "+string(raw), 1)
	}
	doc = strings.Replace(doc, "## Context\n\nText.", "## Context\n\n"+body, 1)
	forkReadWrite(t, planning, rel, []byte(doc))
}

func forkValidationReplaceOwner(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), string(forkReadOwner), "99999999-8888-7777-6666-555555555555", 1))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

type forkValidationResult struct {
	Exit  int
	Value any
}

func forkValidationRun(t *testing.T, bin, root string, args []string, wantExit int) forkValidationResult {
	t.Helper()
	stdout, stderr, err := runSdd(bin, root, args...)
	requireCLIExit(t, err, wantExit, stdout, stderr)
	return forkValidationResult{Exit: wantExit, Value: decodeCLIJSON(t, stdout)}
}

func forkValidationRunInput(t *testing.T, bin, root string, args []string, input string, wantExit int) forkValidationResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	requireCLIExit(t, err, wantExit, stdout.String(), stderr.String())
	return forkValidationResult{Exit: wantExit, Value: decodeCLIJSON(t, stdout.String())}
}

func forkValidationRunInputRaw(bin, root string, args []string, input string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func requireForkValidationIDs(t *testing.T, got forkValidationResult, want []string) {
	t.Helper()
	have := forkValidationIDs(got.Value)
	sort.Strings(want)
	if !reflect.DeepEqual(have, want) {
		t.Fatalf("effective decision IDs = %q, want %q in %#v", have, want, got.Value)
	}
}

func forkValidationIDs(value any) []string {
	set := map[string]bool{}
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case []any:
			for _, item := range value {
				walk(item)
			}
		case map[string]any:
			if id, ok := value["qualified_id"].(string); ok && value["applicability"] == "binding" {
				set[id] = true
			}
			for _, key := range []string{"effective_decision_ids", "effective_ids"} {
				if ids, ok := value[key].([]any); ok {
					for _, raw := range ids {
						if id, ok := raw.(string); ok {
							set[id] = true
						}
					}
				}
			}
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(value)
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func forkValidationContainsID(result forkValidationResult, id string) bool {
	for _, got := range forkValidationIDs(result.Value) {
		if got == id {
			return true
		}
	}
	return false
}

type forkValidationDiagnostic struct {
	Code, Severity, Path, Message, Correction string
	Line                                      int
}

func forkValidationDiagnostics(value any) []forkValidationDiagnostic {
	object, _ := value.(map[string]any)
	raw, _ := object["diagnostics"].([]any)
	out := make([]forkValidationDiagnostic, 0, len(raw))
	for _, item := range raw {
		d, _ := item.(map[string]any)
		path, _ := d["path"].(string)
		path = filepath.ToSlash(path)
		if at := strings.LastIndex(path, "/Decisions/"); at >= 0 {
			path = path[at+1:]
		}
		n, _ := d["line"].(float64)
		out = append(out, forkValidationDiagnostic{
			Code: forkValidationString(d["code"]), Severity: forkValidationString(d["severity"]),
			Path: path, Line: int(n), Message: forkValidationString(d["message"]),
			Correction: forkValidationString(d["correction"]),
		})
	}
	return out
}

func forkValidationDLGDiagnostics(value any) []forkValidationDiagnostic {
	var out []forkValidationDiagnostic
	for _, d := range forkValidationDiagnostics(value) {
		if strings.HasPrefix(d.Code, "DLG") {
			out = append(out, d)
		}
	}
	return out
}

func requireForkValidationDiagnostic(t *testing.T, got forkValidationResult, code, severity, messagePart string) {
	t.Helper()
	for _, d := range forkValidationDiagnostics(got.Value) {
		if d.Code == code && d.Severity == severity && strings.Contains(d.Message+" "+d.Correction, messagePart) {
			return
		}
	}
	t.Fatalf("missing %s/%s diagnostic containing %q: %#v", code, severity, messagePart, forkValidationDiagnostics(got.Value))
}

func requireForkValidationFinding(t *testing.T, got forkValidationResult, code, messagePart string) {
	t.Helper()
	var found bool
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case []any:
			for _, child := range value {
				walk(child)
			}
		case map[string]any:
			if forkValidationString(value["code"]) == code &&
				strings.Contains(forkValidationString(value["message"])+" "+forkValidationString(value["correction"]), messagePart) {
				found = true
			}
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(got.Value)
	if !found {
		t.Fatalf("missing %s finding containing %q in %#v", code, messagePart, got.Value)
	}
}

func forkValidationDiagnosticPaths(value any, code string) map[string]bool {
	out := map[string]bool{}
	for _, d := range forkValidationDiagnostics(value) {
		if d.Code == code {
			out[d.Path] = true
		}
	}
	return out
}

func requireForkValidationValid(t *testing.T, got forkValidationResult, want bool) {
	t.Helper()
	object, ok := got.Value.(map[string]any)
	if !ok || object["valid"] != want {
		t.Fatalf("valid = %#v, want %v in %#v", object["valid"], want, got.Value)
	}
}

func forkValidationString(value any) string {
	s, _ := value.(string)
	return s
}
