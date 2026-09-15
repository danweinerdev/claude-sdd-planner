package testevidence

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	graphdigest "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	graphinputs "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/inputs"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
	"golang.org/x/mod/modfile"
)

const Protocol = "observed-v1"
const maxHeaderBytes int64 = 4 << 20

// Profile resolution performs two independently bounded ten-second Go probes.
// AdmissionHeadroom reserves a finite publication/check margin; it is not a
// promise about human delay and does not renew the claim.
const profileProbeBudget = 20 * time.Second
const admissionHeadroom = 5 * time.Second

var attemptIDPattern = regexp.MustCompile(`^at-[0-9a-f]{32}$`)

type Candidate struct {
	Obligation          string                       `json:"obligation"`
	Artifacts           map[string]string            `json:"artifacts"`
	Dependencies        map[string]map[string]string `json:"dependencies"`
	Inputs              map[string]string            `json:"inputs"`
	Intent              map[string]string            `json:"intent"`
	SelectedTestSources map[string]string            `json:"selected_test_sources"`
	ModuleFiles         map[string]string            `json:"module_files"`
}

type Profile struct {
	Adapter          string            `json:"adapter"`
	Args             []string          `json:"args"`
	Argv             []string          `json:"argv"`
	TimeoutSeconds   int               `json:"timeout_seconds"`
	WorkingDirectory string            `json:"working_directory"`
	Executable       string            `json:"executable_identity"`
	Version          string            `json:"version"`
	Environment      map[string]string `json:"environment_identities"`
	ProbeNanos       int64             `json:"probe_nanos"`
}

type Execution struct {
	Started            bool   `json:"started"`
	Completed          bool   `json:"completed"`
	ExitCode           int    `json:"exit_code"`
	RunNanos           int64  `json:"run_nanos"`
	CleanupNanos       int64  `json:"cleanup_nanos"`
	DescendantsCleaned bool   `json:"descendants_cleaned"`
	StdoutDigest       string `json:"stdout_digest"`
	StderrDigest       string `json:"stderr_digest"`
}

type Attempt struct {
	Protocol          string         `json:"protocol"`
	ID                string         `json:"id"`
	Plan              string         `json:"plan"`
	Node              string         `json:"node"`
	By                string         `json:"by"`
	ClaimInstance     string         `json:"claim_instance"`
	Workspace         string         `json:"workspace_identity"`
	Phase             string         `json:"phase"`
	RedKind           string         `json:"red_kind,omitempty"`
	Fault             string         `json:"fault,omitempty"`
	Started           string         `json:"started"`
	Completed         string         `json:"completed"`
	Before            Candidate      `json:"before"`
	After             Candidate      `json:"after"`
	Profile           Profile        `json:"profile"`
	Execution         Execution      `json:"execution"`
	Selected          []SelectedTest `json:"selected"`
	Report            Report         `json:"report"`
	Digest            string         `json:"digest"`
	ExecutionRevision string         `json:"execution_revision,omitempty"`
}

type preparedGo struct {
	Selected   []SelectedTest
	Profile    Profile
	Args       []string
	Executable string
	Env        []string
}

type RunOptions struct {
	PlanDir, PlanningRoot, RepoRoot string
	Node, By, Phase, RedKind, Fault string
	Now                             func() time.Time
}

type RunResult struct {
	AttemptID       string       `json:"attempt_id,omitempty"`
	ExecutionStatus string       `json:"execution_status"`
	Result          string       `json:"result,omitempty"`
	Tests           []TestResult `json:"tests,omitempty"`
	Refusal         string       `json:"refusal,omitempty"`
	Complete        bool         `json:"complete"`
	Diagnostic      string       `json:"diagnostic,omitempty"`
	StdoutExcerpt   string       `json:"stdout_excerpt,omitempty"`
	StderrExcerpt   string       `json:"stderr_excerpt,omitempty"`
}

type CheckOptions struct {
	PlanDir, PlanningRoot, RepoRoot string
	Node, By, AttemptID, Expect     string
	Now                             func() time.Time
}

type CheckResult struct {
	Eligible      bool              `json:"eligible"`
	Attempt       *Attempt          `json:"attempt,omitempty"`
	Current       Candidate         `json:"current,omitempty"`
	Compatibility map[string]string `json:"compatibility,omitempty"`
	Failed        []string          `json:"failed,omitempty"`
	Refusal       string            `json:"refusal,omitempty"`
}

type CleanupOptions struct {
	PlanDir, Node, AttemptID string
	Abandon                  bool
	Now                      func() time.Time
	MinInactive              time.Duration
}
type CleanupResult struct {
	AttemptID string `json:"attempt_id"`
	Removed   bool   `json:"removed"`
	Finalized bool   `json:"finalized"`
	Consumed  bool   `json:"consumed"`
}

