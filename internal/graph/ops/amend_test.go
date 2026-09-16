package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func frozenReview(t *testing.T, root, findings string) string {
	t.Helper()
	var b strings.Builder
	verdict := "Aligned"
	if strings.Contains(findings, "status: open") {
		verdict = "Amend"
	}
	b.WriteString("---\ntitle: \"Gate review\"\ntype: review\nstatus: resolved\nreview_of: \"Plans/SamplePlan/README.md\"\nfrozen: true\nverdict: " + verdict + "\nreview_mode: single-agent\nlane_results:\n")
	for _, lane := range model.ReviewLanes {
		b.WriteString("  - lane: " + lane + "\n    result: PASS/Aligned\n    evidence: \"looked\"\n")
	}
	b.WriteString("findings:\n" + findings + "---\n\n# Gate review\n\nBody.\n")
	rel := "Plans/SamplePlan/reviews/01-sample-review.md"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func passAt(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean, ContractRev: 1}
}

func revisePlan(after *model.Node) *review.Plan {
	return &review.Plan{Review: "feature-gate", Artifact: "reviews/r.md", ReportDigest: "sha256:report", Scope: []string{"big"}, Amendments: []review.Amendment{{Finding: "F-01", Action: review.ActionRevise, Node: "big", After: after}}}
}
func applyRevision(t *testing.T, mutate func(*model.Node)) (*model.Graph, error) {
	t.Helper()
	_, d := fixtureRoot(t)
	g, _ := gstore.Load(gstore.PathFor(d))
	n := g.NodeByID("big")
	after := *n
	mutate(&after)
	out, _, err := applyAmendments(g, revisePlan(&after), "", nil, "")
	return out, err
}
func TestAmendReviseAdvancesRevisionAndResetsProof(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.SeqCounter = 2
		g.RevisionLineage = map[string]string{"1111111111111111111111111111111111111111": "2222222222222222222222222222222222222222"}
		big := g.NodeByID("big")
		big.Verification = passAt(2)
		big.RedSeqs = map[string]int{"test_big": 1}
		g.NodeByID("helper").Verification = passAt(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: \"big ignores archived rows\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: \"does too much, and excludes archived rows\"\n")
	res, err := review.Record(review.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	if res.Plan == nil || res.ExpectDigest == "" || res.Plan.ReportDigest == "" {
		t.Fatalf("expected preview: %+v", res)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: "0000000000000000", ExpectReportDigest: res.Plan.ReportDigest}); err == nil || !strings.Contains(err.Error(), "changed since the preview") {
		t.Fatalf("stale digest must refuse: %v", err)
	}
	before, _ := os.ReadFile(gstore.PathFor(planDir))
	dry, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, DryRun: true})
	if err != nil || dry.Applied {
		t.Fatalf("dry run: %v %+v", err, dry)
	}
	if after, _ := os.ReadFile(gstore.PathFor(planDir)); string(after) != string(before) {
		t.Fatal("dry run wrote the graph")
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest}); err == nil || !strings.Contains(err.Error(), "--expect-report-digest is required") {
		t.Fatalf("artifact fence required: %v", err)
	}
	out, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest, ExpectReportDigest: res.Plan.ReportDigest})
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	if !out.Applied || out.Seq != 3 {
		t.Fatalf("result=%+v", out)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	big := g.NodeByID("big")
	if big.ContractRev != 2 || big.Contract != "does too much, and excludes archived rows" || big.RedSeqs["test_big"] != 1 || big.Verification == nil {
		t.Fatalf("revised node=%+v", big)
	}
	if len(g.Amendments) != 1 || g.Amendments[0].Revised[0] != "big" || g.Amendments[0].Seq != 3 || len(g.Amendments[0].PreimageTests["big"]) == 0 || g.Amendments[0].PreimageRedSeqs["big"]["test_big"] != 1 {
		t.Fatalf("amendment register=%+v", g.Amendments)
	}
	if g.RevisionLineage["1111111111111111111111111111111111111111"] != "2222222222222222222222222222222222222222" {
		t.Fatalf("lineage=%+v", g.RevisionLineage)
	}
	st := states.Derive(states.Inputs{Graph: g})
	if st["big"].State != states.Ready || !st["big"].RevIncompatible || st["helper"].State != states.Green {
		t.Fatalf("states=%+v", st)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: out.NewDigest, ExpectReportDigest: res.Plan.ReportDigest}); err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Fatalf("replay accepted: %v", err)
	}
}
func TestAmendReviseAddingTestCarriesOverUnchangedHazardRed(t *testing.T) {
	_, d := fixtureRoot(t)
	g, _ := gstore.Load(gstore.PathFor(d))
	n := g.NodeByID("big")
	n.RedSeqs = map[string]int{"test_big": 1}
	after := *n
	after.Gate.Tests = append(after.Gate.Tests, model.Test{ID: "extra", File: "t.ext"})
	out, _, err := applyAmendments(g, revisePlan(&after), "", nil, "")
	if err != nil || out.NodeByID("big").RedSeqs["test_big"] != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
func TestAmendReviseChangingHazardTestClearsItsRed(t *testing.T) {
	_, d := fixtureRoot(t)
	g, _ := gstore.Load(gstore.PathFor(d))
	n := g.NodeByID("big")
	n.RedSeqs = map[string]int{"test_big": 1}
	after := *n
	after.Gate.Tests = []model.Test{{ID: "new", File: "t.ext", Satisfies: []string{"external-format"}}}
	out, _, err := applyAmendments(g, revisePlan(&after), "", nil, "")
	if err != nil || len(out.NodeByID("big").RedSeqs) != 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
func TestAmendExtendAddsSourcedNodeAndGrowsReviewDeps(t *testing.T) {
	_, d := fixtureRoot(t)
	g, _ := gstore.Load(gstore.PathFor(d))
	n := model.Node{ID: "new", Contract: "new", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}
	p := &review.Plan{Review: "feature-gate", Artifact: "r", ReportDigest: "sha256:r", Amendments: []review.Amendment{{Action: review.ActionExtend, Node: "new", New: &n}}}
	out, _, err := applyAmendments(g, p, "", nil, "")
	if err != nil || out.NodeByID("new") == nil || !containsString(out.NodeByID("feature-gate").Deps, "new") {
		t.Fatalf("out=%+v err=%v", out, err)
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
func TestAmendRefusesForeignOrInadmissibleReviewArtifacts(t *testing.T) {
	tests := []struct {
		name, replaceOld, replaceNew, want string
	}{
		{"foreign plan", `review_of: "Plans/SamplePlan/README.md"`, `review_of: "Plans/Foreign/README.md"`, "not under Plans/SamplePlan/"},
		{"unaligned verdict", "verdict: Amend", "verdict: Misaligned", "verdict is"},
		{"missing lane", "  - lane: review_quality\n    result: PASS/Aligned\n    evidence: \"looked\"\n", "", "review_quality is absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, planDir := fixtureRoot(t)
			rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: bad evidence\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: replacement\n")
			path := filepath.Join(root, filepath.FromSlash(rel))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(raw), tc.replaceOld, tc.replaceNew, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			d, err := gstore.Digest(gstore.PathFor(planDir))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: d}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("inadmissible artifact must refuse with %q: %v", tc.want, err)
			}
		})
	}
}
func TestAmendArtifactDigestFenceRejectsSubstitution(t *testing.T) {
	root, planDir := fixtureRoot(t)
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: original finding\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: original replacement\n")
	preview, err := review.Record(review.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: preview.ExpectDigest, ExpectReportDigest: "sha256:not-the-preview"}); err == nil || !strings.Contains(err.Error(), "review artifact changed since the preview") {
		t.Fatalf("mismatched report digest accepted: %v", err)
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "original replacement", "substituted replacement", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: preview.ExpectDigest, ExpectReportDigest: preview.Plan.ReportDigest}); err == nil || !strings.Contains(err.Error(), "review artifact changed since the preview") {
		t.Fatalf("substituted report accepted: %v", err)
	}
	after, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("substitution changed graph")
	}
}
func TestAmendRejectsArtifactAlreadyUsedAsEvidence(t *testing.T) {
	root, planDir := fixtureRoot(t)
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: duplicate evidence\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: duplicate replacement\n")
	artifact, err := review.ReadArtifact(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("feature-gate").Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean, ReportDigest: artifact.ReportDigest}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, DryRun: true}); err == nil || !strings.Contains(err.Error(), "already recorded on gate") {
		t.Fatalf("recorded evidence reused: %v", err)
	}
}
func TestAmendExtendInvalidatesAlreadyGreenReview(t *testing.T) {
	_, d := fixtureRoot(t)
	g, _ := gstore.Load(gstore.PathFor(d))
	g.NodeByID("feature-gate").Verification = &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean, Reviewed: map[string]model.ReviewedRef{"big": {ContractRev: 1}, "helper": {ContractRev: 1}}}
	n := model.Node{ID: "new", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1}
	p := &review.Plan{Review: "feature-gate", Amendments: []review.Amendment{{Action: review.ActionExtend, New: &n, Node: "new"}}}
	out, _, err := applyAmendments(g, p, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if states.Derive(states.Inputs{Graph: out})["feature-gate"].State != states.Stale {
		t.Fatal("review remained green")
	}
}
func TestAmendMaterializesEmptyLegacyScopeAcrossEncoding(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		{ID: "work", Contract: "w", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1, Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}},
		{ID: "review", Contract: "r", Deps: []string{"work"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1, Verification: &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}},
	}}
	newNode := model.Node{ID: "new", Contract: "n", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}
	p := &review.Plan{Review: "review", Scope: []string{"work"}, Amendments: []review.Amendment{{Action: review.ActionExtend, Node: "new", New: &newNode}}}
	out, _, err := applyAmendments(g, p, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.NodeByID("review").Verification.Reviewed == nil || out.NodeByID("review").Verification.Reviewed["work"].ContractRev != 1 {
		t.Fatalf("legacy scope was not materialized: %+v", out.NodeByID("review").Verification)
	}
}
func TestAmendExtendInvalidatesLegacyReviewWithoutChangingContractRevision(t *testing.T) {
	_, d := fixtureRoot(t)
	g, _ := gstore.Load(gstore.PathFor(d))
	r := g.NodeByID("feature-gate")
	r.Verification = &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}
	newNode := model.Node{ID: "new", Contract: "n", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}
	p := &review.Plan{Review: "feature-gate", Scope: []string{"big", "helper"}, Amendments: []review.Amendment{{Action: review.ActionExtend, Node: "new", New: &newNode}}}
	out, _, err := applyAmendments(g, p, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	gate := out.NodeByID("feature-gate")
	if gate.EffectiveContractRev() != gate.Verification.EffectiveContractRev() || states.Derive(states.Inputs{Graph: out})["feature-gate"].State != states.Stale {
		t.Fatalf("legacy review extension did not stale structurally: %+v", gate)
	}
}
func TestAmendRefusesUnsafePlanNameBeforePathResolution(t *testing.T) {
	if _, err := AmendFromReview(AmendOptions{Plan: "../bad"}); err == nil {
		t.Fatal("unsafe plan accepted")
	}
}
func TestAmendReviseInvalidatesLegacyReview(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.SeqCounter = 3
		g.NodeByID("helper").Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
		g.NodeByID("big").Verification = &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}
		g.NodeByID("feature-gate").Verification = &model.Verification{Result: model.ResultPass, Seq: 3, Isolation: model.IsolationClean}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if st := states.Derive(states.Inputs{Graph: g}); st["feature-gate"].State != states.Green {
		t.Fatalf("precondition: %+v", st["feature-gate"])
	}
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: \"big is wrong\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: \"does too much, revised\"\n")
	res, err := review.Record(review.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest, ExpectReportDigest: res.Plan.ReportDigest}); err != nil {
		t.Fatal(err)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	st := states.Derive(states.Inputs{Graph: g})
	if st["feature-gate"].State != states.Stale || !containsString(st["feature-gate"].ReviewStale, "big") {
		t.Fatalf("legacy review must stale naming big: %+v", st["feature-gate"])
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("feature-gate").Verification.Reviewed = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	if st := states.Derive(states.Inputs{Graph: g}); st["feature-gate"].State != states.Stale {
		t.Fatalf("legacy review over revised scope is proof: %+v", st["feature-gate"])
	}
}

func widenedFullGate(id string, deps []string) model.Node {
	return model.Node{ID: id, Contract: id + " survives review", Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1, Deps: deps}
}

func widenedPass(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean}
}

