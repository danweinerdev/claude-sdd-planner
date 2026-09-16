package testevidence

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/evidencecost"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/claims"
	graphdigest "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

func TestRunCheckAndTamperRefusal(t *testing.T) {
	t.Setenv("GOWORK", filepath.Join(t.TempDir(), "missing.work"))
	t.Setenv("GOENV", filepath.Join(t.TempDir(), "host-goenv"))
	t.Setenv("GOTOOLCHAIN", "invalid-ambient-toolchain")
	t.Setenv("GOFLAGS", "-run=Never")
	t.Setenv("FEATURE_MODE", "a")
	t.Setenv("GOPATH", "")
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "Pilot")
	mustMkdir(t, planDir)
	mustWriteAttempt(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":"."}`)
	mustWriteAttempt(t, filepath.Join(planDir, "README.md"), "---\ntitle: Pilot\ntype: plan\nstatus: active\ncreated: 2026-09-14\nupdated: 2026-09-14\ntags: []\nrelated: []\nphases: []\n---\n\n# Pilot\n")
	mustWriteAttempt(t, filepath.Join(root, "go.mod"), "module example.test/pilot\n\ngo 1.22\n")
	mustWriteAttempt(t, filepath.Join(root, "subject.go"), "package pilot\n\nfunc Value() int { return 1 }\n")
	mustWriteAttempt(t, filepath.Join(root, "subject_test.go"), "// valid package comment\npackage pilot\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) { if Value() != 2 { t.Fatalf(\"not implemented\") } }\n")
	mustWriteAttempt(t, filepath.Join(root, "vendor", "inert", "go.mod"), "module inert.example/module\n")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	n := model.Node{ID: "work", Contract: "value is two", Hazards: model.Hazards{"wrong-result"}, Artifacts: []string{"subject.go", "subject_test.go", "go.mod"}, Estimate: 1,
		Gate:  model.Gate{Type: model.GateTests, Evidence: model.EvidenceObservedV1, Tests: []model.Test{{ID: "TestValue", File: "subject_test.go", Satisfies: []string{"wrong-result"}}}, Execution: &model.ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 30, EnvironmentKeys: []string{"FEATURE_MODE", "GOPATH"}}},
		Claim: &model.Claim{By: "worker", Instance: "claim-one", LeaseExpires: now.Add(2 * time.Minute).Format(time.RFC3339)}}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.Nodes = []model.Node{n}; return nil }); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), RunOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", Phase: "red", RedKind: "baseline", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExecutionStatus != "failed" || res.AttemptID == "" {
		t.Fatalf("run = %+v", res)
	}
	a, err := Load(planDir, "work", res.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"GOWORK", "GOENV", "GOTOOLCHAIN", "GOFLAGS"} {
		if !strings.HasPrefix(a.Profile.Environment[k], "sha256:") {
			t.Fatalf("controlled %s identity missing", k)
		}
	}
	if a.Profile.Environment["GOPATH"] == digestBytes(nil) {
		t.Fatal("unset GOPATH recorded empty instead of effective go env default")
	}
	checkCost := evidencecost.New(nil)
	checked, err := Check(CheckOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", AttemptID: res.AttemptID, Expect: "red", Now: func() time.Time { return now }, Cost: checkCost})
	if err != nil {
		t.Fatal(err)
	}
	if !checked.Eligible || len(checked.Failed) != 1 {
		t.Fatalf("check = %+v", checked)
	}
	if got := checked.Cost.Counters[evidencecost.BundleReadRequests]; got != 3 {
		t.Fatalf("public check bundle reads = %d, want 3 (one header and two raw outputs)", got)
	}
	t.Setenv("FEATURE_MODE", "b")
	profileDrift, err := Check(CheckOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", AttemptID: res.AttemptID, Expect: "red", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if profileDrift.Eligible || !strings.Contains(profileDrift.Refusal, "profile changed") {
		t.Fatalf("profile drift check=%+v", profileDrift)
	}
	t.Setenv("FEATURE_MODE", "a")
	rawPath := filepath.Join(planDir, ".graph", "test-evidence", "work", res.AttemptID, "stdout.json")
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, append(raw, []byte(" \n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	checked, err = Check(CheckOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", AttemptID: res.AttemptID, Expect: "red", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if checked.Eligible || !strings.Contains(checked.Refusal, "integrity") {
		t.Fatalf("tamper check = %+v", checked)
	}
	mustWriteAttempt(t, filepath.Join(root, "subject.go"), "package pilot\n\nfunc Value() int { return 2 }\n")
	green, err := Run(context.Background(), RunOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", Phase: "green", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if green.ExecutionStatus != "passed" {
		t.Fatalf("green run = %+v", green)
	}
	checked, err = Check(CheckOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", AttemptID: green.AttemptID, Expect: "green", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if checked.Eligible || !strings.Contains(checked.Refusal, "compatible prior observed red") {
		t.Fatalf("green without admitted red check = %+v", checked)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes[0].Artifacts = []string{"subject.go", "subject_test.go"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), RunOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", Phase: "diagnostic", Now: func() time.Time { return now }}); err == nil || !strings.Contains(err.Error(), "module file go.mod must be") {
		t.Fatalf("undeclared module file err=%v", err)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes[0].Artifacts = []string{"subject.go", "subject_test.go", "go.mod"}
		g.Nodes[0].Gate.Execution.TimeoutSeconds = 1
		g.Nodes[0].Claim.LeaseExpires = now.Add(30 * time.Second).Format(time.RFC3339)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(planDir, ".graph", "test-evidence", "work")
	beforeEntries, _ := os.ReadDir(base)
	if res, err := Run(context.Background(), RunOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", Phase: "diagnostic", Now: func() time.Time { return now }}); err == nil || res != nil || !strings.Contains(err.Error(), "headroom") {
		t.Fatalf("near-expiry run res=%+v err=%v", res, err)
	}
	afterEntries, _ := os.ReadDir(base)
	if len(afterEntries) != len(beforeEntries) {
		t.Fatal("near-expiry refusal allocated an attempt")
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes[0].Claim.LeaseExpires = now.Add(2 * time.Minute).Format(time.RFC3339)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mustWriteAttempt(t, filepath.Join(root, "subject_test.go"), "package pilot\nimport (\"testing\";\"time\")\nfunc TestValue(t *testing.T){time.Sleep(3*time.Second)}\n")
	timed, err := Run(context.Background(), RunOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", Phase: "diagnostic", Now: func() time.Time { return now }, Cost: evidencecost.New(nil)})
	if err == nil || timed == nil || timed.AttemptID == "" || timed.Complete || timed.ExecutionStatus != "execution-incomplete" {
		t.Fatalf("timeout result=%+v err=%v", timed, err)
	}
	incompletePath := filepath.Join(base, timed.AttemptID, "incomplete.json")
	if _, err := os.Stat(incompletePath); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(incompletePath); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(raw), `"cost"`) {
		t.Fatalf("incomplete bundle persisted invocation cost:\n%s", raw)
	}
}

func TestSnapshotUsesClaimWorkspaceForRepositoryInputs(t *testing.T) {
	planning := t.TempDir()
	workspace := t.TempDir()
	planDir := filepath.Join(planning, "Plans", "Pilot")
	mustMkdir(t, planDir)
	mustWriteAttempt(t, filepath.Join(planning, "planning-config.json"), `{"planningRoot":"."}`)
	mustWriteAttempt(t, filepath.Join(planDir, "README.md"), "---\ntitle: Pilot\ntype: plan\nstatus: active\ncreated: 2026-09-14\nupdated: 2026-09-14\ntags: []\nrelated: []\nphases: []\n---\n\n# Pilot\n")
	for _, root := range []string{planning, workspace} {
		mustWriteAttempt(t, filepath.Join(root, "go.mod"), "module example.test/pilot\n\ngo 1.22\n")
		mustWriteAttempt(t, filepath.Join(root, "subject.go"), "package pilot\nfunc Value() int{return 1}\n")
		mustWriteAttempt(t, filepath.Join(root, "subject_test.go"), "package pilot\nimport \"testing\"\nfunc TestValue(t *testing.T){}\n")
	}
	mustWriteAttempt(t, filepath.Join(planning, "context.txt"), "main")
	mustWriteAttempt(t, filepath.Join(workspace, "context.txt"), "workspace")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	in := model.Input{Root: model.InputRootRepository, Path: "context.txt"}
	n := model.Node{ID: "work", Contract: "c", Hazards: model.Hazards{}, Artifacts: []string{"subject.go", "subject_test.go", "go.mod"}, Inputs: []model.Input{in}, Estimate: 1, Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceObservedV1, Tests: []model.Test{{ID: "TestValue", File: "subject_test.go"}}, Execution: &model.ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 30}}, Claim: &model.Claim{By: "worker", Instance: "one", Workspace: workspace, LeaseExpires: now.Add(time.Minute).Format(time.RFC3339)}}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.Nodes = []model.Node{n}; return nil }); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), RunOptions{PlanDir: planDir, PlanningRoot: planning, RepoRoot: planning, Node: "work", By: "worker", Phase: "green", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	a, err := Load(planDir, "work", res.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	want := digestBytes([]byte("workspace"))
	if got := a.Before.Inputs[model.InputKey(in)]; got != want {
		t.Fatalf("input digest=%s want workspace %s", got, want)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.Nodes[0].Claim.Instance = "takeover"; return nil }); err != nil {
		t.Fatal(err)
	}
	taken, err := Check(CheckOptions{PlanDir: planDir, PlanningRoot: planning, RepoRoot: planning, Node: "work", AttemptID: res.AttemptID, Expect: "green", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if taken.Eligible || !strings.Contains(taken.Refusal, "claim instance") {
		t.Fatalf("takeover check=%+v", taken)
	}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.Nodes[0].Claim.Instance = "one"; return nil }); err != nil {
		t.Fatal(err)
	}
	mustWriteAttempt(t, filepath.Join(workspace, "context.txt"), "changed")
	checked, err := Check(CheckOptions{PlanDir: planDir, PlanningRoot: planning, RepoRoot: planning, Node: "work", AttemptID: res.AttemptID, Expect: "green", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if checked.Eligible || !strings.Contains(checked.Refusal, "changed") {
		t.Fatalf("changed workspace input check=%+v", checked)
	}
}

func TestCompatibilityExcludesOtherSelectedTests(t *testing.T) {
	profile := Profile{Adapter: "go-test-v1", Args: []string{}, Argv: []string{"go", "test", "derived-selection"}, Environment: map[string]string{"GOENV": digestBytes([]byte("off"))}}
	base := Candidate{Artifacts: map[string]string{}, Inputs: map[string]string{}, SelectedTestSources: map[string]string{"example/p::TestOld": "old-source", "example/p::TestNew": "new-source"}}
	a := &Attempt{Profile: profile, Before: base, Selected: []SelectedTest{{Package: "example/p", ID: "TestOld", File: "old_test.go"}}}
	n := &model.Node{Gate: model.Gate{Execution: &model.ExecutionProfile{}, Tests: []model.Test{{ID: "TestOld", File: "old_test.go", Satisfies: []string{"h"}}}}}
	old := compatibility(n, a)["example/p::TestOld"]
	a.Selected = append(a.Selected, SelectedTest{Package: "example/p", ID: "TestNew", File: "new_test.go"})
	a.Profile.Argv = []string{"go", "test", "different-derived-selection"}
	n.Gate.Tests = append(n.Gate.Tests, model.Test{ID: "TestNew", File: "new_test.go"})
	if got := compatibility(n, a)["example/p::TestOld"]; got != old {
		t.Fatalf("unrelated selected test changed old compatibility: %s != %s", got, old)
	}
}

func TestAttemptStorageRefusesEscapingSymlink(t *testing.T) {
	plan := t.TempDir()
	outside := t.TempDir()
	mustMkdir(t, filepath.Join(plan, ".graph"))
	link := filepath.Join(plan, ".graph", "test-evidence")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation genuinely unsupported: %v", err)
	}
	if _, _, err := createAttemptDir(plan, "work"); err == nil {
		t.Fatal("created attempt through escaping symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("wrote outside plan: %s", fmt.Sprint(entries))
	}
}

func TestCleanupRefusesActiveAndRemovesReleasedIncomplete(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "Plans", "Pilot")
	mustMkdir(t, plan)
	if _, err := gstore.Init(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Update(gstore.PathFor(plan), func(g *model.Graph) error {
		g.Nodes = []model.Node{{ID: "work", Contract: "c", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Estimate: 1}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id, dir, err := createAttemptDir(plan, "work")
	if err != nil {
		t.Fatal(err)
	}
	release, err := istore.AcquireExclusiveLock(attemptLockPath(plan, "work", id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Cleanup(CleanupOptions{PlanDir: plan, Node: "work", AttemptID: id}); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("active cleanup err=%v", err)
	}
	release()
	if err := writeExclusive(filepath.Join(dir, "incomplete.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	res, err := Cleanup(CleanupOptions{PlanDir: plan, Node: "work", AttemptID: id, Now: time.Now, MinInactive: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Removed {
		t.Fatalf("cleanup=%+v", res)
	}
}

func TestDirectoryArtifactSnapshotMatchesStateDigester(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "Plans", "Pilot")
	mustMkdir(t, plan)
	mustWriteAttempt(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":"."}`)
	mustWriteAttempt(t, filepath.Join(plan, "README.md"), "---\ntitle: Pilot\ntype: plan\nstatus: active\ncreated: 2026-09-14\nupdated: 2026-09-14\ntags: []\nrelated: []\nphases: []\n---\n\n# Pilot\n")
	mustWriteAttempt(t, filepath.Join(root, "go.mod"), "module example.test/pilot\n")
	mustWriteAttempt(t, filepath.Join(root, "subject_test.go"), "package pilot\n")
	mustWriteAttempt(t, filepath.Join(root, "own", "a.txt"), "a")
	mustWriteAttempt(t, filepath.Join(root, "dep", "b.txt"), "b")
	dep := model.Node{ID: "dep", Contract: "d", Gate: model.Gate{Type: model.GateCommand, Command: "true"}, Hazards: model.Hazards{}, Artifacts: []string{"dep/"}, Estimate: 1}
	node := model.Node{ID: "work", Contract: "c", Deps: []string{"dep"}, Gate: model.Gate{Type: model.GateTests, Tests: []model.Test{{ID: "TestX", File: "subject_test.go"}}}, Hazards: model.Hazards{}, Artifacts: []string{"own/", "subject_test.go", "go.mod"}, Estimate: 1}
	g := &model.Graph{Version: 1, Nodes: []model.Node{dep, node}}
	c, err := snapshot(root, root, plan, root, g, &g.Nodes[1], []SelectedTest{{Package: "example.test/pilot", ID: "TestX", File: "subject_test.go"}})
	if err != nil {
		t.Fatal(err)
	}
	d := graphdigest.New(root)
	if c.Artifacts["own/"] != d.Artifact("own/") || c.Dependencies["dep"]["dep/"] != d.Artifact("dep/") {
		t.Fatalf("directory snapshot differs from state digester: %+v", c)
	}
	g.Nodes[0].Verification = &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean, ArtifactDigests: map[string]string{"dep/": d.Artifact("dep/")}, DependencyDigests: map[string]map[string]string{}}
	g.Nodes[1].Verification = &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean, ArtifactDigests: c.Artifacts, DependencyDigests: c.Dependencies}
	if got := states.Derive(states.Inputs{Graph: g, ArtifactDigest: graphdigest.New(root).Artifact})["work"].State; got != states.Green {
		t.Fatalf("state=%s", got)
	}
	mustWriteAttempt(t, filepath.Join(root, "dep", "b.txt"), "changed")
	if got := states.Derive(states.Inputs{Graph: g, ArtifactDigest: graphdigest.New(root).Artifact})["work"].State; got != states.Stale {
		t.Fatalf("changed directory dependency state=%s", got)
	}
	mustMkdir(t, filepath.Join(root, "own", ".graph"))
	if _, err := hashRootFile(root, "own/", plan); err == nil {
		t.Fatal("directory containing live graph runtime was accepted")
	}
}

func TestRunFromActualGitProviderWorkspaceInsidePlanGraphDir(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, ".plans", "Plans", "Pilot")
	mustMkdir(t, plan)
	mustWriteAttempt(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":".plans"}`)
	mustWriteAttempt(t, filepath.Join(plan, "README.md"), "---\ntitle: Pilot\ntype: plan\nstatus: active\ncreated: 2026-09-14\nupdated: 2026-09-14\ntags: []\nrelated: []\nphases: []\n---\n\n# Pilot\n")
	mustWriteAttempt(t, filepath.Join(root, "go.mod"), "module example.test/pilot\n")
	mustWriteAttempt(t, filepath.Join(root, "subject_test.go"), "package pilot\nimport \"testing\"\nfunc TestValue(t *testing.T){t.Fatal(\"red\")}\n")
	if _, err := gstore.Init(plan); err != nil {
		t.Fatal(err)
	}
	n := model.Node{ID: "work", Contract: "c", Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceObservedV1, Tests: []model.Test{{ID: "TestValue", File: "subject_test.go"}}, Execution: &model.ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 30}}, Hazards: model.Hazards{}, Artifacts: []string{"subject_test.go", "go.mod"}, Estimate: 1}
	if _, err := gstore.Update(gstore.PathFor(plan), func(g *model.Graph) error { g.Nodes = []model.Node{n}; return nil }); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = root
		c.Env = os.Environ()
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "fixture@example.invalid")
	runGit("config", "user.name", "Fixture")
	runGit("add", ".")
	runGit("commit", "-m", "fixture")
	p, err := provider.DetectChecked(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed, err := claims.ClaimNode(plan, "work", claims.Options{By: "worker", TTL: 2 * time.Minute, Now: func() time.Time { return now }, Provider: provider.ForClaims(p)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(claimed.Workspace), "/.graph/ws-work") {
		t.Fatalf("workspace not provider-owned under plan graph dir: %s", claimed.Workspace)
	}
	res, err := Run(context.Background(), RunOptions{PlanDir: plan, PlanningRoot: filepath.Join(root, ".plans"), RepoRoot: root, Node: "work", By: "worker", Phase: "red", RedKind: "baseline", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExecutionStatus != "failed" {
		t.Fatalf("run=%+v", res)
	}
	workspace := claimed.Workspace
	if !filepath.IsAbs(workspace) {
		workspace = filepath.Join(root, filepath.FromSlash(workspace))
	}
	copiedGraph := filepath.ToSlash(filepath.Join(".plans", "Plans", "Pilot", "Pilot-Graph.json"))
	if _, err := hashRootFile(workspace, copiedGraph, plan); err == nil {
		t.Fatal("copied own graph inside current workspace was accepted")
	}
	foreignEvidence := filepath.Join(workspace, ".plans", "Plans", "Other", ".graph", "test-evidence", "x", "raw.json")
	mustWriteAttempt(t, foreignEvidence, "x")
	if _, err := hashRootFile(workspace, filepath.ToSlash(strings.TrimPrefix(foreignEvidence, workspace+string(filepath.Separator))), plan); err == nil {
		t.Fatal("foreign evidence output inside workspace was accepted")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
func mustWriteAttempt(t *testing.T, path, body string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
