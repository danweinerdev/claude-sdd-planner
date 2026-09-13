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
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/claims"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
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
	// HandleFor previews the handle Allocate will record for a node,
	// without side effects. Claims persist it BEFORE allocating (the
	// confirm-then-allocate ordering that makes gc concurrency-safe).
	HandleFor(nodeID string) string
	// Release tears a workspace down (merge or graceful abandonment; lease
	// EXPIRY deliberately never calls this — an expired claimant's
	// workspace is post-mortem evidence).
	Release(handle string) error
	// PruneMergedBranches deletes claim branches (graph/*) whose work is
	// fully integrated: tip reachable from the mainline HEAD and checked
	// out in no worktree. The retention policy (review-06 FU-01, decided
	// by the self-hosting pilot): merged branches are gc-reaped litter;
	// unmerged branches are the ONLY reference to their work and always
	// survive, as do active claims' checkouts. Nil for VCS without
	// branches.
	PruneMergedBranches() ([]string, error)
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

// execRunner runs one mutating VCS command through the bounded runner — a
// finite deadline, bounded output, typed causes — so a hung or missing
// executable cannot stall or silently degrade a claim. A command that RAN
// and exited nonzero is an ordinary error carrying its own diagnostics; a
// command that could not run at all wraps vcs.ErrOperational, which every
// caller boundary maps to exit 2 (FR-16, DD-10).
func execRunner(dir, name string, args ...string) ([]byte, error) {
	res, err := procexec.Run(context.Background(), name, args,
		procexec.Policy{Dir: dir})
	if err != nil {
		var pe *procexec.Error
		if errors.As(err, &pe) && pe.Cause == procexec.CauseExit {
			return nil, fmt.Errorf("%s %s: exit %d: %s", name, strings.Join(args, " "),
				pe.ExitCode, strings.TrimSpace(pe.Stderr))
		}
		return nil, fmt.Errorf("%w: %s %s: %w", vcs.ErrOperational, name, strings.Join(args, " "), err)
	}
	return res.Stdout, nil
}

// Detect routes on the repository's detected VCS. planDir hosts the
// gitignored workspace area; repoRoot is where commands run.
//
// Detection that could not run yields the unavailable provider, never the
// plain one: a plain posture on a real repository would disable worktree
// isolation and record digest-only provenance for work that does have a
// revision — an inability dressed as a posture. Callers whose outcome
// depends on the answer should use DetectChecked (FR-16, DD-10).
func Detect(repoRoot, planDir string) Provider {
	p, err := DetectChecked(repoRoot, planDir)
	if err != nil {
		return unavailableProvider{err: err}
	}
	return p
}

// IdleWorkspace reports whether a claimed workspace has no work worth
// keeping: no commits beyond the mainline it was allocated from, and no
// uncommitted changes. Only git worktrees can be non-idle (p4 and plain
// providers hand back handle == "", the shared tree, which release never
// tears down); every other provider reports idle with no reason to give a
// release command a workspace-specific "keep it" line for. branch names the
// worktree's checked-out branch, for diagnostics, when it can be read.
func IdleWorkspace(p Provider, handle string) (idle bool, reason, branch string, err error) {
	g, ok := p.(*gitProvider)
	if !ok || handle == "" {
		return true, "", "", nil
	}
	dir := g.absDir(handle)
	if out, branchErr := g.run(g.repoRoot, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD"); branchErr == nil {
		branch = strings.TrimSpace(string(out))
	}
	repo := vcs.Detect(dir)
	clean, dirty, err := repo.Clean()
	if err != nil {
		return false, "", branch, fmt.Errorf("checking workspace %s for uncommitted changes: %w", handle, err)
	}
	if !clean {
		return false, fmt.Sprintf("uncommitted changes (%s)", strings.Join(dirty, ", ")), branch, nil
	}
	head, err := g.run(g.repoRoot, "git", "-C", dir, "rev-parse", "HEAD")
	if err != nil {
		return false, "", branch, fmt.Errorf("reading workspace %s HEAD: %w", handle, err)
	}
	if _, err := g.run(g.repoRoot, "git", "merge-base", "--is-ancestor", strings.TrimSpace(string(head)), "HEAD"); err != nil {
		return false, "commits not yet merged into the mainline", branch, nil
	}
	return true, "", branch, nil
}

