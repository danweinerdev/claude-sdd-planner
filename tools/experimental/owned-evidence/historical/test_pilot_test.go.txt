package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testevidence"
)

type pilotVariantMetric struct {
	Variant                string          `json:"variant"`
	SelectedTests          int             `json:"selected_tests"`
	ConcreteTests          int             `json:"concrete_tests"`
	ConcreteExecutions     int             `json:"concrete_executions"`
	TargetRuns             int             `json:"target_runs"`
	BuildChecks            int             `json:"build_checks"`
	BroadRuns              int             `json:"broad_runs"`
	TotalGoInvocations     int             `json:"total_go_invocations"`
	MeaningfulRed          int             `json:"meaningful_red"`
	Green                  bool            `json:"green"`
	InitialSeedMutants     int             `json:"initial_seed_mutants"`
	ReviewDerivedHoldouts  int             `json:"review_derived_holdouts"`
	MutantsCaught          int             `json:"mutants_caught"`
	MutantsMissed          int             `json:"mutants_missed"`
	MutantResults          map[string]bool `json:"mutant_results"`
	Failures               int             `json:"failures"`
	Refusals               int             `json:"refusals"`
	ProcessMilliseconds    int64           `json:"process_ms"`
	PipelineMilliseconds   int64           `json:"pipeline_ms"`
	PipelineOverheadMillis int64           `json:"pipeline_overhead_ms"`
}

type pilotNegativeControlMetric struct {
	Holdout              string `json:"holdout"`
	InitialFixtureMissed bool   `json:"initial_fixture_missed"`
	RefinedFixtureCaught bool   `json:"refined_fixture_caught"`
	ReviewerCorrections  int    `json:"reviewer_corrections"`
	GoInvocations        int    `json:"go_invocations"`
	ProcessMilliseconds  int64  `json:"process_ms"`
	PipelineMilliseconds int64  `json:"pipeline_ms"`
}

type pilotGraphMetric struct {
	NodesRed             int      `json:"nodes_red"`
	RedAdmissions        int      `json:"red_admissions"`
	NodesGreen           int      `json:"nodes_green"`
	Attempts             int      `json:"attempts"`
	TargetRuns           int      `json:"target_runs"`
	BroadRuns            int      `json:"broad_runs"`
	Refusals             []string `json:"refusals"`
	ReviewProtocol       string   `json:"review_protocol"`
	ByteIdenticalRebase  bool     `json:"byte_identical_rebase"`
	DependencyMadeStale  bool     `json:"dependency_made_stale"`
	ReviewGreen          bool     `json:"review_green"`
	WorkClosed           bool     `json:"work_closed_before_staleness"`
	AcceptanceClosed     bool     `json:"acceptance_closed_before_staleness"`
	Rechecks             int      `json:"rechecks"`
	ProcessMilliseconds  int64    `json:"process_ms"`
	PipelineOverheadMS   int64    `json:"pipeline_overhead_ms"`
	PipelineMilliseconds int64    `json:"pipeline_ms"`
}

type pilotMetricLog struct {
	Protocol        string                     `json:"protocol"`
	SampleSize      int                        `json:"sample_size"`
	Variants        []pilotVariantMetric       `json:"variants"`
	NegativeControl pilotNegativeControlMetric `json:"negative_control"`
	Graph           pilotGraphMetric           `json:"graph"`
	Limitation      string                     `json:"limitation"`
}

type pilotProcess struct {
	Exit     int
	Output   []byte
	Duration time.Duration
}

// TestEvidencePilot is the controlled one-module pilot from
// Designs/TestEvidencePipeline. It compares two independently authored test
// families without asserting that either is better, then drives composed tests
// through the public observed-v1 graph protocol. The TEST-FIXTURE review lanes
// below exercise storage and gating only; they are not represented as
// independent model reviews or as evidence of generated-test quality.
func TestEvidencePilot(t *testing.T) {
	fixture := pilotFixtureDir(t)
	baseline := runPilotVariant(t, fixture, "baseline")
	composed := runPilotVariant(t, fixture, "composed")
	negative := runPilotNegativeControl(t, fixture)
	variantRaw, err := json.Marshal([]pilotVariantMetric{baseline, composed})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PILOT_VARIANT_METRICS %s", variantRaw)
	graph := runComposedGraphPilot(t, fixture)

	log := pilotMetricLog{
		Protocol:        "settings-evidence-pilot-v1",
		SampleSize:      1,
		Variants:        []pilotVariantMetric{baseline, composed},
		NegativeControl: negative,
		Graph:           graph,
		Limitation:      "one controlled sample; no statistical or quality-improvement claim",
	}
	raw, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PILOT_METRICS %s", raw)
}

func pilotFixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating pilot fixture")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "internal", "testevidence", "testdata", "settings-pilot"))
	if _, err := os.Stat(filepath.Join(dir, "contract.md")); err != nil {
		t.Fatalf("pilot fixture: %v", err)
	}
	return dir
}

