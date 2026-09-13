// Command sdd, subcommand `next`, implements FR-25: report current state and
// the literal next command to run, for one plan or every plan under the
// resolved planning root.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// nextEntry is the most advanced actionable point in one plan.
type nextEntry struct {
	Plan    string `json:"plan"`
	Status  string `json:"status"`
	Phase   string `json:"phase,omitempty"`
	Task    string `json:"task,omitempty"`
	Needs   string `json:"needs"`
	Command string `json:"command,omitempty"`
}

func cmdNext(planPath string, jsonOut bool) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("next: %w", err)
	}
	root, err := store.FindPlanningRoot(wd)
	if err != nil {
		return fmt.Errorf("next: %w", err)
	}

	var planPaths []string
	if planPath != "" {
		p, err := resolvePlanReadme(planPath)
		if err != nil {
			return fmt.Errorf("next: %w", err)
		}
		planPaths = []string{p}
	} else {
		rel, err := store.List(root, "plan")
		if err != nil {
			return fmt.Errorf("next: %w", err)
		}
		sort.Strings(rel)
		for _, r := range rel {
			planPaths = append(planPaths, filepath.Join(root, r))
		}
	}

	var entries []nextEntry
	for _, p := range planPaths {
		e, err := nextForPlan(p)
		if err != nil {
			return fmt.Errorf("next: %w", err)
		}
		entries = append(entries, e)
	}

	if jsonOut {
		return writeJSON(struct {
			Plans []nextEntry `json:"plans"`
		}{entries})
	}
	for _, e := range entries {
		fmt.Printf("%s (%s)\n", e.Plan, e.Status)
		if e.Phase != "" {
			fmt.Printf("  phase: %s\n", e.Phase)
		}
		if e.Task != "" {
			fmt.Printf("  task:  %s\n", e.Task)
		}
		fmt.Printf("  needs: %s\n", e.Needs)
		if e.Command != "" {
			fmt.Printf("  command: %s\n", e.Command)
		}
		fmt.Println()
	}
	return nil
}

// resolvePlanReadme accepts either a plan directory or its README.md.
func resolvePlanReadme(p string) (string, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		p = filepath.Join(p, "README.md")
	}
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

func nextForPlan(readmePath string) (nextEntry, error) {
	art, err := store.Read(readmePath)
	if err != nil {
		return nextEntry{}, err
	}
	doc := artifact.Parse(art.Source)
	planDir := filepath.Dir(readmePath)
	title := strings.Trim(fmVal(doc, "title"), `"`)
	status := strings.Trim(fmVal(doc, "status"), `"`)
	planID := relPath(planDir)

	e := nextEntry{Plan: planID, Status: status}

	switch status {
	case "draft":
		e.Needs = fmt.Sprintf("review and approve the plan %q", title)
		e.Command = fmt.Sprintf("sdd validate --scope %s", relPath(planDir))
		return e, nil
	case "complete":
		e.Needs = "nothing to do"
		return e, nil
	}

	phases := loadPhases(doc)
	if len(phases) == 0 {
		e.Needs = "plan has no phases[]"
		return e, nil
	}

	if ip := lowestPhaseWithStatus(phases, "in-progress"); ip != nil {
		e.Phase = ip.Str("title")
		docPath := filepath.Join(planDir, ip.Str("doc"))
		phaseDoc, err := loadPhaseDoc(docPath)
		if err == nil {
			tasks := loadTasks(phaseDoc)
			if pt := lowestTaskWithStatus(tasks, "planned"); pt != nil {
				e.Task = pt.Str("id")
				e.Needs = fmt.Sprintf("task %s is next: %s", pt.Str("id"), pt.Str("title"))
				e.Command = fmt.Sprintf("sdd show %s --json", relPath(docPath))
				return e, nil
			}
			if allTasksComplete(tasks) {
				e.Needs = "phase needs its completion evidence and the phase-completion review gate"
				e.Command = fmt.Sprintf("sdd validate --scope %s", relPath(planDir))
				return e, nil
			}
		}
		e.Needs = "phase is in progress but has no planned task and is not all complete"
		e.Command = fmt.Sprintf("sdd validate --scope %s", relPath(planDir))
		return e, nil
	}

	if pp := lowestPhaseWithStatus(phases, "planned"); pp != nil {
		e.Phase = pp.Str("title")
		docPath := filepath.Join(planDir, pp.Str("doc"))
		e.Needs = fmt.Sprintf("start phase %s: %s", pp.Str("id"), pp.Str("title"))
		e.Command = fmt.Sprintf("sdd apply %s --dry-run", relPath(docPath))
		return e, nil
	}

	if allPhasesComplete(phases) {
		e.Needs = "all phases complete; plan completion evidence pending"
		e.Command = fmt.Sprintf("sdd validate --scope %s", relPath(planDir))
		return e, nil
	}

	e.Needs = "no planned or in-progress phase found (check blocked/deferred phases)"
	e.Command = fmt.Sprintf("sdd validate --scope %s", relPath(planDir))
	return e, nil
}

