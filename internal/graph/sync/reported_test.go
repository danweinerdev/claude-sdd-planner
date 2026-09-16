package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	graphstates "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/reportevidence"
)

type reportedFixtureData struct {
	planDir, root string
	now           time.Time
	node          model.Node
}

type dirtyReportedProvider struct{ cleanWorkspaceProvider }

func (dirtyReportedProvider) Isolation(string, int) string { return model.IsolationSharedDirty }

func newReportedFixture(t *testing.T) reportedFixtureData {
	t.Helper()
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "README.md"), []byte("---\ntitle: P\ntype: plan\nstatus: active\ncreated: 2026-09-15\nupdated: 2026-09-15\ntags: []\nrelated: [Specs/P]\nphases: []\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Specs", "P.md"), []byte("---\ntitle: P Spec\ntype: spec\nstatus: approved\ncreated: 2026-09-15\nupdated: 2026-09-15\ntags: [spec]\nrelated: []\n---\n\n# P Spec\n\n## Acceptance Criteria\n\n- [ ] **AC-01**: Work passes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"work.go": "package p\n", "work_test.go": "package p\n", "support.txt": "stable\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	input := model.Input{Root: model.InputRootRepository, Path: "support.txt"}
	n := model.Node{ID: "work", Contract: "work passes", Justifies: []string{"AC-01"}, Hazards: model.Hazards{"wrong-result"}, Artifacts: []string{"work.go", "work_test.go"}, Inputs: []model.Input{input}, Estimate: 1, Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceReportedV1, Report: &model.ReportProfile{Format: "go-test-json-v1", Runner: "repo-tests", EnvironmentKeys: []string{}, TestSupportInputs: []string{model.InputKey(input)}, TestSupportArtifacts: []string{}}, Tests: []model.Test{{Package: "example.test/p", ID: "TestWork", File: "work_test.go", Satisfies: []string{"wrong-result"}}}}, Claim: &model.Claim{By: "worker", Instance: "nonce", LeaseExpires: now.Add(time.Hour).Format(time.RFC3339)}}
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.Nodes = []model.Node{n}; return nil }); err != nil {
		t.Fatal(err)
	}
	return reportedFixtureData{planDir, root, now, n}
}

func reportedNative(pass bool) ([]byte, int) {
	action, exit := "fail", 1
	if pass {
		action, exit = "pass", 0
	}
	return []byte(strings.Join([]string{"{\"Action\":\"start\",\"Package\":\"example.test/p\"}", "{\"Action\":\"run\",\"Package\":\"example.test/p\",\"Test\":\"TestWork\"}", "{\"Action\":\"" + action + "\",\"Package\":\"example.test/p\",\"Test\":\"TestWork\"}", "{\"Action\":\"" + action + "\",\"Package\":\"example.test/p\"}", ""}, "\n")), exit
}

func reportedPair(t *testing.T, f reportedFixtureData, phase string) ([]byte, []byte) {
	t.Helper()
	g, err := gstore.Load(gstore.PathFor(f.planDir))
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID("work")
	ctx, err := reportevidence.BuildContext(f.root, f.root, f.planDir, g, n, "worker", f.now)
	if err != nil {
		t.Fatal(err)
	}
	report, exit := reportedNative(phase == "green")
	m := reportevidence.Metadata{Protocol: reportevidence.Protocol, Before: ctx, After: ctx, Phase: phase, Started: f.now.Add(-time.Second).Format(time.RFC3339), Completed: f.now.Format(time.RFC3339), Runner: reportevidence.Runner{Identity: "repo-tests", EnvironmentIdentities: map[string]string{}}, Execution: reportevidence.Execution{Started: true, Completed: true, ReportComplete: true, ExitCode: &exit}, ReportDigest: reportevidence.Digest(report)}
	if phase == "red" {
		m.RedKind = "baseline"
	}
	metadata, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return report, metadata
}

func TestReportedSyncRedGreenAndReplay(t *testing.T) {
	f := newReportedFixture(t)
	redReport, redMetadata := reportedPair(t, f, "red")
	red, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportName: "red.json", ReportBytes: redReport, MetadataBytes: redMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }})
	if err != nil || !red.Recorded || red.Observation.Result != model.ResultFail {
		t.Fatalf("RED: %+v %v", red, err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "work.go"), []byte("package p\n// implementation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	greenReport, greenMetadata := reportedPair(t, f, "green")
	green, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportName: "green.json", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }})
	if err != nil || !green.Recorded || !green.Merged || green.Observation.Result != model.ResultPass {
		t.Fatalf("GREEN: %+v %v", green, err)
	}
	g, err := gstore.Load(gstore.PathFor(f.planDir))
	if err != nil {
		t.Fatal(err)
	}
	if state := graphstates.Derive(graphstates.Inputs{Graph: g})["work"]; state.ObservedEvidenceStale {
		t.Fatalf("recorded linkage is stale: %+v", state)
	}
	replay, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "other", ReportBytes: greenReport, MetadataBytes: greenMetadata, CommandExit: new(int), Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }})
	if err == nil || replay != nil {
		t.Fatalf("replay with unrelated command input accepted: %+v %v", replay, err)
	}
	replay, err = Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "other", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }})
	if err != nil || !replay.Historical || replay.Observation.Seq != green.Observation.Seq {
		t.Fatalf("replay: %+v %v", replay, err)
	}
}

