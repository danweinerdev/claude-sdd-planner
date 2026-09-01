// Package provider supplies per-claim workspaces and observation provenance
// (Designs/SddGraph DD-6, DD-7, DD-8): git gets cheap N-way isolation via
// worktrees, Perforce gets its real-world posture — one shared client, one
// pending changelist, serial execution that is isolation-clean BY
// CONSTRUCTION — and plain trees get digest-only provenance. Parallelism is
// provider capacity, never a correctness assumption: everything holds at
// capacity 1, and the graph never interprets a workspace handle.
//
// This package runs VCS commands that MUTATE working state (worktree add /
// remove), which is exactly what internal/vcs adapters are forbidden to do
// (their read-only contract is NFR-05's). The separate, injectable runner
// here is that boundary made visible, and it doubles as the test seam the
// p4 fixtures mock.
package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/claims"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// Workspace is one allocated working area.
type Workspace struct {
	// Handle is the opaque identifier recorded on the claim. Planning-root-
	// relative for worktrees (a committed graph must never carry a
	// machine-specific absolute path), "" for the shared tree.
	Handle string
	// Dir is where the agent works, absolute.
	Dir string
}

// Provider is the full workspace contract. claims.Provider is its
// scheduling subset; ForClaims adapts.
type Provider interface {
	// Kind names the underlying VCS: git, p4, or plain.
	Kind() string
	// Capacity is how many isolated workspaces this provider sustains.
	Capacity() int
	// Allocate prepares a workspace for one node.
	Allocate(nodeID string) (Workspace, error)
	// Release tears a workspace down (merge or graceful abandonment; lease
	// EXPIRY deliberately never calls this — an expired claimant's
	// workspace is post-mortem evidence).
	Release(handle string) error
	// Isolation classifies an observation produced in the given workspace
	// with activeClaims outstanding (DD-7: clean = merged state plus this
	// node's edits only).
	Isolation(handle string, activeClaims int) string
	// Provenance is the VCS-native reference for an observation made in the
	// workspace — supplementary by design; the digest anchor is what is
	// load-bearing (DD-6). nil for plain trees.
	Provenance(handle string) (*model.Provenance, error)
}

// runner executes one VCS command in a directory — the injectable seam.
type runner func(dir, name string, args ...string) ([]byte, error)

