package sync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// cleanWorkspaceProvider reports a claimed workspace with clean isolation,
// so sync reaches the merge gate's worktree-cleanliness probe — the seam
// under test.
type cleanWorkspaceProvider struct{}

func (cleanWorkspaceProvider) Kind() string  { return "git" }
func (cleanWorkspaceProvider) Capacity() int { return 1 }
func (cleanWorkspaceProvider) Allocate(string) (provider.Workspace, error) {
	return provider.Workspace{}, nil
}
func (cleanWorkspaceProvider) HandleFor(string) string                { return "ws" }
func (cleanWorkspaceProvider) Release(string) error                   { return nil }
func (cleanWorkspaceProvider) PruneMergedBranches() ([]string, error) { return nil, nil }
func (cleanWorkspaceProvider) Isolation(string, int) string           { return model.IsolationClean }
func (cleanWorkspaceProvider) Provenance(string) (*model.Provenance, error) {
	return nil, nil
}

// TestSyncCleanFailureIsOperational (FR-16, AC-08, DD-10): when the merge
// gate's worktree-cleanliness probe cannot run, sync must abort with an
// operational error rather than treat the unprobed workspace as clean and
// record a pass. The pre-fix code tested `cleanErr == nil && !clean`, which
// silently accepted every failed probe.
func TestSyncCleanFailureIsOperational(t *testing.T) {
	gitExe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("SDD_VCS_DISABLE_P4", "1")

	n := testsNode("a", "test_a")
	n.Claim = &model.Claim{By: "holder", Workspace: "ws", LeaseExpires: "2099-01-01T00:00:00Z"}
	planDir, repoRoot := fixture(t, n)

	// The workspace is a real git worktree-shaped directory: a .git marker
	// means the probe MUST consult git and cannot decide structurally.
	wsDir := filepath.Join(repoRoot, "ws")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(gitExe, "-C", wsDir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	report := `<testsuite><testcase name="test_a"/></testsuite>`
	opts := Options{PlanDir: planDir, RepoRoot: repoRoot, Node: "a",
		ReportName: "r.xml", ReportBytes: []byte(report), By: "holder",
		Provider: cleanWorkspaceProvider{}}

	// git cannot run: the cleanliness probe fails and the pass must be
	// refused. (The control — that this same sync records when the probe
	// can answer — is TestSyncRecordsObservationWithAnchors and the merge
	// path in sync_test.go.)
	t.Setenv("PATH", t.TempDir())
	res, err := Run(opts)
	if err == nil {
		t.Fatalf("a failed cleanliness probe recorded an observation instead of failing: %+v", res)
	}
	if !errors.Is(err, vcs.ErrOperational) {
		t.Fatalf("failed Clean() must wrap vcs.ErrOperational; got %v", err)
	}
	if res != nil && res.Recorded {
		t.Fatal("an operational failure must never record an observation")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "workspace") {
		t.Errorf("the error must name the workspace it could not probe: %v", err)
	}

	// And nothing was published to the graph.
	g, lerr := gstore.Load(gstore.PathFor(planDir))
	if lerr != nil {
		t.Fatal(lerr)
	}
	if node := g.NodeByID("a"); node.Verification != nil {
		t.Fatalf("an operational failure published observation %+v", node.Verification)
	}
}
