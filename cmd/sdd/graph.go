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
	"path/filepath"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/hazards"
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
