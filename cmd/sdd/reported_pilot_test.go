package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/reportevidence"
)

// TestReportedEvidencePilot drives the reported-v1 contract through real sdd
// processes and a real repository-owned `go test -json` producer. SDD exports
// and validates contexts; it never owns or launches the test process.
func TestReportedEvidencePilot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	bin := stressBinary(t)
	root := reportedPilotRepository(t, bin)
	claimOut := reportedPilotSddOK(t, bin, root, nil,
		"next", "--plan", "Pilot", "--claim", "--node", "behavior", "--by", "pilot", "--json")
	var claim struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(claimOut), &claim); err != nil || claim.Workspace == "" {
		t.Fatalf("decode claim: %v\n%s", err, claimOut)
	}
	workspace := filepath.Join(root, filepath.FromSlash(claim.Workspace))
	nodeBranch := reportedPilotGit(t, workspace, "rev-parse", "--abbrev-ref", "HEAD")
	reportedPilotWrite(t, filepath.Join(workspace, "behavior.go"), []byte(reportedPilotStub))
	reportedPilotWrite(t, filepath.Join(workspace, "behavior_test.go"), []byte(reportedPilotTest))

	poisonEnv, poisonMarker := reportedPilotPoisonedGo(t)
	beforeGraph := reportedPilotGraphBytes(t, root)
	beforeSnapshot := reportedPilotSnapshot(t, root)
	contextOut := reportedPilotSddOK(t, bin, root, poisonEnv,
		"graph", "evidence-context", "--plan", "Pilot", "--node", "behavior", "--by", "pilot", "--json")
	reportedPilotDecodeContext(t, contextOut)
	if after := reportedPilotGraphBytes(t, root); !bytes.Equal(after, beforeGraph) {
		t.Fatal("evidence-context changed graph bytes")
	}
	if after := reportedPilotSnapshot(t, root); after != beforeSnapshot {
		t.Fatalf("evidence-context changed seq/claim/observation: before=%+v after=%+v", beforeSnapshot, after)
	}
	reportedPilotAssertNotSpawned(t, poisonMarker)

	evidenceDir := t.TempDir() // Raw evidence is outside the repository fingerprint.

	// A completed Go invocation that cannot build is diagnostic setup failure,
	// not behavioral RED. It must not arm the hazard.
	reportedPilotWrite(t, filepath.Join(workspace, "broken.go"), []byte("package reported\n\nfunc broken( {\n"))
	buildBefore := reportedPilotContext(t, bin, root, poisonEnv)
	buildRun := reportedPilotGoTest(t, workspace)
	buildAfter := reportedPilotContext(t, bin, root, poisonEnv)
	if buildRun.ExitCode == 0 || reportedPilotHasTestRun(buildRun.Stdout, "TestDouble") {
		t.Fatalf("build-failure control unexpectedly ran the selected test (exit=%d):\n%s", buildRun.ExitCode, buildRun.Stdout)
	}
	buildReport, buildMetadata := reportedPilotEvidenceFiles(t, evidenceDir, "build", buildRun, "red", buildBefore, buildAfter)
	graphBeforeBuildSync := reportedPilotGraphBytes(t, root)
	out, errb, err := reportedPilotRunSdd(bin, root, poisonEnv,
		"graph", "sync", "--plan", "Pilot", "--node", "behavior", "--by", "pilot", "--report", buildReport, "--metadata", buildMetadata)
	if reportedPilotExitCode(err) != 1 || !strings.Contains(strings.ToLower(out+errb), "run") {
		t.Fatalf("build/setup failure was not refused as a missing real test run: exit=%d err=%v\nstdout:%s\nstderr:%s",
			reportedPilotExitCode(err), err, out, errb)
	}
	if after := reportedPilotGraphBytes(t, root); !bytes.Equal(after, graphBeforeBuildSync) {
		t.Fatal("refused build/setup report changed graph")
	}
	if got := reportedPilotSnapshot(t, root); got.RedCount != 0 || got.ObservationSeq != 0 {
		t.Fatalf("build/setup failure armed RED or recorded an observation: %+v", got)
	}
	reportedPilotAssertNotSpawned(t, poisonMarker)
	if err := os.Remove(filepath.Join(workspace, "broken.go")); err != nil {
		t.Fatal(err)
	}

	redBefore := reportedPilotContext(t, bin, root, poisonEnv)
	redRun := reportedPilotGoTest(t, workspace)
	redAfter := reportedPilotContext(t, bin, root, poisonEnv)
	if redRun.ExitCode == 0 || !reportedPilotHasTestRun(redRun.Stdout, "TestDouble") ||
		!reportedPilotHasTerminal(redRun.Stdout, "TestDouble", "fail") ||
		!bytes.Contains(redRun.Stdout, []byte("Double(2) = 0, want 4")) {
		t.Fatalf("repository runner did not produce actual behavioral RED (exit=%d):\n%s", redRun.ExitCode, redRun.Stdout)
	}
	redReport, redMetadata := reportedPilotEvidenceFiles(t, evidenceDir, "red", redRun, "red", redBefore, redAfter)
	redSync := reportedPilotSddOK(t, bin, root, poisonEnv,
		"graph", "sync", "--plan", "Pilot", "--node", "behavior", "--by", "pilot", "--report", redReport, "--metadata", redMetadata)
	reportedPilotAssertNotSpawned(t, poisonMarker)
	redSnapshot := reportedPilotSnapshot(t, root)
	if redSnapshot.Result != model.ResultFail || redSnapshot.RedCount != 1 || redSnapshot.ObservationSeq == 0 {
		t.Fatalf("RED did not arm and record failure: %+v\n%s", redSnapshot, redSync)
	}

	// Change only the callable subject. The selected test remains byte-identical.
	reportedPilotWrite(t, filepath.Join(workspace, "behavior.go"), []byte(reportedPilotImplementation))
	greenBefore := reportedPilotContext(t, bin, root, poisonEnv)
	greenRun := reportedPilotGoTest(t, workspace)
	greenAfter := reportedPilotContext(t, bin, root, poisonEnv)
	if greenRun.ExitCode != 0 || !reportedPilotHasTerminal(greenRun.Stdout, "TestDouble", "pass") {
		t.Fatalf("repository runner did not produce actual GREEN (exit=%d):\n%s", greenRun.ExitCode, greenRun.Stdout)
	}
	greenReport, greenMetadata := reportedPilotEvidenceFiles(t, evidenceDir, "green", greenRun, "green", greenBefore, greenAfter)

	// Commit after capture but before admission. The candidate bytes are exactly
	// those represented by the returned contexts and exercised by go test.
	reportedPilotGit(t, workspace, "add", "behavior.go", "behavior_test.go")
	reportedPilotGit(t, workspace, "commit", "-q", "-m", "implement behavior")
	testedRevision := reportedPilotGit(t, workspace, "rev-parse", "HEAD")
	greenSync := reportedPilotSddOK(t, bin, root, poisonEnv,
		"graph", "sync", "--plan", "Pilot", "--node", "behavior", "--by", "pilot", "--report", greenReport, "--metadata", greenMetadata)
	reportedPilotAssertNotSpawned(t, poisonMarker)
	greenSnapshot := reportedPilotSnapshot(t, root)
	if greenSnapshot.Result != model.ResultPass || greenSnapshot.ObservationSeq <= redSnapshot.ObservationSeq {
		t.Fatalf("GREEN was not recorded after RED: red=%+v green=%+v\n%s", redSnapshot, greenSnapshot, greenSync)
	}
	if state := reportedPilotState(t, bin, root); state != "STALE" {
		t.Fatalf("target root state before Git integration=%q, want STALE", state)
	}
	reportedPilotIntegrate(t, root, nodeBranch, testedRevision)
	if state := reportedPilotState(t, bin, root); state != "GREEN" {
		t.Fatalf("derived state after byte-identical fast-forward=%q, want GREEN", state)
	}

	// Exact historical pairs are acknowledgements only, even when replayed in
	// the old RED then GREEN order.
	beforeReplay := reportedPilotGraphBytes(t, root)
	beforeReplaySnapshot := reportedPilotSnapshot(t, root)
	redReplay := reportedPilotSddOK(t, bin, root, poisonEnv,
		"graph", "sync", "--plan", "Pilot", "--node", "behavior", "--by", "pilot", "--report", redReport, "--metadata", redMetadata)
	greenReplay := reportedPilotSddOK(t, bin, root, poisonEnv,
		"graph", "sync", "--plan", "Pilot", "--node", "behavior", "--by", "pilot", "--report", greenReport, "--metadata", greenMetadata)
	if after := reportedPilotGraphBytes(t, root); !bytes.Equal(after, beforeReplay) {
		t.Fatal("historical RED/GREEN replay changed graph bytes")
	}
	if after := reportedPilotSnapshot(t, root); after != beforeReplaySnapshot {
		t.Fatalf("historical replay changed seq/claim/observation: before=%+v after=%+v", beforeReplaySnapshot, after)
	}
	if state := reportedPilotState(t, bin, root); state != "GREEN" {
		t.Fatalf("historical replay changed derived state to %q", state)
	}
	reportedPilotAssertNotSpawned(t, poisonMarker)

	t.Logf("NO_RUNNER context stdout=%q; RED sync stdout=%q", strings.TrimSpace(contextOut), strings.TrimSpace(redSync))
	t.Logf("ACTUAL_RED exit=%d seq=%d; ACTUAL_GREEN exit=%d seq=%d state=GREEN", redRun.ExitCode, redSnapshot.ObservationSeq, greenRun.ExitCode, greenSnapshot.ObservationSeq)
	t.Logf("REPLAY red=%q green=%q seq=%d result=%s", strings.TrimSpace(redReplay), strings.TrimSpace(greenReplay), beforeReplaySnapshot.Seq, beforeReplaySnapshot.Result)
}

