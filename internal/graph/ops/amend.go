package ops

// Review-driven amendment (Designs/ReviewDrivenAmendment DD-2, DD-3, DD-7,
// DD-9): apply every open finding of one frozen review artifact to the
// graph as one all-or-nothing write, fenced on the graph file's digest the
// driver previewed against. A revise advances the node's contract revision
// and clears its red bookkeeping — earlier proof becomes history, and RED
// must be observed again at the new revision. An extend appends a node
// sourced by the finding, hanging off the reviewed node, and adds it to the
// review node's deps so the review cannot re-green until it is GREEN.

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// AmendOptions configures one amendment.
type AmendOptions struct {
	Root, RepoRoot, Plan string
	Node                 string // the review node whose artifact is applied
	Artifact             string
	// ExpectDigest is the graph file digest the preview was computed
	// against (`sdd graph review` prints it). Required unless DryRun.
	ExpectDigest string
	// ExpectReportDigest is the review artifact digest the preview evaluated.
	// It is an independent fence: graph bytes can stay fixed while the report
	// path is substituted. Required unless DryRun.
	ExpectReportDigest string
	// By identifies the caller; a revised node claimed by someone else
	// refuses.
	By     string
	DryRun bool
}

// AmendResult reports what was (or would be) applied.
type AmendResult struct {
	Plan               *review.Plan `json:"plan"`
	Applied            bool         `json:"applied"`
	Seq                int          `json:"seq,omitempty"`
	ExpectDigest       string       `json:"expect_digest,omitempty"`
	ExpectReportDigest string       `json:"expect_report_digest,omitempty"`
	NewDigest          string       `json:"new_digest,omitempty"`
	// WidenedReach is a non-refusing notice: revise/extend targets that sit
	// inside the review's Reach (its full dependency closure — admissible
	// since the P-17 fix) but outside its increment scope (the region no
	// earlier frozen full review already covers), and the inner full-review
	// gate(s) that cover each one and will re-stale (ReviewStale, via the
	// target's contract-revision bump) once the amendment lands.
	WidenedReach []WidenedTarget `json:"widened_reach,omitempty"`
}

// WidenedTarget names one amendment target outside the increment scope and
// the inner full-review gates its contract-revision bump will re-stale.
type WidenedTarget struct {
	Finding string   `json:"finding"`
	Node    string   `json:"node"`
	Gates   []string `json:"gates"`
}

// widenedReach finds, for every amendment target in the review's Reach but
// outside its current increment scope, the currently-GREEN full-lane inner
// review gate(s) whose dependency closure covers that target — the gates a
// revise/extend there will re-stale. It reads the graph as evaluated for the
// preview; it never writes.
func widenedReach(g *model.Graph, reviewNode string, plan *review.Plan) []WidenedTarget {
	inScope := map[string]bool{}
	for _, id := range plan.Scope {
		inScope[id] = true
	}
	reach := review.Reach(g, reviewNode)
	statesByID := states.Derive(states.Inputs{Graph: g})

	adjacency := map[string][]string{}
	for i := range g.Nodes {
		adjacency[g.Nodes[i].ID] = g.Nodes[i].Deps
	}
	// Every currently-GREEN full-lane inner review gate in Reach, with its
	// own dependency closure precomputed once.
	type innerGate struct {
		id     string
		covers map[string]bool
	}
	var inner []innerGate
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.ID == reviewNode || !reach[n.ID] || n.Gate.Type != model.GateReview || n.Gate.Lanes != nil {
			continue
		}
		if statesByID[n.ID].State != states.Green {
			continue
		}
		inner = append(inner, innerGate{id: n.ID, covers: algorithms.DependencyClosure(adjacency, n.ID)})
	}

	var out []WidenedTarget
	for _, a := range plan.Amendments {
		target := a.Node
		if a.Action == review.ActionExtend {
			// An extend adds a new node; the widened blast radius is on the
			// dependency it hangs off, which is what an inner gate could
			// already cover.
			if len(a.New.Deps) == 0 {
				continue
			}
			target = a.New.Deps[0]
			for _, d := range a.New.Deps {
				if inScope[d] {
					target = d
					break
				}
			}
		}
		if !reach[target] || inScope[target] {
			continue
		}
		var gates []string
		for _, ig := range inner {
			if ig.covers[target] {
				gates = append(gates, ig.id)
			}
		}
		if len(gates) == 0 {
			continue
		}
		sort.Strings(gates)
		out = append(out, WidenedTarget{Finding: a.Finding, Node: a.Node, Gates: gates})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Finding < out[j].Finding })
	return out
}

