package review

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
)

func work(id string, deps []string, artifacts ...string) model.Node {
	return model.Node{ID: id, Contract: "does " + id, Gate: model.Gate{Type: model.GateTests,
		Tests: []model.Test{{ID: "test_" + id, File: "t.ext"}}},
		Hazards: model.Hazards{}, Estimate: 1, Deps: deps, Artifacts: artifacts}
}

func fullGate(id string, deps []string) model.Node {
	return model.Node{ID: id, Contract: id + " survives review", Gate: model.Gate{Type: model.GateReview},
		Hazards: model.Hazards{}, Estimate: 1, Deps: deps}
}

func subsetGate(id string, deps []string, lanes ...string) model.Node {
	n := fullGate(id, deps)
	n.Gate.Lanes = lanes
	return n
}

func pass(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean}
}

// fixture writes a graph into a temp planning root and returns (root, planDir).
func fixture(t *testing.T, seq int, nodes ...model.Node) (string, string) {
	t.Helper()
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: model.SchemaVersion, SeqCounter: seq, Nodes: nodes}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// artifactText builds a review artifact with the frontmatter shape the
// scaffold/resolve flow produces.
func artifactText(status string, frozen bool, verdict string, lanes map[string]string, findingsBlock string) string {
	var b strings.Builder
	b.WriteString("---\ntitle: \"Gate review\"\ntype: review\nstatus: " + status + "\n")
	fmt.Fprintf(&b, "frozen: %v\nverdict: %s\nreview_mode: single-agent\nlane_results:\n", frozen, verdict)
	// Deterministic lane order for readability; map order is irrelevant.
	for _, lane := range model.ReviewLanes {
		if res, ok := lanes[lane]; ok {
			fmt.Fprintf(&b, "  - lane: %s\n    result: %s\n    evidence: \"looked\"\n", lane, res)
		}
	}
	if findingsBlock == "" {
		b.WriteString("findings: []\n")
	} else {
		b.WriteString("findings:\n" + findingsBlock)
	}
	b.WriteString("---\n\n# Gate review\n\nBody.\n")
	return b.String()
}

func allPass() map[string]string {
	m := map[string]string{}
	for _, lane := range model.ReviewLanes {
		m[lane] = "PASS/Aligned"
	}
	return m
}

func deriveWithDigests(t *testing.T, root, planDir string) map[string]states.NodeState {
	t.Helper()
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	return states.Derive(states.Inputs{Graph: g, ArtifactDigest: digest.New(root).Artifact})
}

func TestScopeNestedGatesDisjointCover(t *testing.T) {
	a, b := work("a", nil, "src/a.ext"), work("b", nil, "src/b.ext")
	g1 := fullGate("g1", []string{"a", "b"})
	g1.Verification = pass(3)
	c := work("c", []string{"g1"}, "src/c.ext")
	d := work("d", []string{"c"}, "src/d.ext")
	g2 := fullGate("g2", []string{"c", "d"})
	_, planDir := fixture(t, 3, a, b, g1, c, d, g2)
	g, _ := gstore.Load(gstore.PathFor(planDir))

	s1, err := Scope(g, "g1")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Scope(g, "g2")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s1, []string{"a", "b"}) || !reflect.DeepEqual(s2, []string{"c", "d"}) {
		t.Fatalf("nested scopes must be disjoint increments: %v %v", s1, s2)
	}
	// Union property: scopes plus the inner gate node cover g2's closure.
	union := map[string]bool{"g1": true}
	for _, id := range append(append([]string{}, s1...), s2...) {
		if union[id] {
			t.Fatalf("scopes overlap on %s", id)
		}
		union[id] = true
	}
	for _, id := range []string{"a", "b", "g1", "c", "d"} {
		if !union[id] {
			t.Fatalf("union misses %s", id)
		}
	}

	// An UNrecorded inner gate does not subtract: its region is unreviewed.
	g.NodeByID("g1").Verification = nil
	s2, _ = Scope(g, "g2")
	if !reflect.DeepEqual(s2, []string{"a", "b", "c", "d", "g1"}) {
		t.Fatalf("an unrecorded inner gate's region belongs to the outer scope: %v", s2)
	}

	// A recorded SUBSET gate does not subtract either: only full gates cover.
	g.NodeByID("g1").Gate.Lanes = model.Lanes{"review_quality"}
	g.NodeByID("g1").Verification = pass(3)
	s2, _ = Scope(g, "g2")
	if !reflect.DeepEqual(s2, []string{"a", "b", "c", "d", "g1"}) {
		t.Fatalf("a subset gate confers no coverage: %v", s2)
	}

	if _, err := Scope(g, "nope"); err == nil {
		t.Fatal("unknown node must refuse")
	}
	if _, err := Scope(g, "a"); err == nil {
		t.Fatal("a non-review node has no scope")
	}
}

