package provider

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/claims"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

func gitOK(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitFixture builds a real repo whose planning root is the repo root, with
// an initialized plan graph.
func gitFixture(t *testing.T) (repoRoot, planDir string) {
	t.Helper()
	repoRoot = t.TempDir()
	gitOK(t, repoRoot, "init", "-q")
	gitOK(t, repoRoot, "config", "user.email", "t@example.com")
	gitOK(t, repoRoot, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repoRoot, "planning-config.json"), []byte(`{"planningRoot": "."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	planDir = filepath.Join(repoRoot, "Plans", "SamplePlan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gstore.Init(planDir); err != nil {
		t.Fatal(err)
	}
	gitOK(t, repoRoot, "add", ".")
	gitOK(t, repoRoot, "commit", "-q", "-m", "base")
	return repoRoot, planDir
}

func TestGitProviderWorktreeRoundTrip(t *testing.T) {
	repoRoot, planDir := gitFixture(t)
	p := Detect(repoRoot, planDir)
	if p.Kind() != "git" || p.Capacity() < 2 {
		t.Fatalf("git provider expected with N-way capacity, got %s/%d", p.Kind(), p.Capacity())
	}

	ws, err := p.Allocate("node-a")
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if ws.Handle == "" || filepath.IsAbs(ws.Handle) || strings.Contains(ws.Handle, `\`) {
		t.Fatalf("handle must be a planning-root-relative slash path, got %q", ws.Handle)
	}
	if !strings.HasPrefix(ws.Handle, "Plans/SamplePlan/.graph/ws-") {
		t.Fatalf("worktrees live under the plan's .graph area: %q", ws.Handle)
	}
	if fi, err := os.Stat(ws.Dir); err != nil || !fi.IsDir() {
		t.Fatalf("worktree dir must exist: %v", err)
	}
	// The worktree is branched from merged state: same HEAD as the repo.
	if gitOK(t, ws.Dir, "rev-parse", "HEAD") != gitOK(t, repoRoot, "rev-parse", "HEAD") {
		t.Fatal("worktree must branch from the repo's HEAD")
	}

	prov, err := p.Provenance(ws.Handle)
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if prov.Kind != "git" || prov.Revision != gitOK(t, repoRoot, "rev-parse", "HEAD") || prov.Worktree != ws.Handle {
		t.Fatalf("provenance = %+v", prov)
	}
	if p.Isolation(ws.Handle, 5) != model.IsolationClean {
		t.Fatal("a worktree is clean isolation regardless of other claims")
	}

	// A leftover workspace is never silently reused.
	if _, err := p.Allocate("node-a"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("leftover workspace must refuse: %v", err)
	}

	// The claim gets a BRANCH, not a detached HEAD: commits made in the
	// worktree must stay reachable after release, or merged work would
	// dangle and eventually be garbage-collected.
	if err := os.WriteFile(filepath.Join(ws.Dir, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, ws.Dir, "add", ".")
	gitOK(t, ws.Dir, "commit", "-q", "-m", "node work")
	workCommit := gitOK(t, ws.Dir, "rev-parse", "HEAD")

	if err := p.Release(ws.Handle); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(ws.Dir); !os.IsNotExist(err) {
		t.Fatal("release must remove the worktree")
	}
	if got := gitOK(t, repoRoot, "rev-parse", "graph/node-a"); got != workCommit {
		t.Fatalf("the claim branch must keep the work reachable after release: %s != %s", got, workCommit)
	}
}

func TestGitProviderBacksClaimsEndToEnd(t *testing.T) {
	repoRoot, planDir := gitFixture(t)
	if _, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		g.Nodes = append(g.Nodes,
			model.Node{ID: "a", Contract: "c", Gate: model.Gate{Type: model.GateTests},
				Hazards: model.Hazards{}, Estimate: 1, Artifacts: []string{"src/a.ext"}},
			model.Node{ID: "b", Contract: "c", Gate: model.Gate{Type: model.GateTests},
				Hazards: model.Hazards{}, Estimate: 1, Artifacts: []string{"src/b.ext"}},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p := Detect(repoRoot, planDir)

	// Effective parallelism = min(artifact-disjoint frontier, capacity):
	// with git's N-way capacity, two disjoint nodes claim concurrently.
	first, err := claims.Claim(planDir, claims.Options{By: "a1", Provider: ForClaims(p)})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	second, err := claims.Claim(planDir, claims.Options{By: "a2", Provider: ForClaims(p)})
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if first.Workspace == second.Workspace || first.Workspace == "" {
		t.Fatalf("each claim gets its own worktree: %q vs %q", first.Workspace, second.Workspace)
	}
	g, _ := gstore.Load(gstore.PathFor(planDir))
	if g.NodeByID(first.Node.ID).Claim.Workspace != first.Workspace {
		t.Fatal("the workspace handle must be recorded on the committed claim")
	}
}

// fakeRun scripts the p4 CLI at the runner seam, consistent with how
// internal/vcs tests mock their commands.
func fakeRun(t *testing.T, calls *[]string) runner {
	return func(dir, name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		*calls = append(*calls, cmd)
		switch {
		case strings.HasPrefix(cmd, "p4 changes"):
			return []byte("Change 4321 on 2026/08/31 by user@client *pending* 'graph plan work'\n"), nil
		case strings.HasPrefix(cmd, "p4 opened"):
			return []byte("//depot/src/a.ext#3 - edit change 4321 (text)\n//depot/src/b.ext#7 - add change 4321 (text)\n"), nil
		default:
			t.Fatalf("unexpected command: %s", cmd)
			return nil, nil
		}
	}
}

func TestP4ProviderSharedClientPosture(t *testing.T) {
	var calls []string
	p := &p4Provider{repoRoot: "/depot/ws", run: fakeRun(t, &calls)}

	if p.Capacity() != 1 {
		t.Fatal("p4 runs one shared client: capacity 1")
	}
	ws, err := p.Allocate("node-a")
	if err != nil || ws.Handle != "" || ws.Dir != "/depot/ws" {
		t.Fatalf("p4 allocates the shared tree: %+v %v", ws, err)
	}
	if p.Isolation("", 1) != model.IsolationClean {
		t.Fatal("serial one-CL execution is clean by construction")
	}
	if p.Isolation("", 2) != model.IsolationSharedDirty {
		t.Fatal("two concurrent claimants in one tree are shared-dirty (defensive)")
	}
	prov, err := p.Provenance("")
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if prov.Kind != "p4" || prov.Changelist != "4321" {
		t.Fatalf("provenance must carry the pending CL: %+v", prov)
	}
	if len(prov.OpenedFiles) != 2 || prov.OpenedFiles[0] != "//depot/src/a.ext" {
		t.Fatalf("provenance must carry the opened-file list: %v", prov.OpenedFiles)
	}
}

func TestPlainProviderDigestOnly(t *testing.T) {
	p := &plainProvider{repoRoot: "/work"}
	if p.Kind() != "plain" || p.Capacity() != 1 {
		t.Fatalf("plain posture: %s/%d", p.Kind(), p.Capacity())
	}
	prov, err := p.Provenance("")
	if err != nil || prov != nil {
		t.Fatalf("plain trees carry no provenance (digests anchor everything): %+v %v", prov, err)
	}
	if p.Isolation("", 1) != model.IsolationClean || p.Isolation("", 3) != model.IsolationSharedDirty {
		t.Fatal("plain isolation follows the concurrent-claimant rule")
	}
}
