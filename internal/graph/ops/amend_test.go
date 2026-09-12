package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
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

	// Preview through the review recorder: nothing written, digest returned.
	res, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	if res.Plan == nil || res.ExpectDigest == "" || res.Plan.ReportDigest == "" {
		t.Fatalf("expected a preview: %+v", res)
	}

	// Wrong fence refuses, writes nothing.
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: "0000000000000000", ExpectReportDigest: res.Plan.ReportDigest}); err == nil || !strings.Contains(err.Error(), "changed since the preview") {
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
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest}); err == nil ||
		!strings.Contains(err.Error(), "--expect-report-digest is required") {
		t.Fatalf("apply must require the independent artifact fence: %v", err)
	}

	out, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest, ExpectReportDigest: res.Plan.ReportDigest})
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
	if g.RevisionLineage["1111111111111111111111111111111111111111"] != "2222222222222222222222222222222222222222" {
		t.Fatalf("amend discarded revision lineage: %+v", g.RevisionLineage)
	}
	st := states.Derive(states.Inputs{Graph: g})
	if st["big"].State != states.Ready || !st["big"].RevIncompatible {
		t.Fatalf("revised node's old pass is not current proof: %+v", st["big"])
	}
	if st["helper"].State != states.Green {
		t.Fatalf("unrelated node untouched: %+v", st["helper"])
	}
	// The same artifact cannot amend twice.
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: out.NewDigest, ExpectReportDigest: res.Plan.ReportDigest}); err == nil || !strings.Contains(err.Error(), "already applied") {
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
	out, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest, ExpectReportDigest: res.Plan.ReportDigest})
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

func TestAmendRefusesForeignOrInadmissibleReviewArtifacts(t *testing.T) {
	tests := []struct {
		name       string
		replaceOld string
		replaceNew string
		want       string
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
	preview, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), "original replacement", "substituted replacement", 1)
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: preview.ExpectDigest, ExpectReportDigest: preview.Plan.ReportDigest}); err == nil ||
		!strings.Contains(err.Error(), "review artifact changed since the preview") {
		t.Fatalf("substituted report must refuse under its own digest fence: %v", err)
	}
	after, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a substituted review artifact must leave the graph unchanged")
	}
}

func TestAmendRejectsArtifactAlreadyUsedAsEvidence(t *testing.T) {
	root, planDir := fixtureRoot(t)
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: duplicate evidence\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: duplicate replacement\n")
	artifact, err := greview.ReadArtifact(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("feature-gate").Verification = &model.Verification{
			Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
			ReportDigest: artifact.ReportDigest,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, DryRun: true}); err == nil ||
		!strings.Contains(err.Error(), "already recorded on gate") {
		t.Fatalf("evidence already consumed by Record must not be reusable by Amend: %v", err)
	}
}