// AmendFromReview plans and applies the artifact's open findings.
func AmendFromReview(o AmendOptions) (*AmendResult, error) {
	if err := review.ValidatePlanName(o.Plan); err != nil {
		return nil, fmt.Errorf("graph amend: %w", err)
	}
	planDir := filepath.Join(o.Root, "Plans", o.Plan)
	graphPath := gstore.PathFor(planDir)
	art, err := istore.Read(graphPath)
	if err != nil {
		return nil, fmt.Errorf("graph amend: reading graph: %w", err)
	}
	if !art.Exists {
		return nil, fmt.Errorf("graph amend: %s does not exist", graphPath)
	}
	g, err := model.DecodeGraph([]byte(art.Source))
	if err != nil {
		return nil, fmt.Errorf("graph amend: graph %s is not valid:\n%w", graphPath, err)
	}
	artifact, err := review.ReadArtifact(o.Root, o.Artifact)
	if err != nil {
		return nil, fmt.Errorf("graph amend: %w", err)
	}
	if err := review.AdmitArtifact(g, o.Plan, o.Node, artifact); err != nil {
		return nil, fmt.Errorf("graph amend: %w", err)
	}
	scope, err := review.CurrentScope(g, o.Node)
	if err != nil {
		return nil, fmt.Errorf("graph amend: %w", err)
	}
	plan, err := review.PlanAmendmentsInScope(g, o.Node, artifact, scope)
	if err != nil {
		return nil, err
	}
	if len(plan.Amendments) == 0 {
		return nil, fmt.Errorf("graph amend: %s has no open findings; record it with `sdd graph review` instead", o.Artifact)
	}
	res := &AmendResult{Plan: plan, ExpectDigest: art.Digest, ExpectReportDigest: artifact.ReportDigest,
		WidenedReach: widenedReach(g, o.Node, plan)}

	// One citation snapshot: new and revised nodes are anchored against
	// it, and the before/after gate re-derives from it. The gate runs on a
	// dry run too — a preview that would be refused is not a preview.
	sources, err := gcompile.NewSources(o.Root, o.RepoRoot, o.Plan)
	if err != nil {
		return nil, err
	}
	before, err := sources.Validate(g)
	if err != nil {
		return nil, err
	}
	rebuilt, seq, err := applyAmendments(g, plan, o.By, sources, o.RepoRoot)
	if err != nil {
		return nil, err
	}
	after, err := sources.Validate(rebuilt)
	if err != nil {
		return nil, err
	}
	if introduced := introducedFindings(before, after); len(introduced) > 0 {
		var b strings.Builder
		b.WriteString("graph amend: refused — the amendment would introduce findings compile refuses:\n")
		for _, f := range introduced {
			fmt.Fprintf(&b, "  %s\n", f.String())
		}
		return nil, errors.New(strings.TrimRight(b.String(), "\n"))
	}
	if o.DryRun {
		return res, nil
	}
	if o.ExpectDigest == "" {
		return nil, fmt.Errorf("graph amend: --expect-digest is required; preview with `sdd graph review` or `--dry-run` and pass the digest it prints (%s)", art.Digest)
	}
	if o.ExpectReportDigest == "" {
		return nil, fmt.Errorf("graph amend: --expect-report-digest is required; preview with `sdd graph review` or `--dry-run` and pass the review artifact digest it prints (%s)", artifact.ReportDigest)
	}
	if !digestMatches(o.ExpectDigest, art.Digest) {
		return nil, fmt.Errorf("graph amend: the graph changed since the preview (expected %s, now %s); re-preview against the current graph, never blind-retry", short(o.ExpectDigest), short(art.Digest))
	}
	if !digestMatches(o.ExpectReportDigest, artifact.ReportDigest) {
		return nil, fmt.Errorf("graph amend: the review artifact changed since the preview (expected %s, now %s); re-preview the current artifact, never blind-retry", short(o.ExpectReportDigest), short(artifact.ReportDigest))
	}
	latestArtifact, err := review.ReadArtifact(o.Root, o.Artifact)
	if err != nil {
		return nil, fmt.Errorf("graph amend: re-reading review artifact before publication: %w", err)
	}
	if latestArtifact.ReportDigest != artifact.ReportDigest {
		return nil, fmt.Errorf("graph amend: the review artifact changed while the amendment was evaluated (expected %s, now %s); re-preview the current artifact", short(artifact.ReportDigest), short(latestArtifact.ReportDigest))
	}
	out, err := rebuilt.Encode()
	if err != nil {
		return nil, err
	}
	// The fence is the digest the driver previewed against: no retry loop,
	// because a concurrent write means the preview is stale by definition.
	if err := istore.WriteAtomicExpecting(graphPath, string(out), art.Digest); err != nil {
		var concurrent *istore.ErrConcurrentWrite
		if errors.As(err, &concurrent) {
			return nil, fmt.Errorf("graph amend: another writer landed while this amendment ran; re-preview against the current graph")
		}
		return nil, err
	}
	res.Applied = true
	res.Seq = seq
	res.NewDigest = istore.Digest(string(out))
	return res, nil
}

