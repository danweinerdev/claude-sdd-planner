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
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// Scope derives what a review gate reviews: the gate's dependency closure
// minus, for every inner full review gate that is currently GREEN, that gate
// and everything at or below it. Nested current gates therefore review
// DISJOINT increments whose union covers the closure — no diff is reviewed
// twice. An unrecorded or STALE inner gate does not subtract: its region is
// unreviewed work, and someone must review it.
func Scope(g *model.Graph, gateID string) ([]string, error) {
	return states.ReviewScope(g, gateID)
}

// CurrentScope derives scope with every staleness axis wired against the
// shared tree. This is the publication-grade scope used by review recording
// and review-driven amendment; Scope remains the graph-only compatibility
// surface for callers that have no roots.
func CurrentScope(root, repoRoot, plan string, g *model.Graph, gateID string) ([]string, error) {
	sources, err := gcompile.NewSources(root, repoRoot, plan)
	if err != nil {
		// Pre-fingerprint graphs may have no related source surface. Preserve
		// that shape only when no node carries an intent/input promise that
		// would otherwise be silently disabled.
		for i := range g.Nodes {
			n := &g.Nodes[i]
			if len(n.IntentHashes) > 0 || len(n.InputHashes) > 0 || len(n.Inputs) > 0 {
				return nil, fmt.Errorf("loading current proof inputs: %w", err)
			}
		}
		sources = nil
	}
	in := states.Inputs{Graph: g, ArtifactDigest: digest.New(repoRoot).Artifact}
	if sources != nil {
		in.CurrentIntentHashes = sources.IntentSnapshot().Hashes()
		in.CurrentInputHashes = sources.InputResolver().GraphHashes(g)
	}
	return states.ReviewScopeFromStates(g, gateID, states.Derive(in))
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
	return states.Closed(g, statesByID)
}

// facts is what the gate reads from a review artifact's frontmatter: the
// three freeze signals D-0020 binds together, the lane results, and the
// findings with the nodes they name.
type facts struct {
	Status      string `yaml:"status"`
	Frozen      bool   `yaml:"frozen"`
	Verdict     string `yaml:"verdict"`
	ReviewOf    string `yaml:"review_of"`
	LaneResults []struct {
		Lane   string `yaml:"lane"`
		Result string `yaml:"result"`
	} `yaml:"lane_results"`
	Findings []finding `yaml:"findings"`
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
	// beforePublish is a deterministic interleaving seam for package tests.
	// It runs once inside the store mutation before the first CAS attempt.
	beforePublish func() error
}

// Result is one recording's outcome. Exactly one of Observation (the
// artifact had no open findings and greened the node) or Plan (it had open
// findings: nothing was written, and Plan is the amendment preview to apply
// with `sdd graph amend --from-review`) is set.
type Result struct {
	Node               string              `json:"node"`
	Artifact           string              `json:"artifact"`
	Scope              []string            `json:"scope"`
	Observation        *model.Verification `json:"observation,omitempty"`
	Plan               *Plan               `json:"plan,omitempty"`
	ExpectDigest       string              `json:"expect_digest,omitempty"`
	ExpectReportDigest string              `json:"expect_report_digest,omitempty"`
	Merged             bool                `json:"merged,omitempty"`
	WorkspaceReleased  string              `json:"workspace_released,omitempty"`
}

