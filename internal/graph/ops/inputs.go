package ops

// set-inputs: replace a node's declared read-only inputs under the store's
// compare-and-swap. It is deliberately narrow — the only things it writes are
// the node's `inputs` list and the tool-computed `input_hashes` — and
// deliberately conservative about when it will write: an UNCLAIMED, UNVERIFIED
// node with no red observations only. A node carrying evidence (a recorded
// verification or a red proof) is never re-pointed at different input text,
// because that would launder the observation against content it never saw.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// SetInputsResult reports a set-inputs run.
type SetInputsResult struct {
	Node   string            `json:"node"`
	Inputs int               `json:"inputs"`
	Hashes map[string]string `json:"input_hashes,omitempty"`
	DryRun bool              `json:"dry_run,omitempty"`
}

// SetInputs replaces one node's declared inputs. The declarations are
// resolved (and thereby validated) with the shared resolver; an unresolvable
// input refuses. Eligibility is re-checked against the FRESH graph inside the
// compare-and-swap, so a claim or observation landing between read and write
// is seen and refused rather than overwritten.
func SetInputs(root, repoRoot, plan, nodeID string, decl []model.Input, dryRun bool) (*SetInputsResult, error) {
	return setInputsWith(root, repoRoot, plan, nodeID, decl, dryRun, gstore.Update)
}

func setInputsWith(root, repoRoot, plan, nodeID string, decl []model.Input, dryRun bool, update updateFunc) (*SetInputsResult, error) {
	planDir := filepath.Join(root, "Plans", plan)
	sources, err := gcompile.NewSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	inRes := sources.InputResolver()

	// Resolve every declaration once (memoized). Unresolvable inputs are
	// authoritative refusals, collected alongside eligibility refusals so one
	// round trip reports them all.
	var refusals []string
	hashes := map[string]string{}
	for _, spec := range decl {
		resolved, rerr := inRes.Resolve(spec)
		if rerr != nil {
			refusals = append(refusals, fmt.Sprintf("declared input %s does not resolve: %v", inputLabel(spec), rerr))
			continue
		}
		hashes[model.InputKey(spec)] = resolved.Digest
	}

	graphPath := gstore.PathFor(planDir)

	// An unresolvable declaration refuses the whole operation up front:
	// inputs are atomic, and a wrong read is never half-recorded.
	if len(refusals) > 0 {
		return nil, &RefusedError{Reasons: sortedStrings(refusals)}
	}

	if dryRun {
		g, err := gstore.Load(graphPath)
		if err != nil {
			return nil, err
		}
		res, rerr := planSetInputs(g, nodeID, decl, hashes, &refusals)
		if rerr != nil {
			return nil, rerr
		}
		if res == nil {
			return nil, &RefusedError{Reasons: sortedStrings(refusals)}
		}
		res.DryRun = true
		return res, nil
	}

	var result *SetInputsResult
	if _, err := update(graphPath, func(fresh *model.Graph) error {
		result = nil // re-derived on every CAS attempt, never carried across a retry
		res, rerr := planSetInputs(fresh, nodeID, decl, hashes, &refusals)
		if rerr != nil {
			return rerr
		}
		if res == nil {
			return &RefusedError{Reasons: sortedStrings(refusals)}
		}
		n := fresh.NodeByID(nodeID)
		n.Inputs = append([]model.Input(nil), decl...)
		n.InputHashes = hashes
		result = res
		return nil
	}); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &RefusedError{Reasons: sortedStrings(refusals)}
	}
	return result, nil
}

// planSetInputs checks one node's eligibility, appending each refusal reason
// to the shared refusals slice. It returns a non-nil result only when the
// node is eligible; a missing node is an operational error (exit 2),
// eligibility violations are refusals (exit 1).
func planSetInputs(g *model.Graph, nodeID string, decl []model.Input, hashes map[string]string, refusals *[]string) (*SetInputsResult, error) {
	n := g.NodeByID(nodeID)
	if n == nil {
		return nil, fmt.Errorf("graph set-inputs: node %q does not exist", nodeID)
	}
	if n.Claim != nil {
		*refusals = append(*refusals, fmt.Sprintf("%s is claimed by %q; release the claim before re-setting inputs", nodeID, n.Claim.By))
		return nil, nil
	}
	if n.Verification != nil {
		// Narrowing is provable (VerificationFreshness DD-4): every new
		// declaration must be a section of a currently declared whole-file
		// input whose bytes the run saw, and that file must be unchanged
		// since the run — so the section is a subset of exercised bytes.
		if reason := narrowingRefusal(n, decl); reason != "" {
			*refusals = append(*refusals, fmt.Sprintf("%s carries a recorded verification; inputs may only be narrowed against evidence — %s", nodeID, reason))
			return nil, nil
		}
		return &SetInputsResult{Node: nodeID, Inputs: len(decl), Hashes: hashes}, nil
	}
	if len(n.RedSeqs) > 0 {
		*refusals = append(*refusals, fmt.Sprintf("%s carries red observations; re-setting inputs would launder them", nodeID))
		return nil, nil
	}
	return &SetInputsResult{Node: nodeID, Inputs: len(decl), Hashes: hashes}, nil
}

// narrowingRefusal returns "" when decl narrows n's declared inputs faithfully
// against its observation snapshot, else the reason it does not.
func narrowingRefusal(n *model.Node, decl []model.Input) string {
	ran := n.Verification.InputHashes
	if ran == nil {
		return "the observation recorded no input snapshot; re-verify after re-setting inputs"
	}
	declared := map[string]model.Input{}
	for _, in := range n.Inputs {
		declared[model.InputKey(in)] = in
	}
	for _, next := range decl {
		key := model.InputKey(next)
		if _, same := declared[key]; same {
			continue // unchanged declaration
		}
		if next.Section == nil {
			return fmt.Sprintf("input %s is not a narrowing (no section)", inputLabel(next))
		}
		whole := model.Input{Root: next.Root, Path: next.Path}
		wholeKey := model.InputKey(whole)
		if _, ok := declared[wholeKey]; !ok {
			return fmt.Sprintf("input %s narrows %s, which is not currently declared whole", inputLabel(next), inputLabel(whole))
		}
		if ran[wholeKey] == "" || n.InputHashes[wholeKey] != ran[wholeKey] {
			return fmt.Sprintf("input %s: the run's recorded digest for %s is missing or predates the anchor; re-verify instead", inputLabel(next), inputLabel(whole))
		}
	}
	return ""
}

// inputLabel renders a declared input for refusal text.
func inputLabel(spec model.Input) string {
	s := spec.Root + ":" + spec.Path
	if spec.Section != nil {
		s += "#" + strings.Join(spec.Section.HeadingPath, " / ")
	}
	return s
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
