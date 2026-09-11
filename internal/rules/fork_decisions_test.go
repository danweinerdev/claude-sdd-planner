package rules

import (
	"context"
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
	forkRulesOwner       = decisionview.OwnerID("11111111-1111-1111-1111-111111111111")
	forkRulesSourceOwner = decisionview.OwnerID("22222222-2222-2222-2222-222222222222")
	forkRulesLocal       = decisionview.CollectionID("33333333-3333-3333-3333-333333333333")
	forkRulesParent      = decisionview.CollectionID("44444444-4444-4444-4444-444444444444")
)

type forkRulesDiskFixture struct {
	repository string
	planning   string
	localQ     string
	parentQ    string
}

func TestForkRulesAuthoritySets(t *testing.T) {
	f := newForkRulesDiskFixture(t, []string{"Research/local.md"}, []string{"Research/parent.md"})
	writeForkRulesResearch(t, f.planning, "Research/local.md", "local target without a citation", "draft")
	writeForkRulesResearch(t, f.planning, "Research/parent.md", "parent target without a citation", "draft")
	before := forkRulesSnapshot(t, f.repository)

	r, err := LoadRootRepo(f.planning, f.repository)
	if err != nil {
		t.Fatal(err)
	}
	diags := Run(r)

	if got := forkRulesByCode(diags, "SDD032"); len(got) != 0 {
		t.Fatalf("equal bare IDs in independently qualified collections are not duplicates: %+v", got)
	}
	wantScoped := map[string]bool{"Research/local.md": true, "Research/parent.md": true}
	gotScoped := map[string]bool{}
	for _, d := range forkRulesByCode(diags, "SDD146") {
		gotScoped[d.Path] = true
	}
	if !reflect.DeepEqual(gotScoped, wantScoped) {
		t.Fatalf("rules did not validate both qualified effective authority sets independently: got %v, want %v", gotScoped, wantScoped)
	}
	if after := forkRulesSnapshot(t, f.repository); !reflect.DeepEqual(after, before) {
		t.Fatalf("programmatic validation changed or created repository files\nbefore: %v\nafter:  %v", before, after)
	}
}