type reportedPilotRun struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Started  time.Time
	Ended    time.Time
}

func reportedPilotGoTest(t *testing.T, dir string) reportedPilotRun {
	t.Helper()
	started := time.Now().UTC()
	result, err := procexec.Capture(context.Background(), "go",
		[]string{"test", "-json", "-count=1", "-run", "^TestDouble$", "./..."},
		procexec.Policy{
			Dir:             dir,
			Env:             reportedPilotGoEnv(),
			Timeout:         45 * time.Second,
			Cleanup:         5 * time.Second,
			MachineLimit:    16 << 20,
			DiagnosticLimit: 1 << 20,
		})
	ended := time.Now().UTC()
	if err != nil {
		t.Fatalf("repository test tool could not complete: %v", err)
	}
	return reportedPilotRun{Stdout: result.Stdout, Stderr: []byte(result.Stderr), ExitCode: result.ExitCode, Started: started, Ended: ended}
}

func reportedPilotEvidenceFiles(t *testing.T, dir, name string, run reportedPilotRun, phase string, before, after reportevidence.Context) (string, string) {
	t.Helper()
	exit := run.ExitCode
	metadata := reportevidence.Metadata{
		Protocol:  reportevidence.Protocol,
		Before:    before,
		After:     after,
		Phase:     phase,
		Started:   run.Started.Format(time.RFC3339),
		Completed: run.Ended.Format(time.RFC3339),
		Runner: reportevidence.Runner{
			Identity:              "repository-unit-tests",
			EnvironmentIdentities: map[string]string{"BUILD_CONTEXT": "pilot-cgo-gcc"},
		},
		Execution:    reportevidence.Execution{Started: true, Completed: true, ReportComplete: true, ExitCode: &exit},
		ReportDigest: reportevidence.Digest(run.Stdout),
	}
	if phase == "red" {
		metadata.RedKind = "baseline"
	}
	reportPath := filepath.Join(dir, name+".json")
	metadataPath := filepath.Join(dir, name+"-metadata.json")
	reportedPilotWrite(t, reportPath, run.Stdout)
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	reportedPilotWrite(t, metadataPath, raw)
	return reportPath, metadataPath
}

