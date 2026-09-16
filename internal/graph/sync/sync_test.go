package sync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/ops"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

const junitReport = `<?xml version="1.0"?>
<testsuites><testsuite name="pkg">
<testcase name="test_a"/><testcase name="test_b"><failure message="boom"/></testcase>
<testcase name="test_c[1]"/><testcase name="test_c[2]"><skipped/></testcase>
</testsuite></testsuites>`

const goJSONReport = `{"Action":"run","Test":"test_a"}
{"Action":"pass","Test":"test_a"}
{"Action":"run","Test":"test_b"}
{"Action":"fail","Test":"test_b"}
{"Action":"pass","Test":"test_c/1"}
{"Action":"skip","Test":"test_c/2"}
{"Action":"pass","Test":""}`

func TestParsersProduceIdenticalSemantics(t *testing.T) {
	fromXML, err := ParseReport("r.xml", []byte(junitReport))
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := ParseReport("r.json", []byte(goJSONReport))
	if err != nil {
		t.Fatal(err)
	}
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
	results := []TestResult{{"test_all_pass[a]", Pass}, {"test_all_pass[b]", Pass}, {"test_one_fail[a]", Pass}, {"test_one_fail[b]", Fail}, {"test_exact", Pass}, {"test_dup", Pass}, {"test_dup", Fail}}
	if f := FoldFor("test_all_pass", results); !f.Resolved || f.Outcome != Pass {
		t.Fatalf("all-pass fold: %+v", f)
	}
	if f := FoldFor("test_one_fail", results); !f.Resolved || f.Outcome != Fail || f.Ambiguous {
		t.Fatalf("one-fail fold: %+v", f)
	}
	if f := FoldFor("test_exact", results); !f.Resolved || f.Outcome != Pass {
		t.Fatalf("exact fold: %+v", f)
	}
	if f := FoldFor("test_one_fail[b]", results); !f.Resolved || f.Outcome != Fail {
		t.Fatalf("one exact case is declarable: %+v", f)
	}
	if f := FoldFor("test_dup", results); !f.Ambiguous || f.Resolved {
		t.Fatalf("conflicting exact duplicates: %+v", f)
	}
	if f := FoldFor("test_absent", results); f.Resolved || f.Withheld || f.Ambiguous {
		t.Fatalf("absent id resolves nothing: %+v", f)
	}
}

func syncFixture(t *testing.T, test model.Test) (string, string) {
	t.Helper()
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{{ID: "n", Contract: "c", Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{test}}, Hazards: model.Hazards{}, Estimate: 1}}}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	return root, planDir
}

func testsNode(id string, testIDs ...string) model.Node {
	n := model.Node{ID: id, Contract: "c", Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1}
	for _, testID := range testIDs {
		n.Gate.Tests = append(n.Gate.Tests, model.Test{ID: testID, File: "t.ext"})
	}
	return n
}
func fixture(t *testing.T, nodes ...model.Node) (string, string) {
	t.Helper()
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := &model.Graph{Version: 1, Nodes: nodes}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	return planDir, root
}

func TestDuplicateReportIsNoOpAndDifferentReportAdvancesSequence(t *testing.T) {
	root, dir := syncFixture(t, model.Test{ID: "TestX", File: "x_test.go"})
	raw := []byte(`<testsuite><testcase name="TestX"/></testsuite>`)
	first, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: raw})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Historical || second.Observation.Seq != first.Observation.Seq {
		t.Fatalf("duplicate=%+v first=%+v", second, first)
	}
	third, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: append(raw, '\n')})
	if err != nil {
		t.Fatal(err)
	}
	if third.Observation.Seq <= first.Observation.Seq {
		t.Fatalf("third=%+v", third)
	}
	g, _ := gstore.Load(gstore.PathFor(dir))
	encoded, _ := g.Encode()
	for _, key := range []string{"artifact_digests", "dependency_digests", "input_hashes", "intent_hashes"} {
		if strings.Contains(string(encoded), key) {
			t.Fatalf("removed %s recorded: %s", key, encoded)
		}
	}
}

