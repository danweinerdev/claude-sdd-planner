// Command evidencecostexercise runs the --cost feature through the real graph,
// claim, observed-test, Git-integration, and review-boundary machinery in an
// isolated copy. It never writes the source repository.
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
)

const (
	baseRevision  = "0afcfc8"
	planName      = "EvidenceCostRealFeature"
	benchmarkPlan = "EvidenceCostGeneratedBenchmark"
	identity      = "evidence-cost-exercise"
)

var foundationOverlays = []string{
	"cmd/sdd/root_test.go",
	"internal/graph/store/store.go",
	"internal/graph/sync/sync.go",
	"internal/graph/sync/sync_test.go",
	"internal/testevidence/attempt.go",
	"internal/evidencecost/format.go",
	"internal/evidencecost/format_test.go",
	"internal/evidencecost/recorder.go",
	"internal/evidencecost/recorder_test.go",
	"internal/evidencecost/types.go",
}

type config struct {
	Mode, Source, WorkRoot, Controller, Candidate, ModuleCache string
}

type runner struct {
	cfg     config
	root    string
	env     []string
	private string
	ops     []operation
	metrics []metric
	status  []graphNodeStatus
}

type operation struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Exit     int    `json:"exit"`
	Millis   int64  `json:"duration_ms"`
}

type metric struct {
	Operation string           `json:"operation"`
	Exit      int              `json:"exit"`
	TotalNS   int64            `json:"total_ns"`
	PhaseNS   map[string]int64 `json:"phase_ns"`
	Counters  map[string]int64 `json:"counters"`
}

type summary struct {
	Version         int                 `json:"version"`
	Status          string              `json:"status"`
	BaseRevision    string              `json:"source_base_revision"`
	ReviewBase      string              `json:"review_base"`
	ReviewEndpoint  string              `json:"review_endpoint"`
	SourceGroups    map[string][]string `json:"source_file_groups"`
	SourceHashes    map[string]string   `json:"source_hashes"`
	Tests           map[string]string   `json:"test_outcomes"`
	Metrics         []metric            `json:"metrics"`
	Operations      []operation         `json:"operations"`
	SourceIdentity  sourceIdentity      `json:"source_identity"`
	RootLifecycle   string              `json:"root_lifecycle"`
	GraphStatus     []graphNodeStatus   `json:"graph_status"`
	ComparisonReady bool                `json:"comparison_ready"`
	ComparisonRan   bool                `json:"comparison_ran"`
	Residual        []string            `json:"residual_unverified_work"`
}

type graphNodeStatus struct {
	ID         string          `json:"id"`
	State      string          `json:"state"`
	Reasons    json.RawMessage `json:"reasons,omitempty"`
	Advisories []string        `json:"advisories,omitempty"`
}

type lifecycleStep struct {
	Name  string
	Stage string
	Args  []string
}

type sourceIdentity struct {
	PrimaryRevision string `json:"primary_revision"`
	SourceRevision  string `json:"source_revision"`
}

type verification struct {
	Status              string            `json:"status"`
	PrimaryRevision     string            `json:"primary_revision"`
	SourceRevision      string            `json:"source_revision"`
	PrimaryMatches      bool              `json:"primary_matches_declared_hashes"`
	SourceMatches       bool              `json:"source_matches_declared_feature"`
	HistoricalOnlyMatch bool              `json:"only_historical_hashes_match"`
	MissingPrimary      []string          `json:"missing_primary_files,omitempty"`
	MissingSource       []string          `json:"missing_source_files,omitempty"`
	StalePrimary        map[string]string `json:"stale_primary_files,omitempty"`
	StaleSource         map[string]string `json:"stale_source_files,omitempty"`
}

type targetBinding struct {
	PrimaryMatches bool              `json:"target_primary_matches_summary"`
	SourceMatches  bool              `json:"working_source_matches_summary"`
	PrimaryStale   map[string]string `json:"target_primary_stale,omitempty"`
	SourceStale    map[string]string `json:"working_source_stale,omitempty"`
}

type benchmarkAttempt struct {
	Node            string `json:"node"`
	AttemptID       string `json:"attempt_id"`
	WorkspaceDigest string `json:"workspace_digest"`
	ClaimInstance   string `json:"claim_instance"`
}

type syncOutcome struct {
	Controller         string `json:"controller"`
	Node               string `json:"node"`
	AttemptID          string `json:"attempt_id"`
	Recorded           bool   `json:"recorded"`
	Historical         bool   `json:"historical"`
	AnchorSnapshot     bool   `json:"anchor_snapshot"`
	CurrentTargetMatch bool   `json:"current_target_match"`
	Result             string `json:"result"`
	Sequence           int    `json:"sequence"`
	Metric             metric `json:"metric"`
}

type checkPair struct {
	Index   int               `json:"index"`
	Order   []string          `json:"order"`
	Metrics map[string]metric `json:"metrics"`
}

type costPayload struct {
	TotalNS  int64            `json:"total_ns"`
	PhaseNS  map[string]int64 `json:"phase_ns"`
	Counters map[string]int64 `json:"counters"`
}

type graphStatusEnvelope struct {
	States map[string]int `json:"states"`
	Closed int            `json:"closed"`
	Nodes  []struct {
		ID      string `json:"id"`
		State   string `json:"state"`
		Closed  bool   `json:"closed"`
		Claimed string `json:"claimed_by,omitempty"`
	} `json:"nodes"`
}

func main() {
	var c config
	flag.StringVar(&c.Mode, "mode", "run", "run, verify, compare, or resume")
	flag.StringVar(&c.Source, "source", "", "cleanly resolved source repository root")
	flag.StringVar(&c.WorkRoot, "work-root", "", "new exercise directory, or existing directory for verify/compare")
	flag.StringVar(&c.Controller, "controller", "", "baseline instrumented sdd executable")
	flag.StringVar(&c.Candidate, "candidate", "", "candidate sdd executable (compare mode)")
	flag.StringVar(&c.ModuleCache, "module-cache", "", "existing cached GOMODCACHE")
	flag.Parse()
	if err := execute(c); err != nil {
		fmt.Fprintln(os.Stderr, "evidencecostexercise:", err)
		os.Exit(1)
	}
}

func execute(c config) error {
	for name, value := range map[string]string{"--source": c.Source, "--work-root": c.WorkRoot} {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if c.Mode != "verify" {
		for name, value := range map[string]string{"--controller": c.Controller, "--module-cache": c.ModuleCache} {
			if value == "" {
				return fmt.Errorf("%s is required", name)
			}
		}
	}
	if c.Mode == "compare" && c.Candidate == "" {
		return errors.New("--candidate is required in compare mode")
	}
	r := &runner{cfg: c, root: filepath.Join(c.WorkRoot, "primary"), private: filepath.Join(c.WorkRoot, "private")}
	if err := r.configure(); err != nil {
		return err
	}
	switch c.Mode {
	case "run":
		return r.run()
	case "verify":
		return r.verify()
	case "compare":
		return r.compare()
	case "resume":
		return errors.New("resume is intentionally unsupported: create a new run root after source or review changes; the historical root remains immutable")
	default:
		return fmt.Errorf("unknown --mode %q", c.Mode)
	}
}

func (r *runner) configure() error {
	absSource, err := canonicalExistingDir(r.cfg.Source)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	absWork, err := canonicalProspectivePath(r.cfg.WorkRoot)
	if err != nil {
		return fmt.Errorf("work root: %w", err)
	}
	gitRoot, err := r.gitOutput(absSource, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("source repository: %w", err)
	}
	canonicalGitRoot, err := canonicalExistingDir(gitRoot)
	if err != nil || !samePath(absSource, canonicalGitRoot) {
		return errors.New("--source must be the physical Git repository root")
	}
	r.cfg.Source, r.cfg.WorkRoot, r.root, r.private = absSource, absWork, filepath.Join(absWork, "primary"), filepath.Join(absWork, "private")
	if samePath(absSource, absWork) || within(absSource, absWork) || within(absWork, absSource) {
		return errors.New("work root must be physically outside the source repository")
	}
	if r.cfg.Mode != "verify" {
		paths := []string{r.cfg.Controller, r.cfg.ModuleCache}
		if r.cfg.Mode == "compare" {
			paths = append(paths, r.cfg.Candidate)
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err != nil {
				return err
			}
		}
	}
	keep := []string{"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "COMSPEC", "TEMP", "TMP", "USERPROFILE"}
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			r.env = append(r.env, k+"="+v)
		}
	}
	r.env = append(r.env,
		"GOMODCACHE="+r.cfg.ModuleCache, "GOCACHE="+filepath.Join(r.cfg.WorkRoot, "go-cache"), "GOPROXY=off", "GOSUMDB=off", "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+nullDevice(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_ASKPASS=", "SSH_ASKPASS=",
	)
	r.env = append(r.env, gitChildConfig()...)
	return nil
}

