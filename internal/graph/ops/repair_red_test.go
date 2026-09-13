package ops

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// TestRepairRedRecoversFromRecordedPreimage: a node whose revise cleared an
// unchanged hazard test's red (the pre-fix bug) is repaired using the
// amendment's own recorded pre-image, and does not fabricate a red for a
// test that changed.
func TestRepairRedRecoversFromRecordedPreimage(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		big := g.NodeByID("big")
		// Simulate the pre-fix bug: the revise already landed, clearing
		// every red_seqs entry, but recorded its pre-image (as this fix's
		// live revise path now always does).
		big.Gate.Tests = []model.Test{
			{ID: "test_big", File: "t.ext", Satisfies: []string{"external-format"}},
			{ID: "test_changed", File: "new.ext", Satisfies: []string{"external-format"}},
		}
		big.ContractRev = 2
		big.RedSeqs = nil
		g.SeqCounter = 5
		g.Amendments = append(g.Amendments, model.AmendmentRecord{
			Seq: 5, Review: "feature-gate", Artifact: "Plans/SamplePlan/reviews/x.md",
			ReportDigest: "sha256:aaaa", Revised: []string{"big"},
			PreimageTests: map[string][]model.Test{
				"big": {
					{ID: "test_big", File: "t.ext", Satisfies: []string{"external-format"}},
					{ID: "test_changed", File: "old.ext", Satisfies: []string{"external-format"}},
				},
			},
			PreimageRedSeqs: map[string]map[string]int{
				"big": {"test_big": 3, "test_changed": 3},
			},
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res, err := RepairRed(root, root, "SamplePlan", "", false)
	if err != nil {
		t.Fatalf("repair-red: %v", err)
	}
	if len(res.Changes) != 1 || res.Changes[0].Node != "big" || res.Changes[0].Test != "test_big" || res.Changes[0].Seq != 3 {
		t.Fatalf("changes: %+v", res.Changes)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	big := g.NodeByID("big")
	if seq, ok := big.RedSeqs["test_big"]; !ok || seq != 3 {
		t.Fatalf("unchanged test must be repaired: %+v", big.RedSeqs)
	}
	if _, ok := big.RedSeqs["test_changed"]; ok {
		t.Fatalf("changed test must not be repaired: %+v", big.RedSeqs)
	}

	// Idempotent: re-running finds nothing left to repair.
	again, err := RepairRed(root, root, "SamplePlan", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != 0 {
		t.Fatalf("repair-red must be idempotent: %+v", again.Changes)
	}
}

// TestRepairRedLeavesUnrepairableNodeAlone: a node with no recorded
// preimage (amended before this fix existed) is left untouched, not
// guessed at.
func TestRepairRedLeavesUnrepairableNodeAlone(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		big := g.NodeByID("big")
		big.ContractRev = 2
		big.RedSeqs = nil
		g.SeqCounter = 5
		g.Amendments = append(g.Amendments, model.AmendmentRecord{
			Seq: 5, Review: "feature-gate", Artifact: "Plans/SamplePlan/reviews/x.md",
			ReportDigest: "sha256:aaaa", Revised: []string{"big"},
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res, err := RepairRed(root, root, "SamplePlan", "", false)
	if err != nil {
		t.Fatalf("repair-red: %v", err)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("a node with no recorded preimage must not be guessed at: %+v", res.Changes)
	}
}

// TestRepairRedDryRunLeavesGraphUnchanged mirrors repair-intent's dry-run
// contract for repair-red.
func TestRepairRedDryRunLeavesGraphUnchanged(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		big := g.NodeByID("big")
		big.ContractRev = 2
		big.RedSeqs = nil
		g.SeqCounter = 5
		g.Amendments = append(g.Amendments, model.AmendmentRecord{
			Seq: 5, Review: "feature-gate", Artifact: "Plans/SamplePlan/reviews/x.md",
			ReportDigest: "sha256:aaaa", Revised: []string{"big"},
			PreimageTests: map[string][]model.Test{
				"big": {{ID: "test_big", File: "t.ext", Satisfies: []string{"external-format"}}},
			},
			PreimageRedSeqs: map[string]map[string]int{"big": {"test_big": 3}},
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res, err := RepairRed(root, root, "SamplePlan", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || len(res.Changes) != 1 {
		t.Fatalf("dry run must still report the plan: %+v", res)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if len(g.NodeByID("big").RedSeqs) != 0 {
		t.Fatal("dry run must not write the graph")
	}
}
