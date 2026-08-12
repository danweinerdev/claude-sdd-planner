package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
)

// maxLedgerEntries bounds the injected context. A ledger grows without limit
// and every entry costs session context, so the newest are carried and the
// remainder summarized.
const maxLedgerEntries = 30

// SessionStartContext returns the additionalContext for a session, or "" when
// there is nothing to inject.
//
// It replaces hooks/load-decisions.sh, which shelled to python3 and parsed the
// ledger with regexes because PyYAML is not stdlib. Reading it through the
// artifact parser instead means the hook sees the same frontmatter model the
// validator does, so an entry the tool considers valid is the entry the
// session is told about.
//
// Every failure is silent: a hook that errors would break session start, and
// missing context is recoverable in a way a broken session is not.
func SessionStartContext(projectDir string) string {
	ledger := findLedger(projectDir)
	if ledger == "" {
		return ""
	}
	raw, err := os.ReadFile(ledger)
	if err != nil {
		return ""
	}
	doc := artifact.Parse(string(raw))
	if doc == nil {
		return ""
	}

	entries := acceptedEntries(doc.FrontmatterRaw)
	if len(entries) == 0 {
		return ""
	}

	var lines []string
	for i, e := range entries {
		if i >= maxLedgerEntries {
			lines = append(lines, "- ... "+strconv.Itoa(len(entries)-maxLedgerEntries)+
				" more accepted entries in the ledger")
			break
		}
		lines = append(lines, "- "+e.id+": "+e.statement)
	}

	return "## Decision Ledger (" + ledger + ")\n" +
		"Accepted decisions — standing constraints on planning and implementation. " +
		"A new decision that contradicts one must stop for user reconciliation " +
		"(see shared/decision-log.md in the sdd-planner plugin):\n" +
		strings.Join(lines, "\n")
}

type ledgerEntry struct{ id, statement string }

// acceptedEntries pulls accepted decisions out of a ledger's frontmatter.
func acceptedEntries(frontmatter []string) []ledgerEntry {
	var out []ledgerEntry
	var node struct {
		Decisions []struct {
			ID        string `yaml:"id"`
			Status    string `yaml:"status"`
			Statement string `yaml:"statement"`
		} `yaml:"decisions"`
	}
	if err := yamlUnmarshal(strings.Join(frontmatter, "\n"), &node); err != nil {
		return nil
	}
	for _, d := range node.Decisions {
		if d.Status == "accepted" && d.Statement != "" {
			out = append(out, ledgerEntry{id: d.ID, statement: d.Statement})
		}
	}
	return out
}

// findLedger resolves the ledger per shared/decision-log.md § Ledger location:
// the planning root's Decisions/decisions.md when a planning-config.json is
// found above the project, else a repo-local ledger.
func findLedger(projectDir string) string {
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	project, err := filepath.Abs(projectDir)
	if err != nil {
		return ""
	}

	var candidates []string
	dir := project
	for {
		cfg := filepath.Join(dir, "planning-config.json")
		if raw, err := os.ReadFile(cfg); err == nil {
			var parsed struct {
				PlanningRoot string `json:"planningRoot"`
			}
			if json.Unmarshal(raw, &parsed) == nil {
				root := parsed.PlanningRoot
				if root == "" {
					root = "."
				}
				if !filepath.IsAbs(root) {
					root = filepath.Join(dir, root)
				}
				candidates = append(candidates, filepath.Join(root, "Decisions", "decisions.md"))
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// External-planning-root and no-config cases keep the ledger with its repo.
	candidates = append(candidates,
		filepath.Join(project, "DECISIONS.md"),
		filepath.Join(project, "Decisions", "decisions.md"))

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.Mode().IsRegular() {
			return c
		}
	}
	return ""
}
