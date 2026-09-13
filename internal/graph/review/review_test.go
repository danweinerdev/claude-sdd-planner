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
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
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
	return artifactTextFor("Plans/P/01-Ungrouped.md", status, frozen, verdict, lanes, findingsBlock)
}

func artifactTextFor(reviewOf, status string, frozen bool, verdict string, lanes map[string]string, findingsBlock string) string {
	var b strings.Builder
	b.WriteString("---\ntitle: \"Gate review\"\ntype: review\nstatus: " + status + "\n")
	fmt.Fprintf(&b, "review_of: \"%s\"\n", reviewOf)
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

// TestLaneResultAdmissionByVerdict covers the three cases the shared
// laneResultProblems admission rule must get right, exercised through both
// callers: Record (via AdmitArtifact) and Check. An Aligned verdict keeps
// the strict all-pass requirement; an Amend verdict additionally admits the
// truthful non-passing token rules.NonPassingLaneResult (CHANGES/Amend); an
// unrecognized or placeholder-shaped token stays refused under either
// verdict.
func TestLaneResultAdmissionByVerdict(t *testing.T) {
	newFixture := func(t *testing.T) (root string) {
		t.Helper()
		a := work("a", nil)
		gate := fullGate("g1", []string{"a"})
		root, _ = fixture(t, 0, a, gate)
		return root
	}
	revise := "  - id: F-01\n    severity: major\n    title: \"needs a fix\"\n    status: open\n    action: revise\n    nodes: [a]\n    revise:\n      contract: \"changed\"\n"

	t.Run("Aligned refuses a non-passing lane", func(t *testing.T) {
		root := newFixture(t)
		lanes := allPass()
		lanes["review_quality"] = "CHANGES/Amend"
		writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", lanes, ""))
		artPath := filepath.Join(root, "reviews", "r.md")

		if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artPath}); err == nil ||
			!strings.Contains(err.Error(), "review_quality reports") {
			t.Fatalf("Aligned with a CHANGES/Amend lane must refuse: %v", err)
		}
		res, err := Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artPath})
		if err != nil {
			t.Fatalf("check must accept a draft artifact regardless of lane results: %v", err)
		}
		found := false
		for _, p := range res.Problems {
			if strings.Contains(p, "review_quality reports") {
				found = true
			}
		}
		if !found {
			t.Fatalf("check must report the same non-passing-lane problem under Aligned: %+v", res.Problems)
		}
	})

	t.Run("Amend admits CHANGES/Amend", func(t *testing.T) {
		root := newFixture(t)
		lanes := allPass()
		lanes["review_quality"] = "CHANGES/Amend"
		writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Amend", lanes, revise))
		artPath := filepath.Join(root, "reviews", "r.md")

		res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artPath})
		if err != nil {
			t.Fatalf("Amend with a truthful CHANGES/Amend lane must be admitted: %v", err)
		}
		if res.Plan == nil || len(res.Plan.Amendments) != 1 {
			t.Fatalf("expected one previewed amendment: %+v", res)
		}
		checkRes, err := Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artPath})
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if len(checkRes.Problems) != 0 {
			t.Fatalf("check must report no lane problems for an admissible Amend artifact: %+v", checkRes.Problems)
		}
	})

	t.Run("unrecognized token stays refused under either verdict", func(t *testing.T) {
		for _, verdict := range []string{"Aligned", "Amend"} {
			verdict := verdict
			t.Run(verdict, func(t *testing.T) {
				root := newFixture(t)
				lanes := allPass()
				lanes["review_quality"] = "TODO/Unfilled"
				findings := ""
				if verdict == "Amend" {
					findings = revise
				}
				writeFile(t, root, "reviews/r.md", artifactText("resolved", true, verdict, lanes, findings))
				artPath := filepath.Join(root, "reviews", "r.md")

				if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artPath}); err == nil ||
					!strings.Contains(err.Error(), "review_quality reports") {
					t.Fatalf("a placeholder-shaped token must stay refused under %s: %v", verdict, err)
				}
				res, err := Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: artPath})
				if err != nil {
					t.Fatalf("check: %v", err)
				}
				found := false
				for _, p := range res.Problems {
					if strings.Contains(p, "review_quality reports") {
						found = true
					}
				}
				if !found {
					t.Fatalf("check must report the same refusal under %s: %+v", verdict, res.Problems)
				}
			})
		}
	})
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

