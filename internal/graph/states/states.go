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
	// call. Fail-closed: it also lists justifications that carry no embedded
	// hash and are not exempt decisions (deleted, unlinked, ambiguous, or
	// never-anchored citations).
	IntentStale []string
	// InputStale lists declared inputs whose embedded fingerprint no longer
	// matches the current content — including an input that no longer
	// resolves (deleted, ambiguous section, path now escapes) or that carries
	// no embedded hash at all. Fail-closed: a recorded pass can never derive
	// GREEN against input text it never anchored to, or that has moved.
	InputStale []string
	// IsolationStale: the pass was observed with non-clean isolation
	// (shared-dirty, or an asserted record). Provisionally accepted, never
	// GREEN: the mandatory clean re-verify is what lifts it (DD-7).
	IsolationStale bool
	// RevIncompatible: the latest observation was recorded against an
	// earlier contract revision (a `revise` amendment landed since). It is
	// history, not current proof; the node derives as if unobserved
	// (ReviewDrivenAmendment DD-3).
	RevIncompatible bool
	// ReviewStale lists, for a review node, the reviewed nodes whose
	// contract revision or artifact digests no longer match the recorded
	// reviewed set (ReviewDrivenAmendment DD-9).
	ReviewStale []string

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
	// fingerprint, "" / absent when the id no longer resolves. nil disables
	// the intent axis entirely (a pure caller intentionally not checking
	// intent passes nil for both intent fields).
	CurrentIntentHashes map[string]string
	// CurrentInputHashes maps input key -> the input's current fingerprint,
	// "" / absent when the input no longer resolves. nil disables the input
	// axis entirely.
	CurrentInputHashes map[string]string
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
		case v.EffectiveContractRev() != n.EffectiveContractRev():
			// Recorded against an earlier contract revision: kept as
			// history, ignored as proof. RED included — the old failure
			// says nothing about the revised obligation, and a fresh RED
			// is required before the next GREEN.
			ns.RevIncompatible = true
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
				if n.Gate.Type == model.GateReview {
					// A review gate's observation records the aggregate
					// diff it reviewed: every scope artifact's digest at
					// review time. Drift in ANY of them is ordinary digest
					// staleness — the reviewed diff is no longer the diff
					// on disk (DD-9's gate-STALE rule). Gate nodes declare
					// no artifacts of their own, so this iterates the
					// recorded keys instead.
					for artifact, recorded := range v.ArtifactDigests {
						if in.ArtifactDigest(artifact) != recorded {
							ns.DigestStale = append(ns.DigestStale, artifact)
						}
					}
				}
				sort.Strings(ns.DigestStale)
			}
			if in.CurrentIntentHashes != nil {
				seen := map[string]bool{}
				// Recorded hashes that no longer match their current
				// fingerprint — including a source deleted since the
				// observation (the current map simply has no entry for it).
				for cited, recorded := range n.IntentHashes {
					if recorded == "" {
						continue // An empty entry still needs disposition checking below.
					}
					seen[cited] = true
					if in.CurrentIntentHashes[cited] != recorded {
						ns.IntentStale = append(ns.IntentStale, cited)
					}
				}
				// Fail closed on the actual citation disposition: a
				// recorded-pass node carrying a justification with NO
				// embedded hash is stale. Every citation is fingerprintable
				// — requirements, design decisions, plan decisions, review
				// findings — so fingerprintable-with-no-hash (the split
				// bug), deleted, unlinked, ambiguous, or unresolved (a
				// citation that has since vanished from the current tree)
				// are all stale, and a PASS can never silently derive GREEN
				// against text it never anchored to.
				for _, cited := range n.Justifies {
					if seen[cited] {
						continue
					}
					ns.IntentStale = append(ns.IntentStale, cited)
				}
				sort.Strings(ns.IntentStale)
			}
			if in.CurrentInputHashes != nil {
				// Fail closed on the input disposition: a declared input with
				// no embedded hash, or whose embedded hash no longer matches
				// the current content (including an input that no longer
				// resolves), is stale. There is no exemption shape for inputs.
				for _, spec := range n.Inputs {
					key := model.InputKey(spec)
					recorded := n.InputHashes[key]
					if recorded == "" || in.CurrentInputHashes[key] != recorded {
						ns.InputStale = append(ns.InputStale, key)
					}
				}
				sort.Strings(ns.InputStale)
			}
			if v.Isolation != model.IsolationClean {
				ns.IsolationStale = true
			}
			// A review node's evidence binds to the reviewed set: each
			// reviewed node's contract revision (always) and artifact
			// digests (when a digester is available). A node that left
			// the graph is drift too.
			for reviewedID, ref := range v.Reviewed {
				reviewed, present := byID[reviewedID]
				if !present || reviewed.EffectiveContractRev() != ref.ContractRev {
					ns.ReviewStale = append(ns.ReviewStale, reviewedID)
					continue
				}
				if in.ArtifactDigest != nil {
					for artifact, recorded := range ref.ArtifactDigests {
						if in.ArtifactDigest(artifact) != recorded {
							ns.ReviewStale = append(ns.ReviewStale, reviewedID)
							break
						}
					}
				}
			}
			sort.Strings(ns.ReviewStale)
			if ns.SeqStale || len(ns.DigestStale) > 0 || len(ns.IntentStale) > 0 || len(ns.InputStale) > 0 || ns.IsolationStale || len(ns.ReviewStale) > 0 {
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
