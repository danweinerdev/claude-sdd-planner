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
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

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

// fixtureRoot builds a planning root whose graph compiles clean: impl node,
// helper node, and a terminal full gate covering both.
func fixtureRoot(t *testing.T) (root, planDir string) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("planning-config.json", `{"planningRoot": "."}`)
	write("Specs/Sample/README.md", opsSpec)
	write("Plans/SamplePlan/README.md", opsPlan)
	planDir = filepath.Join(root, "Plans", "SamplePlan")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes,
			model.Node{ID: "big", Contract: "does too much", Justifies: []string{"AC-01"},
				Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_big", File: "t.ext"}}},
				Hazards: model.Hazards{"external-format"}, Estimate: 3, Phase: "01-core",
				Deps: []string{"helper"}},
			model.Node{ID: "helper", Contract: "helps", Justifies: []string{"AC-01"},
				Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_helper", File: "t.ext"}}},
				Hazards: model.Hazards{}, Estimate: 1},
			model.Node{ID: "feature-gate", Contract: "survives review", Justifies: []string{"AC-01"},
				Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1,
				Deps: []string{"big"}},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The big node's hazard needs a satisfying test for the fixture to be
	// finding-free at baseline.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("big").Gate.Tests[0].Satisfies = []string{"external-format"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

const splitChildren = `{
  "version": 1,
  "nodes": [
    {"id": "big-parse", "contract": "parses the input", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "test_parse", "file": "t.ext", "satisfies": ["external-format"]}]},
     "hazards": ["external-format"]},
    {"id": "big-render", "contract": "renders the output", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "test_render", "file": "t.ext"}]},
     "hazards": []}
  ]
}
`

func TestSplitRetiresRewiresAndInherits(t *testing.T) {
	root, planDir := fixtureRoot(t)
	res, err := Split(root, root, "SamplePlan", "big", []byte(splitChildren))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if res.Retired != "big" || !reflect.DeepEqual(res.Children, []string{"big-parse", "big-render"}) {
		t.Fatalf("result: %+v", res)
	}
	if !reflect.DeepEqual(res.Rewired, []string{"feature-gate"}) {
		t.Fatalf("dependants must re-point at the children: %+v", res.Rewired)
	}

	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("big") != nil {
		t.Fatal("the original must be gone")
	}
	if !reflect.DeepEqual(g.Retired, []string{"big"}) {
		t.Fatalf("the retired register is append-only history: %v", g.Retired)
	}
	// Children without deps inherit the original's; the declared phase
	// label carries over.
	parse := g.NodeByID("big-parse")
	if !reflect.DeepEqual(parse.Deps, []string{"helper"}) || parse.Phase != "01-core" {
		t.Fatalf("inheritance: %+v", parse)
	}
	gate := g.NodeByID("feature-gate")
	if !reflect.DeepEqual(gate.Deps, []string{"big-parse", "big-render"}) {
		t.Fatalf("rewiring: %v", gate.Deps)
	}
}

