package sync

// Reverify is the converted plan's on-ramp (SddGraph phase-5 debrief, skill
// opportunity #2): after `sdd graph convert`, history grants nothing — every
// completed v1 task is an unverified node until observations exist — and the
// honest first move is one real suite run folded against every gate. The
// pilot did this with a 23-iteration shell loop; this verb is that loop with
// one summary instead of per-node untracked-bucket noise.
//
// Semantics are exactly N sequential syncs, sharing sync.Run's discipline:
// nothing is asserted, refusals are collected per node rather than aborting
// the batch (a red-before-green refusal on one hazard-carrying node must not
// stop the other twenty), and claimed nodes are skipped whole — their
// observations belong to the claim holder. Reverify is meant for quiescent
// graphs; when other claims are active, sync's isolation classification
// records passes as shared-dirty (provisional, STALE) exactly as it would
// for any other shared-tree observation.

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// ReverifyOptions configures one batch pass.
type ReverifyOptions struct {
	PlanDir  string
	RepoRoot string
	// Tests gates fold from this report (JUnit XML or `go test -json`).
	ReportName  string
	ReportBytes []byte
	// Command gates fold from this exit code and captured output.
	CommandExit *int
	CommandLog  []byte
	Provider    provider.Provider
	Now         func() time.Time
	TTL         time.Duration
	// All re-attempts nodes whose current observation is already a fresh
	// pass (GREEN — not STALE, not RED). The default skips them: reverify
	// exists to fold one run against unverified/stale work, not to append a
	// redundant pass observation to nodes that already have current proof.
	All bool
}

// ReverifyOutcome is one node's result in the batch.
type ReverifyOutcome struct {
	Node string `json:"node"`
	// Result is the recorded observation ("pass"/"fail") when one landed.
	Result string `json:"result,omitempty"`
	Seq    int    `json:"seq,omitempty"`
	// Skipped names why the node was not attempted (claimed, review gate,
	// no matching input supplied).
	Skipped string `json:"skipped,omitempty"`
	// Refused carries sync's per-node refusal when the attempt could not
	// record honestly (unresolved tests, red-before-green, ...).
	Refused string `json:"refused,omitempty"`
	// ExpectedAbsence: the refusal's only reason is that this node's
	// declared tests never appeared in the report, and the node has no
	// prior observation — the normal mid-walk state (the run simply had not
	// reached this node's tests yet). Reported like any other refusal, but
	// excluded from Refusals: a genuine failure in the report, or a stale
	// node an observation already exists for, keeps counting.
	ExpectedAbsence bool `json:"expected_absence,omitempty"`
}

// ReverifyResult summarizes the batch.
type ReverifyResult struct {
	Outcomes []ReverifyOutcome `json:"outcomes"`
	Passes   int               `json:"passes"`
	Failures int               `json:"failures"`
	Skips    int               `json:"skips"`
	Refusals int               `json:"refusals"`
	// Untracked is the report's decomposition warning, aggregated ONCE for
	// the whole batch: runner ids no node in the graph declares.
	Untracked []string `json:"untracked,omitempty"`
}

