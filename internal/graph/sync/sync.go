package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testevidence"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

type Options struct {
	PlanDir, RepoRoot, Node, By string
	ReportName                  string
	ReportBytes                 []byte
	ReportExit                  *int
	RedKind, Fault              string
	CommandExit                 *int
	CommandLog                  []byte
	Provider                    provider.Provider
	Now                         func() time.Time
	TTL                         time.Duration
	beforePublish               func() error
	reverify                    bool
}

type Buckets struct {
	Updated    []string `json:"updated,omitempty"`
	Unresolved []string `json:"unresolved,omitempty"`
	Untracked  []string `json:"untracked,omitempty"`
	Ambiguous  []string `json:"ambiguous,omitempty"`
}

type Result struct {
	Node              string              `json:"node"`
	Recorded          bool                `json:"recorded"`
	Observation       *model.Verification `json:"observation,omitempty"`
	Buckets           Buckets             `json:"buckets"`
	RedSeqsAdded      map[string]int      `json:"red_seqs_added,omitempty"`
	LeaseRenewed      string              `json:"lease_renewed,omitempty"`
	Refusal           string              `json:"refusal,omitempty"`
	LogPath           string              `json:"log,omitempty"`
	Merged            bool                `json:"merged,omitempty"`
	WorkspaceReleased string              `json:"workspace_released,omitempty"`
	Historical        bool                `json:"historical,omitempty"`
}