func TestRecordOpenFindingsPreviewWithoutWriting(t *testing.T) {
	a := work("a", nil, "src/a.ext")
	a.Verification = pass(1)
	b := work("b", nil, "src/b.ext")
	b.Verification = pass(2)
	gate := fullGate("g1", []string{"a", "b"})
	root, planDir := fixture(t, 2, a, b, gate)
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Amend", allPass(),
		"  - id: F-01\n    severity: major\n    title: \"a is faulted\"\n    status: open\n    action: revise\n    nodes: [a]\n    revise:\n      contract: \"does a, and also handles the empty case\"\n"))
	before, _ := os.ReadFile(gstore.PathFor(planDir))

	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if res.Observation != nil || res.Plan == nil || res.ExpectDigest == "" || res.ExpectReportDigest == "" {
		t.Fatalf("open findings must preview, not record: %+v", res)
	}
	if res.ExpectReportDigest != res.Plan.ReportDigest {
		t.Fatalf("preview report fence must name the artifact used to plan amendments: %+v", res)
	}
	if len(res.Plan.Amendments) != 1 || res.Plan.Amendments[0].Action != ActionRevise || res.Plan.Amendments[0].Node != "a" || !reflect.DeepEqual(res.Plan.Amendments[0].Changed, []string{"contract"}) {
		t.Fatalf("plan = %+v", res.Plan.Amendments)
	}
	after, _ := os.ReadFile(gstore.PathFor(planDir))
	if string(before) != string(after) {
		t.Fatal("a preview must not write the graph")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	st := states.Derive(states.Inputs{Graph: g})
	if st["a"].State != states.Green || g.NodeByID("g1").Verification != nil {
		t.Fatalf("nothing changed on disk: a=%s", st["a"].State)
	}
}

func TestRecordNonOpenFindingsDoNotAmend(t *testing.T) {
	a := work("a", nil, "src/a.ext")
	a.Verification = pass(1)
	gate := fullGate("g1", []string{"a"})
	root, planDir := fixture(t, 1, a, gate)
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(),
		"  - id: F-01\n    severity: minor\n    title: \"deferred note\"\n    status: deferred\n    nodes: [a]\n"))
	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if res.Observation == nil || res.Plan != nil {
		t.Fatalf("a deferred finding is not an amendment: %+v", res)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Verification.Result != model.ResultPass {
		t.Fatal("no demotion: the named node keeps its pass")
	}
}

func TestRecordBindsReviewedSetAndStalesOnContractRevision(t *testing.T) {
	a := work("a", nil, "src/a.ext")
	a.Verification = pass(1)
	b := work("b", nil, "src/b.ext")
	b.Verification = pass(2)
	gate := fullGate("g1", []string{"a", "b"})
	root, planDir := fixture(t, 2, a, b, gate)
	writeFile(t, root, "src/a.ext", "content a")
	writeFile(t, root, "src/b.ext", "content b")
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(), ""))
	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	obs := res.Observation
	if obs.ContractRev != 1 || len(obs.Reviewed) != 2 || obs.Reviewed["a"].ContractRev != 1 || obs.Reviewed["a"].ArtifactDigests["src/a.ext"] == "" {
		t.Fatalf("reviewed set not recorded: %+v", obs)
	}
	if st := deriveWithDigests(t, root, planDir); st["g1"].State != states.Green {
		t.Fatalf("fresh review is GREEN: %+v", st["g1"])
	}
	// A contract-only revision of b, bytes untouched, stales the review.
	if _, err := gstore.Update(gstore.PathFor(planDir), func(fresh *model.Graph) error {
		fresh.NodeByID("b").ContractRev = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st := deriveWithDigests(t, root, planDir)
	if st["g1"].State != states.Stale || !reflect.DeepEqual(st["g1"].ReviewStale, []string{"b"}) {
		t.Fatalf("contract revision must stale the review even with identical bytes: %+v", st["g1"])
	}
	// b's own old pass is history now, not proof: b derives READY.
	if st["b"].State != states.Ready || !st["b"].RevIncompatible {
		t.Fatalf("revised node's prior pass is incompatible: %+v", st["b"])
	}
}

func TestRecordRefusesOutOfScopeFindingNodes(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	unrelated := work("z", nil)
	root, _ := fixture(t, 0, a, gate, unrelated)
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Amend", allPass(),
		"  - id: F-01\n    severity: major\n    title: \"names outsider\"\n    status: open\n    action: revise\n    nodes: [z]\n    revise:\n      contract: \"changed\"\n"))

	_, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err == nil || !strings.Contains(err.Error(), "outside") || !strings.Contains(err.Error(), "F-01") {
		t.Fatalf("a finding naming an out-of-scope node must refuse, naming the finding: %v", err)
	}
}