func TestForkRulesCitationCompatibility(t *testing.T) {
	f := newForkRulesDiskFixture(t, nil, nil)
	writeForkRulesResearch(t, f.planning, "Research/comment.md", "<!-- ledger:99999999-9999-9999-9999-999999999999:D-9999 -->", "draft")
	writeForkRulesResearch(t, f.planning, "Research/qualified-local.md", "uses "+f.localQ+":D-0002", "draft")
	writeForkRulesResearch(t, f.planning, "Research/qualified-history.md", "uses "+f.parentQ+":D-0002", "draft")
	writeForkRulesResearch(t, f.planning, "Research/unknown-collection.md", "uses ledger:99999999-9999-9999-9999-999999999999:D-0001", "draft")
	writeForkRulesResearch(t, f.planning, "Research/unknown-id.md", "uses "+f.parentQ+":D-9999", "draft")
	writeForkRulesResearch(t, f.planning, "Research/legacy.md", "legacy context uses D-0002", "draft")
	writeForkRulesResearch(t, f.planning, "Research/archived.md", "historical artifact uses "+f.parentQ+":D-0002", "archived")
	before := forkRulesSnapshot(t, f.repository)

	r, err := LoadRootRepo(f.planning, f.repository)
	if err != nil {
		t.Fatal(err)
	}
	diags := Run(r)

	wantUnknown := map[string]bool{"Research/unknown-collection.md": true, "Research/unknown-id.md": true}
	gotUnknown := forkRulesPaths(forkRulesByCode(diags, "SDD120"))
	if !reflect.DeepEqual(gotUnknown, wantUnknown) {
		t.Fatalf("qualified unknown citations were not resolved atomically: got %v, want %v", gotUnknown, wantUnknown)
	}
	wantHistorical := map[string]bool{"Research/legacy.md": true, "Research/qualified-history.md": true}
	gotHistorical := forkRulesPaths(forkRulesByCode(diags, "SDD121"))
	if !reflect.DeepEqual(gotHistorical, wantHistorical) {
		t.Fatalf("live historical citations were not distinguished from accepted/archived contexts: got %v, want %v", gotHistorical, wantHistorical)
	}
	for _, path := range []string{"Research/comment.md", "Research/qualified-local.md", "Research/archived.md"} {
		if gotUnknown[path] || gotHistorical[path] {
			t.Errorf("existing comment/frontmatter/liveness behavior regressed for %s", path)
		}
	}
	if after := forkRulesSnapshot(t, f.repository); !reflect.DeepEqual(after, before) {
		t.Fatal("citation validation had read side effects")
	}

	legacy := t.TempDir()
	forkRulesWriteLedger(t, legacy, "Decisions/decisions.md", "active", []map[string]any{
		forkRulesDecision("D-0001", "accepted", "legacy authority", nil),
	}, nil)
	writeForkRulesResearch(t, legacy, "Research/legacy-qualified-text.md", "legacy text ledger:99999999-9999-9999-9999-999999999999:D-0001", "draft")
	legacyRoot, err := LoadRoot(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if got := forkRulesByCode(Run(legacyRoot), "SDD120"); len(got) != 0 {
		t.Fatalf("never-adopted repository stopped using the exact legacy D-NNNN scanner: %+v", got)
	}
}

func TestForkRulesOperationalAndOwnerIsolation(t *testing.T) {
	t.Run("scope remains per effective record", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, []string{"Missing/local.md"}, nil)
		diags := forkRulesRun(t, f)
		if !forkRulesHas(diags, "SDD145", "Decisions/fork.md", "Missing/local.md") {
			t.Fatalf("missing local scope was not retained on its qualified record: %+v", forkRulesByCode(diags, "SDD145"))
		}
	})

	t.Run("filesystem scope uses consuming repository from relative roots", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, []string{"code/existing"}, nil)
		existing := filepath.Join(f.repository, "code", "existing")
		if err := os.MkdirAll(existing, 0o755); err != nil {
			t.Fatal(err)
		}
		original, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(original) })
		check := func(cwd, planning, repository string, wantMissing bool) {
			t.Helper()
			if err := os.Chdir(cwd); err != nil {
				t.Fatal(err)
			}
			r, err := LoadRootRepo(planning, repository)
			if err != nil {
				t.Fatal(err)
			}
			missing := forkRulesHas(Run(r), "SDD145", "Decisions/fork.md", "code/existing")
			if missing != wantMissing {
				t.Fatalf("SDD145 missing=%v, want %v from cwd %s (repo root %s)", missing, wantMissing, cwd, r.RepoRoot)
			}
		}
		parent := filepath.Dir(f.repository)
		check(parent, filepath.Join(filepath.Base(f.repository), ".plans"), filepath.Base(f.repository), false)
		check(f.repository, ".plans", ".", false)
		if err := os.Remove(existing); err != nil {
			t.Fatal(err)
		}
		check(f.repository, ".plans", ".", true)
	})

	t.Run("authoritative diagnostics have one owner", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, nil, nil)
		forkPath := filepath.Join(f.planning, "Decisions", "fork.md")
		raw, err := os.ReadFile(forkPath)
		if err != nil {
			t.Fatal(err)
		}
		lastRejected := strings.LastIndex(string(raw), "rejected: []")
		if lastRejected < 0 {
			t.Fatal("fixture lacks local rejected list")
		}
		raw = append(append(append([]byte(nil), raw[:lastRejected]...), []byte("rejected:\n        - local live decision")...), raw[lastRejected+len("rejected: []"):]...)
		if err := os.WriteFile(forkPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		archivePath := filepath.Join(f.planning, "Decisions", "fork-archive-2025.md")
		archive, err := os.ReadFile(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(archivePath, []byte(strings.Replace(string(archive), "D-0003", "D-0004", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := LoadRootRepo(f.planning, f.repository)
		if err != nil {
			t.Fatal(err)
		}
		run := RunWithWaivers(r)
		if got := len(forkRulesByCode(run, "DLG061")); got != 1 {
			t.Fatalf("candidate multiplicity = %d, want 1: %+v", got, forkRulesByCode(run, "DLG061"))
		}
		if got := len(forkRulesByCode(run, "DLG064")); got != 1 {
			t.Fatalf("warning multiplicity = %d, want 1: %+v", got, forkRulesByCode(run, "DLG064"))
		}
		composed := append(append([]Diagnostic(nil), run...), FocusedDecisionLogs(r, false)...)
		if len(forkRulesByCode(composed, "DLG061")) != 1 || len(forkRulesByCode(composed, "DLG064")) != 1 {
			t.Fatalf("Run+Focused duplicated authoritative diagnostics: %+v", composed)
		}
	})

	t.Run("removed and unreadable selectors cannot become clean legacy", func(t *testing.T) {
		t.Run("retained owner identity", func(t *testing.T) {
			f := newForkRulesDiskFixture(t, nil, nil)
			configPath := filepath.Join(f.repository, "planning-config.json")
			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			delete(config, "decisionLog")
			raw, _ = json.Marshal(config)
			if err := os.WriteFile(configPath, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			r, err := LoadRootRepo(f.planning, f.repository)
			if err != nil {
				t.Fatal(err)
			}
			if r.DecisionView == nil || !forkRulesAny(Run(r), "removed", Error) || len(DecisionStatuses(r)) != 0 {
				t.Fatalf("removed selector silently restored physical legacy authority: view=%+v statuses=%v diagnostics=%+v", r.DecisionView, DecisionStatuses(r), Run(r))
			}
		})

		t.Run("selector read failure", func(t *testing.T) {
			f := newForkRulesDiskFixture(t, nil, nil)
			configPath := filepath.Join(f.repository, "planning-config.json")
			if err := os.Remove(configPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(configPath, 0o755); err != nil {
				t.Fatal(err)
			}
			r, err := LoadRootRepo(f.planning, f.repository)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range Run(r) {
				if d.Severity == Operational && d.Severity.Invalidating() {
					found = true
				}
			}
			if r.DecisionView == nil || !found {
				t.Fatalf("selector read failure was dropped or non-blocking: view=%+v diagnostics=%+v", r.DecisionView, Run(r))
			}
		})

		t.Run("mis-cased declaration", func(t *testing.T) {
			f := newForkRulesDiskFixture(t, nil, nil)
			configPath := filepath.Join(f.repository, "planning-config.json")
			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			raw = []byte(strings.Replace(string(raw), `"decisionLog"`, `"DecisionLog"`, 1))
			if err := os.WriteFile(configPath, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			r, err := LoadRootRepo(f.planning, f.repository)
			if err != nil {
				t.Fatal(err)
			}
			if r.DecisionView == nil || len(DecisionStatuses(r)) != 0 || !forkRulesAny(Run(r), "selector", Error) {
				t.Fatalf("mis-cased selector silently became legacy: view=%+v statuses=%v diagnostics=%+v", r.DecisionView, DecisionStatuses(r), Run(r))
			}
		})
	})

	t.Run("transaction barriers block shared capture", func(t *testing.T) {
		t.Run("local pending from another repository in external planning root", func(t *testing.T) {
			f := newForkRulesDiskFixture(t, nil, nil)
			base := filepath.Dir(f.repository)
			shared := filepath.Join(base, "shared-planning")
			if err := os.Rename(f.planning, shared); err != nil {
				t.Fatal(err)
			}
			f.planning = shared
			forkRulesSetPlanningRoot(t, f.repository, shared)
			otherRepo := filepath.Join(base, "repository-b")
			if err := os.MkdirAll(otherRepo, 0o755); err != nil {
				t.Fatal(err)
			}
			forkRulesWriteOtherSelection(t, otherRepo, shared)
			forkRulesSavePendingBarrier(t, otherRepo, shared, forkRulesLocal, "other-repository-pending")
			before := forkRulesSnapshot(t, base)
			r, err := LoadRootRepo(shared, f.repository)
			if err != nil {
				t.Fatal(err)
			}
			if r.DecisionView == nil || r.DecisionView.View != nil || len(DecisionStatuses(r)) != 0 || !forkRulesAny(Run(r), "barrier", Error) {
				t.Fatalf("foreign pending barrier exposed clean authority: view=%+v statuses=%v diagnostics=%+v", r.DecisionView, DecisionStatuses(r), Run(r))
			}
			if after := forkRulesSnapshot(t, base); !reflect.DeepEqual(after, before) {
				t.Fatal("barrier inspection changed shared planning files")
			}
		})

		t.Run("ancestor pending", func(t *testing.T) {
			f := newForkRulesDiskFixture(t, nil, nil)
			forkRulesSavePendingBarrier(t, f.repository, f.planning, forkRulesParent, "ancestor-pending")
			r, err := LoadRootRepo(f.planning, f.repository)
			if err != nil {
				t.Fatal(err)
			}
			if r.DecisionView == nil || r.DecisionView.View != nil || !forkRulesAny(Run(r), "barrier", Error) {
				t.Fatalf("ancestor barrier did not block authority: %+v", Run(r))
			}
		})

		t.Run("corrupt versus unreadable", func(t *testing.T) {
			t.Run("corrupt is structural", func(t *testing.T) {
				f := newForkRulesDiskFixture(t, nil, nil)
				forkRulesSavePendingBarrier(t, f.repository, f.planning, forkRulesLocal, "corrupt-barrier")
				barrier := filepath.Join(f.planning, "Decisions", ".fork-state", string(forkRulesLocal), "pending.json")
				if err := os.WriteFile(barrier, []byte(`{"version":`), 0o600); err != nil {
					t.Fatal(err)
				}
				r, _ := LoadRootRepo(f.planning, f.repository)
				if !forkRulesAny(Run(r), "barrier", Error) || forkRulesAny(Run(r), "barrier", Operational) {
					t.Fatalf("corrupt barrier severity was not structural: %+v", Run(r))
				}
			})

			t.Run("unreadable journal is operational", func(t *testing.T) {
				f := newForkRulesDiskFixture(t, nil, nil)
				operation := "missing-barrier-journal"
				forkRulesSavePendingBarrier(t, f.repository, f.planning, forkRulesLocal, operation)
				journal := filepath.Join(f.planning, "Decisions", ".fork-state", string(forkRulesLocal), "journals", operation+".json")
				if err := os.Remove(journal); err != nil {
					t.Fatal(err)
				}
				r, _ := LoadRootRepo(f.planning, f.repository)
				if !forkRulesAny(Run(r), "barrier", Operational) {
					t.Fatalf("unreadable barrier journal was not operational: %+v", Run(r))
				}
			})
		})
	})

	t.Run("fork keeps independent mapped repository validation", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, nil, nil)
		target := filepath.Join(filepath.Dir(f.repository), "mapped-repository")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "DECISIONS.md"), []byte("not a ledger\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(f.repository, "planning-config.json")
		raw, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		config["planMapping"] = map[string]any{"Mapped": "target"}
		config["repositories"] = map[string]any{"target": map[string]any{"path": target}}
		raw, _ = json.Marshal(config)
		if err := os.WriteFile(configPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := LoadRootRepo(f.planning, f.repository)
		if err != nil {
			t.Fatal(err)
		}
		if got := forkRulesByCode(FocusedDecisionLogs(r, false), "DLG003"); len(got) != 1 {
			t.Fatalf("mapped repository decision validation was skipped or duplicated: %+v", got)
		}
	})

	t.Run("represented owner mismatch is global", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, nil, nil)
		config := filepath.Join(f.repository, "planning-config.json")
		raw, err := os.ReadFile(config)
		if err != nil {
			t.Fatal(err)
		}
		raw = []byte(strings.Replace(string(raw), string(forkRulesOwner), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1))
		if err := os.WriteFile(config, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		diags := forkRulesRun(t, f)
		if !forkRulesAny(diags, "owner", Error) {
			t.Fatalf("selected collection owner mismatch did not invalidate the shared rules view: %+v", diags)
		}
	})

	t.Run("unreadable source is operational and global", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, nil, nil)
		if err := os.Remove(filepath.Join(f.planning, "Decisions", "decisions.md")); err != nil {
			t.Fatal(err)
		}
		before := forkRulesSnapshot(t, f.repository)
		diags := forkRulesRun(t, f)
		if !forkRulesAny(diags, "source", Severity("operational")) {
			t.Fatalf("missing selected source was not reported as a global operational failure: %+v", diags)
		}
		if after := forkRulesSnapshot(t, f.repository); !reflect.DeepEqual(after, before) {
			t.Fatal("failed source read created recovery or support state")
		}
	})

	t.Run("stale binding basis cannot be hidden", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, nil, nil)
		path := filepath.Join(f.planning, "Decisions", "decisions.md")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		raw = []byte(strings.Replace(string(raw), "parent live decision", "changed parent authority", 1))
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		diags := forkRulesRun(t, f)
		if !forkRulesAny(diags, "binding", Error) && !forkRulesAny(diags, "basis", Error) {
			t.Fatalf("stale source basis was absent from unfiltered programmatic diagnostics: %+v", diags)
		}
	})
}

