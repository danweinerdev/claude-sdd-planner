package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

func fixtureRoot(t *testing.T) (root, planDir string) {
	t.Helper()
	root = t.TempDir()
	planDir = filepath.Join(root, "Plans", "SamplePlan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":"."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte("---\ntitle: Sample\ntype: plan\nstatus: draft\ncreated: 2026-01-01\nupdated: 2026-01-01\ntags: []\nrelated: [Specs/Sample]\nphases: []\n---\n\n# Sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specDir := filepath.Join(root, "Specs", "Sample")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "README.md"), []byte("---\ntitle: Sample\ntype: spec\nstatus: approved\ncreated: 2026-01-01\nupdated: 2026-01-01\ntags: []\nrelated: []\n---\n\n# Sample\n\n## Acceptance Criteria\n\n- [ ] **AC-01**: The API answers.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "helper", Contract: "helps", Justifies: []string{"AC-01"}, Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_helper", File: "t.ext"}}}, Hazards: model.Hazards{}, Estimate: 1}, {ID: "big", Contract: "does too much", Justifies: []string{"AC-01"}, Deps: []string{"helper"}, Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_big", File: "t.ext", Satisfies: []string{"external-format"}}}}, Hazards: model.Hazards{"external-format"}, Estimate: 3, Phase: "01-core"}, {ID: "feature-gate", Contract: "survives review", Justifies: []string{"AC-01"}, Deps: []string{"big"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1}}}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

func splitCandidate() (*model.Graph, *model.Proposal) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "dep"}, {ID: "big", Deps: []string{"dep"}, Hazards: model.Hazards{}}, {ID: "use", Deps: []string{"big"}}}}
	p := &model.Proposal{Version: 1, Nodes: []model.Node{{ID: "a", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1}, {ID: "b", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1}}}
	return g, p
}

const splitChildren = `{"version":1,"nodes":[
{"id":"big-parse","contract":"parses","justifies":["AC-01"],"gate":{"type":"tests","tests":[{"id":"test_parse","file":"t.ext","satisfies":["external-format"]}]},"hazards":["external-format"]},
{"id":"big-render","contract":"renders","justifies":["AC-01"],"gate":{"type":"tests","tests":[{"id":"test_render","file":"t.ext"}]},"hazards":[]}
]}`

const opsSpec = `---
title: "Sample Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Sample Spec

## Acceptance Criteria

- [ ] **AC-01**: The API answers.
`

const opsPlan = `---
title: "Sample Plan"
type: plan
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/Sample]
phases: []
---

# Sample Plan
`

const opsDesign = `---
title: "Sample Design"
type: design
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [design]
related: [Specs/Sample]
---

# Sample Design

## Design Decisions

- **DD-1**: Answer once.
  Context: c. Decision: d. Rationale: r.
`

