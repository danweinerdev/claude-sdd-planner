package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

// maxLedgerEntries bounds the number of effective entries injected. Entries
// retain deterministic ordering; truncation is reported by a notice.
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
// The hook remains nonfatal. Legacy repositories with no ledger are silent,
// while a repository that explicitly selected fork authority receives a
// warning when that authority cannot be resolved safely.
func SessionStartContext(projectDir string) string {
	return sessionStartContext(projectDir, 16*1024)
}

const forkCriticalNotice = "> **Warning:** Configured decision authority is unavailable or truncated. Do not infer standing decisions; read the full resolved view with `sdd decide effective`."
const legacyCriticalNotice = "> **Warning:** Legacy decision context is truncated. Read the ledger with `sdd decide list` or search it with `sdd decide search`."

// sessionStartContext keeps the context budget injectable for focused tests.
// Warnings and full-read guidance consume the budget before decision text.
func sessionStartContext(projectDir string, byteBudget int) string {
	repository := findRepositoryRoot(projectDir)
	capture := decisionview.CaptureForRepository(repository)
	if capture.Declared {
		return forkSessionStartContext(capture, byteBudget)
	}
	return legacySessionStartContext(projectDir, byteBudget)
}

func forkSessionStartContext(capture *decisionview.ConsumerCapture, byteBudget int) string {
	blocking := capture.View == nil || capture.View.Resolution != decisionview.ResolutionComplete
	for _, diagnostic := range capture.Diagnostics {
		if diagnostic.Severity == decisionview.Error || diagnostic.Severity == decisionview.Operational {
			blocking = true
		}
	}
	if blocking {
		var warnings []string
		for _, diagnostic := range capture.Diagnostics {
			location := diagnostic.Path
			if diagnostic.Line > 0 {
				location += ":" + strconv.Itoa(diagnostic.Line)
			}
			if location != "" {
				location = " (" + location + ")"
			}
			warnings = append(warnings, "- "+diagnostic.Code+" ["+string(diagnostic.Severity)+"]"+location+": "+diagnostic.Message)
		}
		if len(warnings) == 0 {
			warnings = append(warnings, "- The configured decision view is not complete.")
		}
		context := "> **Warning:** Configured decision authority is not safe to use. Do not treat any partial or historical records as standing instructions.\n" +
			"Read the full resolved view and reconciliation guidance with `sdd decide effective`.\n" + strings.Join(warnings, "\n")
		if byteBudget <= 0 || len(context) > byteBudget {
			return forkCriticalNotice
		}
		return context
	}

	type effectiveEntry struct {
		id, source, statement string
	}
	var entries []effectiveEntry
	for _, record := range capture.View.Records {
		if record.Applicability != "binding" {
			continue
		}
		statement, _ := record.Original["statement"].(string)
		if statement == "" {
			continue
		}
		entries = append(entries, effectiveEntry{
			id:        record.ID.String(),
			source:    string(record.Source.Root) + ":" + record.Source.Path,
			statement: statement,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, "- "+entry.id+" [source "+entry.source+"]: "+entry.statement)
	}
	header := "## Effective Decision Authority\n" +
		"Standing constraints on planning and implementation. A conflicting new decision requires user reconciliation:\n"
	if len(capture.Diagnostics) > 0 {
		var diagnostics []string
		for _, diagnostic := range capture.Diagnostics {
			diagnostics = append(diagnostics, "- "+diagnostic.Code+" ["+string(diagnostic.Severity)+"]: "+diagnostic.Message)
		}
		header = "> **Warning:** Decision authority has diagnostics. Read the full resolved view with `sdd decide effective`.\n" +
			strings.Join(diagnostics, "\n") + "\n" + header
	}
	if len(entries) == 0 {
		if len(capture.Diagnostics) == 0 {
			return ""
		}
		context := header + "No binding decisions are available from the resolved view."
		if !fitsBudget(byteBudget, context) {
			return forkCriticalNotice
		}
		return context
	}
	if len(lines) <= maxLedgerEntries && fitsBudget(byteBudget, header+strings.Join(lines, "\n")) {
		return header + strings.Join(lines, "\n")
	}
	return budgetedDecisionContext(header, lines, byteBudget,
		"> **Warning:** Decision context is truncated. Read the full resolved view with `sdd decide effective`.\n",
		forkCriticalNotice)
}

func legacySessionStartContext(projectDir string, byteBudget int) string {
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
			break
		}
		lines = append(lines, "- "+e.id+": "+e.statement)
	}

	header := "## Decision Ledger (" + ledger + ")\n" +
		"Accepted decisions — standing constraints on planning and implementation. " +
		"A new decision that contradicts one must stop for user reconciliation " +
		"(see shared/decision-log.md in the sdd-planner plugin):\n"
	if len(entries) <= maxLedgerEntries && fitsBudget(byteBudget, header+strings.Join(lines, "\n")) {
		return header + strings.Join(lines, "\n")
	}
	return budgetedDecisionContext(header, lines, byteBudget,
		"> **Warning:** Legacy decision context is truncated. Read the ledger with `sdd decide list` or search it with `sdd decide search`.\n",
		legacyCriticalNotice)
}

func budgetedDecisionContext(header string, lines []string, byteBudget int, notice, criticalNotice string) string {
	prefix := notice + header
	if byteBudget <= 0 || len(prefix) > byteBudget {
		return criticalNotice
	}
	var included []string
	for _, line := range lines {
		if len(included) >= maxLedgerEntries {
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

func findRepositoryRoot(projectDir string) string {
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	project, err := filepath.Abs(projectDir)
	if err != nil {
		return projectDir
	}
	for dir := project; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "planning-config.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return project
		}
	}
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