func TestSplitGatesOnIntroducedFindings(t *testing.T) {
	root, _ := fixtureRoot(t)
	// Children that drop the hazard-satisfying test would introduce an
	// undischarged-hazard finding: refused, nothing written.
	bad := strings.Replace(splitChildren, `, "satisfies": ["external-format"]`, "", 1)
	_, err := Split(root, root, "SamplePlan", "big", []byte(bad))
	if err == nil || !strings.Contains(err.Error(), "discharged by no test") {
		t.Fatalf("split must refuse introduced findings: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(filepath.Join(root, "Plans", "SamplePlan")))
	if g.NodeByID("big") == nil {
		t.Fatal("a refused split writes nothing")
	}
}

func TestSplitDisciplines(t *testing.T) {
	root, planDir := fixtureRoot(t)
	if _, err := Split(root, root, "SamplePlan", "missing", []byte(splitChildren)); err == nil {
		t.Fatal("splitting a nonexistent node refuses")
	}
	one := `{"version": 1, "nodes": [{"id": "only", "contract": "c", "justifies": ["AC-01"], "gate": {"type": "tests"}, "hazards": []}]}`
	if _, err := Split(root, root, "SamplePlan", "big", []byte(one)); err == nil ||
		!strings.Contains(err.Error(), "at least two children") {
		t.Fatalf("a one-child split is a rename, not a split: %v", err)
	}
	// A claimed node cannot be split from under its holder.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("big").Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Split(root, root, "SamplePlan", "big", []byte(splitChildren)); err == nil ||
		!strings.Contains(err.Error(), "claimed") {
		t.Fatalf("splitting a claimed node refuses: %v", err)
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
		t.Fatal("only the holder edits a claimed node's tests")
	}
	if err := SetTests(planDir, "big", "holder", tests); err != nil {
		t.Fatalf("holder set-tests: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	n := g.NodeByID("big")
	if len(n.Gate.Tests) != 1 || n.Gate.Tests[0].ID != "test_big_renamed" {
		t.Fatalf("tests: %+v", n.Gate.Tests)
	}
	if len(n.RedSeqs) != 0 {
		t.Fatal("a replaced test owes a fresh red proof; stale red_seqs are pruned")
	}

	if err := SetTests(planDir, "big", "holder",
		[]model.Test{{ID: "t", File: "f", Satisfies: []string{"undeclared-hazard"}}}); err == nil {
		t.Fatal("satisfies must name a hazard from the closed vocabulary that the node declares")
	}
	if err := SetTests(planDir, "big", "holder", nil); err == nil {
		t.Fatal("an empty tests gate verifies nothing")
	}
}

func TestGCReapsOrphansAndStalePayloadsOnly(t *testing.T) {
	root, planDir := fixtureRoot(t)
	graphDir := filepath.Join(planDir, gstore.GraphDirName)

	// An orphan workspace (no claim references it), an active one, and one
	// referenced only by a LAPSED claim — the crash story.
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

	// A stale payload (its only node already landed) and a live one.
	stale := `{"version": 1, "nodes": [{"id": "helper", "contract": "c", "justifies": ["AC-01"], "gate": {"type": "tests"}, "hazards": []}]}`
	live := `{"version": 1, "nodes": [{"id": "novel", "contract": "c", "justifies": ["AC-01"], "gate": {"type": "tests"}, "hazards": []}]}`
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
	if !reflect.DeepEqual(res.ExpiredClaims, []string{"helper"}) {
		t.Fatalf("gc persists the expiry of lapsed claims: %+v", res.ExpiredClaims)
	}
	if !reflect.DeepEqual(res.Workspaces, []string{crashedHandle, "Plans/SamplePlan/.graph/ws-orphan"}) {
		t.Fatalf("gc reaps the orphan AND the expired claimant's workspace: %+v", res.Workspaces)
	}
	if !reflect.DeepEqual(res.Kept, []string{activeHandle}) {
		t.Fatalf("gc never touches an unexpired claim's workspace: %+v", res.Kept)
	}
	if !reflect.DeepEqual(res.StalePayloads, []string{"000-stale.json"}) {
		t.Fatalf("gc reaps exactly the stale payloads: %+v", res.StalePayloads)
	}
	if _, err := os.Stat(filepath.Join(graphDir, "ws-active")); err != nil {
		t.Fatal("the active workspace must survive")
	}
	if _, err := os.Stat(filepath.Join(graphDir, "ws-crashed")); !os.IsNotExist(err) {
		t.Fatal("the expired claimant's workspace must be reaped")
	}
	if _, err := os.Stat(filepath.Join(fragDir, "001-live.json")); err != nil {
		t.Fatal("a payload with novel nodes must survive")
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("helper").Claim != nil {
		t.Fatal("the lapsed claim must be cleared in the persisted graph")
	}
	if g.NodeByID("big").Claim == nil {
		t.Fatal("the unexpired claim must survive gc")
	}
}

// gitOps runs one git command in dir for the branch-pruning fixture.
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

// branchExists reports whether a branch ref resolves.
func branchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// TestGCPrunesMergedClaimBranches (pilot-gc-branch-pruning, hazard
// derives-state): gc prunes exactly the graph/* branches whose tips are
// reachable from the mainline HEAD and which no worktree has checked out —
// the prune set is tied to an INDEPENDENT definition (git ancestry plus
// checkout state, computed by the test itself), never to a restatement of
// the implementation. Unmerged work, active claims' branches, and
// non-graph branches always survive.
func TestGCPrunesMergedClaimBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root, _ := fixtureRoot(t)
	gitOps(t, root, "init", "-q")
	gitOps(t, root, "config", "user.email", "t@example.com")
	gitOps(t, root, "config", "user.name", "t")
	gitOps(t, root, "add", "-A")
	gitOps(t, root, "commit", "-q", "-m", "base")
	mainline := gitOps(t, root, "rev-parse", "--abbrev-ref", "HEAD")

	// A merged claim branch: tip reachable from mainline HEAD.
	gitOps(t, root, "branch", "graph/done-ab12", "HEAD")
	// An unmerged claim branch: carries a commit mainline does not have.
	gitOps(t, root, "checkout", "-q", "-b", "graph/wip-cd34")
	if err := os.WriteFile(filepath.Join(root, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOps(t, root, "add", "wip.txt")
	gitOps(t, root, "commit", "-q", "-m", "wip work")
	gitOps(t, root, "checkout", "-q", mainline)
	// A merged branch checked out in a worktree held by an ACTIVE claim:
	// the claim record is what keeps gc's workspace pass from reaping the
	// worktree first (an unreferenced ws-* dir is an orphan by definition),
	// and the live checkout is what keeps the branch pass off the branch.
	gitOps(t, root, "worktree", "add", "-q", "-b", "graph/active-ef56",
		filepath.Join(root, "Plans", "SamplePlan", ".graph", "ws-active-claim"), "HEAD")
	if _, err := gstore.Update(gstore.PathFor(filepath.Join(root, "Plans", "SamplePlan")), func(g *model.Graph) error {
		g.NodeByID("big").Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z",
			Workspace: "Plans/SamplePlan/.graph/ws-active-claim"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A merged NON-graph branch: out of gc's jurisdiction.
	gitOps(t, root, "branch", "feature-x", "HEAD")

	// The independent definition: graph/* branches, tip an ancestor of
	// mainline HEAD, checked out nowhere.
	expectPruned := map[string]bool{}
	for _, line := range strings.Split(gitOps(t, root, "branch", "--list", "graph/*",
		"--format=%(refname:short)|%(worktreepath)"), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 || parts[1] != "" {
			continue // checked out somewhere: never prunable
		}
		name := parts[0]
		cmd := exec.Command("git", "merge-base", "--is-ancestor", name, "HEAD")
		cmd.Dir = root
		if cmd.Run() == nil {
			expectPruned[name] = true
		}
	}
	if !expectPruned["graph/done-ab12"] || len(expectPruned) != 1 {
		t.Fatalf("fixture self-check: independent prune set = %v, want exactly graph/done-ab12", expectPruned)
	}

	res, err := GC(root, root, "SamplePlan")
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	_ = res

	if branchExists(t, root, "graph/done-ab12") {
		t.Error("the merged, unclaimed claim branch must be pruned")
	}
	if !branchExists(t, root, "graph/wip-cd34") {
		t.Error("an unmerged claim branch must survive (it is the only reference to that work)")
	}
	if !branchExists(t, root, "graph/active-ef56") {
		t.Error("a branch checked out in a worktree must survive")
	}
	if !branchExists(t, root, "feature-x") {
		t.Error("non-graph branches are outside gc's jurisdiction")
	}
}