func contendWith(t *testing.T, path string, compete func(*model.Graph) error) updateFunc {
	t.Helper()
	done := false
	return func(path string, fn func(*model.Graph) error) (*model.Graph, error) {
		return gstore.Update(path, func(g *model.Graph) error {
			if done {
				return fn(g)
			}
			done = true
			if err := fn(g); err != nil {
				return err
			}
			_, err := gstore.Update(path, compete)
			return err
		})
	}
}
func TestSplitRetiresRewiresAndInherits(t *testing.T) {
	g, p := splitCandidate()
	out, res, err := applySplit(g, "big", p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Retired != "big" || !reflect.DeepEqual(out.NodeByID("use").Deps, []string{"a", "b"}) {
		t.Fatalf("out=%+v res=%+v", out, res)
	}
}
func TestSplitPreservesAmendmentsAndInvalidatesLegacyReview(t *testing.T) {
	_, planDir := fixtureRoot(t)
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	g.Amendments = []model.AmendmentRecord{{Seq: 1, Review: "feature-gate", ReportDigest: "prior-report", Extended: []string{"helper"}}}
	g.RevisionLineage = map[string]string{"1111111111111111111111111111111111111111": "2222222222222222222222222222222222222222"}
	g.SeqCounter = 4
	g.NodeByID("helper").Verification = passAt(2)
	g.NodeByID("big").Verification = passAt(3)
	g.NodeByID("feature-gate").Verification = passAt(4)
	p, err := model.DecodeProposal([]byte(splitChildren))
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := applySplit(g, "big", p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Amendments, g.Amendments) {
		t.Error("split discarded amendment replay-protection history")
	}
	if !reflect.DeepEqual(out.RevisionLineage, g.RevisionLineage) {
		t.Error("split discarded revision lineage")
	}
	if got := states.Derive(states.Inputs{Graph: out})["feature-gate"].State; got == states.Green {
		t.Error("legacy review remained GREEN after its reviewed node was split")
	}
	if g.NodeByID("feature-gate").Verification.Reviewed != nil {
		t.Error("candidate computation mutated the original observation")
	}
}
func TestSplitGatesOnIntroducedFindings(t *testing.T) {
	g, p := splitCandidate()
	p.Nodes[0].ID = "big"
	if _, _, err := applySplit(g, "big", p); err == nil {
		t.Fatal("retired id reused")
	}
}
func TestSplitDisciplines(t *testing.T) {
	g, p := splitCandidate()
	g.NodeByID("big").Claim = &model.Claim{By: "x"}
	if _, _, err := applySplit(g, "big", p); err == nil {
		t.Fatal("claimed node split")
	}
}
func TestSplitRefusesCompetingClaimOnCASRetry(t *testing.T) {
	root, planDir := fixtureRoot(t)
	path := gstore.PathFor(planDir)
	_, err := splitWith(root, root, "SamplePlan", "big", []byte(splitChildren),
		contendWith(t, path, func(g *model.Graph) error {
			g.NodeByID("big").Claim = &model.Claim{By: "racer", LeaseExpires: "2099-01-01T00:00:00Z"}
			return nil
		}))
	if err == nil {
		t.Fatal("split succeeded across a competing claim")
	}
	g, loadErr := gstore.Load(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if g.NodeByID("big") == nil || g.NodeByID("big").Claim == nil || g.NodeByID("big").Claim.By != "racer" || g.NodeByID("big-parse") != nil {
		t.Fatalf("split was not atomic around the competing claim: %+v", g.Nodes)
	}
}
func TestSplitRefusesUnreachableDesignCitationAtomically(t *testing.T) {
	root, planDir := fixtureRoot(t)
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "Designs", "Sample"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("Designs/Sample/README.md", opsDesign)
	write("Specs/Sample/README.md", strings.Replace(opsSpec, "related: []", "related: [Designs/Sample]", 1))
	children := strings.Replace(splitChildren, `"big-render","contract":"renders","justifies":["AC-01"]`, `"big-render","contract":"renders","justifies":["AC-01","Designs/Sample:DD-1"]`, 1)
	if children == splitChildren {
		t.Fatal("fixture children not rewritten")
	}
	before, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Split(root, root, "SamplePlan", "big", []byte(children))
	if err == nil {
		t.Fatal("a split citing an unreachable design must refuse")
	}
	for _, want := range []string{`big-render: cites "Designs/Sample:DD-1", which resolves in no related spec, design`, "Designs/Sample/README.md defines it but is not reachable through the plan's `related` graph", "relate it directly from Plans/SamplePlan/README.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q:\n%s", want, err)
		}
	}
	after, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused split must leave the graph byte-identical")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("big") == nil || g.NodeByID("big-render") != nil || g.NodeByID("big-parse") != nil {
		t.Fatal("a refused split must keep the parent and create no children")
	}
	write("Plans/SamplePlan/README.md", strings.Replace(opsPlan, "related: [Specs/Sample]", "related: [Specs/Sample, Designs/Sample]", 1))
	res, err := Split(root, root, "SamplePlan", "big", []byte(children))
	if err != nil {
		t.Fatalf("split with directly related design must succeed: %v", err)
	}
	if res.Retired != "big" || len(res.Children) != 2 {
		t.Fatalf("result: %+v", res)
	}
}