func runPilotVariant(t *testing.T, fixture, variant string) pilotVariantMetric {
	t.Helper()
	started := time.Now()
	dir := t.TempDir()
	pilotMaterialize(t, fixture, dir, "go.mod", "module pilot.test/settings\n\ngo 1.22\n")
	for _, name := range []string{"parse.go", "render.go", "store.go"} {
		pilotCopy(t, filepath.Join(fixture, "scaffold", name+".txt"), filepath.Join(dir, name))
		pilotCopy(t, filepath.Join(fixture, variant, strings.TrimSuffix(name, ".go")+"_test.go.txt"), filepath.Join(dir, strings.TrimSuffix(name, ".go")+"_test.go"))
	}

	selectedIDs := []string{"TestParseContract", "TestRenderContract", "TestStoreContract"}
	metric := pilotVariantMetric{
		Variant: variant, SelectedTests: len(selectedIDs), InitialSeedMutants: 5,
		ReviewDerivedHoldouts: 1, MutantResults: map[string]bool{},
	}
	observe := func(res pilotProcess) {
		metric.TotalGoInvocations++
		metric.ProcessMilliseconds += res.Duration.Milliseconds()
		metric.ConcreteExecutions += pilotRunEvents(res.Output)
	}
	for _, testID := range selectedIDs {
		res := pilotGoTest(t, dir, "-json", "-count=1", "-run", "^"+testID+"$")
		metric.TargetRuns++
		observe(res)
		if res.Exit == 1 && pilotHasBehavioralFailure(res.Output, testID) {
			metric.MeaningfulRed++
			metric.Failures++
		} else {
			t.Fatalf("%s scaffold %s did not produce meaningful RED (exit=%d):\n%s", variant, testID, res.Exit, res.Output)
		}
	}
	for _, name := range []string{"parse.go", "render.go", "store.go"} {
		pilotCopy(t, filepath.Join(fixture, "implementation", name+".txt"), filepath.Join(dir, name))
	}
	green := pilotGoTest(t, dir, "-json", "-count=1", "./...")
	metric.BroadRuns++
	observe(green)
	metric.ConcreteTests = pilotConcreteExecuted(green.Output)
	metric.Green = green.Exit == 0
	if !metric.Green {
		t.Fatalf("%s implementation was not GREEN:\n%s", variant, green.Output)
	}

	mutants, err := filepath.Glob(filepath.Join(fixture, "mutants", "*.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(mutants)
	wantMutants := []string{
		"parse-duplicate-last-wins.go.txt", "render-reverse-key-order.go.txt",
		"render-unsafe-escaping.go.txt", "store-destroys-old-before-callback.go.txt",
		"store-ignores-callback-error.go.txt", "store-load-bypasses-parser.go.txt",
	}
	if len(mutants) != metric.InitialSeedMutants+metric.ReviewDerivedHoldouts || len(mutants) != len(wantMutants) {
		t.Fatalf("pilot mutant inventory changed: got %d, want %d initial plus %d review holdout", len(mutants), metric.InitialSeedMutants, metric.ReviewDerivedHoldouts)
	}
	for i, mutant := range mutants {
		if filepath.Base(mutant) != wantMutants[i] {
			t.Fatalf("pilot mutant inventory: got %q, want %q", filepath.Base(mutant), wantMutants[i])
		}
	}
	for _, mutant := range mutants {
		for _, name := range []string{"parse.go", "render.go", "store.go"} {
			pilotCopy(t, filepath.Join(fixture, "implementation", name+".txt"), filepath.Join(dir, name))
		}
		base := filepath.Base(mutant)
		targetFile, targetTest := pilotMutantTarget(base)
		pilotCopy(t, mutant, filepath.Join(dir, targetFile))
		compile := pilotGoTest(t, dir, "-json", "-count=1", "-run", "^$")
		metric.BuildChecks++
		observe(compile)
		if compile.Exit != 0 {
			t.Fatalf("valid-compiling mutant %s did not compile; it cannot count as behavioral detection:\n%s", base, compile.Output)
		}
		result := pilotGoTest(t, dir, "-json", "-count=1", "-run", "^"+targetTest+"$")
		metric.TargetRuns++
		observe(result)
		caught := result.Exit == 1 && pilotHasBehavioralFailure(result.Output, targetTest)
		metric.MutantResults[strings.TrimSuffix(base, ".go.txt")] = caught
		if caught {
			metric.MutantsCaught++
			metric.Failures++
		} else {
			metric.MutantsMissed++
		}
	}
	metric.PipelineMilliseconds = time.Since(started).Milliseconds()
	metric.PipelineOverheadMillis = metric.PipelineMilliseconds - metric.ProcessMilliseconds
	return metric
}

func pilotMutantTarget(name string) (string, string) {
	switch {
	case strings.HasPrefix(name, "parse-"):
		return "parse.go", "TestParseContract"
	case strings.HasPrefix(name, "render-"):
		return "render.go", "TestRenderContract"
	default:
		return "store.go", "TestStoreContract"
	}
}

func pilotGoTest(t *testing.T, dir string, args ...string) pilotProcess {
	t.Helper()
	started := time.Now()
	res, err := procexec.Capture(context.Background(), "go", append([]string{"test"}, args...), procexec.Policy{
		Dir: dir, Env: pilotChildEnv(), Timeout: 45 * time.Second, Cleanup: 5 * time.Second,
		MachineLimit: 16 << 20, DiagnosticLimit: 1 << 20,
	})
	if err != nil {
		t.Fatalf("go test operational failure in %s: %v", dir, err)
	}
	out := append([]byte(nil), res.Stdout...)
	if res.Stderr != "" {
		out = append(out, []byte("\n"+res.Stderr)...)
	}
	return pilotProcess{Exit: res.ExitCode, Output: out, Duration: time.Since(started)}
}

func runPilotNegativeControl(t *testing.T, fixture string) pilotNegativeControlMetric {
	t.Helper()
	started := time.Now()
	dir := t.TempDir()
	pilotWrite(t, filepath.Join(dir, "go.mod"), []byte("module pilot.test/settings\n\ngo 1.22\n"))
	for _, name := range []string{"parse.go", "render.go"} {
		pilotCopy(t, filepath.Join(fixture, "implementation", name+".txt"), filepath.Join(dir, name))
	}
	pilotCopy(t, filepath.Join(fixture, "mutants", "store-load-bypasses-parser.go.txt"), filepath.Join(dir, "store.go"))
	pilotCopy(t, filepath.Join(fixture, "composed", "initial", "store_test.go.txt"), filepath.Join(dir, "store_test.go"))
	initial := pilotGoTest(t, dir, "-json", "-count=1", "-run", "^TestStoreContract$")
	pilotCopy(t, filepath.Join(fixture, "composed", "store_test.go.txt"), filepath.Join(dir, "store_test.go"))
	refined := pilotGoTest(t, dir, "-json", "-count=1", "-run", "^TestStoreContract$")
	metric := pilotNegativeControlMetric{
		Holdout: "store-load-bypasses-parser", InitialFixtureMissed: initial.Exit == 0,
		RefinedFixtureCaught: refined.Exit == 1 && pilotHasBehavioralFailure(refined.Output, "TestStoreContract"),
		ReviewerCorrections:  1, GoInvocations: 2,
		ProcessMilliseconds:  initial.Duration.Milliseconds() + refined.Duration.Milliseconds(),
		PipelineMilliseconds: time.Since(started).Milliseconds(),
	}
	if !metric.InitialFixtureMissed || !metric.RefinedFixtureCaught {
		t.Fatalf("loader holdout negative control initial_exit=%d refined_exit=%d\ninitial:\n%s\nrefined:\n%s",
			initial.Exit, refined.Exit, initial.Output, refined.Output)
	}
	return metric
}

func pilotConcreteExecuted(raw []byte) int {
	seen := map[string]bool{}
	s := bufio.NewScanner(bytes.NewReader(raw))
	for s.Scan() {
		var event struct {
			Action, Package, Test string
		}
		if json.Unmarshal(s.Bytes(), &event) == nil && event.Action == "run" && event.Test != "" {
			seen[event.Package+"::"+event.Test] = true
		}
	}
	return len(seen)
}

func pilotRunEvents(raw []byte) int {
	count := 0
	s := bufio.NewScanner(bytes.NewReader(raw))
	for s.Scan() {
		var event struct {
			Action, Test string
		}
		if json.Unmarshal(s.Bytes(), &event) == nil && event.Action == "run" && event.Test != "" {
			count++
		}
	}
	return count
}

func pilotHasBehavioralFailure(raw []byte, top string) bool {
	s := bufio.NewScanner(bytes.NewReader(raw))
	for s.Scan() {
		var event struct {
			Action, Test string
		}
		if json.Unmarshal(s.Bytes(), &event) == nil && event.Action == "fail" && (event.Test == top || strings.HasPrefix(event.Test, top+"/")) {
			return true
		}
	}
	return false
}

type pilotClaim struct {
	Workspace string
	Branch    string
}

func runComposedGraphPilot(t *testing.T, fixture string) pilotGraphMetric {
	t.Helper()
	started := time.Now()
	root := t.TempDir()
	pilotWrite(t, filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":"."}`))
	pilotWrite(t, filepath.Join(root, "go.mod"), []byte("module pilot.test/settings\n\ngo 1.22\n"))
	pilotCopy(t, filepath.Join(fixture, "contract.md"), filepath.Join(root, "contract.md"))
	pilotWrite(t, filepath.Join(root, "Specs", "SettingsPilot", "README.md"), []byte(pilotSpec))
	pilotWrite(t, filepath.Join(root, "Plans", "Pilot", "README.md"), []byte(pilotPlan))
	pilotWrite(t, filepath.Join(root, "pilot-proposal.json"), []byte(pilotProposal))
	pilotGit(t, root, "init", "-q", "-b", "main")
	pilotCLIExpect(t, root, 0, "graph", "init", "--plan", "Pilot")
	pilotCLIExpect(t, root, 0, "graph", "propose", "--plan", "Pilot", "--file", "pilot-proposal.json")
	pilotCLIExpect(t, root, 0, "compile", "--plan", "Pilot")
	pilotGit(t, root, "add", "-A")
	pilotGit(t, root, "commit", "-q", "-m", "pilot graph baseline")
	base := pilotGit(t, root, "rev-parse", "HEAD")

	metric := pilotGraphMetric{ReviewProtocol: "TEST-FIXTURE single-agent four-lane protocol exercise"}
	redNodes := map[string]bool{}
	parseClaim := pilotClaimNode(t, root, "parse", "pilot-parse")
	renderClaim := pilotClaimNode(t, root, "render", "pilot-render")
	pilotMaterializeNode(t, fixture, root, parseClaim, "parse")
	pilotMaterializeNode(t, fixture, root, renderClaim, "render")

	parseRed := pilotRunAttempt(t, root, "parse", "pilot-parse", "red", "baseline")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "parse", parseRed)
	metric.Refusals = append(metric.Refusals,
		pilotExpectAttemptRefusal(t, root, "render", parseRed, "red", "cross-node"),
	)
	pilotAppend(t, filepath.Join(root, filepath.FromSlash(parseClaim.Workspace), "parse.go"), "\n// candidate drift\n")
	metric.Refusals = append(metric.Refusals, pilotExpectAttemptRefusal(t, root, "parse", parseRed, "red", "changed-candidate"))
	pilotCopy(t, filepath.Join(fixture, "scaffold", "parse.go.txt"), filepath.Join(root, filepath.FromSlash(parseClaim.Workspace), "parse.go"))
	specPath := filepath.Join(root, "Specs", "SettingsPilot", "README.md")
	originalSpec := pilotRead(t, specPath)
	pilotWrite(t, specPath, []byte(strings.Replace(originalSpec, "Parse follows", "Parse strictly follows", 1)))
	metric.Refusals = append(metric.Refusals, pilotExpectAttemptRefusal(t, root, "parse", parseRed, "red", "changed-intent"))
	pilotWrite(t, specPath, []byte(originalSpec))
	pilotCLIExpect(t, root, 0, "test", "check", "--plan", "Pilot", "--node", "parse", "--attempt", parseRed, "--expect", "red")
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "parse", "--by", "pilot-parse", "--attempt", parseRed)
	metric.RedAdmissions++
	redNodes["parse"] = true

	// A second real RED is captured, then a supported release/reclaim reuses
	// the deterministic workspace path with a different claim nonce. The old
	// attempt must refuse even though the bytes and path are restored exactly.
	pilotGit(t, filepath.Join(root, filepath.FromSlash(parseClaim.Workspace)), "add", "parse.go", "parse_test.go")
	pilotGit(t, filepath.Join(root, filepath.FromSlash(parseClaim.Workspace)), "commit", "-q", "-m", "parse red fixture")
	takeoverAttempt := pilotRunAttempt(t, root, "parse", "pilot-parse", "red", "baseline")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "parse", takeoverAttempt)
	pilotCLIExpect(t, root, 0, "graph", "release", "parse", "--plan", "Pilot", "--by", "pilot-parse")
	pilotGit(t, root, "worktree", "remove", "--force", filepath.Join(root, filepath.FromSlash(parseClaim.Workspace)))
	parseClaim = pilotClaimNode(t, root, "parse", "pilot-parse-2")
	pilotMaterializeNode(t, fixture, root, parseClaim, "parse")
	metric.Refusals = append(metric.Refusals, pilotExpectAttemptRefusal(t, root, "parse", takeoverAttempt, "red", "same-path-claim-takeover"))
	parseRed2 := pilotRunAttempt(t, root, "parse", "pilot-parse-2", "red", "baseline")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "parse", parseRed2)
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "parse", "--by", "pilot-parse-2", "--attempt", parseRed2)
	metric.RedAdmissions++
	redNodes["parse"] = true
	pilotCopy(t, filepath.Join(fixture, "implementation", "parse.go.txt"), filepath.Join(root, filepath.FromSlash(parseClaim.Workspace), "parse.go"))
	parseGreen := pilotRunAttempt(t, root, "parse", "pilot-parse-2", "green", "")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "parse", parseGreen)
	pilotCommitNode(t, root, parseClaim, "parse implementation")
	pilotCLIExpect(t, root, 0, "test", "check", "--plan", "Pilot", "--node", "parse", "--attempt", parseGreen, "--expect", "green")
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "parse", "--by", "pilot-parse-2", "--attempt", parseGreen)
	metric.NodesGreen++
	pilotIntegrateBranch(t, root, parseClaim.Branch)
	beforeReplay := pilotGraphSnapshot(t, root, "parse")
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "parse", "--attempt", parseRed2)
	afterReplay := pilotGraphSnapshot(t, root, "parse")
	if beforeReplay.Seq != afterReplay.Seq || beforeReplay.Claim != afterReplay.Claim || afterReplay.Result != model.ResultPass {
		t.Fatalf("old RED replay changed graph: before=%+v after=%+v", beforeReplay, afterReplay)
	}

	// Render was claimed before parse integration, so its clean passing branch
	// is rebased over a changed primary. Its tested bytes remain identical.
	renderRed := pilotRunAttempt(t, root, "render", "pilot-render", "red", "baseline")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "render", renderRed)
	pilotCLIExpect(t, root, 0, "test", "check", "--plan", "Pilot", "--node", "render", "--attempt", renderRed, "--expect", "red")
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "render", "--by", "pilot-render", "--attempt", renderRed)
	metric.RedAdmissions++
	redNodes["render"] = true
	pilotCopy(t, filepath.Join(fixture, "implementation", "render.go.txt"), filepath.Join(root, filepath.FromSlash(renderClaim.Workspace), "render.go"))
	renderGreen := pilotRunAttempt(t, root, "render", "pilot-render", "green", "")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "render", renderGreen)
	pilotCommitNode(t, root, renderClaim, "render implementation")
	oldTip := pilotGit(t, root, "rev-parse", renderClaim.Branch)
	pilotGit(t, filepath.Join(root, filepath.FromSlash(renderClaim.Workspace)), "rebase", "main")
	newTip := pilotGit(t, root, "rev-parse", renderClaim.Branch)
	pilotCLIExpect(t, root, 0, "test", "check", "--plan", "Pilot", "--node", "render", "--attempt", renderGreen, "--expect", "green")
	metric.Rechecks++
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "render", "--by", "pilot-render", "--attempt", renderGreen)
	metric.NodesGreen++
	pilotGit(t, root, "merge", "--ff-only", renderClaim.Branch)
	metric.ByteIdenticalRebase = oldTip != newTip && pilotGraphState(t, root, "render") == "GREEN"
	if !metric.ByteIdenticalRebase {
		t.Fatalf("byte-identical render rebase did not preserve fresh evidence (old=%s new=%s state=%s)", oldTip, newTip, pilotGraphState(t, root, "render"))
	}

	storeClaim := pilotClaimNode(t, root, "store", "pilot-store")
	pilotMaterializeNode(t, fixture, root, storeClaim, "store")
	storeRed := pilotRunAttempt(t, root, "store", "pilot-store", "red", "baseline")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "store", storeRed)
	pilotCLIExpect(t, root, 0, "test", "check", "--plan", "Pilot", "--node", "store", "--attempt", storeRed, "--expect", "red")
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "store", "--by", "pilot-store", "--attempt", storeRed)
	metric.RedAdmissions++
	redNodes["store"] = true
	pilotCopy(t, filepath.Join(fixture, "implementation", "store.go.txt"), filepath.Join(root, filepath.FromSlash(storeClaim.Workspace), "store.go"))
	storeGreen := pilotRunAttempt(t, root, "store", "pilot-store", "green", "")
	metric.Attempts++
	metric.TargetRuns++
	metric.ProcessMilliseconds += pilotAttemptProcessMS(t, root, "store", storeGreen)
	pilotCommitNode(t, root, storeClaim, "store implementation")
	// A disjoint command observation mutates only graph bookkeeping. The same
	// fully captured GREEN attempt must remain eligible afterward.
	unrelated := pilotClaimNode(t, root, "unrelated", "pilot-unrelated")
	unrelatedRun := pilotCommand(t, filepath.Join(root, filepath.FromSlash(unrelated.Workspace)), "go", "version")
	metric.ProcessMilliseconds += unrelatedRun.Duration.Milliseconds()
	unrelatedLog := filepath.Join(root, "unrelated-command.log")
	pilotWrite(t, unrelatedLog, unrelatedRun.Output)
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "unrelated", "--by", "pilot-unrelated", "--command-exit", "0", "--command-log", unrelatedLog)
	pilotCLIExpect(t, root, 0, "test", "check", "--plan", "Pilot", "--node", "store", "--attempt", storeGreen, "--expect", "green")
	metric.Rechecks++
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "store", "--by", "pilot-store", "--attempt", storeGreen)
	metric.NodesGreen++
	pilotIntegrateBranch(t, root, storeClaim.Branch)

	legacyReport := filepath.Join(root, "raw-report.json")
	pilotWrite(t, legacyReport, []byte("{}\n"))
	metric.Refusals = append(metric.Refusals, pilotCLIRefusal(t, root, "raw-report-cannot-observe-green", "graph", "sync", "--plan", "Pilot", "--node", "store", "--report", legacyReport))
	pilotRunReviewProtocol(t, root, base, "review")
	metric.ReviewGreen = pilotGraphState(t, root, "review") == "GREEN"
	metric.ProcessMilliseconds += pilotRunAcceptance(t, root, &metric)
	pilotWrite(t, filepath.Join(root, "acceptance.marker"), []byte("controlled fixture acceptance observed\n"))
	pilotGit(t, root, "add", "acceptance.marker")
	pilotGit(t, root, "commit", "-q", "-m", "record fixture acceptance boundary")
	pilotRunReviewProtocol(t, root, base, "final-review")
	metric.WorkClosed, metric.AcceptanceClosed = pilotClosureBeforeStaleness(t, root)
	if !metric.ReviewGreen || !metric.WorkClosed || !metric.AcceptanceClosed {
		t.Fatalf("synthetic protocol closure precondition review_green=%t work_closed=%t acceptance_closed=%t",
			metric.ReviewGreen, metric.WorkClosed, metric.AcceptanceClosed)
	}

	// These invalid proposals use the public proposal/compile flow and remain
	// ignored scratch fragments. Neither can mutate the live graph.
	metric.Refusals = append(metric.Refusals,
		pilotAdverseCompile(t, "empty-tests", pilotEmptyTestsProposal, "declares no tests"),
		pilotAdverseCompile(t, "own-live-graph-input", pilotSelfInputProposal, "live graph"),
	)

	// Changing dependency bytes after the integrated review makes the consumer
	// stale for the existing content-based reason; no broad rerun is invented.
	pilotAppend(t, filepath.Join(root, "parse.go"), "\n// dependency byte change\n")
	pilotGit(t, root, "add", "parse.go")
	pilotGit(t, root, "commit", "-q", "-m", "change dependency bytes")
	metric.DependencyMadeStale = pilotGraphState(t, root, "store") == "STALE"
	if !metric.DependencyMadeStale {
		t.Fatalf("store did not become STALE after parse dependency bytes changed; state=%s", pilotGraphState(t, root, "store"))
	}
	metric.PipelineMilliseconds = time.Since(started).Milliseconds()
	metric.PipelineOverheadMS = metric.PipelineMilliseconds - metric.ProcessMilliseconds
	metric.NodesRed = len(redNodes)
	return metric
}

