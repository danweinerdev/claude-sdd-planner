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
	"fmt"
	"reflect"
	"sort"
	"strings"

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
	// node's own. Legacy axis: it applies only to observations recorded
	// before dependency digests existed (VerificationFreshness DD-1);
	// an observation carrying dependency_digests never derives it.
	SeqStale bool
	// DependencyStale lists direct dependencies whose artifact bytes differ
	// from what this node's observation recorded it exercised, or whose
	// own derived state is STALE by dependency. A dependency re-verified
	// against identical bytes never appears here.
	DependencyStale []string
	// AnchorAdvisory lists citations and input keys whose compile-time
	// anchor on the node differs from the fingerprint the observation
	// recorded: the text changed since the node was written for it and a
	// judgment is owed (`sdd graph acknowledge`). Advisory only — it never
	// withholds GREEN (VerificationFreshness DD-2, DD-5).
	AnchorAdvisory []string
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
	// ObservedEvidenceStale means an observed-v1 pass lacks its captured
	// protocol marker or matching immutable consumed-attempt index entry.
	ObservedEvidenceStale bool
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

// ReviewScope derives the current increment a review gate is responsible for.
// Only a currently GREEN inner full review subtracts its region. The graph-only
// derive intentionally disables external digest/intent/input axes; callers that
// already have a complete state snapshot should use ReviewScopeFromStates.
func ReviewScope(g *model.Graph, gateID string) ([]string, error) {
	return ReviewScopeFromStates(g, gateID, Derive(Inputs{Graph: g}))
}

// LegacyReviewedSet materializes the reviewed set a pre-reviewed-set
// observation (Reviewed == nil) implicitly covered: the review's current
// scope at the current contract revisions, or every direct dep when the
// scope is empty (everything covered by inner GREEN reviews) — never a
// single dep, which would misstate what was reviewed. Callers pin it onto
// the observation before an amendment or split touches the scope, so the
// old proof becomes REVIEW-STALE by exact comparison afterward.
func LegacyReviewedSet(g *model.Graph, reviewID string, scope []string, artifactDigest func(string) string) map[string]model.ReviewedRef {
	if len(scope) == 0 {
		if r := g.NodeByID(reviewID); r != nil {
			scope = append([]string(nil), r.Deps...)
		}
	}
	out := make(map[string]model.ReviewedRef, len(scope))
	for _, id := range scope {
		prior := g.NodeByID(id)
		if prior == nil {
			continue
		}
		ref := model.ReviewedRef{ContractRev: prior.EffectiveContractRev()}
		// Members' bytes as they stand now: the legacy pass reviewed the
		// tree at the time, and nothing has been amended yet at this point,
		// so the current digests are the best available statement of what
		// it covered — and they give per-member attribution afterward.
		if artifactDigest != nil {
			for _, a := range prior.Artifacts {
				if d := artifactDigest(a); d != "" {
					if ref.ArtifactDigests == nil {
						ref.ArtifactDigests = map[string]string{}
					}
					ref.ArtifactDigests[a] = d
				}
			}
		}
		out[id] = ref
	}
	return out
}

