// Package claims owns who-works-what (Designs/SddGraph DD-10): claim
// selection and recording happen in one read-modify-write cycle against the
// store, so double-claim prevention is structural — it rests on the store's
// compare-and-swap, never on dispatch discipline or on the claimant's
// identity. `claim.by` is diagnostics and lease attribution only.
//
// Leases are fixed-TTL liveness bookkeeping: renewed implicitly by any
// claim-holder store-touching verb, expired at read (an expired claim's node
// returns to the frontier; its workspace file is preserved for post-mortem,
// never destroyed by the takeover). Wall-clock comparison tolerates skew
// because TTLs are minutes-scale and takeover itself is CAS-serialized.
//
// Provider side effects (workspace allocation) happen AFTER the claim is
// confirmed in the CAS cycle, never before: the claim record names the
// workspace handle the provider will produce, then allocation follows, and
// an allocation failure rolls the claim back. The ordering is what makes gc
// safe to run concurrently with claiming — a workspace directory never
// exists without a claim referencing it, so gc's unreferenced-workspace scan
// can never reap an allocation in flight. The inverse window (a claim
// briefly naming a workspace that does not exist yet) is harmless: a crash
// there leaves a claim that expires naturally, and reaping a directory that
// was never created is a no-op.
package claims

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// DefaultTTL is the lease length when planning-config.json names none
// (design OQ-2: the value is a liveness constant, not a correctness input).
const DefaultTTL = 30 * time.Minute

// Provider allocates per-claim workspaces (Designs/SddGraph DD-8). Phase 3.3
// supplies the real git/p4/plain implementations; the stub below is the
// capacity-1 shared-tree posture every VCS can satisfy.
type Provider interface {
	// Capacity is how many isolated workspaces this provider can sustain.
	Capacity() int
	// Allocate prepares a workspace for one node and returns its handle
	// (opaque to the graph; "" means the shared tree).
	Allocate(nodeID string) (string, error)
	// HandleFor previews the handle Allocate will return for a node,
	// without side effects. The claim record carries it BEFORE allocation
	// runs, which is what closes the gc race (see the package comment).
	HandleFor(nodeID string) string
	// Release tears a workspace down after merge or abandonment.
	Release(workspace string) error
}

// StubProvider is the provider every VCS satisfies: one shared working
// tree, no per-claim isolation handle.
type StubProvider struct{}

func (StubProvider) Capacity() int                   { return 1 }
func (StubProvider) Allocate(string) (string, error) { return "", nil }
func (StubProvider) HandleFor(string) string         { return "" }
func (StubProvider) Release(string) error            { return nil }

// Options configures one claim attempt.
type Options struct {
	// By attributes the claim (diagnostics and lease ownership only).
	By string
	// TTL is the lease length; zero means DefaultTTL.
	TTL time.Duration
	// Now is the clock seam; nil means time.Now.
	Now func() time.Time
	// Provider allocates the workspace; nil means StubProvider.
	Provider Provider
	// StatesInputs builds the derive inputs for a graph snapshot; the caller
	// wires digest and intent sources (nil axes are simply disabled).
	StatesInputs func(*model.Graph) states.Inputs
}

