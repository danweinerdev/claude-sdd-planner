package sync

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// --- parser equivalence ----------------------------------------------------

const junitReport = `<?xml version="1.0"?>
<testsuites>
  <testsuite name="pkg">
    <testcase name="test_a"/>
    <testcase name="test_b"><failure message="boom"/></testcase>
    <testcase name="test_c[1]"/>
    <testcase name="test_c[2]"><skipped/></testcase>
  </testsuite>
</testsuites>
`

const goJSONReport = `{"Action":"run","Test":"test_a"}
{"Action":"pass","Test":"test_a"}
{"Action":"run","Test":"test_b"}
{"Action":"fail","Test":"test_b"}
{"Action":"pass","Test":"test_c/1"}
{"Action":"skip","Test":"test_c/2"}
{"Action":"pass","Test":""}
`

func TestParsersProduceIdenticalSemantics(t *testing.T) {
	fromXML, err := ParseReport("r.xml", []byte(junitReport))
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := ParseReport("r.json", []byte(goJSONReport))
	if err != nil {
		t.Fatal(err)
	}
	// Same outcomes for the shared ids; case spellings differ per runner
	// (pytest brackets vs Go subtest slashes) but fold identically below.
	byID := func(rs []TestResult) map[string]Outcome {
		m := map[string]Outcome{}
		for _, r := range rs {
			m[r.ID] = r.Outcome
		}
		return m
	}
	xm, jm := byID(fromXML), byID(fromJSON)
	if xm["test_a"] != Pass || jm["test_a"] != Pass || xm["test_b"] != Fail || jm["test_b"] != Fail {
		t.Fatalf("parser disagreement: %v vs %v", xm, jm)
	}
	for _, rs := range [][]TestResult{fromXML, fromJSON} {
		fold := FoldFor("test_c", rs)
		if fold.Resolved || !fold.Withheld {
			t.Fatalf("a skipped case withholds the fold in both formats: %+v", fold)
		}
	}
	if _, err := ParseReport("r.tap", nil); err == nil {
		t.Fatal("unknown formats are refused by name, not sniffed")
	}
}

func TestFoldingRules(t *testing.T) {
	results := []TestResult{
		{"test_all_pass[a]", Pass}, {"test_all_pass[b]", Pass},
		{"test_one_fail[a]", Pass}, {"test_one_fail[b]", Fail},
		{"test_exact", Pass},
		{"test_dup", Pass}, {"test_dup", Fail},
	}
	if f := FoldFor("test_all_pass", results); !f.Resolved || f.Outcome != Pass {
		t.Fatalf("all-pass fold: %+v", f)
	}
	// One failing case fails the fold — an observation, not an ambiguity.
	if f := FoldFor("test_one_fail", results); !f.Resolved || f.Outcome != Fail || f.Ambiguous {
		t.Fatalf("one-fail fold: %+v", f)
	}
	if f := FoldFor("test_exact", results); !f.Resolved || f.Outcome != Pass {
		t.Fatalf("exact fold: %+v", f)
	}
	if f := FoldFor("test_one_fail[b]", results); !f.Resolved || f.Outcome != Fail {
		t.Fatalf("one exact case is declarable: %+v", f)
	}
	// The same exact id passing AND failing is ambiguity, never guessed.
	if f := FoldFor("test_dup", results); !f.Ambiguous || f.Resolved {
		t.Fatalf("conflicting exact duplicates: %+v", f)
	}
	if f := FoldFor("test_absent", results); f.Resolved || f.Withheld || f.Ambiguous {
		t.Fatalf("absent id resolves nothing: %+v", f)
	}
}

// --- observation recording ---------------------------------------------------

