// Package rules holds the deterministic validation rules, one registered Rule
// per diagnostic code, ported from scripts/sdd_validate.py.
//
// Two properties are structural rather than incidental:
//
// Every rule carries its own Good and Bad examples. A rule with no failing
// example has never been shown to fire, and a rule with no passing example has
// never been shown to stay quiet — either way it is untested regardless of what
// a coverage percentage says. The registry meta-test in rules_test.go fails when
// a registered rule omits either, so examples cannot lag the port.
//
// Codes, messages, and corrections are reproduced from the Python validator
// verbatim. This is a drop-in replacement, not a reimplementation with better
// wording: existing consumers parse this output, and the differential oracle in
// tools/parity compares message text, so a "clearer" message is a parity
// failure.
package rules

import (
	"fmt"
	"sort"
	"strings"
)

// Severity mirrors the Python validator's two reporting levels. `candidate` is
// advisory and does not make a root invalid; `error` does.
type Severity string

const (
	Error     Severity = "error"
	Candidate Severity = "candidate"
)

// Diagnostic is one finding. Field names and JSON keys match the Python
// validator's output shape so consumers need no change.
type Diagnostic struct {
	Code       string   `json:"code"`
	Severity   Severity `json:"severity"`
	Path       string   `json:"path"`
	Line       int      `json:"line"`
	Message    string   `json:"message"`
	Correction string   `json:"correction"`
	Implicated []string `json:"implicated,omitempty"`
}

// Example is a fixture proving a rule's behavior. Files maps
// planning-root-relative paths to content, so an example can span the several
// artifacts a graph or citation rule needs.
type Example struct {
	Name  string
	Files map[string]string
	// Line, when nonzero, asserts the diagnostic's reported line. Rules whose
	// line is intrinsically 1 (whole-artifact findings) leave it zero.
	Line int
	// Setup, when nonempty, is a sequence of argv commands run in the fixture
	// root (via exec.Command — never a shell) after Files is written, before
	// LoadRoot. The evidence rules verify real repository state (a commit
	// exists, is an ancestor of HEAD, is tracked at HEAD, is not a merge...),
	// so their examples need an actual git repository: `{"git", "init", "-q"}`,
	// `{"git", "add", "."}`, `{"git", "commit", ...}`. rules_test.go's
	// runExample runs these with a fixed author/committer identity and
	// timestamp, so a commit's resulting SHA is reproducible and can be
	// hardcoded into an example's Files content.
	Setup [][]string
}

// Rule is one diagnostic code's implementation and its evidence.
type Rule struct {
	Code     string
	Severity Severity
	// What the rule checks, in one line, for `sdd validate --explain`.
	What string
	// PyFunc names the enclosing function in sdd_validate.py this was ported
	// from, so a parity failure can be traced back to its source.
	PyFunc string
	// Good examples must produce NO diagnostic of this code.
	Good []Example
	// Bad examples must each produce at least one diagnostic of this code.
	Bad []Example
	// UnexampledReason, when nonempty, exempts a rule from carrying examples
	// and records WHY in the registry itself. It exists for exactly one shape
	// of rule: one whose failing condition the example harness cannot
	// construct, because the harness always materializes a real directory to
	// hold Files. A rule using this must be covered by a direct unit test
	// instead, named in the reason.
	//
	// This is deliberately narrow. Anything that can be expressed as files —
	// including anything needing a git repository, which SETUP scripts now
	// cover — must carry examples like every other rule.
	UnexampledReason string
	// Check appends diagnostics for a single artifact. Rules that need the whole
	// root (graphs, citations across documents) use CheckRoot instead.
	Check func(a *Artifact, emit func(Diagnostic))
	// CheckRoot runs once over every artifact.
	CheckRoot func(r *Root, emit func(Diagnostic))
}

var registry = map[string]*Rule{}

// Register adds a rule. Duplicate codes panic at init: two implementations of
// one code would make the parity comparison nondeterministic.
func Register(r *Rule) {
	if _, dup := registry[r.Code]; dup {
		panic(fmt.Sprintf("rules: duplicate registration for %s", r.Code))
	}
	if r.Check == nil && r.CheckRoot == nil {
		panic(fmt.Sprintf("rules: %s registers no check", r.Code))
	}
	registry[r.Code] = r
}

// All returns every registered rule, ordered by code.
func All() []*Rule {
	out := make([]*Rule, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Get returns one rule by code, or nil.
func Get(code string) *Rule { return registry[code] }

// Codes lists every implemented diagnostic code.
func Codes() []string {
	out := make([]string, 0, len(registry))
	for c := range registry {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Run evaluates every rule over a root and returns diagnostics in the Python
// validator's deterministic order: path, then line, then code.
func Run(r *Root) []Diagnostic {
	var out []Diagnostic
	emit := func(d Diagnostic) { out = append(out, d) }
	for _, rule := range All() {
		if rule.CheckRoot != nil {
			rule.CheckRoot(r, emit)
		}
	}
	for _, a := range r.Artifacts {
		for _, rule := range All() {
			if rule.Check != nil {
				rule.Check(a, emit)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// Explain renders the implemented-rule table, so `sdd validate --explain` can
// show what the tool checks and what it does not.
func Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d rules implemented\n\n", len(registry))
	for _, r := range All() {
		fmt.Fprintf(&b, "  %-8s %-10s %s\n", r.Code, r.Severity, r.What)
	}
	return b.String()
}