func TestDuplicateReportAfterContractRevisionRecordsNewSequence(t *testing.T) {
	root, dir := syncFixture(t, model.Test{ID: "TestX", File: "x_test.go"})
	raw := []byte(`<testsuite><testcase name="TestX"/></testsuite>`)
	first, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(dir), func(g *model.Graph) error {
		g.NodeByID("n").ContractRev = 2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	second, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: raw})
	if err != nil {
		t.Fatal(err)
	}
	if second.Historical || second.Observation.Seq <= first.Observation.Seq || second.Observation.ContractRev != 2 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestCommandGateSameLogDifferentExitRecords(t *testing.T) {
	n := model.Node{ID: "cmd", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "check"}, Hazards: model.Hazards{}, Estimate: 1}
	dir, root := fixture(t, n)
	zero, one := 0, 1
	first, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "cmd", CommandExit: &zero, CommandLog: []byte("same")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "cmd", CommandExit: &one, CommandLog: []byte("same")})
	if err != nil {
		t.Fatal(err)
	}
	if second.Historical || second.Observation.Result != model.ResultFail || second.Observation.Seq <= first.Observation.Seq {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestCommandGateSameExitAndLogIsNoOp(t *testing.T) {
	n := model.Node{ID: "cmd", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "check"}, Hazards: model.Hazards{}, Estimate: 1}
	dir, root := fixture(t, n)
	exit := 0
	first, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "cmd", CommandExit: &exit, CommandLog: []byte("same")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "cmd", CommandExit: &exit, CommandLog: []byte("same")})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Recorded || !second.Historical || second.Observation.Seq != first.Observation.Seq {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	g, loadErr := gstore.Load(gstore.PathFor(dir))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if g.SeqCounter != first.Observation.Seq {
		t.Fatalf("duplicate burned a sequence: graph=%d first=%d", g.SeqCounter, first.Observation.Seq)
	}
}

func TestSyncRefusesClaimLandedBeforePublish(t *testing.T) {
	root, dir := syncFixture(t, model.Test{ID: "TestX", File: "x_test.go"})
	raw := []byte(`<testsuite><testcase name="TestX"/></testsuite>`)
	_, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: raw,
		beforePublish: func() error {
			_, e := gstore.Update(gstore.PathFor(dir), func(g *model.Graph) error {
				g.NodeByID("n").Claim = &model.Claim{By: "other", LeaseExpires: "2099-01-01T00:00:00Z"}
				return nil
			})
			return e
		}})
	if err == nil || !strings.Contains(err.Error(), `was claimed by "other" while this sync ran`) {
		t.Fatalf("claim race error = %v", err)
	}
	g, loadErr := gstore.Load(gstore.PathFor(dir))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if g.SeqCounter != 0 || g.NodeByID("n").Verification != nil || g.NodeByID("n").Claim == nil {
		t.Fatalf("sync mutation landed across claim race: %+v", g)
	}
}

func TestSyncCASRetryResetsAttemptResults(t *testing.T) {
	n := model.Node{ID: "n", Contract: "c", Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "TestX", File: "x_test.go"}}}, Hazards: model.Hazards{}, Estimate: 1,
		Claim: &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}}
	dir, root := fixture(t, n)
	raw := []byte(`<testsuite><testcase name="TestX"/></testsuite>`)
	res, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", By: "holder", ReportName: "r.xml", ReportBytes: raw,
		beforePublish: func() error {
			_, e := gstore.Update(gstore.PathFor(dir), func(g *model.Graph) error {
				g.NodeByID("n").Claim = nil
				return nil
			})
			return e
		}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged || res.WorkspaceReleased != "" || res.LeaseRenewed != "" || len(res.RedSeqsAdded) != 0 {
		t.Fatalf("stale callback result escaped CAS retry: %+v", res)
	}
}

func TestRedClassificationValidation(t *testing.T) {
	root, dir := syncFixture(t, model.Test{ID: "TestX", File: "x_test.go"})
	fail := []byte(`<testsuite><testcase name="TestX"><failure/></testcase></testsuite>`)
	if _, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: fail, RedKind: "sensitivity"}); err == nil {
		t.Fatal("sensitivity without fault accepted")
	}
	res, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.xml", ReportBytes: fail, RedKind: "sensitivity", Fault: "mutant"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Observation.RedKind != "sensitivity" || res.Observation.Fault != "mutant" {
		t.Fatalf("observation=%+v", res.Observation)
	}
}