// Check validates an artifact's open-finding targets without requiring it to
// be resolved or frozen, and never records anything — an operator should be
// able to catch a finding naming a node outside the gate's dependency
// closure before scaffolding, evidencing, and freezing the artifact, a step
// that cannot be undone once the artifact is admitted.
func TestCheckReportsOutOfClosureTargetWithoutRecording(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	unrelated := work("z", nil)
	root, _ := fixture(t, 0, a, gate, unrelated)
	// A draft carries a full, valid (Amend) lane set here so this test
	// isolates the out-of-closure target problem from the lane-admission
	// problems TestLaneResultAdmissionByVerdict covers separately.
	writeFile(t, root, "reviews/r.md", artifactText("draft", false, "Amend",
		allPass(), "  - id: F-01\n    severity: major\n    title: \"names outsider\"\n    status: open\n    action: revise\n    nodes: [z]\n    revise:\n      contract: \"changed\"\n"))

	res, err := Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "r.md")})
	if err != nil {
		t.Fatalf("check must accept an unfrozen, unresolved artifact: %v", err)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "outside") || !strings.Contains(res.Problems[0], "F-01") {
		t.Fatalf("check must report the out-of-closure target: %+v", res.Problems)
	}

	// Nothing was recorded: the gate node still carries no verification.
	g, err := gstore.Load(gstore.PathFor(filepath.Join(root, "Plans", "P")))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("g1").Verification != nil {
		t.Fatalf("check must never record: %+v", g.NodeByID("g1").Verification)
	}

	// A clean artifact (in-closure target, valid lanes) reports no problems.
	writeFile(t, root, "reviews/ok.md", artifactText("draft", false, "Amend",
		allPass(), "  - id: F-02\n    severity: major\n    title: \"names in-closure\"\n    status: open\n    action: revise\n    nodes: [a]\n    revise:\n      contract: \"changed\"\n"))
	res, err = Check(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "ok.md")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 0 {
		t.Fatalf("an in-closure target must report no problems: %+v", res.Problems)
	}
}

