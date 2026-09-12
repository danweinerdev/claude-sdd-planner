package main

// `sdd graph audit` — the formal read-only audit: one structured report over
// a committed plan graph, reusing compile's resolver and validation. Read-only:
// guard-allowlisted; it derives and reports, never mutates.

import (
	"fmt"
	"strings"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/spf13/cobra"
)

func graphAuditCmd() *cobra.Command {
	var plan string
	var asJSON bool
	c := &cobra.Command{
		Use:   "audit",
		Short: "Formal read-only audit: counts, coverage, staleness, duplicate/shared tests, unresolved inputs",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" {
				return fmt.Errorf("graph audit: --plan is required")
			}
			root, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph audit: %w", err)
			}
			rep, err := gcompile.Audit(root, repoRoot, plan)
			if err != nil {
				return err
			}
			if asJSON {
				if err := writeJSON(rep); err != nil {
					return err
				}
				if !rep.OK {
					return &refusedError{n: len(rep.Findings)}
				}
				return nil
			}
			w := c.OutOrStdout()
			fmt.Fprintf(w, "%s: %d node(s), %d test(s), %d input(s)\n",
				rep.Plan, rep.Counts.Nodes, rep.Counts.Tests, rep.Counts.Inputs)
			if len(rep.Counts.Gates) > 0 {
				var gates []string
				for _, t := range []string{"tests", "command", "review", "unspecified"} {
					if n := rep.Counts.Gates[t]; n > 0 {
						gates = append(gates, fmt.Sprintf("%s=%d", t, n))
					}
				}
				fmt.Fprintf(w, "gates: %s\n", strings.Join(gates, " "))
			}
			fmt.Fprintf(w, "hazards: triaged=%d untriaged=%d\n",
				rep.Counts.Hazards["triaged"], rep.Counts.Hazards["untriaged"])

			// Coverage (informative; AC-on-direct-spec is the mandatory row).
			for _, sc := range rep.Coverage {
				var rows []string
				for _, fc := range sc.Families {
					mark := ""
					if fc.Mandatory {
						mark = " [mandatory]"
					}
					rows = append(rows, fmt.Sprintf("%s %d/%d%s", fc.Family, fc.Covered, fc.Defined, mark))
				}
				fmt.Fprintf(w, "coverage %s: %s\n", sc.Source, strings.Join(rows, ", "))
			}

			if len(rep.Findings) > 0 {
				fmt.Fprintf(w, "findings: %d (mandatory compile errors)\n", len(rep.Findings))
				for _, f := range rep.Findings {
					fmt.Fprintf(w, "  %s\n", f.String())
				}
			}
			if len(rep.Stale) > 0 {
				fmt.Fprintf(w, "stale nodes: %d\n", len(rep.Stale))
				for _, s := range rep.Stale {
					var reasons []string
					if s.SeqStale {
						reasons = append(reasons, "seq (legacy observation)")
					}
					if len(s.DependencyStale) > 0 {
						reasons = append(reasons, "dependency:"+strings.Join(s.DependencyStale, ","))
					}
					if len(s.ReviewStale) > 0 {
						reasons = append(reasons, "review:"+strings.Join(s.ReviewStale, ","))
					}
					if len(s.DigestStale) > 0 {
						reasons = append(reasons, "digest:"+strings.Join(s.DigestStale, ","))
					}
					if len(s.IntentStale) > 0 {
						reasons = append(reasons, "intent:"+strings.Join(s.IntentStale, ","))
					}
					if len(s.InputStale) > 0 {
						reasons = append(reasons, "input:"+strings.Join(s.InputStale, ","))
					}
					fmt.Fprintf(w, "  %s (%s)\n", s.ID, strings.Join(reasons, "; "))
				}
			}
			if len(rep.DuplicateTests) > 0 {
				fmt.Fprintf(w, "duplicate tests within a node: %d\n", len(rep.DuplicateTests))
				for _, d := range rep.DuplicateTests {
					fmt.Fprintf(w, "  %s: %s\n", d.Node, d.TestID)
				}
			}
			if len(rep.SharedTests) > 0 {
				fmt.Fprintf(w, "shared tests across nodes (informational): %d\n", len(rep.SharedTests))
				for _, s := range rep.SharedTests {
					fmt.Fprintf(w, "  %s: %s\n", s.TestID, strings.Join(s.Nodes, ", "))
				}
			}
			if len(rep.UnresolvedInputs) > 0 {
				fmt.Fprintf(w, "unresolved inputs: %d\n", len(rep.UnresolvedInputs))
				for _, u := range rep.UnresolvedInputs {
					fmt.Fprintf(w, "  %s: %s (%s)\n", u.Node, u.Input, u.Error)
				}
			}
			if !rep.OK {
				return &refusedError{n: len(rep.Findings)}
			}
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON")
	return c
}