func TestWidenedReachNamesInnerGate(t *testing.T) {
	implNode := model.Node{ID: "impl", Contract: "does impl", Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_impl", File: "t.ext"}}}, Hazards: model.Hazards{}, Estimate: 1, Artifacts: []string{"src/impl.ext"}, Verification: widenedPass(1)}
	inner := widenedFullGate("inner-review", []string{"impl"})
	inner.Verification = widenedPass(2)
	fullGateCmd := model.Node{ID: "full-gate", Contract: "runs the full gate", Deps: []string{"inner-review"}, Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}
	outer := widenedFullGate("review-final", []string{"full-gate"})
	g := &model.Graph{Version: model.SchemaVersion, SeqCounter: 3, Nodes: []model.Node{implNode, inner, fullGateCmd, outer}}
	scope, err := review.Scope(g, "review-final")
	if err != nil {
		t.Fatal(err)
	}
	if len(scope) != 1 || scope[0] != "full-gate" {
		t.Fatalf("scope=%v", scope)
	}
	plan := &review.Plan{Review: "review-final", Scope: scope, Amendments: []review.Amendment{{Finding: "F-01", Action: review.ActionRevise, Node: "impl"}}}
	widened := widenedReach(g, "review-final", plan)
	if len(widened) != 1 || widened[0].Node != "impl" || len(widened[0].Gates) != 1 || widened[0].Gates[0] != "inner-review" {
		t.Fatalf("widened=%+v", widened)
	}
	planInScope := &review.Plan{Review: "review-final", Scope: scope, Amendments: []review.Amendment{{Finding: "F-02", Action: review.ActionRevise, Node: "full-gate"}}}
	if got := widenedReach(g, "review-final", planInScope); len(got) != 0 {
		t.Fatalf("in-scope widened=%+v", got)
	}
	planExtend := &review.Plan{Review: "review-final", Scope: scope, Amendments: []review.Amendment{{Finding: "F-03", Action: review.ActionExtend, Node: "new", New: &model.Node{ID: "new", Deps: []string{"impl"}}}}}
	if got := widenedReach(g, "review-final", planExtend); len(got) != 1 || got[0].Node != "new" || got[0].Gates[0] != "inner-review" {
		t.Fatalf("extend target widened=%+v", got)
	}
}
