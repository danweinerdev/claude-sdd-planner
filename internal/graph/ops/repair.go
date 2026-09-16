package ops

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

type RefusedError struct {
	Reasons []string `json:"reasons"`
}

func (e *RefusedError) Error() string {
	return "graph: refused (atomic — nothing was written):\n  " + strings.Join(e.Reasons, "\n  ")
}

type RedRepairChange struct {
	Node string `json:"node"`
	Test string `json:"test"`
	Seq  int    `json:"seq"`
}
type RepairRedResult struct {
	Repaired []string          `json:"repaired,omitempty"`
	Changes  []RedRepairChange `json:"changes,omitempty"`
	DryRun   bool              `json:"dry_run,omitempty"`
}

func RepairRed(root, repoRoot, plan, nodeID string, dryRun bool) (*RepairRedResult, error) {
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", plan))
	if dryRun {
		g, err := gstore.Load(graphPath)
		if err != nil {
			return nil, err
		}
		res, err := planRedRepair(g, nodeID)
		if res != nil {
			res.DryRun = true
		}
		return res, err
	}
	var result *RepairRedResult
	_, err := gstore.Update(graphPath, func(g *model.Graph) error {
		res, err := planRedRepair(g, nodeID)
		if err != nil {
			return err
		}
		for _, ch := range res.Changes {
			n := g.NodeByID(ch.Node)
			if n.RedSeqs == nil {
				n.RedSeqs = map[string]int{}
			}
			if _, ok := n.RedSeqs[ch.Test]; !ok {
				n.RedSeqs[ch.Test] = ch.Seq
			}
		}
		result = res
		return nil
	})
	return result, err
}

func planRedRepair(g *model.Graph, nodeID string) (*RepairRedResult, error) {
	var selected []*model.Node
	if nodeID != "" {
		n := g.NodeByID(nodeID)
		if n == nil {
			return nil, fmt.Errorf("graph repair-red: node %q does not exist", nodeID)
		}
		selected = []*model.Node{n}
	} else {
		for i := range g.Nodes {
			selected = append(selected, &g.Nodes[i])
		}
	}
	var changes []RedRepairChange
	for _, n := range selected {
		if n.Gate.Type != model.GateTests {
			continue
		}
		var pre []model.Test
		var preRed map[string]int
		found := false
		for _, a := range g.Amendments {
			if p, ok := a.PreimageTests[n.ID]; ok {
				pre, preRed, found = p, a.PreimageRedSeqs[n.ID], true
			}
		}
		if !found {
			continue
		}
		for id, seq := range carryOverRedSeqs(pre, n.Gate.Tests, preRed) {
			if _, ok := n.RedSeqs[id]; !ok {
				changes = append(changes, RedRepairChange{Node: n.ID, Test: id, Seq: seq})
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Node != changes[j].Node {
			return changes[i].Node < changes[j].Node
		}
		return changes[i].Test < changes[j].Test
	})
	seen := map[string]bool{}
	var repaired []string
	for _, ch := range changes {
		if !seen[ch.Node] {
			seen[ch.Node] = true
			repaired = append(repaired, ch.Node)
		}
	}
	return &RepairRedResult{Repaired: repaired, Changes: changes}, nil
}