func (o *Options) fill() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.TTL <= 0 {
		o.TTL = 30 * time.Minute
	}
}

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
	if node.Claim != nil {
		if o.By == "" {
			return nil, fmt.Errorf("graph sync: %q is claimed by %q; pass --by to sync as its holder", o.Node, node.Claim.By)
		}
		if node.Claim.By != o.By {
			return nil, fmt.Errorf("graph sync: %q is claimed by %q, not %q; a stale claim cannot sync", o.Node, node.Claim.By, o.By)
		}
	}
	evaluatedSnapshot := node.ProofSnapshot()
	evaluatedWorkspace := ""
	if node.Claim != nil {
		evaluatedWorkspace = node.Claim.Workspace
	}
	res := &Result{Node: o.Node}
	var result, reportDigest string
	var failedTests []string
	switch node.Gate.Type {
	case model.GateReview:
		return nil, fmt.Errorf("graph sync: %q is a review gate; record its frozen Aligned review artifact with `sdd graph review`", o.Node)
	case model.GateUnspecified:
		return nil, fmt.Errorf("graph sync: %q carries the unspecified-gate conversion sentinel", o.Node)
	case model.GateCommand:
		if o.CommandExit == nil {
			return nil, fmt.Errorf("graph sync: %q is a command gate; pass --command-exit", o.Node)
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
		res.LogPath, reportDigest = logPath, commandDigest(*o.CommandExit, o.CommandLog)
		if *o.CommandExit == 0 {
			result = model.ResultPass
		} else {
			result = model.ResultFail
		}
	case model.GateTests:
		if o.CommandExit != nil {
			return nil, fmt.Errorf("graph sync: %q is a tests gate; --command-exit does not apply", o.Node)
		}
		if len(node.Gate.Tests) == 0 {
			return nil, fmt.Errorf("graph sync: %q has an empty tests gate", o.Node)
		}
		if len(o.ReportBytes) == 0 {
			return nil, fmt.Errorf("graph sync: %q is a tests gate; pass --report", o.Node)
		}
		reportDigest = bytesDigest(o.ReportBytes)
		if !o.reverify && sameObservation(node, reportDigest) {
			return &Result{Node: o.Node, Recorded: true, Historical: true, Observation: node.Verification}, nil
		}
		qualified := false
		for _, test := range node.Gate.Tests {
			qualified = qualified || test.Package != ""
		}
		if qualified {
			selected := make([]testevidence.SelectedTest, 0, len(node.Gate.Tests))
			for _, test := range node.Gate.Tests {
				selected = append(selected, testevidence.SelectedTest{Package: test.Package, ID: test.ID, File: test.File})
			}
			var parsed testevidence.Report
			if o.ReportExit != nil {
				parsed, err = testevidence.ParseGoReport(o.ReportBytes, selected, *o.ReportExit)
			} else {
				parsed, err = testevidence.ParseGoReportWithoutExit(o.ReportBytes, selected)
			}
			if err != nil {
				res.Refusal = "native report did not provide a valid selected-test run: " + err.Error()
				return res, nil
			}
			result = parsed.Result
			for _, tr := range parsed.Tests {
				res.Buckets.Updated = append(res.Buckets.Updated, tr.QualifiedID)
				if tr.Outcome == model.ResultFail {
					failedTests = append(failedTests, tr.ID)
				}
			}
		} else {
			results, parseErr := ParseReport(o.ReportName, o.ReportBytes)
			if parseErr != nil {
				return nil, parseErr
			}
			res.Buckets.Untracked = untracked(g, results)
			anyFail := false
			for _, test := range node.Gate.Tests {
				fold := FoldFor(test.ID, results)
				switch {
				case fold.Ambiguous:
					res.Buckets.Ambiguous = append(res.Buckets.Ambiguous, test.ID)
				case !fold.Resolved:
					res.Buckets.Unresolved = append(res.Buckets.Unresolved, test.ID)
				default:
					res.Buckets.Updated = append(res.Buckets.Updated, test.ID)
					if fold.Outcome == Fail {
						anyFail = true
						failedTests = append(failedTests, test.ID)
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
		}
	default:
		return nil, fmt.Errorf("graph sync: %q has gate type %q", o.Node, node.Gate.Type)
	}
	if result != model.ResultFail && (o.RedKind != "" || o.Fault != "") {
		return nil, fmt.Errorf("graph sync: --red-kind and --fault apply only to a failing report")
	}
	if result == model.ResultFail {
		switch o.RedKind {
		case "":
			if o.Fault != "" {
				return nil, fmt.Errorf("graph sync: --fault requires --red-kind sensitivity")
			}
		case "baseline":
			if o.Fault != "" {
				return nil, fmt.Errorf("graph sync: baseline cannot carry --fault")
			}
		case "sensitivity":
			if o.Fault == "" {
				return nil, fmt.Errorf("graph sync: sensitivity requires nonempty --fault")
			}
		default:
			return nil, fmt.Errorf("graph sync: --red-kind must be baseline or sensitivity")
		}
	}
	prov := o.Provider
	if prov == nil {
		prov, err = provider.DetectChecked(o.RepoRoot, o.PlanDir)
		if err != nil {
			return nil, fmt.Errorf("graph sync: %w", err)
		}
	}
	handle := evaluatedWorkspace
	provenance, err := prov.Provenance(handle)
	if err != nil {
		return nil, fmt.Errorf("graph sync: reading provenance: %w", err)
	}
	activeClaims := 0
	for i := range g.Nodes {
		if g.Nodes[i].Claim != nil {
			activeClaims++
		}
	}
	isolation := prov.Isolation(handle, activeClaims)
	var dirtyPaths []string
	workspaceRoot := o.RepoRoot
	if handle != "" {
		if filepath.IsAbs(handle) {
			workspaceRoot = handle
		} else {
			workspaceRoot = filepath.Join(o.RepoRoot, filepath.FromSlash(handle))
		}
	}
	if isolation == model.IsolationSharedDirty {
		if repo := vcs.Detect(workspaceRoot); repo != nil {
			if _, dirty, e := repo.Clean(); e == nil {
				dirtyPaths = dirty
			}
		}
	}
	if result == model.ResultPass {
		if isolation == model.IsolationAsserted {
			return nil, fmt.Errorf("graph sync: %q: an asserted observation is refused", o.Node)
		}
		var unproven []string
		for _, test := range node.Gate.Tests {
			if len(test.Satisfies) > 0 {
				if _, seen := node.RedSeqs[test.ID]; !seen {
					unproven = append(unproven, test.ID)
				}
			}
		}
		if len(unproven) > 0 {
			return nil, fmt.Errorf("graph sync: red-before-green: hazard-discharging test(s) %v have never been observed failing", unproven)
		}
		if handle != "" {
			repo, e := vcs.DetectChecked(workspaceRoot)
			if e != nil {
				return nil, fmt.Errorf("graph sync: workspace cleanliness could not be established: %w", e)
			}
			clean, dirty, e := repo.Clean()
			if errors.Is(e, vcs.ErrUnsupported) {
				clean, e = true, nil
			}
			if e != nil {
				return nil, e
			}
			if !clean {
				return nil, fmt.Errorf("graph sync: workspace has %d uncommitted path(s)", len(dirty))
			}
		}
	}
	var recorded *model.Verification
	redAdded := map[string]int{}
	merged, hookRan := false, false
	leaseRenewed := ""
	historical := false
	if _, err := gstore.Update(graphPath, func(fresh *model.Graph) error {
		merged = false
		redAdded = map[string]int{}
		leaseRenewed = ""
		historical = false
		n := fresh.NodeByID(o.Node)
		if n == nil {
			return fmt.Errorf("graph sync: node vanished")
		}
		if n.Claim != nil && n.Claim.By != o.By {
			return fmt.Errorf("graph sync: %q was claimed by %q while this sync ran", o.Node, n.Claim.By)
		}
		if !o.reverify && sameObservation(n, reportDigest) {
			recorded = n.Verification
			historical = true
			return nil
		}
		workspace := ""
		if n.Claim != nil {
			workspace = n.Claim.Workspace
		}
		if n.ProofSnapshot() != evaluatedSnapshot || workspace != evaluatedWorkspace {
			return fmt.Errorf("graph sync: %q's contract changed while its report was evaluated", o.Node)
		}
		if !hookRan && o.beforePublish != nil {
			hookRan = true
			if e := o.beforePublish(); e != nil {
				return e
			}
		}
		fresh.SeqCounter++
		recorded = &model.Verification{Result: result, Seq: fresh.SeqCounter, ContractRev: n.EffectiveContractRev(), ReportDigest: reportDigest, Isolation: isolation, IsolationDirtyPaths: dirtyPaths, Provenance: provenance, RedKind: o.RedKind, Fault: o.Fault}
		n.Verification = recorded
		for _, id := range failedTests {
			if n.RedSeqs == nil {
				n.RedSeqs = map[string]int{}
			}
			if _, ok := n.RedSeqs[id]; !ok {
				n.RedSeqs[id], redAdded[id] = fresh.SeqCounter, fresh.SeqCounter
			}
		}
		if result == model.ResultPass && isolation == model.IsolationClean && n.Claim != nil && n.Claim.By == o.By {
			n.Claim, merged = nil, true
		} else if n.Claim != nil && n.Claim.By == o.By {
			n.Claim.LeaseExpires = o.Now().Add(o.TTL).UTC().Format(time.RFC3339)
			leaseRenewed = n.Claim.LeaseExpires
		}
		return nil
	}); err != nil {
		return nil, err
	}
	res.Recorded, res.Historical, res.Observation, res.RedSeqsAdded, res.LeaseRenewed, res.Merged = true, historical, recorded, redAdded, leaseRenewed, merged
	if merged && handle != "" {
		if err := prov.Release(handle); err != nil {
			return res, err
		}
		res.WorkspaceReleased = handle
	}
	return res, nil
}

func bytesDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func commandDigest(exit int, log []byte) string {
	return bytesDigest(append([]byte(fmt.Sprintf("exit=%d\n", exit)), log...))
}

func sameObservation(n *model.Node, reportDigest string) bool {
	return n != nil && n.Verification != nil &&
		n.Verification.EffectiveContractRev() == n.EffectiveContractRev() &&
		n.Verification.ReportDigest == reportDigest
}

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
	if len(b.Unresolved) > 0 {
		return fmt.Sprintf("%d declared test(s) never ran or were withheld by skips (%v)", len(b.Unresolved), b.Unresolved)
	}
	return fmt.Sprintf("%d declared test(s) were ambiguous (%v)", len(b.Ambiguous), b.Ambiguous)
}