func fixture(t *testing.T, nodes ...model.Node) (planDir, repoRoot string) {
	t.Helper()
	repoRoot = t.TempDir()
	planDir = filepath.Join(repoRoot, "Plans", "SamplePlan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes, nodes...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return planDir, repoRoot
}

func testsNode(id string, testIDs ...string) model.Node {
	n := model.Node{ID: id, Contract: "c", Gate: model.Gate{Type: model.GateTests},
		Hazards: model.Hazards{}, Estimate: 1}
	for _, tid := range testIDs {
		n.Gate.Tests = append(n.Gate.Tests, model.Test{ID: tid, File: "t.ext"})
	}
	return n
}

func TestSyncRecordsObservationWithAnchors(t *testing.T) {
	node := testsNode("a", "test_a", "test_b")
	node.Artifacts = []string{"src/a.ext", "src/missing.ext"}
	planDir, repoRoot := fixture(t, node)
	if err := os.MkdirAll(filepath.Join(repoRoot, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "src", "a.ext"), []byte("impl"), 0o644); err != nil {
		t.Fatal(err)
	}

	report := `<testsuite><testcase name="test_a"/><testcase name="test_b"/><testcase name="test_stray"/></testsuite>`
	res, err := Run(Options{
		PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(report),
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !res.Recorded || res.Observation == nil {
		t.Fatalf("observation must record: %+v", res)
	}
	v := res.Observation
	if v.Result != model.ResultPass || v.Seq != 1 {
		t.Fatalf("observation: %+v", v)
	}
	if v.ArtifactDigests["src/a.ext"] != digest.Bytes([]byte("impl")) {
		t.Fatalf("artifact digest anchor: %+v", v.ArtifactDigests)
	}
	if _, recorded := v.ArtifactDigests["src/missing.ext"]; recorded {
		t.Fatal("a missing artifact stays unrecorded (states will hold the node stale — honest)")
	}
	if v.ReportDigest != digest.Bytes([]byte(report)) {
		t.Fatal("report digest anchor missing")
	}
	if v.Isolation != model.IsolationClean {
		t.Fatalf("single-claimant shared tree is clean: %+v", v)
	}
	if !reflect.DeepEqual(res.Buckets.Untracked, []string{"test_stray"}) {
		t.Fatalf("untracked bucket: %+v", res.Buckets)
	}
	// Committed, not just returned.
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.SeqCounter != 1 || g.NodeByID("a").Verification == nil {
		t.Fatal("the observation must land in the store under the incremented seq")
	}
}

func TestSyncRedRunRecordsRedSeqOnce(t *testing.T) {
	planDir, repoRoot := fixture(t, testsNode("a", "test_a"))
	failing := `<testsuite><testcase name="test_a"><failure/></testcase></testsuite>`

	first, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(failing)})
	if err != nil || !first.Recorded || first.Observation.Result != model.ResultFail {
		t.Fatalf("red run records a fail observation: %+v %v", first, err)
	}
	if first.RedSeqsAdded["test_a"] != 1 {
		t.Fatalf("first failure records red_seq: %+v", first.RedSeqsAdded)
	}
	second, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(failing)})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.RedSeqsAdded) != 0 {
		t.Fatal("red_seq is the FIRST failure's seq, recorded once and kept")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").RedSeqs["test_a"] != 1 {
		t.Fatalf("red_seq must persist: %+v", g.NodeByID("a").RedSeqs)
	}
}

func TestSyncRefusesWithoutGuessing(t *testing.T) {
	planDir, repoRoot := fixture(t, testsNode("a", "test_present", "test_absent"))
	report := `<testsuite><testcase name="test_present"/></testsuite>`
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(report)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Recorded {
		t.Fatal("an unresolved declared test leaves the node unverified")
	}
	if !reflect.DeepEqual(res.Buckets.Unresolved, []string{"test_absent"}) ||
		!reflect.DeepEqual(res.Buckets.Updated, []string{"test_present"}) {
		t.Fatalf("buckets: %+v", res.Buckets)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Verification != nil || g.SeqCounter != 0 {
		t.Fatal("a refusal writes nothing")
	}

	// Ambiguity refuses the same way.
	dup := `<testsuite><testcase name="test_present"/><testcase name="test_present"><failure/></testcase><testcase name="test_absent"/></testsuite>`
	res, err = Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(dup)})
	if err != nil || res.Recorded {
		t.Fatalf("ambiguous ids leave the node unverified: %+v %v", res, err)
	}
	if !reflect.DeepEqual(res.Buckets.Ambiguous, []string{"test_present"}) {
		t.Fatalf("ambiguous bucket: %+v", res.Buckets)
	}
}

func TestSyncClaimDiscipline(t *testing.T) {
	node := testsNode("a", "test_a")
	node.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
	planDir, repoRoot := fixture(t, node)
	report := `<testsuite><testcase name="test_a"/></testsuite>`

	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(report), By: "impostor"}); err == nil ||
		!strings.Contains(err.Error(), "stale claim cannot sync") {
		t.Fatalf("a stale claimant's sync is refused: %v", err)
	}
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(report)}); err == nil {
		t.Fatal("a claimed node requires --by")
	}

	// A RED run by the holder renews the lease (the walk continues); a
	// clean PASS by the holder MERGES instead — claim cleared atomically.
	t0 := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	failing := `<testsuite><testcase name="test_a"><failure/></testcase></testsuite>`
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(failing), By: "holder",
		Now: func() time.Time { return t0 }, TTL: 20 * time.Minute})
	if err != nil || !res.Recorded || res.Merged {
		t.Fatalf("holder red run records without merging: %+v %v", res, err)
	}
	if res.LeaseRenewed != t0.Add(20*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("a non-completing sync is the liveness proof; the lease must renew: %q", res.LeaseRenewed)
	}

	res, err = Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(report), By: "holder"})
	if err != nil || !res.Recorded || !res.Merged {
		t.Fatalf("a clean pass by the holder merges: %+v %v", res, err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Claim != nil {
		t.Fatal("merge clears the claim atomically with the observation")
	}
}

func TestRedBeforeGreenGatesHazardTests(t *testing.T) {
	n := testsNode("a", "test_h")
	n.Gate.Tests[0].Satisfies = []string{"external-format"}
	n.Hazards = model.Hazards{"external-format"}
	planDir, repoRoot := fixture(t, n)

	passing := `<testsuite><testcase name="test_h"/></testsuite>`
	_, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(passing)})
	if err == nil || !strings.Contains(err.Error(), "red-before-green") {
		t.Fatalf("a hazard-discharging test must be seen failing before its pass counts: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Verification != nil {
		t.Fatal("the refused pass must not record")
	}

	failing := `<testsuite><testcase name="test_h"><failure/></testcase></testsuite>`
	if res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(failing)}); err != nil || !res.Recorded {
		t.Fatalf("the red run records freely: %+v %v", res, err)
	}
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(passing)})
	if err != nil || !res.Recorded || res.Observation.Result != model.ResultPass {
		t.Fatalf("after the recorded red, the pass counts: %+v %v", res, err)
	}
}