func gitChildConfig() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	// The isolated workspaces can exceed legacy Win32 path limits. This
	// process-local Git override reaches provider-spawned Git children too;
	// it writes no repository, global, or system configuration.
	return []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.longpaths", "GIT_CONFIG_VALUE_0=true"}
}

func (r *runner) run() error {
	parent := filepath.Dir(r.cfg.WorkRoot)
	if st, err := os.Stat(parent); err != nil || !st.IsDir() {
		return fmt.Errorf("verified parent %s is unavailable", parent)
	}
	if _, err := os.Stat(r.cfg.WorkRoot); !os.IsNotExist(err) {
		return errors.New("refusing to reuse an existing work root")
	}
	if err := os.Mkdir(r.cfg.WorkRoot, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(r.root, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(r.private, 0o700); err != nil {
		return err
	}

	tracked, err := r.gitOutput(r.cfg.Source, "ls-tree", "-r", "--name-only", baseRevision, "--", "cmd/sdd", "internal", "go.mod", "go.sum")
	if err != nil {
		return err
	}
	paths := selectSourcePaths(nonemptyLines(tracked))
	// Archive only the explicit ls-files result: no untracked or unrelated root
	// resources are read, while embedded assets under cmd/internal are retained.
	args := append([]string{"archive", "--format=tar", baseRevision, "--"}, paths...)
	archive, err := r.capture("source archive", "archive", r.cfg.Source, "git", args, 2*time.Minute, false)
	if err != nil {
		return err
	}
	if err := extractSafeTar(archive.Stdout, r.root); err != nil {
		return err
	}
	if err := r.git(r.root, "init"); err != nil {
		return err
	}
	if err := r.commitAll(r.root, "exercise base source"); err != nil {
		return err
	}
	reviewBase, err := r.gitOutput(r.root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	if err := r.overlay(r.root, foundationOverlays); err != nil {
		return err
	}
	if err := r.writePlanning(); err != nil {
		return err
	}
	if err := r.commitAll(r.root, "exercise foundation and generated intent"); err != nil {
		return err
	}
	if err := r.applyLifecycle(); err != nil {
		return err
	}
	if err := r.controller("graph-init", "graph", "init", "--plan", planName, "--json"); err != nil {
		return err
	}
	proposalPath, groups, err := r.writeProposal(paths)
	if err != nil {
		return err
	}
	if err := r.controller("graph-propose", "graph", "propose", "--plan", planName, "--file", proposalPath, "--json"); err != nil {
		return err
	}
	if err := r.controller("graph-assemble", "graph", "assemble", "--plan", planName, "--json"); err != nil {
		return err
	}
	if err := r.controller("compile", "compile", "--plan", planName, "--json"); err != nil {
		return err
	}
	if err := r.activatePrimaryPlan(); err != nil {
		return err
	}
	if err := r.commitAll(r.root, "compile exercise graph"); err != nil {
		return err
	}

	tests := map[string]string{}
	testWorkspace, err := r.claim("test-cost")
	if err != nil {
		return err
	}
	syncWorkspace, err := r.claim("sync-cost")
	if err != nil {
		return err
	}
	if err := r.exerciseLeaf("test-cost", testWorkspace, "cmd/sdd/test_cost_test.go", "cmd/sdd/test.go", false, tests); err != nil {
		return err
	}
	if err := r.exerciseLeaf("sync-cost", syncWorkspace, "cmd/sdd/graph_cost_test.go", "cmd/sdd/graph.go", true, tests); err != nil {
		return err
	}
	if err := r.reverifyNode("test-cost", "cmd/sdd/test_cost_test.go", tests); err != nil {
		return err
	}
	if err := r.controller("integration-input-reanchor-after-leaf-changes", "graph", "set-inputs", "--plan", planName, "--node", "integration-cost", "--file", filepath.Join(r.cfg.WorkRoot, "inputs.json"), "--json"); err != nil {
		return err
	}
	_, err = r.exerciseIntegration(tests)
	if err != nil {
		return err
	}

	endpoint, err := r.gitOutput(r.root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	featurePaths := uniqueSorted(append(append([]string{}, foundationOverlays...), "cmd/sdd/test.go", "cmd/sdd/graph.go", "cmd/sdd/test_cost_test.go", "cmd/sdd/graph_cost_test.go", "cmd/sdd/cost_integration_test.go"))
	hashes := map[string]string{}
	for _, p := range featurePaths {
		b, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(p)))
		if err != nil {
			return err
		}
		hashes[p] = digest(b)
	}
	groups["declared_source_feature"] = featurePaths
	sourceRevision, err := r.gitOutput(r.cfg.Source, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	rootState, err := r.graphLifecycle()
	if err != nil {
		return err
	}
	s := summary{Version: 2, Status: "review-boundary", BaseRevision: baseRevision, ReviewBase: strings.TrimSpace(reviewBase), ReviewEndpoint: strings.TrimSpace(endpoint), SourceGroups: groups, SourceHashes: hashes, Tests: tests, Metrics: r.metrics, Operations: r.ops, SourceIdentity: sourceIdentity{PrimaryRevision: strings.TrimSpace(endpoint), SourceRevision: strings.TrimSpace(sourceRevision)}, RootLifecycle: rootState, GraphStatus: r.status, ComparisonReady: false, ComparisonRan: false, Residual: []string{"independent four-lane review of the new frozen review range", "review artifact admission and graph closure", "fresh live-claim paired controller comparison"}}
	if err := writeJSONFile(filepath.Join(r.cfg.WorkRoot, "summary.json"), s); err != nil {
		return err
	}
	fmt.Printf("work root: %s\nreview range: %s..%s\nsummary: %s\n", r.cfg.WorkRoot, s.ReviewBase, s.ReviewEndpoint, filepath.Join(r.cfg.WorkRoot, "summary.json"))
	return nil
}

func (r *runner) verify() error {
	var s summary
	if err := readJSONFile(filepath.Join(r.cfg.WorkRoot, "summary.json"), &s); err != nil {
		return err
	}
	result := verification{StalePrimary: map[string]string{}, StaleSource: map[string]string{}}
	result.PrimaryRevision, _ = r.gitOutput(r.root, "rev-parse", "HEAD")
	result.SourceRevision, _ = r.gitOutput(r.cfg.Source, "rev-parse", "HEAD")
	result.PrimaryMatches = true
	result.SourceMatches = true
	for rel, want := range s.SourceHashes {
		got, err := hashFile(filepath.Join(r.root, filepath.FromSlash(rel)))
		if os.IsNotExist(err) {
			result.MissingPrimary = append(result.MissingPrimary, rel)
			result.PrimaryMatches = false
		} else if err != nil {
			return err
		} else if got != want {
			result.StalePrimary[rel] = got
			result.PrimaryMatches = false
		}
		got, err = hashFile(filepath.Join(r.cfg.Source, filepath.FromSlash(rel)))
		if os.IsNotExist(err) {
			result.MissingSource = append(result.MissingSource, rel)
			result.SourceMatches = false
		} else if err != nil {
			return err
		} else if got != want {
			result.StaleSource[rel] = got
			result.SourceMatches = false
		}
	}
	sort.Strings(result.MissingPrimary)
	sort.Strings(result.MissingSource)
	result.HistoricalOnlyMatch = result.PrimaryMatches && !result.SourceMatches
	switch {
	case result.PrimaryMatches && result.SourceMatches:
		result.Status = "current-source-and-primary-match"
	case result.HistoricalOnlyMatch:
		result.Status = "historical-primary-only"
	default:
		result.Status = "missing-or-stale"
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	if !result.PrimaryMatches || !result.SourceMatches {
		return errors.New("source verification failed; the JSON verdict identifies historical-only, missing, and stale files")
	}
	return nil
}

func (r *runner) exerciseLeaf(node, workspace, testFile, implementation string, refreshBeforeGreen bool, outcomes map[string]string) error {
	if err := r.overlay(workspace, []string{testFile}); err != nil {
		return err
	}
	if err := assertSame(filepath.Join(workspace, filepath.FromSlash(testFile)), filepath.Join(r.cfg.Source, filepath.FromSlash(testFile))); err != nil {
		return err
	}
	red, attempt, err := r.costCommand(node+"-red", 1, "test", "run", "--plan", planName, "--node", node, "--by", identity, "--phase", "red", "--red-kind", "baseline", "--cost", "--json")
	if err != nil {
		return err
	}
	outcomes[node+" RED"] = fmt.Sprintf("exit %d", red)
	if err := r.costSync(node+"-red-sync", node, attempt, 0); err != nil {
		return err
	}
	if err := r.git(workspace, "add", "--", testFile); err != nil {
		return err
	}
	if err := r.git(workspace, "commit", "-m", node+" contract test"); err != nil {
		return err
	}
	if refreshBeforeGreen {
		if err := r.git(workspace, "rebase", "master"); err != nil {
			return err
		}
	}
	if err := r.overlay(workspace, []string{implementation}); err != nil {
		return err
	}
	if err := assertSame(filepath.Join(workspace, filepath.FromSlash(testFile)), filepath.Join(r.cfg.Source, filepath.FromSlash(testFile))); err != nil {
		return err
	}
	green, greenAttempt, err := r.costCommand(node+"-green", 0, "test", "run", "--plan", planName, "--node", node, "--by", identity, "--phase", "green", "--cost", "--json")
	if err != nil {
		return err
	}
	outcomes[node+" GREEN"] = fmt.Sprintf("exit %d", green)
	if err := r.git(workspace, "add", "--", implementation); err != nil {
		return err
	}
	if err := r.git(workspace, "commit", "-m", node+" implementation"); err != nil {
		return err
	}
	if err := r.integrate(workspace); err != nil {
		return err
	}
	if err := assertSame(filepath.Join(workspace, filepath.FromSlash(testFile)), filepath.Join(r.cfg.Source, filepath.FromSlash(testFile))); err != nil {
		return err
	}
	if _, _, err := r.costCommand(node+"-check", 0, "test", "check", "--plan", planName, "--node", node, "--attempt", greenAttempt, "--expect", "green", "--cost", "--json"); err != nil {
		return err
	}
	return r.costSync(node+"-green-sync", node, greenAttempt, 0)
}

func (r *runner) exerciseIntegration(outcomes map[string]string) (string, error) {
	workspace, err := r.claim("integration-cost")
	if err != nil {
		return "", err
	}
	file := "cmd/sdd/cost_integration_test.go"
	if err := r.overlay(workspace, []string{file}); err != nil {
		return "", err
	}
	if err := assertSame(filepath.Join(workspace, filepath.FromSlash(file)), filepath.Join(r.cfg.Source, filepath.FromSlash(file))); err != nil {
		return "", err
	}
	exit, attempt, err := r.costCommand("integration-green", 0, "test", "run", "--plan", planName, "--node", "integration-cost", "--by", identity, "--phase", "green", "--cost", "--json")
	if err != nil {
		return "", err
	}
	outcomes["integration GREEN characterization"] = fmt.Sprintf("exit %d", exit)
	if err := r.git(workspace, "add", "--", file); err != nil {
		return "", err
	}
	if err := r.git(workspace, "commit", "-m", "integration cost contract coverage"); err != nil {
		return "", err
	}
	if err := r.integrate(workspace); err != nil {
		return "", err
	}
	if err := assertSame(filepath.Join(workspace, filepath.FromSlash(file)), filepath.Join(r.cfg.Source, filepath.FromSlash(file))); err != nil {
		return "", err
	}
	if _, _, err := r.costCommand("integration-check", 0, "test", "check", "--plan", planName, "--node", "integration-cost", "--attempt", attempt, "--expect", "green", "--cost", "--json"); err != nil {
		return "", err
	}
	if err := r.costSync("integration-green-sync", "integration-cost", attempt, 0); err != nil {
		return "", err
	}
	return "", nil
}

func (r *runner) reverifyNode(node, testFile string, outcomes map[string]string) error {
	workspace, err := r.claim(node)
	if err != nil {
		return err
	}
	if err := assertSame(filepath.Join(workspace, filepath.FromSlash(testFile)), filepath.Join(r.cfg.Source, filepath.FromSlash(testFile))); err != nil {
		return err
	}
	exit, attempt, err := r.costCommand(node+"-input-staleness-reverify", 0, "test", "run", "--plan", planName, "--node", node, "--by", identity, "--phase", "green", "--cost", "--json")
	if err != nil {
		return err
	}
	outcomes[node+" GREEN reverify after sibling input change"] = fmt.Sprintf("exit %d", exit)
	if _, _, err := r.costCommand(node+"-input-staleness-check", 0, "test", "check", "--plan", planName, "--node", node, "--attempt", attempt, "--expect", "green", "--cost", "--json"); err != nil {
		return err
	}
	return r.costSync(node+"-input-staleness-sync", node, attempt, 0)
}

func (r *runner) compare() (retErr error) {
	if r.cfg.Candidate == "" {
		return errors.New("--candidate is required in compare mode")
	}
	var s summary
	if err := readJSONFile(filepath.Join(r.cfg.WorkRoot, "summary.json"), &s); err != nil {
		return err
	}
	if s.Version < 2 {
		return errors.New("historical summary has only a cleared unadmitted attempt; refusing the invalid expected-exit-1 comparison (run a corrected exercise in a new root)")
	}
	realPlanState, err := r.realPlanComparisonState()
	if err != nil {
		return err
	}
	binding, err := r.targetBinding(s)
	if err != nil {
		return err
	}
	if !binding.PrimaryMatches {
		return errors.New("comparison target primary no longer matches the summary's frozen feature bytes")
	}
	controllerHashes, err := controllerIdentities(r.cfg.Controller, r.cfg.Candidate)
	if err != nil {
		return err
	}
	attempts, err := r.prepareBenchmarkAttempts()
	if err != nil {
		return err
	}
	cleanupPending := true
	defer func() {
		if cleanupPending {
			if cleanupErr := r.cleanupBenchmarkClaims(); cleanupErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("benchmark claim cleanup: %w", cleanupErr))
			}
		}
	}()
	before, err := r.comparisonSnapshot()
	if err != nil {
		return err
	}
	start := len(r.metrics)
	checkPairs := make([]checkPair, 0, 3)
	for pair := 0; pair < 3; pair++ {
		order := []string{"baseline", "candidate"}
		if pair%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		pairResult := checkPair{Index: pair + 1, Order: append([]string(nil), order...), Metrics: map[string]metric{}}
		for _, controller := range order {
			exe := r.cfg.Controller
			if controller == "candidate" {
				exe = r.cfg.Candidate
			}
			old := r.cfg.Controller
			r.cfg.Controller = exe
			attempt := attempts[controller]
			_, _, checkErr := r.costCommand(fmt.Sprintf("compare-pair-%d-%s-readonly-check", pair+1, controller), 0, "test", "check", "--plan", benchmarkPlan, "--node", attempt.Node, "--attempt", attempt.AttemptID, "--expect", "green", "--cost", "--json")
			r.cfg.Controller = old
			if checkErr != nil {
				return checkErr
			}
			pairResult.Metrics[controller] = r.metrics[len(r.metrics)-1]
		}
		checkPairs = append(checkPairs, pairResult)
	}
	checkMetrics := append([]metric(nil), r.metrics[start:]...)
	if err := validateCheckMetrics(checkMetrics); err != nil {
		return err
	}
	after, err := r.comparisonSnapshot()
	if err != nil {
		return err
	}
	if err := validateReadonlyState(before, after); err != nil {
		return err
	}
	syncBefore := after
	syncs := make([]syncOutcome, 0, 2)
	for _, controller := range []string{"baseline", "candidate"} {
		exe := r.cfg.Controller
		if controller == "candidate" {
			exe = r.cfg.Candidate
		}
		old := r.cfg.Controller
		r.cfg.Controller = exe
		outcome, syncErr := r.benchmarkSync(controller, attempts[controller])
		r.cfg.Controller = old
		if syncErr != nil {
			return syncErr
		}
		syncs = append(syncs, outcome)
	}
	if err := validateSyncOutcomes(syncs); err != nil {
		return err
	}
	syncAfter, err := r.comparisonSnapshot()
	if err != nil {
		return err
	}
	if syncBefore == syncAfter {
		return errors.New("sync measurements admitted no graph-state change")
	}
	postSyncBinding, err := r.targetBinding(s)
	if err != nil {
		return err
	}
	if !postSyncBinding.PrimaryMatches {
		return errors.New("benchmark sync changed or invalidated the frozen target source")
	}
	for i := range syncs {
		syncs[i].CurrentTargetMatch = true
	}
	if err := r.cleanupBenchmarkClaims(); err != nil {
		return err
	}
	cleanupPending = false
	benchmarkState, err := r.benchmarkCompletionState()
	if err != nil {
		return err
	}
	out := struct {
		Verdict               string                      `json:"verdict"`
		Notice                string                      `json:"notice"`
		ComparisonReady       bool                        `json:"comparison_ready"`
		ComparisonRan         bool                        `json:"comparison_ran"`
		RealPlanState         string                      `json:"real_feature_plan_state"`
		BenchmarkState        string                      `json:"benchmark_plan_state"`
		TargetBinding         targetBinding               `json:"target_binding"`
		PostSyncTargetBinding targetBinding               `json:"post_sync_target_binding"`
		ControllerHashes      map[string]string           `json:"controller_binary_hashes"`
		Attempts              map[string]benchmarkAttempt `json:"attempts"`
		ReadonlyStateBefore   string                      `json:"readonly_state_before"`
		ReadonlyStateAfter    string                      `json:"readonly_state_after"`
		SyncStateBefore       string                      `json:"sync_state_before"`
		SyncStateAfter        string                      `json:"sync_state_after"`
		CheckPairs            []checkPair                 `json:"check_pairs"`
		SyncOutcomes          []syncOutcome               `json:"sync_outcomes"`
	}{"both-controllers-check-and-sync-exit-0", "three alternating read-only check pairs followed by one fresh nonhistorical sync per controller; counter assertions only, no timing or statistical speedup claim", true, true, realPlanState, benchmarkState, binding, postSyncBinding, controllerHashes, attempts, before, after, syncBefore, syncAfter, checkPairs, syncs}
	return writeJSONFile(filepath.Join(r.cfg.WorkRoot, "comparison.json"), out)
}

func (r *runner) prepareBenchmarkAttempts() (attempts map[string]benchmarkAttempt, retErr error) {
	var inputs []map[string]string
	if err := readJSONFile(filepath.Join(r.cfg.WorkRoot, "inputs.json"), &inputs); err != nil {
		return nil, err
	}
	proposalBody := benchmarkProposal(inputs)
	planDir := filepath.Join(r.root, ".exercise-plans", "Plans", benchmarkPlan)
	path := benchmarkArtifactPath()
	if _, err := os.Stat(planDir); os.IsNotExist(err) {
		if err := r.createBenchmarkPlan(path, proposalBody); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		if err := r.validateReusableBenchmark(proposalBody); err != nil {
			return nil, err
		}
		if err := r.controller("benchmark-resume-gc", "graph", "gc", "--plan", benchmarkPlan, "--json"); err != nil {
			return nil, err
		}
	}
	attempts = map[string]benchmarkAttempt{}
	claimsStarted := false
	defer func() {
		if retErr != nil && claimsStarted {
			if cleanupErr := r.cleanupBenchmarkClaims(); cleanupErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("cleanup after benchmark preparation failure: %w", cleanupErr))
			}
		}
	}()
	for _, controller := range []string{"baseline", "candidate"} {
		node := "benchmark-" + controller
		workspace, err := r.claimPlan(benchmarkPlan, node)
		if err != nil {
			return nil, err
		}
		claimsStarted = true
		claimInstance, err := r.claimInstance(node)
		if err != nil {
			return nil, err
		}
		exe := r.cfg.Controller
		if controller == "candidate" {
			exe = r.cfg.Candidate
		}
		old := r.cfg.Controller
		r.cfg.Controller = exe
		exit, attempt, runErr := r.costCommand("benchmark-"+controller+"-actual-green", 0, "test", "run", "--plan", benchmarkPlan, "--node", node, "--by", identity, "--phase", "green", "--cost", "--json")
		r.cfg.Controller = old
		if runErr != nil || exit != 0 || attempt == "" {
			return nil, fmt.Errorf("benchmark %s did not produce an eligible attempt: %w", controller, runErr)
		}
		relWorkspace, err := filepath.Rel(r.root, workspace)
		if err != nil {
			return nil, err
		}
		attempts[controller] = benchmarkAttempt{Node: node, AttemptID: attempt, WorkspaceDigest: digest([]byte(filepath.ToSlash(relWorkspace))), ClaimInstance: claimInstance}
	}
	if err := r.assertBenchmarkClaimsLive(); err != nil {
		return nil, err
	}
	if err := validateAttemptIdentities(attempts); err != nil {
		return nil, err
	}
	return attempts, nil
}