func Cleanup(o CleanupOptions) (*CleanupResult, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MinInactive <= 0 {
		o.MinInactive = time.Minute
	}
	if err := safeName(o.Node); err != nil {
		return nil, err
	}
	if !attemptIDPattern.MatchString(o.AttemptID) {
		return nil, fmt.Errorf("test cleanup: invalid attempt id")
	}
	g, err := gstore.Load(gstore.PathFor(o.PlanDir))
	if err != nil {
		return nil, err
	}
	n := g.NodeByID(o.Node)
	if n == nil {
		return nil, fmt.Errorf("test cleanup: node %q does not exist", o.Node)
	}
	dir, err := ensureOwnedDir(o.PlanDir, []string{gstore.GraphDirName, "test-evidence", o.Node, o.AttemptID}, false)
	if err != nil {
		return nil, fmt.Errorf("test cleanup: %w", err)
	}
	release, acquired, err := istore.TryAcquireExclusiveLock(attemptLockPath(o.PlanDir, o.Node, o.AttemptID))
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("test cleanup: attempt capture is active")
	}
	defer release()
	res := &CleanupResult{AttemptID: o.AttemptID}
	_, headerErr := os.Lstat(filepath.Join(dir, "header.json"))
	headerExists := headerErr == nil
	if headerErr != nil && !os.IsNotExist(headerErr) {
		return nil, headerErr
	}
	a, loadErr := Load(o.PlanDir, o.Node, o.AttemptID)
	res.Finalized = headerExists
	_, res.Consumed = n.ConsumedAttempts[o.AttemptID]
	if headerExists && loadErr != nil && !res.Consumed && !o.Abandon {
		return nil, fmt.Errorf("test cleanup: finalized receipt is invalid and retained; pass --abandon to explicitly discard it: %w", loadErr)
	}
	if n.Claim != nil && (a == nil || n.Claim.Instance == a.ClaimInstance) {
		return nil, fmt.Errorf("test cleanup: attempt has a current matching claim")
	}
	if res.Finalized && !res.Consumed && !o.Abandon {
		return nil, fmt.Errorf("test cleanup: finalized unadmitted attempt is retained; pass --abandon to explicitly discard it")
	}
	if !res.Finalized {
		info, e := os.Stat(dir)
		if e != nil {
			return nil, e
		}
		if o.Now().Sub(info.ModTime()) < o.MinInactive {
			return nil, fmt.Errorf("test cleanup: incomplete attempt has not been inactive long enough")
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("test cleanup: remove owned attempt: %w", err)
	}
	res.Removed = true
	return res, nil
}

func Run(ctx context.Context, o RunOptions) (*RunResult, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Phase != "red" && o.Phase != "green" && o.Phase != "diagnostic" {
		return nil, fmt.Errorf("test run: phase must be red, green, or diagnostic")
	}
	if o.Phase == "red" {
		if o.RedKind != "baseline" && o.RedKind != "sensitivity" {
			return nil, fmt.Errorf("test run: red phase requires --red-kind baseline or sensitivity")
		}
		if o.RedKind == "sensitivity" && strings.TrimSpace(o.Fault) == "" {
			return nil, fmt.Errorf("test run: sensitivity red requires a concise --fault")
		}
		if o.RedKind == "baseline" && o.Fault != "" {
			return nil, fmt.Errorf("test run: --fault applies only to sensitivity red")
		}
	} else if o.RedKind != "" || o.Fault != "" {
		return nil, fmt.Errorf("test run: --red-kind and --fault apply only to red phase")
	}
	if strings.ContainsAny(o.Fault, "\r\n") || len(o.Fault) > 200 {
		return nil, fmt.Errorf("test run: --fault must be one concise line of at most 200 bytes")
	}
	g, node, workspace, err := loadCurrent(o.PlanDir, o.RepoRoot, o.Node, o.By, o.Now())
	if err != nil {
		return nil, err
	}
	if node.Gate.Evidence != model.EvidenceObservedV1 {
		return nil, fmt.Errorf("test run: node %q does not require observed-v1 evidence", o.Node)
	}
	if problems := model.ValidateEvidenceGate(node); len(problems) > 0 {
		return nil, fmt.Errorf("test run: invalid observed gate: %s", strings.Join(problems, "; "))
	}
	expires, _ := time.Parse(time.RFC3339, node.Claim.LeaseExpires)
	needed := profileProbeBudget + time.Duration(node.Gate.Execution.TimeoutSeconds)*time.Second + procexec.DefaultCleanup + admissionHeadroom
	if expires.Sub(o.Now()) < needed {
		return nil, fmt.Errorf("test run: claim lease has insufficient time remaining for profile probes, timeout, cleanup, and admission headroom")
	}
	prepared, err := prepareGo(ctx, workspace, o.PlanDir, node)
	if err != nil {
		return nil, fmt.Errorf("test run: %w", err)
	}
	before, err := snapshot(o.PlanningRoot, o.RepoRoot, o.PlanDir, workspace, g, node, prepared.Selected)
	if err != nil {
		return nil, fmt.Errorf("test run: candidate before execution: %w", err)
	}
	expires, _ = time.Parse(time.RFC3339, node.Claim.LeaseExpires)
	if expires.Sub(o.Now()) < time.Duration(node.Gate.Execution.TimeoutSeconds)*time.Second+procexec.DefaultCleanup+admissionHeadroom {
		return nil, fmt.Errorf("test run: claim lease no longer has execution, cleanup, and admission headroom after profile probes")
	}
	id, dir, err := createAttemptDir(o.PlanDir, o.Node)
	if err != nil {
		return nil, fmt.Errorf("test run: %w", err)
	}
	releaseLock, err := istore.AcquireExclusiveLock(attemptLockPath(o.PlanDir, o.Node, id))
	if err != nil {
		return recordAttemptFailure(id, dir, "storage-incomplete", fmt.Errorf("acquire attempt capture lock: %w", err))
	}
	defer releaseLock()
	started := o.Now().UTC()
	revision, err := executionRevision(workspace)
	if err != nil {
		return recordAttemptFailure(id, dir, "provenance-incomplete", fmt.Errorf("test run: capture execution revision: %w", err))
	}
	res, captureErr := procexec.Capture(ctx, prepared.Executable, prepared.Args, procexec.Policy{Dir: workspace, Env: prepared.Env, Timeout: time.Duration(node.Gate.Execution.TimeoutSeconds) * time.Second, Cleanup: procexec.DefaultCleanup})
	if captureErr != nil {
		return recordAttemptFailure(id, dir, "execution-incomplete", captureErr)
	}
	if err := writeExclusive(filepath.Join(dir, "stdout.json"), res.Stdout, 0o600); err != nil {
		return recordAttemptFailure(id, dir, "storage-incomplete", err)
	}
	if err := writeExclusive(filepath.Join(dir, "stderr.txt"), []byte(res.Stderr), 0o600); err != nil {
		return recordAttemptFailure(id, dir, "storage-incomplete", err)
	}
	report, parseErr := ParseGoReport(res.Stdout, prepared.Selected, res.ExitCode)
	if parseErr != nil {
		return recordAttemptFailure(id, dir, "report-incomplete", parseErr)
	}
	freshGraph, freshNode, freshWorkspace, err := loadCurrent(o.PlanDir, o.RepoRoot, o.Node, o.By, o.Now())
	if err != nil {
		return recordAttemptFailure(id, dir, "claim-incomplete", fmt.Errorf("test run: claim changed during execution: %w", err))
	}
	if freshNode.Claim.Instance != node.Claim.Instance || freshWorkspace != workspace {
		return recordAttemptFailure(id, dir, "claim-incomplete", fmt.Errorf("test run: claim instance or workspace changed during execution"))
	}
	afterPrepared, err := prepareGo(ctx, freshWorkspace, o.PlanDir, freshNode)
	if err != nil {
		return recordAttemptFailure(id, dir, "profile-incomplete", fmt.Errorf("test run: execution profile after execution: %w", err))
	}
	if !equalJSON(prepared.Selected, afterPrepared.Selected) || !profilesEqual(prepared.Profile, afterPrepared.Profile) {
		return recordAttemptFailure(id, dir, "profile-incomplete", fmt.Errorf("selected tests or execution profile changed during execution"))
	}
	after, err := snapshot(o.PlanningRoot, o.RepoRoot, o.PlanDir, freshWorkspace, freshGraph, freshNode, afterPrepared.Selected)
	if err != nil {
		return recordAttemptFailure(id, dir, "candidate-incomplete", fmt.Errorf("test run: candidate after execution: %w", err))
	}
	completed := o.Now().UTC()
	a := Attempt{Protocol: Protocol, ID: id, Plan: filepath.Base(o.PlanDir), Node: o.Node, By: o.By, ClaimInstance: node.Claim.Instance, ExecutionRevision: revision,
		Workspace: opaqueWorkspace(node.Claim.Workspace), Phase: o.Phase, RedKind: o.RedKind, Fault: strings.TrimSpace(o.Fault), Started: started.Format(time.RFC3339Nano), Completed: completed.Format(time.RFC3339Nano),
		Before: before, After: after, Profile: prepared.Profile, Selected: prepared.Selected, Report: report,
		Execution: Execution{Started: true, Completed: true, ExitCode: res.ExitCode, RunNanos: res.Run.Nanoseconds(), CleanupNanos: res.Cleanup.Nanoseconds(), DescendantsCleaned: res.DescendantsCleaned, StdoutDigest: digestBytes(res.Stdout), StderrDigest: digestBytes([]byte(res.Stderr))}}
	a.Digest = attemptDigest(a)
	if err := publishFinal(filepath.Join(dir, "header.json"), mustJSON(a)); err != nil {
		return recordAttemptFailure(id, dir, "storage-incomplete", fmt.Errorf("test run: publish attempt: %w", err))
	}
	status := "passed"
	if report.Result == model.ResultFail {
		status = "failed"
	}
	return &RunResult{AttemptID: id, ExecutionStatus: status, Result: report.Result, Tests: report.Tests, Complete: true}, nil
}

