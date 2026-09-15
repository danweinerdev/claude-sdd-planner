package sync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testevidence"
)

func TestObservedAdmissionAndHistoricalReplay(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, "Plans", "Pilot")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeObserved(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":"."}`)
	writeObserved(t, filepath.Join(planDir, "README.md"), "---\ntitle: Pilot\ntype: plan\nstatus: active\ncreated: 2026-09-14\nupdated: 2026-09-14\ntags: []\nrelated: []\nphases: []\n---\n\n# Pilot\n")
	writeObserved(t, filepath.Join(root, "go.mod"), "module example.test/pilot\n\ngo 1.22\n")
	writeObserved(t, filepath.Join(root, "subject.go"), "package pilot\nfunc Value() int { return 1 }\n")
	writeObserved(t, filepath.Join(root, "subject_test.go"), "package pilot\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=2 { t.Fatal(\"red\") } }\n")
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	n := model.Node{ID: "work", Contract: "value", Hazards: model.Hazards{"wrong-result"}, Artifacts: []string{"subject.go", "subject_test.go", "go.mod"}, Estimate: 1, Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceObservedV1, Tests: []model.Test{{ID: "TestValue", File: "subject_test.go", Satisfies: []string{"wrong-result"}}}, Execution: &model.ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 30}}, Claim: &model.Claim{By: "worker", Instance: "one", LeaseExpires: now.Add(2 * time.Minute).Format(time.RFC3339)}}
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error { g.Nodes = []model.Node{n}; return nil }); err != nil {
		t.Fatal(err)
	}
	cap, err := testevidence.Run(context.Background(), testevidence.RunOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", Phase: "red", RedKind: "baseline", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	drift := Options{PlanDir: planDir, RepoRoot: root, Node: "work", By: "worker", AttemptID: cap.AttemptID, Now: func() time.Time { return now }, beforePublish: func() error {
		return os.WriteFile(filepath.Join(root, "subject.go"), []byte("package pilot\nfunc Value() int { return 9 }\n"), 0o644)
	}}
	if _, err := Run(drift); err == nil || !strings.Contains(err.Error(), "became inadmissible") {
		t.Fatalf("candidate drift admission err=%v", err)
	}
	writeObserved(t, filepath.Join(root, "subject.go"), "package pilot\nfunc Value() int { return 1 }\n")
	clock := now
	expired := Options{PlanDir: planDir, RepoRoot: root, Node: "work", By: "worker", AttemptID: cap.AttemptID, Now: func() time.Time { return clock }, beforePublish: func() error { clock = now.Add(3 * time.Minute); return nil }}
	if _, err := Run(expired); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired publication err=%v", err)
	}
	var wg sync.WaitGroup
	results := make(chan *Result, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := Run(Options{PlanDir: planDir, RepoRoot: root, Node: "work", By: "worker", AttemptID: cap.AttemptID, Now: func() time.Time { return now }})
			if e != nil {
				errs <- e
			} else {
				results <- r
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	var res *Result
	historical := 0
	for r := range results {
		if r.Historical {
			historical++
		} else {
			res = r
		}
	}
	if historical != 1 || res == nil {
		t.Fatalf("concurrent admissions historical=%d result=%+v", historical, res)
	}
	if !res.Recorded || res.Observation.Attempt == nil {
		t.Fatalf("admission = %+v", res)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes[0].RedEvidence) != 1 || g.Nodes[0].ConsumedAttempts[cap.AttemptID].Seq != 1 {
		t.Fatalf("stored node = %+v", g.Nodes[0])
	}
	writeObserved(t, filepath.Join(root, "subject.go"), "package pilot\nfunc Value() int { return 2 }\n")
	writeObserved(t, filepath.Join(root, "new_test.go"), "package pilot\nimport \"testing\"\nfunc TestNew(t *testing.T){}\n")
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes[0].Artifacts = append(g.Nodes[0].Artifacts, "new_test.go")
		g.Nodes[0].Gate.Tests = append(g.Nodes[0].Gate.Tests, model.Test{ID: "TestNew", File: "new_test.go"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	green, err := testevidence.Run(context.Background(), testevidence.RunOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", Phase: "green", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	checked, err := testevidence.Check(testevidence.CheckOptions{PlanDir: planDir, PlanningRoot: root, RepoRoot: root, Node: "work", By: "worker", AttemptID: green.AttemptID, Expect: "green", Now: func() time.Time { return now }})
	if err != nil || !checked.Eligible {
		t.Fatalf("old red compatibility after unrelated test addition: %+v, %v", checked, err)
	}
	greenSync, err := Run(Options{PlanDir: planDir, RepoRoot: root, Node: "work", By: "worker", AttemptID: green.AttemptID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if greenSync.Observation.Result != model.ResultPass {
		t.Fatalf("green sync=%+v", greenSync)
	}
	g, err = gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes[0].RedEvidence) != 1 || g.Nodes[0].Verification.Result != model.ResultPass {
		t.Fatalf("green replaced compatible red history: %+v", g.Nodes[0])
	}
	cleaned, err := testevidence.Cleanup(testevidence.CleanupOptions{PlanDir: planDir, Node: "work", AttemptID: cap.AttemptID})
	if err != nil {
		t.Fatal(err)
	}
	if !cleaned.Removed || !cleaned.Consumed {
		t.Fatalf("cleanup=%+v", cleaned)
	}
	replay, err := Run(Options{PlanDir: planDir, RepoRoot: root, Node: "work", By: "foreign", AttemptID: cap.AttemptID})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Historical || replay.Observation.Seq != 1 {
		t.Fatalf("replay = %+v", replay)
	}
	g, err = gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		t.Fatal(err)
	}
	if g.SeqCounter != 2 || g.Nodes[0].Verification.Result != model.ResultPass || g.Nodes[0].Claim != nil {
		t.Fatalf("red replay mutated green node: %+v", g.Nodes[0])
	}
}

func TestAttemptModeNamesUnsupportedGateType(t *testing.T) {
	for _, gate := range []model.Gate{{Type: model.GateCommand, Command: "true"}, {Type: model.GateReview}} {
		t.Run(gate.Type, func(t *testing.T) {
			plan, repo := fixture(t, model.Node{ID: "n", Contract: "c", Gate: gate, Hazards: model.Hazards{}, Estimate: 1})
			if _, err := Run(Options{PlanDir: plan, RepoRoot: repo, Node: "n", AttemptID: "at-00000000000000000000000000000000"}); err == nil || !strings.Contains(err.Error(), "unsupported for "+gate.Type+" gate") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func writeObserved(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
