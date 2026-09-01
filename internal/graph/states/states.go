// Package states derives every node's execution state on read — the single
// place the state rules live (Designs/SddGraph § Node states, DD-3).
//
// Nothing here is persisted, and nothing here may ever be: a stored state
// field would drift the moment anyone edits code outside the tool, which is
// the exact failure the derived model removes. If a derive pass is ever too
// slow, the sanctioned fix is a faster pass, never a cache field in the
// model.
//
// The rules, each encoding a defect class stored-status systems ship:
//
//   - RED outranks BLOCKED: a recorded failure is never hidden by an
//     unrelated upstream change.
//   - Workable ≠ frontier: READY, RED, and STALE can all be worked, but the
//     frontier re-gates on deps independently — a RED node with a non-GREEN
//     dep stays off it.
//   - Staleness propagates three ways: seq (an ancestor re-verified more
//     recently), digest (a declared artifact's bytes drifted from the
//     observation — the silent-edit catcher, DD-6), and intent (a cited
//     requirement's fingerprint no longer matches — INTENT-STALE, DD-4,
//     surfaced distinctly because its remedy is judgment, not re-running a
//     suite).
package states

import (
	"sort"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

// State is a derived execution state. GREEN is assumed closure — sufficient
// to build on, never completion-grade; the closed predicate (phase 4) layers
// on top (D-0022).
type State string

const (
	Blocked State = "BLOCKED"
	Ready   State = "READY"
	Red     State = "RED"
	Green   State = "GREEN"
	Stale   State = "STALE"
)

// NodeState is one node's derived state and, when STALE, why.
type NodeState struct {
	ID    string
	State State

	// SeqStale: an ancestor carries a verification seq newer than this
	// node's own.
	SeqStale bool
	// DigestStale lists declared artifacts whose current digest no longer
	// matches the observation (or that the observation never recorded).
	DigestStale []string
	// IntentStale lists cited ids whose embedded fingerprint no longer
	// matches the requirement's current text — the INTENT-STALE diagnostic,
	// distinct because its remedy (re-hash / rework / replan) is a judgment
	// call.
	IntentStale []string

	// Workable: READY, RED, or STALE.
	Workable bool
	// OnFrontier: workable AND every dep GREEN — what `next` serves.
	OnFrontier bool
	// InCycle marks a node the topological pass could not order. Compile
	// refuses cycles, so this is defensive: such a node reports BLOCKED and
	// names the cause rather than deriving nonsense.
	InCycle bool
}

// Inputs carries everything a derive pass reads. ArtifactDigest and
// CurrentIntentHashes are optional: nil disables that staleness axis (a
// caller without a repo checkout still gets seq semantics).
type Inputs struct {
	Graph *model.Graph
	// ArtifactDigest returns a declared artifact's current content digest,
	// "" when the file is missing or unreadable.
	ArtifactDigest func(rel string) string
	// CurrentIntentHashes maps cited id -> the requirement's current
	// fingerprint, "" / absent when the id no longer resolves.
	CurrentIntentHashes map[string]string
}

// Derive computes every node's state in one topological pass.
func Derive(in Inputs) map[string]NodeState {
	g := in.Graph
	adjacency := algorithms.Graph{}
	byID := map[string]*model.Node{}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		adjacency[n.ID] = n.Deps
		byID[n.ID] = n
	}

	order := algorithms.TopoSort(adjacency)
	ordered := map[string]bool{}
	for _, id := range order {
		ordered[id] = true
	}

	out := make(map[string]NodeState, len(g.Nodes))
	// effective[n] = the highest verification seq among n and its ancestors,
	// carried forward through the topological order.
	effective := map[string]int{}

	for _, id := range order {
		n := byID[id]
		ns := NodeState{ID: id}

		ancestorSeq := 0
		depsAllGreen := true
		for _, dep := range n.Deps {
			if e, ok := effective[dep]; ok && e > ancestorSeq {
				ancestorSeq = e
			}
			if out[dep].State != Green {
				depsAllGreen = false
			}
		}

		ownSeq := 0
		if v := n.Verification; v != nil {
			ownSeq = v.Seq
		}
		eff := ownSeq
		if ancestorSeq > eff {
			eff = ancestorSeq
		}
		effective[id] = eff

		switch v := n.Verification; {
		case v == nil:
			if depsAllGreen {
				ns.State = Ready
			} else {
				ns.State = Blocked
			}
		case v.Result == model.ResultFail:
			// RED outranks BLOCKED: the failure already happened; what the
			// deps look like now does not un-happen it.
			ns.State = Red
		default: // a recorded pass
			if ancestorSeq > ownSeq {
				ns.SeqStale = true
			}
			if in.ArtifactDigest != nil {
				for _, artifact := range n.Artifacts {
					recorded := ""
					if v.ArtifactDigests != nil {
						recorded = v.ArtifactDigests[artifact]
					}
					if recorded == "" || in.ArtifactDigest(artifact) != recorded {
						ns.DigestStale = append(ns.DigestStale, artifact)
					}
				}
				sort.Strings(ns.DigestStale)
			}
			if in.CurrentIntentHashes != nil {
				for cited, recorded := range n.IntentHashes {
					if in.CurrentIntentHashes[cited] != recorded {
						ns.IntentStale = append(ns.IntentStale, cited)
					}
				}
				sort.Strings(ns.IntentStale)
			}
			if ns.SeqStale || len(ns.DigestStale) > 0 || len(ns.IntentStale) > 0 {
				ns.State = Stale
			} else {
				ns.State = Green
			}
		}

		ns.Workable = ns.State == Ready || ns.State == Red || ns.State == Stale
		ns.OnFrontier = ns.Workable && depsAllGreen
		out[id] = ns
	}

	// Cycle members never entered the order: compile refuses cycles, so this
	// is a defensive posture for a hand-damaged graph — BLOCKED, flagged,
	// never workable.
	for id := range adjacency {
		if !ordered[id] {
			out[id] = NodeState{ID: id, State: Blocked, InCycle: true}
		}
	}
	return out
}

// Frontier returns the frontier node ids in deterministic (sorted) order;
// `next` layers critical-path preference on top.
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
