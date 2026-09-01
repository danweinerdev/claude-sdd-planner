package main

// The `sdd graph` verb tree: plan-graph operations per Designs/SddGraph.
//
// This file currently carries only the read-only vocabulary inspector
// (`graph hazards`); the store/compile/walk verbs land phase by phase
// (Plans/SddGraph). Guard posture: `graph` is not in the pretooluse
// read-only allowlist yet, so every `sdd graph` invocation is denied for the
// read-only agents by default (D-0014's allowlist semantics); task 2.6
// allowlists the read surface deliberately.

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/algorithms"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/claims"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	gconvert "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/convert"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/hazards"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/ops"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/provider"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	greview "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	gsync "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/sync"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/spf13/cobra"
)

func graphCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "graph",
		Short: "Plan-graph operations (Designs/SddGraph)",
		RunE: func(c *cobra.Command, args []string) error {
			return c.Help()
		},
	}
	c.AddCommand(graphHazardsCmd())
	c.AddCommand(graphInitCmd())
	c.AddCommand(graphProposeCmd())
	c.AddCommand(graphAssembleCmd())
	c.AddCommand(graphConvertCmd())
	c.AddCommand(graphReleaseCmd())
	c.AddCommand(graphSyncCmd())
	c.AddCommand(graphReviewCmd())
	c.AddCommand(graphSplitCmd())
	c.AddCommand(graphSetTestsCmd())
	c.AddCommand(graphGCCmd())
	c.AddCommand(graphPathCmd())
	c.AddCommand(graphRiskCmd())
	c.AddCommand(graphShapeCmd())
	c.AddCommand(graphStatusCmd())
	c.AddCommand(graphShowCmd())
	c.AddCommand(graphExportCmd())
	return c
}

// graphSplitCmd retires a too-big node into children (the stopping rule's
// remedy: two consecutive failures propose a split). Gated like a compile:
// the mutation must introduce no finding the compiler would refuse.
func graphSplitCmd() *cobra.Command {
	var plan, node, file string
	var asJSON bool
	c := &cobra.Command{
		Use:   "split",
		Short: "Retire a node into children from a proposal payload",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" || node == "" || file == "" {
				return fmt.Errorf("graph split: --plan, --node, and --file are all required")
			}
			root, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph split: %w", err)
			}
			payload, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("graph split: %w", err)
			}
			res, err := ops.Split(root, repoRoot, plan, node, payload)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(struct {
					OK bool `json:"ok"`
					*ops.SplitResult
				}{true, res})
			}
			fmt.Fprintf(c.OutOrStdout(), "retired %s into %s (retired ids are never reused)\n",
				res.Retired, strings.Join(res.Children, ", "))
			if len(res.Rewired) > 0 {
				fmt.Fprintf(c.OutOrStdout(), "re-pointed dependants: %s\n", strings.Join(res.Rewired, ", "))
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&node, "node", "", "node id to retire")
	c.Flags().StringVar(&file, "file", "", "proposal payload declaring the children")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphSetTestsCmd edits one node's declared test list under the lock.
func graphSetTestsCmd() *cobra.Command {
	var plan, node, by, file string
	var asJSON bool
	c := &cobra.Command{
		Use:   "set-tests",
		Short: "Replace a node's declared tests (holder-only while claimed)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" || node == "" || file == "" {
				return fmt.Errorf("graph set-tests: --plan, --node, and --file are all required")
			}
			planDir, err := planDirFor(plan, "set-tests")
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("graph set-tests: %w", err)
			}
			var tests []model.Test
			if err := json.Unmarshal(raw, &tests); err != nil {
				return fmt.Errorf("graph set-tests: %s is not a JSON array of {id, file, satisfies?}: %v", file, err)
			}
			if err := ops.SetTests(planDir, node, by, tests); err != nil {
				return err
			}
			if asJSON {
				return writeJSON(struct {
					OK    bool   `json:"ok"`
					Node  string `json:"node"`
					Tests int    `json:"tests"`
				}{true, node, len(tests)})
			}
			fmt.Fprintf(c.OutOrStdout(), "set %d test(s) on %s (red proofs for removed tests pruned)\n", len(tests), node)
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&node, "node", "", "node id to edit")
	c.Flags().StringVar(&by, "by", "", "claimant identity (required while the node is claimed)")
	c.Flags().StringVar(&file, "file", "", "JSON array of tests: [{\"id\": ..., \"file\": ..., \"satisfies\": [...]}]")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphGCCmd reaps abandoned workspace state: orphan ws-* directories and
