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
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
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

	var renderPlan string
	var renderJSON bool
	render := &cobra.Command{
		Use: "render", Short: "Write the plan's generated Design.md from its decisions and graph", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return cmdDecideRender(c, renderPlan, renderJSON) },
	}
	render.Flags().StringVar(&renderPlan, "plan", "", "plan name (directory under Plans/)")
	render.Flags().BoolVar(&renderJSON, "json", false, "emit JSON")
	_ = render.MarkFlagRequired("plan")

	var syncPlan string
	var syncJSON bool
	sync := &cobra.Command{
		Use: "sync", Short: "Copy the related designs' DD bullets into the plan's decisions file (what compile does first)", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return cmdDecideSync(c, syncPlan, syncJSON) },
	}
	sync.Flags().StringVar(&syncPlan, "plan", "", "plan name (directory under Plans/)")
	sync.Flags().BoolVar(&syncJSON, "json", false, "emit JSON")
	_ = sync.MarkFlagRequired("plan")

	c.AddCommand(add, list, current, lookup, render, sync)
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
	_, index, err := decisions.LoadValidatedIndex(root)
	if err != nil {
		return "", nil, fmt.Errorf("decide: %w", err)
	}
	return root, index, nil
}

func decidePlanDir(root, plan, verb string) (string, error) {
	if plan == "" || plan == "." || plan == ".." || filepath.IsAbs(plan) ||
		strings.ContainsAny(plan, `/\`) || strings.ContainsRune(plan, ':') || filepath.Clean(plan) != plan {
		return "", fmt.Errorf("decide %s: plan %q must be a single directory name under Plans/", verb, plan)
	}
	plansDir := filepath.Join(root, "Plans")
	plansInfo, err := os.Lstat(plansDir)
	if err != nil || !plansInfo.IsDir() || plansInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("decide %s: Plans/ must be a real directory under the planning root", verb)
	}
	planDir := filepath.Join(plansDir, plan)
	info, err := os.Stat(planDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("decide %s: plan %q has no directory under Plans/", verb, plan)
	}
	realPlans, err := filepath.EvalSymlinks(plansDir)
	if err != nil {
		return "", fmt.Errorf("decide %s: resolving Plans/: %w", verb, err)
	}
	realPlan, err := filepath.EvalSymlinks(planDir)
	if err != nil {
		return "", fmt.Errorf("decide %s: resolving plan %q: %w", verb, plan, err)
	}
	rel, err := filepath.Rel(realPlans, realPlan)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("decide %s: plan %q resolves outside Plans/", verb, plan)
	}
	if linkInfo, err := os.Lstat(planDir); err != nil || linkInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("decide %s: plan %q must be a real directory, not a symlink under Plans/", verb, plan)
	}
	// Write through the resolved path, not the lexical one: the containment
	// check above holds for realPlan, and a symlink swapped in between the
	// check and the write would otherwise be followed.
	return realPlan, nil
}

func cmdDecideAdd(c *cobra.Command, plan, statement, supersedes, source string, asJSON bool) error {
	root, index, err := decisionIndex()
	if err != nil {
		return err
	}
	planDir, err := decidePlanDir(root, plan, "add")
	if err != nil {
		return err
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
	planDir, err := decidePlanDir(root, plan, "list")
	if err != nil {
		return err
	}
	path := decisions.PathFor(planDir)
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
	root, index, err := decisionIndex()
	if err != nil {
		return err
	}
	if plan != "" {
		if _, err := decidePlanDir(root, plan, "current"); err != nil {
			return err
		}
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
	root, index, err := decisionIndex()
	if err != nil {
		return err
	}
	if plan != "" {
		if _, err := decidePlanDir(root, plan, "lookup"); err != nil {
			return err
		}
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

// cmdDecideRender writes Plans/<P>/Design.md — the plan's closing design
// document, generated from the decisions file and the graph
// (Designs/PlanDecisions DD-7). Disposable and read-only: regenerating it
// for the same inputs is byte-identical, and a hand edit is overwritten.
func cmdDecideRender(c *cobra.Command, plan string, asJSON bool) error {
	root, index, err := decisionIndex()
	if err != nil {
		return err
	}
	planDir, err := decidePlanDir(root, plan, "render")
	if err != nil {
		return err
	}
	entries, err := decisions.Load(decisions.PathFor(planDir))
	if err != nil {
		return fmt.Errorf("decide render: %w", err)
	}
	// Which nodes cite which decision, from the committed graph if any.
	citedBy := map[string][]string{}
	if g, err := gstore.Load(gstore.PathFor(planDir)); err == nil {
		for _, n := range g.Nodes {
			for _, j := range n.Justifies {
				if _, id, ok := decisions.ParseRef(j); ok {
					citedBy[id] = append(citedBy[id], n.ID)
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"%s — Decisions\"\ntype: decisions-view\nplan: \"%s\"\n---\n\n", plan, plan)
	fmt.Fprintf(&b, "# %s — Decisions\n\n", plan)
	b.WriteString("Generated by `sdd decide render` from `" + filepath.Base(decisions.PathFor(planDir)) + "` and the plan graph. Do not edit; regenerate.\n\n")
	var current, superseded []decisions.Entry
	for _, e := range entries {
		if _, gone := index.SuccessorOf(e.ID); gone {
			superseded = append(superseded, e)
		} else {
			current = append(current, e)
		}
	}
	sortEntries(current)
	sortEntries(superseded)
	b.WriteString("## Current decisions\n\n")
	if len(current) == 0 {
		b.WriteString("None.\n\n")
	}
	for _, e := range current {
		fmt.Fprintf(&b, "### %s (%s)\n\n%s\n\n", e.ID, e.Date, e.Statement)
		if e.Source != "" {
			fmt.Fprintf(&b, "- Source: `%s`\n", e.Source)
		}
		if e.Supersedes != "" {
			fmt.Fprintf(&b, "- Supersedes: %s\n", e.Supersedes)
		}
		if nodes := citedBy[e.ID]; len(nodes) > 0 {
			sort.Strings(nodes)
			fmt.Fprintf(&b, "- Cited by: %s\n", strings.Join(nodes, ", "))
		}
		if e.Source != "" || e.Supersedes != "" || len(citedBy[e.ID]) > 0 {
			b.WriteString("\n")
		}
	}
	b.WriteString("## Superseded\n\n")
	if len(superseded) == 0 {
		b.WriteString("None.\n")
	}
	for _, e := range superseded {
		succ, _ := index.SuccessorOf(e.ID)
		fmt.Fprintf(&b, "- ~~%s~~ (%s) → %s:%s — %s\n", e.ID, e.Date, succ.Plan, succ.Entry.ID, e.Statement)
	}
	out := filepath.Join(planDir, "Design.md")
	if err := store.WriteAtomic(out, b.String()); err != nil {
		return fmt.Errorf("decide render: %w", err)
	}
	if asJSON {
		return writeJSON(struct {
			OK         bool   `json:"ok"`
			Path       string `json:"path"`
			Current    int    `json:"current"`
			Superseded int    `json:"superseded"`
		}{true, relPath(out), len(current), len(superseded)})
	}
	fmt.Fprintf(c.OutOrStdout(), "rendered %s (%d current, %d superseded)\n", relPath(out), len(current), len(superseded))
	return nil
}

// cmdDecideSync is the decisions half of `sdd compile` on its own: for a
// plan with no proposal to compile (a record-only plan, or a design that
// gained DDs after its plan was built), copy every related design's DD
// bullets into the plan's decisions file, verbatim, with declared
// supersession edges (PlanDecisions DD-4). Idempotent.
func cmdDecideSync(c *cobra.Command, plan string, asJSON bool) error {
	root, err := decisionsRoot()
	if err != nil {
		return err
	}
	if _, err := decidePlanDir(root, plan, "sync"); err != nil {
		return err
	}
	_, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return fmt.Errorf("decide sync: %w", err)
	}
	res, err := gcompile.SyncDesignDecisions(root, repoRoot, plan, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(res)
	}
	fmt.Fprintf(c.OutOrStdout(), "recorded %d design decision(s) in %s (%d already present)\n", len(res.Added), relPath(res.Path), len(res.Skipped))
	for _, e := range res.Added {
		fmt.Fprintf(c.OutOrStdout(), "  %s  %s\n", e.ID, e.Source)
	}
	return nil
}

func sortEntries(list []decisions.Entry) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Date != list[j].Date {
			return list[i].Date < list[j].Date
		}
		return list[i].ID < list[j].ID
	})
}
