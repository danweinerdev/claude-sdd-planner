package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkScopedAuthorityPreservation(t *testing.T) {
	f := newForkRulesDiskFixture(t, nil, nil)
	r, err := LoadRootRepo(f.planning, f.repository)
	if err != nil {
		t.Fatal(err)
	}
	if r.DecisionView == nil || r.DecisionView.View == nil {
		t.Fatalf("fixture did not produce persisted shared authority: %+v", r.DecisionView)
	}
	r.DecisionDiagnostics = append(r.DecisionDiagnostics, Diagnostic{
		Code: "FDL999", Severity: Error, Path: "planning-config.json", Message: "shared authority failure",
	})

	for name, scoped := range map[string]*Root{
		"plan": ScopeToPlan(r, "Plans/Current"),
		"doc":  ScopeToDoc(r, "Specs/Current/README.md"),
	} {
		t.Run(name, func(t *testing.T) {
			if scoped.DecisionView != r.DecisionView {
				t.Fatal("scoping dropped or recaptured the persisted DecisionView")
			}
			if !reflect.DeepEqual(scoped.DecisionDiagnostics, r.DecisionDiagnostics) {
				t.Fatalf("scoping dropped shared authority diagnostics: got %+v want %+v", scoped.DecisionDiagnostics, r.DecisionDiagnostics)
			}
			if got := forkRulesByCode(Run(scoped), "FDL999"); len(got) != 1 {
				t.Fatalf("scoped validation did not retain the global authority failure: %+v", got)
			}
		})
	}
}

func TestForkOwnerAwareScopeAndLegacyContext(t *testing.T) {
	t.Run("mapped external plan retains its repository owner", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, []string{"Plans/Mapped"}, nil)
		shared := filepath.Join(filepath.Dir(f.repository), "shared-planning")
		if err := os.Rename(f.planning, shared); err != nil {
			t.Fatal(err)
		}
		f.planning = shared
		forkRulesSetPlanningRoot(t, f.repository, shared)

		planPath := filepath.Join(shared, "Plans", "Mapped", "README.md")
		if err := os.MkdirAll(filepath.Dir(planPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(planPath, []byte(validPlan(false)), 0o644); err != nil {
			t.Fatal(err)
		}
		mapped := filepath.Join(filepath.Dir(f.repository), "mapped-repository")
		if err := os.MkdirAll(mapped, 0o755); err != nil {
			t.Fatal(err)
		}
		foreignOwner := decisionview.OwnerID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
		mappedConfig, _ := json.Marshal(map[string]any{"repositoryId": foreignOwner})
		if err := os.WriteFile(filepath.Join(mapped, "planning-config.json"), mappedConfig, 0o644); err != nil {
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
		config["repositories"] = map[string]any{"mapped": map[string]any{"path": mapped}}
		config["planMapping"] = map[string]any{"Mapped": "mapped"}
		raw, _ = json.Marshal(config)
		if err := os.WriteFile(configPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}

		r, err := LoadRootRepo(shared, f.repository)
		if err != nil {
			t.Fatal(err)
		}
		if !forkRulesHas(Run(r), "FDL030", "Plans/Mapped", "foreign") {
			t.Fatalf("CheckScopes did not reject a consuming decision scoped to a foreign mapped repository: %+v", Run(r))
		}
	})

	t.Run("bare IDs require an explicit path inventory", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, nil, nil)
		writeForkRulesResearch(t, f.planning, "Research/uninventoried.md", "uses uniquely numbered D-0003", "draft")
		r, err := LoadRootRepo(f.planning, f.repository)
		if err != nil {
			t.Fatal(err)
		}
		if r.DecisionView == nil || r.DecisionView.View == nil {
			t.Fatalf("fixture did not compose fork authority: %+v", r.DecisionView)
		}
		if !forkRulesHas(Run(r), "SDD120", "Research/uninventoried.md", "D-0003") {
			t.Fatalf("a unique bare ID resolved without explicit legacy context: %+v", forkRulesByCode(Run(r), "SDD120"))
		}
	})

	t.Run("legacy inventory root must match the citing artifact", func(t *testing.T) {
		f := newForkRulesDiskFixture(t, nil, nil)
		forkPath := filepath.Join(f.planning, "Decisions", "fork.md")
		raw, err := os.ReadFile(forkPath)
		if err != nil {
			t.Fatal(err)
		}
		contextRoot := strings.LastIndex(string(raw), string(decisionview.SourceRootPlanning))
		if contextRoot < 0 {
			t.Fatal("fixture lacks planning-root legacy context")
		}
		raw = append(append(append([]byte(nil), raw[:contextRoot]...), []byte(decisionview.SourceRootRepository)...), raw[contextRoot+len(decisionview.SourceRootPlanning):]...)
		if err := os.WriteFile(forkPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		writeForkRulesResearch(t, f.planning, "Research/legacy.md", "wrong-root context uses D-0002", "draft")
		r, err := LoadRootRepo(f.planning, f.repository)
		if err != nil {
			t.Fatal(err)
		}
		if r.DecisionView == nil || r.DecisionView.View == nil {
			t.Fatalf("fixture did not compose fork authority: %+v", r.DecisionView)
		}
		if !forkRulesHas(Run(r), "SDD120", "Research/legacy.md", "D-0002") {
			t.Fatalf("a repository-root inventory authorized a planning-root citation: %+v", forkRulesByCode(Run(r), "SDD120"))
		}
	})
}