// TestPlanAmendmentsScopeVsReach reproduces the top-level review shape
// review-final -> full-gate (command) -> inner-review (GREEN, full) ->
// impl: the increment scope subtracts the inner review's covered region,
// leaving only full-gate, but a finding amending impl is still within
// review-final's dependency closure and must be admitted.
func TestPlanAmendmentsScopeVsReach(t *testing.T) {
	impl := work("impl", nil, "src/impl.ext")
	impl.Verification = pass(1)
	inner := fullGate("inner-review", []string{"impl"})
	inner.Verification = pass(2)
	fullGateCmd := model.Node{ID: "full-gate", Contract: "runs the full gate", Deps: []string{"inner-review"},
		Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}
	outer := fullGate("review-final", []string{"full-gate"})
	_, planDir := fixture(t, 3, impl, inner, fullGateCmd, outer)
	g, _ := gstore.Load(gstore.PathFor(planDir))

	scope, err := Scope(g, "review-final")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scope, []string{"full-gate"}) {
		t.Fatalf("increment scope must subtract the inner GREEN review's covered region: %v", scope)
	}

	art := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-01", Status: "open", Action: ActionRevise, Nodes: []string{"impl"},
			Revise: map[string]any{"contract": "does impl, revised"}}},
	}}
	plan, err := PlanAmendmentsInScope(g, "review-final", art, scope)
	if err != nil {
		t.Fatalf("a revise of a node in the review's dependency closure must be admitted: %v", err)
	}
	if len(plan.Amendments) != 1 || plan.Amendments[0].Node != "impl" {
		t.Fatalf("plan = %+v", plan.Amendments)
	}

	// A node truly outside the closure is still refused.
	outsider := work("z", nil)
	g.Nodes = append(g.Nodes, outsider)
	artOutside := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-02", Status: "open", Action: ActionRevise, Nodes: []string{"z"},
			Revise: map[string]any{"contract": "changed"}}},
	}}
	_, err = PlanAmendmentsInScope(g, "review-final", artOutside, scope)
	if err == nil || !strings.Contains(err.Error(), "outside") || !strings.Contains(err.Error(), "F-02") {
		t.Fatalf("a node outside the dependency closure must still refuse: %v", err)
	}

	// An extend depending on a closure node not in the increment scope
	// (impl, covered by the inner GREEN review) is accepted.
	artExtend := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-03", Status: "open", Action: ActionExtend, Node: map[string]any{
			"id": "impl-followup", "contract": "handles the gap F-03 found", "deps": []any{"impl"},
			"estimate": 1, "hazards": "untriaged",
			"gate": map[string]any{"type": "tests", "tests": []any{map[string]any{"id": "test_followup", "file": "t.ext"}}},
		}}},
	}}
	plan2, err := PlanAmendmentsInScope(g, "review-final", artExtend, scope)
	if err != nil {
		t.Fatalf("an extend hanging off an in-closure node must be admitted even outside the increment scope: %v", err)
	}
	if len(plan2.Amendments) != 1 || plan2.Amendments[0].Node != "impl-followup" {
		t.Fatalf("plan2 = %+v", plan2.Amendments)
	}

	// A contract-only revise naming a review-gate node in the closure is
	// accepted: a decision can retire what the node's contract demands while
	// the node itself stays legitimate, so the contract is revisable in place.
	artReviewGate := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-04", Status: "open", Action: ActionRevise, Nodes: []string{"inner-review"},
			Revise: map[string]any{"contract": "changed"}}},
	}}
	planReview, err := PlanAmendmentsInScope(g, "review-final", artReviewGate, scope)
	if err != nil {
		t.Fatalf("a contract-only revise of a review-gate node must be accepted: %v", err)
	}
	if len(planReview.Amendments) != 1 {
		t.Fatalf("planReview = %+v", planReview.Amendments)
	}
	amended := planReview.Amendments[0]
	if amended.Node != "inner-review" || len(amended.Changed) != 1 || amended.Changed[0] != "contract" {
		t.Fatalf("the revise must change the contract and nothing else: %+v", amended)
	}
	// The node keeps its identity and its gate across the revise.
	if amended.After.ID != "inner-review" || amended.After.Gate.Type != model.GateReview {
		t.Fatalf("a review node's id and gate must survive a contract revise: %+v", amended.After)
	}

	// A contract-only revise of a command-gate node is accepted for the same
	// reason.
	artCommandGate := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-05", Status: "open", Action: ActionRevise, Nodes: []string{"full-gate"},
			Revise: map[string]any{"contract": "changed"}}},
	}}
	if _, err = PlanAmendmentsInScope(g, "review-final", artCommandGate, scope); err != nil {
		t.Fatalf("a contract-only revise of a command-gate node must be accepted: %v", err)
	}

	// Only the contract is revisable on such a node. A revise that would also
	// restructure the gate is still refused — that is a retire-and-replace.
	artReviewGateStructural := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-06", Status: "open", Action: ActionRevise, Nodes: []string{"inner-review"},
			Revise: map[string]any{"contract": "changed", "gate": map[string]any{"type": "review", "lanes": []any{"review_quality"}}}}},
	}}
	_, err = PlanAmendmentsInScope(g, "review-final", artReviewGateStructural, scope)
	if err == nil || !strings.Contains(err.Error(), "is not — retire and replace it instead") {
		t.Fatalf("a revise changing a review node's gate must be refused: %v", err)
	}

	// A revised obligation routinely cites the decision that revised it, so a
	// review node's inputs follow its contract rather than being frozen with
	// the gate.
	artReviewGateInputs := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-07", Status: "open", Action: ActionRevise, Nodes: []string{"inner-review"},
			Revise: map[string]any{
				"contract": "changed",
				"inputs":   []any{map[string]any{"root": "planning", "path": "Decisions/decisions.md"}},
			}}},
	}}
	planInputs, err := PlanAmendmentsInScope(g, "review-final", artReviewGateInputs, scope)
	if err != nil {
		t.Fatalf("a review node's inputs must be revisable alongside its contract: %v", err)
	}
	if len(planInputs.Amendments) != 1 || len(planInputs.Amendments[0].Changed) != 2 {
		t.Fatalf("planInputs = %+v", planInputs.Amendments)
	}

	// `justifies` is structural for the same reason the gate is.
	artReviewGateJustifies := &Artifact{Rel: "reviews/r.md", Qualifier: "reviews/r", Facts: &facts{
		Findings: []finding{{ID: "F-08", Status: "open", Action: ActionRevise, Nodes: []string{"inner-review"},
			Revise: map[string]any{"contract": "changed", "justifies": []any{"reviews/other:F-01"}}}},
	}}
	_, err = PlanAmendmentsInScope(g, "review-final", artReviewGateJustifies, scope)
	if err == nil || !strings.Contains(err.Error(), "is not — retire and replace it instead") {
		t.Fatalf("a revise changing a review node's justifies must be refused: %v", err)
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

func TestRecordBindsArtifactToGate(t *testing.T) {
	a := work("a", nil)
	g1 := fullGate("g1", []string{"a"})
	b := work("b", nil)
	g2 := fullGate("g2", []string{"b"})
	root, planDir := fixture(t, 0, a, g1, b, g2)

	// review_of must exist: an artifact naming no reviewed document
	// cannot be bound to any gate.
	writeFile(t, root, "reviews/none.md", artifactTextFor("", "resolved", true, "Aligned", allPass(), ""))
	_, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "none.md")})
	if err == nil || !strings.Contains(err.Error(), "carries no review_of") {
		t.Fatalf("missing review_of must refuse: %v", err)
	}

	// review_of must lie under THIS plan.
	writeFile(t, root, "reviews/other.md", artifactTextFor("Plans/OtherPlan/01-Phase.md", "resolved", true, "Aligned", allPass(), ""))
	_, err = Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "other.md")})
	if err == nil || !strings.Contains(err.Error(), "not under Plans/P/") {
		t.Fatalf("another plan's review must refuse: %v", err)
	}

	// Cross-gate reuse: one artifact greens one gate. Record on g1, then
	// pointing g2 at the SAME artifact refuses naming g1.
	writeFile(t, root, "reviews/r.md", artifactText("resolved", true, "Aligned", allPass(), ""))
	artifact := filepath.Join(root, "reviews", "r.md")
	if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: artifact}); err != nil {
		t.Fatalf("first record: %v", err)
	}
	_, err = Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g2",
		Artifact: artifact})
	if err == nil || !strings.Contains(err.Error(), `already recorded on gate "g1"`) {
		t.Fatalf("cross-gate reuse must refuse naming the prior gate: %v", err)
	}
	_ = planDir
}