// staged payloads whose nodes all landed (the crash window between compile's
// graph write and payload consumption).
func graphGCCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "gc",
		Short: "Reap orphan workspaces and stale staged payloads",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" {
				return fmt.Errorf("graph gc: --plan is required")
			}
			root, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph gc: %w", err)
			}
			res, err := ops.GC(root, repoRoot, plan)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(struct {
					OK bool `json:"ok"`
					*ops.GCResult
				}{true, res})
			}
			if len(res.Workspaces) == 0 && len(res.StalePayloads) == 0 && len(res.PrunedBranches) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "nothing to reap")
			}
			for _, w := range res.Workspaces {
				fmt.Fprintf(c.OutOrStdout(), "reaped workspace %s\n", w)
			}
			for _, p := range res.StalePayloads {
				fmt.Fprintf(c.OutOrStdout(), "reaped stale payload %s\n", p)
			}
			for _, b := range res.PrunedBranches {
				fmt.Fprintf(c.OutOrStdout(), "pruned merged branch %s\n", b)
			}
			for _, k := range res.Kept {
				fmt.Fprintf(c.OutOrStdout(), "kept %s (active claim)\n", k)
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphSyncCmd records one observation from mechanical input — the only
// path toward GREEN (DD-5). A red run is a SUCCESSFUL sync: recording the
// failure is what arms red-before-green.
func graphSyncCmd() *cobra.Command {
	var plan, node, by, report, commandLog string
	var commandExit int
	var asJSON bool
	c := &cobra.Command{
		Use:   "sync",
		Short: "Record a node's observation from a test report or command result",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, cmdArgs []string) error {
			planDir, err := planDirFor(plan, "sync")
			if err != nil {
				return err
			}
			if node == "" {
				return fmt.Errorf("graph sync: --node is required")
			}
			_, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph sync: %w", err)
			}
			opts := gsync.Options{PlanDir: planDir, RepoRoot: repoRoot, Node: node, By: by}
			if report != "" {
				raw, err := os.ReadFile(report)
				if err != nil {
					return fmt.Errorf("graph sync: %w", err)
				}
				opts.ReportName, opts.ReportBytes = report, raw
			}
			if c.Flags().Changed("command-exit") {
				opts.CommandExit = &commandExit
				if commandLog != "" {
					raw, err := os.ReadFile(commandLog)
					if err != nil {
						return fmt.Errorf("graph sync: %w", err)
					}
					opts.CommandLog = raw
				}
			}
			cfg, _ := store.LoadConfig(".")
			opts.TTL = time.Duration(cfg.GraphLeaseTtlMinutes) * time.Minute

			res, err := gsync.Run(opts)
			if err != nil {
				return err
			}
			if asJSON {
				if err := writeJSON(res); err != nil {
					return err
				}
				if !res.Recorded {
					return &refusedError{n: 1}
				}
				return nil
			}
			printBucket := func(name string, ids []string) {
				if len(ids) > 0 {
					fmt.Fprintf(c.OutOrStdout(), "%s: %s\n", name, strings.Join(ids, ", "))
				}
			}
			printBucket("updated", res.Buckets.Updated)
			printBucket("unresolved", res.Buckets.Unresolved)
			printBucket("untracked", res.Buckets.Untracked)
			printBucket("ambiguous", res.Buckets.Ambiguous)
			if !res.Recorded {
				return fmt.Errorf("graph sync: %s", res.Refusal)
			}
			fmt.Fprintf(c.OutOrStdout(), "recorded %s at seq %d (isolation %s)\n",
				res.Observation.Result, res.Observation.Seq, res.Observation.Isolation)
			for id, seq := range res.RedSeqsAdded {
				fmt.Fprintf(c.OutOrStdout(), "red_seq[%s] = %d (first observed failure — arms red-before-green)\n", id, seq)
			}
			if res.LeaseRenewed != "" {
				fmt.Fprintf(c.OutOrStdout(), "lease renewed to %s\n", res.LeaseRenewed)
			}
			if res.LogPath != "" {
				fmt.Fprintf(c.OutOrStdout(), "output teed to %s\n", relPath(res.LogPath))
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&node, "node", "", "node id to record the observation for")
	c.Flags().StringVar(&by, "by", "", "claimant identity (required when the node is claimed; renews the lease)")
	c.Flags().StringVar(&report, "report", "", "test report file: JUnit XML (.xml) or `go test -json` stream (.json)")
	c.Flags().IntVar(&commandExit, "command-exit", 0, "command gate: the check command's exit code")
	c.Flags().StringVar(&commandLog, "command-log", "", "command gate: file with the captured output (teed to the node log)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphReviewCmd records a review gate's observation from a persisted
// frozen Aligned review artifact (DD-9, D-0020): the gate greens only from
// all three freeze signals read together, and findings that name scope
// nodes demote them in the same compare-and-swap cycle. Mutating:
// guard-covered per D-0014.
func graphReviewCmd() *cobra.Command {
	var plan, node, artifact, by string
	var asJSON bool
	c := &cobra.Command{
		Use:   "review",
		Short: "Record a review gate's observation from a frozen Aligned review artifact",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, cmdArgs []string) error {
			if plan == "" {
				return fmt.Errorf("graph review: --plan is required")
			}
			if node == "" {
				return fmt.Errorf("graph review: --node is required")
			}
			if artifact == "" {
				return fmt.Errorf("graph review: --artifact is required (the frozen review artifact produced by `sdd review scaffold` → `sdd review resolve`)")
			}
			root, err := store.FindPlanningRoot(".")
			if err != nil {
				return fmt.Errorf("graph review: %w", err)
			}
			_, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph review: %w", err)
			}
			res, err := greview.Record(greview.Options{
				Root: root, RepoRoot: repoRoot, Plan: plan, Node: node,
				Artifact: artifact, By: by,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(res)
			}
			fmt.Fprintf(c.OutOrStdout(), "recorded review gate %s: pass at seq %d\n", res.Node, res.Observation.Seq)
			fmt.Fprintf(c.OutOrStdout(), "scope (%d node(s)): %s\n", len(res.Scope), strings.Join(res.Scope, ", "))
			if len(res.Demoted) > 0 {
				fmt.Fprintf(c.OutOrStdout(), "demoted by findings (RED, workable again): %s\n", strings.Join(res.Demoted, ", "))
			}
			if res.Merged {
				fmt.Fprintln(c.OutOrStdout(), "claim completed")
			}
			if res.WorkspaceReleased != "" {
				fmt.Fprintf(c.OutOrStdout(), "workspace released: %s\n", res.WorkspaceReleased)
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&node, "node", "", "the review-gate node id")
	c.Flags().StringVar(&artifact, "artifact", "", "path to the frozen Aligned review artifact")
	c.Flags().StringVar(&by, "by", "", "claimant identity (required when the gate node is claimed)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphConvertCmd converts a v1 markdown plan into a staged proposal whose
// unmade judgments are blocking sentinels (DD-15): hazards untriaged, gates
// unspecified, contracts marked NEEDS-CONTRACT. The converted graph does not
// compile until an operator resolves each sentinel through the payload path.
// Mutating: guard-covered per D-0014 (task 2.6 lands the entries).
func graphConvertCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "convert",
		Short: "Convert a v1 markdown plan into a staged graph proposal with blocking sentinels",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" {
				return fmt.Errorf("graph convert: --plan is required")
			}
			root, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph convert: %w", err)
			}
			res, err := gconvert.Run(root, repoRoot, plan)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(struct {
					OK               bool   `json:"ok"`
					Fragment         string `json:"fragment"`
					Nodes            int    `json:"nodes"`
					Phases           int    `json:"phases"`
					CompletedCarried int    `json:"completed_carried"`
				}{true, relPath(res.Fragment), res.Nodes, res.Phases, res.CompletedCarried})
			}
			fmt.Fprintf(c.OutOrStdout(),
				"converted %d task(s) across %d phase(s) into %s (%d completed task(s) carry v1 provenance)\n"+
					"every node carries blocking sentinels; resolve them in the staged payload, then `sdd compile --plan %s`\n"+
					"(compile's rendered views target the v1 phase-doc filenames: retiring each v1 document is a deliberate, visible step)\n",
				res.Nodes, res.Phases, relPath(res.Fragment), res.CompletedCarried, plan)
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// compileCmd is the top-level `sdd compile`: validate the staged proposal
// wholesale (parse -> schema -> semantic, every finding in one report),
// embed intent fingerprints, and append the nodes to the committed graph.
// Mutating: guard-covered per D-0014 (task 2.6 lands the entries).
func compileCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "compile",
		Short: "Compile the staged proposal into the plan graph",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" {
				return fmt.Errorf("compile: --plan is required")
			}
			root, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("compile: %w", err)
			}
			res, findings, err := gcompile.Run(root, repoRoot, plan)
			if err != nil {
				return err
			}
			if len(findings) > 0 {
				if asJSON {
					out := struct {
						OK       bool              `json:"ok"`
						Findings []gcompile.Finding `json:"findings"`
					}{false, findings}
					if err := writeJSON(out); err != nil {
						return err
					}
					return &refusedError{n: len(findings)}
				}
				var b strings.Builder
				fmt.Fprintf(&b, "compile: refused — %d finding(s), all reported:\n", len(findings))
				for _, f := range findings {
					fmt.Fprintf(&b, "  %s\n", f.String())
				}
				return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
			}
			if asJSON {
				views := make([]string, len(res.Views))
				for i, v := range res.Views {
					views[i] = relPath(v)
				}
				return writeJSON(struct {
					OK       bool                         `json:"ok"`
					Graph    string                       `json:"graph"`
					Added    []string                     `json:"added"`
					Hashes   map[string]map[string]string `json:"intent_hashes,omitempty"`
					Views    []string                     `json:"views,omitempty"`
					Consumed string                       `json:"consumed"`
				}{true, relPath(res.GraphPath), res.Added, res.Hashes, views, relPath(res.Consumed)})
			}
			hashed := 0
			for _, m := range res.Hashes {
				hashed += len(m)
			}
			fmt.Fprintf(c.OutOrStdout(), "compiled %d node(s) into %s (%d intent fingerprint(s) embedded, %d view(s) rendered); consumed %s\n",
				len(res.Added), relPath(res.GraphPath), hashed, len(res.Views), relPath(res.Consumed))
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphNext serves the frontier for a graph-executed plan: the read form
// lists claimable work critical-path-first, --claim records a lease and
// prints the full context payload (contract, inlined cited requirement
// text, tests, hazards, workspace) so the agent needs no other reads to
// start (Designs/SddGraph § The execution loop). Returns handled=false when
// the plan has no committed graph, so v1 plans fall through untouched.
func graphNext(planPath string, claim bool, by string, jsonOut bool) (bool, error) {
	readme, err := resolvePlanReadme(planPath)
	if err != nil {
		return false, nil // let the v1 path report the resolution problem
	}
	planDir := filepath.Dir(readme)
	if _, err := os.Stat(gstore.PathFor(planDir)); err != nil {
		return false, nil
	}
	plan := filepath.Base(planDir)
	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return true, fmt.Errorf("next: %w", err)
	}
	items, err := gcompile.CurrentIntent(root, repoRoot, plan)
	if err != nil {
		return true, fmt.Errorf("next: %w", err)
	}
	hashes := make(map[string]string, len(items))
	for id, item := range items {
		hashes[id] = item.Hash
	}
	digester := digest.New(repoRoot)
	statesInputs := func(g *model.Graph) states.Inputs {
		return states.Inputs{Graph: g, ArtifactDigest: digester.Artifact, CurrentIntentHashes: hashes}
	}

	if !claim {
		g, err := gstore.Load(gstore.PathFor(planDir))
		if err != nil {
			return true, fmt.Errorf("next: %w", err)
		}
		derived := states.Derive(statesInputs(g))
		adjacency := algorithms.Graph{}
		estimate := map[string]int{}
		claimed := 0
		counts := map[states.State]int{}
		for i := range g.Nodes {
			n := &g.Nodes[i]
			adjacency[n.ID] = n.Deps
			estimate[n.ID] = n.Estimate
			counts[derived[n.ID].State]++
			if n.Claim != nil {
				claimed++
			}
		}
		weight := algorithms.CriticalWeight(adjacency, estimate)
		frontier := states.Frontier(derived)
		sort.SliceStable(frontier, func(i, j int) bool {
			if weight[frontier[i]] != weight[frontier[j]] {
				return weight[frontier[i]] > weight[frontier[j]]
			}
			return frontier[i] < frontier[j]
		})
		if jsonOut {
			type row struct {
				ID     string `json:"id"`
				State  string `json:"state"`
				Weight int    `json:"critical_weight"`
			}
			out := struct {
				OK       bool                    `json:"ok"`
				Plan     string                  `json:"plan"`
				States   map[states.State]int    `json:"states"`
				Claimed  int                     `json:"claimed"`
				Frontier []row                   `json:"frontier"`
			}{true, plan, counts, claimed, nil}
			for _, id := range frontier {
				out.Frontier = append(out.Frontier, row{id, string(derived[id].State), weight[id]})
			}
			return true, writeJSON(out)
		}
		fmt.Printf("%s: %d node(s)", plan, len(g.Nodes))
		for _, s := range []states.State{states.Green, states.Ready, states.Red, states.Stale, states.Blocked} {
			if counts[s] > 0 {
				fmt.Printf("  %s=%d", s, counts[s])
			}
		}
		fmt.Printf("  claimed=%d\n", claimed)
		if len(frontier) == 0 {
			fmt.Println("frontier: empty")
			return true, nil
		}
		for i, id := range frontier {
			mark := ""
			if i == 0 {
				mark = "  [critical path]"
			}
			if c := g.NodeByID(id).Claim; c != nil {
				mark += "  [claimed by " + c.By + "]"
			}
			fmt.Printf("%2d. %s (%s, weight %d)%s\n", i+1, id, derived[id].State, weight[id], mark)
		}
		fmt.Println("claim the head with `sdd next " + planPath + " --claim`")
		return true, nil
	}

	if by == "" {
		by = "agent-" + randHex(4)
	}
	cfg, _ := store.LoadConfig(".")
	ttl := time.Duration(cfg.GraphLeaseTtlMinutes) * time.Minute
	prov := provider.Detect(repoRoot, planDir)
	claimed, err := claims.Claim(planDir, claims.Options{
		By: by, TTL: ttl, StatesInputs: statesInputs, Provider: provider.ForClaims(prov),
	})
	if err != nil {
		return true, err
	}
	node := claimed.Node
	type citedText struct {
		ID   string `json:"id"`
		Text string `json:"text,omitempty"`
	}
	var cited []citedText
	for _, id := range node.Justifies {
		cited = append(cited, citedText{ID: id, Text: items[id].Normalized})
	}
	if jsonOut {
		return true, writeJSON(struct {
			OK           bool        `json:"ok"`
			Node         model.Node  `json:"node"`
			Cited        []citedText `json:"cited"`
			LeaseExpires string      `json:"lease_expires"`
			By           string      `json:"by"`
			Workspace    string      `json:"workspace,omitempty"`
			Reclaimed    []string    `json:"reclaimed_expired,omitempty"`
		}{true, node, cited, claimed.LeaseExpires, by, claimed.Workspace, claimed.ReclaimedExpired})
	}
	fmt.Printf("claimed %s (by %s, lease expires %s)\n\n", node.ID, by, claimed.LeaseExpires)
	fmt.Printf("contract: %s\n", node.Contract)
	for _, c := range cited {
		if c.Text != "" {
			fmt.Printf("justifies %s: %s\n", c.ID, c.Text)
		} else {
			fmt.Printf("justifies %s\n", c.ID)
		}
	}
	fmt.Printf("gate: %s\nhazards: %s\n", describeGateBrief(node.Gate), describeHazardsBrief(node.Hazards))
	if len(node.Artifacts) > 0 {
		fmt.Printf("artifacts: %s\n", strings.Join(node.Artifacts, ", "))
	}
	if node.History != "" {
		fmt.Printf("history: %s\n", node.History)
	}
	if claimed.Workspace != "" {
		fmt.Printf("workspace: %s\n", claimed.Workspace)
	}
	if len(claimed.ReclaimedExpired) > 0 {
		fmt.Printf("reclaimed expired claim(s): %s\n", strings.Join(claimed.ReclaimedExpired, ", "))
	}
	return true, nil
}

