package ops

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

type SetInputsResult struct {
	Node   string `json:"node"`
	Inputs int    `json:"inputs"`
	DryRun bool   `json:"dry_run,omitempty"`
}

func SetInputs(root, repoRoot, plan, nodeID string, decl []model.Input, dryRun bool) (*SetInputsResult, error) {
	return setInputsWith(root, repoRoot, plan, nodeID, decl, dryRun, gstore.Update)
}

func setInputsWith(root, repoRoot, plan, nodeID string, decl []model.Input, dryRun bool, update updateFunc) (*SetInputsResult, error) {
	sources, err := gcompile.NewSources(root, repoRoot, plan)
	if err != nil {
		return nil, err
	}
	var refusals []string
	for _, spec := range decl {
		if _, err := sources.InputResolver().Resolve(spec); err != nil {
			refusals = append(refusals, fmt.Sprintf("declared input %s does not resolve: %v", inputLabel(spec), err))
		}
	}
	if len(refusals) > 0 {
		return nil, &RefusedError{Reasons: sortedStrings(refusals)}
	}
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", plan))
	apply := func(g *model.Graph) error {
		n := g.NodeByID(nodeID)
		if n == nil {
			return fmt.Errorf("graph set-inputs: node %q does not exist", nodeID)
		}
		if n.Claim != nil {
			return &RefusedError{Reasons: []string{fmt.Sprintf("%s is claimed by %q; release the claim before re-setting inputs", nodeID, n.Claim.By)}}
		}
		if n.Verification == nil && len(n.RedSeqs) > 0 {
			return &RefusedError{Reasons: []string{fmt.Sprintf("%s carries red observations; re-setting inputs would launder them", nodeID)}}
		}
		if n.Verification != nil {
			n.ContractRev = n.EffectiveContractRev() + 1
			n.RedSeqs = cloneRedSeqs(n.RedSeqs)
		}
		n.Inputs = append([]model.Input(nil), decl...)
		return nil
	}
	if dryRun {
		g, err := gstore.Load(graphPath)
		if err != nil {
			return nil, err
		}
		if err := apply(g); err != nil {
			return nil, err
		}
		return &SetInputsResult{Node: nodeID, Inputs: len(decl), DryRun: true}, nil
	}
	if _, err := update(graphPath, apply); err != nil {
		return nil, err
	}
	return &SetInputsResult{Node: nodeID, Inputs: len(decl)}, nil
}

func cloneRedSeqs(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for id, seq := range in {
		out[id] = seq
	}
	return out
}

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