func TestPackageQualifiedReportUsesStrictParser(t *testing.T) {
	root, dir := syncFixture(t, model.Test{Package: "example/p", ID: "TestX", File: "x_test.go"})
	good := []byte("{\"Action\":\"start\",\"Package\":\"example/p\"}\n{\"Action\":\"run\",\"Package\":\"example/p\",\"Test\":\"TestX\"}\n{\"Action\":\"pass\",\"Package\":\"example/p\",\"Test\":\"TestX\"}\n{\"Action\":\"pass\",\"Package\":\"example/p\"}\n")
	res, err := Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.json", ReportBytes: good})
	if err != nil || !res.Recorded {
		t.Fatalf("strict parse: %+v %v", res, err)
	}
	root, dir = syncFixture(t, model.Test{Package: "example/p", ID: "TestX", File: "x_test.go"})
	bad := []byte("{\"Action\":\"start\",\"Package\":\"wrong/p\"}\n{\"Action\":\"skip\",\"Package\":\"wrong/p\"}\n")
	res, err = Run(Options{PlanDir: dir, RepoRoot: root, Node: "n", ReportName: "r.json", ReportBytes: bad})
	if err != nil || res.Recorded || res.Refusal == "" {
		t.Fatalf("bad strict parse: %+v %v", res, err)
	}
}

func TestSyncRedRunRecordsRedSeqOnce(t *testing.T) {
	planDir, repoRoot := fixture(t, testsNode("a", "test_a"))
	failing := `<testsuite><testcase name="test_a"><failure/></testcase></testsuite>`
	first, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(failing)})
	if err != nil || !first.Recorded || first.Observation.Result != model.ResultFail {
		t.Fatalf("red run records a fail observation: %+v %v", first, err)
	}
	if first.RedSeqsAdded["test_a"] != 1 {
		t.Fatalf("first failure records red_seq: %+v", first.RedSeqsAdded)
	}
	second, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(failing)})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.RedSeqsAdded) != 0 {
		t.Fatal("red_seq is the first failure's seq")
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").RedSeqs["test_a"] != 1 {
		t.Fatalf("red_seq must persist: %+v", g.NodeByID("a").RedSeqs)
	}
}

func TestSyncRefusesWithoutGuessing(t *testing.T) {
	planDir, repoRoot := fixture(t, testsNode("a", "test_present", "test_absent"))
	report := `<testsuite><testcase name="test_present"/></testsuite>`
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(report)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Recorded {
		t.Fatal("an unresolved declared test leaves the node unverified")
	}
	if !reflect.DeepEqual(res.Buckets.Unresolved, []string{"test_absent"}) || !reflect.DeepEqual(res.Buckets.Updated, []string{"test_present"}) {
		t.Fatalf("buckets: %+v", res.Buckets)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Verification != nil || g.SeqCounter != 0 {
		t.Fatal("a refusal writes nothing")
	}
	dup := `<testsuite><testcase name="test_present"/><testcase name="test_present"><failure/></testcase><testcase name="test_absent"/></testsuite>`
	res, err = Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(dup)})
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
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(report), By: "impostor"}); err == nil || !strings.Contains(err.Error(), "stale claim cannot sync") {
		t.Fatalf("a stale claimant's sync is refused: %v", err)
	}
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(report)}); err == nil {
		t.Fatal("a claimed node requires --by")
	}
	t0 := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	failing := `<testsuite><testcase name="test_a"><failure/></testcase></testsuite>`
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(failing), By: "holder", Now: func() time.Time { return t0 }, TTL: 20 * time.Minute})
	if err != nil || !res.Recorded || res.Merged {
		t.Fatalf("holder red run records without merging: %+v %v", res, err)
	}
	if res.LeaseRenewed != t0.Add(20*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("lease must renew: %q", res.LeaseRenewed)
	}
	res, err = Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(report), By: "holder"})
	if err != nil || !res.Recorded || !res.Merged {
		t.Fatalf("a clean pass by the holder merges: %+v %v", res, err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Claim != nil {
		t.Fatal("merge clears the claim atomically")
	}
}

func TestRedBeforeGreenGatesHazardTests(t *testing.T) {
	n := testsNode("a", "test_h")
	n.Gate.Tests[0].Satisfies = []string{"external-format"}
	n.Hazards = model.Hazards{"external-format"}
	planDir, repoRoot := fixture(t, n)
	passing := `<testsuite><testcase name="test_h"/></testsuite>`
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(passing)}); err == nil || !strings.Contains(err.Error(), "red-before-green") {
		t.Fatalf("hazard test must be seen failing first: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Verification != nil {
		t.Fatal("refused pass recorded")
	}
	failing := `<testsuite><testcase name="test_h"><failure/></testcase></testsuite>`
	if res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(failing)}); err != nil || !res.Recorded {
		t.Fatalf("red run records: %+v %v", res, err)
	}
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(passing)})
	if err != nil || !res.Recorded || res.Observation.Result != model.ResultPass {
		t.Fatalf("pass after red: %+v %v", res, err)
	}
}

