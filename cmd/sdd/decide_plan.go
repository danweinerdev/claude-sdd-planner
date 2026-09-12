// Command sdd, subcommand `decide`: the per-plan decision record
// (Designs/PlanDecisions). One flat JSON array per plan, five fields per
// entry, content-addressed ids, append-only. `add` is the only write; it is
// called once per decision after the user approves the exact statement.
// `list`, `current`, and `lookup` derive on every call.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

func decideCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "decide",
		Short: "Record and read per-plan decisions",
		Long: `Decisions live in Plans/<Name>/<Name>-Decisions.json: one flat array,
five fields per entry (id, date, statement, supersedes, source), append-only.
The id is a digest of the statement, so it never renumbers and two branches
recording the same sentence converge. 'add' is the only write path: show the
user the exact statement, get approval, run 'add' once.`,
	}

	var addPlan, statement, supersedes, source string
	var addJSON bool
	add := &cobra.Command{
		Use: "add", Short: "Append one user-approved decision", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return cmdDecideAdd(c, addPlan, statement, supersedes, source, addJSON)
		},
	}
	add.Flags().StringVar(&addPlan, "plan", "", "plan name (directory under Plans/)")
	add.Flags().StringVar(&statement, "statement", "", "the decision, exactly as the user approved it")
	add.Flags().StringVar(&supersedes, "supersedes", "", "pd-<hex> or <Plan>:pd-<hex>; comma-separate several to reconcile competing successors")
	add.Flags().StringVar(&source, "source", "", "provenance: Designs/<X>:DD-N or <review>.md:F-NN")
	add.Flags().BoolVar(&addJSON, "json", false, "emit the written entry as JSON")
	_ = add.MarkFlagRequired("plan")
	_ = add.MarkFlagRequired("statement")

	var listPlan string
	var listJSON bool
	list := &cobra.Command{
		Use: "list", Short: "Print one plan's decisions in append order", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return cmdDecideList(c, listPlan, listJSON) },
	}
	list.Flags().StringVar(&listPlan, "plan", "", "plan name (directory under Plans/)")
	list.Flags().BoolVar(&listJSON, "json", false, "emit JSON")
	_ = list.MarkFlagRequired("plan")

	var curPlan string
	var curJSON bool
	current := &cobra.Command{
		Use: "current", Short: "Derive the standing decisions across every plan", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return cmdDecideCurrent(c, curPlan, curJSON) },
	}
	current.Flags().StringVar(&curPlan, "plan", "", "narrow to decisions carried by this plan")
	current.Flags().BoolVar(&curJSON, "json", false, "emit JSON")

	var lookPlan string
	var lookJSON bool
	lookup := &cobra.Command{
		Use: "lookup <pd-id | Plan:pd-id>", Short: "Print one decision and its supersession chain", Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error { return cmdDecideLookup(c, args[0], lookPlan, lookJSON) },
	}
	lookup.Flags().StringVar(&lookPlan, "plan", "", "plan a bare id is read from")
	lookup.Flags().BoolVar(&lookJSON, "json", false, "emit JSON")

	c.AddCommand(add, list, current, lookup)
	return c
}

func decisionsRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return store.FindPlanningRoot(wd)
}

func decisionIndex() (string, *decisions.Index, error) {
	root, err := decisionsRoot()
	if err != nil {
		return "", nil, err
	}
	files, err := decisions.LoadRoot(root)
	if err != nil {
		return "", nil, err
	}
	for _, f := range files {
		if f.Err != nil {
			return "", nil, fmt.Errorf("decide: %v", f.Err)
		}
	}
	return root, decisions.NewIndex(files), nil
}

func cmdDecideAdd(c *cobra.Command, plan, statement, supersedes, source string, asJSON bool) error {
	root, index, err := decisionIndex()
	if err != nil {
		return err
	}
	planDir := filepath.Join(root, "Plans", plan)
	if info, err := os.Stat(planDir); err != nil || !info.IsDir() {
		return fmt.Errorf("decide: plan %q has no directory under Plans/", plan)
	}
	res, err := decisions.Append(decisions.PathFor(planDir), statement, supersedes, source, time.Now().UTC().Format("2006-01-02"), plan, index)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(struct {
			OK      bool            `json:"ok"`
			Created bool            `json:"created"`
			Path    string          `json:"path"`
			Entry   decisions.Entry `json:"entry"`
		}{true, res.Created, relPath(res.Path), res.Entry})
	}
	verb := "recorded"
	if !res.Created {
		verb = "already recorded"
	}
	fmt.Fprintf(c.OutOrStdout(), "%s %s in %s\n", verb, res.Entry.ID, relPath(res.Path))
	printEntry(c, res.Entry)
	return nil
}

