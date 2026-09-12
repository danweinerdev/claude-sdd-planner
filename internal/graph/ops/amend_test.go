package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	greview "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// frozenReview writes a resolved, frozen, Aligned review of the sample
// plan carrying the given findings block.
func frozenReview(t *testing.T, root, findings string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\ntitle: \"Gate review\"\ntype: review\nstatus: resolved\nreview_of: \"Plans/SamplePlan/README.md\"\nfrozen: true\nverdict: Aligned\nreview_mode: single-agent\nlane_results:\n")
	for _, lane := range model.ReviewLanes {
		b.WriteString("  - lane: " + lane + "\n    result: PASS/Aligned\n    evidence: \"looked\"\n")
	}
	b.WriteString("findings:\n" + findings + "---\n\n# Gate review\n\nBody.\n")
	rel := "Plans/SamplePlan/reviews/01-sample-review.md"
	path := filepath.Join(root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func passAt(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean, ContractRev: 1}
}

func TestAmendReviseAdvancesRevisionAndResetsProof(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.SeqCounter = 2
		big := g.NodeByID("big")
		big.Verification = passAt(2)
		big.RedSeqs = map[string]int{"test_big": 1}
		g.NodeByID("helper").Verification = passAt(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: \"big ignores archived rows\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: \"does too much, and excludes archived rows\"\n")

	// Preview through the review recorder: nothing written, digest returned.
	res, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	if res.Plan == nil || res.ExpectDigest == "" {
		t.Fatalf("expected a preview: %+v", res)
	}

	// Wrong fence refuses, writes nothing.
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: "0000000000000000"}); err == nil || !strings.Contains(err.Error(), "changed since the preview") {
		t.Fatalf("stale digest must refuse: %v", err)
	}
	// Dry run writes nothing.
	before, _ := os.ReadFile(gstore.PathFor(planDir))
	dry, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, DryRun: true})
	if err != nil || dry.Applied {
		t.Fatalf("dry run: %v %+v", err, dry)
	}
	if after, _ := os.ReadFile(gstore.PathFor(planDir)); string(after) != string(before) {
		t.Fatal("dry run wrote the graph")
	}

	out, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest})
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	if !out.Applied || out.Seq != 3 {
		t.Fatalf("result = %+v", out)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	big := g.NodeByID("big")
	if big.ContractRev != 2 || big.Contract != "does too much, and excludes archived rows" || big.RedSeqs != nil {
		t.Fatalf("revise did not advance/reset: rev=%d red=%v contract=%q", big.ContractRev, big.RedSeqs, big.Contract)
	}
	if big.Verification == nil || big.Verification.Result != model.ResultPass {
		t.Fatal("history is kept: the old pass stays recorded")
	}
	if len(g.Amendments) != 1 || g.Amendments[0].Revised[0] != "big" || g.Amendments[0].Seq != 3 {
		t.Fatalf("amendment register = %+v", g.Amendments)
	}
	st := states.Derive(states.Inputs{Graph: g})
	if st["big"].State != states.Ready || !st["big"].RevIncompatible {
		t.Fatalf("revised node's old pass is not current proof: %+v", st["big"])
	}
	if st["helper"].State != states.Green {
		t.Fatalf("unrelated node untouched: %+v", st["helper"])
	}
	// The same artifact cannot amend twice.
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: out.NewDigest}); err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Fatalf("second application must refuse: %v", err)
	}
}

func TestAmendExtendAddsSourcedNodeAndGrowsReviewDeps(t *testing.T) {
	root, planDir := fixtureRoot(t)
	rel := frozenReview(t, root, "  - id: F-02\n    severity: minor\n    title: \"no audit event on refusal\"\n    status: open\n    action: extend\n    node:\n      id: big-audit\n      contract: \"emits an audit event when big refuses\"\n      deps: [big]\n      gate:\n        type: tests\n        tests:\n          - id: test_big_audit\n            file: t.ext\n      hazards: []\n")
	res, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	out, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest})
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	if !out.Applied {
		t.Fatal("not applied")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	n := g.NodeByID("big-audit")
	if n == nil {
		t.Fatal("extend node missing")
	}
	wantCite := "Plans/SamplePlan/reviews/01-sample-review:F-02"
	if n.Origin == nil || n.Origin.Finding != "F-02" || n.ContractRev != 1 || !containsString(n.Justifies, wantCite) {
		t.Fatalf("extend node = %+v origin=%+v", n, n.Origin)
	}
	if n.IntentHashes[wantCite] == "" {
		t.Fatalf("finding citation must be fingerprinted: %+v", n.IntentHashes)
	}
	gate := g.NodeByID("feature-gate")
	if !containsString(gate.Deps, "big-audit") {
		t.Fatalf("review node deps must grow: %v", gate.Deps)
	}
	st := states.Derive(states.Inputs{Graph: g})
	if st["big-audit"].State != states.Blocked || st["feature-gate"].State != states.Blocked {
		t.Fatalf("new node blocked on big, review blocked on new node: %+v %+v", st["big-audit"], st["feature-gate"])
	}
}

func TestAmendRefusesUnclassifiedNoOpAndOutOfScope(t *testing.T) {
	root, _ := fixtureRoot(t)
	cases := map[string]string{
		"no action":                "  - id: F-01\n    severity: major\n    title: \"x\"\n    status: open\n    nodes: [big]\n",
		"no-op revise":             "  - id: F-01\n    severity: major\n    title: \"x\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: \"does too much\"\n",
		"out of scope":             "  - id: F-01\n    severity: major\n    title: \"x\"\n    status: open\n    action: revise\n    nodes: [helper-of-nobody]\n    revise:\n      contract: \"y\"\n",
		"extend off-scope":         "  - id: F-01\n    severity: major\n    title: \"x\"\n    status: open\n    action: extend\n    node:\n      id: stray\n      contract: \"c\"\n      deps: []\n      gate: {type: tests, tests: [{id: t, file: t.ext}]}\n      hazards: []\n",
		"revise drops hazard test": "  - id: F-01\n    severity: major\n    title: \"x\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      gate: {type: tests, tests: [{id: test_other, file: t.ext}]}\n",
	}
	for name, block := range cases {
		rel := frozenReview(t, root, block)
		_, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, DryRun: true})
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
