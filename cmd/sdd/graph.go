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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/hazards"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
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