func TestAmendExtendInvalidatesAlreadyGreenReview(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("helper").Verification = passAt(1)
		g.NodeByID("big").Verification = passAt(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cleanRel := frozenReview(t, root, "")
	if _, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: cleanRel}); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-Reviewed-map observation. It remains compatible until
	// this extension changes the scope; amend must materialize the old scope.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("feature-gate").Verification.Reviewed = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	extendRel := "Plans/SamplePlan/reviews/02-extend.md"
	original := filepath.Join(root, filepath.FromSlash(cleanRel))
	raw, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	findings := "  - id: F-02\n    severity: minor\n    title: extend scope\n    status: open\n    action: extend\n    node:\n      id: big-audit\n      contract: emits audit\n      deps: [big]\n      gate:\n        type: tests\n        tests: [{id: test_big_audit, file: t.ext}]\n      hazards: []\n"
	extendText := strings.Replace(strings.Replace(string(raw), "findings:\n---", "findings:\n"+findings+"---", 1), "verdict: Aligned", "verdict: Amend", 1)
	extendPath := filepath.Join(root, filepath.FromSlash(extendRel))
	if err := os.WriteFile(extendPath, []byte(extendText), 0o644); err != nil {
		t.Fatal(err)
	}
	preview, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: extendRel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: extendRel, ExpectDigest: preview.ExpectDigest, ExpectReportDigest: preview.Plan.ReportDigest}); err != nil {
		t.Fatal(err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	st := states.Derive(states.Inputs{Graph: g})
	if ns := st["feature-gate"]; ns.State == states.Green || ns.RevIncompatible || len(ns.ReviewStale) == 0 {
		t.Fatalf("scope extension must stale prior coverage without pretending owned contract fields changed: %+v", ns)
	}
}

func TestAmendMaterializesEmptyLegacyScopeAcrossEncoding(t *testing.T) {
	root, _ := fixtureRoot(t)
	a := model.Node{ID: "a", Contract: "a", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1, Verification: passAt(1)}
	inner := model.Node{ID: "inner", Role: model.RoleReview, Contract: "inner", Deps: []string{"a"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1, Verification: passAt(2)}
	inner.Verification.Reviewed = map[string]model.ReviewedRef{"a": {ContractRev: 1}}
	outer := model.Node{ID: "outer", Role: model.RoleReview, Contract: "outer", Deps: []string{"inner"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1, Verification: passAt(3)}
	// nil Reviewed is the legacy shape; inner currently covers the whole
	// incremental scope, so the materialized pre-extension scope is empty.
	g := &model.Graph{Version: model.SchemaVersion, SeqCounter: 3, Nodes: []model.Node{a, inner, outer}}
	sources, err := gcompile.NewSources(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	plan := &greview.Plan{
		Review: "outer", Scope: nil, Artifact: "reviews/legacy.md", ReportDigest: "legacy-report",
		Amendments: []greview.Amendment{{
			Finding: "F-01", Action: greview.ActionExtend, Node: "new", New: &model.Node{
				ID: "new", Contract: "new work", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1,
			},
		}},
	}
	rebuilt, _, err := applyAmendments(g, plan, "", sources)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rebuilt.Encode()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := model.DecodeGraph(raw)
	if err != nil {
		t.Fatal(err)
	}
	ns := states.Derive(states.Inputs{Graph: reloaded})["outer"]
	if ns.State == states.Green || len(ns.ReviewStale) == 0 {
		t.Fatalf("empty legacy scope must not round-trip back to nil compatibility after extend: %+v", ns)
	}
}

func TestAmendExtendInvalidatesLegacyReviewWithoutChangingContractRevision(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("helper").Verification = passAt(1)
		g.NodeByID("big").Verification = passAt(1)
		gate := g.NodeByID("feature-gate")
		gate.Verification = passAt(2) // legacy wire shape: Reviewed remains nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rel := frozenReview(t, root, "  - id: F-legacy\n    severity: minor\n    title: extend legacy scope\n    status: open\n    action: extend\n    node:\n      id: legacy-audit\n      contract: audits legacy work\n      deps: [big]\n      gate:\n        type: tests\n        tests: [{id: test_legacy_audit, file: t.ext}]\n      hazards: []\n")
	preview, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel,
		ExpectDigest: preview.ExpectDigest, ExpectReportDigest: preview.Plan.ReportDigest}); err != nil {
		t.Fatal(err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	gate := g.NodeByID("feature-gate")
	ns := states.Derive(states.Inputs{Graph: g})["feature-gate"]
	if gate.EffectiveContractRev() != gate.Verification.EffectiveContractRev() || ns.State == states.Green || len(ns.ReviewStale) == 0 {
		t.Fatalf("legacy scope growth must stale coverage without changing contract identity: gate=%+v state=%+v", gate, ns)
	}
}

func TestAmendRefusesUnsafePlanNameBeforePathResolution(t *testing.T) {
	if _, err := AmendFromReview(AmendOptions{Root: t.TempDir(), RepoRoot: t.TempDir(), Plan: "../Foreign", Node: "gate", Artifact: "review.md", DryRun: true}); err == nil ||
		!strings.Contains(err.Error(), "single safe directory name") {
		t.Fatalf("plan traversal must refuse before graph or artifact access: %v", err)
	}
}

// A legacy review pass (no reviewed set) must not survive a revise of a node
// it reviewed: the amendment materializes the pre-revise set, and derivation
// independently treats a legacy pass over a revised scope as history.
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
		t.Fatalf("precondition: legacy review is GREEN: %+v", st["feature-gate"])
	}
	rel := frozenReview(t, root, "  - id: F-01\n    severity: major\n    title: \"big is wrong\"\n    status: open\n    action: revise\n    nodes: [big]\n    revise:\n      contract: \"does too much, revised\"\n")
	res, err := greview.Record(greview.Options{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmendFromReview(AmendOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "feature-gate", Artifact: rel, ExpectDigest: res.ExpectDigest, ExpectReportDigest: res.Plan.ReportDigest}); err != nil {
		t.Fatal(err)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	st := states.Derive(states.Inputs{Graph: g})
	if st["feature-gate"].State != states.Stale || !containsString(st["feature-gate"].ReviewStale, "big") {
		t.Fatalf("legacy review must stale on a revise of a reviewed node: %+v reviewed=%v", st["feature-gate"], g.NodeByID("feature-gate").Verification.Reviewed)
	}
	// The derive rule alone (no materialization) also catches it.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("feature-gate").Verification.Reviewed = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	if st := states.Derive(states.Inputs{Graph: g}); st["feature-gate"].State != states.Stale {
		t.Fatalf("a legacy review over a revised scope is history, not proof: %+v", st["feature-gate"])
	}
}

// AC4: acknowledge rebinds the anchor and records the judgment, and it can
// never green a node by itself.
func TestAcknowledgeRebindsAnchorButCannotGreen(t *testing.T) {
	root, planDir := fixtureRoot(t)
	// helper cites AC-01 with a stale compile anchor and a pass whose snapshot
	// also predates the change: STALE by intent.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID("helper")
		n.IntentHashes = map[string]string{"AC-01": "sha256:stale"}
		n.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
			DependencyDigests: map[string]map[string]string{}, IntentHashes: map[string]string{"AC-01": "sha256:stale"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dry, err := Acknowledge(AcknowledgeOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "helper", Citation: "AC-01", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Applied || dry.Record.Old != "sha256:stale" || dry.Record.New == "" || dry.Record.New == dry.Record.Old {
		t.Fatalf("dry run = %+v", dry)
	}
	if _, err := Acknowledge(AcknowledgeOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "helper", Citation: "AC-01", ExpectDigest: "0000000000000000"}); err == nil {
		t.Fatal("stale fence must refuse")
	}
	res, err := Acknowledge(AcknowledgeOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "helper", Citation: "AC-01", ExpectDigest: dry.ExpectDigest, By: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	g, lerr := gstore.Load(gstore.PathFor(planDir))
	if lerr != nil {
		t.Fatalf("graph after acknowledge does not load: %v", lerr)
	}
	n := g.NodeByID("helper")
	if n.IntentHashes["AC-01"] != res.Record.New || len(g.Acknowledgements) != 1 || g.Acknowledgements[0].By != "reviewer" || g.Acknowledgements[0].Seq == 0 {
		t.Fatalf("anchor not rebound or judgment not recorded: %+v %+v", n.IntentHashes, g.Acknowledgements)
	}
	if n.Verification.IntentHashes["AC-01"] != "sha256:stale" {
		t.Fatal("acknowledge must not touch the observation")
	}
	// Still not GREEN: the run's snapshot predates the text; only a new pass greens.
	snap, _ := gcompile.LoadIntentSnapshot(root, root, "SamplePlan")
	st := states.Derive(states.Inputs{Graph: g, CurrentIntentHashes: snap.Hashes()})
	if st["helper"].State == states.Green {
		t.Fatalf("acknowledge alone must not green: %+v", st["helper"])
	}
	// Nothing to acknowledge twice.
	if _, err := Acknowledge(AcknowledgeOptions{Root: root, RepoRoot: root, Plan: "SamplePlan", Node: "helper", Citation: "AC-01", DryRun: true}); err == nil {
		t.Fatal("a current anchor has nothing to acknowledge")
	}
}