func TestSyncRejectsMetadataOutsideReportedBeforeWrites(t *testing.T) {
	command := model.Node{ID: "command", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}
	planDir, root := fixture(t, command)
	exit := 0
	logPath := filepath.Join(planDir, gstore.GraphDirName, "logs", "command.log")
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: root, Node: "command", CommandExit: &exit, CommandLog: []byte("must not write"), MetadataBytes: []byte{}}); err == nil {
		t.Fatal("empty supplied metadata accepted")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("command log was written: %v", err)
	}
	legacy := testsNode("legacy", "TestWork")
	planDir, root = fixture(t, legacy)
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: root, Node: "legacy", ReportBytes: []byte{}, MetadataBytes: []byte{}}); err == nil {
		t.Fatal("legacy metadata accepted")
	}
}

func TestReportedSyncRefusesDriftClaimAndMissingRed(t *testing.T) {
	f := newReportedFixture(t)
	greenReport, greenMetadata := reportedPair(t, f, "green")
	if _, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}); err == nil || !strings.Contains(err.Error(), "red-before-green") {
		t.Fatalf("missing red accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "support.txt"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}); err != nil || res.Recorded || !strings.Contains(res.Refusal, "contexts differ") {
		t.Fatalf("support drift accepted: %+v %v", res, err)
	}
	f = newReportedFixture(t)
	greenReport, greenMetadata = reportedPair(t, f, "green")
	if err := os.WriteFile(filepath.Join(f.root, "work_test.go"), []byte("package p\n// changed test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}); err != nil || res.Recorded || !strings.Contains(res.Refusal, "contexts differ") {
		t.Fatalf("test source drift accepted: %+v %v", res, err)
	}
	f = newReportedFixture(t)
	greenReport, greenMetadata = reportedPair(t, f, "green")
	if err := os.WriteFile(filepath.Join(f.root, "Specs", "P.md"), []byte("---\ntitle: P Spec\ntype: spec\nstatus: approved\ncreated: 2026-09-15\nupdated: 2026-09-15\ntags: [spec]\nrelated: []\n---\n\n# P Spec\n\n## Acceptance Criteria\n\n- [ ] **AC-01**: Work passes differently.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}); err != nil || res.Recorded || !strings.Contains(res.Refusal, "contexts differ") {
		t.Fatalf("intent drift accepted: %+v %v", res, err)
	}
	f = newReportedFixture(t)
	redReport, redMetadata := reportedPair(t, f, "red")
	if _, err := gstore.Update(gstore.PathFor(f.planDir), func(g *model.Graph) error {
		g.NodeByID("work").Claim.LeaseExpires = f.now.Add(-time.Second).Format(time.RFC3339)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: redReport, MetadataBytes: redMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired claim accepted: %v", err)
	}
	f = newReportedFixture(t)
	redReport, redMetadata = reportedPair(t, f, "red")
	opts := Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: redReport, MetadataBytes: redMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}
	opts.beforePublish = func() error {
		_, err := gstore.Update(gstore.PathFor(f.planDir), func(g *model.Graph) error { g.NodeByID("work").Claim.Instance = "replacement"; return nil })
		return err
	}
	if _, err := Run(opts); err == nil || !strings.Contains(err.Error(), "claim") {
		t.Fatalf("claim replacement accepted: %v", err)
	}
}

func TestHistoricalObservedRefusesNewAdmission(t *testing.T) {
	n := testsNode("old", "TestWork")
	n.Gate.Evidence = model.EvidenceObservedV1
	n.Gate.Execution = &model.ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 1}
	n.Artifacts = []string{"t.ext"}
	planDir, root := fixture(t, n)
	if _, err := Run(Options{PlanDir: planDir, RepoRoot: root, Node: "old", ReportBytes: []byte("historical")}); err == nil || !strings.Contains(err.Error(), "reported-v1") {
		t.Fatalf("historical observed admission not retired: %v", err)
	}
}

func TestReportedSyncRefusesDirtyPass(t *testing.T) {
	f := newReportedFixture(t)
	redReport, redMetadata := reportedPair(t, f, "red")
	if _, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: redReport, MetadataBytes: redMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}); err != nil {
		t.Fatal(err)
	}
	greenReport, greenMetadata := reportedPair(t, f, "green")
	if _, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: dirtyReportedProvider{}, Now: func() time.Time { return f.now }}); err == nil || !strings.Contains(err.Error(), "clean isolation") {
		t.Fatalf("dirty pass accepted: %v", err)
	}
}

func TestReportedReleaseFailureReturnsRecordedResult(t *testing.T) {
	f := newReportedFixture(t)
	if _, err := gstore.Update(gstore.PathFor(f.planDir), func(g *model.Graph) error { g.NodeByID("work").Claim.Workspace = f.root; return nil }); err != nil {
		t.Fatal(err)
	}
	redReport, redMetadata := reportedPair(t, f, "red")
	if _, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: redReport, MetadataBytes: redMetadata, Provider: cleanWorkspaceProvider{}, Now: func() time.Time { return f.now }}); err != nil {
		t.Fatal(err)
	}
	greenReport, greenMetadata := reportedPair(t, f, "green")
	res, err := Run(Options{PlanDir: f.planDir, RepoRoot: f.root, Node: "work", By: "worker", ReportBytes: greenReport, MetadataBytes: greenMetadata, Provider: failingReleaseProvider{}, Now: func() time.Time { return f.now }})
	if err == nil || res == nil || !res.Recorded || !res.Merged {
		t.Fatalf("release failure lost recorded result: %+v %v", res, err)
	}
}