func newForkRulesDiskFixture(t *testing.T, localScope, parentScope []string) forkRulesDiskFixture {
	t.Helper()
	repository := filepath.Join(t.TempDir(), "repository")
	planning := filepath.Join(repository, ".plans")
	if err := os.MkdirAll(filepath.Join(planning, "Decisions"), 0o755); err != nil {
		t.Fatal(err)
	}

	parentEntries := []map[string]any{
		forkRulesDecision("D-0001", "accepted", "parent live decision", parentScope),
	}
	forkRulesWriteLedger(t, planning, "Decisions/decisions.md", "active", parentEntries, nil)
	forkRulesWriteLedger(t, planning, "Decisions/archive-2025.md", "archived", []map[string]any{
		forkRulesDecision("D-0002", "rejected", "parent historical decision", nil),
	}, nil)
	parentLocator := decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/decisions.md", Archives: []string{"Decisions/archive-*.md"}}
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: repository, Planning: planning}, forkRulesParent, parentLocator)
	if err != nil {
		t.Fatalf("persisted parent fixture is invalid: %v", err)
	}
	binding, err := decisionview.BindCollection("parent-binding", forkRulesSourceOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	metadata := decisionview.ForkMetadata{
		Version:         decisionview.Version1,
		LedgerID:        forkRulesLocal,
		RepositoryID:    forkRulesOwner,
		Archives:        []string{"Decisions/fork-archive-*.md"},
		ParentBindingID: binding.ID,
		Bindings:        []decisionview.Binding{binding},
		LegacyContexts: []decisionview.LegacyContext{{
			Root: decisionview.SourceRootPlanning, Path: "Research/legacy.md", Namespace: forkRulesParent, LocalIDs: []string{"D-0001", "D-0002"},
		}},
	}
	forkRulesWriteLedger(t, planning, "Decisions/fork.md", "active", []map[string]any{
		forkRulesDecision("D-0001", "accepted", "local live decision", localScope),
		forkRulesDecision("D-0002", "accepted", "local decision sharing a historical parent ID", nil),
	}, metadata)
	forkRulesWriteLedger(t, planning, "Decisions/fork-archive-2025.md", "archived", []map[string]any{
		forkRulesDecision("D-0003", "rejected", "local historical decision", nil),
	}, nil)

	config := map[string]any{
		"planningRoot": ".plans",
		"repositoryId": forkRulesOwner,
		"decisionLog":  map[string]any{"version": 1, "mode": "fork", "path": "Decisions/fork.md", "ledgerId": forkRulesLocal},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "planning-config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return forkRulesDiskFixture{
		repository: repository,
		planning:   planning,
		localQ:     "ledger:" + string(forkRulesLocal),
		parentQ:    "ledger:" + string(forkRulesParent),
	}
}

