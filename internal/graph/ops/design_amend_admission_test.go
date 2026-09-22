package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestAmendFromDesignOnlyReviewWithoutLaneRows(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.SeqCounter = 2
		g.NodeByID("big").Verification = passAt(2)
		g.NodeByID("big").RedSeqs = map[string]int{"test_big": 1}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: \"design changes the contract and proof\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: \"implements the amended design\"\n      gate:\n        type: tests\n        tests:\n          - id: test_revised_big\n            file: t.ext\n            satisfies: [external-format]\n")
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(raw), "lane_results:\n")
	end := strings.Index(string(raw), "findings:\n")
	if start < 0 || end < start {
		t.Fatalf("fixture has no lane block:\n%s", raw)
	}
	withoutLanes := string(raw[:start]) + "lane_results: []\n" + string(raw[end:])
	if err := os.WriteFile(path, []byte(withoutLanes), 0o644); err != nil {
		t.Fatal(err)
	}

	dry, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, DryRun: true})
	if err != nil {
		t.Fatalf("zero-lane design amendment dry run: %v", err)
	}
	if dry.Applied || dry.ExpectDigest == "" || dry.ExpectReportDigest == "" {
		t.Fatalf("dry run must return both publication fences without applying: %+v", dry)
	}

	out, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel,
		ExpectDigest: dry.ExpectDigest, ExpectReportDigest: dry.ExpectReportDigest})
	if err != nil {
		t.Fatalf("apply zero-lane design amendment: %v", err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	big := g.NodeByID("big")
	if !out.Applied || big.ContractRev != 2 || big.Contract != "implements the amended design" || len(big.RedSeqs) != 0 {
		t.Fatalf("amendment must advance the contract and clear proof for the changed test: out=%+v big=%+v", out, big)
	}
	if g.NodeByID("feature-gate").Verification != nil {
		t.Fatalf("an Amend artifact must not create review verification: %+v", g.NodeByID("feature-gate").Verification)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel,
		ExpectDigest: out.NewDigest, ExpectReportDigest: dry.ExpectReportDigest}); err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Fatalf("one design review artifact may be consumed only once: %v", err)
	}
}