func (r *runner) createBenchmarkPlan(path string, proposalBody map[string]any) error {
	if err := r.controllerInput("benchmark-apply-plan", []byte(benchmarkPlanProposal()), applyCommandArgs(path, "plan")...); err != nil {
		return err
	}
	if err := r.controller("benchmark-graph-init", "graph", "init", "--plan", benchmarkPlan, "--json"); err != nil {
		return err
	}
	proposal := filepath.Join(r.cfg.WorkRoot, "benchmark-proposal.json")
	if err := writeJSONFile(proposal, proposalBody); err != nil {
		return err
	}
	for _, step := range []struct {
		name string
		args []string
	}{
		{"benchmark-graph-propose", []string{"graph", "propose", "--plan", benchmarkPlan, "--file", proposal, "--json"}},
		{"benchmark-graph-assemble", []string{"graph", "assemble", "--plan", benchmarkPlan, "--json"}},
		{"benchmark-compile", []string{"compile", "--plan", benchmarkPlan, "--json"}},
		{"benchmark-plan-approve", []string{"plan", "approve", path, "--json"}},
		{"benchmark-plan-activate", []string{"plan", "activate", path, "--json"}},
	} {
		if err := r.controller(step.name, step.args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) validateReusableBenchmark(proposalBody map[string]any) error {
	readme, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(benchmarkArtifactPath())))
	if err != nil {
		return fmt.Errorf("existing benchmark plan is incomplete: %w", err)
	}
	if !strings.Contains(string(readme), "\nstatus: active\n") {
		return errors.New("existing benchmark plan is not active; refusing to rewrite or advance it")
	}
	raw, err := json.Marshal(proposalBody)
	if err != nil {
		return err
	}
	expected, err := decodeHistoricalProposal(raw)
	if err != nil {
		return fmt.Errorf("decode expected historical benchmark proposal: %w", err)
	}
	status, err := r.graphStatus(benchmarkPlan, "benchmark-resume-status")
	if err != nil {
		return err
	}
	if len(status.Nodes) != len(expected.Nodes) {
		return fmt.Errorf("existing benchmark has %d nodes, want %d", len(status.Nodes), len(expected.Nodes))
	}
	expectedByID := map[string]model.Node{}
	for _, node := range expected.Nodes {
		expectedByID[node.ID] = node
	}
	for _, line := range status.Nodes {
		want, ok := expectedByID[line.ID]
		if !ok {
			return fmt.Errorf("existing benchmark contains unexpected node %q", line.ID)
		}
		if line.Claimed != "" {
			return fmt.Errorf("existing benchmark node %q has active claimant %q; refusing takeover", line.ID, line.Claimed)
		}
		wantState := "READY"
		if line.ID == "benchmark-review" {
			wantState = "BLOCKED"
		}
		if line.State != wantState || line.Closed {
			return fmt.Errorf("existing benchmark node %q state=%s closed=%t, want %s open with no observations", line.ID, line.State, line.Closed, wantState)
		}
		got, err := r.benchmarkNode(line.ID)
		if err != nil {
			return err
		}
		if err := validateBenchmarkDeclaration(want, got); err != nil {
			return fmt.Errorf("existing benchmark node %q: %w", line.ID, err)
		}
	}
	return nil
}