func loadPhaseDoc(path string) (*artifact.Doc, error) {
	art, err := store.Read(path)
	if err != nil {
		return nil, err
	}
	if !art.Exists {
		return nil, fmt.Errorf("%s does not exist", path)
	}
	return artifact.Parse(art.Source), nil
}

func loadPhases(doc *artifact.Doc) []fmItem {
	items := fmSequence(doc.FrontmatterRaw, "phases")
	sort.SliceStable(items, func(i, j int) bool {
		a, _ := strconv.Atoi(items[i].Str("id"))
		b, _ := strconv.Atoi(items[j].Str("id"))
		return a < b
	})
	return items
}

func loadTasks(doc *artifact.Doc) []fmItem {
	items := fmSequence(doc.FrontmatterRaw, "tasks")
	sort.SliceStable(items, func(i, j int) bool {
		return taskLess(items[i].Str("id"), items[j].Str("id"))
	})
	return items
}

var taskNumRe = regexp.MustCompile(`^(\d+)\.(\d+)([a-z]?)$`)

// taskLess orders task ids numerically (major.minor, then any letter suffix)
// rather than lexically, so "1.10" sorts after "1.2".
func taskLess(a, b string) bool {
	ma := taskNumRe.FindStringSubmatch(a)
	mb := taskNumRe.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return a < b
	}
	amaj, _ := strconv.Atoi(ma[1])
	amin, _ := strconv.Atoi(ma[2])
	bmaj, _ := strconv.Atoi(mb[1])
	bmin, _ := strconv.Atoi(mb[2])
	if amaj != bmaj {
		return amaj < bmaj
	}
	if amin != bmin {
		return amin < bmin
	}
	return ma[3] < mb[3]
}

func lowestPhaseWithStatus(phases []fmItem, status string) *fmItem {
	for i := range phases {
		if phases[i].Str("status") == status {
			return &phases[i]
		}
	}
	return nil
}

func lowestTaskWithStatus(tasks []fmItem, status string) *fmItem {
	for i := range tasks {
		if tasks[i].Str("status") == status {
			return &tasks[i]
		}
	}
	return nil
}

func allTasksComplete(tasks []fmItem) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if t.Str("status") != "complete" {
			return false
		}
	}
	return true
}

func allPhasesComplete(phases []fmItem) bool {
	if len(phases) == 0 {
		return false
	}
	for _, p := range phases {
		if p.Str("status") != "complete" {
			return false
		}
	}
	return true
}

