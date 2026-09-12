package hook

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
)

// maxStandingDecisions bounds the number of standing decisions injected. Entries
// retain deterministic ordering; truncation is reported by a notice.
const maxStandingDecisions = 30

// SessionStartContext returns the additionalContext for a session, or "" when
// there is nothing to inject.
func SessionStartContext(projectDir string) string {
	return sessionStartContext(projectDir, 16*1024)
}

const truncatedNotice = "> **Warning:** Standing decisions are truncated. Read the full set with `sdd decide current`.\n"

// sessionStartContext keeps the context budget injectable for focused tests.
func sessionStartContext(projectDir string, byteBudget int) string {
	root := findPlanningRoot(projectDir)
	files, err := decisions.LoadRoot(root)
	if err != nil {
		return ""
	}
	index := decisions.NewIndex(files)
	current := index.Current("")
	if len(current) == 0 {
		return ""
	}

	lines := make([]string, 0, len(current))
	for _, e := range current {
		lines = append(lines, "- "+e.ID+" ["+strings.Join(e.Plans, ", ")+"]: "+e.Statement)
	}

	header := "## Standing decisions\n" +
		"Accepted plan decisions — standing constraints on planning and implementation. " +
		"A new decision that contradicts one must stop for user reconciliation:\n"
	if len(lines) <= maxStandingDecisions && fitsBudget(byteBudget, header+strings.Join(lines, "\n")) {
		return header + strings.Join(lines, "\n")
	}
	return budgetedDecisionContext(header, lines, byteBudget, truncatedNotice)
}

func budgetedDecisionContext(header string, lines []string, byteBudget int, notice string) string {
	prefix := notice + header
	if byteBudget <= 0 || len(prefix) > byteBudget {
		return notice
	}
	var included []string
	for _, line := range lines {
		if len(included) >= maxStandingDecisions {
			break
		}
		candidate := prefix + strings.Join(append(included, line), "\n")
		if len(candidate) > byteBudget {
			break
		}
		included = append(included, line)
	}
	return prefix + strings.Join(included, "\n")
}

func fitsBudget(byteBudget int, context string) bool {
	return byteBudget > 0 && len(context) <= byteBudget
}

// findPlanningRoot resolves the planning root per shared/path-resolution.md:
// the nearest ancestor declaring planning-config.json's planningRoot, else
// the project directory itself.
func findPlanningRoot(projectDir string) string {
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	project, err := filepath.Abs(projectDir)
	if err != nil {
		return projectDir
	}
	dir := project
	for {
		cfg := filepath.Join(dir, "planning-config.json")
		if raw, err := os.ReadFile(cfg); err == nil {
			var parsed struct {
				PlanningRoot string `json:"planningRoot"`
			}
			if jsonUnmarshal(raw, &parsed) == nil {
				root := parsed.PlanningRoot
				if root == "" {
					root = "."
				}
				if !filepath.IsAbs(root) {
					root = filepath.Join(dir, root)
				}
				return root
			}
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return project
}
