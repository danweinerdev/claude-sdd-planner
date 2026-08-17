// Package dlg ports scripts/sdd_decision_validate.py, the decision-ledger
// validator.
//
// This is a second validator alongside internal/rules, not a family within it.
// The two answer different questions: internal/rules validates the planning
// root's artifacts against each other, while this validates decision ledgers
// as documents in their own right — including ledgers that live outside the
// planning root entirely, in the repositories they represent. sdd_validate.py
// surfaces its diagnostics through _focused_decision_logs, and the Go
// validator wires them in the same way.
//
// Diagnostic codes are DLG*, disjoint from SDD*, and the two validators never
// report the same finding.
package dlg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity mirrors the Python validator's levels. "operational" marks a
// condition that prevented validation rather than a defect in the content.
type Severity string

const (
	Error       Severity = "error"
	Operational Severity = "operational"
	Candidate   Severity = "candidate"
	// Warning is a real defect that does not invalidate the ledger — the
	// compiler model: an error stops the build, a warning is reported and
	// compilation continues. It exists for conditions that are genuinely
	// wrong but cannot threaten correctness, and — critically — for ones a
	// legacy ledger may be unable to repair at all, because append-only
	// history forbids the renumbering their "fix" would require.
	Warning Severity = "warning"
	// Waived is a finding a human explicitly excepted in the ledger's
	// frontmatter. Reported like any other, never invalidating. Distinct from
	// a dropped diagnostic so "nothing found" and "found and excused" can
	// never look the same.
	Waived Severity = "waived"
)

// Invalidating reports whether a severity makes the ledger invalid. Only
// errors and operational failures do; warnings, candidates, and waived
// findings are reported and moved past.
func (s Severity) Invalidating() bool {
	return s == Error || s == Operational
}

// Diagnostic is one finding, shaped to match the Python dataclass so the
// differential oracle can compare them field for field.
type Diagnostic struct {
	Severity   Severity `json:"severity"`
	Code       string   `json:"code"`
	Path       string   `json:"path"`
	Line       int      `json:"line"`
	Message    string   `json:"message"`
	Correction string   `json:"correction"`
}

// Ledger is one parsed decision-log file.
type Ledger struct {
	Path     string
	Source   string
	Meta     map[string]any
	Body     string
	BodyLine int
}

// Line returns the 1-indexed source line containing text, or 1. Python's
// Ledger.line does a substring search, so a diagnostic points at the first
// line mentioning a field rather than at a parsed node.
func (l *Ledger) Line(text string) int {
	for i, value := range strings.Split(l.Source, "\n") {
		if strings.Contains(value, text) {
			return i + 1
		}
	}
	return 1
}

var (
	decisionIDRe  = regexp.MustCompile(`^D-\d{4,9}$`)
	archiveNameRe = regexp.MustCompile(`^archive-\d{4}\.md$`)
	isoDateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

var (
	kinds         = map[string]bool{"decision": true, "definition": true, "answered-question": true, "assumption": true}
	statuses      = map[string]bool{"proposed": true, "accepted": true, "rejected": true, "superseded": true}
	deciders      = map[string]bool{"agent": true, "user": true, "user-approved": true}
	reversibility = map[string]bool{"one-way": true, "two-way": true}
)

// sortedKeys renders a set the way Python's ", ".join(sorted(SET)) does, for
// the corrections that list allowed values.
func sortedKeys(set map[string]bool) string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sortStrings(out)
	return strings.Join(out, ", ")
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

func diag(l *Ledger, code, message, correction string, line int, path string, severity Severity) Diagnostic {
	p := path
	if p == "" && l != nil {
		p = l.Path
	}
	if line <= 0 {
		line = 1
	}
	return Diagnostic{Severity: severity, Code: code, Path: p, Line: line, Message: message, Correction: correction}
}

// ParseLedger ports parse_ledger: read, check delimiters, decode frontmatter.
// A nil Ledger means parsing failed and the caller must not validate further —
// every failure here already reported why.
func ParseLedger(path string) (*Ledger, []Diagnostic) {
	var out []Diagnostic

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, []Diagnostic{diag(nil, "DLG001",
			"Cannot read ledger as UTF-8: "+err.Error(),
			"Store the ledger as readable UTF-8.", 1, path, Operational)}
	}
	if !utf8Valid(raw) {
		return nil, []Diagnostic{diag(nil, "DLG001",
			"Cannot read ledger as UTF-8: invalid UTF-8",
			"Store the ledger as readable UTF-8.", 1, path, Operational)}
	}
	source := string(raw)

	if strings.Contains(source, "\r\n") {
		out = append(out, diag(nil, "DLG002", "Ledger uses CRLF line endings.",
			"Normalize it to UTF-8 with LF endings.", 1, path, Error))
	}

	lines := splitKeepends(source)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		out = append(out, diag(nil, "DLG003", "Missing opening YAML frontmatter delimiter.",
			"Start the ledger with standalone `---`.", 1, path, Error))
		return nil, out
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		out = append(out, diag(nil, "DLG004", "Missing closing YAML frontmatter delimiter.",
			"Close frontmatter with standalone `---`.", 1, path, Error))
		return nil, out
	}

	payload := strings.Join(lines[1:end], "")
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(payload), &node); err != nil {
		out = append(out, diag(nil, "DLG005", "Invalid YAML frontmatter: "+err.Error(),
			"Correct the YAML syntax.", yamlErrorLine(err)+1, path, Error))
		return nil, out
	}
	if len(node.Content) == 0 {
		out = append(out, diag(nil, "DLG006", "Frontmatter is not a mapping.",
			"Use key/value YAML frontmatter.", 1, path, Error))
		return nil, out
	}
	// Python loads with UniqueKeyLoader, which raises on a duplicate key
	// rather than silently keeping the last. duplicateKey reproduces that as a
	// DLG005 with the same shape, since PyYAML surfaces it as a YAMLError.
	if key, dup := duplicateMappingKey(node.Content[0]); dup {
		out = append(out, diag(nil, "DLG005",
			"Invalid YAML frontmatter: while constructing a mapping\nduplicate key: "+key,
			"Correct the YAML syntax.", 1, path, Error))
		return nil, out
	}
	meta, ok := nodeToMap(node.Content[0])
	if !ok {
		out = append(out, diag(nil, "DLG006", "Frontmatter is not a mapping.",
			"Use key/value YAML frontmatter.", 1, path, Error))
		return nil, out
	}

	return &Ledger{
		Path:     path,
		Source:   source,
		Meta:     meta,
		Body:     strings.Join(lines[end+1:], ""),
		BodyLine: end + 2,
	}, out
}

// duplicateMappingKey reports the first repeated key in a mapping node.
func duplicateMappingKey(n *yaml.Node) (string, bool) {
	if n.Kind != yaml.MappingNode {
		return "", false
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if seen[k] {
			return k, true
		}
		seen[k] = true
	}
	return "", false
}

var yamlLineRe = regexp.MustCompile(`line (\d+):`)

func yamlErrorLine(err error) int {
	if m := yamlLineRe.FindStringSubmatch(err.Error()); m != nil {
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		return n
	}
	return 0
}

// isSymlink reports whether path is a symbolic link, for DLG019.
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// canonicalName reports whether a ledger filename is one of the two canonical
// forms: a repository-root DECISIONS.md, or Decisions/decisions.md.
func canonicalName(path string) bool {
	name := filepath.Base(path)
	if name == "DECISIONS.md" {
		return true
	}
	return name == "decisions.md" && filepath.Base(filepath.Dir(path)) == "Decisions"
}