// ReviewScopeFromStates derives review scope from an already-computed state
// snapshot. Keeping this below package review lets Derive and completion
// closure use the same current-GREEN subtraction rule without an import cycle.
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
	delete(closure, gateID) // preserve start-excluded semantics even in a cycle
	covered := map[string]bool{}
	for i := range g.Nodes {
		inner := &g.Nodes[i]
		if inner.ID == gateID || !closure[inner.ID] || inner.Gate.Type != model.GateReview || inner.Gate.Lanes != nil {
			continue
		}
		if !current(inner.ID) {
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

// Closed derives completion-grade coverage from current node states. Full
// reviews close only their exact current increments; integration acceptance
// closes only after all of its upstream work is already covered by current
// full-review evidence (or an earlier qualifying acceptance).
func Closed(g *model.Graph, statesByID map[string]NodeState) map[string]bool {
	closed := map[string]bool{}
	for i := range g.Nodes {
		review := &g.Nodes[i]
		if review.Gate.Type != model.GateReview || review.Gate.Lanes != nil || review.Verification == nil || review.Verification.Result != model.ResultPass || statesByID[review.ID].State != Green {
			continue
		}
		closed[review.ID] = true
		scope, err := ReviewScopeFromStates(g, review.ID, statesByID)
		if err != nil {
			continue
		}
		for _, id := range scope {
			if statesByID[id].State == Green {
				closed[id] = true
			}
		}
	}

	// Iterate to a fixed point so acceptance chains do not depend on storage
	// order. dependencyClosure excludes its start node.
	changed := true
	for changed {
		changed = false
		for i := range g.Nodes {
			acceptance := &g.Nodes[i]
			if closed[acceptance.ID] || acceptance.EffectiveRole() != model.RoleIntegrationAcceptance || statesByID[acceptance.ID].State != Green {
				continue
			}
			upstream := dependencyClosure(g, acceptance.ID)
			covered := len(upstream) > 0
			for _, id := range upstream {
				if !closed[id] {
					covered = false
					break
				}
			}
			if covered {
				closed[acceptance.ID] = true
				changed = true
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
			if n.Gate.Type == model.GateTests && n.Gate.Evidence == model.EvidenceReportedV1 {
				if !validReportedPass(n, v) {
					ns.ObservedEvidenceStale = true
				}
			}
			if n.Gate.Type == model.GateTests && n.Gate.Evidence == model.EvidenceObservedV1 {
				if v.Attempt == nil || v.Attempt.Protocol != model.EvidenceObservedV1 || v.Attempt.ID == "" || !validDigestIdentity(v.Attempt.Digest) || !validDigestIdentity(v.Attempt.CandidateDigest) {
					ns.ObservedEvidenceStale = true
				} else if consumed, ok := n.ConsumedAttempts[v.Attempt.ID]; !ok || consumed.Digest != v.Attempt.Digest || consumed.Seq != v.Seq || consumed.Result != v.Result || !reflect.DeepEqual(consumed.RedEvidence, v.RedEvidence) {
					ns.ObservedEvidenceStale = true
				}
			}
			if v.DependencyDigests == nil {
				// Legacy observation: no record of what it exercised
				// below it, so observation order is the only proxy.
				if ancestorSeq > ownSeq {
					ns.SeqStale = true
				}
			} else {
				// Content identity for dependencies (DD-1): compare the
				// bytes the run exercised with the bytes on disk now, and
				// ripple a dependency's own dependency-staleness upward.
				for _, dep := range n.Deps {
					recorded, present := v.DependencyDigests[dep]
					depNode, exists := byID[dep]
					switch {
					case !exists:
						ns.DependencyStale = append(ns.DependencyStale, dep)
					case !present:
						// A dependency added after the observation (an
						// extend, a rewire): the run never exercised it. A
						// review gate declares no Node.Artifacts of its own
						// (its identity is the recorded scope-diff digests), so
						// it counts here on gate type, not on Artifacts.
						if len(depNode.Artifacts) > 0 || depNode.Gate.Type == model.GateReview {
							ns.DependencyStale = append(ns.DependencyStale, dep)
						}
					case len(out[dep].DependencyStale) > 0:
						ns.DependencyStale = append(ns.DependencyStale, dep)
					case depNode.Gate.Type == model.GateReview && out[dep].State != Green:
						// A review gate carries no Node.Artifacts of its own
						// (its identity is the recorded scope-diff digests),
						// so the literal per-artifact loop below has nothing
						// to walk. Any non-GREEN state on it — most commonly
						// DigestStale over its recorded scope — is the signal
						// a consumer must ripple on instead. This is what
						// gives a command gate (e.g. a full-suite gate
						// depending on a review gate) the same
						// dependency-digest staleness a tests gate gets.
						ns.DependencyStale = append(ns.DependencyStale, dep)
					case in.ArtifactDigest != nil:
						for _, artifact := range depNode.Artifacts {
							if in.ArtifactDigest(artifact) != recorded[artifact] {
								ns.DependencyStale = append(ns.DependencyStale, dep)
								break
							}
						}
					}
				}
				sort.Strings(ns.DependencyStale)
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
				// The run's own snapshot is the identity when it exists
				// (DD-2); the node's compile anchor is the fallback for
				// older observations, and the difference between the two is
				// an advisory, never staleness.
				recordedIntent := n.IntentHashes
				if v.IntentHashes != nil {
					recordedIntent = v.IntentHashes
					for cited, ran := range v.IntentHashes {
						if anchor := n.IntentHashes[cited]; anchor != "" && anchor != ran {
							ns.AnchorAdvisory = append(ns.AnchorAdvisory, cited)
						}
					}
				}
				// Recorded hashes that no longer match their current
				// fingerprint — including a source deleted since the
				// observation (the current map simply has no entry for it).
				for cited, recorded := range recordedIntent {
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
				recordedInputs := n.InputHashes
				if v.InputHashes != nil {
					recordedInputs = v.InputHashes
					for key, ran := range v.InputHashes {
						if anchor := n.InputHashes[key]; anchor != "" && anchor != ran {
							ns.AnchorAdvisory = append(ns.AnchorAdvisory, key)
						}
					}
				}
				for _, spec := range n.Inputs {
					key := model.InputKey(spec)
					recorded := recordedInputs[key]
					if recorded == "" || in.CurrentInputHashes[key] != recorded {
						ns.InputStale = append(ns.InputStale, key)
					}
				}
				sort.Strings(ns.InputStale)
			}
			if v.Isolation != model.IsolationClean {
				ns.IsolationStale = true
			}
			// A review node's evidence binds to the exact current scope, not
			// merely to whichever recorded entries still happen to match.
			// This catches scope growth (including a direct dep appended by an
			// extend amendment) and scope shrinkage before old evidence can
			// remain GREEN. A nil Reviewed map is the pre-reviewed-set wire
			// shape. Preserve its historical meaning for existing graphs;
			// amendment code materializes that assumed legacy scope before an
			// extension, so later scope growth still invalidates the old proof.
			// A non-nil set must match exactly.
			reviewStale := map[string]bool{}
			if n.Gate.Type == model.GateReview && v.Reviewed == nil {
				// Legacy shape (no reviewed set): current only while nothing
				// in its scope has been revised. A revision above 1 anywhere
				// in scope means the reviewed contract moved under an
				// observation that cannot say which revision it covered, so
				// it is history, not proof (ReviewDrivenAmendment DD-9).
				if currentScope, err := reviewScope(g, n.ID, func(id string) bool { return out[id].State == Green }); err == nil {
					for _, reviewedID := range currentScope {
						if m, present := byID[reviewedID]; present && m.EffectiveContractRev() > 1 {
							reviewStale[reviewedID] = true
						}
					}
				}
			}
			if n.Gate.Type == model.GateReview && v.Reviewed != nil {
				if currentScope, err := reviewScope(g, n.ID, func(id string) bool { return out[id].State == Green }); err == nil {
					current := map[string]bool{}
					for _, reviewedID := range currentScope {
						current[reviewedID] = true
						if _, present := v.Reviewed[reviewedID]; !present {
							reviewStale[reviewedID] = true
						}
					}
					for reviewedID := range v.Reviewed {
						if !current[reviewedID] {
							reviewStale[reviewedID] = true
						}
					}
				}
			}
			// Every recorded member must still name the same contract revision
			// and artifact bytes. A node that left the graph is drift too.
			for reviewedID, ref := range v.Reviewed {
				reviewed, present := byID[reviewedID]
				if !present || reviewed.EffectiveContractRev() != ref.ContractRev {
					reviewStale[reviewedID] = true
					continue
				}
				if in.ArtifactDigest != nil {
					for artifact, recorded := range ref.ArtifactDigests {
						if in.ArtifactDigest(artifact) != recorded {
							reviewStale[reviewedID] = true
							break
						}
					}
				}
			}
			for reviewedID := range reviewStale {
				ns.ReviewStale = append(ns.ReviewStale, reviewedID)
			}
			sort.Strings(ns.ReviewStale)
			sort.Strings(ns.AnchorAdvisory)
			if ns.SeqStale || len(ns.DependencyStale) > 0 || len(ns.DigestStale) > 0 || len(ns.IntentStale) > 0 || len(ns.InputStale) > 0 || ns.IsolationStale || ns.ObservedEvidenceStale || len(ns.ReviewStale) > 0 {
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

func validDigestIdentity(s string) bool {
	if !strings.HasPrefix(s, "sha256:") || len(s) != 71 {
		return false
	}
	for _, c := range s[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validReportedPass(n *model.Node, v *model.Verification) bool {
	if v == nil || v.Result != model.ResultPass || v.Report == nil || v.Report.Phase != "green" ||
		v.Report.Protocol != model.EvidenceReportedV1 || !validDigestIdentity(v.Report.ID) ||
		!validDigestIdentity(v.Report.ReportDigest) || !validDigestIdentity(v.Report.MetadataDigest) ||
		!validDigestIdentity(v.Report.CandidateDigest) || v.Report.ClaimInstance == "" || v.Report.By == "" {
		return false
	}
	e, ok := n.ReportEvidence[v.Report.ID]
	if !ok || e.Protocol != v.Report.Protocol || e.ReportDigest != v.Report.ReportDigest ||
		e.MetadataDigest != v.Report.MetadataDigest || e.ClaimInstance != v.Report.ClaimInstance ||
		e.By != v.Report.By || e.Phase != v.Report.Phase || e.Phase != "green" || e.RedKind != "" || e.Fault != "" ||
		e.CandidateDigest != v.Report.CandidateDigest || !validDigestIdentity(e.CompatibilityHash) {
		return false
	}
	c, ok := n.ConsumedReports[v.Report.ID]
	if !ok || c.Protocol != v.Report.Protocol || c.ReportDigest != v.Report.ReportDigest ||
		c.MetadataDigest != v.Report.MetadataDigest || c.ClaimInstance != v.Report.ClaimInstance ||
		c.By != v.Report.By || c.Phase != v.Report.Phase || c.CandidateDigest != v.Report.CandidateDigest ||
		c.Seq != v.Seq || c.Result != v.Result || !reflect.DeepEqual(c.RedEvidence, v.RedEvidence) {
		return false
	}
	for _, test := range n.Gate.Tests {
		if len(test.Satisfies) == 0 {
			continue
		}
		qid := test.Package + "::" + test.ID
		if _, ok := v.RedEvidence[qid]; !ok {
			return false
		}
	}
	for qid, red := range v.RedEvidence {
		if red.ReportID == "" || red.AttemptID != "" || red.Seq <= 0 || red.Seq >= v.Seq ||
			!validDigestIdentity(red.ReportID) || !validDigestIdentity(red.CompatibilityKey) {
			return false
		}
		redConsumed, ok := n.ConsumedReports[red.ReportID]
		if !ok || redConsumed.Protocol != model.EvidenceReportedV1 || redConsumed.Result != model.ResultFail ||
			redConsumed.Phase != "red" || redConsumed.Seq != red.Seq || redConsumed.RedEvidence[qid] != red {
			return false
		}
		redEntry, ok := n.ReportEvidence[red.ReportID]
		if !ok || redEntry.Protocol != redConsumed.Protocol || redEntry.ReportDigest != redConsumed.ReportDigest ||
			redEntry.MetadataDigest != redConsumed.MetadataDigest || redEntry.ClaimInstance != redConsumed.ClaimInstance ||
			redEntry.By != redConsumed.By || redEntry.Phase != redConsumed.Phase || redEntry.CandidateDigest != redConsumed.CandidateDigest ||
			redEntry.RedKind != red.Kind || redEntry.Fault != red.Fault || !validDigestIdentity(redEntry.CompatibilityHash) {
			return false
		}
	}
	return true
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
