package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
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
				Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_big", File: "t.ext"}}},
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

// TestRetireTombstonesAnId: the register accepts ids that never were graph
// nodes (a superseded v1 task id), refuses live nodes and duplicates, and
// stays sorted append-only.
func TestRetireTombstonesAnId(t *testing.T) {
	_, planDir := fixtureRoot(t)

	if err := Retire(planDir, "3.3"); err != nil {
		t.Fatalf("retiring a never-a-node id must succeed: %v", err)
	}
	if err := Retire(planDir, "3.3"); err == nil || !strings.Contains(err.Error(), "already retired") {
		t.Fatalf("duplicate retirement must refuse: %v", err)
	}
	if err := Retire(planDir, "big"); err == nil || !strings.Contains(err.Error(), "live node") {
		t.Fatalf("retiring a live node must refuse: %v", err)
	}
	if err := Retire(planDir, "  "); err == nil {
		t.Fatal("an empty id must refuse")
	}
	if err := Retire(planDir, "1.9"); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if len(g.Retired) != 2 || g.Retired[0] != "1.9" || g.Retired[1] != "3.3" {
		t.Fatalf("register must stay sorted append-only: %v", g.Retired)
	}
}

// --- split intent-anchoring fixtures and tests ---------------------------------

const hashingSpec = `---
title: "Sample Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Sample Spec

## Functional Requirements

- **FR-01**: The loader SHALL accept every documented key.

## Acceptance Criteria

- [ ] **AC-01**: A valid config loads with zero findings.
- [ ] **AC-02**: An unknown key names itself in the refusal.
`

const hashingDecisions = `---
title: "Decisions"
type: decision-log
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
decisions:
  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-08-01
    decided_by: user
    statement: "An accepted truth."
    scope: []
---

# Decisions
`