func reportedPilotContext(t *testing.T, bin, root string, env []string) reportevidence.Context {
	t.Helper()
	out := reportedPilotSddOK(t, bin, root, env,
		"graph", "evidence-context", "--plan", "Pilot", "--node", "behavior", "--by", "pilot", "--json")
	return reportedPilotDecodeContext(t, out)
}

func reportedPilotDecodeContext(t *testing.T, raw string) reportevidence.Context {
	t.Helper()
	var context reportevidence.Context
	if err := json.Unmarshal([]byte(raw), &context); err != nil {
		t.Fatalf("decode evidence context: %v\n%s", err, raw)
	}
	if context.Protocol != reportevidence.Protocol || context.ClaimInstance == "" || len(context.Selected) != 1 {
		t.Fatalf("incomplete returned evidence context: %+v", context)
	}
	return context
}

type reportedPilotGraphSnapshot struct {
	Seq            int
	Claim          string
	Result         string
	ObservationSeq int
	RedCount       int
}

func reportedPilotSnapshot(t *testing.T, root string) reportedPilotGraphSnapshot {
	t.Helper()
	g, err := gstore.Load(gstore.PathFor(filepath.Join(root, "Plans", "Pilot")))
	if err != nil {
		t.Fatal(err)
	}
	n := g.NodeByID("behavior")
	if n == nil {
		t.Fatal("behavior node missing")
	}
	s := reportedPilotGraphSnapshot{Seq: g.SeqCounter, RedCount: len(n.RedEvidence)}
	if n.Claim != nil {
		s.Claim = n.Claim.Instance + ":" + n.Claim.By
	}
	if n.Verification != nil {
		s.Result = n.Verification.Result
		s.ObservationSeq = n.Verification.Seq
	}
	return s
}