func TestSyncPassesAfterReviseCarriesOverUnchangedHazardRed(t *testing.T) {
	n := testsNode("a", "test_h")
	n.Gate.Tests[0].Satisfies, n.Hazards = []string{"external-format"}, model.Hazards{"external-format"}
	planDir, repoRoot := fixture(t, n)
	failing := `<testsuite><testcase name="test_h"><failure/></testcase></testsuite>`
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "red.xml", ReportBytes: []byte(failing)}); err != nil {
		t.Fatal(err)
	}
	if err := ops.SetTests(planDir, "a", "", []model.Test{{ID: "test_h", File: "t.ext", Satisfies: []string{"external-format"}}, {ID: "test_new", File: "t.ext", Satisfies: []string{"external-format"}}}); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	a := g.NodeByID("a")
	if a.ContractRev != 2 || a.RedSeqs["test_h"] != 1 {
		t.Fatalf("unchanged red did not carry: %+v", a)
	}
	if _, ok := a.RedSeqs["test_new"]; ok {
		t.Fatal("new test carried fabricated red")
	}
	bothPassing := `<testsuite><testcase name="test_h"/><testcase name="test_new"/></testsuite>`
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "green.xml", ReportBytes: []byte(bothPassing)}); err == nil || !strings.Contains(err.Error(), "red-before-green") || strings.Contains(err.Error(), "test_h") {
		t.Fatalf("only new test should owe red: %v", err)
	}
	newFails := `<testsuite><testcase name="test_h"/><testcase name="test_new"><failure/></testcase></testsuite>`
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "red2.xml", ReportBytes: []byte(newFails)}); err != nil {
		t.Fatal(err)
	}
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "green2.xml", ReportBytes: []byte(bothPassing)})
	if err != nil || !res.Recorded || res.Observation.Result != model.ResultPass {
		t.Fatalf("pass after all reds: %+v %v", res, err)
	}
}

func TestSyncStillRefusesWhenHazardTestItselfChanged(t *testing.T) {
	n := testsNode("a", "test_h")
	n.Gate.Tests[0].Satisfies, n.Hazards = []string{"external-format"}, model.Hazards{"external-format"}
	planDir, repoRoot := fixture(t, n)
	failing := `<testsuite><testcase name="test_h"><failure/></testcase></testsuite>`
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "red.xml", ReportBytes: []byte(failing)}); err != nil {
		t.Fatal(err)
	}
	if err := ops.SetTests(planDir, "a", "", []model.Test{{ID: "test_h", File: "other.ext", Satisfies: []string{"external-format"}}}); err != nil {
		t.Fatal(err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if _, ok := g.NodeByID("a").RedSeqs["test_h"]; ok {
		t.Fatal("changed test retained red")
	}
	passing := `<testsuite><testcase name="test_h"/></testsuite>`
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "green.xml", ReportBytes: []byte(passing)}); err == nil || !strings.Contains(err.Error(), "red-before-green") {
		t.Fatalf("changed test should owe red: %v", err)
	}
}

type dirtyProvider struct{}

