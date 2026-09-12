package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/ops"
	"github.com/spf13/cobra"
)

// graphRemapRevisionsCmd records ordinary Git rewrite identity without
// rewriting the observations or proof anchored to the original commits.
func graphRemapRevisionsCmd() *cobra.Command {
	var plan, mapFile, expect string
	var dryRun, asJSON bool
	c := &cobra.Command{
		Use:   "remap-revisions",
		Short: "Append Git rewrite lineage without changing recorded observations",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if plan == "" || mapFile == "" || (!dryRun && expect == "") {
				return fmt.Errorf("graph remap-revisions: --plan, --map, and --expect-digest are required (omit --expect-digest only with --dry-run)")
			}
			root, repoRoot, err := resolveRoots(".", "")
			if err != nil {
				return fmt.Errorf("graph remap-revisions: %w", err)
			}
			raw, err := os.ReadFile(mapFile)
			if err != nil {
				return fmt.Errorf("graph remap-revisions: reading --map: %w", err)
			}
			res, err := ops.RemapRevisions(ops.RemapOptions{Root: root, RepoRoot: repoRoot, Plan: plan, Mapping: raw, ExpectDigest: expect, DryRun: dryRun})
			if err != nil {
				var refusal *ops.RefusedError
				if errors.As(err, &refusal) {
					if asJSON {
						if werr := writeJSON(struct {
							OK      bool     `json:"ok"`
							Reasons []string `json:"reasons"`
						}{false, refusal.Reasons}); werr != nil {
							return werr
						}
					}
					return &refusedError{n: len(refusal.Reasons), msg: refusal.Error()}
				}
				return err
			}
			if asJSON {
				return writeJSON(res)
			}
			w := c.OutOrStdout()
			for _, outcome := range res.Outcomes {
				switch outcome.Status {
				case ops.RemapRecorded:
					verb := "recorded"
					if dryRun {
						verb = "would record"
					}
					fmt.Fprintf(w, "%s %s -> %s\n", verb, outcome.Old, outcome.New)
				case ops.RemapAlreadyRecorded:
					fmt.Fprintf(w, "already recorded %s -> %s (no-op)\n", outcome.Old, outcome.New)
				case ops.RemapUnchanged:
					fmt.Fprintf(w, "unchanged %s -> %s (self-mapping no-op; not stored)\n", outcome.Old, outcome.New)
				case ops.RemapUnreferenced:
					fmt.Fprintf(w, "skipped unreferenced %s -> %s (not stored)\n", outcome.Old, outcome.New)
				}
			}
			fmt.Fprintf(w, "expect-digest: %s\n", res.ExpectDigest)
			if res.Applied {
				fmt.Fprintf(w, "new-digest: %s\n", res.NewDigest)
			}
			fmt.Fprintf(w, "note: %s.\n", res.Note)
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan name (directory under Plans/)")
	c.Flags().StringVar(&mapFile, "map", "", "two-column native post-rewrite mapping file: OLD_COMMIT NEW_COMMIT")
	c.Flags().StringVar(&expect, "expect-digest", "", "graph digest from the dry-run preview (required unless --dry-run)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "validate and report the lineage update without writing the graph")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the result as JSON")
	return c
}
