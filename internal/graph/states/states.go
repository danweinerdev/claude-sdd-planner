// Package states derives node state from graph structure and observations.
package states

import (
	"fmt"
	"sort"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

type State string

const (
	Blocked State = "BLOCKED"
	Ready   State = "READY"
	Red     State = "RED"
	Green   State = "GREEN"
	Stale   State = "STALE"
)

// NodeState is one derived state and its mechanical reasons.
type NodeState struct {
	ID              string
	State           State
	DependencyStale []string
	IsolationStale  bool
	RevIncompatible bool
	ReviewStale     []string
	Workable        bool
	OnFrontier      bool
	InCycle         bool
}

// Inputs is intentionally graph-only. Repository content is declaration data,
// never a state input.
type Inputs struct{ Graph *model.Graph }

func ReviewScope(g *model.Graph, gateID string) ([]string, error) {
	return ReviewScopeFromStates(g, gateID, Derive(Inputs{Graph: g}))
}

// LegacyReviewedSet materializes the structural meaning of a legacy review.
func LegacyReviewedSet(g *model.Graph, reviewID string, scope []string) map[string]model.ReviewedRef {
	if len(scope) == 0 {
		if r := g.NodeByID(reviewID); r != nil {
			scope = append([]string(nil), r.Deps...)
		}
	}
	out := make(map[string]model.ReviewedRef, len(scope))
	for _, id := range scope {
		if n := g.NodeByID(id); n != nil {
			out[id] = model.ReviewedRef{ContractRev: n.EffectiveContractRev()}
		}
	}
	return out
}

func ReviewScopeFromStates(g *model.Graph, gateID string, current map[string]NodeState) ([]string, error) {
	return reviewScope(g, gateID, func(id string) bool { return current[id].State == Green })
}

func reviewScope(g *model.Graph, gateID string, current func(string) bool) ([]string, error) {
	gate := g.NodeByID(gateID)
	if gate == nil {
		return nil, fmt.Errorf("graph review: node %q does not exist", gateID)
	}
	if gate.Gate.Type != model.GateReview {
		return nil, fmt.Errorf("graph review: %q has gate type %q; scope derives for review gates only", gateID, gate.Gate.Type)
	}
	adjacency := algorithms.Graph{}
	for i := range g.Nodes {
		adjacency[g.Nodes[i].ID] = g.Nodes[i].Deps
	}
	closure := algorithms.DependencyClosure(adjacency, gateID)
	delete(closure, gateID)
	covered := map[string]bool{}
	for i := range g.Nodes {
		inner := &g.Nodes[i]
		if inner.ID == gateID || !closure[inner.ID] || inner.Gate.Type != model.GateReview || inner.Gate.Lanes != nil || !current(inner.ID) {
			continue
		}
		covered[inner.ID] = true
		for id := range algorithms.DependencyClosure(adjacency, inner.ID) {
			covered[id] = true
		}
	}
	var out []string
	for id := range closure {
		if !covered[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func Closed(g *model.Graph, statesByID map[string]NodeState) map[string]bool {
	closed := map[string]bool{}
	for i := range g.Nodes {
		r := &g.Nodes[i]
		if r.Gate.Type != model.GateReview || r.Gate.Lanes != nil || r.Verification == nil || r.Verification.Result != model.ResultPass || statesByID[r.ID].State != Green {
			continue
		}
		closed[r.ID] = true
		scope, err := ReviewScopeFromStates(g, r.ID, statesByID)
		if err == nil {
			for _, id := range scope {
				if statesByID[id].State == Green {
					closed[id] = true
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := range g.Nodes {
			n := &g.Nodes[i]
			if closed[n.ID] || n.EffectiveRole() != model.RoleIntegrationAcceptance || statesByID[n.ID].State != Green {
				continue
			}
			upstream := dependencyClosure(g, n.ID)
			covered := len(upstream) > 0
			for _, id := range upstream {
				covered = covered && closed[id]
			}
			if covered {
				closed[n.ID], changed = true, true
			}
		}
	}
	return closed
}

func dependencyClosure(g *model.Graph, nodeID string) []string {
	adjacency := algorithms.Graph{}
	for i := range g.Nodes {
		adjacency[g.Nodes[i].ID] = g.Nodes[i].Deps
	}
	set := algorithms.DependencyClosure(adjacency, nodeID)
	delete(set, nodeID)
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Derive uses only structure, observation sequence, contract revision,
// isolation, and red-before-green bookkeeping.
func Derive(in Inputs) map[string]NodeState {
	g := in.Graph
	adjacency := algorithms.Graph{}
	byID := map[string]*model.Node{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		adjacency[n.ID], byID[n.ID] = n.Deps, n
	}
	order := algorithms.TopoSort(adjacency)
	ordered := map[string]bool{}
	out := make(map[string]NodeState, len(g.Nodes))
	for _, id := range order {
		ordered[id] = true
		n := byID[id]
		ns := NodeState{ID: id}
		depsAllGreen := true
		for _, dep := range n.Deps {
			depsAllGreen = depsAllGreen && out[dep].State == Green
		}
		v := n.Verification
		switch {
		case v == nil:
			if depsAllGreen {
				ns.State = Ready
			} else {
				ns.State = Blocked
			}
		case v.EffectiveContractRev() != n.EffectiveContractRev():
			ns.RevIncompatible = true
			if depsAllGreen {
				ns.State = Ready
			} else {
				ns.State = Blocked
			}
		case v.Result == model.ResultFail:
			ns.State = Red
		default:
			for _, dep := range n.Deps {
				if dn := byID[dep]; dn != nil && dn.Verification != nil && dn.Verification.Seq > v.Seq {
					ns.DependencyStale = append(ns.DependencyStale, dep)
				}
			}
			if v.Isolation != model.IsolationClean {
				ns.IsolationStale = true
			}
			if n.Gate.Type == model.GateReview {
				stale := map[string]bool{}
				currentScope, err := reviewScope(g, n.ID, func(id string) bool { return out[id].State == Green })
				if err == nil {
					if v.Reviewed == nil {
						for _, reviewedID := range currentScope {
							rn := byID[reviewedID]
							if rn != nil && (rn.EffectiveContractRev() > 1 || rn.Verification != nil && rn.Verification.Seq > v.Seq) {
								stale[reviewedID] = true
							}
						}
					} else {
						current := map[string]bool{}
						for _, reviewedID := range currentScope {
							current[reviewedID] = true
							ref, present := v.Reviewed[reviewedID]
							rn := byID[reviewedID]
							if !present || rn == nil || rn.EffectiveContractRev() > ref.ContractRev || rn.Verification != nil && rn.Verification.Seq > v.Seq {
								stale[reviewedID] = true
							}
						}
						for reviewedID := range v.Reviewed {
							if !current[reviewedID] {
								stale[reviewedID] = true
							}
						}
					}
				}
				for reviewedID := range stale {
					ns.ReviewStale = append(ns.ReviewStale, reviewedID)
				}
				sort.Strings(ns.ReviewStale)
			}
			sort.Strings(ns.DependencyStale)
			if len(ns.DependencyStale) > 0 || ns.IsolationStale || len(ns.ReviewStale) > 0 {
				ns.State = Stale
			} else {
				ns.State = Green
			}
		}
		ns.Workable = ns.State == Ready || ns.State == Red || ns.State == Stale
		ns.OnFrontier = ns.Workable && depsAllGreen
		out[id] = ns
	}
	for id := range adjacency {
		if !ordered[id] {
			out[id] = NodeState{ID: id, State: Blocked, InCycle: true}
		}
	}
	return out
}

func Frontier(statesByID map[string]NodeState) []string {
	var out []string
	for id, ns := range statesByID {
		if ns.OnFrontier {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