// DetectChecked is Detect with the failure distinguished from the answer: a
// detection probe that could not run returns (nil, err) wrapping
// vcs.ErrOperational, while a directory under no supported VCS still returns
// the plain provider with a nil error.
func DetectChecked(repoRoot, planDir string) (Provider, error) {
	repo, err := vcs.DetectChecked(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("detecting the VCS at %s: %w", repoRoot, err)
	}
	switch repo.Kind() {
	case vcs.Git, vcs.GitWorktree:
		return &gitProvider{repoRoot: repoRoot, planDir: planDir, run: execRunner}, nil
	case vcs.Perforce:
		return &p4Provider{repoRoot: repoRoot, run: execRunner}, nil
	case vcs.UnavailableKind:
		// DetectChecked's own error is the usual route here; this arm
		// catches an Unavailable adapter arriving by any other path, so
		// the inability can never reach the default (plain) arm.
		cause := error(vcs.ErrOperational)
		if u, ok := repo.(vcs.Unavailable); ok && u.Err != nil {
			cause = u.Err
		}
		return nil, fmt.Errorf("detecting the VCS at %s: %w", repoRoot, cause)
	default:
		return &plainProvider{repoRoot: repoRoot}, nil
	}
}

// unavailableProvider is what the unchecked Detect returns when detection
// could not run. Its Kind is never "plain": every operation reports the
// operational failure, so an unchecked caller that keeps going still cannot
// read the outcome as a plain-tree posture.
type unavailableProvider struct{ err error }

func (u unavailableProvider) Kind() string  { return string(vcs.UnavailableKind) }
func (u unavailableProvider) Capacity() int { return 1 }
func (u unavailableProvider) Allocate(string) (Workspace, error) {
	return Workspace{}, u.err
}
func (u unavailableProvider) HandleFor(string) string                { return "" }
func (u unavailableProvider) Release(string) error                   { return u.err }
func (u unavailableProvider) PruneMergedBranches() ([]string, error) { return nil, u.err }
func (u unavailableProvider) Isolation(string, int) string           { return model.IsolationSharedDirty }
func (u unavailableProvider) Provenance(string) (*model.Provenance, error) {
	return nil, u.err
}

// ForClaims adapts a Provider to the scheduling subset claims consumes.
func ForClaims(p Provider) claims.Provider { return claimsAdapter{p} }

type claimsAdapter struct{ p Provider }

func (a claimsAdapter) Capacity() int { return a.p.Capacity() }
func (a claimsAdapter) Allocate(nodeID string) (string, error) {
	ws, err := a.p.Allocate(nodeID)
	return ws.Handle, err
}
func (a claimsAdapter) HandleFor(nodeID string) string { return a.p.HandleFor(nodeID) }
func (a claimsAdapter) Release(handle string) error    { return a.p.Release(handle) }

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

func (g *gitProvider) wsDirFor(nodeID string) string {
	return filepath.Join(g.planDir, gstore.GraphDirName, "ws-"+sanitize(nodeID))
}

// HandleFor is Allocate's handle, computed without side effects: the
// workspace DIRECTORY is deterministic per node (only the branch carries a
// per-allocation suffix), so the preview is exact.
func (g *gitProvider) HandleFor(nodeID string) string {
	return g.handleFor(g.wsDirFor(nodeID))
}

func (g *gitProvider) Allocate(nodeID string) (Workspace, error) {
	wsDir := g.wsDirFor(nodeID)
	if _, err := os.Stat(wsDir); err == nil {
		// A leftover worktree (an expired claimant's post-mortem evidence,
		// or a crashed allocate) is never silently reused or destroyed.
		return Workspace{}, fmt.Errorf("workspace %s already exists; inspect and remove it (or `git worktree remove` it) before reclaiming this node", wsDir)
	}
	// A BRANCH per claim, not a detached HEAD: the node's commits must stay
	// reachable after the worktree is released at merge — a detached
	// worktree's commits would dangle and eventually be garbage-collected,
	// which is silent data loss of exactly the work the claim produced.
	if _, err := g.run(g.repoRoot, "git", "worktree", "add", "-b", g.branchFor(nodeID), wsDir, "HEAD"); err != nil {
		return Workspace{}, err
	}
	return Workspace{Handle: g.handleFor(wsDir), Dir: wsDir}, nil
}