func (dirtyProvider) Kind() string                                 { return "plain" }
func (dirtyProvider) Capacity() int                                { return 2 }
func (dirtyProvider) Allocate(string) (provider.Workspace, error)  { return provider.Workspace{}, nil }
func (dirtyProvider) HandleFor(string) string                      { return "" }
func (dirtyProvider) Release(string) error                         { return nil }
func (dirtyProvider) PruneMergedBranches() ([]string, error)       { return nil, nil }
func (dirtyProvider) Isolation(string, int) string                 { return model.IsolationSharedDirty }
func (dirtyProvider) Provenance(string) (*model.Provenance, error) { return nil, nil }

func syncGitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestSharedDirtyPassRecordsIsolationDirtyPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	n := testsNode("a", "test_a")
	n.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
	planDir, repoRoot := fixture(t, n)
	syncGitOK(t, repoRoot, "init", "-q")
	syncGitOK(t, repoRoot, "config", "user.email", "t@example.com")
	syncGitOK(t, repoRoot, "config", "user.name", "t")
	syncGitOK(t, repoRoot, "add", ".")
	syncGitOK(t, repoRoot, "commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(repoRoot, "stray.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(`<testsuite><testcase name="test_a"/></testsuite>`), By: "holder", Provider: dirtyProvider{}})
	if err != nil || !res.Recorded || !reflect.DeepEqual(res.Observation.IsolationDirtyPaths, []string{"stray.txt"}) {
		t.Fatalf("dirty paths: %+v %v", res, err)
	}
}

func TestSharedDirtyPassRecordsProvisionally(t *testing.T) {
	n := testsNode("a", "test_a")
	n.Claim = &model.Claim{By: "holder", LeaseExpires: "2099-01-01T00:00:00Z"}
	planDir, repoRoot := fixture(t, n)
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "r.xml", ReportBytes: []byte(`<testsuite><testcase name="test_a"/></testsuite>`), By: "holder", Provider: dirtyProvider{}})
	if err != nil || !res.Recorded || res.Merged {
		t.Fatalf("shared-dirty pass: %+v %v", res, err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("a").Claim == nil {
		t.Fatal("claim must survive provisional pass")
	}
	derived := states.Derive(states.Inputs{Graph: g})
	if derived["a"].State != states.Stale || !derived["a"].IsolationStale {
		t.Fatalf("shared-dirty pass state: %+v", derived["a"])
	}
}

func TestSyncCommandGateTeesTheLog(t *testing.T) {
	n := model.Node{ID: "build", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "make build"}, Hazards: model.Hazards{}, Estimate: 1}
	planDir, repoRoot := fixture(t, n)
	exit := 0
	logBytes := []byte("build ok\n")
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "build", CommandExit: &exit, CommandLog: logBytes})
	if err != nil || !res.Recorded || res.Observation.Result != model.ResultPass || res.Observation.ReportDigest == "" {
		t.Fatalf("command gate pass: %+v %v", res, err)
	}
	teed, err := os.ReadFile(filepath.Join(planDir, ".graph", "logs", "build.log"))
	if err != nil || !reflect.DeepEqual(teed, logBytes) {
		t.Fatalf("full output tees to log: %v %q", err, teed)
	}
	exit = 2
	res, err = Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "build", CommandExit: &exit, CommandLog: []byte("boom")})
	if err != nil || res.Observation.Result != model.ResultFail {
		t.Fatalf("nonzero exit: %+v %v", res, err)
	}
}

func TestSyncGateRouting(t *testing.T) {
	review := model.Node{ID: "gate", Contract: "c", Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1}
	unspecified := model.Node{ID: "conv", Contract: "c", Gate: model.Gate{Type: model.GateUnspecified}, Hazards: model.Hazards{}, Estimate: 1}
	tests := testsNode("t", "test_a")
	planDir, repoRoot := fixture(t, review, unspecified, tests)
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "gate", ReportName: "r.xml", ReportBytes: []byte("<testsuite/>")}); err == nil || !strings.Contains(err.Error(), "sdd graph review") {
		t.Fatalf("review route: %v", err)
	}
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "conv", ReportName: "r.xml", ReportBytes: []byte("<testsuite/>")}); err == nil {
		t.Fatal("unspecified gate accepted")
	}
	exit := 0
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "t", CommandExit: &exit}); err == nil {
		t.Fatal("tests gate accepted command exit")
	}
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "t"}); err == nil {
		t.Fatal("tests gate omitted report")
	}
}

