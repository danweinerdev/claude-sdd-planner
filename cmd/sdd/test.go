package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testevidence"
	"github.com/spf13/cobra"
)

func testCmd() *cobra.Command {
	c := &cobra.Command{Use: "test", Short: "Capture and check owned observed test evidence"}
	var plan, node, by, phase, redKind, fault string
	var runJSON bool
	run := &cobra.Command{Use: "run", Short: "Run an observed-v1 gate and publish an immutable attempt", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if plan == "" || node == "" || by == "" || phase == "" {
				return fmt.Errorf("test run: --plan, --node, --by, and --phase are required")
			}
			planningRoot, repoRoot, planDir, err := testRoots(plan)
			if err != nil {
				return err
			}
			res, err := testevidence.Run(cmd.Context(), testevidence.RunOptions{PlanDir: planDir, PlanningRoot: planningRoot, RepoRoot: repoRoot, Node: node, By: by, Phase: phase, RedKind: redKind, Fault: fault})
			if res != nil && runJSON {
				if err := writeJSON(res); err != nil {
					return err
				}
			} else if res != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "attempt %s: %s", res.AttemptID, res.ExecutionStatus)
				if res.Result != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " (%s)", res.Result)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if err != nil {
				return err
			}
			if res.ExecutionStatus == "failed" {
				return &refusedError{n: 1, msg: "selected tests completed with failures"}
			}
			if res.ExecutionStatus != "passed" {
				return fmt.Errorf("test run: evidence could not be completed: %s", res.Refusal)
			}
			return nil
		}}
	rf := run.Flags()
	rf.StringVar(&plan, "plan", "", "plan name")
	rf.StringVar(&node, "node", "", "node id")
	rf.StringVar(&by, "by", "", "current claim holder")
	rf.StringVar(&phase, "phase", "", "red|green|diagnostic")
	rf.StringVar(&redKind, "red-kind", "", "baseline|sensitivity for red")
	rf.StringVar(&fault, "fault", "", "concise sensitivity fault description")
	rf.BoolVar(&runJSON, "json", false, "emit JSON")
	var checkPlan, checkNode, attempt, expect string
	var checkJSON bool
	check := &cobra.Command{Use: "check", Short: "Read-only integrity and candidate check of an attempt", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if checkPlan == "" || checkNode == "" || attempt == "" || expect == "" {
			return fmt.Errorf("test check: --plan, --node, --attempt, and --expect are required")
		}
		planningRoot, repoRoot, planDir, err := testRoots(checkPlan)
		if err != nil {
			return err
		}
		res, err := testevidence.Check(testevidence.CheckOptions{PlanDir: planDir, PlanningRoot: planningRoot, RepoRoot: repoRoot, Node: checkNode, AttemptID: attempt, Expect: expect})
		if err != nil {
			return err
		}
		if checkJSON {
			if err := writeJSON(res); err != nil {
				return err
			}
		} else if res.Eligible {
			fmt.Fprintf(cmd.OutOrStdout(), "attempt %s is eligible for %s admission\n", attempt, expect)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "attempt %s is inadmissible: %s\n", attempt, res.Refusal)
		}
		if !res.Eligible {
			return &refusedError{n: 1, msg: "test check: " + res.Refusal}
		}
		return nil
	}}
	cf := check.Flags()
	cf.StringVar(&checkPlan, "plan", "", "plan name")
	cf.StringVar(&checkNode, "node", "", "node id")
	cf.StringVar(&attempt, "attempt", "", "immutable attempt id")
	cf.StringVar(&expect, "expect", "", "red|green")
	cf.BoolVar(&checkJSON, "json", false, "emit JSON")
	c.AddCommand(run, check)
	var cleanupPlan, cleanupNode, cleanupAttempt string
	var cleanupJSON, abandon bool
	cleanup := &cobra.Command{Use: "cleanup", Short: "Explicitly remove one inactive owned attempt bundle", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if cleanupPlan == "" || cleanupNode == "" || cleanupAttempt == "" {
			return fmt.Errorf("test cleanup: --plan, --node, and --attempt are required")
		}
		_, _, planDir, err := testRoots(cleanupPlan)
		if err != nil {
			return err
		}
		res, err := testevidence.Cleanup(testevidence.CleanupOptions{PlanDir: planDir, Node: cleanupNode, AttemptID: cleanupAttempt, Abandon: abandon})
		if err != nil {
			return err
		}
		if cleanupJSON {
			return writeJSON(res)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "removed attempt %s\n", res.AttemptID)
		return nil
	}}
	clf := cleanup.Flags()
	clf.StringVar(&cleanupPlan, "plan", "", "plan name")
	clf.StringVar(&cleanupNode, "node", "", "node id")
	clf.StringVar(&cleanupAttempt, "attempt", "", "attempt id")
	clf.BoolVar(&abandon, "abandon", false, "explicitly discard a finalized unadmitted attempt")
	clf.BoolVar(&cleanupJSON, "json", false, "emit JSON")
	c.AddCommand(cleanup)
	return c
}

func testRoots(plan string) (string, string, string, error) {
	if err := validPlanName(plan); err != nil {
		return "", "", "", fmt.Errorf("test: %w", err)
	}
	root, baseRepo, err := resolveRoots(".", "")
	if err != nil {
		return "", "", "", fmt.Errorf("test: %w", err)
	}
	planDir := filepath.Join(root, "Plans", plan)
	if st, err := os.Stat(planDir); err != nil || !st.IsDir() {
		return "", "", "", fmt.Errorf("test: plan %q does not exist", plan)
	}
	sources, err := gcompile.NewSources(root, baseRepo, plan)
	if err != nil {
		return "", "", "", fmt.Errorf("test: resolve plan sources: %w", err)
	}
	repo := strings.TrimSpace(sources.RepositoryRoot())
	if repo == "" {
		return "", "", "", fmt.Errorf("test: target repository did not resolve")
	}
	return root, repo, planDir, nil
}

func validPlanName(plan string) error {
	if plan == "" || plan == "." || plan == ".." || filepath.IsAbs(plan) || filepath.Base(plan) != plan || strings.ContainsAny(plan, `/\:`) || strings.HasSuffix(plan, ".") || strings.HasSuffix(plan, " ") {
		return fmt.Errorf("invalid plan name %q", plan)
	}
	base := strings.ToUpper(strings.SplitN(plan, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
		return fmt.Errorf("invalid plan name %q", plan)
	}
	return nil
}