// applyAmendments computes the post-amendment graph without touching disk.
func applyAmendments(g *model.Graph, plan *review.Plan, by string, sources *gcompile.Sources, repoRoot string) (*model.Graph, int, error) {
	out := *g
	out.Nodes = append([]model.Node(nil), g.Nodes...)
	out.Amendments = append([]model.AmendmentRecord(nil), g.Amendments...)
	out.RevisionLineage = cloneRevisionLineage(g.RevisionLineage)
	index := map[string]int{}
	for i := range out.Nodes {
		index[out.Nodes[i].ID] = i
	}
	reviewIdx, ok := index[plan.Review]
	if !ok {
		return nil, 0, fmt.Errorf("graph amend: review node %q does not exist", plan.Review)
	}
	record := model.AmendmentRecord{Review: plan.Review, Artifact: plan.Artifact, ReportDigest: plan.ReportDigest}
	preimageTests := map[string][]model.Test{}
	preimageRedSeqs := map[string]map[string]int{}
	for _, a := range plan.Amendments {
		switch a.Action {
		case review.ActionRevise:
			i, ok := index[a.Node]
			if !ok {
				return nil, 0, fmt.Errorf("graph amend: revised node %q vanished", a.Node)
			}
			n := &out.Nodes[i]
			if n.Claim != nil && n.Claim.By != by {
				return nil, 0, fmt.Errorf("graph amend: %q is claimed by %q; the holder applies the revise, or releases the claim first", n.ID, n.Claim.By)
			}
			oldTests := n.Gate.Tests
			oldRed := n.RedSeqs
			if problems := model.ValidateEvidenceGate(a.After); len(problems) > 0 {
				return nil, 0, fmt.Errorf("graph amend: %q: %s", n.ID, strings.Join(problems, "; "))
			}
			n.Contract = a.After.Contract
			n.Gate = a.After.Gate
			n.Justifies = a.After.Justifies
			n.Inputs = a.After.Inputs
			n.ContractRev = n.EffectiveContractRev() + 1
			// Proof compatibility boundary (DD-9): a test whose (id, file,
			// satisfies) is unchanged across the revise keeps its recorded
			// red — the proof it discharges is unchanged. A changed,
			// removed, or new test owes a fresh red at the new revision.
			n.RedSeqs = carryOverRedSeqs(oldTests, n.Gate.Tests, oldRed)
			if len(oldTests) > 0 {
				preimageTests[n.ID] = oldTests
			}
			if len(oldRed) > 0 {
				preimageRedSeqs[n.ID] = oldRed
			}
			record.Revised = append(record.Revised, n.ID)
		case review.ActionExtend:
			n := *a.New
			if problems := model.ValidateEvidenceGate(&n); len(problems) > 0 {
				return nil, 0, fmt.Errorf("graph amend: %q: %s", n.ID, strings.Join(problems, "; "))
			}
			out.Nodes = append(out.Nodes, n)
			index[n.ID] = len(out.Nodes) - 1
			r := &out.Nodes[reviewIdx]
			if !containsString(r.Deps, n.ID) {
				r.Deps = append(r.Deps, n.ID)
			}
			record.Extended = append(record.Extended, n.ID)
		}
	}
	if len(record.Extended) > 0 || len(record.Revised) > 0 {
		r := &out.Nodes[reviewIdx]
		// A pre-reviewed-set graph encoded a passing review with Reviewed absent.
		// Existing graphs retain that historical meaning until an amendment
		// touches their scope. Materialize the scope assumed immediately before
		// this amend, at the pre-amend contract revisions, so an extended
		// dependency is mechanically absent and a revised member's revision no
		// longer matches; the old proof becomes REVIEW-STALE without misusing
		// contract_rev (deps are structural) or deleting the observation.
		if r.Verification != nil && r.Verification.Reviewed == nil {
			v := *r.Verification
			// Pinned from the pre-amend graph `g`, so revised members keep
			// their old revision in the set and mismatch afterward.
			v.Reviewed = states.LegacyReviewedSet(g, plan.Review, plan.Scope)
			r.Verification = &v
		}
	}
	sort.Strings(record.Revised)
	sort.Strings(record.Extended)
	if len(preimageTests) > 0 {
		record.PreimageTests = preimageTests
	}
	if len(preimageRedSeqs) > 0 {
		record.PreimageRedSeqs = preimageRedSeqs
	}
	out.SeqCounter++
	record.Seq = out.SeqCounter
	out.Amendments = append(out.Amendments, record)
	return &out, record.Seq, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func digestMatches(want, got string) bool {
	want = strings.TrimSpace(want)
	if want == got {
		return true
	}
	// Accept an unambiguous prefix of at least 12 characters, the way the
	// artifact write path does.
	return len(want) >= 12 && strings.HasPrefix(got, want)
}

func short(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
