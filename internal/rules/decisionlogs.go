package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/dlg"
)

// FocusedDecisionLogs ports Validator._focused_decision_logs: it runs the
// decision-ledger validator (internal/dlg) over the ledgers this planning root
// governs and folds its DLG* diagnostics into the SDD* stream.
//
// One ledger surface is validated per directory, not per candidate path: the
// canonical ledger and its archives are one logical ledger, and dlg.Validate
// discovers the siblings itself.
//
// Paths are rewritten planning-root-relative where possible, matching Python,
// so a diagnostic about an in-root ledger reads the same as any other. A
// ledger in an external repository keeps its absolute path, because there is
// no root-relative way to name it.
func FocusedDecisionLogs(r *Root, historical bool) []Diagnostic {
	var candidates []string

	internal := filepath.Join(r.Dir, "Decisions")
	canonical := filepath.Join(internal, "decisions.md")
	if isFile(canonical) {
		candidates = append(candidates, canonical)
	} else {
		candidates = append(candidates, firstArchive(internal)...)
	}

	repos := map[string]bool{r.RepoRoot: true}
	for _, p := range r.PlanRepos {
		repos[p] = true
	}
	var repoList []string
	for repo := range repos {
		repoList = append(repoList, repo)
	}
	sort.Strings(repoList)
	for _, repo := range repoList {
		canonical := filepath.Join(repo, "DECISIONS.md")
		if isFile(canonical) {
			candidates = append(candidates, canonical)
			continue
		}
		candidates = append(candidates, firstArchive(repo)...)
	}

	var out []Diagnostic
	seen := map[string]bool{}
	for _, path := range candidates {
		directory := absPath(filepath.Dir(path))
		if seen[directory] {
			continue
		}
		seen[directory] = true

		// History comparison is meaningless for an archival audit, which is
		// reconstructing a past state rather than checking the current one.
		history := !historical && hasGitRoot(path)
		for _, d := range dlg.Validate(path, history) {
			out = append(out, Diagnostic{
				Code:       d.Code,
				Severity:   Severity(d.Severity),
				Path:       relativeToRoot(r.Dir, d.Path),
				Line:       d.Line,
				Message:    d.Message,
				Correction: d.Correction,
			})
		}
	}
	return out
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// firstArchive returns the lexicographically first archive-*.md in dir, or
// nothing. Python takes `sorted(glob(...))[:1]`.
func firstArchive(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "archive-") && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return []string{filepath.Join(dir, names[0])}
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// relativeToRoot renders a ledger path planning-root-relative when it lives
// inside the root, and leaves it absolute otherwise.
func relativeToRoot(root, path string) string {
	rel, err := filepath.Rel(absPath(root), absPath(path))
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

// hasGitRoot reports whether a ledger's directory sits in a git repository,
// which is what Python's `git_root(path) is not None` guard tests.
func hasGitRoot(path string) bool {
	return detectedSCM(filepath.Dir(path)) == "git"
}