func TestClosedAcceptanceRequiresCurrentFullReviewUpstream(t *testing.T) {
	a := work("a", nil)
	a.Verification = pass(1)
	subset := subsetGate("subset", []string{"a"}, "review_quality")
	subset.Verification = pass(1)
	subset.Verification.Reviewed = map[string]model.ReviewedRef{"a": {ContractRev: 1}}
	accept := work("accept", []string{"subset"})
	accept.Role = model.RoleIntegrationAcceptance
	accept.Gate = model.Gate{Type: model.GateCommand, Command: "true"}
	accept.Verification = pass(1)
	final := fullGate("final", []string{"accept"}) // structurally downstream, not run
	g := &model.Graph{Version: 1, Nodes: []model.Node{a, subset, accept, final}}
	st := states.Derive(states.Inputs{Graph: g})
	closed := Closed(g, st)
	if closed["accept"] || closed["subset"] || closed["a"] {
		t.Fatalf("subset-only acceptance must not confer completion closure before the full review runs: %v", closed)
	}

	// Existing graphs that place the full backstop after acceptance remain
	// executable: they simply do not close until that full review is current.
	final.Verification = pass(2)
	final.Verification.Reviewed = map[string]model.ReviewedRef{
		"a": {ContractRev: 1}, "subset": {ContractRev: 1}, "accept": {ContractRev: 1},
	}
	g = &model.Graph{Version: 1, Nodes: []model.Node{a, subset, accept, final}}
	st = states.Derive(states.Inputs{Graph: g})
	closed = Closed(g, st)
	for _, id := range []string{"a", "subset", "accept", "final"} {
		if !closed[id] {
			t.Fatalf("a current downstream full review should close existing graph member %s: %v", id, closed)
		}
	}

	full := fullGate("full", []string{"a"})
	full.Verification = pass(1)
	full.Verification.Reviewed = map[string]model.ReviewedRef{"a": {ContractRev: 1}}
	accept.Deps = []string{"full"}
	g = &model.Graph{Version: 1, Nodes: []model.Node{a, full, accept}}
	st = states.Derive(states.Inputs{Graph: g})
	closed = Closed(g, st)
	for _, id := range []string{"a", "full", "accept"} {
		if !closed[id] {
			t.Fatalf("current full review upstream should permit acceptance closure for %s: %v", id, closed)
		}
	}
}