func recordAttemptFailure(id, dir, status string, cause error) (*RunResult, error) {
	r := &RunResult{AttemptID: id, ExecutionStatus: status, Refusal: cause.Error(), Diagnostic: cause.Error()}
	var pe *procexec.Error
	if errors.As(cause, &pe) {
		r.StdoutExcerpt = pe.Stdout
		r.StderrExcerpt = pe.Stderr
	}
	payload := mustJSON(r)
	if err := writeExclusive(filepath.Join(dir, "incomplete.json"), payload, 0o600); err != nil {
		cause = fmt.Errorf("%w; writing incomplete attempt diagnostics: %v", cause, err)
		r.Diagnostic = cause.Error()
		r.Refusal = cause.Error()
	}
	return r, cause
}
func attemptLockPath(planDir, node, id string) string {
	return filepath.Join(planDir, gstore.GraphDirName, "test-evidence", node, id+".active")
}

func Check(o CheckOptions) (*CheckResult, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Expect != "red" && o.Expect != "green" {
		return nil, fmt.Errorf("test check: expect must be red or green")
	}
	g, err := gstore.Load(gstore.PathFor(o.PlanDir))
	if err != nil {
		return nil, err
	}
	node := g.NodeByID(o.Node)
	if node == nil {
		return nil, fmt.Errorf("test check: node %q does not exist", o.Node)
	}
	if _, err := Load(o.PlanDir, o.Node, o.AttemptID); err != nil {
		for i := range g.Nodes {
			other := &g.Nodes[i]
			if other.ID == o.Node {
				continue
			}
			if a, foreignErr := Load(o.PlanDir, other.ID, o.AttemptID); foreignErr == nil {
				return &CheckResult{Attempt: a, Refusal: "attempt belongs to a different node"}, nil
			}
		}
		return nil, err
	}
	return CheckCurrent(o, g, node)
}

