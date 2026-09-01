// Package ops carries the locked single-node mutations of the walk
// (Designs/SddGraph DD-11's imperative half): split a node that proved too
// big, edit a node's declared tests, and reap abandoned workspace state.
// Construction stays declarative (payloads through compile); these verbs are
// the sanctioned exceptions, each one read-modify-write cycle under the
// store's compare-and-swap.
package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/claims"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/hazards"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// SplitResult reports a split.
type SplitResult struct {
	Retired  string   `json:"retired"`
	Children []string `json:"children"`
	// Rewired lists dependants whose deps were re-pointed at the children.
	Rewired []string `json:"rewired,omitempty"`
}

// Split retires a node into children (the stopping rule's remedy: a node
// that failed twice is too big). The children arrive as a strict proposal
// payload; they inherit the original's deps, hazards triage, and phase label
// where they declare none; every dependant of the original is re-pointed at
// all children; the original's id enters the retired register and is never
// reused. The mutation is gated like a compile: it must introduce no
// semantic finding the compiler would refuse.
func Split(root, repoRoot, plan, nodeID string, childrenPayload []byte) (*SplitResult, error) {
	planDir := filepath.Join(root, "Plans", plan)
	p, err := model.DecodeProposal(childrenPayload)
	if err != nil {
		return nil, fmt.Errorf("graph split: children payload refused:\n%w", err)
	}
	if len(p.Nodes) < 2 {
		return nil, fmt.Errorf("graph split: a split produces at least two children; %d supplied", len(p.Nodes))
	}

	// Build the candidate graph in memory, then gate it on introduced
	// findings before anything is written.
	current, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		return nil, err
	}
	candidate, res, err := applySplit(current, nodeID, p)
	if err != nil {
		return nil, err
	}
	before, err := gcompile.Validate(root, repoRoot, plan, current)
	if err != nil {
		return nil, err
	}
	after, err := gcompile.Validate(root, repoRoot, plan, candidate)
	if err != nil {
		return nil, err
	}
	if introduced := introducedFindings(before, after); len(introduced) > 0 {
		var b strings.Builder
		b.WriteString("graph split: refused — the split would introduce findings compile refuses:\n")
		for _, f := range introduced {
			fmt.Fprintf(&b, "  %s\n", f.String())
		}
		return nil, fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}

	if _, err := gstore.Update(gstore.PathFor(planDir), func(fresh *model.Graph) error {
		rebuilt, _, err := applySplit(fresh, nodeID, p)
		if err != nil {
			return err
		}
		*fresh = *rebuilt
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// applySplit computes the post-split graph without touching disk. Pure so
// the gate can inspect the candidate and the CAS cycle can re-derive it.
func applySplit(g *model.Graph, nodeID string, p *model.Proposal) (*model.Graph, *SplitResult, error) {
	original := g.NodeByID(nodeID)
	if original == nil {
		return nil, nil, fmt.Errorf("graph split: node %q does not exist", nodeID)
	}
	if original.Claim != nil {
		return nil, nil, fmt.Errorf("graph split: %q is claimed by %q; release the claim first", nodeID, original.Claim.By)
	}
	existing := map[string]bool{}
	for _, id := range g.NodeIDs() {
		existing[id] = true
	}
	retired := map[string]bool{}
	for _, id := range g.Retired {
		retired[id] = true
	}
	childIDs := map[string]bool{}
	children := make([]model.Node, len(p.Nodes))
	copy(children, p.Nodes)
	for i := range children {
		c := &children[i]
		switch {
		case c.ID == nodeID:
			return nil, nil, fmt.Errorf("graph split: child %q reuses the retired id", c.ID)
		case existing[c.ID]:
			return nil, nil, fmt.Errorf("graph split: child %q already exists in the graph", c.ID)
		case retired[c.ID]:
			return nil, nil, fmt.Errorf("graph split: child %q was retired; retired ids are never reused", c.ID)
		case childIDs[c.ID]:
			return nil, nil, fmt.Errorf("graph split: child %q declared twice", c.ID)
		}
		childIDs[c.ID] = true
		// Inheritance: what a child does not declare, it keeps from the
		// node it came from — deps, hazards triage, phase label.
		if len(c.Deps) == 0 {
			c.Deps = append([]string(nil), original.Deps...)
		}
		if c.Hazards == nil && original.Hazards != nil {
			c.Hazards = append(model.Hazards{}, original.Hazards...)
		}
		if c.Phase == "" {
			c.Phase = original.Phase
		}
	}

	out := &model.Graph{Version: g.Version, SeqCounter: g.SeqCounter}
	out.Retired = append(append([]string(nil), g.Retired...), nodeID)
	sort.Strings(out.Retired)

	sortedChildren := make([]string, 0, len(childIDs))
	for id := range childIDs {
		sortedChildren = append(sortedChildren, id)
	}
	sort.Strings(sortedChildren)

	var rewired []string
	for i := range g.Nodes {
		n := g.Nodes[i] // copy
		if n.ID == nodeID {
			continue
		}
		var deps []string
		replaced := false
		for _, d := range n.Deps {
			if d == nodeID {
				replaced = true
				continue
			}
			deps = append(deps, d)
		}
		if replaced {
			present := map[string]bool{}
			for _, d := range deps {
				present[d] = true
			}
			for _, c := range sortedChildren {
				if !present[c] {
					deps = append(deps, c)
				}
			}
			sort.Strings(deps)
			rewired = append(rewired, n.ID)
		}
		n.Deps = deps
		out.Nodes = append(out.Nodes, n)
	}
	out.Nodes = append(out.Nodes, children...)
	sort.Strings(rewired)
	return out, &SplitResult{Retired: nodeID, Children: sortedChildren, Rewired: rewired}, nil
}

// introducedFindings diffs two finding sets by rendered text.
func introducedFindings(before, after []gcompile.Finding) []gcompile.Finding {
	prior := map[string]bool{}
	for _, f := range before {
		prior[f.String()] = true
	}
	var out []gcompile.Finding
	for _, f := range after {
		if !prior[f.String()] {
			out = append(out, f)
		}
	}
	return out
}

// SetTests replaces a node's declared test list under the lock. Holder-only
// when the node is claimed. red_seq entries for tests no longer declared are
// pruned: a renamed or replaced test owes a fresh red proof.
func SetTests(planDir, nodeID, by string, tests []model.Test) error {
	if len(tests) == 0 {
		return fmt.Errorf("graph set-tests: at least one test is required (an empty tests gate verifies nothing)")
	}
	seen := map[string]bool{}
	for _, t := range tests {
		if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.File) == "" {
			return fmt.Errorf("graph set-tests: every test needs a nonempty id and file")
		}
		if seen[t.ID] {
			return fmt.Errorf("graph set-tests: test %q declared twice", t.ID)
		}
		seen[t.ID] = true
	}
	_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID(nodeID)
		if n == nil {
			return fmt.Errorf("graph set-tests: node %q does not exist", nodeID)
		}
		if n.Claim != nil && n.Claim.By != by {
			return fmt.Errorf("graph set-tests: %q is claimed by %q; only the holder edits its tests", nodeID, n.Claim.By)
		}
		if n.Gate.Type != model.GateTests {
			return fmt.Errorf("graph set-tests: %q has gate type %q; set-tests applies to tests gates", nodeID, n.Gate.Type)
		}
		declared := map[string]bool{}
		for _, h := range n.Hazards {
			declared[h] = true
		}
		for _, t := range tests {
			for _, s := range t.Satisfies {
				if err := hazards.RequireKnown(s, "test "+t.ID); err != nil {
					return fmt.Errorf("graph set-tests: %v", err)
				}
				if !declared[s] {
					return fmt.Errorf("graph set-tests: test %q satisfies %q, which node %q does not declare", t.ID, s, nodeID)
				}
			}
		}
		n.Gate.Tests = tests
		for id := range n.RedSeqs {
			if !seen[id] {
				delete(n.RedSeqs, id)
			}
		}
		return nil
	})
	return err
}

