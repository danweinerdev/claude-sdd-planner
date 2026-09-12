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

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
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
	// By identifies the caller; a revised node claimed by someone else
	// refuses.
	By     string
	DryRun bool
}

// AmendResult reports what was (or would be) applied.
type AmendResult struct {
	Plan         *review.Plan `json:"plan"`
	Applied      bool         `json:"applied"`
	Seq          int          `json:"seq,omitempty"`
	ExpectDigest string       `json:"expect_digest,omitempty"`
	NewDigest    string       `json:"new_digest,omitempty"`
}

// AmendFromReview plans and applies the artifact's open findings.
func AmendFromReview(o AmendOptions) (*AmendResult, error) {
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
	f := artifact.Facts
	if f.Status != "resolved" || !f.Frozen {
		return nil, fmt.Errorf("graph amend: %s is not a resolved, frozen review; only frozen findings are applied", o.Artifact)
	}
	for _, a := range g.Amendments {
		if a.ReportDigest == artifact.ReportDigest {
			return nil, fmt.Errorf("graph amend: %s was already applied (seq %d); one artifact amends the graph once", o.Artifact, a.Seq)
		}
	}
	plan, err := review.PlanAmendments(g, o.Node, artifact)
	if err != nil {
		return nil, err
	}
	if len(plan.Amendments) == 0 {
		return nil, fmt.Errorf("graph amend: %s has no open findings; record it with `sdd graph review` instead", o.Artifact)
	}
	res := &AmendResult{Plan: plan, ExpectDigest: art.Digest}

	// One citation snapshot: new and revised nodes are anchored against
	// it, and the before/after gate re-derives from it. The gate runs on a
	// dry run too — a preview that would be refused is not a preview.
	sources, err := gcompile.NewSources(o.Root, o.RepoRoot, o.Plan)
	if err != nil {
		return nil, err
	}
	before := sources.Validate(g)
	rebuilt, seq, err := applyAmendments(g, plan, o.By, sources)
	if err != nil {
		return nil, err
	}
	after := sources.Validate(rebuilt)
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
	if !digestMatches(o.ExpectDigest, art.Digest) {
		return nil, fmt.Errorf("graph amend: the graph changed since the preview (expected %s, now %s); re-preview against the current graph, never blind-retry", short(o.ExpectDigest), short(art.Digest))
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
func applyAmendments(g *model.Graph, plan *review.Plan, by string, sources *gcompile.Sources) (*model.Graph, int, error) {
	out := *g
	out.Nodes = append([]model.Node(nil), g.Nodes...)
	out.Amendments = append([]model.AmendmentRecord(nil), g.Amendments...)
	index := map[string]int{}
	for i := range out.Nodes {
		index[out.Nodes[i].ID] = i
	}
	reviewIdx, ok := index[plan.Review]
	if !ok {
		return nil, 0, fmt.Errorf("graph amend: review node %q does not exist", plan.Review)
	}
	record := model.AmendmentRecord{Review: plan.Review, Artifact: plan.Artifact, ReportDigest: plan.ReportDigest}
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
			n.Contract = a.After.Contract
			n.Gate = a.After.Gate
			n.Justifies = a.After.Justifies
			n.Inputs = a.After.Inputs
			n.ContractRev = n.EffectiveContractRev() + 1
			// Proof compatibility boundary (DD-9): the obligation changed,
			// so RED is owed again for every test regardless of name.
			n.RedSeqs = nil
			// Re-anchor against the snapshot: hashes belong to the new
			// citation and input sets, never carried over.
			n.IntentHashes = nil
			n.InputHashes = nil
			sources.Anchor(n)
			if err := sources.AnchorInputs(n); err != nil {
				return nil, 0, fmt.Errorf("graph amend: %q: %w", n.ID, err)
			}
			record.Revised = append(record.Revised, n.ID)
		case review.ActionExtend:
			n := *a.New
			sources.Anchor(&n)
			if err := sources.AnchorInputs(&n); err != nil {
				return nil, 0, fmt.Errorf("graph amend: %q: %w", n.ID, err)
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
	sort.Strings(record.Revised)
	sort.Strings(record.Extended)
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
