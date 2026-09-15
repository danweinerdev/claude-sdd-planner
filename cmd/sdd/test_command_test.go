package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func TestTestCommandExitCodesAndNoReexecution(t *testing.T) {
	if _, _, _, err := testRoots("../Pilot"); err == nil {
		t.Fatal("testRoots accepted traversal")
	}
	if _, err := planDirFor("../Pilot", "sync"); err == nil {
		t.Fatal("planDirFor accepted traversal")
	}
	root := cliEvidenceFixture(t, false)
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	runErr, out := executeEvidenceCLI(t, "test", "run", "--plan", "Pilot", "--node", "work", "--by", "worker", "--phase", "green")
	if code := exitCode(runErr); code != 0 {
		t.Fatalf("run exit=%d err=%v out=%s", code, runErr, out)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		t.Fatalf("run output %q", out)
	}
	attempt := strings.TrimSuffix(fields[1], ":")
	marker := filepath.Join(root, "runs.txt")
	before := readEvidenceCLI(t, marker)
	err, _ := executeEvidenceCLI(t, "test", "check", "--plan", "Pilot", "--node", "work", "--attempt", attempt, "--expect", "green")
	if code := exitCode(err); code != 0 {
		t.Fatalf("check exit=%d err=%v", code, err)
	}
	if got := readEvidenceCLI(t, marker); got != before {
		t.Fatal("check re-executed selected test")
	}
	err, _ = executeEvidenceCLI(t, "test", "check", "--plan", "Pilot", "--node", "work", "--attempt", attempt, "--expect", "red")
	if code := exitCode(err); code != 1 {
		t.Fatalf("mismatch check exit=%d err=%v", code, err)
	}
	err, _ = executeEvidenceCLI(t, "test", "check", "--plan", "Pilot", "--node", "work", "--attempt", "at-00000000000000000000000000000000", "--expect", "green")
	if code := exitCode(err); code != 2 {
		t.Fatalf("missing header exit=%d err=%v", code, err)
	}
	reportPath := filepath.Join(root, "legacy.json")
	if e := os.WriteFile(reportPath, []byte("{}\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	err, _ = executeEvidenceCLI(t, "graph", "sync", "--plan", "Pilot", "--node", "work", "--by", "worker", "--report", reportPath)
	if exitCode(err) != 1 || !strings.Contains(err.Error(), "requires observed capture") {
		t.Fatalf("observed raw import err=%v", err)
	}
	err, _ = executeEvidenceCLI(t, "graph", "sync", "--plan", "Pilot", "--node", "work", "--by", "worker", "--attempt", attempt, "--command-exit", "0")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("attempt command conflict err=%v", err)
	}
	err, _ = executeEvidenceCLI(t, "graph", "sync", "--plan", "Pilot", "--node", "work", "--by", "worker", "--attempt", attempt)
	if code := exitCode(err); code != 0 {
		t.Fatalf("sync exit=%d err=%v", code, err)
	}
	if got := readEvidenceCLI(t, marker); got != before {
		t.Fatal("sync re-executed selected test")
	}
	err, _ = executeEvidenceCLI(t, "test", "cleanup", "--plan", "Pilot", "--node", "work", "--attempt", attempt)
	if code := exitCode(err); code != 0 {
		t.Fatalf("cleanup exit=%d err=%v", code, err)
	}
	err, _ = executeEvidenceCLI(t, "graph", "sync", "--plan", "Pilot", "--node", "work", "--attempt", attempt)
	if code := exitCode(err); code != 0 {
		t.Fatalf("cleaned replay exit=%d err=%v", code, err)
	}

	failing := cliEvidenceFixture(t, true)
	if err := os.Chdir(failing); err != nil {
		t.Fatal(err)
	}
	err, _ = executeEvidenceCLI(t, "test", "run", "--plan", "Pilot", "--node", "work", "--by", "worker", "--phase", "red", "--red-kind", "baseline")
	if code := exitCode(err); code != 1 {
		t.Fatalf("red run exit=%d err=%v", code, err)
	}
	if err := os.Remove(filepath.Join(failing, "go.mod")); err != nil {
		t.Fatal(err)
	}
	err, _ = executeEvidenceCLI(t, "test", "run", "--plan", "Pilot", "--node", "work", "--by", "worker", "--phase", "diagnostic")
	if code := exitCode(err); code != 2 {
		t.Fatalf("unusable run exit=%d err=%v", code, err)
	}
}

func executeEvidenceCLI(t *testing.T, args ...string) (error, string) {
	t.Helper()
	cmd := newRootCmd()
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetErr(&b)
	cmd.SetArgs(args)
	return cmd.Execute(), b.String()
}
func readEvidenceCLI(t *testing.T, path string) string {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func cliEvidenceFixture(t *testing.T, fail bool) string {
	t.Helper()
	root := t.TempDir()
	plan := filepath.Join(root, "Plans", "Pilot")
	if e := os.MkdirAll(plan, 0o755); e != nil {
		t.Fatal(e)
	}
	writeCLI := func(path, body string) {
		if e := os.WriteFile(path, []byte(body), 0o644); e != nil {
			t.Fatal(e)
		}
	}
	writeCLI(filepath.Join(root, "planning-config.json"), `{"planningRoot":"."}`)
	writeCLI(filepath.Join(plan, "README.md"), "---\ntitle: Pilot\ntype: plan\nstatus: active\ncreated: 2026-09-14\nupdated: 2026-09-14\ntags: []\nrelated: []\nphases: []\n---\n\n# Pilot\n")
	writeCLI(filepath.Join(root, "go.mod"), "module example.test/pilot\n\ngo 1.22\n")
	body := "package pilot\nimport (\"os\";\"testing\")\nfunc TestValue(t *testing.T){f,_:=os.OpenFile(\"runs.txt\",os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);_,_=f.WriteString(\"x\");_=f.Close()"
	if fail {
		body += ";t.Fatal(\"red\")"
	}
	body += "}\n"
	writeCLI(filepath.Join(root, "subject_test.go"), body)
	writeCLI(filepath.Join(root, "runs.txt"), "")
	if _, e := gstore.Init(plan); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	n := model.Node{ID: "work", Contract: "c", Hazards: model.Hazards{}, Artifacts: []string{"subject_test.go", "go.mod"}, Estimate: 1, Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceObservedV1, Tests: []model.Test{{ID: "TestValue", File: "subject_test.go"}}, Execution: &model.ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 30}}, Claim: &model.Claim{By: "worker", Instance: "one", LeaseExpires: now.Add(2 * time.Minute).Format(time.RFC3339)}}
	if _, e := gstore.Update(gstore.PathFor(plan), func(g *model.Graph) error { g.Nodes = []model.Node{n}; return nil }); e != nil {
		t.Fatal(e)
	}
	return root
}
