package reportevidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testevidence"
)

func reportedFixture() (time.Time, Context, []byte, Metadata, *model.ReportProfile) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	ctx := Context{
		Protocol: Protocol, Plan: "P", Node: "n", By: "worker", ClaimInstance: "nonce", WorkspaceIdentity: "shared",
		Selected:        []testevidence.SelectedTest{{Package: "example.test/p", ID: "TestWork", File: "work_test.go"}},
		DeclaredHazards: map[string][]string{"example.test/p::TestWork": {"wrong-result"}},
		Candidate:       Candidate{Obligation: Digest([]byte("obligation")), Artifacts: map[string]string{"work_test.go": Digest([]byte("test"))}, Dependencies: map[string]map[string]string{}, Inputs: map[string]string{}, Intent: map[string]string{}, SelectedTestSources: map[string]string{"example.test/p::TestWork": Digest([]byte("test"))}},
	}
	report := []byte("{\"Action\":\"start\",\"Package\":\"example.test/p\"}\n{\"Action\":\"run\",\"Package\":\"example.test/p\",\"Test\":\"TestWork\"}\n{\"Action\":\"fail\",\"Package\":\"example.test/p\",\"Test\":\"TestWork\"}\n{\"Action\":\"fail\",\"Package\":\"example.test/p\"}\n")
	exit := 1
	metadata := Metadata{Protocol: Protocol, Before: ctx, After: ctx, Phase: "red", RedKind: "baseline", Started: now.Add(-time.Second).Format(time.RFC3339), Completed: now.Format(time.RFC3339), Runner: Runner{Identity: "repo-tests", EnvironmentIdentities: map[string]string{}}, Execution: Execution{Started: true, Completed: true, ReportComplete: true, ExitCode: &exit}, ReportDigest: Digest(report)}
	return now, ctx, report, metadata, &model.ReportProfile{Format: "go-test-json-v1", Runner: "repo-tests"}
}

func TestValidateRepositoryReportRedAndGreen(t *testing.T) {
	now, ctx, report, metadata, profile := reportedFixture()
	raw, _ := json.Marshal(metadata)
	a, err := Validate(report, raw, ctx, profile, now)
	if err != nil || a.Result != model.ResultFail || len(a.Failed) != 1 {
		t.Fatalf("actual RED not admitted: %+v %v", a, err)
	}
	greenReport := []byte("{\"Action\":\"start\",\"Package\":\"example.test/p\"}\n{\"Action\":\"run\",\"Package\":\"example.test/p\",\"Test\":\"TestWork\"}\n{\"Action\":\"pass\",\"Package\":\"example.test/p\",\"Test\":\"TestWork\"}\n{\"Action\":\"pass\",\"Package\":\"example.test/p\"}\n")
	zero := 0
	metadata.Phase, metadata.RedKind, metadata.Execution.ExitCode, metadata.ReportDigest = "green", "", &zero, Digest(greenReport)
	greenMeta, _ := json.Marshal(metadata)
	green, err := Validate(greenReport, greenMeta, ctx, profile, now)
	if err != nil || green.Result != model.ResultPass {
		t.Fatalf("GREEN refused: %+v %v", green, err)
	}
}

