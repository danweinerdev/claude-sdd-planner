package main

// Read-only graph analytics verbs (Designs/SddGraph DD-14): everything here
// reads the committed graph and prints derived truth — analytics are
// first-class review inputs (cut vertices aim review attention, the
// silhouette diagnoses decomposition, the ceiling prices parallelism),
// never mutations. The guard allowlists each verb read-only.

import (
	"fmt"
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
	items, err := gcompile.CurrentIntent(root, repoRoot, plan)
	if err != nil {
		return nil, fmt.Errorf("graph %s: %w", verb, err)
	}
	hashes := make(map[string]string, len(items))
	for id, item := range items {
		hashes[id] = item.Hash
	}
	digester := digest.New(repoRoot)
	st := states.Derive(states.Inputs{Graph: g, ArtifactDigest: digester.Artifact, CurrentIntentHashes: hashes})
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
			type line struct {
				ID      string `json:"id"`
				State   string `json:"state"`
				Closed  bool   `json:"closed"`
				Claimed string `json:"claimed_by,omitempty"`
			}
			lines := make([]line, 0, len(ctx.g.Nodes))
			for i := range ctx.g.Nodes {
				n := &ctx.g.Nodes[i]
				ns := ctx.st[n.ID]
				counts[string(ns.State)]++
				l := line{ID: n.ID, State: string(ns.State), Closed: ctx.closed[n.ID]}
				if l.Closed {
					closedCount++
				}
				if n.Claim != nil {
					l.Claimed = n.Claim.By
				}
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
				fmt.Fprintf(c.OutOrStdout(), "  %-24s %s%s\n", l.ID, l.State, mark)
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
	var asJSON bool
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
					OK     bool        `json:"ok"`
					Node   *model.Node `json:"node"`
					State  string      `json:"state"`
					Closed bool        `json:"closed"`
					Stale  []string    `json:"stale_artifacts,omitempty"`
					Intent []string    `json:"stale_intent,omitempty"`
				}{true, n, string(ns.State), ctx.closed[n.ID], ns.DigestStale, ns.IntentStale})
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "%s  [%s]\n", n.ID, ns.State)
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
			fmt.Fprintf(w, "  estimate: %d\n", n.Estimate)
			if v := n.Verification; v != nil {
				fmt.Fprintf(w, "  observation: %s at seq %d (isolation %s)\n", v.Result, v.Seq, v.Isolation)
			}
			if ctx.closed[n.ID] {
				fmt.Fprintln(w, "  closure: closed (completion-grade)")
			}
			if len(ns.DigestStale) > 0 {
				fmt.Fprintf(w, "  stale artifacts: %s\n", strings.Join(ns.DigestStale, ", "))
			}
			if len(ns.IntentStale) > 0 {
				fmt.Fprintf(w, "  INTENT-STALE: %s (re-read the cited requirements)\n", strings.Join(ns.IntentStale, ", "))
			}
			if cl := n.Claim; cl != nil {
				fmt.Fprintf(w, "  claim: %s (lease expires %s)\n", cl.By, cl.LeaseExpires)
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
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
