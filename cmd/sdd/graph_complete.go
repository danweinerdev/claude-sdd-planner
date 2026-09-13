package main

// Graph-plan branch for `sdd plan complete` and `sdd phase complete`
// (CLAUDE.md "Completion Evidence": for a graph plan "closure is a derived
// predicate" — the observation record already IS the completion record, so
// the v1 markdown-evidence gates (SDD059/SDD070/SDD158, ...) do not apply.
// The gate here is the graph's own derived closure: every node GREEN and
// covered by a passing frozen full review gate, exactly what `sdd graph
// status` reports. On success, the same view renderer `sdd compile` uses is
// reused (never duplicated) so phase docs and the README projection agree
// with the graph, then the README's own `status:` is flipped through the
// same frontmatter write path the v1 transition uses.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	greview "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// graphPlanDir resolves path (a plan directory or its README.md) to the
// plan's directory and name, returning ok=false when there is no committed
// graph next to it — the v1 caller falls through untouched, same contract
// as graphNext.
func graphPlanDir(path string) (planDir, plan string, ok bool) {
	readme, err := resolvePlanReadme(path)
	if err != nil {
		return "", "", false
	}
	planDir = filepath.Dir(readme)
	if _, err := os.Stat(gstore.PathFor(planDir)); err != nil {
		return "", "", false
	}
	return planDir, filepath.Base(planDir), true
}

// graphDerive loads the graph and derives its three-axis state plus
// closure, exactly as `sdd graph status` does.
func graphDerive(planDir, plan string) (*model.Graph, map[string]states.NodeState, map[string]bool, error) {
	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return nil, nil, nil, err
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		return nil, nil, nil, err
	}
	sources, err := gcompile.NewSources(root, repoRoot, plan)
	if err != nil {
		return nil, nil, nil, err
	}
	snap := sources.IntentSnapshot()
	digester := digest.New(repoRoot)
	st := states.Derive(states.Inputs{Graph: g, ArtifactDigest: digester.Artifact,
		CurrentIntentHashes: snap.Hashes(),
		CurrentInputHashes:  sources.InputResolver().GraphHashes(g)})
	closed := greview.Closed(g, st)
	return g, st, closed, nil
}

// blockingNode names one node that is not yet closed, for a refusal
// message.
type blockingNode struct {
	ID    string
	State string
}