// CheckCurrent revalidates an attempt against a caller-supplied fresh graph
// snapshot. Graph admission uses it inside its CAS callback so source,
// profile, claim, and compatible-red checks are repeated on every retry.
func CheckCurrent(o CheckOptions, g *model.Graph, node *model.Node) (*CheckResult, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	a, err := Load(o.PlanDir, o.Node, o.AttemptID)
	if err != nil {
		return nil, err
	}
	refuse := func(s string) (*CheckResult, error) { return &CheckResult{Attempt: a, Refusal: s}, nil }
	if node.Gate.Evidence != model.EvidenceObservedV1 {
		return nil, fmt.Errorf("test check: node %q does not require observed-v1 evidence", o.Node)
	}
	if a.Node != o.Node || a.Plan != filepath.Base(o.PlanDir) {
		return refuse("attempt belongs to a different plan or node")
	}
	if o.By != "" && a.By != o.By {
		return refuse("attempt belongs to a different holder")
	}
	if node.Claim == nil {
		return refuse("attempt claim is no longer current")
	}
	expires, e := time.Parse(time.RFC3339, node.Claim.LeaseExpires)
	if e != nil || !expires.After(o.Now()) {
		return refuse("attempt claim has expired")
	}
	if a.By != node.Claim.By || a.ClaimInstance != node.Claim.Instance || a.Workspace != opaqueWorkspace(node.Claim.Workspace) {
		return refuse("attempt belongs to a different holder, claim instance, or workspace")
	}
	workspace := o.RepoRoot
	if node.Claim.Workspace != "" {
		if filepath.IsAbs(node.Claim.Workspace) {
			workspace = node.Claim.Workspace
		} else {
			workspace = filepath.Join(o.RepoRoot, filepath.FromSlash(node.Claim.Workspace))
		}
	}
	workspace, e = filepath.Abs(workspace)
	if e != nil {
		return nil, e
	}
	if a.Phase == "diagnostic" || a.Phase != o.Expect {
		return refuse("attempt phase cannot satisfy the expected admission")
	}
	raw, err := readBundleFile(o.PlanDir, o.Node, o.AttemptID, "stdout.json")
	if err != nil {
		return nil, err
	}
	stderr, err := readBundleFile(o.PlanDir, o.Node, o.AttemptID, "stderr.txt")
	if err != nil {
		return nil, err
	}
	if digestBytes(raw) != a.Execution.StdoutDigest || digestBytes(stderr) != a.Execution.StderrDigest {
		return refuse("attempt raw output integrity check failed")
	}
	report, err := ParseGoReport(raw, a.Selected, a.Execution.ExitCode)
	if err != nil {
		return refuse("attempt report is no longer valid: " + err.Error())
	}
	if !equalJSON(report, a.Report) || attemptDigest(*a) != a.Digest {
		return refuse("attempt header or parsed report integrity check failed")
	}
	prepared, err := prepareGo(context.Background(), workspace, o.PlanDir, node)
	if err != nil {
		return nil, fmt.Errorf("test check: resolve current execution profile: %w", err)
	}
	if !equalJSON(prepared.Selected, a.Selected) || !profilesEqual(prepared.Profile, a.Profile) {
		return refuse("selected tests or effective execution profile changed")
	}
	current, err := snapshot(o.PlanningRoot, o.RepoRoot, o.PlanDir, workspace, g, node, a.Selected)
	if err != nil {
		return nil, fmt.Errorf("test check: current candidate: %w", err)
	}
	if !equalJSON(a.Before, a.After) || !equalJSON(a.Before, current) {
		return refuse("candidate, inputs, intent, or obligation changed during or after execution")
	}
	if o.Expect == "red" && report.Result != model.ResultFail {
		return refuse("red admission requires actual selected-test failures")
	}
	if o.Expect == "green" && report.Result != model.ResultPass {
		return refuse("green admission requires all selected tests to pass")
	}
	compat := compatibility(node, a)
	var failed []string
	for _, t := range report.Tests {
		if t.Outcome == model.ResultFail {
			failed = append(failed, t.QualifiedID)
		}
	}
	if o.Expect == "green" {
		var missing []string
		for _, t := range node.Gate.Tests {
			if len(t.Satisfies) == 0 {
				continue
			}
			for _, s := range a.Selected {
				if s.ID == t.ID && s.File == t.File {
					r, ok := node.RedEvidence[s.Qualified()]
					consumed := node.ConsumedAttempts[r.AttemptID]
					linked, hasLink := consumed.RedEvidence[s.Qualified()]
					first, hasFirst := node.RedSeqs[t.ID]
					validKind := r.Kind == "baseline" || r.Kind == "sensitivity"
					validFault := (r.Kind == "baseline" && r.Fault == "") || (r.Kind == "sensitivity" && strings.TrimSpace(r.Fault) != "")
					if !ok || r.CompatibilityKey != compat[s.Qualified()] || r.Seq <= 0 || consumed.Seq != r.Seq || consumed.Result != model.ResultFail || !hasLink || linked != r || !hasFirst || first <= 0 || first > r.Seq || !validKind || !validFault || r.Seq >= nextAdmissionSeq(g) {
						missing = append(missing, s.Qualified())
					}
				}
			}
		}
		if len(missing) > 0 {
			return refuse("compatible prior observed red is absent for " + strings.Join(missing, ", "))
		}
	}
	return &CheckResult{Eligible: true, Attempt: a, Current: current, Compatibility: compat, Failed: failed}, nil
}

func nextAdmissionSeq(g *model.Graph) int { return g.SeqCounter + 1 }