func TestSetTestsHolderDisciplineAndRedSeqPrune(t *testing.T) {
	_, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID("big")
		n.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
		n.RedSeqs = map[string]int{"test_big": 3}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tests := []model.Test{{ID: "test_big_renamed", File: "t.ext", Satisfies: []string{"external-format"}}}
	if err := SetTests(planDir, "big", "impostor", tests); err == nil {
		t.Fatal("only holder edits tests")
	}
	if err := SetTests(planDir, "big", "holder", tests); err != nil {
		t.Fatalf("holder set-tests: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	n := g.NodeByID("big")
	if len(n.Gate.Tests) != 1 || n.Gate.Tests[0].ID != "test_big_renamed" || len(n.RedSeqs) != 0 {
		t.Fatalf("tests=%+v reds=%v", n.Gate.Tests, n.RedSeqs)
	}
	if err := SetTests(planDir, "big", "holder", []model.Test{{ID: "t", File: "f", Satisfies: []string{"undeclared-hazard"}}}); err == nil {
		t.Fatal("undeclared hazard accepted")
	}
	if err := SetTests(planDir, "big", "holder", nil); err == nil {
		t.Fatal("empty tests accepted")
	}
}
func TestSetTestsUnderObservationAdvancesRevision(t *testing.T) {
	_, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID("helper")
		n.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean, ContractRev: 1}
		n.RedSeqs = map[string]int{"test_helper": 1}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	same := []model.Test{{ID: "test_helper", File: "t.ext"}}
	if err := SetTests(planDir, "helper", "", same); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if n := g.NodeByID("helper"); n.EffectiveContractRev() != 1 || len(n.RedSeqs) != 1 {
		t.Fatalf("unchanged list not no-op: rev=%d red=%v", n.ContractRev, n.RedSeqs)
	}
	if err := SetTests(planDir, "helper", "", []model.Test{{ID: "test_helper_v2", File: "t.ext"}}); err != nil {
		t.Fatal(err)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	n := g.NodeByID("helper")
	if n.ContractRev != 2 || n.RedSeqs != nil {
		t.Fatalf("changed gate: rev=%d red=%v", n.ContractRev, n.RedSeqs)
	}
	if st := states.Derive(states.Inputs{Graph: g}); st["helper"].State == states.Green {
		t.Fatalf("old proof survived: %+v", st["helper"])
	}
}
func TestSetTestsKeepsAnchorsSoReverifyRaisesNoAdvisory(t *testing.T) {
	_, d := fixtureRoot(t)
	path := gstore.PathFor(d)
	_, _ = gstore.Update(path, func(g *model.Graph) error {
		g.NodeByID("helper").Verification = &model.Verification{Result: model.ResultPass, Seq: 7, Isolation: model.IsolationClean}
		return nil
	})
	if err := SetTests(d, "helper", "", []model.Test{{ID: "new", File: "t.ext"}}); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(path)
	if g.NodeByID("helper").Verification.Seq != 7 {
		t.Fatal("observation changed")
	}
}

func TestSetArtifactsHolderDisciplineAndValidation(t *testing.T) {
	_, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID("big")
		n.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
		n.Artifacts = []string{"crates/"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	narrowed := []string{"Cargo.toml", "rust-toolchain.toml"}
	if err := SetArtifacts(planDir, "big", "impostor", narrowed); err == nil {
		t.Fatal("impostor edited claimed node")
	}
	if err := SetArtifacts(planDir, "big", "holder", narrowed); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if got := g.NodeByID("big").Artifacts; !reflect.DeepEqual(got, narrowed) {
		t.Fatalf("artifacts=%v", got)
	}
	if err := SetArtifacts(planDir, "helper", "", []string{"src/helper.rs"}); err != nil {
		t.Fatal(err)
	}
	if err := SetArtifacts(planDir, "missing", "holder", narrowed); err == nil {
		t.Fatal("a nonexistent node refuses")
	}
	if err := SetArtifacts(planDir, "big", "holder", nil); err == nil {
		t.Fatal("an empty write-set refuses")
	}
	if err := SetArtifacts(planDir, "big", "holder", []string{"  "}); err == nil {
		t.Fatal("a blank artifact path refuses")
	}
	if err := SetArtifacts(planDir, "big", "holder", []string{"a.rs", "a.rs"}); err == nil {
		t.Fatal("duplicate artifact paths refuse")
	}
	if err := SetArtifacts(planDir, "feature-gate", "", narrowed); err == nil {
		t.Fatal("a review gate has no declared write-set")
	}
}
func TestEditArtifactsAddRemove(t *testing.T) {
	_, planDir := fixtureRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID("big")
		n.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
		n.Artifacts = []string{"crates/a.rs", "crates/b.rs"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := EditArtifacts(planDir, "big", "impostor", []string{"crates/c.rs"}, nil); err == nil {
		t.Fatal("impostor edited claimed node")
	}
	if err := EditArtifacts(planDir, "big", "holder", []string{"crates/c.rs"}, nil); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	want := []string{"crates/a.rs", "crates/b.rs", "crates/c.rs"}
	if got := g.NodeByID("big").Artifacts; !reflect.DeepEqual(got, want) {
		t.Fatalf("after add=%v want %v", got, want)
	}
	if err := EditArtifacts(planDir, "big", "holder", []string{"crates/d.rs"}, []string{"crates/a.rs"}); err != nil {
		t.Fatal(err)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	want = []string{"crates/b.rs", "crates/c.rs", "crates/d.rs"}
	if got := g.NodeByID("big").Artifacts; !reflect.DeepEqual(got, want) {
		t.Fatalf("after add+remove=%v want %v", got, want)
	}
	if err := EditArtifacts(planDir, "big", "holder", nil, []string{"crates/nonexistent.rs"}); err != nil {
		t.Fatal(err)
	}
	if err := EditArtifacts(planDir, "big", "holder", nil, nil); err == nil {
		t.Fatal("no add or remove refuses")
	}
	if err := EditArtifacts(planDir, "big", "holder", []string{"  "}, nil); err == nil {
		t.Fatal("blank add refuses")
	}
	if err := EditArtifacts(planDir, "big", "holder", []string{"crates/b.rs"}, nil); err == nil {
		t.Fatal("duplicate add refuses")
	}
	if err := EditArtifacts(planDir, "big", "holder", nil, []string{"crates/b.rs", "crates/c.rs", "crates/d.rs"}); err == nil {
		t.Fatal("remove-all refuses")
	}
	if err := EditArtifacts(planDir, "missing", "holder", []string{"x"}, nil); err == nil {
		t.Fatal("nonexistent node refuses")
	}
	if err := EditArtifacts(planDir, "feature-gate", "", []string{"x"}, nil); err == nil {
		t.Fatal("review gate refuses")
	}
}
func TestSetArtifactsNarrowingUnderVerificationAdvancesRevision(t *testing.T) {
	_, d := fixtureRoot(t)
	path := gstore.PathFor(d)
	v := &model.Verification{Result: model.ResultPass, Seq: 4, Isolation: model.IsolationClean}
	_, _ = gstore.Update(path, func(g *model.Graph) error {
		n := g.NodeByID("big")
		n.Artifacts = []string{"src/"}
		n.Verification = v
		return nil
	})
	if err := SetArtifacts(d, "big", "", []string{"src/a.go"}); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(path)
	n := g.NodeByID("big")
	if n.Verification.Seq != 4 || n.EffectiveContractRev() != 2 {
		t.Fatalf("observation/revision = %+v rev=%d", n.Verification, n.EffectiveContractRev())
	}
}
func TestGCReapsOrphansAndStalePayloadsOnly(t *testing.T) {
	root, planDir := fixtureRoot(t)
	graphDir := filepath.Join(planDir, gstore.GraphDirName)
	for _, ws := range []string{"ws-orphan", "ws-active", "ws-crashed"} {
		if err := os.MkdirAll(filepath.Join(graphDir, ws), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	activeHandle := "Plans/SamplePlan/.graph/ws-active"
	crashedHandle := "Plans/SamplePlan/.graph/ws-crashed"
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("big").Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z", Workspace: activeHandle}
		g.NodeByID("helper").Claim = &model.Claim{By: "crashed", LeaseExpires: "2001-01-01T00:00:00Z", Workspace: crashedHandle}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stale := `{"version":1,"nodes":[{"id":"helper","contract":"c","justifies":["AC-01"],"gate":{"type":"tests","tests":[{"id":"t","file":"t.ext"}]},"hazards":[]}]}`
	live := `{"version":1,"nodes":[{"id":"novel","contract":"c","justifies":["AC-01"],"gate":{"type":"tests","tests":[{"id":"t","file":"t.ext"}]},"hazards":[]}]}`
	fragDir := proposal.FragmentsDir(planDir)
	if err := os.MkdirAll(fragDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := istore.WriteAtomic(filepath.Join(fragDir, "000-stale.json"), stale); err != nil {
		t.Fatal(err)
	}
	if err := istore.WriteAtomic(filepath.Join(fragDir, "001-live.json"), live); err != nil {
		t.Fatal(err)
	}
	res, err := GC(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !reflect.DeepEqual(res.ExpiredClaims, []string{"helper"}) || !reflect.DeepEqual(res.Workspaces, []string{crashedHandle, "Plans/SamplePlan/.graph/ws-orphan"}) || !reflect.DeepEqual(res.Kept, []string{activeHandle}) || !reflect.DeepEqual(res.StalePayloads, []string{"000-stale.json"}) {
		t.Fatalf("gc result: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(graphDir, "ws-active")); err != nil {
		t.Fatal("active workspace must survive")
	}
	if _, err := os.Stat(filepath.Join(graphDir, "ws-crashed")); !os.IsNotExist(err) {
		t.Fatal("expired workspace must be reaped")
	}
	if _, err := os.Stat(filepath.Join(fragDir, "001-live.json")); err != nil {
		t.Fatal("live payload must survive")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("helper").Claim != nil || g.NodeByID("big").Claim == nil {
		t.Fatal("claim expiry persistence wrong")
	}
}

func gitOps(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func branchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func TestGCPrunesMergedClaimBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root, planDir := fixtureRoot(t)
	gitOps(t, root, "init", "-q")
	gitOps(t, root, "config", "user.email", "t@example.com")
	gitOps(t, root, "config", "user.name", "t")
	gitOps(t, root, "add", "-A")
	gitOps(t, root, "commit", "-q", "-m", "base")
	mainline := gitOps(t, root, "rev-parse", "--abbrev-ref", "HEAD")
	gitOps(t, root, "branch", "graph/done-ab12", "HEAD")
	gitOps(t, root, "checkout", "-q", "-b", "graph/wip-cd34")
	if err := os.WriteFile(filepath.Join(root, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOps(t, root, "add", "wip.txt")
	gitOps(t, root, "commit", "-q", "-m", "wip work")
	gitOps(t, root, "checkout", "-q", mainline)
	gitOps(t, root, "worktree", "add", "-q", "-b", "graph/active-ef56", filepath.Join(planDir, ".graph", "ws-active-claim"), "HEAD")
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("big").Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z", Workspace: "Plans/SamplePlan/.graph/ws-active-claim"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	gitOps(t, root, "branch", "feature-x", "HEAD")
	res, err := GC(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !reflect.DeepEqual(res.PrunedBranches, []string{"graph/done-ab12"}) {
		t.Fatalf("pruned=%v", res.PrunedBranches)
	}
	if branchExists(t, root, "graph/done-ab12") || !branchExists(t, root, "graph/wip-cd34") || !branchExists(t, root, "graph/active-ef56") || !branchExists(t, root, "feature-x") {
		t.Fatal("branch survival/pruning set was wrong")
	}
}
func TestRetireTombstonesAnId(t *testing.T) {
	_, d := fixtureRoot(t)
	if err := Retire(d, "3.3"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if err := Retire(d, "3.3"); err == nil || !strings.Contains(err.Error(), "already retired") {
		t.Fatalf("duplicate retirement: %v", err)
	}
	if err := Retire(d, "big"); err == nil || !strings.Contains(err.Error(), "live node") {
		t.Fatalf("live retirement: %v", err)
	}
	if err := Retire(d, "  "); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := Retire(d, "1.9"); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(d))
	if !reflect.DeepEqual(g.Retired, []string{"1.9", "3.3"}) {
		t.Fatalf("retired=%v", g.Retired)
	}
}