func forkRulesSavePendingBarrier(t *testing.T, repository, planning string, collection decisionview.CollectionID, operation string) {
	t.Helper()
	store, err := decisionview.OpenLocalStore(repository, planning, "rules-barrier-fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	view := &decisionview.ResolvedView{Version: 1, Resolution: decisionview.ResolutionComplete, Records: []decisionview.ResolvedDecision{}, Diagnostics: []decisionview.Diagnostic{}}
	preview, err := decisionview.NewPreviewEnvelope(
		"override", operation, "2026-09-10", json.RawMessage(`{"fixture":true}`),
		[]decisionview.PreviewFileChange{{Root: decisionview.SourceRootPlanning, Path: "Decisions/barrier-probe.md", Before: "before", BeforeExists: true, After: "after"}},
		nil, view, view,
	)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decisionview.NewForkJournal("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", []decisionview.CollectionID{collection}, preview)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveForkJournal(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
}

func forkRulesSetPlanningRoot(t *testing.T, repository, planning string) {
	t.Helper()
	path := filepath.Join(repository, "planning-config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["planningRoot"] = planning
	raw, _ = json.Marshal(config)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func forkRulesWriteOtherSelection(t *testing.T, repository, planning string) {
	t.Helper()
	config := map[string]any{
		"planningRoot": planning,
		"repositoryId": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"decisionLog":  map[string]any{"version": 1, "mode": "fork", "path": "Decisions/fork.md", "ledgerId": forkRulesLocal},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "planning-config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func forkRulesDecision(id, status, statement string, scope []string) map[string]any {
	if scope == nil {
		scope = []string{}
	}
	return map[string]any{
		"id": id, "kind": "decision", "status": status, "date": "2026-09-10", "decided_by": "user",
		"statement": statement, "rationale": "persisted rules integration fixture", "scope": scope,
		"rejected": []string{}, "tags": []string{}, "reversibility": "two-way",
	}
}

func forkRulesWriteLedger(t *testing.T, root, rel, status string, entries []map[string]any, fork any) {
	t.Helper()
	meta := map[string]any{
		"title": "Fork rules fixture", "type": "decision-log", "status": status,
		"created": "2026-09-10", "updated": "2026-09-10", "tags": []string{}, "related": []string{}, "decisions": entries,
	}
	if fork != nil {
		meta["fork"] = fork
	}
	raw, err := yaml.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(append([]byte("---\n"), raw...), []byte("---\n\n# Fork rules fixture\n")...)
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeForkRulesResearch(t *testing.T, root, rel, context, status string) {
	t.Helper()
	source := strings.Replace(validResearch, "status: draft", "status: "+status, 1)
	source = strings.Replace(source, "## Context\n\nText.", "## Context\n\n"+context, 1)
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func forkRulesRun(t *testing.T, f forkRulesDiskFixture) []Diagnostic {
	t.Helper()
	r, err := LoadRootRepo(f.planning, f.repository)
	if err != nil {
		t.Fatal(err)
	}
	return Run(r)
}

func forkRulesByCode(diags []Diagnostic, code string) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func forkRulesPaths(diags []Diagnostic) map[string]bool {
	out := map[string]bool{}
	for _, d := range diags {
		out[d.Path] = true
	}
	return out
}

func forkRulesHas(diags []Diagnostic, code, path, text string) bool {
	for _, d := range diags {
		if d.Code == code && d.Path == path && strings.Contains(d.Message, text) {
			return true
		}
	}
	return false
}

func forkRulesAny(diags []Diagnostic, text string, severity Severity) bool {
	for _, d := range diags {
		if d.Severity == severity && strings.Contains(strings.ToLower(d.Message+" "+d.Correction), strings.ToLower(text)) {
			return true
		}
	}
	return false
}

func forkRulesSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
