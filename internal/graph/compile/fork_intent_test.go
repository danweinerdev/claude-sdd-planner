package compile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"gopkg.in/yaml.v3"
)

const (
	forkIntentOwner  = decisionview.OwnerID("51515151-5151-5151-5151-515151515151")
	forkIntentParent = decisionview.CollectionID("61616161-6161-6161-6161-616161616161")
	forkIntentLocal  = decisionview.CollectionID("71717171-7171-7171-7171-717171717171")
)

// TestForkGraphIntentIndependentSets is deliberately a production-path test:
// every case persists selection, collection metadata, and graph/proposal bytes
// before calling Run or NewSources. It does not manufacture a decision status
// map, because doing so would miss the fork context graph consumers must honor.
func TestForkGraphIntentIndependentSets(t *testing.T) {
	t.Run("qualified equal IDs and explicit legacy context", func(t *testing.T) {
		root := forkIntentFixture(t, true)
		planDir := filepath.Join(root, ".plans", "Plans", "Demo")
		existing := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{
			{ID: "legacy", Contract: "retains inherited citation", Justifies: []string{"D-0001"}, Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "legacy", File: "legacy_test.go"}}}, Hazards: model.Hazards{}, Estimate: 1},
			{ID: "legacy-review", Contract: "reviews inherited citation", Justifies: []string{forkIntentQualified(forkIntentLocal, "D-0001")}, Deps: []string{"legacy"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1},
		}}
		if err := gstore.Save(gstore.PathFor(planDir), existing); err != nil {
			t.Fatal(err)
		}
		sources, err := NewSources(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		if findings := sources.Validate(existing); len(findings) != 0 {
			t.Fatalf("captured legacy citation in committed graph must remain valid: %+v", findings)
		}
		stageForkIntent(t, root, []string{
			forkIntentQualified(forkIntentParent, "D-0001"),
			forkIntentQualified(forkIntentLocal, "D-0001"),
		})
		result, findings, err := Run(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("qualified and captured historical identities must compile independently: %+v", findings)
		}
		if result == nil || len(result.Added) == 0 {
			t.Fatal("compile did not persist the fork-aware proposal")
		}
	})

	t.Run("captured inventory does not license new bare proposal", func(t *testing.T) {
		root := forkIntentFixture(t, true)
		stageForkIntent(t, root, []string{"D-0001"})
		_, findings, err := Run(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		if !forkIntentFinding(findings, "must be qualified") {
			t.Fatalf("new bare citation must receive qualification-required refusal: %+v", findings)
		}
	})

	t.Run("new ambiguous bare reference is qualification refusal", func(t *testing.T) {
		root := forkIntentFixture(t, false)
		// Research/new.md has no captured legacy inventory. Classifying the same
		// bare ID from its context must not pick local or parent by ordering.
		sources, err := NewSources(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		got := sources.ClassifyCitation("D-0001")
		if got.Kind != CitationAmbiguous || len(got.Suggestions) != 2 {
			t.Fatalf("new equal bare decision ID = kind %v suggestions %v, want ambiguous qualified alternatives", got.Kind, got.Suggestions)
		}
	})

	t.Run("unresolved ancestry refuses source snapshot", func(t *testing.T) {
		root := forkIntentFixture(t, true)
		parent := filepath.Join(root, ".plans", "Decisions", "parent.md")
		raw, err := os.ReadFile(parent)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(parent, []byte(strings.Replace(string(raw), "parent original", "unapproved parent mutation", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewSources(filepath.Join(root, ".plans"), root, "Demo"); err == nil {
			t.Fatal("stale ancestor authority must refuse graph reliance, not return a partial exemption set")
		}
	})

	t.Run("pending barrier refuses source snapshot", func(t *testing.T) {
		root := forkIntentFixture(t, true)
		store, err := decisionview.OpenLocalStore(root, filepath.Join(root, ".plans"), "fork-intent-test")
		if err != nil {
			t.Fatal(err)
		}
		view := &decisionview.ResolvedView{Version: 1, Resolution: decisionview.ResolutionComplete}
		preview, err := decisionview.NewPreviewEnvelope("override", "pending-graph-intent", "2026-09-10", json.RawMessage(`{"fixture":true}`), []decisionview.PreviewFileChange{{Root: decisionview.SourceRootPlanning, Path: "Decisions/probe.md", Before: "before", BeforeExists: true, After: "after"}}, nil, view, view)
		if err != nil {
			t.Fatal(err)
		}
		journal, err := decisionview.NewForkJournal("81818181-8181-8181-8181-818181818181", []decisionview.CollectionID{forkIntentLocal}, preview)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveForkJournal(context.Background(), journal); err != nil {
			t.Fatal(err)
		}
		_ = store.Close()
		if _, err := NewSources(filepath.Join(root, ".plans"), root, "Demo"); err == nil {
			t.Fatal("pending authority barrier must refuse graph reliance")
		}
	})

	t.Run("actual operational source failure remains distinct", func(t *testing.T) {
		root := forkIntentFixture(t, true)
		if err := os.Remove(filepath.Join(root, ".plans", "Decisions", "parent.md")); err != nil {
			t.Fatal(err)
		}
		_, err := NewSources(filepath.Join(root, ".plans"), root, "Demo")
		var authority *AuthorityError
		if !errors.As(err, &authority) || !authority.Operational || len(authority.Diagnostics) == 0 {
			t.Fatalf("missing ancestor must retain complete operational authority diagnostics: %T %v", err, err)
		}
	})

	t.Run("restored override cannot justify new reliance", func(t *testing.T) {
		root := forkIntentFixture(t, true, true)
		stageForkIntent(t, root, []string{forkIntentQualified(forkIntentLocal, "D-0001")})
		_, findings, err := Run(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		if !forkIntentFinding(findings, "no current binding effective decision") {
			t.Fatalf("accepted inactive override must be refused before it can become permanently stale: %+v", findings)
		}
	})

	t.Run("candidate diagnostics do not gate graph reads", func(t *testing.T) {
		root := forkIntentFixture(t, true)
		capture := decisionview.CaptureForRepository(root)
		candidate := false
		for _, diagnostic := range capture.Diagnostics {
			candidate = candidate || diagnostic.Severity == decisionview.Candidate
		}
		if !candidate {
			t.Fatal("fixture did not produce its candidate-only authority diagnostic")
		}
		if _, err := NewSources(filepath.Join(root, ".plans"), root, "Demo"); err != nil {
			t.Fatalf("candidate-only authority must remain non-gating: %v", err)
		}
	})

	t.Run("semantic findings distinguish nonaccepted and ambiguous decisions", func(t *testing.T) {
		root := forkIntentFixture(t, false)
		stageForkIntent(t, root, []string{forkIntentQualified(forkIntentLocal, "D-0009"), "D-0001"})
		_, findings, err := Run(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		if !forkIntentFinding(findings, `status "rejected"`) || !forkIntentFinding(findings, "must be qualified") {
			t.Fatalf("semantic findings lost nonaccepted/qualification distinctions: %+v", findings)
		}
	})

	t.Run("source digest refresh preserves citation spelling", func(t *testing.T) {
		root := forkIntentFixture(t, true)
		writeForkIntent(t, root, ".plans/Specs/Intent/README.md", []byte(forkIntentSpec("first requirement text")))
		writeForkIntent(t, root, ".plans/Plans/Demo/README.md", []byte(forkIntentPlan("[Specs/Intent]")))
		before, err := NewSources(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		n := model.Node{Justifies: []string{"FR-01"}}
		before.Anchor(&n)
		oldHash := n.IntentHashes["FR-01"]
		writeForkIntent(t, root, ".plans/Specs/Intent/README.md", []byte(forkIntentSpec("refreshed requirement text")))
		after, err := NewSources(filepath.Join(root, ".plans"), root, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		snap := after.IntentSnapshot()
		if snap.Items["FR-01"].Hash == oldHash || n.Justifies[0] != "FR-01" || n.IntentHashes["FR-01"] != oldHash {
			t.Fatalf("refresh must expose a new digest without rewriting stored citations/hashes: node=%+v snapshot=%+v", n, snap.Items)
		}
	})
}

func forkIntentFixture(t *testing.T, legacy bool, restored ...bool) string {
	t.Helper()
	root := t.TempDir()
	planning := filepath.Join(root, ".plans")
	parentEntries := []map[string]any{forkIntentEntry("D-0001", "accepted", "parent original")}
	forkIntentLedger(t, planning, "Decisions/parent.md", parentEntries, nil)
	parent, err := decisionview.LoadCollection(decisionview.Roots{Repository: root, Planning: planning}, forkIntentParent, decisionview.SourceLocator{Root: decisionview.SourceRootPlanning, Path: "Decisions/parent.md"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := decisionview.BindCollection("parent-binding", forkIntentOwner, parent)
	if err != nil {
		t.Fatal(err)
	}
	meta := decisionview.ForkMetadata{Version: 1, LedgerID: forkIntentLocal, RepositoryID: forkIntentOwner, ParentBindingID: binding.ID, Bindings: []decisionview.Binding{binding}}
	if legacy {
		meta.LegacyContexts = []decisionview.LegacyContext{{Root: decisionview.SourceRootPlanning, Path: "Plans/Demo/README.md", Namespace: forkIntentParent, LocalIDs: []string{"D-0001"}}}
	}
	target := decisionview.QualifiedID(forkIntentQualified(forkIntentParent, "D-0001"))
	basis, err := decisionview.CreateBasis(binding.ID, parent, target, []string{binding.ID})
	if err != nil {
		t.Fatal(err)
	}
	replacement := forkIntentEntry("D-0001", "accepted", "local effective replacement")
	replacement["question"] = "Which fixture authority applies?"
	relation, err := json.Marshal(decisionview.OverrideDeclaration{Target: target, Basis: basis})
	if err != nil {
		t.Fatal(err)
	}
	var override map[string]any
	if err := json.Unmarshal(relation, &override); err != nil {
		t.Fatal(err)
	}
	replacement["override"] = override
	competing := forkIntentEntry("D-0002", "accepted", "different candidate answer")
	competing["question"] = "Which fixture authority applies?"
	rejected := forkIntentEntry("D-0009", "rejected", "rejected history")
	rejected["decided_by"] = "user"
	if len(restored) > 0 && restored[0] {
		localID := decisionview.QualifiedID(forkIntentQualified(forkIntentLocal, "D-0001"))
		meta.Events = []decisionview.AuthorityEvent{{Version: 1, ID: "restore-1", Kind: decisionview.EventRestore, Date: "2026-09-10", DecidedBy: "user-approved", Target: &localID}}
	}
	forkIntentLedger(t, planning, "Decisions/fork.md", []map[string]any{replacement, competing, rejected}, meta)
	config, _ := json.Marshal(map[string]any{"planningRoot": ".plans", "repositoryId": forkIntentOwner, "decisionLog": map[string]any{"version": 1, "mode": "fork", "path": "Decisions/fork.md", "ledgerId": forkIntentLocal}})
	writeForkIntent(t, root, "planning-config.json", config)
	writeForkIntent(t, root, ".plans/Plans/Demo/README.md", []byte(forkIntentPlan("[]")))
	if _, err := gstore.Init(filepath.Join(planning, "Plans", "Demo")); err != nil {
		t.Fatal(err)
	}
	return root
}

func stageForkIntent(t *testing.T, root string, citations []string) {
	t.Helper()
	planDir := filepath.Join(root, ".plans", "Plans", "Demo")
	p := model.Proposal{Version: model.SchemaVersion}
	var deps []string
	for i, citation := range citations {
		id := "decision-" + string(rune('a'+i))
		deps = append(deps, id)
		p.Nodes = append(p.Nodes, model.Node{ID: id, Contract: "retains citation identity", Justifies: []string{citation}, Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_decision", File: "decision_test.go"}}}, Hazards: model.Hazards{}, Estimate: 1})
	}
	p.Nodes = append(p.Nodes, model.Node{ID: "review", Contract: "reviews decision identity", Justifies: []string{forkIntentQualified(forkIntentLocal, "D-0001")}, Deps: deps, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1})
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposal.Stage(planDir, raw); err != nil {
		t.Fatal(err)
	}
}

func forkIntentQualified(collection decisionview.CollectionID, id string) string {
	return "ledger:" + string(collection) + ":" + id
}

func forkIntentFinding(findings []Finding, text string) bool {
	for _, finding := range findings {
		if strings.Contains(finding.Msg, text) {
			return true
		}
	}
	return false
}

func forkIntentEntry(id, status, statement string) map[string]any {
	return map[string]any{"id": id, "kind": "decision", "status": status, "date": "2026-09-10", "decided_by": "user-approved", "statement": statement, "rationale": "fixture", "confirmation": "fixture", "scope": []string{}, "rejected": []string{}, "tags": []string{}, "reversibility": "two-way"}
}

func forkIntentLedger(t *testing.T, root, rel string, entries []map[string]any, metadata any) {
	t.Helper()
	fm := map[string]any{"title": "Fork intent", "type": "decision-log", "status": "active", "created": "2026-09-10", "updated": "2026-09-10", "tags": []string{}, "related": []string{}, "decisions": entries}
	if metadata != nil {
		fm["fork"] = metadata
	}
	raw, err := yaml.Marshal(fm)
	if err != nil {
		t.Fatal(err)
	}
	forkIntentWrite(t, root, rel, append(append([]byte("---\n"), raw...), []byte("---\n\n# Decisions\n")...))
}

func writeForkIntent(t *testing.T, root, rel string, raw []byte) { forkIntentWrite(t, root, rel, raw) }
func forkIntentWrite(t *testing.T, root, rel string, raw []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func forkIntentPlan(related string) string {
	return "---\ntitle: Demo\ntype: plan\nstatus: draft\ncreated: 2026-09-10\nupdated: 2026-09-10\ntags: []\nrelated: " + related + "\nphases: []\n---\n\n# Demo\n"
}
func forkIntentSpec(text string) string {
	return "---\ntitle: Intent\ntype: spec\nstatus: approved\ncreated: 2026-09-10\nupdated: 2026-09-10\ntags: []\nrelated: []\n---\n\n# Intent\n\n## Functional Requirements\n\n- **FR-01**: " + text + ".\n"
}
