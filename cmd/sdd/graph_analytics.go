package main

// Read-only graph analytics verbs (Designs/SddGraph DD-14): everything here
// reads the committed graph and prints derived truth — analytics are
// first-class review inputs (cut vertices aim review attention, the
// silhouette diagnoses decomposition, the ceiling prices parallelism),
// never mutations. The guard allowlists each verb read-only.

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	greview "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// analyticsCtx is what every analytics verb needs: the graph, the full
// three-axis derived states, the closed predicate, and the adjacency +
// estimate maps the algorithms consume.
type analyticsCtx struct {
	planDir   string
	g         *model.Graph
	st        map[string]states.NodeState
	closed    map[string]bool
	adjacency algorithms.Graph
	estimate  map[string]int
}

const revisionLineageNote = "revision lineage records Git rewrite identity only; it does not prove the rewritten code"

type revisionLineageView struct {
	RecordedRevision  string   `json:"recorded_revision"`
	RewrittenRevision string   `json:"rewritten_revision"`
	Chain             []string `json:"chain"`
	Note              string   `json:"note"`
}

func nodeRevisionLineage(g *model.Graph, n *model.Node) *revisionLineageView {
	if n.Verification == nil || n.Verification.Provenance == nil {
		return nil
	}
	recorded := n.Verification.Provenance.Revision
	chain := g.RevisionChain(recorded)
	if len(chain) < 2 {
		return nil
	}
	return &revisionLineageView{RecordedRevision: recorded, RewrittenRevision: chain[len(chain)-1], Chain: chain, Note: revisionLineageNote}
}

func printNodeRevisionLineage(w io.Writer, g *model.Graph, n *model.Node) {
	lineage := nodeRevisionLineage(g, n)
	if lineage == nil {
		return
	}
	fmt.Fprintf(w, "  recorded revision: %s\n", lineage.RecordedRevision)
	fmt.Fprintf(w, "  rewritten revision: %s\n", lineage.RewrittenRevision)
	fmt.Fprintf(w, "  revision lineage: %s\n", strings.Join(lineage.Chain, " -> "))
	fmt.Fprintf(w, "  note: %s.\n", lineage.Note)
}

func loadAnalytics(plan, verb string) (*analyticsCtx, error) {
	planDir, err := planDirFor(plan, verb)
	if err != nil {
		return nil, err
	}
	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return nil, fmt.Errorf("graph %s: %w", verb, err)
	}
	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		return nil, err
	}
	sources, err := gcompile.NewSources(root, repoRoot, plan)
	if err != nil {
		return nil, fmt.Errorf("graph %s: %w", verb, err)
	}
	snap := sources.IntentSnapshot()
	digester := digest.New(repoRoot)
	st := states.Derive(states.Inputs{Graph: g, ArtifactDigest: digester.Artifact,
		CurrentIntentHashes: snap.Hashes(),
		CurrentInputHashes:  sources.InputResolver().GraphHashes(g)})
	ctx := &analyticsCtx{planDir: planDir, g: g, st: st, closed: greview.Closed(g, st),
		adjacency: algorithms.Graph{}, estimate: map[string]int{}}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		ctx.adjacency[n.ID] = n.Deps
		ctx.estimate[n.ID] = n.Estimate
	}
	return ctx, nil
}