func reportedPilotState(t *testing.T, bin, root string) string {
	t.Helper()
	out := reportedPilotSddOK(t, bin, root, nil, "graph", "status", "--plan", "Pilot", "--json")
	var status struct {
		Nodes []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, out)
	}
	for _, n := range status.Nodes {
		if n.ID == "behavior" {
			return n.State
		}
	}
	return ""
}

func reportedPilotRepository(t *testing.T, bin string) string {
	t.Helper()
	root := t.TempDir()
	reportedPilotGit(t, root, "init", "-q", "-b", "main")
	reportedPilotWrite(t, filepath.Join(root, "planning-config.json"), []byte(`{"planningRoot":"."}`))
	reportedPilotWrite(t, filepath.Join(root, "go.mod"), []byte("module pilot.test/reported\n\ngo 1.22\n"))
	reportedPilotWrite(t, filepath.Join(root, "Specs", "Reported", "README.md"), []byte(reportedPilotSpec))
	reportedPilotWrite(t, filepath.Join(root, "Plans", "Pilot", "README.md"), []byte(reportedPilotPlan))
	reportedPilotWrite(t, filepath.Join(root, "proposal.json"), []byte(reportedPilotProposal))
	for _, args := range [][]string{
		{"graph", "init", "--plan", "Pilot"},
		{"graph", "propose", "--plan", "Pilot", "--file", "proposal.json"},
		{"compile", "--plan", "Pilot"},
	} {
		reportedPilotSddOK(t, bin, root, nil, args...)
	}
	reportedPilotGit(t, root, "add", "-A")
	reportedPilotGit(t, root, "commit", "-q", "-m", "reported pilot graph")
	return root
}

func reportedPilotIntegrate(t *testing.T, root, nodeBranch, testedRevision string) {
	t.Helper()
	if nodeBranch == "" || nodeBranch == "HEAD" {
		t.Fatalf("invalid captured node branch %q", nodeBranch)
	}
	if got := reportedPilotGit(t, root, "rev-parse", nodeBranch); got != testedRevision {
		t.Fatalf("captured node branch moved before integration: got=%s want=%s", got, testedRevision)
	}
	testedTree := reportedPilotGit(t, root, "rev-parse", testedRevision+"^{tree}")
	ancestor := exec.Command("git", "merge-base", "--is-ancestor", "main", nodeBranch)
	ancestor.Dir = root
	if err := ancestor.Run(); err != nil {
		if reportedPilotExitCode(err) != 1 {
			t.Fatalf("check whether %s needs rebase: %v", nodeBranch, err)
		}
		scratch := filepath.Join(t.TempDir(), "integration")
		reportedPilotGit(t, root, "worktree", "add", "-q", scratch, nodeBranch)
		reportedPilotGit(t, scratch, "rebase", "main")
		reportedPilotGit(t, root, "worktree", "remove", "--force", scratch)
	}
	integratedRevision := reportedPilotGit(t, root, "rev-parse", nodeBranch)
	if got := reportedPilotGit(t, root, "rev-parse", integratedRevision+"^{tree}"); got != testedTree {
		t.Fatalf("rebase changed tested tree: got=%s want=%s", got, testedTree)
	}
	reportedPilotGit(t, root, "merge", "--ff-only", nodeBranch)
	if got := reportedPilotGit(t, root, "rev-parse", "HEAD"); got != integratedRevision {
		t.Fatalf("fast-forward target revision=%s, want node revision=%s", got, integratedRevision)
	}
}