func TestScopeThreeLevelNesting(t *testing.T) {
	a := work("a", nil)
	g1 := fullGate("g1", []string{"a"})
	g1.Verification = pass(2)
	b := work("b", []string{"g1"})
	g2 := fullGate("g2", []string{"b"})
	g2.Verification = pass(4)
	c := work("c", []string{"g2"})
	g3 := fullGate("g3", []string{"c"})
	_, planDir := fixture(t, 4, a, g1, b, g2, c, g3)
	g, _ := gstore.Load(gstore.PathFor(planDir))

	s3, err := Scope(g, "g3")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s3, []string{"c"}) {
		t.Fatalf("everything at or below a recorded full gate is covered, transitively: %v", s3)
	}
}

func TestRecordRefusesUnfrozenArtifacts(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	root, _ := fixture(t, 0, a, gate)
	writeFile(t, root, "reviews/r.md", artifactText("open", false, "Misaligned", allPass(), ""))

	_, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err == nil {
		t.Fatal("an unfrozen artifact must refuse")
	}
	for _, signal := range []string{"status is \"open\"", "frozen is not true", "verdict is \"Misaligned\""} {
		if !strings.Contains(err.Error(), signal) {
			t.Fatalf("the refusal must name every failing signal, missing %q: %v", signal, err)
		}
	}

	// Each signal alone is insufficient — resolved but reopened (frozen
	// false) is exactly the D-0020 trap.
	writeFile(t, root, "reviews/r2.md", artifactText("resolved", false, "Aligned", allPass(), ""))
	_, err = Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r2.md")})
	if err == nil || !strings.Contains(err.Error(), "frozen is not true") {
		t.Fatalf("resolved-but-not-frozen must refuse naming frozen: %v", err)
	}
}

func TestRecordLaneConformance(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	root, _ := fixture(t, 0, a, gate)

	// Full gate, one lane missing.
	partial := allPass()
	delete(partial, "review_blind_spots")
	writeFile(t, root, "reviews/missing.md", artifactText("resolved", true, "Aligned", partial, ""))
	_, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "missing.md")})
	if err == nil || !strings.Contains(err.Error(), "review_blind_spots is absent") {
		t.Fatalf("a full gate needs all four lanes: %v", err)
	}

	// Full gate, one lane failing.
	failing := allPass()
	failing["review_quality"] = "FAIL/Drifted"
	writeFile(t, root, "reviews/failing.md", artifactText("resolved", true, "Aligned", failing, ""))
	_, err = Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "failing.md")})
	if err == nil || !strings.Contains(err.Error(), "review_quality reports") {
		t.Fatalf("a failing lane must refuse: %v", err)
	}

	// Subset gate: only the named lanes are required.
	subsetRoot, subsetPlanDir := fixture(t, 0, work("a", nil),
		subsetGate("g1", []string{"a"}, "review_quality"))
	writeFile(t, subsetRoot, "reviews/subset.md", artifactText("resolved", true, "Aligned",
		map[string]string{"review_quality": "PASS/Aligned"}, ""))
	res, err := Record(Options{Root: subsetRoot, RepoRoot: subsetRoot, Plan: "P", Node: "g1",
		Artifact: filepath.Join(subsetRoot, "reviews", "subset.md")})
	if err != nil {
		t.Fatalf("a subset gate needs only its named lanes: %v", err)
	}
	if res.Observation == nil || res.Observation.Result != model.ResultPass {
		t.Fatalf("subset gate records a pass: %+v", res)
	}
	g, _ := gstore.Load(gstore.PathFor(subsetPlanDir))
	if g.NodeByID("g1").Verification == nil {
		t.Fatal("the observation must persist")
	}
}