// graphPathCmd prints the critical path: the wall-clock floor no capacity
// can beat, and the speedup ceiling unlimited capacity buys.
func graphPathCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "path",
		Short: "Critical path: length, total estimate, speedup ceiling",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, err := loadAnalytics(plan, "path")
			if err != nil {
				return err
			}
			rep := algorithms.CriticalPath(ctx.adjacency, ctx.estimate)
			if asJSON {
				return writeJSON(struct {
					OK bool `json:"ok"`
					algorithms.PathReport
				}{true, rep})
			}
			if len(rep.Path) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "the graph is empty (or entirely cyclic); no critical path")
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "critical path (%d of %d estimate units; ceiling %.2fx):\n  %s\n",
				rep.Length, rep.Total, rep.Ceiling, strings.Join(rep.Path, " -> "))
			fmt.Fprintf(c.OutOrStdout(), "no provider capacity can finish this plan in fewer than %d units; %d units of other work can overlap it\n",
				rep.Length, rep.Total-rep.Length)
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphRiskCmd prints the cut vertices: single nodes whose failure stalls
// otherwise-independent work on both sides — where review attention goes.
func graphRiskCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "risk",
		Short: "Cut vertices: nodes whose failure disconnects the walk",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, err := loadAnalytics(plan, "risk")
			if err != nil {
				return err
			}
			cuts := algorithms.CutVertices(ctx.adjacency)
			weight := algorithms.CriticalWeight(ctx.adjacency, ctx.estimate)
			type vertex struct {
				ID     string `json:"id"`
				State  string `json:"state"`
				Weight int    `json:"critical_weight"`
			}
			out := make([]vertex, 0, len(cuts))
			for _, id := range cuts {
				out = append(out, vertex{ID: id, State: string(ctx.st[id].State), Weight: weight[id]})
			}
			if asJSON {
				return writeJSON(struct {
					OK          bool     `json:"ok"`
					CutVertices []vertex `json:"cut_vertices"`
				}{true, out})
			}
			if len(out) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "no cut vertices: every node has a way around it")
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "%d cut vertex(es) — a failure here stalls both sides:\n", len(out))
			for _, v := range out {
				fmt.Fprintf(c.OutOrStdout(), "  %s  [%s, critical weight %d]\n", v.ID, v.State, v.Weight)
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphShapeCmd prints the depth histogram and its silhouette class.
func graphShapeCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "shape",
		Short: "Depth histogram and silhouette class (FLAT/CHAIN/FUNNEL/HOURGLASS/MIXED)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, err := loadAnalytics(plan, "shape")
			if err != nil {
				return err
			}
			hist := algorithms.DepthHistogram(ctx.adjacency)
			class := algorithms.Silhouette(hist)
			if asJSON {
				return writeJSON(struct {
					OK        bool   `json:"ok"`
					Histogram []int  `json:"histogram"`
					Class     string `json:"class"`
				}{true, hist, class})
			}
			fmt.Fprintf(c.OutOrStdout(), "%s\n", renderShape(hist, class))
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// renderShape draws the histogram as depth rows — shared by `graph shape`
// and `graph export --format shape`.
func renderShape(hist []int, class string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "silhouette: %s\n", class)
	for depth, width := range hist {
		fmt.Fprintf(&b, "  depth %d | %s (%d)\n", depth, strings.Repeat("#", width), width)
	}
	switch class {
	case algorithms.ShapeChain:
		b.WriteString("a chain prices zero parallelism: consider splitting independent concerns\n")
	case algorithms.ShapeHourglass:
		b.WriteString("an hourglass has a waist: `sdd graph risk` names it\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// graphStatusCmd summarizes derived state counts and per-node lines.
func graphStatusCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Derived state counts and per-node status lines",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, err := loadAnalytics(plan, "status")
			if err != nil {
				return err
			}
			counts := map[string]int{}
			closedCount := 0
			type reasons struct {
				Dependency []string `json:"dependency,omitempty"`
				Digest     []string `json:"digest,omitempty"`
				Intent     []string `json:"intent,omitempty"`
				Input      []string `json:"input,omitempty"`
				Review     []string `json:"review,omitempty"`
				Seq        bool     `json:"seq,omitempty"`
				Revision   bool     `json:"revision,omitempty"`
				Isolation  bool     `json:"isolation,omitempty"`
			}
			type line struct {
				ID          string   `json:"id"`
				State       string   `json:"state"`
				Closed      bool     `json:"closed"`
				Role        string   `json:"role"`
				ContractRev int      `json:"contract_rev"`
				Claimed     string   `json:"claimed_by,omitempty"`
				Reasons     *reasons `json:"reasons,omitempty"`
				Advisories  []string `json:"advisories,omitempty"`
			}
			lines := make([]line, 0, len(ctx.g.Nodes))
			for i := range ctx.g.Nodes {
				n := &ctx.g.Nodes[i]
				ns := ctx.st[n.ID]
				counts[string(ns.State)]++
				l := line{ID: n.ID, State: string(ns.State), Closed: ctx.closed[n.ID],
					Role: n.EffectiveRole(), ContractRev: n.EffectiveContractRev()}
				if l.Closed {
					closedCount++
				}
				if n.Claim != nil {
					l.Claimed = n.Claim.By
				}
				if ns.State == states.Stale {
					l.Reasons = &reasons{
						Dependency: ns.DependencyStale, Digest: ns.DigestStale, Intent: ns.IntentStale,
						Input: ns.InputStale, Review: ns.ReviewStale, Seq: ns.SeqStale,
						Revision: ns.RevIncompatible, Isolation: ns.IsolationStale,
					}
				}
				l.Advisories = ns.AnchorAdvisory
				lines = append(lines, l)
			}
			sort.Slice(lines, func(i, j int) bool { return lines[i].ID < lines[j].ID })
			if asJSON {
				return writeJSON(struct {
					OK     bool           `json:"ok"`
					States map[string]int `json:"states"`
					Closed int            `json:"closed"`
					Nodes  []line         `json:"nodes"`
				}{true, counts, closedCount, lines})
			}
			var keys []string
			for k := range counts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(c.OutOrStdout(), "%s=%d ", k, counts[k])
			}
			fmt.Fprintf(c.OutOrStdout(), "closed=%d/%d\n", closedCount, len(lines))
			for _, l := range lines {
				mark := ""
				if l.Closed {
					mark = "  [closed]"
				}
				if l.Claimed != "" {
					mark += "  [claimed by " + l.Claimed + "]"
				}
				fmt.Fprintf(c.OutOrStdout(), "  %-24s %s  role=%s contract_rev=%d%s\n", l.ID, l.State, l.Role, l.ContractRev, mark)
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphShowCmd prints one node's full record plus its derived state.
func graphShowCmd() *cobra.Command {
	var plan string
	var asJSON, brief bool
	c := &cobra.Command{
		Use:   "show <node-id>",
		Short: "One node's full record with its derived state and closure",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx, err := loadAnalytics(plan, "show")
			if err != nil {
				return err
			}
			n := ctx.g.NodeByID(args[0])
			if n == nil {
				return fmt.Errorf("graph show: node %q does not exist", args[0])
			}
			ns := ctx.st[n.ID]
			if asJSON {
				return writeJSON(struct {
					OK          bool                 `json:"ok"`
					Node        *model.Node          `json:"node"`
					Role        string               `json:"role"`
					ContractRev int                  `json:"contract_rev"`
					State       string               `json:"state"`
					Closed      bool                 `json:"closed"`
					Stale       []string             `json:"stale_artifacts,omitempty"`
					Intent      []string             `json:"stale_intent,omitempty"`
					Inputs      []string             `json:"stale_inputs,omitempty"`
					Lineage     *revisionLineageView `json:"revision_lineage,omitempty"`
				}{true, n, n.EffectiveRole(), n.EffectiveContractRev(), string(ns.State), ctx.closed[n.ID], ns.DigestStale, ns.IntentStale, ns.InputStale, nodeRevisionLineage(ctx.g, n)})
			}
			w := c.OutOrStdout()
			if brief {
				printBrief(w, ctx.g, n, ctx.st)
				printNodeRevisionLineage(w, ctx.g, n)
				return nil
			}
			fmt.Fprintf(w, "%s  [%s] (role %s, contract_rev %d)\n", n.ID, ns.State, n.EffectiveRole(), n.EffectiveContractRev())
			fmt.Fprintf(w, "  contract: %s\n", n.Contract)
			fmt.Fprintf(w, "  justifies: %s\n", strings.Join(n.Justifies, ", "))
			if len(n.Deps) > 0 {
				fmt.Fprintf(w, "  deps: %s\n", strings.Join(n.Deps, ", "))
			}
			fmt.Fprintf(w, "  gate: %s\n", n.Gate.Type)
			for _, t := range n.Gate.Tests {
				fmt.Fprintf(w, "    test %s (%s)\n", t.ID, t.File)
			}
			if len(n.Artifacts) > 0 {
				fmt.Fprintf(w, "  artifacts: %s\n", strings.Join(n.Artifacts, ", "))
			}
			if len(n.Inputs) > 0 {
				fmt.Fprintf(w, "  inputs: %s\n", describeInputsBrief(n.Inputs))
			}
			fmt.Fprintf(w, "  estimate: %d\n", n.Estimate)
			if v := n.Verification; v != nil {
				fmt.Fprintf(w, "  observation: %s at seq %d (isolation %s)\n", v.Result, v.Seq, v.Isolation)
			}
			printNodeRevisionLineage(w, ctx.g, n)
			if ctx.closed[n.ID] {
				fmt.Fprintln(w, "  closure: closed (completion-grade)")
			}
			if len(ns.DigestStale) > 0 {
				fmt.Fprintf(w, "  stale artifacts: %s\n", strings.Join(ns.DigestStale, ", "))
			}
			if len(ns.IntentStale) > 0 {
				fmt.Fprintf(w, "  INTENT-STALE: %s (re-read the cited requirements)\n", strings.Join(ns.IntentStale, ", "))
			}
			if len(ns.InputStale) > 0 {
				fmt.Fprintf(w, "  INPUT-STALE: %s (re-read the declared inputs)\n", strings.Join(ns.InputStale, ", "))
			}
			if ns.RevIncompatible {
				fmt.Fprintln(w, "  REV-INCOMPATIBLE: the latest observation predates a revise; it is history, not proof")
			}
			if ns.SeqStale {
				fmt.Fprintln(w, "  SEQ-STALE: legacy observation (no dependency digests); a dependency re-verified after it — re-sync to record what this node exercises")
			}
			if len(ns.DependencyStale) > 0 {
				fmt.Fprintf(w, "  DEPENDENCY-STALE: %s changed since this run exercised it (re-run the gate)\n", strings.Join(ns.DependencyStale, ", "))
			}
			if len(ns.AnchorAdvisory) > 0 {
				fmt.Fprintf(w, "  advisory: text changed since the anchor for %s (judge it: `sdd graph acknowledge`, or rework)\n", strings.Join(ns.AnchorAdvisory, ", "))
			}
			if len(ns.ReviewStale) > 0 {
				fmt.Fprintf(w, "  REVIEW-STALE: %s changed since this review (re-review)\n", strings.Join(ns.ReviewStale, ", "))
			}
			if cl := n.Claim; cl != nil {
				fmt.Fprintf(w, "  claim: %s (lease expires %s)\n", cl.By, cl.LeaseExpires)
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	c.Flags().BoolVar(&brief, "brief", false, "self-contained claim brief: for a review node, the reviewed contracts, artifacts, digests, and lanes")
	return c
}

// describeInputsBrief renders a node's declared inputs compactly for
// `graph show`.
func describeInputsBrief(inputs []model.Input) string {
	parts := make([]string, len(inputs))
	for i, in := range inputs {
		s := in.Root + ":" + in.Path
		if in.Section != nil {
			s += "#" + strings.Join(in.Section.HeadingPath, " / ")
		}
		parts[i] = s
	}
	return strings.Join(parts, ", ")
}

// graphExportCmd renders the graph in presentation formats. Presentation
// ONLY: no information beyond graph + derived state, nothing parseable back.
func graphExportCmd() *cobra.Command {
	var plan, format string
	var asJSON bool
	c := &cobra.Command{
		Use:   "export",
		Short: "Render the graph: --format mermaid|dot|plan|shape",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, err := loadAnalytics(plan, "export")
			if err != nil {
				return err
			}
			var body string
			switch format {
			case "mermaid":
				body = exportMermaid(ctx)
			case "dot":
				body = exportDot(ctx)
			case "plan":
				body = exportPlan(ctx)
			case "shape":
				hist := algorithms.DepthHistogram(ctx.adjacency)
				body = renderShape(hist, algorithms.Silhouette(hist))
			default:
				return fmt.Errorf("graph export: unknown --format %q; the formats are mermaid, dot, plan, shape", format)
			}
			if asJSON {
				return writeJSON(struct {
					OK     bool   `json:"ok"`
					Format string `json:"format"`
					Body   string `json:"body"`
				}{true, format, body})
			}
			fmt.Fprintln(c.OutOrStdout(), body)
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&format, "format", "mermaid", "output format: mermaid, dot, plan, or shape")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

func exportMermaid(ctx *analyticsCtx) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	for _, id := range algorithms.TopoSort(ctx.adjacency) {
		fmt.Fprintf(&b, "  %s[\"%s (%s)\"]\n", mermaidID(id), id, ctx.st[id].State)
	}
	for _, id := range algorithms.TopoSort(ctx.adjacency) {
		for _, dep := range ctx.adjacency[id] {
			fmt.Fprintf(&b, "  %s --> %s\n", mermaidID(dep), mermaidID(id))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

var mermaidIDRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

func mermaidID(id string) string { return mermaidIDRe.ReplaceAllString(id, "_") }

func exportDot(ctx *analyticsCtx) string {
	var b strings.Builder
	b.WriteString("digraph plan {\n  rankdir=TB;\n")
	for _, id := range algorithms.TopoSort(ctx.adjacency) {
		fmt.Fprintf(&b, "  %q [label=\"%s\\n%s\"];\n", id, id, ctx.st[id].State)
	}
	for _, id := range algorithms.TopoSort(ctx.adjacency) {
		for _, dep := range ctx.adjacency[id] {
			fmt.Fprintf(&b, "  %q -> %q;\n", dep, id)
		}
	}
	b.WriteString("}")
	return b.String()
}

// exportPlan is the flat ordered reading view: dependency-first, one line
// per node, human-scannable.
func exportPlan(ctx *analyticsCtx) string {
	var b strings.Builder
	for i, id := range algorithms.TopoSort(ctx.adjacency) {
		n := ctx.g.NodeByID(id)
		mark := string(ctx.st[id].State)
		if ctx.closed[id] {
			mark += ", closed"
		}
		fmt.Fprintf(&b, "%2d. %s — %s [%s, estimate %d]\n", i+1, id, n.Contract, mark, n.Estimate)
	}
	return strings.TrimRight(b.String(), "\n")
}

// printBrief renders the self-contained brief a claimant needs (contract
// text, inputs, gate) and, for a review node, the reviewed set: every scope
// node's contract, contract revision, artifacts with current digests, and
// the lanes to run — so the review needs no other reads
// (ReviewDrivenAmendment § Interfaces).
func printBrief(w io.Writer, g *model.Graph, n *model.Node, st map[string]states.NodeState) {
	fmt.Fprintf(w, "%s  [%s] role %s, contract_rev %d\n", n.ID, st[n.ID].State, n.EffectiveRole(), n.EffectiveContractRev())
	fmt.Fprintf(w, "contract: %s\n", n.Contract)
	if len(n.Justifies) > 0 {
		fmt.Fprintf(w, "justifies: %s\n", strings.Join(n.Justifies, ", "))
	}
	if n.EffectiveRole() != model.RoleReview {
		fmt.Fprintf(w, "gate: %s\n", n.Gate.Type)
		for _, t := range n.Gate.Tests {
			fmt.Fprintf(w, "  test %s (%s)\n", t.ID, t.File)
		}
		if len(n.Artifacts) > 0 {
			fmt.Fprintf(w, "artifacts: %s\n", strings.Join(n.Artifacts, ", "))
		}
		return
	}
	lanes := n.Gate.Lanes
	if lanes == nil {
		lanes = model.ReviewLanes
	}
	fmt.Fprintf(w, "lanes: %s\n", strings.Join(lanes, ", "))
	scope, err := states.ReviewScopeFromStates(g, n.ID, st)
	if err != nil {
		fmt.Fprintf(w, "scope: %v\n", err)
		return
	}
	fmt.Fprintf(w, "reviewed set (%d node(s)):\n", len(scope))
	for _, id := range scope {
		m := g.NodeByID(id)
		fmt.Fprintf(w, "  %s  [%s] contract_rev %d\n    contract: %s\n", id, st[id].State, m.EffectiveContractRev(), m.Contract)
		for _, a := range m.Artifacts {
			fmt.Fprintf(w, "    artifact: %s\n", a)
		}
	}
	if v := n.Verification; v != nil {
		fmt.Fprintf(w, "last review: %s at seq %d against contract_rev %d\n", v.Result, v.Seq, v.EffectiveContractRev())
	}
	if len(st[n.ID].ReviewStale) > 0 {
		fmt.Fprintf(w, "REVIEW-STALE: %s\n", strings.Join(st[n.ID].ReviewStale, ", "))
	}
}