func reportedPilotRunSdd(bin, dir string, env []string, args ...string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func reportedPilotSddOK(t *testing.T, bin, dir string, env []string, args ...string) string {
	t.Helper()
	out, errb, err := reportedPilotRunSdd(bin, dir, env, args...)
	if err != nil {
		t.Fatalf("sdd %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, out, errb)
	}
	return out
}

func reportedPilotGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Reported Pilot", "GIT_AUTHOR_EMAIL=pilot@example.invalid",
		"GIT_COMMITTER_NAME=Reported Pilot", "GIT_COMMITTER_EMAIL=pilot@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func reportedPilotPoisonedGo(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "go-was-spawned")
	var name, body string
	if runtime.GOOS == "windows" {
		name = "go.cmd"
		body = "@echo poisoned>\"" + marker + "\"\r\n@exit /b 97\r\n"
	} else {
		name = "go"
		body = "#!/bin/sh\nprintf poisoned > '" + marker + "'\nexit 97\n"
	}
	path := filepath.Join(dir, name)
	reportedPilotWrite(t, path, []byte(body))
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return reportedPilotEnvOverride("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")), marker
}

func reportedPilotAssertNotSpawned(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("production context/admission spawned the poisoned Go runner")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func reportedPilotGoEnv() []string {
	env := reportedPilotEnvOverride("GOENV", "off")
	env = reportedPilotEnvOverrideIn(env, "GOWORK", "off")
	env = reportedPilotEnvOverrideIn(env, "GOTOOLCHAIN", "local")
	env = reportedPilotEnvOverrideIn(env, "GOFLAGS", "")
	env = reportedPilotEnvOverrideIn(env, "BUILD_CONTEXT", "pilot-cgo-gcc")
	return env
}

func reportedPilotEnvOverride(key, value string) []string {
	return reportedPilotEnvOverrideIn(os.Environ(), key, value)
}

func reportedPilotEnvOverrideIn(source []string, key, value string) []string {
	out := make([]string, 0, len(source)+1)
	for _, item := range source {
		name := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			name = item[:i]
		}
		if !strings.EqualFold(name, key) {
			out = append(out, item)
		}
	}
	return append(out, key+"="+value)
}

func reportedPilotGraphBytes(t *testing.T, root string) []byte {
	t.Helper()
	raw, err := os.ReadFile(gstore.PathFor(filepath.Join(root, "Plans", "Pilot")))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func reportedPilotWrite(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func reportedPilotExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return -1
}

func reportedPilotHasTestRun(raw []byte, test string) bool {
	return reportedPilotHasTerminal(raw, test, "run")
}

func reportedPilotHasTerminal(raw []byte, test, action string) bool {
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		var event struct {
			Action string
			Test   string
		}
		if json.Unmarshal(line, &event) == nil && event.Action == action && event.Test == test {
			return true
		}
	}
	return false
}

const reportedPilotStub = `package reported

func Double(int) int { return 0 }
`

const reportedPilotImplementation = `package reported

func Double(value int) int { return value * 2 }
`

const reportedPilotTest = `package reported

import "testing"

func TestDouble(t *testing.T) {
	if got := Double(2); got != 4 {
		t.Fatalf("Double(2) = %d, want 4", got)
	}
}
`

const reportedPilotSpec = `---
title: "Reported Evidence Pilot"
type: spec
status: approved
created: 2026-09-15
updated: 2026-09-15
tags: [test-fixture]
related: []
---

# Reported Evidence Pilot

## Acceptance Criteria

- [ ] **AC-01**: Double returns twice its input.
`

const reportedPilotPlan = `---
title: "Pilot"
type: plan
status: active
created: 2026-09-15
updated: 2026-09-15
tags: [test-fixture]
related: [Specs/Reported]
phases: []
---

# Pilot
`

const reportedPilotProposal = `{
  "version": 1,
  "nodes": [
    {
      "id": "behavior",
      "contract": "Double returns twice its input",
      "justifies": ["AC-01"],
      "inputs": [{"root":"repository","path":"go.mod"}],
      "gate": {
        "type": "tests",
        "evidence": "reported-v1",
        "report": {
          "format": "go-test-json-v1",
          "runner": "repository-unit-tests",
          "environment_keys": ["BUILD_CONTEXT"],
          "test_support_inputs": [],
          "test_support_artifacts": []
        },
		"tests": [{"id":"TestDouble","package":"pilot.test/reported","file":"behavior_test.go","satisfies":["computes-number"]}]
      },
	  "hazards": ["computes-number"],
      "artifacts": ["behavior.go", "behavior_test.go"],
      "phase": "01-pilot"
    },
    {
      "id": "final-review",
      "role": "review",
      "contract": "The reported evidence pilot passes final review",
      "justifies": ["AC-01"],
      "deps": ["behavior"],
      "gate": {"type":"review","lanes":"full"},
      "hazards": [],
      "phase": "01-pilot"
    }
  ]
}`