// AdmitArtifact applies the provenance/admissibility policy shared by review
// recording and review-driven amendment. A frozen artifact is evidence, not a
// bearer token: it must bind to this plan, satisfy this gate's lane contract,
// and be unused by every prior observation or amendment.
func AdmitArtifact(g *model.Graph, plan, nodeID string, art *Artifact) error {
	if err := ValidatePlanName(plan); err != nil {
		return err
	}
	node := g.NodeByID(nodeID)
	if node == nil {
		return fmt.Errorf("node %q does not exist", nodeID)
	}
	if node.Gate.Type != model.GateReview {
		return fmt.Errorf("%q has gate type %q; review evidence applies to review gates only", nodeID, node.Gate.Type)
	}
	if role := node.EffectiveRole(); role != model.RoleReview {
		return fmt.Errorf("%q has role %q; review evidence applies to review nodes only", nodeID, role)
	}
	for _, lane := range node.Gate.Lanes {
		if !model.KnownReviewLane(lane) {
			return fmt.Errorf("%q names unknown lane %q; the lanes are %s", nodeID, lane, strings.Join(model.ReviewLanes, ", "))
		}
	}

	f := art.Facts
	var missing []string
	if f.Status != "resolved" {
		missing = append(missing, fmt.Sprintf("status is %q, need \"resolved\"", f.Status))
	}
	if !f.Frozen {
		missing = append(missing, "frozen is not true (a reopened or in-progress review is not evidence)")
	}
	open := len(art.OpenFindings())
	switch f.Verdict {
	case "Aligned":
		if open > 0 {
			missing = append(missing, fmt.Sprintf("verdict is Aligned but %d finding(s) are open; an Aligned review has every finding terminal (resolve refuses this shape)", open))
		}
	case "Amend":
		if open == 0 {
			missing = append(missing, "verdict is Amend but no finding is open; an Amend review carries the revise/extend findings `sdd graph amend` applies")
		}
	default:
		missing = append(missing, fmt.Sprintf("verdict is %q, need \"Aligned\" (greens the review node) or \"Amend\" (frozen findings report for `sdd graph amend`)", f.Verdict))
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s is not a frozen Aligned or Amend review — %s; run `sdd review resolve` on it first", art.Rel, strings.Join(missing, "; "))
	}

	if strings.TrimSpace(f.ReviewOf) == "" {
		return fmt.Errorf("%s carries no review_of; a review must bind to this plan's reviewed document", art.Rel)
	}
	reviewOf := filepath.Clean(filepath.FromSlash(f.ReviewOf))
	planDir := filepath.Join("Plans", plan)
	relToPlan, err := filepath.Rel(planDir, reviewOf)
	if err != nil || filepath.IsAbs(reviewOf) || relEscapes(relToPlan) || relToPlan == "." {
		return fmt.Errorf("%s reviews %q, which is not under %s/ — a review of another plan is not evidence for this gate", art.Rel, f.ReviewOf, filepath.ToSlash(planDir))
	}

	for i := range g.Nodes {
		if v := g.Nodes[i].Verification; v != nil && v.ReportDigest == art.ReportDigest {
			return fmt.Errorf("%s is already recorded on gate %q; one review artifact supplies evidence once", art.Rel, g.Nodes[i].ID)
		}
	}
	for _, amendment := range g.Amendments {
		if amendment.ReportDigest == art.ReportDigest {
			return fmt.Errorf("%s was already applied as an amendment (seq %d); one review artifact supplies evidence once", art.Rel, amendment.Seq)
		}
	}

	laneResults := map[string]string{}
	duplicates := map[string]bool{}
	for _, lr := range f.LaneResults {
		if _, exists := laneResults[lr.Lane]; exists {
			duplicates[lr.Lane] = true
		}
		laneResults[lr.Lane] = lr.Result
	}
	required := node.Gate.Lanes
	if required == nil {
		required = model.ReviewLanes
	}
	var laneProblems []string
	for _, lane := range required {
		res, ok := laneResults[lane]
		switch {
		case duplicates[lane]:
			laneProblems = append(laneProblems, fmt.Sprintf("lane %s appears more than once", lane))
		case !ok:
			laneProblems = append(laneProblems, fmt.Sprintf("lane %s is absent from the artifact", lane))
		case !strings.HasPrefix(res, "PASS"):
			laneProblems = append(laneProblems, fmt.Sprintf("lane %s reports %q, not a pass", lane, res))
		}
	}
	if len(laneProblems) > 0 {
		return fmt.Errorf("%s does not satisfy %q's lane set — %s", art.Rel, nodeID, strings.Join(laneProblems, "; "))
	}
	return nil
}