func describeGateBrief(g model.Gate) string {
	switch g.Type {
	case model.GateTests:
		var ids []string
		for _, t := range g.Tests {
			ids = append(ids, t.ID)
		}
		return "tests (" + strings.Join(ids, ", ") + ")"
	case model.GateCommand:
		return "command `" + g.Command + "`"
	case model.GateReview:
		if g.Lanes == nil {
			return "review (full)"
		}
		return "review (" + strings.Join(g.Lanes, ", ") + ")"
	default:
		return g.Type
	}
}

func describeHazardsBrief(h model.Hazards) string {
	switch {
	case h == nil:
		return "UNTRIAGED"
	case len(h) == 0:
		return "none (explicit)"
	default:
		return strings.Join(h, ", ")
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return fmt.Sprintf("%x", b)
}

// graphReleaseCmd is the graceful abandonment path: holder-only unless
// --force names the takeover deliberately (DD-10).
func graphReleaseCmd() *cobra.Command {
	var plan, by string
	var force, asJSON bool
	c := &cobra.Command{
		Use:   "release <node-id>",
		Short: "Release a claimed node back to the frontier",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			planDir, err := planDirFor(plan, "release")
			if err != nil {
				return err
			}
			if by == "" && !force {
				return fmt.Errorf("graph release: --by is required (a lease is released by its holder; use --force for a deliberate takeover)")
			}
			workspace, err := claims.Release(planDir, args[0], by, force)
			if err != nil {
				return err
			}
			// Graceful abandonment tears the workspace down (unlike lease
			// EXPIRY, which preserves it for post-mortem).
			cleaned := false
			if workspace != "" {
				_, repoRoot, rootsErr := resolveRoots(".", "")
				if rootsErr == nil {
					if relErr := provider.Detect(repoRoot, planDir).Release(workspace); relErr == nil {
						cleaned = true
					} else {
						fmt.Fprintf(c.ErrOrStderr(), "warning: workspace %s could not be removed: %v\n", workspace, relErr)
					}
				}
			}
			if asJSON {
				return writeJSON(struct {
					OK        bool   `json:"ok"`
					Node      string `json:"node"`
					Workspace string `json:"workspace,omitempty"`
					Cleaned   bool   `json:"workspace_cleaned,omitempty"`
				}{true, args[0], workspace, cleaned})
			}
			fmt.Fprintf(c.OutOrStdout(), "released %s back to the frontier\n", args[0])
			if workspace != "" && cleaned {
				fmt.Fprintf(c.OutOrStdout(), "workspace removed: %s\n", workspace)
			} else if workspace != "" {
				fmt.Fprintf(c.OutOrStdout(), "workspace left in place: %s\n", workspace)
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&by, "by", "", "claimant identity that holds the lease")
	c.Flags().BoolVar(&force, "force", false, "take over a claim held by someone else, deliberately")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// planDirFor resolves Plans/<plan> under the planning root — the shared
// preamble of every plan-scoped graph verb.
func planDirFor(plan, verb string) (string, error) {
	if plan == "" {
		return "", fmt.Errorf("graph %s: --plan is required", verb)
	}
	root, err := store.FindPlanningRoot(".")
	if err != nil {
		return "", fmt.Errorf("graph %s: %w", verb, err)
	}
	return filepath.Join(root, "Plans", plan), nil
}

// graphProposeCmd stages one payload file as a fragment: validated
// wholesale, refused without staging on any finding (DD-11 — construction
// is declarative and batched; the repair loop is an edit to the payload
// file). Mutating: guard-covered per D-0014 (task 2.6 lands the entries).
func graphProposeCmd() *cobra.Command {
	var plan, file string
	var asJSON bool
	c := &cobra.Command{
		Use:   "propose",
		Short: "Validate a proposal payload and stage it as a fragment",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			planDir, err := planDirFor(plan, "propose")
			if err != nil {
				return err
			}
			if file == "" {
				return fmt.Errorf("graph propose: --file is required (author the payload from `sdd template graph-proposal`)")
			}
			payload, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("graph propose: %w", err)
			}
			staged, err := proposal.Stage(planDir, payload)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(struct {
					OK       bool   `json:"ok"`
					Fragment string `json:"fragment"`
				}{true, relPath(staged)})
			}
			fmt.Fprintf(c.OutOrStdout(), "staged %s\n", relPath(staged))
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&file, "file", "", "payload file to validate and stage")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphAssembleCmd merges every staged fragment into one proposal set for
// compile, refusing node-id collisions by naming both declaring fragments.
// Mutating: guard-covered per D-0014 (task 2.6 lands the entries).
func graphAssembleCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "assemble",
		Short: "Merge staged fragments into one proposal set",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			planDir, err := planDirFor(plan, "assemble")
			if err != nil {
				return err
			}
			assembled, merged, err := proposal.Assemble(planDir)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(struct {
					OK       bool   `json:"ok"`
					Proposal string `json:"proposal"`
					Nodes    int    `json:"nodes"`
				}{true, relPath(assembled), len(merged.Nodes)})
			}
			fmt.Fprintf(c.OutOrStdout(), "assembled %s (%d nodes)\n", relPath(assembled), len(merged.Nodes))
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphInitCmd creates a plan's empty committed graph plus the .gitignore
// entries that keep lock sidecars and the .graph/ workspace out of version
// control. Mutating: guard-covered per D-0014 (task 2.6 lands the entries).
func graphInitCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Create an empty plan graph (Plans/<Plan>/<Plan>-Graph.json)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" {
				return fmt.Errorf("graph init: --plan is required")
			}
			root, err := store.FindPlanningRoot(".")
			if err != nil {
				return fmt.Errorf("graph init: %w", err)
			}
			planDir := filepath.Join(root, "Plans", plan)
			path, err := gstore.Init(planDir)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(struct {
					OK   bool   `json:"ok"`
					Path string `json:"path"`
				}{true, relPath(path)})
			}
			fmt.Fprintf(c.OutOrStdout(), "initialized %s\n", relPath(path))
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

// graphHazardsCmd prints the closed hazard vocabulary and the test shape
// each hazard requires. Read-only; exists so triage during decomposition is
// a lookup, not a memory exercise.
func graphHazardsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "hazards",
		Short: "List the closed hazard vocabulary and each hazard's required test shape",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			all := hazards.All()
			if asJSON {
				enc := json.NewEncoder(c.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(all)
			}
			width := 0
			for _, h := range all {
				if len(h.Name) > width {
					width = len(h.Name)
				}
			}
			for _, h := range all {
				fmt.Fprintf(c.OutOrStdout(), "%-*s  requires a test that %s\n", width, h.Name, h.RequiresTestThat)
			}
			fmt.Fprintf(c.OutOrStdout(), "\nAn empty hazard list is a legitimate claim, but it must be explicit; a node nobody has triaged carries the string \"untriaged\" and will not compile.\n")
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the vocabulary as JSON")
	return c
}

