// Package review implements the feature-scoped review gates of
// Designs/SddGraph DD-9: scope derives from the graph, the gate's
// observation is a persisted frozen Aligned review artifact (the existing
// `sdd review scaffold` → `resolve` flow — no new review mechanism), findings
// demote the nodes they name mechanically, and the *closed* predicate —
// GREEN and covered by a GREEN frozen full gate — is the completion-grade
// truth rendered views project and frozen-view refusal keys on (D-0022).
//
// The two failure modes this package exists to prevent (DD-9): uniform
// review weight at graph granularity (scope derivation makes each gate
// review exactly the increment no earlier frozen full gate covered), and a
// faulted node still reading GREEN between finding and fix (demotion happens
// as part of RECORDING the review, never as agent courtesy).
package review

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
)

// Scope derives what a review gate reviews: the gate's dependency closure
// minus, for every inner full review gate that already carries a recorded
// passing observation (its review is frozen history), that gate and
// everything at or below it. Nested gates therefore review DISJOINT
// increments whose union covers the closure — no diff is reviewed twice.
// An inner full gate that has NOT been recorded yet does not subtract: its
// region is unreviewed work, and someone must review it.
func Scope(g *model.Graph, gateID string) ([]string, error) {
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

	covered := map[string]bool{}
	for i := range g.Nodes {
		b := &g.Nodes[i]
		if b.ID == gateID || !closure[b.ID] {
			continue
		}
		if b.Gate.Type != model.GateReview || b.Gate.Lanes != nil {
			continue // only FULL gates cover; subsets are lighter checkpoints
		}
		if b.Verification == nil || b.Verification.Result != model.ResultPass {
			continue // not yet frozen: its region is this gate's to review
		}
		covered[b.ID] = true
		for id := range algorithms.DependencyClosure(adjacency, b.ID) {
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

// Closed derives the completion-grade predicate (D-0022): a node is closed
// when its own state is GREEN AND it lies inside the scope of a full review
// gate whose recorded observation is a pass and whose own derived state is
// GREEN — gate GREEN is what "matching diff digest" means, because the
// gate's observation records the aggregate scope-artifact digests and any
// drift derives the gate STALE (ordinary digest staleness). GREEN without
// such coverage is assumed-closed: sufficient to build on, never
// completion-grade.
func Closed(g *model.Graph, statesByID map[string]states.NodeState) map[string]bool {
	closed := map[string]bool{}
	for i := range g.Nodes {
		b := &g.Nodes[i]
		if b.Gate.Type != model.GateReview || b.Gate.Lanes != nil {
			continue
		}
		if b.Verification == nil || b.Verification.Result != model.ResultPass {
			continue
		}
		if statesByID[b.ID].State != states.Green {
			continue
		}
		// The gate itself is closed too: its completion-grade evidence IS
		// its own frozen Aligned review — without this, a view containing
		// its own gate could never freeze.
		closed[b.ID] = true
		scope, err := Scope(g, b.ID)
		if err != nil {
			continue
		}
		for _, m := range scope {
			if statesByID[m].State == states.Green {
				closed[m] = true
			}
		}
	}
	return closed
}

// facts is what the gate reads from a review artifact's frontmatter: the
// three freeze signals D-0020 binds together, the lane results, and the
// findings with the nodes they name.
type facts struct {
	Status      string `yaml:"status"`
	Frozen      bool   `yaml:"frozen"`
	Verdict     string `yaml:"verdict"`
	LaneResults []struct {
		Lane   string `yaml:"lane"`
		Result string `yaml:"result"`
	} `yaml:"lane_results"`
	Findings []struct {
		ID     string   `yaml:"id"`
		Status string   `yaml:"status"`
		Nodes  []string `yaml:"nodes"`
	} `yaml:"findings"`
}

// readFacts extracts and decodes the artifact's frontmatter block.
func readFacts(raw []byte) (*facts, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("the artifact has no frontmatter block")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("the artifact's frontmatter block never closes")
	}
	var f facts
	if err := yaml.Unmarshal([]byte(rest[:end]), &f); err != nil {
		return nil, fmt.Errorf("the artifact's frontmatter does not parse: %v", err)
	}
	return &f, nil
}

// Options configures one gate recording.
type Options struct {
	Root     string // planning root (absolute)
	RepoRoot string
	Plan     string
	Node     string // the review-gate node
	Artifact string // path to the frozen review artifact
	// By identifies the caller for claim discipline; required when the gate
	// node is claimed.
	By       string
	Provider provider.Provider
	Now      func() time.Time
}

// Result is one recording's outcome.
type Result struct {
	Node     string   `json:"node"`
	Artifact string   `json:"artifact"`
	Scope    []string `json:"scope"`
	// Demoted lists scope nodes the artifact's findings named: each now
	// carries a failing observation (RED, workable again).
	Demoted           []string            `json:"demoted,omitempty"`
	Observation       *model.Verification `json:"observation,omitempty"`
	Merged            bool                `json:"merged,omitempty"`
	WorkspaceReleased string              `json:"workspace_released,omitempty"`
}

// Record wires a frozen review artifact into a review gate's observation.
// The gate greens ONLY from an artifact that is resolved AND frozen: true
// AND verdict Aligned — three signals read together, because D-0020 sets
// them atomically at resolve and any one alone can be a stale or reopened
// artifact. Findings that name scope nodes demote them in the same
// compare-and-swap cycle: failing observations, seq-stamped before the
// gate's own, so a faulted node is RED the moment the review is recorded and
// the gate goes seq-stale the moment rework re-verifies it.
func Record(o Options) (*Result, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	planDir := filepath.Join(o.Root, "Plans", o.Plan)
	graphPath := gstore.PathFor(planDir)
	g, err := gstore.Load(graphPath)
	if err != nil {
		return nil, err
	}
	node := g.NodeByID(o.Node)
	if node == nil {
		return nil, fmt.Errorf("graph review: node %q does not exist", o.Node)
	}
	if node.Gate.Type != model.GateReview {
		return nil, fmt.Errorf("graph review: %q has gate type %q; test and command gates record through `sdd graph sync`", o.Node, node.Gate.Type)
	}
	if node.Claim != nil {
		if o.By == "" {
			return nil, fmt.Errorf("graph review: %q is claimed by %q; pass --by to record as its holder", o.Node, node.Claim.By)
		}
		if node.Claim.By != o.By {
			return nil, fmt.Errorf("graph review: %q is claimed by %q, not %q", o.Node, node.Claim.By, o.By)
		}
	}
	for _, lane := range node.Gate.Lanes {
		if !model.KnownReviewLane(lane) {
			return nil, fmt.Errorf("graph review: %q names unknown lane %q; the lanes are %s (it should not have compiled)", o.Node, lane, strings.Join(model.ReviewLanes, ", "))
		}
	}

	artifactPath := o.Artifact
	if _, statErr := os.Stat(artifactPath); statErr != nil && !filepath.IsAbs(artifactPath) {
		artifactPath = filepath.Join(o.Root, filepath.FromSlash(o.Artifact))
	}
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("graph review: reading the review artifact: %w", err)
	}
	f, err := readFacts(raw)
	if err != nil {
		return nil, fmt.Errorf("graph review: %s: %v", o.Artifact, err)
	}

	// The three freeze signals, refused together (batched, naming each).
	var missing []string
	if f.Status != "resolved" {
		missing = append(missing, fmt.Sprintf("status is %q, need \"resolved\"", f.Status))
	}
	if !f.Frozen {
		missing = append(missing, "frozen is not true (a reopened or in-progress review is not evidence)")
	}
	if f.Verdict != "Aligned" {
		missing = append(missing, fmt.Sprintf("verdict is %q, need \"Aligned\"", f.Verdict))
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("graph review: %s is not a frozen Aligned review — %s; run `sdd review resolve` on it first (D-0020 freezes all three signals atomically)", o.Artifact, strings.Join(missing, "; "))
	}

	// Lane conformance: a full gate needs all four lanes, a subset gate
	// needs exactly the lanes it names; each must carry a passing result.
	laneResult := map[string]string{}
	for _, lr := range f.LaneResults {
		laneResult[lr.Lane] = lr.Result
	}
	required := node.Gate.Lanes
	if required == nil {
		required = model.ReviewLanes
	}
	var laneProblems []string
	for _, lane := range required {
		res, ok := laneResult[lane]
		switch {
		case !ok:
			laneProblems = append(laneProblems, fmt.Sprintf("lane %s is absent from the artifact", lane))
		case !strings.HasPrefix(res, "PASS"):
			laneProblems = append(laneProblems, fmt.Sprintf("lane %s reports %q, not a pass", lane, res))
		}
	}
	if len(laneProblems) > 0 {
		return nil, fmt.Errorf("graph review: %s does not satisfy %q's lane set — %s", o.Artifact, o.Node, strings.Join(laneProblems, "; "))
	}

	scope, err := Scope(g, o.Node)
	if err != nil {
		return nil, err
	}
	inScope := map[string]bool{}
	for _, id := range scope {
		inScope[id] = true
	}

	// Demotion set: findings name graph nodes via their `nodes:` field. The
	// field's presence IS the demotion request — a finding without it (a
	// deferred hygiene note, a followup tracked elsewhere) demotes nothing.
	demote := map[string]bool{}
	var outOfScope []string
	for _, finding := range f.Findings {
		for _, named := range finding.Nodes {
			if !inScope[named] {
				outOfScope = append(outOfScope, fmt.Sprintf("%s names %q", finding.ID, named))
				continue
			}
			demote[named] = true
		}
	}
	if len(outOfScope) > 0 {
		return nil, fmt.Errorf("graph review: finding(s) name node(s) outside %q's scope — %s; the scope is: %s", o.Node, strings.Join(outOfScope, "; "), strings.Join(scope, ", "))
	}
	demoted := make([]string, 0, len(demote))
	for id := range demote {
		demoted = append(demoted, id)
	}
	sort.Strings(demoted)

	// The aggregate diff the gate reviewed: every scope node's declared
	// artifacts, digested from the shared tree (a review is of merged,
	// committed state). Recorded on the gate's observation so drift in any
	// of them derives the gate STALE via ordinary digest staleness (DD-6).
	digester := digest.New(o.RepoRoot)
	agg := map[string]string{}
	for _, id := range scope {
		for _, a := range g.NodeByID(id).Artifacts {
			if _, seen := agg[a]; seen {
				continue
			}
			if d := digester.Artifact(a); d != "" {
				agg[a] = d
			}
		}
	}

	prov := o.Provider
	if prov == nil {
		prov = provider.Detect(o.RepoRoot, planDir)
	}
	// Provenance is the shared tree's: the review is anchored to committed
	// state, not to any claimant's workspace.
	provenance, err := prov.Provenance("")
	if err != nil {
		return nil, fmt.Errorf("graph review: reading provenance: %w", err)
	}

	reportDigest := digest.Bytes(raw)
	res := &Result{Node: o.Node, Artifact: o.Artifact, Scope: scope, Demoted: demoted}
	handle := ""
	if _, err := gstore.Update(graphPath, func(fresh *model.Graph) error {
		n := fresh.NodeByID(o.Node)
		if n == nil {
			return fmt.Errorf("graph review: node %q vanished mid-record", o.Node)
		}
		if n.Claim != nil && n.Claim.By != o.By {
			return fmt.Errorf("graph review: %q was claimed by %q while this record ran", o.Node, n.Claim.By)
		}
		// Demotions first: the named nodes' failing observations carry
		// seqs BELOW the gate's, so the gate derives GREEN now and goes
		// seq-stale exactly when rework re-verifies a demoted node.
		for _, id := range demoted {
			dn := fresh.NodeByID(id)
			if dn == nil {
				return fmt.Errorf("graph review: demoted node %q vanished mid-record", id)
			}
			fresh.SeqCounter++
			dn.Verification = &model.Verification{
				Result:       model.ResultFail,
				Seq:          fresh.SeqCounter,
				ReportDigest: reportDigest,
				Isolation:    model.IsolationClean,
			}
		}
		fresh.SeqCounter++
		n.Verification = &model.Verification{
			Result:          model.ResultPass,
			Seq:             fresh.SeqCounter,
			ArtifactDigests: agg,
			ReportDigest:    reportDigest,
			Isolation:       model.IsolationClean,
			Provenance:      provenance,
		}
		res.Observation = n.Verification
		if n.Claim != nil && n.Claim.By == o.By {
			// A recorded gate pass completes the claim (DD-10's atomic
			// sequence, same as sync's merge).
			handle = n.Claim.Workspace
			n.Claim = nil
			res.Merged = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if res.Merged && handle != "" {
		if err := prov.Release(handle); err != nil {
			return res, fmt.Errorf("graph review: recorded, but workspace %s could not be released (reap it with `sdd graph gc`): %w", handle, err)
		}
		res.WorkspaceReleased = handle
	}
	return res, nil
}
