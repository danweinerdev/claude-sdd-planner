// Command sdd, subcommand `next`, implements FR-25: report current state and
// the literal next command to run, for one plan or every plan under the
// resolved planning root.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/internal/ymlite"
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

func cmdNext(args []string) error {
	fs2 := flag.NewFlagSet("next", flag.ContinueOnError)
	fs2.SetOutput(os.Stderr)
	jsonOut := fs2.Bool("json", false, "emit JSON")

	flags, positional := splitArgs(args, map[string]bool{})
	if err := fs2.Parse(flags); err != nil {
		return fmt.Errorf("next: %w", err)
	}
	if len(positional) > 1 {
		return fmt.Errorf("next: unexpected extra argument %q", positional[1])
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("next: %w", err)
	}
	root, err := store.FindPlanningRoot(wd)
	if err != nil {
		return fmt.Errorf("next: %w", err)
	}

	var planPaths []string
	if len(positional) == 1 {
		p, err := resolvePlanReadme(positional[0])
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

	if *jsonOut {
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

func loadPhases(doc *artifact.Doc) []ymlite.Item {
	start, end, found := ymlite.Block(doc.FrontmatterRaw, "phases")
	if !found {
		return nil
	}
	items := ymlite.Items(doc.FrontmatterRaw[start:end])
	sort.SliceStable(items, func(i, j int) bool {
		a, _ := strconv.Atoi(items[i].Str("id"))
		b, _ := strconv.Atoi(items[j].Str("id"))
		return a < b
	})
	return items
}

func loadTasks(doc *artifact.Doc) []ymlite.Item {
	start, end, found := ymlite.Block(doc.FrontmatterRaw, "tasks")
	if !found {
		return nil
	}
	items := ymlite.Items(doc.FrontmatterRaw[start:end])
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

func lowestPhaseWithStatus(phases []ymlite.Item, status string) *ymlite.Item {
	for i := range phases {
		if phases[i].Str("status") == status {
			return &phases[i]
		}
	}
	return nil
}

func lowestTaskWithStatus(tasks []ymlite.Item, status string) *ymlite.Item {
	for i := range tasks {
		if tasks[i].Str("status") == status {
			return &tasks[i]
		}
	}
	return nil
}

func allTasksComplete(tasks []ymlite.Item) bool {
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

func allPhasesComplete(phases []ymlite.Item) bool {
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