func pilotMaterializeNode(t *testing.T, fixture, root string, claim pilotClaim, family string) {
	t.Helper()
	ws := filepath.Join(root, filepath.FromSlash(claim.Workspace))
	pilotCopy(t, filepath.Join(fixture, "scaffold", family+".go.txt"), filepath.Join(ws, family+".go"))
	pilotCopy(t, filepath.Join(fixture, "composed", family+"_test.go.txt"), filepath.Join(ws, family+"_test.go"))
}

func pilotClaimNode(t *testing.T, root, node, by string) pilotClaim {
	t.Helper()
	out := pilotCLIExpect(t, root, 0, "next", "--plan", "Pilot", "--claim", "--node", node, "--by", by, "--json")
	var got struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Workspace == "" {
		t.Fatalf("decode claim for %s: %v\n%s", node, err, out)
	}
	branch := pilotGit(t, filepath.Join(root, filepath.FromSlash(got.Workspace)), "rev-parse", "--abbrev-ref", "HEAD")
	return pilotClaim{Workspace: got.Workspace, Branch: branch}
}

func pilotRunAttempt(t *testing.T, root, node, by, phase, redKind string) string {
	t.Helper()
	args := []string{"test", "run", "--plan", "Pilot", "--node", node, "--by", by, "--phase", phase, "--json"}
	want := 0
	if phase == "red" {
		args = append(args, "--red-kind", redKind)
		want = 1
	}
	out := pilotCLIExpect(t, root, want, args...)
	var got struct {
		AttemptID       string `json:"attempt_id"`
		ExecutionStatus string `json:"execution_status"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.AttemptID == "" {
		t.Fatalf("decode %s %s attempt: %v\n%s", node, phase, err, out)
	}
	if phase == "red" && got.ExecutionStatus != "failed" {
		t.Fatalf("%s RED status=%q", node, got.ExecutionStatus)
	}
	if phase == "green" && got.ExecutionStatus != "passed" {
		t.Fatalf("%s GREEN status=%q", node, got.ExecutionStatus)
	}
	return got.AttemptID
}

func pilotAttemptProcessMS(t *testing.T, root, node, attempt string) int64 {
	t.Helper()
	a, err := testevidence.Load(filepath.Join(root, "Plans", "Pilot"), node, attempt)
	if err != nil {
		t.Fatalf("load attempt %s: %v", attempt, err)
	}
	return (a.Execution.RunNanos + a.Execution.CleanupNanos) / int64(time.Millisecond)
}

func pilotCommand(t *testing.T, dir, name string, args ...string) pilotProcess {
	t.Helper()
	started := time.Now()
	res, err := procexec.Capture(context.Background(), name, args, procexec.Policy{
		Dir: dir, Env: pilotChildEnv(), Timeout: 30 * time.Second, Cleanup: 5 * time.Second,
		MachineLimit: 16 << 20, DiagnosticLimit: 1 << 20,
	})
	if err != nil {
		t.Fatalf("%s %s operational failure: %v", name, strings.Join(args, " "), err)
	}
	out := append([]byte(nil), res.Stdout...)
	if res.Stderr != "" {
		out = append(out, []byte("\n"+res.Stderr)...)
	}
	return pilotProcess{Exit: res.ExitCode, Output: out, Duration: time.Since(started)}
}

func pilotExpectAttemptRefusal(t *testing.T, root, node, attempt, expect, label string) string {
	t.Helper()
	wantReason := map[string]string{
		"cross-node": "different node", "changed-candidate": "candidate",
		"changed-intent": "intent", "same-path-claim-takeover": "claim",
	}[label]
	out, err := pilotCLIRun(t, root, "test", "check", "--plan", "Pilot", "--node", node, "--attempt", attempt, "--expect", expect)
	if exitCode(err) != 1 || !strings.Contains(strings.ToLower(out+"\n"+errorText(err)), wantReason) {
		t.Fatalf("%s refusal did not name %q: exit=%d err=%v\n%s", label, wantReason, exitCode(err), err, out)
	}
	return label
}

func pilotCommitNode(t *testing.T, root string, claim pilotClaim, message string) {
	t.Helper()
	ws := filepath.Join(root, filepath.FromSlash(claim.Workspace))
	pilotGit(t, ws, "add", "-A")
	pilotGit(t, ws, "commit", "-q", "-m", message)
}

func pilotIntegrateBranch(t *testing.T, root, branch string) {
	t.Helper()
	parent := t.TempDir()
	ws := filepath.Join(parent, "integration")
	pilotGit(t, root, "worktree", "add", "-q", ws, branch)
	pilotGit(t, ws, "rebase", "main")
	pilotGit(t, root, "worktree", "remove", "--force", ws)
	pilotGit(t, root, "merge", "--ff-only", branch)
}

type pilotNodeSnapshot struct {
	Seq    int
	Claim  string
	Result string
}

func pilotGraphSnapshot(t *testing.T, root, node string) pilotNodeSnapshot {
	t.Helper()
	g, err := gstore.Load(gstore.PathFor(filepath.Join(root, "Plans", "Pilot")))
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID(node)
	if n == nil || n.Verification == nil {
		t.Fatalf("missing verification for %s", node)
	}
	claim := ""
	if n.Claim != nil {
		claim = n.Claim.Instance + ":" + n.Claim.By
	}
	return pilotNodeSnapshot{Seq: g.SeqCounter, Claim: claim, Result: n.Verification.Result}
}

func pilotGraphState(t *testing.T, root, node string) string {
	t.Helper()
	out := pilotCLIExpect(t, root, 0, "graph", "status", "--plan", "Pilot", "--json")
	var got struct {
		Nodes []struct {
			ID, State string
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode graph status: %v\n%s", err, out)
	}
	for _, n := range got.Nodes {
		if n.ID == node {
			return n.State
		}
	}
	return ""
}

func pilotRunAcceptance(t *testing.T, root string, metric *pilotGraphMetric) int64 {
	t.Helper()
	claim := pilotClaimNode(t, root, "accept", "pilot-accept")
	result := pilotGoTest(t, filepath.Join(root, filepath.FromSlash(claim.Workspace)), "-json", "-count=1", "./...")
	metric.BroadRuns++
	if result.Exit != 0 {
		t.Fatalf("integration acceptance broad fixture test failed:\n%s", result.Output)
	}
	log := filepath.Join(root, "acceptance-command.log")
	pilotWrite(t, log, result.Output)
	pilotCLIExpect(t, root, 0, "graph", "sync", "--plan", "Pilot", "--node", "accept", "--by", "pilot-accept", "--command-exit", "0", "--command-log", log)
	return result.Duration.Milliseconds()
}

func pilotClosureBeforeStaleness(t *testing.T, root string) (bool, bool) {
	t.Helper()
	out := pilotCLIExpect(t, root, 0, "graph", "status", "--plan", "Pilot", "--json")
	var got struct {
		Nodes []struct {
			ID     string `json:"id"`
			Closed bool   `json:"closed"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode closure status: %v\n%s", err, out)
	}
	workClosed, acceptanceClosed, workSeen := true, false, 0
	for _, n := range got.Nodes {
		switch n.ID {
		case "parse", "render", "store", "unrelated":
			workSeen++
			workClosed = workClosed && n.Closed
		case "accept":
			acceptanceClosed = n.Closed
		}
	}
	return workClosed && workSeen == 4, acceptanceClosed
}