func TestValidateMetadataRefusals(t *testing.T) {
	now, ctx, report, metadata, profile := reportedFixture()
	valid, _ := json.Marshal(metadata)
	cases := map[string][]byte{
		"invalid UTF-8":    append(append([]byte{}, valid...), 0xff),
		"unknown":          []byte(strings.Replace(string(valid), "{", "{\"unknown\":true,", 1)),
		"duplicate":        []byte(strings.Replace(string(valid), "{\"protocol\":\"reported-v1\"", "{\"protocol\":\"reported-v1\",\"protocol\":\"reported-v1\"", 1)),
		"trailing":         append(append([]byte{}, valid...), []byte(" {}")...),
		"missing exit":     []byte(strings.Replace(string(valid), ",\"exit_code\":1", "", 1)),
		"invalid protocol": []byte(strings.Replace(string(valid), "reported-v1", "reported-v2", 1)),
		"incomplete":       []byte(strings.Replace(string(valid), "\"report_complete\":true", "\"report_complete\":false", 1)),
		"digest mismatch":  []byte(strings.Replace(string(valid), metadata.ReportDigest, Digest([]byte("other")), 1)),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Validate(report, raw, ctx, profile, now); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	badProfile := *profile
	badProfile.Format = "other"
	if _, err := Validate(report, valid, ctx, &badProfile, now); err == nil {
		t.Fatal("invalid direct-call profile accepted")
	}
}

func TestCompatibilityBindsHazardsProfileRunnerAndSupportNotImplementation(t *testing.T) {
	_, ctx, _, metadata, profile := reportedFixture()
	base := Compatibility(ctx, profile, metadata.Runner)["example.test/p::TestWork"]
	implementation := ctx
	implementation.Candidate.Artifacts = map[string]string{"work_test.go": ctx.Candidate.Artifacts["work_test.go"], "work.go": Digest([]byte("changed"))}
	if got := Compatibility(implementation, profile, metadata.Runner)["example.test/p::TestWork"]; got != base {
		t.Fatal("implementation-only change invalidated red")
	}
	changed := ctx
	changed.DeclaredHazards = map[string][]string{"example.test/p::TestWork": {"external-format"}}
	if Compatibility(changed, profile, metadata.Runner)["example.test/p::TestWork"] == base {
		t.Fatal("hazard change retained compatibility")
	}
	changedProfile := *profile
	changedProfile.EnvironmentKeys = []string{"BUILD_CONTEXT"}
	if Compatibility(ctx, &changedProfile, Runner{Identity: "repo-tests", EnvironmentIdentities: map[string]string{"BUILD_CONTEXT": "opaque"}})["example.test/p::TestWork"] == base {
		t.Fatal("profile change retained compatibility")
	}
}

func TestBuildContextRejectsUnsafeAndOwnedPaths(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "P")
	if err := os.MkdirAll(filepath.Join(planDir, ".graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{filepath.Join(planDir, "README.md"): "---\ntitle: P\ntype: plan\nstatus: active\ncreated: 2026-09-15\nupdated: 2026-09-15\ntags: []\nrelated: []\nphases: []\n---\n", filepath.Join(planDir, "P-Graph.json"): "{}", filepath.Join(root, "work_test.go"): "package p", filepath.Join(root, "work.go"): "package p"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	base := model.Node{ID: "n", Contract: "c", Hazards: model.Hazards{}, Artifacts: []string{"work.go", "work_test.go"}, Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceReportedV1, Report: &model.ReportProfile{Format: "go-test-json-v1", Runner: "repo", EnvironmentKeys: []string{}, TestSupportInputs: []string{}, TestSupportArtifacts: []string{}}, Tests: []model.Test{{Package: "example.test/p", ID: "TestWork", File: "work_test.go"}}}, Claim: &model.Claim{By: "worker", Instance: "nonce", LeaseExpires: now.Add(time.Hour).Format(time.RFC3339)}, Estimate: 1}
	if _, err := BuildContext(root, root, planDir, &model.Graph{Version: 1, Nodes: []model.Node{base}}, &base, "worker", now); err != nil {
		t.Fatalf("safe context refused: %v", err)
	}
	for name, path := range map[string]string{"traversal": "../escape", "absolute": filepath.Join(root, "work_test.go"), "windows": "C:/escape"} {
		t.Run(name, func(t *testing.T) {
			n := base
			n.Artifacts = []string{path, "work_test.go"}
			if _, err := BuildContext(root, root, planDir, &model.Graph{Version: 1, Nodes: []model.Node{n}}, &n, "worker", now); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	ownedSource := base
	ownedSource.Artifacts = []string{"work.go", "Plans/P/P-Graph.json"}
	ownedSource.Gate.Tests = append([]model.Test(nil), base.Gate.Tests...)
	ownedSource.Gate.Tests[0].File = "Plans/P/P-Graph.json"
	if _, err := BuildContext(root, root, planDir, &model.Graph{Version: 1, Nodes: []model.Node{ownedSource}}, &ownedSource, "worker", now); err == nil {
		t.Fatal("graph-owned selected test source accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.go")); err == nil {
		symlink := base
		symlink.Artifacts = []string{"escape.go", "work_test.go"}
		if _, err := BuildContext(root, root, planDir, &model.Graph{Version: 1, Nodes: []model.Node{symlink}}, &symlink, "worker", now); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
	owned := base
	owned.Inputs = []model.Input{{Root: model.InputRootPlanning, Path: "Plans/P/P-Graph.json"}}
	if _, err := BuildContext(root, root, planDir, &model.Graph{Version: 1, Nodes: []model.Node{owned}}, &owned, "worker", now); err == nil {
		t.Fatal("graph-owned planning input accepted")
	}
	if _, err := BuildContext(root, root, planDir, nil, nil, "worker", now); err == nil {
		t.Fatal("nil graph/node accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "source-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	directory := base
	directory.Artifacts = []string{"source-dir", "work_test.go"}
	if _, err := BuildContext(root, root, planDir, &model.Graph{Version: 1, Nodes: []model.Node{directory}}, &directory, "worker", now); err == nil || !strings.Contains(err.Error(), "directory sources are unsupported") {
		t.Fatalf("directory source refusal unclear: %v", err)
	}
	withDep := base
	withDep.Deps = []string{"dep"}
	dep := model.Node{ID: "dep", Artifacts: []string{"../escape"}}
	if _, err := BuildContext(root, root, planDir, &model.Graph{Version: 1, Nodes: []model.Node{withDep, dep}}, &withDep, "worker", now); err == nil {
		t.Fatal("unsafe dependency artifact accepted")
	}
}
