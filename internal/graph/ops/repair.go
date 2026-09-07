package ops

// repair-intent: the conservative migration that backfills missing intent
// fingerprints (Designs/SddGraph DD-4) onto nodes the split bug left
// unanchored. It is deliberately narrow — the only thing it will ever write
// is a hash into a node's intent_hashes where that entry is missing or empty.
// It will not overwrite a nonempty hash (even a stale one), will not touch a
// claimed, verified, or red-observed node, and refuses atomically if any
// selected repair candidate is ineligible, so the migration can never bless
// evidence against today's text or silently half-complete.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// RepairChange is one backfilled (node, citation) pair with the hash written.
// The full list is what lets a caller independently assert exactly which
// children and citations were repaired.
type RepairChange struct {
	Node  string `json:"node"`
	Cited string `json:"cited"`
	Hash  string `json:"hash"`
}

// RepairIntentResult reports a repair-intent run.
type RepairIntentResult struct {
	// Repaired lists the node ids whose missing/empty hashes were backfilled,
	// sorted.
	Repaired []string `json:"repaired,omitempty"`
	// Changes lists every backfilled (node, citation, hash) triple in
	// deterministic (graph order, then justification order) order.
	Changes []RepairChange `json:"changes,omitempty"`
	// DryRun is true when the plan was computed but nothing was persisted.
	DryRun bool `json:"dry_run,omitempty"`
}

// RefusedError is an eligibility-policy refusal: a verb computed a plan but
// declined to write it because at least one selected node is ineligible
// (repair-intent: cites an ambiguous or unresolved requirement, or is
// claimed, verified, or red-observed; set-inputs: claimed, verified, or
// red-observed, or declares an unresolvable input). It is authoritative —
// exit 1 at the CLI — and never an operational "could not run" error (exit 2).
type RefusedError struct {
	// Reasons is every ineligibility, sorted for determinism.
	Reasons []string `json:"reasons"`
}

func (e *RefusedError) Error() string {
	var b strings.Builder
	b.WriteString("graph: refused (atomic — nothing was written):\n")
	for _, r := range e.Reasons {
		fmt.Fprintf(&b, "  %s\n", r)
	}
	return strings.TrimRight(b.String(), "\n")
}

// RepairIntent backfills missing/empty intent hashes. With a non-empty
// nodeID it considers only that node (which must exist); with an empty nodeID
// it considers every node. A node whose citations are all decisions (D-NNNN)
// needs no fingerprint and is left alone — that is not a refusal. The repair
// is idempotent: re-running over an already-repaired graph reports zero
// changes. dryRun computes and returns the same planned changes without
// writing the graph (and never touches staged proposals either way).
func RepairIntent(root, repoRoot, plan, nodeID string, dryRun bool) (*RepairIntentResult, error) {
	return repairIntentWith(root, repoRoot, plan, nodeID, dryRun, gstore.Update)
}

// repairIntentWith is RepairIntent with the CAS primitive injected, so the
// CAS-retry tests can force a genuine digest collision and prove the fresh
// re-plan refuses a node claimed or verified between the read and the write.
func repairIntentWith(root, repoRoot, plan, nodeID string, dryRun bool, update updateFunc) (*RepairIntentResult, error) {
	planDir := filepath.Join(root, "Plans", plan)
	sources, err := gcompile.NewSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}

	if dryRun {
		g, err := gstore.Load(gstore.PathFor(planDir))
		if err != nil {
			return nil, err
		}
		res, err := planRepair(g, nodeID, sources)
		if err != nil {
			return nil, err
		}
		res.DryRun = true
		return res, nil
	}

	// Eligibility is re-planned against the FRESH graph inside the CAS
	// callback, so a claim or observation landing between read and write is
	// seen and refused rather than overwritten.
	var result *RepairIntentResult
	if _, err := update(gstore.PathFor(planDir), func(fresh *model.Graph) error {
		res, err := planRepair(fresh, nodeID, sources)
		if err != nil {
			return err
		}
		for _, ch := range res.Changes {
			n := fresh.NodeByID(ch.Node)
			if n.IntentHashes == nil {
				n.IntentHashes = map[string]string{}
			}
			n.IntentHashes[ch.Cited] = ch.Hash
		}
		result = res
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// planRepair classifies a graph's selected nodes and returns the planned
// backfills, or a refusal when any selected repair candidate is ineligible.
// Pure: it never writes.
func planRepair(g *model.Graph, nodeID string, sources *gcompile.Sources) (*RepairIntentResult, error) {
	var selected []*model.Node
	if nodeID != "" {
		n := g.NodeByID(nodeID)
		if n == nil {
			return nil, fmt.Errorf("graph repair-intent: node %q does not exist", nodeID)
		}
		selected = []*model.Node{n}
	} else {
		for i := range g.Nodes {
			selected = append(selected, &g.Nodes[i])
		}
	}

	var changes []RepairChange
	var refusals []string
	for _, n := range selected {
		needsRepair := false
		var nodeChanges []RepairChange
		var nodeProblems []string
		for _, cited := range n.Justifies {
			d := sources.ClassifyCitation(cited)
			switch d.Kind {
			case gcompile.CitationFingerprintable:
				// Only the missing/empty entries are backfilled; a nonempty
				// hash — even a stale one — is never overwritten.
				if n.IntentHashes[cited] == "" {
					needsRepair = true
					nodeChanges = append(nodeChanges, RepairChange{Node: n.ID, Cited: cited, Hash: d.Item.Hash})
				}
			case gcompile.CitationAmbiguous:
				nodeProblems = append(nodeProblems, fmt.Sprintf("%s cites %q, which is defined by more than one related source (%s)", n.ID, cited, strings.Join(d.Suggestions, ", ")))
			case gcompile.CitationUnresolved:
				nodeProblems = append(nodeProblems, fmt.Sprintf("%s cites %q, which resolves in no related spec, design, or decision ledger", n.ID, cited))
			case gcompile.CitationDecision:
				// Decisions are never fingerprinted — not a refusal.
			}
		}
		// A citation that cannot be blessed against today's text refuses even
		// when every fingerprintable citation is already anchored (needsRepair
		// false): a node whose only citation is ambiguous or unresolved must
		// not be silently skipped.
		refusals = append(refusals, nodeProblems...)
		if !needsRepair {
			continue // already anchored, or D-only: no change, no refusal
		}
		// A repair candidate must be UNCLAIMED, UNVERIFIED, carry no red
		// observations, and cite nothing ambiguous or unresolved; anything
		// else cannot be blessed against today's text.
		if n.Claim != nil {
			refusals = append(refusals, fmt.Sprintf("%s is claimed by %q; release the claim before repairing", n.ID, n.Claim.By))
		}
		if n.Verification != nil {
			refusals = append(refusals, fmt.Sprintf("%s carries a recorded verification; evidence cannot be blessed against today's text", n.ID))
		}
		if len(n.RedSeqs) > 0 {
			refusals = append(refusals, fmt.Sprintf("%s carries red observations; repair would launder them", n.ID))
		}
		changes = append(changes, nodeChanges...)
	}

	if len(refusals) > 0 {
		sort.Strings(refusals)
		return nil, &RefusedError{Reasons: refusals}
	}

	repaired := map[string]bool{}
	for _, ch := range changes {
		repaired[ch.Node] = true
	}
	ids := make([]string, 0, len(repaired))
	for id := range repaired {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return &RepairIntentResult{Repaired: ids, Changes: changes}, nil
}