func (r *runner) benchmarkNode(node string) (model.Node, error) {
	name := "benchmark-resume-show-" + node
	res, err := r.capture(name, "graph status", r.root, r.cfg.Controller, []string{"graph", "show", "--plan", benchmarkPlan, node, "--json"}, 5*time.Minute, true)
	if err != nil {
		return model.Node{}, err
	}
	if err := r.recordControllerOutput(name, res.Stdout, []byte(res.Stderr)); err != nil {
		return model.Node{}, err
	}
	if res.ExitCode != 0 {
		return model.Node{}, fmt.Errorf("%s exit %d: %s", name, res.ExitCode, res.Stderr)
	}
	var out struct {
		Node map[string]json.RawMessage `json:"node"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return model.Node{}, err
	}
	for _, key := range []string{"claim", "verification", "red_seqs", "red_evidence", "consumed_attempts"} {
		if rawJSONPresent(out.Node[key]) {
			return model.Node{}, fmt.Errorf("existing benchmark node %q has %s history; refusing reuse", node, key)
		}
	}
	for _, key := range []string{"intent_hashes", "input_hashes", "claim", "verification", "red_seqs", "red_evidence", "consumed_attempts"} {
		delete(out.Node, key)
	}
	payload, err := json.Marshal(map[string]any{"version": 1, "nodes": []map[string]json.RawMessage{out.Node}})
	if err != nil {
		return model.Node{}, err
	}
	proposal, err := decodeHistoricalProposal(payload)
	if err != nil {
		return model.Node{}, fmt.Errorf("decode existing historical benchmark node %q declaration: %w", node, err)
	}
	if len(proposal.Nodes) != 1 {
		return model.Node{}, fmt.Errorf("existing benchmark node %q did not decode uniquely", node)
	}
	return proposal.Nodes[0], nil
}

// decodeHistoricalProposal reads the archived observed-v1 declaration as
// strict stored graph data. It is only for comparing this historical exercise
// archive; production authoring must continue to use DecodeProposal, which
// correctly refuses observed-v1.
func decodeHistoricalProposal(data []byte) (*model.Proposal, error) {
	graph, err := model.DecodeGraph(data)
	if err != nil {
		return nil, err
	}
	return &model.Proposal{Version: graph.Version, Nodes: graph.Nodes}, nil
}

func rawJSONPresent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "{}" && trimmed != "[]"
}

func validateBenchmarkDeclaration(want, got model.Node) error {
	if want.ID != got.ID || want.Contract != got.Contract || want.Estimate != got.Estimate || want.Phase != got.Phase || want.Role != got.Role ||
		!equalStringSlices(want.Justifies, got.Justifies) || !equalStringSlices(want.Deps, got.Deps) || !reflect.DeepEqual(want.Gate, got.Gate) ||
		!reflect.DeepEqual(want.Inputs, got.Inputs) || !equalStringSlices([]string(want.Hazards), []string(got.Hazards)) || len(got.Artifacts) != 0 {
		return errors.New("contract, coverage, profile, inputs, hazards, or artifact declaration differs")
	}
	return nil
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *runner) realPlanComparisonState() (string, error) {
	status, err := r.graphStatus(planName, "compare-real-plan-status")
	if err != nil {
		return "", err
	}
	if len(status.Nodes) > 0 && status.Closed == len(status.Nodes) {
		return "closed-by-cli", nil
	}
	green, reviewReady := 0, false
	for _, node := range status.Nodes {
		if node.State == "GREEN" {
			green++
		}
		if node.ID == "feature-review" && node.State == "READY" {
			reviewReady = true
		}
	}
	if green == 3 && reviewReady {
		return "explicit-review-boundary-not-closed", nil
	}
	return "", fmt.Errorf("real feature plan is neither CLI-closed nor at the explicit review boundary: states=%v closed=%d", status.States, status.Closed)
}

func (r *runner) targetBinding(s summary) (targetBinding, error) {
	binding := targetBinding{PrimaryMatches: true, SourceMatches: true, PrimaryStale: map[string]string{}, SourceStale: map[string]string{}}
	for rel, want := range s.SourceHashes {
		for _, check := range []struct {
			root    string
			matches *bool
			stale   map[string]string
		}{{r.root, &binding.PrimaryMatches, binding.PrimaryStale}, {r.cfg.Source, &binding.SourceMatches, binding.SourceStale}} {
			got, err := hashFile(filepath.Join(check.root, filepath.FromSlash(rel)))
			if os.IsNotExist(err) {
				*check.matches = false
				check.stale[rel] = "missing"
				continue
			}
			if err != nil {
				return binding, err
			}
			if got != want {
				*check.matches = false
				check.stale[rel] = got
			}
		}
	}
	return binding, nil
}

func controllerIdentities(baseline, candidate string) (map[string]string, error) {
	out := map[string]string{}
	for name, path := range map[string]string{"baseline": baseline, "candidate": candidate} {
		hash, err := hashFile(path)
		if err != nil {
			return nil, fmt.Errorf("hash %s controller: %w", name, err)
		}
		out[name] = hash
	}
	if out["baseline"] == out["candidate"] {
		return nil, errors.New("baseline and candidate controller binaries are identical")
	}
	return out, nil
}

func validateCheckMetrics(metrics []metric) error {
	if len(metrics) != 6 {
		return fmt.Errorf("read-only comparison produced %d metrics, want 6", len(metrics))
	}
	for _, m := range metrics {
		if m.Exit != 0 || m.Counters["profile_resolutions"] == 0 || m.Counters["input_resolutions"] == 0 {
			return fmt.Errorf("%s did not complete a full current check", m.Operation)
		}
		wantReads := int64(4)
		if strings.Contains(m.Operation, "candidate") {
			wantReads = 3
		}
		if got := m.Counters["bundle_read_requests"]; got != wantReads {
			return fmt.Errorf("%s bundle_read_requests=%d, want %d", m.Operation, got, wantReads)
		}
	}
	return nil
}

func validateReadonlyState(before, after string) error {
	if before == "" || after == "" {
		return errors.New("read-only comparison snapshots must be nonempty")
	}
	if before != after {
		return errors.New("read-only comparison changed source or graph state")
	}
	return nil
}

func validateSyncOutcomes(outcomes []syncOutcome) error {
	if len(outcomes) != 2 {
		return fmt.Errorf("sync comparison produced %d outcomes, want 2", len(outcomes))
	}
	for _, outcome := range outcomes {
		if !outcome.Recorded || outcome.Historical || !outcome.AnchorSnapshot || outcome.Result != "pass" || outcome.Sequence <= 0 {
			return fmt.Errorf("%s sync was not a fresh recorded pass: %+v", outcome.Controller, outcome)
		}
		if outcome.Metric.Exit != 0 || outcome.Metric.Counters["profile_resolutions"] == 0 || outcome.Metric.Counters["input_resolutions"] == 0 || outcome.Metric.Counters["historical_replays"] != 0 {
			return fmt.Errorf("%s sync did not complete a full nonhistorical admission", outcome.Controller)
		}
		wantReads, wantLegacy := int64(8), int64(1)
		if outcome.Controller == "candidate" {
			wantReads, wantLegacy = 6, 0
		}
		if got := outcome.Metric.Counters["bundle_read_requests"]; got != wantReads {
			return fmt.Errorf("%s sync bundle_read_requests=%d, want %d", outcome.Controller, got, wantReads)
		}
		if got := outcome.Metric.Counters["legacy_anchor_scans"]; got != wantLegacy {
			return fmt.Errorf("%s sync legacy_anchor_scans=%d, want %d", outcome.Controller, got, wantLegacy)
		}
	}
	if outcomes[1].Sequence <= outcomes[0].Sequence {
		return fmt.Errorf("sync graph sequence did not advance: %d then %d", outcomes[0].Sequence, outcomes[1].Sequence)
	}
	return nil
}

func validateAttemptIdentities(attempts map[string]benchmarkAttempt) error {
	baseline, bok := attempts["baseline"]
	candidate, cok := attempts["candidate"]
	if !bok || !cok {
		return errors.New("benchmark requires baseline and candidate attempts")
	}
	for controller, attempt := range attempts {
		if attempt.AttemptID == "" || attempt.ClaimInstance == "" || attempt.WorkspaceDigest == "" {
			return fmt.Errorf("%s attempt has incomplete live-claim identity", controller)
		}
	}
	if baseline.AttemptID == candidate.AttemptID || baseline.ClaimInstance == candidate.ClaimInstance || baseline.WorkspaceDigest == candidate.WorkspaceDigest {
		return errors.New("benchmark controllers must use distinct attempts, claim nonces, and workspaces")
	}
	return nil
}

func (r *runner) writePlanning() error {
	if err := os.MkdirAll(filepath.Join(r.root, ".exercise-plans"), 0o755); err != nil {
		return err
	}
	config := `{"planningRoot":".exercise-plans","planMapping":{"` + planName + `":"exercise","` + benchmarkPlan + `":"exercise"},"repositories":{"exercise":{"path":"."}},"graphLeaseTtlMinutes":120}` + "\n"
	if err := os.WriteFile(filepath.Join(r.root, "planning-config.json"), []byte(config), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.private, "spec-proposal.md"), []byte(primarySpecProposal()), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.private, "plan-proposal.md"), []byte(primaryPlanProposal()), 0o600)
}

func primaryArtifactPaths() (string, string) {
	return filepath.ToSlash(filepath.Join(".exercise-plans", "Specs", "EvidenceCost", "README.md")),
		filepath.ToSlash(filepath.Join(".exercise-plans", "Plans", planName, "README.md"))
}

func benchmarkArtifactPath() string {
	return filepath.ToSlash(filepath.Join(".exercise-plans", "Plans", benchmarkPlan, "README.md"))
}

func primaryLifecycleSteps() []lifecycleStep {
	specPath, planPath := primaryArtifactPaths()
	return []lifecycleStep{
		{Name: "spec-submit", Stage: "before-graph", Args: []string{"spec", "submit", specPath, "--json"}},
		{Name: "spec-approve", Stage: "before-graph", Args: []string{"spec", "approve", specPath, "--json"}},
		{Name: "plan-approve", Stage: "after-compile", Args: []string{"plan", "approve", planPath, "--json"}},
		{Name: "plan-activate", Stage: "after-compile", Args: []string{"plan", "activate", planPath, "--json"}},
	}
}

func primarySpecProposal() string {
	return `---
title: "Evidence Cost Real Feature"
tags: []
related: []
supersedes: ""
superseded_by: ""
implemented_in: ""
waivers: []
---

# Evidence Cost Real Feature

## Overview
Exercise anonymous --cost attribution on real observed evidence commands.

## Goals
- Preserve command validity while reporting anonymous measured costs.

## Non-Goals
- Optimization or performance claims.

## Requirements

### Functional Requirements
- **FR-01**: test run, test check, and graph sync optionally report anonymous measured cost without persisting it.

### Non-Functional Requirements
- **NFR-01**: the exercise uses hermetic cached Go dependencies and real Git-isolated claims.

## User Stories
- As a maintainer, I want measured evidence costs so that later optimization is evidence based.

## Acceptance Criteria
- [ ] **AC-01**: independent test and sync contracts fail without their CLI flags and pass with the source feature bytes.
- [ ] **AC-02**: integrated real-Go capture preserves refusals, replay, and RED behavior.

## Constraints
- Stop at the review boundary.

## Dependencies
- Cached Go module dependencies.

## Open Questions
None.
`
}

func primaryPlanProposal() string {
	return planProposal("Evidence Cost Real Feature", "Real-feature graph integration exercise.", "Two independent CLI leaves integrate before characterization coverage and a full review gate.", "Cost is observational and is not persisted.")
}

func benchmarkPlanProposal() string {
	return planProposal("Generated Evidence Cost Benchmark", "Generate equivalent live claims for read-only controller checks.", "Two equivalent test nodes retain independently captured attempts.", "Each controller checks only the attempt it actually generated.")
}

func benchmarkProposal(baseInputs []map[string]string) map[string]any {
	inputs := append([]map[string]string(nil), baseInputs...)
	for _, required := range []string{"cmd/sdd/cost_integration_test.go", "cmd/sdd/root_test.go", "cmd/sdd/test_command_test.go"} {
		found := false
		for _, input := range inputs {
			if input["root"] == "repository" && input["path"] == required {
				found = true
				break
			}
		}
		if !found {
			inputs = append(inputs, map[string]string{"root": "repository", "path": required})
		}
	}
	supportKeys := []string{`{"root":"repository","path":"cmd/sdd/root_test.go"}`, `{"root":"repository","path":"cmd/sdd/test_command_test.go"}`}
	tests := []map[string]any{
		{"id": "TestCostIntegrationRealGoCaptureRefusalsAndReplay", "file": "cmd/sdd/cost_integration_test.go"},
		{"id": "TestCostIntegrationRealGoRedCapture", "file": "cmd/sdd/cost_integration_test.go"},
		{"id": "TestGraphSyncCostJSONPreservesRecordedResultOnReleaseError", "file": "cmd/sdd/cost_integration_test.go"},
		{"id": "TestGraphSyncCostJSONDoesNotHideWriterFailure", "file": "cmd/sdd/cost_integration_test.go"},
	}
	nodes := make([]map[string]any, 0, 3)
	for _, controller := range []string{"baseline", "candidate"} {
		nodes = append(nodes, map[string]any{
			"id":        "benchmark-" + controller,
			"contract":  "run and admit the four real integration contracts against the frozen target with the " + controller + " controller",
			"justifies": []string{"FR-01", "AC-01", "AC-02", "NFR-01"},
			"deps":      []string{},
			"inputs":    inputs,
			"gate": map[string]any{
				"type":      "tests",
				"evidence":  "observed-v1",
				"execution": map[string]any{"adapter": "go-test-v1", "timeout_seconds": 900, "environment_keys": []string{"GOMODCACHE", "GOPROXY", "GOSUMDB"}, "test_support_inputs": supportKeys},
				"tests":     tests,
			},
			"hazards":   []string{},
			"artifacts": []string{},
			"estimate":  1,
			"phase":     "01-generated-benchmark",
		})
	}
	nodes = append(nodes, map[string]any{
		"id": "benchmark-review", "contract": "independently review the generated benchmark work without manufacturing a pass", "justifies": []string{"FR-01", "AC-01", "AC-02", "NFR-01"},
		"deps": []string{"benchmark-baseline", "benchmark-candidate"}, "gate": map[string]any{"type": "review", "lanes": "full"}, "hazards": []string{}, "artifacts": []string{}, "estimate": 1, "phase": "01-generated-benchmark",
	})
	return map[string]any{"version": 1, "nodes": nodes}
}

func planProposal(title, overview, architecture, decision string) string {
	return fmt.Sprintf(`---
title: %q
tags: []
related:
  - Specs/EvidenceCost
phases: []
waivers: []
---

# %s

## Overview
%s

## Non-Goals
Review completion, optimization, fabricated hazards, or statistical claims.

## Architecture
%s

## Key Decisions
%s

## Dependencies
The corrected real-feature exercise source and cached Go dependencies.

## Plan Completion Evidence
Pending — not complete.

## Open Questions
None.
`, title, title, overview, architecture, decision)
}

func (r *runner) applyLifecycle() error {
	specPath, planPath := primaryArtifactPaths()
	for _, item := range []struct {
		name, target, proposal, artifactType string
	}{
		{"apply-spec-proposal", specPath, filepath.Join(r.private, "spec-proposal.md"), "spec"},
		{"apply-plan-proposal", planPath, filepath.Join(r.private, "plan-proposal.md"), "plan"},
	} {
		payload, err := os.ReadFile(item.proposal)
		if err != nil {
			return err
		}
		if err := r.controllerInput(item.name, payload, applyCommandArgs(item.target, item.artifactType)...); err != nil {
			return err
		}
	}
	for _, step := range primaryLifecycleSteps() {
		if step.Stage != "before-graph" {
			continue
		}
		if err := r.controller(step.Name, step.Args...); err != nil {
			return err
		}
	}
	return nil
}

func applyCommandArgs(target, artifactType string) []string {
	return []string{"apply", target, "--create", "--type", artifactType, "--json"}
}

func (r *runner) activatePrimaryPlan() error {
	for _, step := range primaryLifecycleSteps() {
		if step.Stage != "after-compile" {
			continue
		}
		if err := r.controller(step.Name, step.Args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) controllerInput(name string, stdin []byte, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.cfg.Controller, args...)
	cmd.Dir = r.root
	cmd.Env = r.env
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			return fmt.Errorf("%s could not run: %w", name, err)
		}
	}
	r.ops = append(r.ops, operation{"artifact lifecycle", name, exit, time.Since(start).Milliseconds()})
	if saveErr := r.recordControllerOutput(name, stdout.Bytes(), stderr.Bytes()); saveErr != nil {
		return saveErr
	}
	if exit != 0 {
		return fmt.Errorf("%s exit %d\nstdout:\n%s\nstderr:\n%s", name, exit, stdout.String(), stderr.String())
	}
	return nil
}

func (r *runner) graphLifecycle() (string, error) {
	res, err := r.capture("graph-status", "graph status", r.root, r.cfg.Controller, []string{"graph", "status", "--plan", planName, "--json"}, 5*time.Minute, true)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("graph status exit %d: %s", res.ExitCode, res.Stderr)
	}
	var status struct {
		Nodes []graphNodeStatus `json:"nodes"`
	}
	if err := json.Unmarshal(res.Stdout, &status); err != nil {
		return "", fmt.Errorf("decode graph status: %w", err)
	}
	green, ready := 0, false
	for _, node := range status.Nodes {
		if node.State == "GREEN" {
			green++
		}
		if node.ID == "feature-review" && node.State == "READY" {
			ready = true
		}
	}
	r.status = status.Nodes
	if green != 3 || !ready {
		return "", fmt.Errorf("unexpected graph status: %d GREEN work nodes, feature-review READY=%t", green, ready)
	}
	return "3-work-nodes-GREEN;feature-review-READY", nil
}

func (r *runner) writeProposal(tracked []string) (string, map[string][]string, error) {
	var production, support []string
	for _, p := range tracked {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			production = append(production, p)
		}
		if p == "cmd/sdd/root_test.go" || p == "cmd/sdd/test_command_test.go" {
			support = append(support, p)
		}
	}
	for _, p := range foundationOverlays {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			production = append(production, p)
		}
	}
	production = uniqueSorted(production)
	inputs := make([]map[string]string, 0, len(production)+4)
	for _, p := range append(append([]string{}, production...), "go.mod", "go.sum") {
		inputs = append(inputs, map[string]string{"root": "repository", "path": p})
	}
	for _, p := range support {
		inputs = append(inputs, map[string]string{"root": "repository", "path": p})
	}
	if err := writeJSONFile(filepath.Join(r.cfg.WorkRoot, "inputs.json"), inputs); err != nil {
		return "", nil, err
	}
	supportKeys := []string{`{"root":"repository","path":"cmd/sdd/root_test.go"}`, `{"root":"repository","path":"cmd/sdd/test_command_test.go"}`}
	node := func(id, contract string, deps, artifacts []string, tests []map[string]string) map[string]any {
		return map[string]any{"id": id, "contract": contract, "justifies": []string{"FR-01", "AC-01"}, "deps": deps, "inputs": inputs, "gate": map[string]any{"type": "tests", "evidence": "observed-v1", "execution": map[string]any{"adapter": "go-test-v1", "timeout_seconds": 900, "environment_keys": []string{"GOMODCACHE", "GOPROXY", "GOSUMDB"}, "test_support_inputs": supportKeys}, "tests": tests}, "hazards": []string{}, "artifacts": artifacts, "estimate": 1, "phase": "01-real-feature"}
	}
	nodes := []map[string]any{
		node("test-cost", "test run and check expose optional anonymous cost output without changing evidence validity", nil, []string{"cmd/sdd/test.go", "cmd/sdd/test_cost_test.go"}, []map[string]string{{"id": "TestTestCostOutputContract", "file": "cmd/sdd/test_cost_test.go"}, {"id": "TestTestCostIncompleteCaptureRetainsAttemptID", "file": "cmd/sdd/test_cost_test.go"}}),
		node("sync-cost", "graph sync exposes optional anonymous cost output without changing admission or replay", nil, []string{"cmd/sdd/graph.go", "cmd/sdd/graph_cost_test.go"}, []map[string]string{{"id": "TestGraphSyncCostOutputContract", "file": "cmd/sdd/graph_cost_test.go"}}),
		node("integration-cost", "both cost surfaces preserve real Go capture refusals, replay, and RED capture", []string{"test-cost", "sync-cost"}, []string{"cmd/sdd/cost_integration_test.go"}, []map[string]string{{"id": "TestCostIntegrationRealGoCaptureRefusalsAndReplay", "file": "cmd/sdd/cost_integration_test.go"}, {"id": "TestCostIntegrationRealGoRedCapture", "file": "cmd/sdd/cost_integration_test.go"}, {"id": "TestGraphSyncCostJSONPreservesRecordedResultOnReleaseError", "file": "cmd/sdd/cost_integration_test.go"}, {"id": "TestGraphSyncCostJSONDoesNotHideWriterFailure", "file": "cmd/sdd/cost_integration_test.go"}}),
		{"id": "feature-review", "contract": "the integrated --cost feature survives independent full review", "justifies": []string{"AC-01", "AC-02", "NFR-01"}, "deps": []string{"integration-cost"}, "gate": map[string]any{"type": "review", "lanes": "full"}, "hazards": []string{}, "estimate": 1, "phase": "01-real-feature"},
	}
	p := filepath.Join(r.cfg.WorkRoot, "proposal.json")
	if err := writeJSONFile(p, map[string]any{"version": 1, "nodes": nodes}); err != nil {
		return "", nil, err
	}
	return p, map[string][]string{"base_archive": tracked, "foundation_overlay": foundationOverlays, "leaf_overlays": {"cmd/sdd/test.go", "cmd/sdd/test_cost_test.go", "cmd/sdd/graph.go", "cmd/sdd/graph_cost_test.go"}, "integration_overlay": {"cmd/sdd/cost_integration_test.go"}, "declared_production_readset": production, "test_support_inputs": support}, nil
}

func (r *runner) claim(node string) (string, error) {
	return r.claimPlan(planName, node)
}

func (r *runner) claimPlan(plan, node string) (string, error) {
	res, err := r.capture("claim-"+node, "claim", r.root, r.cfg.Controller, []string{"next", "--plan", plan, "--claim", "--node", node, "--by", identity, "--json"}, 2*time.Minute, true)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("claim %s exit %d: %s", node, res.ExitCode, res.Stderr)
	}
	var out struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return "", err
	}
	if out.Workspace == "" {
		return "", errors.New("claim did not allocate an isolated workspace")
	}
	if filepath.IsAbs(out.Workspace) {
		return out.Workspace, nil
	}
	return filepath.Join(r.root, filepath.FromSlash(out.Workspace)), nil
}

func (r *runner) claimInstance(node string) (string, error) {
	name := "benchmark-claim-identity-" + node
	res, err := r.capture(name, "graph status", r.root, r.cfg.Controller, []string{"graph", "show", "--plan", benchmarkPlan, node, "--json"}, 5*time.Minute, true)
	if err != nil {
		return "", err
	}
	if err := r.recordControllerOutput(name, res.Stdout, []byte(res.Stderr)); err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("%s exit %d: %s", name, res.ExitCode, res.Stderr)
	}
	var out struct {
		Node struct {
			Claim *struct {
				Instance string `json:"instance"`
			} `json:"claim"`
		} `json:"node"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return "", err
	}
	if out.Node.Claim == nil || out.Node.Claim.Instance == "" {
		return "", errors.New("benchmark claim has no instance identity")
	}
	return out.Node.Claim.Instance, nil
}