func execRunner(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Detect routes on the repository's detected VCS. planDir hosts the
// gitignored workspace area; repoRoot is where commands run.
func Detect(repoRoot, planDir string) Provider {
	switch vcs.Detect(repoRoot).Kind() {
	case vcs.Git, vcs.GitWorktree:
		return &gitProvider{repoRoot: repoRoot, planDir: planDir, run: execRunner}
	case vcs.Perforce:
		return &p4Provider{repoRoot: repoRoot, run: execRunner}
	default:
		return &plainProvider{repoRoot: repoRoot}
	}
}

// ForClaims adapts a Provider to the scheduling subset claims consumes.
func ForClaims(p Provider) claims.Provider { return claimsAdapter{p} }

type claimsAdapter struct{ p Provider }

func (a claimsAdapter) Capacity() int { return a.p.Capacity() }
func (a claimsAdapter) Allocate(nodeID string) (string, error) {
	ws, err := a.p.Allocate(nodeID)
	return ws.Handle, err
}
func (a claimsAdapter) Release(handle string) error { return a.p.Release(handle) }

// --- git: a worktree per claim -------------------------------------------

// gitCapacity is a throughput knob, not a correctness input: worktrees are
// cheap, and every guarantee in the walk holds at capacity 1 (DD-8).
const gitCapacity = 8

type gitProvider struct {
	repoRoot string
	planDir  string
	run      runner
}

func (g *gitProvider) Kind() string  { return "git" }
func (g *gitProvider) Capacity() int { return gitCapacity }

func (g *gitProvider) Allocate(nodeID string) (Workspace, error) {
	wsDir := filepath.Join(g.planDir, gstore.GraphDirName, "ws-"+sanitize(nodeID))
	if _, err := os.Stat(wsDir); err == nil {
		// A leftover worktree (an expired claimant's post-mortem evidence,
		// or a crashed allocate) is never silently reused or destroyed.
		return Workspace{}, fmt.Errorf("workspace %s already exists; inspect and remove it (or `git worktree remove` it) before reclaiming this node", wsDir)
	}
	if _, err := g.run(g.repoRoot, "git", "worktree", "add", "--detach", wsDir, "HEAD"); err != nil {
		return Workspace{}, err
	}
	return Workspace{Handle: g.handleFor(wsDir), Dir: wsDir}, nil
}

func (g *gitProvider) handleFor(wsDir string) string {
	if rel, err := filepath.Rel(g.repoRoot, wsDir); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(wsDir)
}

func (g *gitProvider) absDir(handle string) string {
	if filepath.IsAbs(handle) {
		return handle
	}
	return filepath.Join(g.repoRoot, filepath.FromSlash(handle))
}

func (g *gitProvider) Release(handle string) error {
	if handle == "" {
		return nil
	}
	_, err := g.run(g.repoRoot, "git", "worktree", "remove", "--force", g.absDir(handle))
	return err
}

func (g *gitProvider) Isolation(handle string, activeClaims int) string {
	if handle != "" {
		return model.IsolationClean // a worktree is isolation by construction
	}
	if activeClaims <= 1 {
		return model.IsolationClean
	}
	return model.IsolationSharedDirty
}

func (g *gitProvider) Provenance(handle string) (*model.Provenance, error) {
	dir := g.repoRoot
	if handle != "" {
		dir = g.absDir(handle)
	}
	out, err := g.run(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	p := &model.Provenance{Kind: "git", Revision: strings.TrimSpace(string(out))}
	if handle != "" {
		p.Worktree = handle
	}
	return p, nil
}

// --- p4: one shared client, one pending changelist ------------------------

type p4Provider struct {
	repoRoot string
	run      runner
}

func (p *p4Provider) Kind() string  { return "p4" }
func (p *p4Provider) Capacity() int { return 1 }

func (p *p4Provider) Allocate(string) (Workspace, error) {
	// The real-world Perforce posture: agents work the shared client's tree
	// serially in one pending changelist; there is nothing to allocate.
	return Workspace{Handle: "", Dir: p.repoRoot}, nil
}

func (p *p4Provider) Release(string) error { return nil }

func (p *p4Provider) Isolation(_ string, activeClaims int) string {
	// Serial execution in one CL is clean BY CONSTRUCTION (DD-7): the tree
	// holds merged state plus the single claimant's edits. Two concurrent
	// claimants would taint every report — defensive, capacity forbids it.
	if activeClaims <= 1 {
		return model.IsolationClean
	}
	return model.IsolationSharedDirty
}

var p4ChangeRe = regexp.MustCompile(`^Change (\d+) `)

func (p *p4Provider) Provenance(string) (*model.Provenance, error) {
	out, err := p.run(p.repoRoot, "p4", "changes", "-m", "1", "-s", "pending")
	if err != nil {
		return nil, err
	}
	prov := &model.Provenance{Kind: "p4"}
	if m := p4ChangeRe.FindStringSubmatch(strings.TrimSpace(string(out))); m != nil {
		prov.Changelist = m[1]
		opened, err := p.run(p.repoRoot, "p4", "opened", "-c", m[1])
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(opened)), "\n") {
				if line == "" {
					continue
				}
				if i := strings.Index(line, "#"); i > 0 {
					prov.OpenedFiles = append(prov.OpenedFiles, line[:i])
				}
			}
		}
	}
	return prov, nil
}

// --- plain: no VCS, digest-only anchoring ---------------------------------

type plainProvider struct{ repoRoot string }

func (p *plainProvider) Kind() string                       { return "plain" }
func (p *plainProvider) Capacity() int                      { return 1 }
func (p *plainProvider) Allocate(string) (Workspace, error) { return Workspace{Dir: p.repoRoot}, nil }
func (p *plainProvider) Release(string) error               { return nil }
func (p *plainProvider) Isolation(_ string, activeClaims int) string {
	if activeClaims <= 1 {
		return model.IsolationClean
	}
	return model.IsolationSharedDirty
}

// Provenance is nil for plain trees: the digest anchor carries everything
// (DD-6), and inventing a pseudo-revision would be provenance nobody stands
// behind.
func (p *plainProvider) Provenance(string) (*model.Provenance, error) { return nil, nil }

var sanitizeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitize(s string) string {
	return sanitizeRe.ReplaceAllString(s, "-")
}