func TestRecordGreensGateAndStalesOnDrift(t *testing.T) {
	a := work("a", nil, "src/a.ext")
	a.Verification = pass(1)
	b := work("b", nil, "src/b.ext")
	b.Verification = pass(2)
	gate := fullGate("g1", []string{"a", "b"})
	root, planDir := fixture(t, 2, a, b, gate)
	writeFile(t, root, "src/a.ext", "content a")
	writeFile(t, root, "src/b.ext", "content b")
	// The work observations must anchor the CURRENT bytes or the members
	// derive digest-stale themselves.
	d := digest.New(root)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("a").Verification.ArtifactDigests = map[string]string{"src/a.ext": d.Artifact("src/a.ext")}
		g.NodeByID("b").Verification.ArtifactDigests = map[string]string{"src/b.ext": d.Artifact("src/b.ext")}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(), ""))

	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !reflect.DeepEqual(res.Scope, []string{"a", "b"}) {
		t.Fatalf("scope: %v", res.Scope)
	}
	if len(res.Observation.ArtifactDigests) != 2 {
		t.Fatalf("the gate observation records the aggregate scope diff: %+v", res.Observation.ArtifactDigests)
	}
	if res.Observation.ReportDigest == "" {
		t.Fatal("the artifact's own digest anchors the observation")
	}

	st := deriveWithDigests(t, root, planDir)
	if st["g1"].State != states.Green {
		t.Fatalf("the gate derives GREEN from the frozen Aligned artifact: %+v", st["g1"])
	}

	// Drift one scope artifact: the reviewed diff is no longer the diff on
	// disk — the gate derives STALE via ordinary digest staleness.
	writeFile(t, root, "src/a.ext", "content a CHANGED")
	st = deriveWithDigests(t, root, planDir)
	if st["g1"].State != states.Stale || len(st["g1"].DigestStale) == 0 {
		t.Fatalf("scope drift must derive the gate STALE: %+v", st["g1"])
	}
}

func TestRecordDemotesNamedNodes(t *testing.T) {
	a := work("a", nil, "src/a.ext")
	a.Verification = pass(1)
	b := work("b", nil, "src/b.ext")
	b.Verification = pass(2)
	gate := fullGate("g1", []string{"a", "b"})
	root, planDir := fixture(t, 2, a, b, gate)
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(),
		"  - id: F-01\n    severity: major\n    title: \"a is faulted\"\n    status: deferred\n    nodes: [a]\n"))

	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !reflect.DeepEqual(res.Demoted, []string{"a"}) {
		t.Fatalf("demoted: %v", res.Demoted)
	}

	g, _ := gstore.Load(gstore.PathFor(planDir))
	demoted := g.NodeByID("a").Verification
	gateObs := g.NodeByID("g1").Verification
	if demoted.Result != model.ResultFail {
		t.Fatal("a named node carries a failing observation")
	}
	if !(demoted.Seq < gateObs.Seq) {
		t.Fatalf("demotions are seq-stamped before the gate's own observation: %d vs %d", demoted.Seq, gateObs.Seq)
	}
	st := states.Derive(states.Inputs{Graph: g})
	if st["a"].State != states.Red || !st["a"].Workable {
		t.Fatalf("a demoted node is RED and workable again: %+v", st["a"])
	}
	if st["b"].State != states.Green {
		t.Fatalf("an unnamed sibling keeps its state: %+v", st["b"])
	}
	if st["g1"].State != states.Green {
		t.Fatalf("the gate itself is GREEN until rework re-verifies: %+v", st["g1"])
	}

	// Rework re-verifies the demoted node: the gate goes STALE by seq —
	// its review predates the rework.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(fresh *model.Graph) error {
		fresh.SeqCounter++
		fresh.NodeByID("a").Verification = pass(fresh.SeqCounter)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g, _ = gstore.Load(gstore.PathFor(planDir))
	st = states.Derive(states.Inputs{Graph: g})
	if st["g1"].State != states.Stale || !st["g1"].SeqStale {
		t.Fatalf("rework must stale the gate by seq: %+v", st["g1"])
	}
}