// dirtyProvider forces shared-dirty isolation to prove provisional
// acceptance: recorded, never merged.
type dirtyProvider struct{}

func (dirtyProvider) Kind() string                    { return "plain" }
func (dirtyProvider) Capacity() int                   { return 2 }
func (dirtyProvider) Allocate(string) (provider.Workspace, error) {
	return provider.Workspace{}, nil
}
func (dirtyProvider) HandleFor(string) string               { return "" }
func (dirtyProvider) Release(string) error                  { return nil }
func (dirtyProvider) Isolation(string, int) string          { return model.IsolationSharedDirty }
func (dirtyProvider) Provenance(string) (*model.Provenance, error) { return nil, nil }

func TestSharedDirtyPassRecordsProvisionally(t *testing.T) {
	n := testsNode("a", "test_a")
	n.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
	planDir, repoRoot := fixture(t, n)
	report := `<testsuite><testcase name="test_a"/></testsuite>`

	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(report), By: "holder",
		Provider: dirtyProvider{}})
	if err != nil || !res.Recorded {
		t.Fatalf("shared-dirty pass records provisionally: %+v %v", res, err)
	}
	if res.Merged {
		t.Fatal("shared-dirty never merges; the mandatory clean re-verify does")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	node := g.NodeByID("a")
	if node.Claim == nil {
		t.Fatal("the claim survives a provisional pass")
	}
	// And states hold it STALE, not GREEN (the DD-7 wiring).
	derived := states.Derive(states.Inputs{Graph: g})
	if derived["a"].State != states.Stale || !derived["a"].IsolationStale {
		t.Fatalf("a shared-dirty pass derives STALE with the isolation cause: %+v", derived["a"])
	}
}

func TestSyncCommandGateTeesTheLog(t *testing.T) {
	n := model.Node{ID: "build", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "make build"},
		Hazards: model.Hazards{}, Estimate: 1}
	planDir, repoRoot := fixture(t, n)

	exit := 0
	logBytes := []byte("build ok\n")
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "build",
		CommandExit: &exit, CommandLog: logBytes})
	if err != nil || !res.Recorded || res.Observation.Result != model.ResultPass {
		t.Fatalf("command gate pass: %+v %v", res, err)
	}
	if res.Observation.ReportDigest != digest.Bytes(logBytes) {
		t.Fatal("the observation records the output digest, not the output")
	}
	teed, err := os.ReadFile(filepath.Join(planDir, ".graph", "logs", "build.log"))
	if err != nil || string(teed) != string(logBytes) {
		t.Fatalf("full output tees to the gitignored log: %v %q", err, teed)
	}

	exit = 2
	res, err = Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "build",
		CommandExit: &exit, CommandLog: []byte("boom")})
	if err != nil || res.Observation.Result != model.ResultFail {
		t.Fatalf("nonzero exit is a fail observation: %+v %v", res, err)
	}
}

func TestSyncGateRouting(t *testing.T) {
	review := model.Node{ID: "gate", Contract: "c", Gate: model.Gate{Type: model.GateReview},
		Hazards: model.Hazards{}, Estimate: 1}
	unspecified := model.Node{ID: "conv", Contract: "c", Gate: model.Gate{Type: model.GateUnspecified},
		Hazards: model.Hazards{}, Estimate: 1}
	tests := testsNode("t", "test_a")
	planDir, repoRoot := fixture(t, review, unspecified, tests)

	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "gate",
		ReportName: "r.xml", ReportBytes: []byte("<testsuite/>")}); err == nil ||
		!strings.Contains(err.Error(), "sdd graph review") {
		t.Fatalf("review gates route to `sdd graph review`: %v", err)
	}
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "conv",
		ReportName: "r.xml", ReportBytes: []byte("<testsuite/>")}); err == nil {
		t.Fatal("unspecified gates refuse")
	}
	exit := 0
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "t",
		CommandExit: &exit}); err == nil {
		t.Fatal("a tests gate refuses --command-exit")
	}
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "t"}); err == nil {
		t.Fatal("a tests gate requires --report")
	}
}
