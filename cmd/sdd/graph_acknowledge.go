package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/ops"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// graphAcknowledgeCmd records the judgment that a citation's or input's text
// changed without changing a node's obligation (VerificationFreshness DD-3).
// It rebinds one compile anchor and appends an acknowledgement record; it
// writes no observation and can green nothing. Mutating.
func graphAcknowledgeCmd() *cobra.Command {
	var plan, node, citation, input, expect, by string
	var dryRun, asJSON bool
	c := &cobra.Command{
		Use:   "acknowledge",
		Short: "Record that a cited requirement's or input's text changed without changing the obligation",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" || node == "" {
				return fmt.Errorf("graph acknowledge: --plan and --node are required")
			}
			root, err := store.FindPlanningRoot(".")
			if err != nil {
				return fmt.Errorf("graph acknowledge: %w", err)
			}
			_, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph acknowledge: %w", err)
			}
			res, err := ops.Acknowledge(ops.AcknowledgeOptions{
				Root: root, RepoRoot: repoRoot, Plan: plan, Node: node,
				Citation: citation, Input: input, ExpectDigest: expect, By: by, DryRun: dryRun,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(res)
			}
			w := c.OutOrStdout()
			r := res.Record
			if !res.Applied {
				fmt.Fprintf(w, "dry run: would rebind %s %s on %s\n  from %s\n  to   %s\nexpect-digest: %s\n", r.Kind, r.Key, r.Node, shortOr(r.Old, "<none>"), r.New, res.ExpectDigest)
				return nil
			}
			fmt.Fprintf(w, "acknowledged %s %s on %s at seq %d (anchor %s → %s)\n", r.Kind, r.Key, r.Node, r.Seq, shortOr(r.Old, "<none>"), r.New)
			fmt.Fprintln(w, "the anchor is rebound; GREEN still needs a pass whose own snapshot matches the current text")
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&node, "node", "", "node whose anchor is rebound")
	c.Flags().StringVar(&citation, "citation", "", "cited id whose text changed (one of --citation / --input)")
	c.Flags().StringVar(&input, "input", "", "declared input key whose text changed (as `graph show` prints it)")
	c.Flags().StringVar(&expect, "expect-digest", "", "graph digest the preview was computed against")
	c.Flags().StringVar(&by, "by", "", "identity recording the judgment")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the rebinding and expect-digest without writing")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}

func shortOr(s, empty string) string {
	if s == "" {
		return empty
	}
	if len(s) > 19 {
		return s[:19]
	}
	return s
}