// GCResult reports what gc reaped.
type GCResult struct {
	// ExpiredClaims lists nodes whose lapsed claims gc expired (persisted)
	// before reaping — the crash story's bookkeeping half.
	ExpiredClaims []string `json:"expired_claims,omitempty"`
	Workspaces    []string `json:"workspaces,omitempty"`
	StalePayloads []string `json:"stale_payloads,omitempty"`
	// PrunedBranches lists fully-integrated claim branches deleted by the
	// provider (git: graph/* tips reachable from mainline HEAD, checked
	// out nowhere). Unmerged branches always survive — they are the only
	// reference to their work.
	PrunedBranches []string `json:"pruned_branches,omitempty"`
	Kept           []string `json:"kept,omitempty"`
}

// GC reaps abandoned workspace state: ws-* directories no active claim
// references (expired-claim leftovers, crashed merges), and staged payloads
// whose every node already exists in the graph (the review-05 FU-01 case: a
// crash between compile's graph write and payload consumption). Active
// claims' workspaces and payloads carrying novel nodes are never touched.
func GC(root, repoRoot, plan string) (*GCResult, error) {
	planDir := filepath.Join(root, "Plans", plan)
	// The crash story's bookkeeping half runs first: persisting the expiry
	// of lapsed claims is what makes a dead claimant's workspace
	// unreferenced — and therefore reapable — while unexpired claims keep
	// theirs untouchable.
	expired, err := claims.ExpireLapsed(planDir, nil)
	if err != nil {
		return nil, err
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for i := range g.Nodes {
		if c := g.Nodes[i].Claim; c != nil && c.Workspace != "" {
			active[filepath.ToSlash(c.Workspace)] = true
		}
	}
	existing := map[string]bool{}
	for _, id := range g.NodeIDs() {
		existing[id] = true
	}

	res := &GCResult{ExpiredClaims: expired}
	prov := provider.Detect(repoRoot, planDir)

	graphDir := filepath.Join(planDir, gstore.GraphDirName)
	entries, err := os.ReadDir(graphDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "ws-") {
			continue
		}
		abs := filepath.Join(graphDir, e.Name())
		handle := abs
		if rel, err := filepath.Rel(repoRoot, abs); err == nil && !strings.HasPrefix(rel, "..") {
			handle = filepath.ToSlash(rel)
		}
		if active[handle] {
			res.Kept = append(res.Kept, handle)
			continue
		}
		// The provider gets first refusal (a live git worktree needs
		// `worktree remove`, not a bare delete), but its success is not
		// trusted to mean the directory is gone: plain and p4 providers
		// no-op Release, and a half-torn worktree can "succeed" too.
		provErr := prov.Release(handle)
		if _, statErr := os.Stat(abs); statErr == nil {
			if rmErr := os.RemoveAll(abs); rmErr != nil {
				return nil, fmt.Errorf("graph gc: %s could not be reaped: %v (provider release: %v)", handle, rmErr, provErr)
			}
		}
		res.Workspaces = append(res.Workspaces, handle)
	}

	// Stale payloads: every declared node already landed.
	fragDir := proposal.FragmentsDir(planDir)
	frags, err := os.ReadDir(fragDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var payloads []string
	for _, e := range frags {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			payloads = append(payloads, filepath.Join(fragDir, e.Name()))
		}
	}
	if _, err := os.Stat(proposal.AssembledPath(planDir)); err == nil {
		payloads = append(payloads, proposal.AssembledPath(planDir))
	}
	for _, path := range payloads {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		p, err := model.DecodeProposal(raw)
		if err != nil || len(p.Nodes) == 0 {
			continue // undecodable or empty: not provably stale, keep it
		}
		stale := true
		for _, n := range p.Nodes {
			if !existing[n.ID] {
				stale = false
				break
			}
		}
		if !stale {
			continue
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("graph gc: stale payload %s could not be removed: %v", path, err)
		}
		res.StalePayloads = append(res.StalePayloads, filepath.Base(path))
	}
	// Branch pruning runs LAST: a workspace released above may have been
	// the only checkout holding its branch, and this pass is what finally
	// reaps it once the work is mainline-reachable.
	pruned, err := prov.PruneMergedBranches()
	if err != nil {
		return nil, fmt.Errorf("graph gc: %w", err)
	}
	res.PrunedBranches = pruned

	sort.Strings(res.Workspaces)
	sort.Strings(res.StalePayloads)
	sort.Strings(res.Kept)
	return res, nil
}
