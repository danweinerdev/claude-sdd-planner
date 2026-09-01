package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// Options configures one sync.
type Options struct {
	PlanDir  string
	RepoRoot string
	Node     string
	// By identifies the caller for the claim check and the implicit lease
	// renewal (DD-10: liveness is proven by observed store activity).
	By string
	// Tests gate: the report file's name (format routing) and bytes.
	ReportName  string
	ReportBytes []byte
	// Command gate: the check command's exit code and captured output.
	CommandExit *int
	CommandLog  []byte
	// Provider supplies isolation classification and provenance; nil means
	// the plain posture.
	Provider provider.Provider
	Now      func() time.Time
	TTL      time.Duration
}

// Buckets is the honest reconciliation report.
type Buckets struct {
	Updated    []string `json:"updated,omitempty"`
	Unresolved []string `json:"unresolved,omitempty"`
	Untracked  []string `json:"untracked,omitempty"`
	Ambiguous  []string `json:"ambiguous,omitempty"`
}

// Result is one sync's outcome. Recorded=false with a Refusal is a SUCCESSFUL
// reconciliation that found the node unverifiable (unresolved or ambiguous
// declared tests) — the buckets say exactly why, and nothing was guessed.
type Result struct {
	Node         string             `json:"node"`
	Recorded     bool               `json:"recorded"`
	Observation  *model.Verification `json:"observation,omitempty"`
	Buckets      Buckets            `json:"buckets"`
	RedSeqsAdded map[string]int     `json:"red_seqs_added,omitempty"`
	LeaseRenewed string             `json:"lease_renewed,omitempty"`
	Refusal      string             `json:"refusal,omitempty"`
	LogPath      string             `json:"log,omitempty"`
	// Merged: the observation was a clean pass and the claim completed in
	// the same cycle — claim cleared, workspace released (DD-10's atomic
	// sequence). A pass that records without merging (shared-dirty
	// isolation, or an unclaimed re-verify) reports Merged=false.
	Merged bool `json:"merged,omitempty"`
	// WorkspaceReleased names the workspace handle torn down on merge.
	WorkspaceReleased string `json:"workspace_released,omitempty"`
}

func (o *Options) fill() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.TTL <= 0 {
		o.TTL = 30 * time.Minute
	}
}

