package compile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// TestAuditEmptyGraphIsNonemptyJSON: an empty committed graph still yields a
// complete, serializable report — the audit never returns nothing.
func TestAuditEmptyGraphIsNonemptyJSON(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	rep, err := Audit(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if rep.Plan != "SamplePlan" || rep.Schema != 1 {
		t.Fatalf("report identity: %+v", rep)
	}
	if rep.Counts.Nodes != 0 {
		t.Fatalf("empty graph must report 0 nodes: %+v", rep.Counts)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("report must marshal: %v", err)
	}
	for _, key := range []string{`"ok"`, `"counts"`, `"findings"`, `"coverage"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("report JSON missing %q:\n%s", key, b)
		}
	}
}

// TestAuditCountsCoverageAndTestDiagnostics: a populated graph yields the
// right census, per-source per-family coverage, within-node duplicate tests,
// and cross-node shared tests.
func TestAuditCountsCoverageAndTestDiagnostics(t *testing.T) {
	root := fixtureRoot(t, fixtureSpec)
	planDir := root + "/Plans/SamplePlan"
	sources, err := NewSources(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	nodes := []model.Node{
		{ID: "a", Contract: "fr work", Justifies: []string{"FR-01"},
			Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{
				{ID: "test_x", File: "t.ext"}, {ID: "test_x", File: "t.ext"},
			}}, Hazards: model.Hazards{}, Estimate: 1},
		{ID: "b", Contract: "ac work", Justifies: []string{"AC-01"}, Deps: []string{"a"},
			Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{
				{ID: "test_x", File: "t.ext"},
			}}, Hazards: model.Hazards{}, Estimate: 1},
		{ID: "gate", Contract: "review", Justifies: []string{"AC-01"}, Deps: []string{"b"},
			Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1},
	}
	for i := range nodes {
		sources.Anchor(&nodes[i])
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = nodes
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := Audit(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if rep.Counts.Nodes != 3 || rep.Counts.Tests != 3 {
		t.Fatalf("counts: %+v", rep.Counts)
	}
	if rep.Counts.Gates["tests"] != 2 || rep.Counts.Gates["review"] != 1 {
		t.Fatalf("gate counts: %+v", rep.Counts.Gates)
	}

	// Duplicate test within node a; test_x shared across a and b.
	if len(rep.DuplicateTests) != 1 || rep.DuplicateTests[0].Node != "a" || rep.DuplicateTests[0].TestID != "test_x" {
		t.Fatalf("duplicate tests: %+v", rep.DuplicateTests)
	}
	if len(rep.SharedTests) != 1 || rep.SharedTests[0].TestID != "test_x" || len(rep.SharedTests[0].Nodes) != 2 {
		t.Fatalf("shared tests: %+v", rep.SharedTests)
	}

	// Coverage: FR-01 covered (1/1); AC-01 covered, AC-02 uncovered.
	coverage := map[string]FamilyCoverage{}
	for _, sc := range rep.Coverage {
		if strings.HasSuffix(sc.Source, "Specs/Sample/README.md") || sc.Source == "Specs/Sample/README.md" {
			for _, fc := range sc.Families {
				coverage[fc.Family] = fc
			}
		}
	}
	if fr := coverage["FR"]; fr.Defined != 1 || fr.Covered != 1 {
		t.Fatalf("FR coverage: %+v", coverage["FR"])
	}
	if ac := coverage["AC"]; ac.Defined != 2 || ac.Covered != 1 || !ac.Mandatory {
		t.Fatalf("AC coverage: %+v", coverage["AC"])
	}
	if !strings.Contains(strings.Join(coverage["AC"].Uncovered, ","), "AC-02") {
		t.Fatalf("AC-02 must be uncovered: %+v", coverage["AC"].Uncovered)
	}

	// The mandatory AC-02 gap is a compile finding (the only thing that
	// gates): OK is false and AC-02 is named.
	if rep.OK {
		t.Fatal("an uncovered mandatory AC must set OK=false")
	}
	joined := findingsToString(rep.Findings)
	if !strings.Contains(joined, "AC-02") {
		t.Fatalf("the mandatory AC coverage gap must surface in findings:\n%s", joined)
	}
}