func pilotAdverseCompile(t *testing.T, label, payload, reason string) string {
	t.Helper()
	root := t.TempDir()
	pilotWrite(t, filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":"."}`))
	pilotWrite(t, filepath.Join(root, "Specs", "SettingsPilot", "README.md"), []byte(pilotSpec))
	pilotWrite(t, filepath.Join(root, "Plans", "Pilot", "README.md"), []byte(pilotPlan))
	pilotWrite(t, filepath.Join(root, "proposal.json"), []byte(payload))
	pilotCLIExpect(t, root, 0, "graph", "init", "--plan", "Pilot")
	pilotCLIExpect(t, root, 0, "graph", "propose", "--plan", "Pilot", "--file", "proposal.json")
	graphPath := filepath.Join(root, "Plans", "Pilot", "Pilot-Graph.json")
	before := pilotRead(t, graphPath)
	out, err := pilotCLIRun(t, root, "compile", "--plan", "Pilot")
	if exitCode(err) != 1 || !strings.Contains(strings.ToLower(out+"\n"+errorText(err)), strings.ToLower(reason)) {
		t.Fatalf("%s compile refusal did not name %q: exit=%d err=%v\n%s", label, reason, exitCode(err), err, out)
	}
	if after := pilotRead(t, graphPath); after != before {
		t.Fatalf("%s refused compile changed graph bytes", label)
	}
	return label
}

func pilotRunReviewProtocol(t *testing.T, root, base, node string) {
	t.Helper()
	endpoint := pilotGit(t, root, "rev-parse", "HEAD")
	planDir := filepath.Join(root, "Plans", "Pilot")
	entries, err := os.ReadDir(planDir)
	if err != nil {
		t.Fatal(err)
	}
	phase := ""
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "README.md" && strings.HasSuffix(entry.Name(), ".md") {
			phase = filepath.Join(planDir, entry.Name())
			break
		}
	}
	if phase == "" {
		t.Fatal("compile did not render a referenced phase")
	}
	before, err := filepath.Glob(filepath.Join(planDir, "reviews", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	pilotCLIExpect(t, root, 0, "review", "scaffold", phase, "--frozen", base+".."+endpoint, "--mode", "single-agent")
	reviews, err := filepath.Glob(filepath.Join(planDir, "reviews", "*.md"))
	if err != nil || len(reviews) != len(before)+1 {
		t.Fatalf("review artifact for %s: %v before=%v after=%v", node, err, before, reviews)
	}
	review := reviews[len(reviews)-1]
	for _, lane := range reviewLaneIDs() {
		evidence := "TEST-FIXTURE protocol lane inspected the controlled settings diff and command evidence; this record exercises persistence and is not an independent quality review"
		pilotCLIExpect(t, root, 0, "review", "evidence", "set", review, "--lane", lane, "--result", "pass", "--evidence", evidence)
	}
	pilotCLIExpect(t, root, 0, "review", "resolve", review)
	rel, err := filepath.Rel(root, review)
	if err != nil {
		t.Fatal(err)
	}
	pilotCLIExpect(t, root, 0, "graph", "review", "--plan", "Pilot", "--node", node, "--artifact", filepath.ToSlash(rel))
}

func pilotCLIRefusal(t *testing.T, root, label string, args ...string) string {
	t.Helper()
	out, err := pilotCLIRun(t, root, args...)
	if exitCode(err) != 1 {
		t.Fatalf("%s: exit=%d want=1 err=%v\n%s", label, exitCode(err), err, out)
	}
	if label == "raw-report-cannot-observe-green" && !strings.Contains(out+"\n"+errorText(err), "requires observed capture") {
		t.Fatalf("raw-report refusal did not name observed capture: %v\n%s", err, out)
	}
	return label
}

func pilotCLIExpect(t *testing.T, dir string, want int, args ...string) string {
	t.Helper()
	out, err := pilotCLIRun(t, dir, args...)
	if got := exitCode(err); got != want {
		t.Fatalf("sdd %s exit=%d want=%d err=%v\n%s", strings.Join(args, " "), got, want, err, out)
	}
	return out
}

func pilotCLIRun(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	stdout, executeErr := pilotCaptureStdout(t, cmd.Execute)
	err = executeErr
	if stdout != "" {
		out.WriteString(stdout)
	}
	return out.String(), err
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func pilotCaptureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	runErr := run()
	_ = w.Close()
	os.Stdout = old
	stdout := <-done
	_ = r.Close()
	return stdout, runErr
}

func pilotGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	env := append(pilotChildEnv(),
		"GIT_AUTHOR_NAME=Pilot Fixture", "GIT_AUTHOR_EMAIL=pilot@example.invalid",
		"GIT_COMMITTER_NAME=Pilot Fixture", "GIT_COMMITTER_EMAIL=pilot@example.invalid")
	res, err := procexec.Capture(context.Background(), "git", args, procexec.Policy{
		Dir: dir, Env: env, Timeout: 30 * time.Second, Cleanup: 5 * time.Second,
		MachineLimit: 16 << 20, DiagnosticLimit: 1 << 20,
	})
	if err != nil {
		t.Fatalf("git %s in %s operational failure: %v", strings.Join(args, " "), dir, err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("git %s in %s: exit %d\n%s\n%s", strings.Join(args, " "), dir, res.ExitCode, res.Stdout, res.Stderr)
	}
	return strings.TrimSpace(string(res.Stdout))
}

func pilotChildEnv() []string {
	overrides := map[string]string{
		"GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local", "GOFLAGS": "",
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		if _, replaced := overrides[strings.ToUpper(key)]; !replaced {
			env = append(env, item)
		}
	}
	for _, key := range []string{"GOENV", "GOFLAGS", "GOTOOLCHAIN", "GOWORK"} {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func pilotCopy(t *testing.T, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	pilotWrite(t, to, raw)
}

func pilotMaterialize(t *testing.T, _ string, dir, name, body string) {
	t.Helper()
	pilotWrite(t, filepath.Join(dir, name), []byte(body))
}

func pilotWrite(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func pilotAppend(t *testing.T, path, suffix string) {
	t.Helper()
	pilotWrite(t, path, []byte(pilotRead(t, path)+suffix))
}

func pilotRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const pilotSpec = `---
title: "Settings Pilot"
type: spec
status: approved
created: 2026-09-14
updated: 2026-09-14
tags: [test-fixture]
related: []
---

# Settings Pilot

## Acceptance Criteria

- [ ] **AC-01**: Parse follows the controlled contract, including invalid and duplicate input.
- [ ] **AC-02**: Render follows the controlled deterministic external-format contract.
- [ ] **AC-03**: Save and Load preserve state and callback-failure invariants.
`

const pilotPlan = `---
title: "Pilot"
type: plan
status: active
created: 2026-09-14
updated: 2026-09-14
tags: [test-fixture]
related: [Specs/SettingsPilot]
phases: []
---

# Pilot
`

const pilotProposal = `{
  "version": 1,
  "nodes": [
    {"id":"parse","contract":"Parse obeys the settings contract","justifies":["AC-01"],"inputs":[{"root":"repository","path":"go.mod"},{"root":"repository","path":"contract.md"}],"gate":{"type":"tests","evidence":"observed-v1","execution":{"adapter":"go-test-v1","timeout_seconds":30},"tests":[{"id":"TestParseContract","file":"parse_test.go"}]},"hazards":[],"artifacts":["parse.go","parse_test.go"],"phase":"01-pilot"},
    {"id":"render","contract":"Render emits the specified external format","justifies":["AC-02"],"inputs":[{"root":"repository","path":"go.mod"},{"root":"repository","path":"contract.md"}],"gate":{"type":"tests","evidence":"observed-v1","execution":{"adapter":"go-test-v1","timeout_seconds":30},"tests":[{"id":"TestRenderContract","file":"render_test.go","satisfies":["external-format"]}]},"hazards":["external-format"],"artifacts":["render.go","render_test.go"],"phase":"01-pilot"},
    {"id":"store","contract":"Save and Load integrate parser and renderer and preserve state","justifies":["AC-03"],"deps":["parse","render"],"inputs":[{"root":"repository","path":"go.mod"},{"root":"repository","path":"contract.md"}],"gate":{"type":"tests","evidence":"observed-v1","execution":{"adapter":"go-test-v1","timeout_seconds":30},"tests":[{"id":"TestStoreContract","file":"store_test.go","satisfies":["persists-state"]}]},"hazards":["persists-state"],"artifacts":["store.go","store_test.go"],"phase":"01-pilot"},
    {"id":"unrelated","contract":"An unrelated command observation does not relabel captured test evidence","justifies":["AC-01"],"gate":{"type":"command","command":"go version"},"hazards":[],"phase":"01-pilot"},
    {"id":"review","role":"review","contract":"Controlled slices pass the persisted synthetic full review protocol","justifies":["AC-01","AC-02","AC-03"],"deps":["store","unrelated"],"gate":{"type":"review","lanes":"full"},"hazards":[],"phase":"01-pilot"},
    {"id":"accept","role":"integration-acceptance","contract":"The integrated controlled fixture passes its actual broad command","justifies":["AC-01","AC-02","AC-03"],"deps":["review"],"gate":{"type":"command","command":"go test -json -count=1 ./..."},"hazards":[],"phase":"01-pilot"},
    {"id":"final-review","role":"review","contract":"The synthetic protocol records a terminal full review after acceptance","justifies":["AC-01","AC-02","AC-03"],"deps":["accept"],"gate":{"type":"review","lanes":"full"},"hazards":[],"phase":"01-pilot"}
  ]
}`

const pilotEmptyTestsProposal = `{
  "version": 1,
  "nodes": [
    {"id":"adverse-empty","contract":"must not admit an empty test gate","justifies":["AC-01"],"gate":{"type":"tests","evidence":"legacy","tests":[]},"hazards":[]}
  ]
}`

const pilotSelfInputProposal = `{
  "version": 1,
  "nodes": [
    {"id":"adverse-self","contract":"must not consume its own live graph","justifies":["AC-01"],"inputs":[{"root":"planning","path":"Plans/Pilot/Pilot-Graph.json"}],"gate":{"type":"tests","evidence":"observed-v1","execution":{"adapter":"go-test-v1","timeout_seconds":30},"tests":[{"id":"TestSelf","file":"self_test.go"}]},"hazards":[],"artifacts":["self_test.go"]}
  ]
}`