func (o *Options) fill() {
	if o.TTL <= 0 {
		o.TTL = DefaultTTL
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Provider == nil {
		o.Provider = StubProvider{}
	}
	if o.StatesInputs == nil {
		o.StatesInputs = func(g *model.Graph) states.Inputs { return states.Inputs{Graph: g} }
	}
	if o.By == "" {
		o.By = "agent"
	}
}

// Claimed reports a successful claim.
type Claimed struct {
	Node         model.Node
	LeaseExpires string
	Workspace    string
	// ReclaimedExpired lists nodes whose lapsed claims were cleared during
	// this pass (their workspaces survive for post-mortem).
	ReclaimedExpired []string
}

// claimAttempts bounds the allocate-confirm race loop, same posture as the
// store's CAS bound.
const claimAttempts = 8

// Claim selects the heaviest claimable frontier node and records the claim.
// A nil error with a nil Claimed cannot happen: refusals are errors that
// explain the frontier (state counts, active claims, capacity).
func Claim(planDir string, o Options) (*Claimed, error) {
	o.fill()
	graphPath := gstore.PathFor(planDir)

	for attempt := 0; attempt < claimAttempts; attempt++ {
		g, err := gstore.Load(graphPath)
		if err != nil {
			return nil, err
		}
		reclaimed := expireLapsed(g, o.Now())
		candidate, explain := selectCandidate(g, o)
		if candidate == "" {
			return nil, fmt.Errorf("graph claim: nothing claimable — %s", explain)
		}

		// Confirm FIRST, allocate after (see the package comment): the
		// claim record — naming the handle the provider will produce —
		// lands in the CAS cycle before any directory exists, so a
		// concurrent gc can never mistake an allocation in flight for an
		// unreferenced leftover.
		handle := o.Provider.HandleFor(candidate)
		leaseExpires := o.Now().Add(o.TTL).UTC().Format(time.RFC3339)
		var claimedNode model.Node
		raced := false
		_, err = gstore.Update(graphPath, func(fresh *model.Graph) error {
			now := o.Now()
			reclaimed = expireLapsed(fresh, now)
			n := fresh.NodeByID(candidate)
			if n == nil {
				raced = true
				return nil
			}
			if stillCandidate, _ := selectableSet(fresh, o); !stillCandidate[candidate] {
				raced = true
				return nil
			}
			n.Claim = &model.Claim{By: o.By, LeaseExpires: leaseExpires, Workspace: handle}
			claimedNode = *n
			return nil
		})
		if err != nil {
			return nil, err
		}
		if raced {
			// Someone else took it (or the graph moved) between selection
			// and confirmation: nothing was recorded or allocated,
			// re-select.
			continue
		}

		workspace, allocErr := o.Provider.Allocate(candidate)
		if allocErr != nil {
			// Roll the claim back — but only OUR claim: match on the exact
			// lease so a concurrent expiry+reclaim is never clobbered.
			_, _ = gstore.Update(graphPath, func(fresh *model.Graph) error {
				if n := fresh.NodeByID(candidate); n != nil && n.Claim != nil &&
					n.Claim.By == o.By && n.Claim.LeaseExpires == leaseExpires {
					n.Claim = nil
				}
				return nil
			})
			return nil, fmt.Errorf("graph claim: workspace allocation for %q failed, claim rolled back: %w", candidate, allocErr)
		}
		if workspace != handle {
			// The provider disagreed with its own preview; the record
			// carries the truth.
			if _, err := gstore.Update(graphPath, func(fresh *model.Graph) error {
				if n := fresh.NodeByID(candidate); n != nil && n.Claim != nil &&
					n.Claim.By == o.By && n.Claim.LeaseExpires == leaseExpires {
					n.Claim.Workspace = workspace
				}
				return nil
			}); err != nil {
				return nil, err
			}
			claimedNode.Claim.Workspace = workspace
		}
		return &Claimed{
			Node:             claimedNode,
			LeaseExpires:     leaseExpires,
			Workspace:        workspace,
			ReclaimedExpired: reclaimed,
		}, nil
	}
	return nil, fmt.Errorf("graph claim: gave up after %d selection races; the frontier is contended, retry", claimAttempts)
}

// ExpireLapsed persists the expiry of every lapsed claim: the crash story's
// bookkeeping half. gc runs it before reaping so a dead claimant's workspace
// stops being referenced and becomes reapable; the workspace itself is
// preserved until gc, as post-mortem evidence.
func ExpireLapsed(planDir string, now func() time.Time) ([]string, error) {
	if now == nil {
		now = time.Now
	}
	var reclaimed []string
	_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		reclaimed = expireLapsed(g, now())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reclaimed, nil
}

// expireLapsed clears every claim whose lease has lapsed, in place, and
// returns the affected node ids sorted. Workspace handles are deliberately
// NOT released: an expired claimant's workspace is post-mortem evidence.
func expireLapsed(g *model.Graph, now time.Time) []string {
	var out []string
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Claim == nil {
			continue
		}
		expires, err := time.Parse(time.RFC3339, n.Claim.LeaseExpires)
		if err != nil || !expires.After(now) {
			n.Claim = nil
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}

// selectableSet computes the claimable frontier after claim/artifact/
// capacity screens. The second return is the human explanation used when
// the set is empty.
func selectableSet(g *model.Graph, o Options) (map[string]bool, string) {
	derived := states.Derive(o.StatesInputs(g))

	stateCount := map[states.State]int{}
	activeClaims := 0
	claimedArtifacts := map[string]bool{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		stateCount[derived[n.ID].State]++
		if n.Claim != nil {
			activeClaims++
			for _, a := range n.Artifacts {
				claimedArtifacts[a] = true
			}
		}
	}

	capacity := o.Provider.Capacity()
	out := map[string]bool{}
	if activeClaims >= capacity {
		return out, fmt.Sprintf("provider capacity %d is fully claimed (%d active claim(s)); merge or release first", capacity, activeClaims)
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if !derived[n.ID].OnFrontier || n.Claim != nil {
			continue
		}
		overlap := false
		for _, a := range n.Artifacts {
			if claimedArtifacts[a] {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		out[n.ID] = true
	}
	if len(out) == 0 {
		var parts []string
		for _, s := range []states.State{states.Green, states.Ready, states.Red, states.Stale, states.Blocked} {
			if c := stateCount[s]; c > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", c, s))
			}
		}
		explain := "no workable node has all deps GREEN"
		if len(parts) > 0 {
			explain += " (" + strings.Join(parts, ", ") + ")"
		}
		if activeClaims > 0 {
			explain += fmt.Sprintf("; %d node(s) actively claimed", activeClaims)
		}
		return out, explain
	}
	return out, ""
}

// selectCandidate picks the heaviest claimable node (critical-path-first,
// DD-14's scheduling half), ties broken by id for determinism.
func selectCandidate(g *model.Graph, o Options) (string, string) {
	set, explain := selectableSet(g, o)
	if len(set) == 0 {
		return "", explain
	}
	adjacency := algorithms.Graph{}
	estimate := map[string]int{}
	for i := range g.Nodes {
		adjacency[g.Nodes[i].ID] = g.Nodes[i].Deps
		estimate[g.Nodes[i].ID] = g.Nodes[i].Estimate
	}
	weight := algorithms.CriticalWeight(adjacency, estimate)

	best := ""
	for id := range set {
		if best == "" || weight[id] > weight[best] || (weight[id] == weight[best] && id < best) {
			best = id
		}
	}
	return best, ""
}

// Renew extends the caller's own lease — the implicit renewal every
// claim-holder store-touching verb performs. A claim held by someone else
// (or by nobody) refuses: liveness is proven by observed activity against
// the store, never by self-report.
func Renew(planDir, nodeID, by string, ttl time.Duration, now func() time.Time) (string, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	leaseExpires := now().Add(ttl).UTC().Format(time.RFC3339)
	_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID(nodeID)
		if n == nil {
			return fmt.Errorf("graph renew: node %q does not exist", nodeID)
		}
		if n.Claim == nil {
			return fmt.Errorf("graph renew: %q is not claimed; claim it with `sdd next --claim`", nodeID)
		}
		if n.Claim.By != by {
			return fmt.Errorf("graph renew: %q is claimed by %q, not %q; a lease is renewed only by its holder", nodeID, n.Claim.By, by)
		}
		n.Claim.LeaseExpires = leaseExpires
		return nil
	})
	if err != nil {
		return "", err
	}
	return leaseExpires, nil
}

// Release clears a claim: the graceful abandonment path. Only the holder may
// release, unless force names the takeover explicitly. The workspace handle
// is returned so the caller can hand it to the provider.
func Release(planDir, nodeID, by string, force bool) (string, error) {
	workspace := ""
	_, err := gstore.Update(gstore.PathFor(planDir), func(g *model.Graph) error {
		n := g.NodeByID(nodeID)
		if n == nil {
			return fmt.Errorf("graph release: node %q does not exist", nodeID)
		}
		if n.Claim == nil {
			return fmt.Errorf("graph release: %q is not claimed", nodeID)
		}
		if n.Claim.By != by && !force {
			return fmt.Errorf("graph release: %q is claimed by %q, not %q; pass --force to take it over deliberately", nodeID, n.Claim.By, by)
		}
		workspace = n.Claim.Workspace
		n.Claim = nil
		return nil
	})
	if err != nil {
		return "", err
	}
	return workspace, nil
}