// Reverify folds the supplied inputs against every unclaimed, non-review
// node in dependency order. Dependency order matters: a dep recorded before
// its dependant carries the lower seq, so a clean batch introduces no
// spurious seq-staleness.
func Reverify(o ReverifyOptions) (*ReverifyResult, error) {
	if o.ReportBytes == nil && o.CommandExit == nil {
		return nil, fmt.Errorf("graph reverify: supply --report for tests gates, --command-exit (and --command-log) for command gates, or both")
	}
	g, err := gstore.Load(gstore.PathFor(o.PlanDir))
	if err != nil {
		return nil, err
	}

	adjacency := algorithms.Graph{}
	byID := map[string]*model.Node{}
	for i := range g.Nodes {
		adjacency[g.Nodes[i].ID] = g.Nodes[i].Deps
		byID[g.Nodes[i].ID] = &g.Nodes[i]
	}

	// Cycle preflight: TopoSort silently omits cycle members, and silently
	// skipping nodes is the wrong failure mode for a batch verb that records
	// observations — a cycle means the graph is not ready for reverify.
	if cycles := algorithms.Cycles(adjacency); len(cycles) > 0 {
		return nil, fmt.Errorf("graph reverify: dependency cycle(s) detected: %v; resolve the cycle(s) first", cycles)
	}

	res := &ReverifyResult{}
	if o.ReportBytes != nil {
		if results, err := ParseReport(o.ReportName, o.ReportBytes); err == nil {
			res.Untracked = untracked(g, results)
		}
		// A malformed report surfaces per node below with sync's own
		// error text; the aggregate untracked list is best-effort.
	}

	// Best-effort derived states, used only to skip nodes whose current
	// observation is already a fresh pass (DD-1's digest/intent/input axes
	// wired when the plan's sources resolve; graph-only semantics otherwise
	// — a caller without a repo checkout still gets seq/digest staleness).
	var statesByID map[string]states.NodeState
	if !o.All {
		in := states.Inputs{Graph: g}
		if sources, serr := gcompile.NewSources(filepath.Dir(filepath.Dir(o.PlanDir)), o.RepoRoot, filepath.Base(o.PlanDir)); serr == nil {
			snap := sources.IntentSnapshot()
			in.ArtifactDigest = digest.New(o.RepoRoot).Artifact
			in.CurrentIntentHashes = snap.Hashes()
			in.CurrentInputHashes = sources.InputResolver().GraphHashes(g)
		}
		statesByID = states.Derive(in)
	}

	for _, id := range algorithms.TopoSort(adjacency) {
		n := byID[id]
		out := ReverifyOutcome{Node: id}
		switch {
		case n.Claim != nil:
			out.Skipped = "claimed by " + n.Claim.By + " (the holder owns its observations)"
		case n.Gate.Type == model.GateReview:
			out.Skipped = "review gate (its observation is a frozen review artifact; use `sdd graph review`)"
		case n.Gate.Type == model.GateTests && n.Gate.Evidence == model.EvidenceObservedV1:
			out.Skipped = "requires observed capture (`sdd test run` then `sdd graph sync --attempt`)"
		case !o.All && statesByID[id].State == states.Green:
			out.Skipped = "fresh, skipped (already a current pass; pass --all to re-verify anyway)"
		case n.Gate.Type == model.GateTests && o.ReportBytes == nil:
			out.Skipped = "tests gate, no --report supplied"
		case n.Gate.Type == model.GateCommand && o.CommandExit == nil:
			out.Skipped = "command gate, no --command-exit supplied"
		default:
			runOpts := Options{
				PlanDir: o.PlanDir, RepoRoot: o.RepoRoot, Node: id,
				Provider: o.Provider, Now: o.Now, TTL: o.TTL,
			}
			if n.Gate.Type == model.GateCommand {
				runOpts.CommandExit = o.CommandExit
				runOpts.CommandLog = o.CommandLog
			} else {
				runOpts.ReportName = o.ReportName
				runOpts.ReportBytes = o.ReportBytes
			}
			r, err := Run(runOpts)
			switch {
			case err != nil:
				out.Refused = err.Error()
			case !r.Recorded:
				out.Refused = r.Refusal
				// A node with no observations yet, refused ONLY because its
				// declared tests never appeared in this report, is the
				// normal mid-walk state (the rest of the graph's tests ran,
				// this node's have not been reached) — reported, but not a
				// violation to exit nonzero over. Any ambiguity, or a node
				// that already carries an observation and still cannot be
				// re-verified (a stale node this run could not clear), keeps
				// the refusal counted.
				out.ExpectedAbsence = n.Verification == nil && len(r.Buckets.Ambiguous) == 0 && len(r.Buckets.Unresolved) > 0
			default:
				out.Result = r.Observation.Result
				out.Seq = r.Observation.Seq
			}
		}
		switch {
		case out.Result == model.ResultPass:
			res.Passes++
		case out.Result == model.ResultFail:
			res.Failures++
		case out.Skipped != "":
			res.Skips++
		case out.ExpectedAbsence:
			// Reported, not a violation: see ExpectedAbsence's doc comment.
			res.Skips++
		default:
			res.Refusals++
		}
		res.Outcomes = append(res.Outcomes, out)
	}
	return res, nil
}
