package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// Lifecycle transition verbs (FR-21): `sdd task complete`, `sdd phase
// complete`, `sdd plan complete`.
//
// Each enforces the gate its schema declares by running the same rule
// implementations `sdd validate` uses. That reuse is the requirement, not an
// optimization: a second copy of "what makes a phase completable" would drift
// from the validator, and the gate that matters is the one the validator
// enforces.
//
// The check is performed by making the transition in memory and validating the
// result. If completing the entity would produce a diagnostic, the transition
// is refused and the diagnostic is the refusal — so the reason a gate is unmet
// is always a real finding with an artifact path and line, exactly as FR-21
// requires.

const transitionUsage = `sdd task complete <phase-path> --id ID
sdd phase complete <phase-path>
sdd plan complete <plan-path>

Each verb refuses unless the gate its schema declares is met, evaluated by the
same rules sdd validate runs. Pass --dry-run to see the verdict without
writing.`

func cmdTransition(kind string, args []string) error {
	if len(args) == 0 || args[0] != "complete" {
		return fmt.Errorf("%s: expected `complete`\n\n%s", kind, transitionUsage)
	}
	fs := flag.NewFlagSet(kind+" complete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	id := fs.String("id", "", "task id (task complete only)")
	dryRun := fs.Bool("dry-run", false, "report the verdict without writing")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%s complete: expected exactly one artifact path\n\n%s", kind, transitionUsage)
	}
	if kind == "task" && *id == "" {
		return fmt.Errorf("task complete: --id is required")
	}

	path := fs.Arg(0)
	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}
	if !art.Exists {
		return fmt.Errorf("%s complete: %s does not exist", kind, path)
	}

	today := time.Now().Format("2006-01-02")
	updated, err := applyTransition(art.Source, kind, *id, today)
	if err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}

	// Validate the would-be result, not the current state: the question is
	// whether completing is permitted, which only the completed form answers.
	blocking, err := gateDiagnostics(path, updated)
	if err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}
	if len(blocking) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%s complete: refused — the completion gate is not met:\n", kind)
		for _, d := range blocking {
			fmt.Fprintf(&b, "  %s %s:%d: %s\n", d.Code, d.Path, d.Line, d.Message)
			fmt.Fprintf(&b, "      fix: %s\n", d.Correction)
		}
		return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}

	if *dryRun {
		fmt.Printf("%s complete: gate met; would mark %s complete\n", kind, describeTarget(kind, *id, path))
		return nil
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}
	fmt.Printf("marked %s complete in %s\n", describeTarget(kind, *id, path), path)
	return nil
}

func describeTarget(kind, id, path string) string {
	if kind == "task" {
		return "task " + id
	}
	return kind
}

// applyTransition sets the target's status to `complete` in a copy of source.
func applyTransition(source, kind, id, today string) (string, error) {
	lines := strings.Split(source, "\n")
	switch kind {
	case "plan", "phase":
		if !setTopLevelStatus(lines, "complete") {
			return "", fmt.Errorf("no top-level `status:` field to advance")
		}
	case "task":
		if !setEntryStatus(lines, id, "complete") {
			return "", fmt.Errorf("no task `%s` in this phase's tasks[]", id)
		}
	}
	return restampUpdated(strings.Join(lines, "\n"), today), nil
}

// setTopLevelStatus rewrites the frontmatter's own `status:` line.
func setTopLevelStatus(lines []string, value string) bool {
	for i, l := range lines {
		if i > 0 && strings.TrimSpace(l) == "---" {
			return false
		}
		if strings.HasPrefix(l, "status:") {
			lines[i] = "status: " + value
			return true
		}
	}
	return false
}

// setEntryStatus rewrites one tasks[] entry's status, located by its id.
func setEntryStatus(lines []string, id, value string) bool {
	inEntry := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "- id:") {
			entryID := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- id:")), `"'`)
			inEntry = entryID == id
			continue
		}
		if inEntry && strings.HasPrefix(trimmed, "status:") {
			indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
			lines[i] = indent + "status: " + value
			return true
		}
	}
	return false
}

// gateDiagnostics validates a candidate document in place and returns the
// diagnostics that the transition itself would introduce.
//
// Pre-existing findings are excluded deliberately: refusing a completion
// because of an unrelated defect elsewhere would make the verb unusable in a
// root that is not already perfect, and those findings are `sdd validate`'s to
// report.
func gateDiagnostics(path, candidate string) ([]rules.Diagnostic, error) {
	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return nil, err
	}
	before, err := rules.LoadRootRepo(root, repoRoot)
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	for _, d := range rules.Run(before) {
		existing[diagKey(d)] = true
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := store.WriteAtomic(path, candidate); err != nil {
		return nil, err
	}
	defer store.WriteAtomic(path, string(original))

	after, err := rules.LoadRootRepo(root, repoRoot)
	if err != nil {
		return nil, err
	}
	var introduced []rules.Diagnostic
	for _, d := range rules.Run(after) {
		if !existing[diagKey(d)] {
			introduced = append(introduced, d)
		}
	}
	return introduced, nil
}

func diagKey(d rules.Diagnostic) string {
	return d.Code + "\x00" + d.Path + "\x00" + fmt.Sprint(d.Line) + "\x00" + d.Message
}

var _ = artifact.Parse