func openNodes(nodes []*model.Node, st map[string]states.NodeState, closed map[string]bool) []blockingNode {
	var out []blockingNode
	for _, n := range nodes {
		if closed[n.ID] {
			continue
		}
		out = append(out, blockingNode{ID: n.ID, State: string(st[n.ID].State)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func liveClaims(nodes []*model.Node) []string {
	var out []string
	for _, n := range nodes {
		if n.Claim != nil {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}

// graphPlanComplete implements `sdd plan complete` on a graph plan: the
// gate is every node GREEN and closed, and no live claims. Returns
// handled=false when path is not a graph plan, so the caller falls through
// to the v1 transition.
func graphPlanComplete(path string, o completeOpts) (handled bool, err error) {
	planDir, plan, ok := graphPlanDir(path)
	if !ok {
		return false, nil
	}
	readme := filepath.Join(planDir, "README.md")

	g, st, closed, err := graphDerive(planDir, plan)
	if err != nil {
		return true, fmt.Errorf("plan complete: %w", err)
	}
	nodes := make([]*model.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodes[i] = &g.Nodes[i]
	}
	open := openNodes(nodes, st, closed)
	claims := liveClaims(nodes)

	res := transitionResult{Path: relPath(readme), Kind: "plan", Verb: "complete", To: "complete", DryRun: o.DryRun}
	if len(open) > 0 || len(claims) > 0 {
		if o.JSON {
			return true, emitTransitionJSON(res)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "plan complete: refused — the graph is not closed:\n")
		for _, n := range open {
			fmt.Fprintf(&b, "  %s: %s (not closed)\n", n.ID, n.State)
		}
		for _, id := range claims {
			fmt.Fprintf(&b, "  %s: live claim\n", id)
		}
		return true, &refusedError{n: len(open) + len(claims), msg: strings.TrimRight(b.String(), "\n")}
	}
	res.OK = true

	if o.DryRun {
		if o.JSON {
			return true, emitTransitionJSON(res)
		}
		fmt.Printf("plan complete: graph closed (%d/%d); would mark plan complete\n", len(g.Nodes), len(g.Nodes))
		return true, nil
	}

	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return true, fmt.Errorf("plan complete: %w", err)
	}
	if _, err := gcompile.RenderViews(root, plan, repoRoot, g, st, closed); err != nil {
		return true, fmt.Errorf("plan complete: %w", err)
	}
	if err := writeReadmeStatusComplete(readme); err != nil {
		return true, fmt.Errorf("plan complete: %w", err)
	}
	res.Wrote = true
	if o.JSON {
		return true, emitTransitionJSON(res)
	}
	fmt.Printf("marked plan complete in %s\n", readme)
	return true, nil
}

// graphPhaseComplete implements `sdd phase complete` on a graph plan: the
// gate is every node in that phase's group GREEN and closed. Returns
// handled=false when path does not resolve to a phase doc rendered from a
// graph plan, so the caller falls through to the v1 transition.
func graphPhaseComplete(path string, o completeOpts) (handled bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, nil
	}
	planDir := filepath.Dir(abs)
	plan := filepath.Base(planDir)
	if _, err := os.Stat(gstore.PathFor(planDir)); err != nil {
		return false, nil
	}

	g, st, closed, err := graphDerive(planDir, plan)
	if err != nil {
		return true, fmt.Errorf("phase complete: %w", err)
	}
	groups := gcompile.GroupPhases(g, plan)
	docName := filepath.Base(abs)
	var group *gcompile.PhaseGroup
	for i := range groups {
		if groups[i].Doc == docName {
			group = &groups[i]
			break
		}
	}
	if group == nil {
		return false, nil // not a rendered phase doc of this graph plan
	}

	open := openNodes(group.Nodes, st, closed)
	claims := liveClaims(group.Nodes)

	res := transitionResult{Path: relPath(abs), Kind: "phase", Verb: "complete", To: "complete", DryRun: o.DryRun}
	if len(open) > 0 || len(claims) > 0 {
		if o.JSON {
			return true, emitTransitionJSON(res)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "phase complete: refused — phase %d is not closed:\n", group.Ordinal)
		for _, n := range open {
			fmt.Fprintf(&b, "  %s: %s (not closed)\n", n.ID, n.State)
		}
		for _, id := range claims {
			fmt.Fprintf(&b, "  %s: live claim\n", id)
		}
		return true, &refusedError{n: len(open) + len(claims), msg: strings.TrimRight(b.String(), "\n")}
	}
	res.OK = true

	if o.DryRun {
		if o.JSON {
			return true, emitTransitionJSON(res)
		}
		fmt.Printf("phase complete: phase %d closed; would mark phase complete\n", group.Ordinal)
		return true, nil
	}

	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return true, fmt.Errorf("phase complete: %w", err)
	}
	if _, err := gcompile.RenderViews(root, plan, repoRoot, g, st, closed); err != nil {
		return true, fmt.Errorf("phase complete: %w", err)
	}
	res.Wrote = true
	if o.JSON {
		return true, emitTransitionJSON(res)
	}
	fmt.Printf("marked phase %d complete in %s\n", group.Ordinal, abs)
	return true, nil
}

// writeReadmeStatusComplete flips the plan README's top-level `status:` to
// complete through the same frontmatter write path the v1 transition uses
// (setTopLevelStatus + restampUpdated), independent of the v1 evidence
// gate: the graph's own closure, checked above, is this transition's gate.
func writeReadmeStatusComplete(readme string) error {
	art, err := store.Read(readme)
	if err != nil {
		return err
	}
	if !art.Exists {
		return fmt.Errorf("%s does not exist", readme)
	}
	doc := artifact.Parse(art.Source)
	current, _ := doc.FM("status")
	if strings.Trim(current, `"'`) == "complete" {
		return nil
	}
	lines := strings.Split(art.Source, "\n")
	if !setTopLevelStatus(lines, "complete") {
		return fmt.Errorf("no top-level `status:` field to advance")
	}
	updated := restampUpdated(strings.Join(lines, "\n"), time.Now().Format("2006-01-02"))
	return store.WriteAtomic(art.Path, updated)
}
