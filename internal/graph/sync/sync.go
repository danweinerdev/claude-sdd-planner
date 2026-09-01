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
		return nil, fmt.Errorf("graph sync: %q is a review gate; its observation is the frozen Aligned review artifact recorded by the phase-4 review flow, not a test report", o.Node)
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

	leaseRenewed := ""
	var recorded *model.Verification
	redAdded := map[string]int{}
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
		// Implicit lease renewal: syncing IS the liveness proof (DD-10).
		if n.Claim != nil && n.Claim.By == o.By {
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