func Load(planDir, node, id string) (*Attempt, error) {
	if !attemptIDPattern.MatchString(id) {
		return nil, fmt.Errorf("test evidence: invalid attempt id %q", id)
	}
	raw, err := readBundleFile(planDir, node, id, "header.json")
	if err != nil {
		return nil, fmt.Errorf("test evidence: attempt is not finalized: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var a Attempt
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("test evidence: invalid attempt header: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("test evidence: attempt header has trailing data")
	}
	if a.Protocol != Protocol || a.ID != id || a.Node != node {
		return nil, fmt.Errorf("test evidence: foreign or unsupported attempt header")
	}
	if err := validateAttempt(&a); err != nil {
		return nil, fmt.Errorf("test evidence: invalid attempt header: %w", err)
	}
	return &a, nil
}

func validateAttempt(a *Attempt) error {
	if !a.Execution.Started || !a.Execution.Completed {
		return fmt.Errorf("execution is not complete")
	}
	if a.Execution.ExitCode < 0 {
		return fmt.Errorf("invalid exit code")
	}
	start, e := time.Parse(time.RFC3339Nano, a.Started)
	if e != nil {
		return fmt.Errorf("invalid started timestamp")
	}
	end, e := time.Parse(time.RFC3339Nano, a.Completed)
	if e != nil || end.Before(start) {
		return fmt.Errorf("invalid completed timestamp")
	}
	switch a.Phase {
	case "red":
		if a.RedKind != "baseline" && a.RedKind != "sensitivity" {
			return fmt.Errorf("red attempt lacks a valid red kind")
		}
		if a.RedKind == "sensitivity" && strings.TrimSpace(a.Fault) == "" {
			return fmt.Errorf("sensitivity red lacks fault")
		}
		if a.RedKind == "baseline" && a.Fault != "" {
			return fmt.Errorf("baseline red carries fault metadata")
		}
	case "green", "diagnostic":
		if a.RedKind != "" || a.Fault != "" {
			return fmt.Errorf("non-red attempt carries red metadata")
		}
	default:
		return fmt.Errorf("invalid phase")
	}
	if len(a.Selected) == 0 || len(a.Report.Tests) != len(a.Selected) {
		return fmt.Errorf("selected/report identity count mismatch")
	}
	for i, s := range a.Selected {
		r := a.Report.Tests[i]
		if r.Package != s.Package || r.ID != s.ID || r.File != s.File || r.QualifiedID != s.Qualified() {
			return fmt.Errorf("selected/report identity mismatch")
		}
	}
	if a.Profile.Adapter != "go-test-v1" || a.Profile.Executable == "" || a.Profile.Version == "" || a.Profile.TimeoutSeconds <= 0 {
		return fmt.Errorf("incomplete execution profile")
	}
	for _, k := range []string{"GOOS", "GOARCH", "CGO_ENABLED", "CC", "CXX", "GOWORK", "GOENV", "GOTOOLCHAIN", "GOFLAGS"} {
		if a.Profile.Environment[k] == "" {
			return fmt.Errorf("execution profile lacks controlled %s identity", k)
		}
	}
	if a.Execution.StdoutDigest == "" || a.Execution.StderrDigest == "" {
		return fmt.Errorf("execution output digests are missing")
	}
	return nil
}

func loadCurrent(planDir, repoRoot, nodeID, by string, now time.Time) (*model.Graph, *model.Node, string, error) {
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		return nil, nil, "", err
	}
	n := g.NodeByID(nodeID)
	if n == nil {
		return nil, nil, "", fmt.Errorf("test evidence: node %q does not exist", nodeID)
	}
	if n.Claim == nil || n.Claim.Instance == "" {
		return nil, nil, "", fmt.Errorf("test evidence: observed capture requires a current claim with an instance nonce")
	}
	if n.Claim.By != by || by == "" {
		return nil, nil, "", fmt.Errorf("test evidence: node is claimed by %q, not %q", n.Claim.By, by)
	}
	expires, err := time.Parse(time.RFC3339, n.Claim.LeaseExpires)
	if err != nil || !expires.After(now) {
		return nil, nil, "", fmt.Errorf("test evidence: claim has expired")
	}
	workspace := repoRoot
	if n.Claim.Workspace != "" {
		if filepath.IsAbs(n.Claim.Workspace) {
			workspace = n.Claim.Workspace
		} else {
			workspace = filepath.Join(repoRoot, filepath.FromSlash(n.Claim.Workspace))
		}
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, nil, "", err
	}
	return g, n, abs, nil
}

func createAttemptDir(planDir, node string) (string, string, error) {
	if err := safeName(node); err != nil {
		return "", "", err
	}
	_, err := ensureOwnedDir(planDir, []string{gstore.GraphDirName, "test-evidence", node}, true)
	if err != nil {
		return "", "", err
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	id := "at-" + hex.EncodeToString(b[:])
	dir, err := ensureOwnedDir(planDir, []string{gstore.GraphDirName, "test-evidence", node, id}, true)
	if err != nil {
		return "", "", err
	}
	return id, dir, nil
}

func readBundleFile(planDir, node, id, name string) ([]byte, error) {
	if err := safeName(node); err != nil {
		return nil, err
	}
	if !attemptIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid attempt id")
	}
	if name != "header.json" && name != "stdout.json" && name != "stderr.txt" && name != "incomplete.json" {
		return nil, fmt.Errorf("invalid attempt file name")
	}
	base, err := ensureOwnedDir(planDir, []string{gstore.GraphDirName, "test-evidence", node, id}, false)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(base, name)
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("attempt file is not a regular owned file")
	}
	limit := int64(procexec.DefaultMachineLimit)
	if name == "stderr.txt" {
		limit = int64(procexec.DefaultDiagnosticLimit)
	} else if name == "header.json" || name == "incomplete.json" {
		limit = maxHeaderBytes
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("attempt file exceeds bounded read limit")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("attempt file changed during open")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e == nil && int64(len(b)) > limit {
		e = fmt.Errorf("attempt file exceeds bounded read limit")
	}
	return b, e
}
func writeExclusive(path string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	w := writeAll(f, b)
	if w == nil {
		w = f.Sync()
	}
	c := f.Close()
	if w != nil {
		return w
	}
	return c
}
func publishFinal(path string, b []byte) error {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := path + ".tmp-" + hex.EncodeToString(nonce[:])
	if err := writeExclusive(tmp, b, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := os.Link(tmp, path); err != nil {
		return err
	}
	return nil
}
func safeName(s string) error {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(s) || strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") || isWindowsReserved(s) {
		return fmt.Errorf("unsafe node identity %q", s)
	}
	return nil
}
func writeAll(f *os.File, b []byte) error {
	for len(b) > 0 {
		n, e := f.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func isWindowsReserved(s string) bool {
	base := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return strings.Contains(s, ":")
}
func ensureOwnedDir(planDir string, parts []string, create bool) (string, error) {
	planAbs, err := filepath.Abs(planDir)
	if err != nil {
		return "", err
	}
	planReal, err := filepath.EvalSymlinks(planAbs)
	if err != nil {
		return "", err
	}
	cur := planAbs
	for _, part := range parts {
		if part != gstore.GraphDirName && part != "test-evidence" {
			if err := safeName(part); err != nil {
				return "", err
			}
		}
		cur = filepath.Join(cur, part)
		info, e := os.Lstat(cur)
		if os.IsNotExist(e) && create {
			if e = os.Mkdir(cur, 0o700); e != nil {
				return "", e
			}
			info, e = os.Lstat(cur)
		}
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("owned attempt path component %q is not a real directory", part)
		}
		real, e := filepath.EvalSymlinks(cur)
		if e != nil {
			return "", e
		}
		if !within(planReal, real) {
			return "", fmt.Errorf("owned attempt path escapes plan directory")
		}
	}
	return cur, nil
}
func within(base, p string) bool {
	r, e := filepath.Rel(base, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
func opaqueWorkspace(s string) string {
	if s == "" {
		return "shared"
	}
	if !filepath.IsAbs(s) {
		return "workspace:" + filepath.ToSlash(filepath.Clean(s))
	}
	return "workspace:" + digestBytes([]byte(filepath.Clean(s)))
}
func digestBytes(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func mustJSON(v any) []byte       { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func attemptDigest(a Attempt) string     { a.Digest = ""; return digestBytes(mustJSON(a)) }
func CandidateDigest(c Candidate) string { return digestBytes(mustJSON(c)) }
func profileDigest(p Profile) string     { return digestBytes(mustJSON(p)) }

func compatibilityProfileDigest(p Profile) string {
	p.Argv = nil
	p.ProbeNanos = 0
	return profileDigest(p)
}
func profilesEqual(a, b Profile) bool { a.ProbeNanos = 0; b.ProbeNanos = 0; return equalJSON(a, b) }

func compatibility(n *model.Node, a *Attempt) map[string]string {
	out := map[string]string{}
	supportA := map[string]string{}
	supportI := map[string]string{}
	for _, p := range n.Gate.Execution.TestSupportArtifacts {
		supportA[p] = a.Before.Artifacts[p]
	}
	for _, k := range n.Gate.Execution.TestSupportInputs {
		supportI[k] = a.Before.Inputs[k]
	}
	for _, s := range a.Selected {
		var hazards []string
		for _, t := range n.Gate.Tests {
			if t.ID == s.ID && t.File == s.File {
				hazards = append([]string(nil), t.Satisfies...)
			}
		}
		sort.Strings(hazards)
		key := struct {
			Package, ID, File, Source, Profile string
			Hazards                            []string
			SupportArtifacts, SupportInputs    map[string]string
		}{s.Package, s.ID, s.File, a.Before.SelectedTestSources[s.Qualified()], compatibilityProfileDigest(a.Profile), hazards, supportA, supportI}
		out[s.Qualified()] = digestBytes(mustJSON(key))
	}
	return out
}

func (s SelectedTest) Qualified() string { return s.Package + "::" + s.ID }

func snapshot(planningRoot, repoRoot, planDir, workspace string, g *model.Graph, n *model.Node, selected []SelectedTest) (Candidate, error) {
	c := Candidate{Obligation: n.ProofSnapshot(), Artifacts: map[string]string{}, Dependencies: map[string]map[string]string{}, Inputs: map[string]string{}, Intent: map[string]string{}, SelectedTestSources: map[string]string{}, ModuleFiles: map[string]string{}}
	for _, a := range n.Artifacts {
		d, e := hashRootFile(workspace, a, planDir)
		if e != nil {
			return c, fmt.Errorf("artifact %s: %w", a, e)
		}
		c.Artifacts[a] = d
	}
	for _, dep := range n.Deps {
		dn := g.NodeByID(dep)
		if dn == nil {
			return c, fmt.Errorf("dependency %s is missing", dep)
		}
		m := map[string]string{}
		for _, a := range dn.Artifacts {
			d, e := hashRootFile(workspace, a, planDir)
			if e != nil {
				return c, fmt.Errorf("dependency artifact %s: %w", a, e)
			}
			m[a] = d
		}
		c.Dependencies[dep] = m
	}
	sources, e := gcompile.NewSources(planningRoot, repoRoot, filepath.Base(planDir))
	if e != nil {
		return c, e
	}
	current := sources.IntentSnapshot().Hashes()
	for _, j := range n.Justifies {
		h := current[j]
		if h == "" {
			return c, fmt.Errorf("cited intent %s is missing", j)
		}
		c.Intent[j] = h
	}
	inputResolver := graphinputs.NewResolver(graphinputs.Roots{Repository: workspace, Planning: planningRoot})
	for _, in := range n.Inputs {
		r, e := inputResolver.Resolve(in)
		if e != nil {
			return c, fmt.Errorf("input %s: %w", model.InputKey(in), e)
		}
		inputRoot := planningRoot
		if in.Root == model.InputRootRepository {
			inputRoot = workspace
		}
		if e := rejectOwnedPath(filepath.Join(inputRoot, filepath.FromSlash(r.Path)), planDir, workspace); e != nil {
			return c, e
		}
		c.Inputs[model.InputKey(in)] = r.Digest
	}
	for _, s := range selected {
		d, e := hashRootFile(workspace, s.File, planDir)
		if e != nil {
			return c, e
		}
		c.SelectedTestSources[s.Qualified()] = d
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		p := filepath.Join(workspace, name)
		if _, e := os.Lstat(p); e == nil {
			b, e := readSafeRootFile(workspace, name, planDir, 4<<20)
			if e != nil {
				return c, fmt.Errorf("read %s: %w", name, e)
			}
			if !declaredWholeSource(n, name) {
				return c, fmt.Errorf("module file %s must be a declared artifact or whole repository input", name)
			}
			c.ModuleFiles[name] = digestBytes(b)
		} else if name == "go.mod" {
			return c, fmt.Errorf("go.mod is missing")
		} else if !os.IsNotExist(e) {
			return c, fmt.Errorf("read go.sum: %w", e)
		}
	}
	return c, nil
}
func declaredWholeSource(n *model.Node, path string) bool {
	for _, a := range n.Artifacts {
		if a == path {
			return true
		}
	}
	for _, in := range n.Inputs {
		if in.Root == model.InputRootRepository && in.Path == path && in.Section == nil {
			return true
		}
	}
	return false
}

func hashRootFile(root, rel, planDir string) (string, error) {
	if filepath.IsAbs(rel) || filepath.ToSlash(rel) != rel || hasSegment(rel, "..") || (runtime.GOOS == "windows" && (strings.Contains(rel, ":") || unsafeRelativeComponent(rel))) {
		return "", fmt.Errorf("unsafe root-relative path %q", rel)
	}
	base, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	p, e := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
	if e != nil {
		return "", e
	}
	if !within(base, p) {
		return "", fmt.Errorf("path escapes repository root")
	}
	if e := rejectOwnedPath(p, planDir, root); e != nil {
		return "", e
	}
	info, e := os.Stat(p)
	if e != nil {
		return "", e
	}
	if info.IsDir() {
		if e := validateArtifactTree(p, planDir, root); e != nil {
			return "", e
		}
		d := graphdigest.New(root).Artifact(rel)
		if d == "" {
			return "", fmt.Errorf("directory artifact could not be digested")
		}
		return d, nil
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return "", e
	}
	return digestBytes(b), nil
}
func validateArtifactTree(root, planDir, workspace string) error {
	return filepath.WalkDir(root, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory artifact contains symlink %s", filepath.ToSlash(path))
		}
		if de.IsDir() && de.Name() == gstore.GraphDirName {
			return fmt.Errorf("directory artifact contains live graph runtime %s", filepath.ToSlash(path))
		}
		if !de.IsDir() && de.Name() == filepath.Base(planDir)+"-Graph.json" {
			return fmt.Errorf("directory artifact contains live graph file %s", filepath.ToSlash(path))
		}
		return rejectOwnedPath(path, planDir, workspace)
	})
}
func unsafeRelativeComponent(path string) bool {
	parts := strings.Split(path, "/")
	for i, s := range parts {
		if s == "" && i == len(parts)-1 {
			continue
		}
		if s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") || isWindowsReserved(s) {
			return true
		}
	}
	return false
}
func rejectOwnedPath(path, planDir, workspace string) error {
	real, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	workspaceReal, e := filepath.EvalSymlinks(workspace)
	if e != nil {
		return e
	}
	if within(workspaceReal, real) {
		rel, e := filepath.Rel(workspaceReal, real)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if hasSegment(rel, gstore.GraphDirName) || hasSegment(rel, ".git") {
			return fmt.Errorf("graph, evidence, or repository control output cannot be an exercised source")
		}
		plan := filepath.Base(planDir)
		graphSuffix := filepath.ToSlash(filepath.Join("Plans", plan, plan+"-Graph.json"))
		if rel == plan+"-Graph.json" || strings.HasSuffix(rel, "/"+graphSuffix) {
			return fmt.Errorf("live graph copy cannot be an exercised source")
		}
		return nil
	}
	owned := []string{gstore.PathFor(planDir), filepath.Join(planDir, gstore.GraphDirName)}
	for _, p := range owned {
		rp, e := filepath.EvalSymlinks(p)
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return e
		}
		if real == rp || within(rp, real) {
			return fmt.Errorf("live graph or test-evidence output cannot be an exercised source")
		}
	}
	return nil
}
func hasSegment(path, want string) bool {
	for _, s := range strings.Split(filepath.ToSlash(path), "/") {
		if s == want {
			return true
		}
	}
	return false
}

func prepareGo(ctx context.Context, workspace, planDir string, n *model.Node) (preparedGo, error) {
	var zero preparedGo
	modRaw, e := readSafeRootFile(workspace, "go.mod", planDir, 4<<20)
	if e != nil {
		return zero, fmt.Errorf("one root go.mod is required: %w", e)
	}
	module := modfile.ModulePath(modRaw)
	if module == "" {
		return zero, fmt.Errorf("go.mod has no valid module directive")
	}
	if _, e := os.Lstat(filepath.Join(workspace, "go.work")); e == nil {
		return zero, fmt.Errorf("go.work layouts are unsupported")
	} else if !os.IsNotExist(e) {
		return zero, e
	}
	pkgs := map[string]bool{}
	var selected []SelectedTest
	for _, t := range n.Gate.Tests {
		raw, e := readSafeRootFile(workspace, t.File, planDir, 16<<20)
		if e != nil {
			return zero, fmt.Errorf("selected test source %s: %w", t.File, e)
		}
		f, e := parser.ParseFile(token.NewFileSet(), t.File, raw, parser.PackageClauseOnly)
		if e != nil || f.Name == nil {
			return zero, fmt.Errorf("test source %s has no valid Go package declaration", t.File)
		}
		dir := filepath.ToSlash(filepath.Dir(t.File))
		if dir == "." {
			dir = ""
		}
		if e := rejectSelectedNestedModule(workspace, dir); e != nil {
			return zero, e
		}
		pkg := module
		if dir != "" {
			pkg += "/" + dir
		}
		selected = append(selected, SelectedTest{Package: pkg, ID: t.ID, File: t.File})
		pkgs[dir] = true
	}
	var dirs []string
	for d := range pkgs {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	var patterns []string
	for _, s := range selected {
		patterns = append(patterns, regexp.QuoteMeta(s.ID))
	}
	runRE := "^(?:" + strings.Join(patterns, "|") + ")(?:/|$)"
	args := []string{"test", "-json", "-count=1", "-run", runRE}
	args = append(args, n.Gate.Execution.Args...)
	args = append(args, "-timeout", fmt.Sprintf("%ds", n.Gate.Execution.TimeoutSeconds))
	for _, d := range dirs {
		if d == "" {
			args = append(args, ".")
		} else {
			args = append(args, "./"+d)
		}
	}
	env := controlledGoEnv()
	goPath, e := procexec.LookPath("go", env)
	if e != nil {
		return zero, e
	}
	exe, e := os.ReadFile(goPath)
	if e != nil {
		return zero, e
	}
	version, e := procexec.Capture(ctx, goPath, []string{"version"}, procexec.Policy{Dir: workspace, Env: env, Timeout: 10 * time.Second})
	if e != nil {
		return zero, e
	}
	if version.ExitCode != 0 {
		return zero, fmt.Errorf("go version exited %d", version.ExitCode)
	}
	keys := []string{"GOOS", "GOARCH", "CGO_ENABLED", "CC", "CXX", "GOWORK", "GOENV", "GOTOOLCHAIN", "GOFLAGS"}
	envResult, e := procexec.Capture(ctx, goPath, []string{"env", "-json"}, procexec.Policy{Dir: workspace, Env: env, Timeout: 10 * time.Second})
	if e != nil {
		return zero, e
	}
	if envResult.ExitCode != 0 {
		return zero, fmt.Errorf("go env exited %d", envResult.ExitCode)
	}
	actual := map[string]string{}
	if e = json.Unmarshal(envResult.Stdout, &actual); e != nil {
		return zero, fmt.Errorf("decode go env: %w", e)
	}
	identities := map[string]string{}
	for _, k := range keys {
		identities[k] = digestBytes([]byte(actual[k]))
	}
	for _, k := range n.Gate.Execution.EnvironmentKeys {
		value, ok := actual[k]
		if !ok {
			value = envValue(env, k)
		}
		identities[k] = digestBytes([]byte(value))
	}
	probeNanos := version.Run.Nanoseconds() + version.Cleanup.Nanoseconds() + envResult.Run.Nanoseconds() + envResult.Cleanup.Nanoseconds()
	p := Profile{Adapter: "go-test-v1", Args: append([]string(nil), n.Gate.Execution.Args...), Argv: append([]string{"go"}, args...), TimeoutSeconds: n.Gate.Execution.TimeoutSeconds, WorkingDirectory: ".", Executable: digestBytes(exe), Version: strings.TrimSpace(string(version.Stdout)), Environment: identities, ProbeNanos: probeNanos}
	return preparedGo{Selected: selected, Profile: p, Args: args, Executable: goPath, Env: env}, nil
}

func controlledGoEnv() []string {
	out := append([]string(nil), os.Environ()...)
	for k, v := range map[string]string{"GOWORK": "off", "GOENV": "off", "GOTOOLCHAIN": "local", "GOFLAGS": ""} {
		setEnv(&out, k, v)
	}
	return out
}
func setEnv(env *[]string, key, value string) {
	for i, kv := range *env {
		if k, _, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, key) {
			(*env)[i] = key + "=" + value
			return
		}
	}
	*env = append(*env, key+"="+value)
}
func envValue(env []string, key string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
func rejectSelectedNestedModule(root, dir string) error {
	cur := filepath.FromSlash(dir)
	for cur != "" && cur != "." {
		for _, name := range []string{"go.mod", "go.work"} {
			p := filepath.Join(root, cur, name)
			if _, e := os.Lstat(p); e == nil {
				return fmt.Errorf("selected test path crosses unsupported nested %s at %s", name, filepath.ToSlash(filepath.Join(cur, name)))
			} else if !os.IsNotExist(e) {
				return e
			}
		}
		cur = filepath.Dir(cur)
	}
	return nil
}
func readSafeRootFile(root, rel, planDir string, limit int64) ([]byte, error) {
	if _, e := hashRootFile(root, rel, planDir); e != nil {
		return nil, e
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	info, e := os.Stat(p)
	if e != nil {
		return nil, e
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds limit")
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}
func executionRevision(workspace string) (string, error) {
	repo, e := vcs.DetectChecked(workspace)
	if e != nil {
		return "", e
	}
	rev, e := repo.Head()
	if errors.Is(e, vcs.ErrUnsupported) {
		return "", nil
	}
	return rev, e
}