type graphMutatingProvider struct {
	provider.Provider
	mutate func() error
}

func (p graphMutatingProvider) Provenance(string) (*model.Provenance, error) { return nil, p.mutate() }
func (p graphMutatingProvider) Isolation(string, int) string                 { return model.IsolationClean }

func TestSyncRefusesPublicationAcrossContractRevision(t *testing.T) {
	n := testsNode("a", "test_old")
	n.Hazards, n.Gate.Tests[0].Satisfies = model.Hazards{"external-format"}, []string{"external-format"}
	planDir, repoRoot := fixture(t, n)
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "red.xml", ReportBytes: []byte(`<testsuite><testcase name="test_old"><failure/></testcase></testsuite>`)}); err != nil {
		t.Fatal(err)
	}
	prov := graphMutatingProvider{mutate: func() error {
		_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
			a := g.NodeByID("a")
			a.ContractRev, a.Contract, a.Gate.Tests[0].ID, a.RedSeqs = 2, "revised promise", "test_new", nil
			return nil
		})
		return err
	}}
	_, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "green.xml", ReportBytes: []byte(`<testsuite><testcase name="test_old"/></testsuite>`), Provider: prov})
	if err == nil || !strings.Contains(err.Error(), "contract changed") {
		t.Fatalf("old report publication: %v", err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if got := g.NodeByID("a"); got.Verification != nil && got.Verification.Result == model.ResultPass {
		t.Fatalf("old report published: %+v", got)
	}
}

func TestSyncAllowsConcurrentUnrelatedGraphWrite(t *testing.T) {
	planDir, repoRoot := fixture(t, testsNode("a", "test_a"), testsNode("b", "test_b"))
	prov := graphMutatingProvider{mutate: func() error {
		_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.NodeByID("b").Contract = "unrelated edit"; return nil })
		return err
	}}
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "green.xml", ReportBytes: []byte(`<testsuite><testcase name="test_a"/></testsuite>`), Provider: prov})
	if err != nil || !res.Recorded {
		t.Fatalf("unrelated write: %+v %v", res, err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("b").Contract != "unrelated edit" || g.NodeByID("a").Verification == nil {
		t.Fatalf("both writes did not survive: %+v", g.Nodes)
	}
}

func TestSyncRetriesCASAfterUnrelatedWriteDuringPublication(t *testing.T) {
	planDir, repoRoot := fixture(t, testsNode("a", "test_a"), testsNode("b", "test_b"))
	hookCalls := 0
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", ReportName: "green.xml", ReportBytes: []byte(`<testsuite><testcase name="test_a"/></testsuite>`), beforePublish: func() error {
		hookCalls++
		_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.NodeByID("b").Contract = "landed between read and CAS"; return nil })
		return err
	}})
	if err != nil || !res.Recorded || hookCalls != 1 {
		t.Fatalf("CAS retry: %+v calls=%d err=%v", res, hookCalls, err)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID("b").Contract != "landed between read and CAS" || g.NodeByID("a").Verification == nil {
		t.Fatalf("CAS retry lost write: %+v", g.Nodes)
	}
}

var errReleaseMeasured = errors.New("release measured failure")

type failingReleaseProvider struct{ cleanWorkspaceProvider }

func (failingReleaseProvider) Release(string) error { return errReleaseMeasured }

func TestSyncAttributesWorkspaceReleaseThroughFailure(t *testing.T) {
	n := testsNode("a", "test_a")
	n.Claim = &model.Claim{By: "holder", Instance: "claim", Workspace: "ws", LeaseExpires: "2099-01-01T00:00:00Z"}
	planDir, repoRoot := fixture(t, n)
	if err := os.MkdirAll(filepath.Join(repoRoot, "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Run(Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a", By: "holder", ReportName: "green.xml", ReportBytes: []byte(`<testsuite><testcase name="test_a"/></testsuite>`), Provider: failingReleaseProvider{}})
	if !errors.Is(err, errReleaseMeasured) {
		t.Fatalf("release error = %v", err)
	}
	if res == nil || !res.Recorded || !res.Merged || res.Observation == nil {
		t.Fatalf("recorded result lost on release failure: %+v", res)
	}
}