func TestRecordRefusesOutOfScopeFindingNodes(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	unrelated := work("z", nil)
	root, _ := fixture(t, 0, a, gate, unrelated)
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(),
		"  - id: F-01\n    severity: major\n    title: \"names outsider\"\n    status: deferred\n    nodes: [z]\n"))

	_, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err == nil || !strings.Contains(err.Error(), "outside") || !strings.Contains(err.Error(), "F-01") {
		t.Fatalf("a finding naming an out-of-scope node must refuse, naming the finding: %v", err)
	}
}

func TestRecordClaimDiscipline(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	gate.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
	root, planDir := fixture(t, 0, a, gate)
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(), ""))
	artifact := filepath.Join(root, "reviews", "r.md")

	if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: artifact, By: "impostor"}); err == nil || !strings.Contains(err.Error(), "claimed by") {
		t.Fatalf("claim discipline: %v", err)
	}
	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: artifact, By: "holder"})
	if err != nil {
		t.Fatalf("holder records: %v", err)
	}
	if !res.Merged {
		t.Fatal("a recorded gate pass completes the holder's claim")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("g1").Claim != nil {
		t.Fatal("the claim must be cleared")
	}
}

func TestClosedPredicate(t *testing.T) {
	a := work("a", nil, "src/a.ext")
	a.Verification = pass(1)
	b := work("b", nil, "src/b.ext") // never verified: READY, not closed
	gate := fullGate("g1", []string{"a"})
	free := work("free", nil) // GREEN but covered by no gate
	free.Verification = pass(2)
	root, planDir := fixture(t, 2, a, b, gate, free)
	writeFile(t, root, "src/a.ext", "content a")
	d := digest.New(root)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("a").Verification.ArtifactDigests = map[string]string{"src/a.ext": d.Artifact("src/a.ext")}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(), ""))
	if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")}); err != nil {
		t.Fatal(err)
	}

	g, _ := gstore.Load(gstore.PathFor(planDir))
	st := states.Derive(states.Inputs{Graph: g, ArtifactDigest: digest.New(root).Artifact})
	closed := Closed(g, st)
	if !closed["a"] {
		t.Fatal("GREEN inside a GREEN frozen full gate's scope is closed")
	}
	if closed["free"] {
		t.Fatal("GREEN without full-gate coverage is assumed-closed, never closed")
	}
	if closed["b"] {
		t.Fatalf("non-GREEN members are not closed: %v", closed)
	}
	if !closed["g1"] {
		t.Fatal("a GREEN recorded full gate is closed by its own frozen review")
	}

	// Drift the reviewed artifact: the gate goes STALE, and closure is
	// withdrawn — a stale gate certifies nothing.
	writeFile(t, root, "src/a.ext", "content a CHANGED")
	st = states.Derive(states.Inputs{Graph: g, ArtifactDigest: digest.New(root).Artifact})
	closed = Closed(g, st)
	if closed["a"] {
		t.Fatal("a stale gate must withdraw closure")
	}
}