const hashingPlan = `---
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

// hashingFixture builds a root whose graph is finding-free: an anchored
// parent node (justifies FR-01, AC-01, AC-02) plus a terminal full review
// gate covering it. Split retires the parent, so this is the "before" state.
func hashingFixture(t *testing.T) (root, planDir string) {
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
	write("Specs/Sample/README.md", hashingSpec)
	write("Decisions/decisions.md", hashingDecisions)
	write("Plans/SamplePlan/README.md", hashingPlan)
	planDir = filepath.Join(root, "Plans", "SamplePlan")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	sources, err := gcompile.NewSources(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	big := model.Node{ID: "big", Contract: "does too much", Justifies: []string{"FR-01", "AC-01", "AC-02"},
		Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_big", File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1}
	sources.Anchor(&big)
	gate := model.Node{ID: "gate", Contract: "survives review", Justifies: []string{"AC-01"}, Deps: []string{"big"},
		Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1}
	sources.Anchor(&gate)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, big, gate)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

// TestSplitAnchorsChildrenOwnHashes: split embeds each child's OWN
// justifications — bare, qualified, and D-only — and never copies the
// parent's map (a citation the child does not carry must not ride along).
func TestSplitAnchorsChildrenOwnHashes(t *testing.T) {
	root, planDir := hashingFixture(t)
	children := `{
  "version": 1,
  "nodes": [
    {"id": "big-bare", "contract": "bare", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t1", "file": "t.ext"}]}, "hazards": []},
    {"id": "big-qual", "contract": "qualified", "justifies": ["Sample:AC-02"],
     "gate": {"type": "tests", "tests": [{"id": "t2", "file": "t.ext"}]}, "hazards": []},
    {"id": "big-d", "contract": "decision", "justifies": ["D-0001"],
     "gate": {"type": "tests", "tests": [{"id": "t3", "file": "t.ext"}]}, "hazards": []}
  ]
}
`
	if _, err := Split(root, root, "SamplePlan", "big", []byte(children)); err != nil {
		t.Fatalf("split: %v", err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(g.NodeByID("big-bare").IntentHashes["AC-01"], "sha256:") {
		t.Fatalf("bare citation must fingerprint: %+v", g.NodeByID("big-bare").IntentHashes)
	}
	if !strings.HasPrefix(g.NodeByID("big-qual").IntentHashes["Sample:AC-02"], "sha256:") {
		t.Fatalf("qualified citation must fingerprint under its written spelling: %+v", g.NodeByID("big-qual").IntentHashes)
	}
	if len(g.NodeByID("big-d").IntentHashes) != 0 {
		t.Fatalf("a D-only child must carry no fingerprints: %+v", g.NodeByID("big-d").IntentHashes)
	}
	// Non-inheritance: the parent cited FR-01, but no child does — so no
	// child may carry the parent's FR-01 hash.
	for _, id := range []string{"big-bare", "big-qual", "big-d"} {
		if g.NodeByID(id).IntentHashes["FR-01"] != "" {
			t.Fatalf("child %s must not inherit the parent's FR-01 hash", id)
		}
	}
}

// TestSplitChildGoesStaleOnSourceDrift: the child's hash is a real anchor,
// so a source edit after the split derives the (verified) child INTENT-STALE.
func TestSplitChildGoesStaleOnSourceDrift(t *testing.T) {
	root, planDir := hashingFixture(t)
	children := `{
  "version": 1,
  "nodes": [
    {"id": "big-bare", "contract": "bare", "justifies": ["AC-01"],
     "gate": {"type": "tests", "tests": [{"id": "t1", "file": "t.ext"}]}, "hazards": []},
    {"id": "big-qual", "contract": "qualified", "justifies": ["AC-02"],
     "gate": {"type": "tests", "tests": [{"id": "t2", "file": "t.ext"}]}, "hazards": []},
    {"id": "big-d", "contract": "decision", "justifies": ["D-0001"],
     "gate": {"type": "tests", "tests": [{"id": "t3", "file": "t.ext"}]}, "hazards": []}
  ]
}
`
	if _, err := Split(root, root, "SamplePlan", "big", []byte(children)); err != nil {
		t.Fatalf("split: %v", err)
	}
	// Record a pass on the child so the intent axis is evaluated.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("big-bare").Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Drift AC-01's wording.
	drifted := strings.Replace(hashingSpec, "A valid config loads with zero findings.", "A valid config loads with zero complaints.", 1)
	if drifted == hashingSpec {
		t.Fatal("fixture drift did not apply")
	}
	if err := os.WriteFile(filepath.Join(root, "Specs", "Sample", "README.md"), []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}

	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := gcompile.LoadIntentSnapshot(root, root, "SamplePlan")
	if err != nil {
		t.Fatal(err)
	}
	st := states.Derive(states.Inputs{Graph: g, CurrentIntentHashes: snap.Hashes(), DecisionExemptions: snap.Exemptions})
	ns := st["big-bare"]
	if ns.State != states.Stale || len(ns.IntentStale) != 1 || ns.IntentStale[0] != "AC-01" {
		t.Fatalf("a split child whose source drifted must derive INTENT-STALE: %+v", ns)
	}
}

// --- repair-intent fixtures and tests -----------------------------------------

// repairRoot builds a root with two related specs (so a bare AC-01 is
// ambiguous), FR/AC ids in Sample, and an accepted decision, plus an
// initialized empty graph. Individual tests seed the nodes they need.
func repairRoot(t *testing.T) (root, planDir string) {
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
	other := `---
title: "Other Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Other Spec

## Acceptance Criteria

- [ ] **AC-01**: The other spec's criterion one.
`
	write("planning-config.json", `{"planningRoot": "."}`)
	write("Specs/Sample/README.md", hashingSpec)
	write("Specs/Other/README.md", other)
	write("Decisions/decisions.md", hashingDecisions)
	plan := strings.Replace(hashingPlan, "related: [Specs/Sample]", "related: [Specs/Sample, Specs/Other]", 1)
	write("Plans/SamplePlan/README.md", plan)
	planDir = filepath.Join(root, "Plans", "SamplePlan")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

func repNode(id string, justifies []string, hashes map[string]string) model.Node {
	return model.Node{ID: id, Contract: "c", Justifies: justifies, IntentHashes: hashes,
		Gate:    model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "test_" + id, File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1}
}

func TestRepairIntentSelectedNodeBackfillsOnlyThatNode(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes,
			repNode("missing", []string{"AC-02"}, nil),
			repNode("other-missing", []string{"AC-02"}, nil),
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res, err := RepairIntent(root, root, "SamplePlan", "missing", false)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(res.Changes) != 1 || res.Changes[0].Node != "missing" || res.Changes[0].Cited != "AC-02" {
		t.Fatalf("changes: %+v", res.Changes)
	}
	if !strings.HasPrefix(res.Changes[0].Hash, "sha256:") {
		t.Fatalf("the backfilled hash must be a real fingerprint: %q", res.Changes[0].Hash)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("missing").IntentHashes["AC-02"] == "" {
		t.Fatal("the selected node must be repaired")
	}
	if g.NodeByID("other-missing").IntentHashes != nil {
		t.Fatal("an unselected node must be left alone")
	}
}

func TestRepairIntentWholeGraphRefusesAtomically(t *testing.T) {
	root, planDir := repairRoot(t)
	pass := func(id string) model.Node {
		n := repNode(id, []string{"AC-02"}, nil)
		return n
	}
	claimed := pass("claimed")
	claimed.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
	verified := pass("verified")
	verified.Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}
	redseq := pass("redseq")
	redseq.RedSeqs = map[string]int{"test_redseq": 2}
	ambiguous := repNode("ambiguous", []string{"AC-02", "AC-01"}, nil) // AC-01 bare is ambiguous here
	unresolved := repNode("unresolved", []string{"AC-02", "AC-99"}, nil)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, repNode("missing", []string{"AC-02"}, nil),
			claimed, verified, redseq, ambiguous, unresolved)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	_, err = RepairIntent(root, root, "SamplePlan", "", false)
	if err == nil {
		t.Fatal("a whole-graph repair with ineligible candidates must refuse")
	}
	for _, want := range []string{
		"is claimed by",
		"recorded verification",
		"red observations",
		"more than one related source",
		"resolves in no related spec",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q:\n%s", want, err)
		}
	}
	after, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused repair must write nothing")
	}
}

func TestRepairIntentIdempotent(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes,
			repNode("missing", []string{"AC-02"}, nil),
			repNode("partial", []string{"FR-01", "AC-02"}, map[string]string{"FR-01": "sha256:existing"}),
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res, err := RepairIntent(root, root, "SamplePlan", "", false)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(res.Changes) != 2 {
		t.Fatalf("first repair must backfill both missing entries: %+v", res.Changes)
	}
	// The existing FR-01 hash must be untouched (never overwritten).
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("partial").IntentHashes["FR-01"] != "sha256:existing" {
		t.Fatal("a nonempty hash must never be overwritten")
	}
	// Second run: zero changes.
	res2, err := RepairIntent(root, root, "SamplePlan", "", false)
	if err != nil {
		t.Fatalf("idempotent re-run must not error: %v", err)
	}
	if len(res2.Changes) != 0 || len(res2.Repaired) != 0 {
		t.Fatalf("idempotent re-run must report zero changes: %+v", res2)
	}
}

func TestRepairIntentDryRunByteIdentical(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, repNode("missing", []string{"AC-02"}, nil))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	res, err := RepairIntent(root, root, "SamplePlan", "", true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !res.DryRun || len(res.Changes) != 1 || res.Changes[0].Node != "missing" {
		t.Fatalf("dry-run must plan the same changes without writing: %+v", res)
	}
	after, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a dry-run must not change the graph")
	}
}

func TestRepairIntentPreservesStaleHashAndDOnly(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes,
			repNode("stale", []string{"FR-01"}, map[string]string{"FR-01": "sha256:STALE"}),
			repNode("d-only", []string{"D-0001"}, nil),
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res, err := RepairIntent(root, root, "SamplePlan", "", false)
	if err != nil {
		t.Fatalf("a stale-but-anchored node and a D-only node must not refuse: %v", err)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("nothing is repairable here: %+v", res.Changes)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("stale").IntentHashes["FR-01"] != "sha256:STALE" {
		t.Fatal("a nonempty (stale) hash must be preserved")
	}
}

func TestRepairIntentNamedNodeMustExist(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := RepairIntent(root, root, "SamplePlan", "nope", false); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("a named --node must exist: %v", err)
	}
	_ = planDir
}

// TestRepairIntentRefusesAmbiguousOrUnresolvedEvenWithoutMissingHash: a node
// whose ONLY citation is ambiguous or unresolved — or whose fingerprintable
// citation is already anchored alongside an ambiguous/unresolved one — must
// refuse atomically. needsRepair is false for all of these, so the guard is
// that the refusal does not depend on there being a missing hash to backfill.
func TestRepairIntentRefusesAmbiguousOrUnresolvedEvenWithoutMissingHash(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes,
			repNode("only-ambiguous", []string{"AC-01"}, nil),                                           // bare AC-01 is ambiguous here
			repNode("only-unknown", []string{"AC-99"}, nil),                                             // resolves nowhere
			repNode("mixed", []string{"FR-01", "AC-01"}, map[string]string{"FR-01": "sha256:existing"}), // anchored + ambiguous
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	_, err = RepairIntent(root, root, "SamplePlan", "", false)
	if err == nil {
		t.Fatal("a graph carrying only ambiguous/unresolved citations must refuse")
	}
	for _, want := range []string{"more than one related source", "resolves in no related spec"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q:\n%s", want, err)
		}
	}
	after, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused repair must write nothing")
	}
}

// TestRepairIntentSelectedNodeRefusesAmbiguousOnly: --node on a node whose
// only citation is ambiguous refuses, and leaves the graph byte-identical.
func TestRepairIntentSelectedNodeRefusesAmbiguousOnly(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, repNode("only-ambiguous", []string{"AC-01"}, nil))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	_, err = RepairIntent(root, root, "SamplePlan", "only-ambiguous", false)
	if err == nil || !strings.Contains(err.Error(), "more than one related source") {
		t.Fatalf("a selected ambiguous-only node must refuse: %v", err)
	}
	after, err := os.ReadFile(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused selected-node repair must write nothing")
	}
}

// contendWith wraps the real store.Update so the mutation fn, on its FIRST
// invocation, also lands a competing mutation (the compete callback) before
// that attempt's compare-and-swap write. The outer write therefore collides on
// digest, the store re-reads the fresh graph, and the mutation must re-plan
// against the competing state — the exact read-modify-write race the
// compare-and-swap exists to close. No sleeps: the nested update completes
// synchronously inside the first attempt.
func contendWith(t *testing.T, path string, compete func(*model.Graph) error) updateFunc {
	t.Helper()
	var done bool
	return func(path string, fn func(*model.Graph) error) (*model.Graph, error) {
		wrapped := func(g *model.Graph) error {
			if done {
				return fn(g)
			}
			done = true
			if err := fn(g); err != nil {
				return err
			}
			if _, err := gstore.Update(path, compete); err != nil {
				return err
			}
			return nil
		}
		return gstore.Update(path, wrapped)
	}
}

// TestRepairIntentRefusesCompetingClaimOnCASRetry drives a real digest
// collision: the first attempt computes the repair, then a competing claim
// lands via a nested store.Update before that attempt's write. The retry must
// re-plan against the freshly claimed node, refuse, and preserve the competing
// write (claim present, no backfilled hash).
func TestRepairIntentRefusesCompetingClaimOnCASRetry(t *testing.T) {
	root, planDir := repairRoot(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, repNode("missing", []string{"AC-02"}, nil))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := gstore.PathFor(planDir)
	_, err := repairIntentWith(root, root, "SamplePlan", "missing", false,
		contendWith(t, path, func(g *model.Graph) error {
			g.NodeByID("missing").Claim = &model.Claim{By: "racer", LeaseExpires: "2099-01-01T00:00:00Z"}
			return nil
		}))
	if err == nil || !strings.Contains(err.Error(), "is claimed by") {
		t.Fatalf("the retry must re-plan against the competing claim and refuse: %v", err)
	}
	g, err := gstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID("missing")
	if n.Claim == nil || n.Claim.By != "racer" {
		t.Fatalf("the competing claim must be preserved: %+v", n.Claim)
	}
	if len(n.IntentHashes) != 0 {
		t.Fatalf("the repair must not backfill over a competing claim: %+v", n.IntentHashes)
	}
}

// TestSplitRefusesCompetingClaimOnCASRetry drives the same collision for split:
// the first attempt computes the split, a competing claim lands on the parent,
// and the retry must refuse and leave the freshly claimed parent in place.
func TestSplitRefusesCompetingClaimOnCASRetry(t *testing.T) {
	root, planDir := fixtureRoot(t)
	path := gstore.PathFor(planDir)
	_, err := splitWith(root, root, "SamplePlan", "big", []byte(splitChildren),
		contendWith(t, path, func(g *model.Graph) error {
			g.NodeByID("big").Claim = &model.Claim{By: "racer", LeaseExpires: "2099-01-01T00:00:00Z"}
			return nil
		}))
	if err == nil || !strings.Contains(err.Error(), "claimed") {
		t.Fatalf("the retry must re-plan against the competing claim and refuse: %v", err)
	}
	g, err := gstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("big") == nil {
		t.Fatal("the freshly claimed parent must not be removed")
	}
	if g.NodeByID("big").Claim == nil || g.NodeByID("big").Claim.By != "racer" {
		t.Fatal("the competing claim must be preserved")
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
		t.Fatal("only the holder edits a claimed node's artifacts")
	}
	if err := SetArtifacts(planDir, "big", "holder", narrowed); err != nil {
		t.Fatalf("holder set-artifacts: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if got := g.NodeByID("big").Artifacts; !reflect.DeepEqual(got, narrowed) {
		t.Fatalf("artifacts: %v", got)
	}

	// An unclaimed node is editable by anyone (same rule as set-tests).
	if err := SetArtifacts(planDir, "helper", "", []string{"src/helper.rs"}); err != nil {
		t.Fatalf("unclaimed set-artifacts: %v", err)
	}

	if err := SetArtifacts(planDir, "missing", "holder", narrowed); err == nil {
		t.Fatal("a nonexistent node refuses")
	}
	if err := SetArtifacts(planDir, "big", "holder", nil); err == nil {
		t.Fatal("an empty write-set anchors nothing")
	}
	if err := SetArtifacts(planDir, "big", "holder", []string{"  "}); err == nil {
		t.Fatal("a blank artifact path refuses")
	}
	if err := SetArtifacts(planDir, "big", "holder", []string{"a.rs", "a.rs"}); err == nil {
		t.Fatal("duplicate artifact paths refuse")
	}
	if err := SetArtifacts(planDir, "feature-gate", "", narrowed); err == nil {
		t.Fatal("a review gate's recorded digests are the reviewed diff, not a declared write-set")
	}
}

// TestSetArtifactsNarrowingHealsDirectoryOverlapStaleness is the verb's
// reason to exist end to end: a node whose over-broad directory write-set
// was re-staled by a descendant's verified merge derives GREEN again once
// the declaration is narrowed to the files the node actually owns — with
// the recorded observation untouched.
func TestSetArtifactsNarrowingHealsDirectoryOverlapStaleness(t *testing.T) {
	root, planDir := fixtureRoot(t)

	// The ancestor "helper" verified a directory write-set plus one owned
	// file; a descendant then legitimately added a file under the same
	// directory, so the directory's digest no longer matches the record.
	if err := os.MkdirAll(filepath.Join(root, "crates", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "crates", "a", "lib.rs"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[workspace]"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := digest.New(root)
	recordedDir := d.Artifact("crates/")
	recordedFile := d.Artifact("Cargo.toml")
	if recordedDir == "" || recordedFile == "" {
		t.Fatal("fixture digests must record")
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID("helper")
		n.Artifacts = []string{"crates/", "Cargo.toml"}
		n.Verification = &model.Verification{
			Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
			ArtifactDigests: map[string]string{"crates/": recordedDir, "Cargo.toml": recordedFile},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The descendant's verified merge adds a file under crates/.
	if err := os.WriteFile(filepath.Join(root, "crates", "a", "new.rs"), []byte("descendant"), 0o644); err != nil {
		t.Fatal(err)
	}
	derive := func() states.NodeState {
		g, err := gstore.Load(gstore.PathFor(planDir))
		if err != nil {
			t.Fatal(err)
		}
		s := states.Derive(states.Inputs{Graph: g, ArtifactDigest: digest.New(root).Artifact})
		return s["helper"]
	}
	if ns := derive(); ns.State != states.Stale || len(ns.DigestStale) != 1 || ns.DigestStale[0] != "crates/" {
		t.Fatalf("directory overlap must derive STALE on crates/: %+v", ns)
	}

	// Narrowing the write-set to the owned file heals the derive without
	// touching the observation.
	if err := SetArtifacts(planDir, "helper", "", []string{"Cargo.toml"}); err != nil {
		t.Fatalf("set-artifacts: %v", err)
	}
	if ns := derive(); ns.State != states.Green {
		t.Fatalf("narrowed write-set must derive GREEN: %+v", ns)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	v := g.NodeByID("helper").Verification
	if v == nil || v.ArtifactDigests["crates/"] != recordedDir {
		t.Fatal("the recorded observation is history and must remain untouched")
	}
}

// TestRehashAcknowledgesCosmeticIntentDrift covers the INTENT-STALE exit
// path: after the walker judges a cited requirement's diff cosmetic, rehash
// re-embeds the current fingerprint — and nothing else.
func TestRehashAcknowledgesCosmeticIntentDrift(t *testing.T) {
	root, planDir := fixtureRoot(t)

	// Embed a deliberately stale fingerprint for the citation "AC-01".
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID("big")
		n.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
		n.IntentHashes = map[string]string{"AC-01": "sha256:stale"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := Rehash(root, root, "SamplePlan", "big", "impostor", nil); err == nil {
		t.Fatal("only the holder rehashes a claimed node")
	}
	updated, err := Rehash(root, root, "SamplePlan", "big", "holder", nil)
	if err != nil {
		t.Fatalf("holder rehash: %v", err)
	}
	if len(updated) != 1 || updated[0] != "AC-01" {
		t.Fatalf("updated: %v", updated)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	fresh := g.NodeByID("big").IntentHashes["AC-01"]
	if fresh == "sha256:stale" || !strings.HasPrefix(fresh, "sha256:") {
		t.Fatalf("fingerprint must be re-embedded from current sources: %q", fresh)
	}

	// Idempotent: a second rehash reports no drift.
	updated, err = Rehash(root, root, "SamplePlan", "big", "holder", nil)
	if err != nil {
		t.Fatalf("idempotent rehash: %v", err)
	}
	if len(updated) != 0 {
		t.Fatalf("no drift expected on second rehash: %v", updated)
	}

	// Refusals: unknown node, uncited key, unresolvable citation.
	if _, err := Rehash(root, root, "SamplePlan", "missing", "", nil); err == nil {
		t.Fatal("a nonexistent node refuses")
	}
	if _, err := Rehash(root, root, "SamplePlan", "big", "holder", []string{"AC-77"}); err == nil {
		t.Fatal("a citation the node does not fingerprint refuses")
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("big").IntentHashes["AC-99"] = "sha256:orphaned"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Rehash(root, root, "SamplePlan", "big", "holder", []string{"AC-99"}); err == nil {
		t.Fatal("a citation that no longer resolves is a replan signal, not a rehash")
	}
}

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

// TestSplitRefusesUnreachableDesignCitationAtomically: review children
// citing a design's DD ids compile only when the plan relates the design
// directly. With the design reachable only through the spec's back-link the
// split refuses — naming the fix — and the graph stays byte-identical
// (parent present, no children). Relating the design from the plan makes
// the same split succeed.
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
	children := strings.Replace(splitChildren, `"big-render", "contract": "renders the output", "justifies": ["AC-01"]`,
		`"big-render", "contract": "renders the output", "justifies": ["AC-01", "Designs/Sample:DD-1"]`, 1)
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
	for _, want := range []string{
		`big-render: cites "Designs/Sample:DD-1", which resolves in no related spec, design, or decision ledger`,
		"Designs/Sample/README.md defines it but is not reachable through the plan's `related` graph",
		"relate it directly from Plans/SamplePlan/README.md",
	} {
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

	// Representation A: relate the design directly from the plan.
	write("Plans/SamplePlan/README.md", strings.Replace(opsPlan, "related: [Specs/Sample]", "related: [Specs/Sample, Designs/Sample]", 1))
	res, err := Split(root, root, "SamplePlan", "big", []byte(children))
	if err != nil {
		t.Fatalf("split with the design directly related must succeed: %v", err)
	}
	if res.Retired != "big" || len(res.Children) != 2 {
		t.Fatalf("result: %+v", res)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("big-render").IntentHashes["Designs/Sample:DD-1"] == "" {
		t.Fatal("the qualified DD citation must anchor its fingerprint")
	}
}
