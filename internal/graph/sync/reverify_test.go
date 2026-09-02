package sync

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// TestReverifyFoldsOneRunAcrossTheGraph: the converted-plan on-ramp — one
// report and one command result folded against every foldable node, in
// dependency order, with skips and refusals collected instead of aborting.
func TestReverifyFoldsOneRunAcrossTheGraph(t *testing.T) {
	a := testsNode("a", "test_a")
	b := testsNode("b", "test_b")
	b.Deps = []string{"a"}
	cmd := model.Node{ID: "cmd", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "make test"},
		Hazards: model.Hazards{}, Estimate: 1, Deps: []string{"a"}}
	gate := model.Node{ID: "gate", Contract: "c", Gate: model.Gate{Type: model.GateReview},
		Hazards: model.Hazards{}, Estimate: 1, Deps: []string{"b"}}
	claimed := testsNode("claimed", "test_claimed")
	claimed.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
	hazard := testsNode("hazard", "test_hazard")
	hazard.Hazards = model.Hazards{"external-format"}
	hazard.Gate.Tests[0].Satisfies = []string{"external-format"}

	planDir, repoRoot := fixture(t, a, b, cmd, gate, claimed, hazard)

	report := `<testsuite>
	  <testcase name="test_a"/><testcase name="test_b"/>
	  <testcase name="test_claimed"/><testcase name="test_hazard"/>
	  <testcase name="test_untracked_zz"/>
	</testsuite>`
	exit := 0
	res, err := Reverify(ReverifyOptions{
		PlanDir: planDir, RepoRoot: repoRoot,
		ReportName: "r.xml", ReportBytes: []byte(report),
		CommandExit: &exit, CommandLog: []byte("ok"),
	})
	if err != nil {
		t.Fatalf("reverify: %v", err)
	}

	byNode := map[string]ReverifyOutcome{}
	var order []string
	for _, o := range res.Outcomes {
		byNode[o.Node] = o
		order = append(order, o.Node)
	}

	// Recorded passes for both gate kinds.
	if byNode["a"].Result != model.ResultPass || byNode["b"].Result != model.ResultPass {
		t.Fatalf("tests gates must record from the report: %+v", res.Outcomes)
	}
	if byNode["cmd"].Result != model.ResultPass {
		t.Fatalf("command gates must record from the exit code: %+v", byNode["cmd"])
	}
	// Dependency order: a's seq precedes b's, so no spurious seq-staleness.
	if !(byNode["a"].Seq < byNode["b"].Seq) {
		t.Fatalf("deps record before dependants: a=%d b=%d", byNode["a"].Seq, byNode["b"].Seq)
	}
	ia, ib := -1, -1
	for i, id := range order {
		if id == "a" {
			ia = i
		}
		if id == "b" {
			ib = i
		}
	}
	if ia > ib {
		t.Fatal("outcomes must follow topological order")
	}

	// Skips: claimed nodes and review gates are never touched.
	if !strings.Contains(byNode["claimed"].Skipped, "claimed by holder") {
		t.Fatalf("claimed node must be skipped whole: %+v", byNode["claimed"])
	}
	if !strings.Contains(byNode["gate"].Skipped, "review gate") {
		t.Fatalf("review gate must be skipped: %+v", byNode["gate"])
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("claimed").Verification != nil {
		t.Fatal("a skipped claimed node must carry no observation")
	}

	// Refusals collect without aborting: the hazard test was never seen
	// red, so its pass refuses (red-before-green) while the rest recorded.
	if !strings.Contains(byNode["hazard"].Refused, "red-before-green") {
		t.Fatalf("the hazard node's pass must refuse and be collected: %+v", byNode["hazard"])
	}

	// Aggregate accounting.
	if res.Passes != 3 || res.Skips != 2 || res.Refusals != 1 || res.Failures != 0 {
		t.Fatalf("counts: %+v", res)
	}
	foundZZ := false
	for _, id := range res.Untracked {
		if id == "test_untracked_zz" {
			foundZZ = true
		}
	}
	if !foundZZ {
		t.Fatalf("the report's untracked ids aggregate once: %v", res.Untracked)
	}
}