func TestClosedDoesNotSubtractAStaleInnerFullReview(t *testing.T) {
	a := work("a", nil)
	inner := fullGate("inner", []string{"a"})
	inner.Verification = pass(1)
	outer := fullGate("outer", []string{"inner"})
	outer.Verification = pass(2)
	g := &model.Graph{Version: 1, Nodes: []model.Node{a, inner, outer}}
	derived := map[string]states.NodeState{
		"a":     {ID: "a", State: states.Green},
		"inner": {ID: "inner", State: states.Stale},
		"outer": {ID: "outer", State: states.Green},
	}
	closed := Closed(g, derived)
	if !closed["a"] || !closed["outer"] || closed["inner"] {
		t.Fatalf("outer current review must absorb the stale inner region without closing the stale gate: %v", closed)
	}
}

func TestRecordScopeDoesNotSubtractArtifactStaleInnerReview(t *testing.T) {
	a := work("a", nil, "src/a.ext")
	inner := fullGate("inner", []string{"a"})
	outer := fullGate("outer", []string{"inner"})
	root, _ := fixture(t, 2, a, inner, outer)
	writeFile(t, root, "src/a.ext", "current\n")
	current := digest.New(root).Artifact("src/a.ext")

	planDir := filepath.Join(root, "Plans", "P")
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.NodeByID("a").Verification = &model.Verification{
			Result: model.ResultPass, Seq: 1, ContractRev: 1,
			ArtifactDigests: map[string]string{"src/a.ext": current}, Isolation: model.IsolationClean,
		}
		g.NodeByID("inner").Verification = &model.Verification{
			Result: model.ResultPass, Seq: 2, ContractRev: 1,
			ArtifactDigests: map[string]string{"src/a.ext": "sha256:stale"},
			Reviewed: map[string]model.ReviewedRef{"a": {
				ContractRev: 1, ArtifactDigests: map[string]string{"src/a.ext": current},
			}},
			Isolation: model.IsolationClean,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "reviews/outer.md", artifactText("resolved", true, "Aligned", allPass(), ""))

	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "outer", Artifact: "reviews/outer.md"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "inner"}; !reflect.DeepEqual(res.Scope, want) {
		t.Fatalf("artifact-stale inner review must not subtract its region: got %v want %v", res.Scope, want)
	}
}

func TestRecordRefusesArtifactOutsidePlanningRoot(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	root, _ := fixture(t, 0, a, gate)
	outside := filepath.Join(t.TempDir(), "foreign.md")
	if err := os.WriteFile(outside, []byte(artifactText("resolved", true, "Aligned", allPass(), "")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: outside}); err == nil ||
		!strings.Contains(err.Error(), "outside the planning root") {
		t.Fatalf("an out-of-root review artifact must refuse: %v", err)
	}
}

func TestReadArtifactRefusesLexicalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "foreign.md")
	if err := os.WriteFile(outside, []byte(artifactText("resolved", true, "Aligned", allPass(), "")), 0o644); err != nil {
		t.Fatal(err)
	}

	parentEscape := filepath.Join(root, "..", "foreign.md")
	if err := os.WriteFile(parentEscape, []byte(artifactText("resolved", true, "Aligned", allPass(), "")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(parentEscape) })
	if _, err := ReadArtifact(root, "../foreign.md"); err == nil || !strings.Contains(err.Error(), "outside the planning root") {
		t.Fatalf("lexical parent escape must refuse: %v", err)
	}

	linkDir := filepath.Join(root, "reviews")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "foreign.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := ReadArtifact(root, "reviews/foreign.md"); err == nil || !strings.Contains(err.Error(), "outside the planning root") {
		t.Fatalf("symlink escape must refuse: %v", err)
	}
}

func TestRecordRefusesUnsafePlanNameBeforePathResolution(t *testing.T) {
	if _, err := Record(Options{Root: t.TempDir(), RepoRoot: t.TempDir(), Plan: "../Foreign", Node: "gate", Artifact: "review.md"}); err == nil ||
		!strings.Contains(err.Error(), "single safe directory name") {
		t.Fatalf("plan traversal must refuse before graph or artifact access: %v", err)
	}
}