func cmdDecideList(c *cobra.Command, plan string, asJSON bool) error {
	root, err := decisionsRoot()
	if err != nil {
		return err
	}
	path := decisions.PathFor(filepath.Join(root, "Plans", plan))
	entries, err := decisions.Load(path)
	if err != nil {
		return fmt.Errorf("decide: %w", err)
	}
	if asJSON {
		if entries == nil {
			entries = []decisions.Entry{}
		}
		return writeJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Fprintf(c.OutOrStdout(), "no decisions recorded for %s\n", plan)
		return nil
	}
	for _, e := range entries {
		printEntry(c, e)
	}
	return nil
}

func cmdDecideCurrent(c *cobra.Command, plan string, asJSON bool) error {
	_, index, err := decisionIndex()
	if err != nil {
		return err
	}
	cur := index.Current(plan)
	conflicts := index.Conflicts()
	if asJSON {
		if cur == nil {
			cur = []decisions.CurrentEntry{}
		}
		if conflicts == nil {
			conflicts = []decisions.Conflict{}
		}
		return writeJSON(struct {
			Current   []decisions.CurrentEntry `json:"current"`
			Conflicts []decisions.Conflict     `json:"conflicts"`
		}{cur, conflicts})
	}
	for _, k := range conflicts {
		var names []string
		for _, s := range k.Successors {
			names = append(names, s.Plan+":"+s.Entry.ID)
		}
		fmt.Fprintf(c.OutOrStdout(), "CONFLICT %s has competing successors: %s\n    reconcile with one `sdd decide add --supersedes %s`\n", k.ID, strings.Join(names, ", "), strings.Join(names, ","))
	}
	if len(cur) == 0 {
		fmt.Fprintln(c.OutOrStdout(), "no standing decisions")
		return nil
	}
	for _, e := range cur {
		fmt.Fprintf(c.OutOrStdout(), "%s  %s  [%s]\n    %s\n", e.ID, e.Date, strings.Join(e.Plans, ", "), e.Statement)
		if e.Source != "" {
			fmt.Fprintf(c.OutOrStdout(), "    source: %s\n", e.Source)
		}
	}
	return nil
}

func cmdDecideLookup(c *cobra.Command, ref, plan string, asJSON bool) error {
	_, index, err := decisionIndex()
	if err != nil {
		return err
	}
	chain, ok := index.Lookup(ref, plan)
	if !ok {
		if amb := index.Ambiguous(ref, plan); len(amb) > 0 {
			return fmt.Errorf("decide: %s is carried by more than one plan; qualify it (%s)", ref, strings.Join(amb, ", "))
		}
		return fmt.Errorf("decide: %s is not recorded in any plan under the planning root", ref)
	}
	if asJSON {
		return writeJSON(chain)
	}
	fmt.Fprintf(c.OutOrStdout(), "%s (plan %s)\n", chain.Found.Entry.ID, chain.Found.Plan)
	printEntry(c, chain.Found.Entry)
	for _, prev := range chain.Supersedes {
		fmt.Fprintf(c.OutOrStdout(), "supersedes %s:%s\n    %s\n", prev.Plan, prev.Entry.ID, prev.Entry.Statement)
	}
	if chain.SupersededBy != nil {
		fmt.Fprintf(c.OutOrStdout(), "superseded by %s:%s\n    %s\n", chain.SupersededBy.Plan, chain.SupersededBy.Entry.ID, chain.SupersededBy.Entry.Statement)
	}
	return nil
}

func printEntry(c *cobra.Command, e decisions.Entry) {
	fmt.Fprintf(c.OutOrStdout(), "%s  %s\n    %s\n", e.ID, e.Date, e.Statement)
	if e.Supersedes != "" {
		fmt.Fprintf(c.OutOrStdout(), "    supersedes: %s\n", e.Supersedes)
	}
	if e.Source != "" {
		fmt.Fprintf(c.OutOrStdout(), "    source: %s\n", e.Source)
	}
}