// graphNextShow reprints the payload of every node currently claimed by `by`
// without claiming anything (P-06b): re-running --claim to re-read a payload
// otherwise claims a second node. Returns handled=false when the plan has no
// committed graph, so the caller falls through the same way graphNext does.
func graphNextShow(planPath, by string, jsonOut bool) (bool, error) {
	readme, err := resolvePlanReadme(planPath)
	if err != nil {
		return false, nil // let the caller report the resolution problem
	}
	planDir := filepath.Dir(readme)
	if _, err := os.Stat(gstore.PathFor(planDir)); err != nil {
		return false, nil
	}
	if by == "" {
		return true, fmt.Errorf("next: --show requires --by <who> to identify the holder")
	}
	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return true, fmt.Errorf("next: %w", err)
	}
	plan := filepath.Base(planDir)
	sources, err := gcompile.NewSources(root, repoRoot, plan)
	if err != nil {
		return true, fmt.Errorf("next: %w", err)
	}
	snap := sources.IntentSnapshot()
	inRes := sources.InputResolver()

	g, err := gstore.Load(gstore.PathFor(planDir))
	if err != nil {
		return true, fmt.Errorf("next: %w", err)
	}
	var held []*model.Node
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Claim != nil && n.Claim.By == by {
			held = append(held, n)
		}
	}
	if len(held) == 0 {
		if jsonOut {
			return true, writeJSON(struct {
				OK    bool   `json:"ok"`
				Plan  string `json:"plan"`
				By    string `json:"by"`
				Nodes []any  `json:"nodes"`
			}{true, plan, by, nil})
		}
		fmt.Printf("%s: %s holds no claim\n", plan, by)
		return true, nil
	}

	type citedText struct {
		ID   string `json:"id"`
		Text string `json:"text,omitempty"`
	}
	type inputText struct {
		Root     string   `json:"root"`
		Path     string   `json:"path"`
		Kind     string   `json:"kind"`
		Digest   string   `json:"digest"`
		Binary   bool     `json:"binary,omitempty"`
		Headings []string `json:"headings,omitempty"`
		Text     string   `json:"text,omitempty"`
	}
	type nodePayload struct {
		Node         model.Node  `json:"node"`
		Cited        []citedText `json:"cited"`
		Inputs       []inputText `json:"inputs"`
		LeaseExpires string      `json:"lease_expires"`
		By           string      `json:"by"`
		Workspace    string      `json:"workspace,omitempty"`
	}

	var payloads []nodePayload
	for _, node := range held {
		var cited []citedText
		for _, id := range node.Justifies {
			cited = append(cited, citedText{ID: id, Text: snap.Items[id].Normalized})
		}
		var inputs []inputText
		for _, spec := range node.Inputs {
			it := inputText{Root: spec.Root, Path: spec.Path, Kind: string(inputsKind(spec))}
			if resolved, err := inRes.Resolve(spec); err == nil {
				it.Kind = string(resolved.Kind)
				it.Digest = resolved.Digest
				it.Binary = resolved.Binary
				it.Headings = resolved.Headings
				it.Text = resolved.Text
			}
			inputs = append(inputs, it)
		}
		workspace := ""
		leaseExpires := ""
		if node.Claim != nil {
			workspace = node.Claim.Workspace
			leaseExpires = node.Claim.LeaseExpires
		}
		payloads = append(payloads, nodePayload{
			Node: *node, Cited: cited, Inputs: inputs,
			LeaseExpires: leaseExpires, By: by, Workspace: workspace,
		})
	}

	if jsonOut {
		return true, writeJSON(struct {
			OK    bool          `json:"ok"`
			Plan  string        `json:"plan"`
			By    string        `json:"by"`
			Nodes []nodePayload `json:"nodes"`
		}{true, plan, by, payloads})
	}
	for _, p := range payloads {
		fmt.Printf("holds %s (by %s, lease expires %s)\n\n", p.Node.ID, p.By, p.LeaseExpires)
		fmt.Printf("contract: %s\n", p.Node.Contract)
		for _, c := range p.Cited {
			if c.Text != "" {
				fmt.Printf("justifies %s: %s\n", c.ID, c.Text)
			} else {
				fmt.Printf("justifies %s\n", c.ID)
			}
		}
		for _, in := range p.Inputs {
			label := in.Root + ":" + in.Path
			if len(in.Headings) > 0 {
				label += "#" + strings.Join(in.Headings, " / ")
			}
			if in.Text != "" {
				fmt.Printf("input %s (%s):\n%s\n", label, in.Kind, in.Text)
			} else {
				fmt.Printf("input %s (%s, digest %s, binary=%t)\n", label, in.Kind, in.Digest, in.Binary)
			}
		}
		fmt.Printf("gate: %s\nhazards: %s\n", describeGateBrief(p.Node.Gate), describeHazardsBrief(p.Node.Hazards))
		if len(p.Node.Artifacts) > 0 {
			fmt.Printf("artifacts: %s\n", strings.Join(p.Node.Artifacts, ", "))
		}
		if p.Node.History != "" {
			fmt.Printf("history: %s\n", p.Node.History)
		}
		if p.Workspace != "" {
			fmt.Printf("workspace: %s\n", p.Workspace)
		}
		fmt.Println()
	}
	return true, nil
}