func (r *runner) benchmarkSync(controller string, attempt benchmarkAttempt) (syncOutcome, error) {
	name := "benchmark-" + controller + "-fresh-sync"
	args := []string{"graph", "sync", "--plan", benchmarkPlan, "--node", attempt.Node, "--by", identity, "--attempt", attempt.AttemptID, "--cost", "--json"}
	res, err := r.capture(name, "admission and rechecks", r.root, r.cfg.Controller, args, 20*time.Minute, true)
	if err != nil {
		return syncOutcome{}, err
	}
	if err := os.WriteFile(filepath.Join(r.private, safeFilename(name)+".stdout.json"), res.Stdout, 0o600); err != nil {
		return syncOutcome{}, err
	}
	if err := os.WriteFile(filepath.Join(r.private, safeFilename(name)+".stderr.txt"), []byte(res.Stderr), 0o600); err != nil {
		return syncOutcome{}, err
	}
	var envelope struct {
		Recorded       bool   `json:"recorded"`
		Historical     bool   `json:"historical"`
		AnchorSnapshot bool   `json:"anchor_snapshot"`
		AttemptID      string `json:"attempt_id"`
		Observation    *struct {
			Result string `json:"result"`
			Seq    int    `json:"seq"`
		} `json:"observation"`
		Cost *costPayload `json:"cost"`
	}
	if err := json.Unmarshal(res.Stdout, &envelope); err != nil {
		return syncOutcome{}, fmt.Errorf("%s returned non-JSON output: %w", name, err)
	}
	if res.ExitCode != 0 || envelope.Cost == nil || envelope.Observation == nil {
		return syncOutcome{}, fmt.Errorf("%s exit=%d recorded=%t historical=%t: %s", name, res.ExitCode, envelope.Recorded, envelope.Historical, res.Stderr)
	}
	m := metric{name, res.ExitCode, envelope.Cost.TotalNS, envelope.Cost.PhaseNS, envelope.Cost.Counters}
	r.metrics = append(r.metrics, m)
	return syncOutcome{Controller: controller, Node: attempt.Node, AttemptID: envelope.AttemptID, Recorded: envelope.Recorded, Historical: envelope.Historical, AnchorSnapshot: envelope.AnchorSnapshot, Result: envelope.Observation.Result, Sequence: envelope.Observation.Seq, Metric: m}, nil
}