// Record wires a frozen review artifact into a review node's observation.
// The node greens ONLY from an artifact that is resolved AND frozen: true
// AND verdict Aligned — three signals read together, because resolve sets
// them atomically and any one alone can be a stale or reopened artifact —
// AND that carries no open findings. Open findings never demote anything:
// they are amendments (revise / extend), previewed here and applied by
// `sdd graph amend --from-review` under a digest fence
// (ReviewDrivenAmendment DD-2, DD-7). A pass records the reviewed set —
// every scope node's contract revision and artifact digests — so a later
// contract-only change stales the review even when the bytes did not move
// (DD-9).
func Record(o Options) (*Result, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if err := ValidatePlanName(o.Plan); err != nil {
		return nil, fmt.Errorf("graph review: %w", err)
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
	if role := node.EffectiveRole(); role != model.RoleReview {
		return nil, fmt.Errorf("graph review: %q has role %q; only a review node records a review artifact", o.Node, role)
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

	art, err := ReadArtifact(o.Root, o.Artifact)
	if err != nil {
		return nil, fmt.Errorf("graph review: %w", err)
	}
	if err := AdmitArtifact(g, o.Plan, o.Node, art); err != nil {
		return nil, fmt.Errorf("graph review: %w", err)
	}
	reportDigest := art.ReportDigest

	// Scope subtraction is based on fully current inner reviews. Graph-only
	// GREEN is insufficient: artifact, requirement, or declared-input drift
	// makes an inner gate stale and puts its region back into this increment.
	scope, err := CurrentScope(o.Root, o.RepoRoot, o.Plan, g, o.Node)
	if err != nil {
		return nil, fmt.Errorf("graph review: %w", err)
	}
	digester := digest.New(o.RepoRoot)

	// Open findings are amendments, not demotions. Plan them now so a
	// malformed finding refuses here, then hand the preview back unwritten:
	// the node cannot green from an artifact that still demands change.
	if open := art.OpenFindings(); len(open) > 0 {
		plan, err := PlanAmendmentsInScope(g, o.Node, art, scope)
		if err != nil {
			return nil, err
		}
		expect, err := gstore.Digest(graphPath)
		if err != nil {
			return nil, err
		}
		return &Result{Node: o.Node, Artifact: o.Artifact, Scope: scope, Plan: plan,
			ExpectDigest: expect, ExpectReportDigest: reportDigest}, nil
	}
	// The reviewed set (DD-9): every scope node's contract revision and
	// artifact digests, digested from the shared tree (a review is of
	// merged, committed state). The aggregate artifact digests are also
	// recorded on the observation so drift in any of them derives the node
	// STALE via ordinary digest staleness (DD-6).
	agg := map[string]string{}
	reviewed := map[string]model.ReviewedRef{}
	for _, id := range scope {
		sn := g.NodeByID(id)
		ref := model.ReviewedRef{ContractRev: sn.EffectiveContractRev()}
		for _, a := range sn.Artifacts {
			d := digester.Artifact(a)
			if d == "" {
				continue
			}
			if ref.ArtifactDigests == nil {
				ref.ArtifactDigests = map[string]string{}
			}
			ref.ArtifactDigests[a] = d
			if _, seen := agg[a]; !seen {
				agg[a] = d
			}
		}
		reviewed[id] = ref
	}
	evaluatedGate := node.ProofSnapshot()
	evaluatedScope := make(map[string]string, len(scope))
	for _, id := range scope {
		evaluatedScope[id] = g.NodeByID(id).ProofSnapshot()
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

	res := &Result{Node: o.Node, Artifact: o.Artifact, Scope: scope}
	handle := ""
	hookRan := false
	if _, err := gstore.Update(graphPath, func(fresh *model.Graph) error {
		n := fresh.NodeByID(o.Node)
		if n == nil {
			return fmt.Errorf("graph review: node %q vanished mid-record", o.Node)
		}
		if n.Claim != nil && n.Claim.By != o.By {
			return fmt.Errorf("graph review: %q was claimed by %q while this record ran", o.Node, n.Claim.By)
		}
		currentScope, err := CurrentScope(o.Root, o.RepoRoot, o.Plan, fresh, o.Node)
		if err != nil {
			return err
		}
		if n.ProofSnapshot() != evaluatedGate || !slices.Equal(currentScope, scope) {
			return fmt.Errorf("graph review: %q's gate or scope changed while the artifact was evaluated; re-review the current graph", o.Node)
		}
		for _, id := range scope {
			if fresh.NodeByID(id).ProofSnapshot() != evaluatedScope[id] {
				return fmt.Errorf("graph review: scope changed while the artifact was evaluated: %q has a different contract snapshot; re-review the current graph", id)
			}
		}
		currentArtifact, err := ReadArtifact(o.Root, o.Artifact)
		if err != nil {
			return fmt.Errorf("graph review: re-reading artifact before publication: %w", err)
		}
		if currentArtifact.ReportDigest != reportDigest {
			return fmt.Errorf("graph review: %s changed while its evidence was being recorded; re-review the current artifact", art.Rel)
		}
		if err := AdmitArtifact(fresh, o.Plan, o.Node, currentArtifact); err != nil {
			return fmt.Errorf("graph review: %w", err)
		}
		if !hookRan && o.beforePublish != nil {
			hookRan = true
			if err := o.beforePublish(); err != nil {
				return err
			}
		}
		fresh.SeqCounter++
		n.Verification = &model.Verification{
			Result:          model.ResultPass,
			Seq:             fresh.SeqCounter,
			ContractRev:     node.EffectiveContractRev(),
			ArtifactDigests: agg,
			Reviewed:        reviewed,
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