// Run records one observation from mechanical input. It never asserts: a
// tests gate needs a report, a command gate needs an exit code, a review
// gate is phase 4's flow, and an unresolvable report leaves the node
// unverified with the buckets explaining why.
func Run(o Options) (*Result, error) {
	o.fill()
	graphPath := gstore.PathFor(o.PlanDir)
	g, err := gstore.Load(graphPath)
	if err != nil {
		return nil, err
	}
	node := g.NodeByID(o.Node)
	if node == nil {
		return nil, fmt.Errorf("graph sync: node %q does not exist", o.Node)
	}

	// The claim check runs before any parsing: a stale claimant's late sync
	// is refused whole (their takeover successor owns the node now).
	if node.Claim != nil {
		if o.By == "" {
			return nil, fmt.Errorf("graph sync: %q is claimed by %q; pass --by to sync as its holder", o.Node, node.Claim.By)
		}
		if node.Claim.By != o.By {
			return nil, fmt.Errorf("graph sync: %q is claimed by %q, not %q; a stale claim cannot sync (its lease was taken over)", o.Node, node.Claim.By, o.By)
		}
	}

	res := &Result{Node: o.Node}
	var result string
	var reportDigest string
	var failedTests []string

	switch node.Gate.Type {
	case model.GateReview:
		return nil, fmt.Errorf("graph sync: %q is a review gate; record its frozen Aligned review artifact with `sdd graph review --plan <plan> --node %s --artifact <review path>`, not a test report", o.Node, o.Node)
	case model.GateUnspecified:
		return nil, fmt.Errorf("graph sync: %q carries the unspecified-gate conversion sentinel; specify its gate (it should not have compiled)", o.Node)
	case model.GateCommand:
		if o.CommandExit == nil {
			return nil, fmt.Errorf("graph sync: %q is a command gate; pass --command-exit (and --command-log with the captured output)", o.Node)
		}
		if o.ReportBytes != nil {
			return nil, fmt.Errorf("graph sync: %q is a command gate; --report does not apply", o.Node)
		}
		logPath := filepath.Join(o.PlanDir, gstore.GraphDirName, "logs", o.Node+".log")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, err
		}
		if err := istore.WriteAtomic(logPath, string(o.CommandLog)); err != nil {
			return nil, err
		}
		res.LogPath = logPath
		reportDigest = digest.Bytes(o.CommandLog)
		if *o.CommandExit == 0 {
			result = model.ResultPass
		} else {
			result = model.ResultFail
		}
	case model.GateTests:
		if o.CommandExit != nil {
			return nil, fmt.Errorf("graph sync: %q is a tests gate; --command-exit does not apply", o.Node)
		}
		if len(o.ReportBytes) == 0 {
			return nil, fmt.Errorf("graph sync: %q is a tests gate; pass --report with the runner's output file", o.Node)
		}
		results, err := ParseReport(o.ReportName, o.ReportBytes)
		if err != nil {
			return nil, err
		}
		reportDigest = digest.Bytes(o.ReportBytes)
		res.Buckets.Untracked = untracked(g, results)

		anyFail := false
		for _, t := range node.Gate.Tests {
			fold := FoldFor(t.ID, results)
			switch {
			case fold.Ambiguous:
				res.Buckets.Ambiguous = append(res.Buckets.Ambiguous, t.ID)
			case !fold.Resolved:
				res.Buckets.Unresolved = append(res.Buckets.Unresolved, t.ID)
			default:
				res.Buckets.Updated = append(res.Buckets.Updated, t.ID)
				if fold.Outcome == Fail {
					anyFail = true
					failedTests = append(failedTests, t.ID)
				}
			}
		}
		sortBuckets(&res.Buckets)
		if len(res.Buckets.Ambiguous) > 0 || len(res.Buckets.Unresolved) > 0 {
			res.Refusal = "the node stays unverified: " + explainBuckets(res.Buckets)
			return res, nil
		}
		if anyFail {
			result = model.ResultFail
		} else {
			result = model.ResultPass
		}
	default:
		return nil, fmt.Errorf("graph sync: %q has gate type %q, which sync does not understand", o.Node, node.Gate.Type)
	}

	// Provenance and isolation come from the provider, computed against the
	// claimed workspace (or the shared tree when unclaimed).
	prov := o.Provider
	if prov == nil {
		prov = provider.Detect(o.RepoRoot, o.PlanDir)
	}
	handle := ""
	if node.Claim != nil {
		handle = node.Claim.Workspace
	}
	activeClaims := 0
	for i := range g.Nodes {
		if g.Nodes[i].Claim != nil {
			activeClaims++
		}
	}
	provenance, err := prov.Provenance(handle)
	if err != nil {
		return nil, fmt.Errorf("graph sync: reading provenance: %w", err)
	}
	isolation := prov.Isolation(handle, activeClaims)

	// The workspace is where the observed bytes live: digest artifacts
	// there, not in the shared tree, when the claim carries one.
	digestRoot := o.RepoRoot
	if handle != "" {
		if filepath.IsAbs(handle) {
			digestRoot = handle
		} else {
			digestRoot = filepath.Join(o.RepoRoot, filepath.FromSlash(handle))
		}
	}
	digester := digest.New(digestRoot)
	artifactDigests := map[string]string{}
	for _, a := range node.Artifacts {
		if d := digester.Artifact(a); d != "" {
			artifactDigests[a] = d
		}
	}

	// Merge-gate preconditions bind the RECORDING of a pass (DD-5): a pass
	// that fails them is refused whole with the failing condition named, so
	// no dependant ever unblocks on unproven work. Red runs record freely —
	// they are how the proof is produced.
	if result == model.ResultPass {
		if isolation == model.IsolationAsserted {
			return nil, fmt.Errorf("graph sync: %q: an asserted observation is refused by default; produce a real report", o.Node)
		}
		var unproven []string
		for _, t := range node.Gate.Tests {
			if len(t.Satisfies) == 0 {
				continue
			}
			if _, seen := node.RedSeqs[t.ID]; !seen {
				unproven = append(unproven, t.ID)
			}
		}
		if len(unproven) > 0 {
			return nil, fmt.Errorf("graph sync: red-before-green: hazard-discharging test(s) %v have never been observed failing; run them against the broken or unimplemented state and sync that failing report first — a test that passes against both correct and broken code guards nothing", unproven)
		}
		if handle != "" {
			if clean, dirty, cleanErr := vcs.Detect(digestRoot).Clean(); cleanErr == nil && !clean {
				example := ""
				if len(dirty) > 0 {
					example = " (e.g. " + dirty[0] + ")"
				}
				return nil, fmt.Errorf("graph sync: workspace %s has %d uncommitted path(s)%s; commit the complete slice, then re-sync the passing report — the revision anchor must name the tested bytes", handle, len(dirty), example)
			}
		}
	}

	leaseRenewed := ""
	var recorded *model.Verification
	redAdded := map[string]int{}
	merged := false
	if _, err := gstore.Update(graphPath, func(fresh *model.Graph) error {
		n := fresh.NodeByID(o.Node)
		if n == nil {
			return fmt.Errorf("graph sync: node %q vanished mid-sync", o.Node)
		}
		if n.Claim != nil && n.Claim.By != o.By {
			return fmt.Errorf("graph sync: %q was claimed by %q while this sync ran", o.Node, n.Claim.By)
		}
		fresh.SeqCounter++
		seq := fresh.SeqCounter
		v := &model.Verification{
			Result:          result,
			Seq:             seq,
			ArtifactDigests: artifactDigests,
			ReportDigest:    reportDigest,
			Isolation:       isolation,
			Provenance:      provenance,
		}
		n.Verification = v
		// red_seq: the first observed failure per declared test, recorded
		// once and kept — the merge gate's red-before-green reads it (DD-5).
		for _, id := range failedTests {
			if n.RedSeqs == nil {
				n.RedSeqs = map[string]int{}
			}
			if _, seen := n.RedSeqs[id]; !seen {
				n.RedSeqs[id] = seq
				redAdded[id] = seq
			}
		}
		merged = false
		switch {
		case result == model.ResultPass && isolation == model.IsolationClean &&
			n.Claim != nil && n.Claim.By == o.By:
			// The atomic completion (DD-10): a clean pass by the holder
			// merges — observation recorded, claim cleared, workspace
			// released after the write lands. A pass with shared-dirty
			// isolation records provisionally instead (STALE, never GREEN)
			// and keeps the claim for the mandatory clean re-verify.
			n.Claim = nil
			merged = true
		case n.Claim != nil && n.Claim.By == o.By:
			// Implicit lease renewal: syncing IS the liveness proof (DD-10).
			n.Claim.LeaseExpires = o.Now().Add(o.TTL).UTC().Format(time.RFC3339)
			leaseRenewed = n.Claim.LeaseExpires
		}
		recorded = v
		return nil
	}); err != nil {
		return nil, err
	}
	res.Recorded = true
	res.Observation = recorded
	res.RedSeqsAdded = redAdded
	res.LeaseRenewed = leaseRenewed
	res.Merged = merged
	if merged && handle != "" {
		if err := prov.Release(handle); err != nil {
			return res, fmt.Errorf("graph sync: merged, but workspace %s could not be released (reap it with `sdd graph gc`): %w", handle, err)
		}
		res.WorkspaceReleased = handle
	}
	return res, nil
}

// untracked lists report ids no node in the graph declares, directly or as
// a parameter case — a decomposition warning, never an error.
func untracked(g *model.Graph, results []TestResult) []string {
	var out []string
	for _, r := range results {
		claimed := false
		for i := range g.Nodes {
			for _, t := range g.Nodes[i].Gate.Tests {
				if r.ID == t.ID || caseOf(t.ID, r.ID) {
					claimed = true
					break
				}
			}
			if claimed {
				break
			}
		}
		if !claimed {
			out = append(out, r.ID)
		}
	}
	sort.Strings(out)
	return out
}

func sortBuckets(b *Buckets) {
	sort.Strings(b.Updated)
	sort.Strings(b.Unresolved)
	sort.Strings(b.Ambiguous)
}

func explainBuckets(b Buckets) string {
	s := ""
	if len(b.Unresolved) > 0 {
		s += fmt.Sprintf("%d declared test(s) never ran or were withheld by skips (%v)", len(b.Unresolved), b.Unresolved)
	}
	if len(b.Ambiguous) > 0 {
		if s != "" {
			s += "; "
		}
		s += fmt.Sprintf("%d declared test(s) both passed and failed in one report (%v) — never guessed at", len(b.Ambiguous), b.Ambiguous)
	}
	return s
}