// TestReverifyPartialInputsSkipTheOtherKind: report-only and command-only
// invocations fold what they can and skip the rest by name.
func TestReverifyPartialInputsSkipTheOtherKind(t *testing.T) {
	a := testsNode("a", "test_a")
	cmd := model.Node{ID: "cmd", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "make test"},
		Hazards: model.Hazards{}, Estimate: 1}
	planDir, repoRoot := fixture(t, a, cmd)

	res, err := Reverify(ReverifyOptions{
		PlanDir: planDir, RepoRoot: repoRoot,
		ReportName: "r.xml", ReportBytes: []byte(`<testsuite><testcase name="test_a"/></testsuite>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	byNode := map[string]ReverifyOutcome{}
	for _, o := range res.Outcomes {
		byNode[o.Node] = o
	}
	if byNode["a"].Result != model.ResultPass {
		t.Fatalf("report-only must still fold tests gates: %+v", byNode["a"])
	}
	if !strings.Contains(byNode["cmd"].Skipped, "no --command-exit") {
		t.Fatalf("command gate skips by name without an exit code: %+v", byNode["cmd"])
	}

	// No inputs at all refuses up front.
	if _, err := Reverify(ReverifyOptions{PlanDir: planDir, RepoRoot: repoRoot}); err == nil {
		t.Fatal("no inputs must refuse naming both flags")
	}
}

// TestReverifyCommandOnlyPartialInput: command-only folds what it can and
// skips the rest by name — the mirror of report-only in the existing test.
func TestReverifyCommandOnlyPartialInput(t *testing.T) {
	a := testsNode("a", "test_a")
	cmd := model.Node{ID: "cmd", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "make test"},
		Hazards: model.Hazards{}, Estimate: 1}
	planDir, repoRoot := fixture(t, a, cmd)

	exit := 0
	res, err := Reverify(ReverifyOptions{
		PlanDir: planDir, RepoRoot: repoRoot,
		CommandExit: &exit, CommandLog: []byte("ok"),
	})
	if err != nil {
		t.Fatal(err)
	}
	byNode := map[string]ReverifyOutcome{}
	for _, o := range res.Outcomes {
		byNode[o.Node] = o
	}
	if byNode["cmd"].Result != model.ResultPass {
		t.Fatalf("command-only must still fold command gates: %+v", byNode["cmd"])
	}
	if !strings.Contains(byNode["a"].Skipped, "no --report") {
		t.Fatalf("tests gate skips by name without a report: %+v", byNode["a"])
	}
}

// TestReverifyCyclePreflightRefuses: a dependency cycle must be caught before
// any node records an observation, and the graph must not be mutated.
func TestReverifyCyclePreflightRefuses(t *testing.T) {
	a := testsNode("a", "test_a")
	a.Deps = []string{"b"}
	b := testsNode("b", "test_b")
	b.Deps = []string{"a"} // a <-> b cycle
	planDir, repoRoot := fixture(t, a, b)

	seqBefore := func() int {
		g, _ := gstore.Load(gstore.PathFor(planDir))
		return g.SeqCounter
	}
	before := seqBefore()

	report := `<testsuite><testcase name="test_a"/><testcase name="test_b"/></testsuite>`
	_, err := Reverify(ReverifyOptions{
		PlanDir: planDir, RepoRoot: repoRoot,
		ReportName: "r.xml", ReportBytes: []byte(report),
	})
	if err == nil {
		t.Fatal("cycle preflight must return an error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle preflight error must mention the cycle: %v", err)
	}

	// No mutation: seq counter and observations must be unchanged.
	after := seqBefore()
	if after != before {
		t.Fatalf("seq counter changed from %d to %d despite cycle preflight", before, after)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	for _, n := range g.Nodes {
		if n.Verification != nil {
			t.Fatalf("node %q carries an observation despite cycle preflight", n.ID)
		}
	}
}

// TestReverifySingleSelfLoopRefuses: a self-looping node must also be caught
// (a depends on a), no mutation, clear error.
func TestReverifySingleSelfLoopRefuses(t *testing.T) {
	a := testsNode("a", "test_a")
	a.Deps = []string{"a"} // self-loop
	planDir, repoRoot := fixture(t, a)

	before := func() int {
		g, _ := gstore.Load(gstore.PathFor(planDir))
		return g.SeqCounter
	}()

	report := `<testsuite><testcase name="test_a"/></testsuite>`
	_, err := Reverify(ReverifyOptions{
		PlanDir: planDir, RepoRoot: repoRoot,
		ReportName: "r.xml", ReportBytes: []byte(report),
	})
	if err == nil {
		t.Fatal("self-loop preflight must return an error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("self-loop preflight error must mention the cycle: %v", err)
	}

	after := func() int {
		g, _ := gstore.Load(gstore.PathFor(planDir))
		return g.SeqCounter
	}()
	if after != before {
		t.Fatalf("seq counter changed from %d to %d despite self-loop preflight", before, after)
	}
}

// TestReverifyRefusalsInSummary: refusals from per-node failures (e.g.
// hazard red-before-green) must count in ReverifyResult.Refusals for the
// CLI to map them to exit code 1 via refusedError.
func TestReverifyRefusalsInSummary(t *testing.T) {
	hazard := testsNode("hazard", "test_hazard")
	hazard.Hazards = model.Hazards{"external-format"}
	hazard.Gate.Tests[0].Satisfies = []string{"external-format"}
	clean := testsNode("clean", "test_clean")
	planDir, repoRoot := fixture(t, hazard, clean)

	report := `<testsuite><testcase name="test_hazard"/><testcase name="test_clean"/></testsuite>`
	res, err := Reverify(ReverifyOptions{
		PlanDir: planDir, RepoRoot: repoRoot,
		ReportName: "r.xml", ReportBytes: []byte(report),
	})
	if err != nil {
		t.Fatalf("reverify: %v", err)
	}
	// The hazard node refuses (red-before-green), the clean node passes.
	if res.Refusals != 1 {
		t.Fatalf("expected 1 refusal for hazard red-before-green, got %d", res.Refusals)
	}
	if res.Passes != 1 {
		t.Fatalf("expected 1 pass for clean node, got %d", res.Passes)
	}
	if res.Failures != 0 {
		t.Fatalf("expected 0 failures, got %d", res.Failures)
	}
}