// branchFor names a claim's branch: graph/<node>-<hex4>, unique per
// ALLOCATION, not per node. A deterministic name would make the branch of an
// earlier claim — which legitimately survives gc precisely so merged or
// crashed work stays reachable — collide with the next claim's worktree add,
// wedging every node that ever crashed once. The node id keeps branches
// findable (`git branch --list 'graph/<node>-*'`); the suffix keeps history
// append-only.
func (g *gitProvider) branchFor(nodeID string) string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Timestamps beat failing the claim over an entropy hiccup.
		return fmt.Sprintf("graph/%s-%d", sanitize(nodeID), os.Getpid())
	}
	return "graph/" + sanitize(nodeID) + "-" + hex.EncodeToString(b[:])
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
	branch, branchErr := g.run(g.repoRoot, "git", "-C", g.absDir(handle), "rev-parse", "--abbrev-ref", "HEAD")
	if _, err := g.run(g.repoRoot, "git", "worktree", "remove", "--force", g.absDir(handle)); err != nil {
		return err
	}
	if branchErr != nil {
		return nil // worktree removed; branch name unavailable is not fatal
	}
	name := strings.TrimSpace(string(branch))
	if name == "" || name == "HEAD" || !strings.HasPrefix(name, "graph/") {
		return nil
	}
	// Delete the claim branch only when it carries no commits beyond the
	// mainline it was allocated from (`git branch -d` independently refuses
	// unmerged branches, so this is belt-and-suspenders, not the only
	// check): a branch with real work is the only reference to it and must
	// survive release.
	if _, err := g.run(g.repoRoot, "git", "merge-base", "--is-ancestor", name, "HEAD"); err != nil {
		return nil // unmerged: keep it
	}
	if _, err := g.run(g.repoRoot, "git", "branch", "-d", name); err != nil {
		return nil // benign: concurrent checkout/delete race, or already gone
	}
	return nil
}

// PruneMergedBranches derives the prune set from git itself — branch list,
// checkout state, ancestry — never from graph bookkeeping: a branch whose
// tip the mainline already contains and which no worktree holds is litter
// by definition, whatever the graph believes. `git branch -d` is the safety
// net (it independently refuses unmerged and checked-out branches).
func (g *gitProvider) PruneMergedBranches() ([]string, error) {
	out, err := g.run(g.repoRoot, "git", "branch", "--list", "graph/*",
		"--format=%(refname:short)|%(worktreepath)")
	if err != nil {
		return nil, err
	}
	var pruned []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] != "" {
			continue // checked out somewhere (or empty listing): never prunable
		}
		name := parts[0]
		if _, err := g.run(g.repoRoot, "git", "merge-base", "--is-ancestor", name, "HEAD"); err != nil {
			continue // unmerged: the only reference to that work
		}
		if _, err := g.run(g.repoRoot, "git", "branch", "-d", name); err != nil {
			// Allocation can attach a worktree after the listing above but
			// before deletion. In that race, branch -d is the promised safety
			// net: re-check checkout state and treat its refusal as benign.
			// A branch concurrently pruned by another gc is benign too.
			current, checkErr := g.run(g.repoRoot, "git", "branch", "--list", name,
				"--format=%(refname:short)|%(worktreepath)")
			if checkErr == nil {
				line := strings.TrimSpace(string(current))
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "|", 2)
				if len(parts) == 2 && parts[1] != "" {
					continue
				}
			}
			return pruned, fmt.Errorf("pruning merged branch %s: %w", name, err)
		}
		pruned = append(pruned, name)
	}
	sort.Strings(pruned)
	return pruned, nil
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

func (p *p4Provider) HandleFor(string) string { return "" }

// PruneMergedBranches is nil for Perforce: one shared client, no branches.
func (p *p4Provider) PruneMergedBranches() ([]string, error) { return nil, nil }

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
func (p *plainProvider) HandleFor(string) string            { return "" }
func (p *plainProvider) Release(string) error               { return nil }

// PruneMergedBranches is nil for plain trees: no VCS, no branches.
func (p *plainProvider) PruneMergedBranches() ([]string, error) { return nil, nil }
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