func TestRecordRefusesArtifactSymlinkEscapingPlanningRoot(t *testing.T) {
	a := work("a", nil)
	gate := fullGate("g1", []string{"a"})
	root, _ := fixture(t, 0, a, gate)
	outside := filepath.Join(t.TempDir(), "foreign.md")
	if err := os.WriteFile(outside, []byte(artifactText("resolved", true, "Aligned", allPass(), "")), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "reviews", "escape.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1", Artifact: link}); err == nil ||
		!strings.Contains(err.Error(), "outside the planning root") {
		t.Fatalf("an escaping artifact symlink must refuse: %v", err)
	}
}

type mutatingReviewProvider struct {
	mutate func() error
}

func (mutatingReviewProvider) Kind() string  { return "plain" }
func (mutatingReviewProvider) Capacity() int { return 1 }
func (mutatingReviewProvider) Allocate(string) (provider.Workspace, error) {
	return provider.Workspace{}, nil
}
func (mutatingReviewProvider) HandleFor(string) string                { return "" }
func (mutatingReviewProvider) Release(string) error                   { return nil }
func (mutatingReviewProvider) PruneMergedBranches() ([]string, error) { return nil, nil }
func (mutatingReviewProvider) Isolation(string, int) string           { return model.IsolationClean }
func (p mutatingReviewProvider) Provenance(string) (*model.Provenance, error) {
	return nil, p.mutate()
}

func TestRecordRefusesPublicationAcrossReviewedContractRevision(t *testing.T) {
	a := work("a", nil)
	a.Verification = pass(1)
	gate := fullGate("g1", []string{"a"})
	root, planDir := fixture(t, 1, a, gate)
	writeFile(t, root, "reviews/race.md", artifactText("resolved", true, "Aligned", allPass(), ""))
	prov := mutatingReviewProvider{mutate: func() error {
		_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
			g.NodeByID("a").ContractRev = 2
			g.NodeByID("a").Contract = "changed during review publication"
			return nil
		})
		return err
	}}
	if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "race.md"), Provider: prov}); err == nil ||
		!strings.Contains(err.Error(), "scope changed") {
		t.Fatalf("review must publish only against the evaluated scope snapshot: %v", err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("g1").Verification != nil {
		t.Fatal("stale review evidence must not be published")
	}
}

func TestRecordRefusesPublicationAcrossScopeGrowth(t *testing.T) {
	a := work("a", nil)
	a.Verification = pass(1)
	b := work("b", nil)
	b.Verification = pass(1)
	gate := fullGate("g1", []string{"a"})
	root, planDir := fixture(t, 1, a, b, gate)
	writeFile(t, root, "reviews/scope-race.md", artifactText("resolved", true, "Aligned", allPass(), ""))
	prov := mutatingReviewProvider{mutate: func() error {
		_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
			g.NodeByID("g1").Deps = append(g.NodeByID("g1").Deps, "b")
			return nil
		})
		return err
	}}
	if _, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "scope-race.md"), Provider: prov}); err == nil ||
		!strings.Contains(err.Error(), "gate or scope changed") {
		t.Fatalf("review must publish only against the evaluated scope set: %v", err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("g1").Verification != nil {
		t.Fatal("review evidence evaluated before scope growth must not be published")
	}
}

func TestRecordRetriesCASAfterUnrelatedWriteDuringPublication(t *testing.T) {
	a := work("a", nil)
	a.Verification = pass(1)
	b := work("b", nil)
	gate := fullGate("g1", []string{"a"})
	root, planDir := fixture(t, 1, a, b, gate)
	writeFile(t, root, "reviews/retry.md", artifactText("resolved", true, "Aligned", allPass(), ""))
	hookCalls := 0
	res, err := Record(Options{Root: root, RepoRoot: root, Plan: "P", Node: "g1",
		Artifact: filepath.Join(root, "reviews", "retry.md"),
		beforePublish: func() error {
			hookCalls++
			_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
				g.NodeByID("b").Contract = "unrelated concurrent edit"
				return nil
			})
			return err
		}})
	if err != nil || res.Observation == nil {
		t.Fatalf("review publication should retry unrelated contention: %+v %v", res, err)
	}
	if hookCalls != 1 {
		t.Fatalf("publication hook ran %d times, want once", hookCalls)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.NodeByID("b").Contract != "unrelated concurrent edit" || g.NodeByID("g1").Verification == nil {
		t.Fatalf("CAS retry lost one of the writes: %+v", g.Nodes)
	}
}