func (r *runner) graphStatus(plan, name string) (graphStatusEnvelope, error) {
	res, err := r.capture(name, "graph status", r.root, r.cfg.Controller, []string{"graph", "status", "--plan", plan, "--json"}, 5*time.Minute, true)
	if err != nil {
		return graphStatusEnvelope{}, err
	}
	if err := r.recordControllerOutput(name, res.Stdout, []byte(res.Stderr)); err != nil {
		return graphStatusEnvelope{}, err
	}
	if res.ExitCode != 0 {
		return graphStatusEnvelope{}, fmt.Errorf("%s exit %d: %s", name, res.ExitCode, res.Stderr)
	}
	var status graphStatusEnvelope
	if err := json.Unmarshal(res.Stdout, &status); err != nil {
		return status, err
	}
	return status, nil
}

func (r *runner) cleanupBenchmarkClaims() error {
	status, err := r.graphStatus(benchmarkPlan, "benchmark-pre-cleanup-status")
	if err != nil {
		return err
	}
	for _, node := range status.Nodes {
		if node.Claimed == "" {
			continue
		}
		if err := r.controller("benchmark-release-"+node.ID, "graph", "release", node.ID, "--plan", benchmarkPlan, "--by", identity, "--json"); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) benchmarkCompletionState() (string, error) {
	status, err := r.graphStatus(benchmarkPlan, "benchmark-final-status")
	if err != nil {
		return "", err
	}
	green, reviewReady := 0, false
	for _, node := range status.Nodes {
		if strings.HasPrefix(node.ID, "benchmark-") && node.ID != "benchmark-review" && node.State == "GREEN" && !node.Closed {
			green++
		}
		if node.ID == "benchmark-review" && node.State == "READY" && !node.Closed {
			reviewReady = true
		}
	}
	if green != 2 || !reviewReady || status.Closed != 0 {
		return "", fmt.Errorf("unexpected benchmark final state: green-work=%d review-ready=%t closed=%d", green, reviewReady, status.Closed)
	}
	return "2-work-nodes-GREEN-not-closed;benchmark-review-READY-unrun", nil
}

func (r *runner) assertBenchmarkClaimsLive() error {
	status, err := r.graphStatus(benchmarkPlan, "benchmark-claim-status")
	if err != nil {
		return err
	}
	return validateLiveBenchmarkClaims(status)
}

func validateLiveBenchmarkClaims(status graphStatusEnvelope) error {
	live := map[string]bool{}
	for _, node := range status.Nodes {
		if node.Claimed == identity {
			live[node.ID] = true
		} else if node.Claimed != "" {
			return fmt.Errorf("benchmark node %q is claimed by unexpected identity %q", node.ID, node.Claimed)
		}
	}
	if !live["benchmark-baseline"] || !live["benchmark-candidate"] {
		return errors.New("benchmark attempts are not both under live claims")
	}
	return nil
}

func (r *runner) comparisonSnapshot() (string, error) {
	h := sha256.New()
	for _, rel := range []string{"cmd/sdd", "internal", "go.mod", "go.sum", filepath.ToSlash(filepath.Join(".exercise-plans", "Plans", benchmarkPlan))} {
		path := filepath.Join(r.root, filepath.FromSlash(rel))
		st, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !st.IsDir() {
			b, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
			h.Write(b)
			continue
		}
		err = filepath.Walk(path, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("snapshot refuses symlink %s", p)
			}
			relPath, err := filepath.Rel(r.root, p)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00", filepath.ToSlash(relPath))
			h.Write(b)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func (r *runner) integrate(workspace string) error {
	branch, err := r.gitOutput(workspace, "branch", "--show-current")
	if err != nil {
		return err
	}
	if branch == "" {
		return errors.New("claimed workspace has no branch")
	}
	if err := r.git(workspace, "rebase", "master"); err != nil {
		return err
	}
	return r.git(r.root, "merge", "--ff-only", branch)
}

func (r *runner) costSync(name, node, attempt string, want int) error {
	_, _, err := r.costCommand(name, want, "graph", "sync", "--plan", planName, "--node", node, "--by", identity, "--attempt", attempt, "--cost", "--json")
	return err
}

func (r *runner) costCommand(name string, want int, args ...string) (int, string, error) {
	res, err := r.capture(name, category(args), r.root, r.cfg.Controller, args, 20*time.Minute, true)
	if err != nil {
		return -1, "", err
	}
	if err := os.WriteFile(filepath.Join(r.private, safeFilename(name)+".stdout.json"), res.Stdout, 0o600); err != nil {
		return -1, "", err
	}
	if err := os.WriteFile(filepath.Join(r.private, safeFilename(name)+".stderr.txt"), []byte(res.Stderr), 0o600); err != nil {
		return -1, "", err
	}
	var envelope struct {
		AttemptID string `json:"attempt_id"`
		Cost      *struct {
			TotalNS  int64            `json:"total_ns"`
			PhaseNS  map[string]int64 `json:"phase_ns"`
			Counters map[string]int64 `json:"counters"`
		} `json:"cost"`
	}
	if err := json.Unmarshal(res.Stdout, &envelope); err != nil {
		return res.ExitCode, "", fmt.Errorf("%s returned non-JSON output: %w", name, err)
	}
	if envelope.Cost == nil {
		return res.ExitCode, envelope.AttemptID, fmt.Errorf("%s returned no cost object", name)
	}
	r.metrics = append(r.metrics, metric{name, res.ExitCode, envelope.Cost.TotalNS, envelope.Cost.PhaseNS, envelope.Cost.Counters})
	if res.ExitCode != want {
		return res.ExitCode, envelope.AttemptID, fmt.Errorf("%s exit %d, want %d: %s", name, res.ExitCode, want, res.Stderr)
	}
	if strings.Contains(string(res.Stdout), r.cfg.WorkRoot) || strings.Contains(string(res.Stdout), r.cfg.Source) {
		return res.ExitCode, envelope.AttemptID, fmt.Errorf("%s cost output leaked a machine path", name)
	}
	return res.ExitCode, envelope.AttemptID, nil
}

func (r *runner) controller(name string, args ...string) error {
	res, err := r.capture(name, category(args), r.root, r.cfg.Controller, args, 5*time.Minute, true)
	if err != nil {
		return err
	}
	if err := r.recordControllerOutput(name, res.Stdout, []byte(res.Stderr)); err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s exit %d\nstdout:\n%s\nstderr:\n%s", name, res.ExitCode, res.Stdout, res.Stderr)
	}
	return nil
}

func (r *runner) recordControllerOutput(name string, stdout, stderr []byte) error {
	base := filepath.Join(r.private, safeFilename(name))
	if err := os.WriteFile(base+".controller.stdout", stdout, 0o600); err != nil {
		return err
	}
	return os.WriteFile(base+".controller.stderr", stderr, 0o600)
}

func (r *runner) overlay(dest string, paths []string) error {
	for _, rel := range paths {
		if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.ContainsAny(rel, ":\x00") || strings.Contains(filepath.ToSlash(rel), "../") {
			return fmt.Errorf("unsafe overlay %q", rel)
		}
		source := filepath.Join(r.cfg.Source, filepath.FromSlash(rel))
		info, err := os.Lstat(source)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("overlay source is not a regular file: %s", rel)
		}
		b, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) git(dir string, args ...string) error {
	out, err := r.gitCapture(dir, args...)
	if err != nil {
		return err
	}
	if out.ExitCode != 0 {
		return fmt.Errorf("git %s exit %d: %s", strings.Join(args, " "), out.ExitCode, out.Stderr)
	}
	return nil
}
func (r *runner) gitOutput(dir string, args ...string) (string, error) {
	out, err := r.gitCapture(dir, args...)
	if err != nil {
		return "", err
	}
	if out.ExitCode != 0 {
		return "", fmt.Errorf("git %s exit %d: %s", strings.Join(args, " "), out.ExitCode, out.Stderr)
	}
	return strings.TrimSpace(string(out.Stdout)), nil
}
func (r *runner) gitCapture(dir string, args ...string) (procexec.Result, error) {
	prefix := []string{"-c", "user.name=Evidence Cost Exercise", "-c", "user.email=exercise.invalid", "-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "-c", "core.hooksPath=" + nullDevice(), "-c", "core.autocrlf=false"}
	return r.capture("git-"+safeFilename(strings.Join(args, "-")), gitCategory(args), dir, "git", append(prefix, args...), 5*time.Minute, true)
}
func (r *runner) commitAll(dir, message string) error {
	if err := r.git(dir, "add", "--all"); err != nil {
		return err
	}
	return r.git(dir, "commit", "-m", message)
}

func (r *runner) capture(name, cat, dir, exe string, args []string, timeout time.Duration, record bool) (procexec.Result, error) {
	start := time.Now()
	res, err := procexec.Capture(context.Background(), exe, args, procexec.Policy{Dir: dir, Env: r.env, Timeout: timeout, Cleanup: 10 * time.Second, DiagnosticLimit: 256 << 10, MachineLimit: 128 << 20})
	elapsed := time.Since(start)
	if record {
		r.ops = append(r.ops, operation{cat, name, res.ExitCode, elapsed.Milliseconds()})
	}
	if err != nil {
		return res, fmt.Errorf("%s could not run: %w", name, err)
	}
	return res, nil
}

func extractSafeTar(raw []byte, root string) error {
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader || h.Typeflag == tar.TypeXHeader {
			continue
		}
		if h.Name == "" || strings.ContainsAny(h.Name, "\\:\x00") || filepath.VolumeName(h.Name) != "" {
			return fmt.Errorf("unsafe archive name %q", h.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(h.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive name %q", h.Name)
		}
		target := filepath.Join(root, clean)
		if !within(target, root) || samePath(target, root) {
			return fmt.Errorf("archive escape %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(h.Mode)&0o777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported archive entry %q type %d", h.Name, h.Typeflag)
		}
	}
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}
func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
func assertSame(a, b string) error {
	x, e := os.ReadFile(a)
	if e != nil {
		return e
	}
	y, e := os.ReadFile(b)
	if e != nil {
		return e
	}
	if !bytes.Equal(x, y) {
		return fmt.Errorf("selected test bytes differ: %s", filepath.Base(a))
	}
	return nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digest(b), nil
}
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
func nonemptyLines(s string) []string {
	var out []string
	for _, v := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, filepath.ToSlash(v))
		}
	}
	sort.Strings(out)
	return out
}
func selectSourcePaths(all []string) []string {
	var out []string
	for _, p := range all {
		p = filepath.ToSlash(p)
		if p == "go.mod" || p == "go.sum" || strings.HasPrefix(p, "cmd/sdd/") || strings.HasPrefix(p, "internal/") {
			out = append(out, p)
		}
	}
	return uniqueSorted(out)
}
func category(args []string) string {
	if len(args) >= 2 && args[0] == "test" && args[1] == "run" {
		return "Go test execution and profiles"
	}
	if len(args) >= 2 && args[0] == "test" && args[1] == "check" {
		return "hashes and evidence checks"
	}
	if len(args) >= 2 && args[0] == "graph" && args[1] == "sync" {
		return "admission and rechecks"
	}
	if len(args) > 0 && args[0] == "next" {
		return "claims"
	}
	return "graph construction"
}
func gitCategory(args []string) string {
	if len(args) > 0 {
		switch args[0] {
		case "commit":
			return "Git commit"
		case "rebase":
			return "Git rebase"
		case "merge":
			return "Git fast-forward"
		}
	}
	return "Git setup/query"
}
func safeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}
func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}
func within(path, root string) bool {
	p, _ := filepath.Abs(path)
	r, _ := filepath.Abs(root)
	rel, err := filepath.Rel(r, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func canonicalExistingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(physical), nil
}
func canonicalProspectivePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err == nil {
		physical, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", err
		}
		return filepath.Clean(physical), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent, err := canonicalExistingDir(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("parent must already exist: %w", err)
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}
func nullDevice() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}
