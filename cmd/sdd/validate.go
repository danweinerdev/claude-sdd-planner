// Command sdd, subcommand `validate`, is a drop-in native replacement for
// scripts/sdd_validate.py, built on the ported rule registry in
// internal/rules. Every diagnostic code, message, correction, severity, and
// reported line is expected to match the Python validator's output exactly
// for the rule families that have been ported (see internal/rules for the
// registry and tools/parity/parity.py for the differential oracle); codes
// from families not yet ported simply do not appear.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// outDiagnostic's field order matches sdd_validate.py's `json.dumps(...,
// sort_keys=True)` output: alphabetical by key. Go's encoding/json preserves
// struct declaration order rather than sorting, so declaring the fields in
// that order reproduces the same key order without a second sorting pass.
type outDiagnostic struct {
	Code       string   `json:"code"`
	Correction string   `json:"correction"`
	Implicated []string `json:"implicated"`
	Line       int      `json:"line"`
	Message    string   `json:"message"`
	Path       string   `json:"path"`
	Severity   string   `json:"severity"`
}

// outDoc mirrors main()'s successful-run JSON dict, field order alphabetical
// for the same reason as outDiagnostic.
type outDoc struct {
	ArtifactsInScope   []string        `json:"artifacts_in_scope"`
	ArtifactsInspected int             `json:"artifacts_inspected"`
	Diagnostics        []outDiagnostic `json:"diagnostics"`
	PlanningRoot       string          `json:"planning_root"`
	Valid              bool            `json:"valid"`
}

func cmdValidate(args []string) error {
	fs2 := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs2.SetOutput(os.Stderr)
	root := fs2.String("root", "", "planning root (default: resolved from planning-config.json)")
	scope := fs2.String("scope", "", "limit findings to an artifact/path and paths it directly relates to")
	format := fs2.String("format", "text", "output format: text|json")

	flags, positional := splitArgs(args, map[string]bool{
		"-root": true, "--root": true,
		"-scope": true, "--scope": true,
		"-format": true, "--format": true,
	})
	if err := fs2.Parse(flags); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if len(positional) > 0 {
		return fmt.Errorf("validate: unexpected extra argument %q", positional[0])
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("validate: --format must be text or json")
	}

	resolved := *root
	if resolved == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("validate: %w", err)
		}
		resolved, err = store.FindPlanningRoot(wd)
		if err != nil {
			return fmt.Errorf("validate: %w", err)
		}
	}
	resolved, err := filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
		return fmt.Errorf("validate: planning root %q is not a directory", resolved)
	}

	r, err := rules.LoadRoot(resolved)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if len(r.Artifacts) == 0 {
		return fmt.Errorf("validate: planning root contains no discoverable SDD artifacts")
	}

	diags := rules.Run(r)

	artifactsInScope := make([]string, 0, len(r.Artifacts))
	for _, a := range r.Artifacts {
		artifactsInScope = append(artifactsInScope, a.Rel)
	}
	if *scope != "" {
		artifactsInScope = filterInScope(artifactsInScope, *scope)
		diags = selectInScope(diags, *scope, artifactsInScope)
	}
	sort.Strings(artifactsInScope)

	valid := true
	for _, d := range diags {
		if d.Severity == rules.Error {
			valid = false
			break
		}
	}

	out := make([]outDiagnostic, len(diags))
	for i, d := range diags {
		implicated := d.Implicated
		if implicated == nil {
			implicated = []string{}
		}
		out[i] = outDiagnostic{
			Code: d.Code, Correction: d.Correction, Implicated: implicated,
			Line: d.Line, Message: d.Message, Path: d.Path, Severity: string(d.Severity),
		}
	}

	if *format == "json" {
		doc := outDoc{
			ArtifactsInScope:   artifactsInScope,
			ArtifactsInspected: len(r.Artifacts),
			Diagnostics:        out,
			PlanningRoot:       resolved,
			Valid:              valid,
		}
		if err := printJSON(doc); err != nil {
			return err
		}
	} else {
		printValidateReport(resolved, *scope, len(r.Artifacts), len(artifactsInScope), out, valid)
	}

	if !valid {
		return &refusedError{n: countErrorsOut(out)}
	}
	return nil
}

// printJSON writes v as indented JSON without HTML-escaping, matching
// Python's json.dumps(..., indent=2) byte for byte (Go's encoding/json
// escapes <, >, and & by default; Python's json module never does).
func printJSON(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	fmt.Print(buf.String())
	return nil
}

func countErrorsOut(ds []outDiagnostic) int {
	n := 0
	for _, d := range ds {
		if d.Severity == "error" {
			n++
		}
	}
	return n
}

// printValidateReport mirrors main()'s text-format branch exactly: a one-line
// summary, then one two-line block per diagnostic, then (only when there were
// none) a closing "Checked ..." line.
func printValidateReport(root, scope string, inspected, inScope int, diags []outDiagnostic, valid bool) {
	status := "Valid"
	if !valid {
		status = "Invalid"
	}
	scopeSummary := ""
	if scope != "" {
		scopeSummary = fmt.Sprintf(", %d in scope", inScope)
	}
	fmt.Printf("%s: %s (%d artifacts inspected%s)\n", status, root, inspected, scopeSummary)
	for _, d := range diags {
		fmt.Printf("%s %s %s:%d: %s\n", strings.ToUpper(d.Severity), d.Code, d.Path, d.Line, d.Message)
		fmt.Printf("  Required correction: %s\n", d.Correction)
	}
	if len(diags) == 0 {
		fmt.Println("Checked structure, frontmatter, paths, identifiers, hierarchy, dependencies, reviews, decisions, and completion-evidence shape.")
	}
}

// filterInScope keeps every artifact path under the scope root. This is a
// deliberately simplified reading of --scope: sdd_validate.py's resolve_scope
// additionally walks the transitive `related` graph from the scoped artifact,
// which tools/parity/parity.py's oracle never exercises (it always runs
// without --scope) and which is not part of this port's assigned rule
// families.
func filterInScope(paths []string, scope string) []string {
	scope = strings.Trim(filepath.ToSlash(scope), "/")
	var out []string
	for _, p := range paths {
		if p == scope || strings.HasPrefix(p, scope+"/") {
			out = append(out, p)
		}
	}
	return out
}

// splitIdent splits an id like "D-0007" into its namespace and number. It is
// shared with cmd/sdd/decide.go, which is unrelated to validation but reused
// this small helper rather than duplicating it.
// fmVal returns a frontmatter value or "" when the key is absent. Shared with
// cmd/sdd/next.go, which needs the same small helper.
func fmVal(doc *artifact.Doc, key string) string {
	v, _ := doc.FM(key)
	return v
}

func splitIdent(id string) (string, int, bool) {
	ns, rest, ok := strings.Cut(id, "-")
	if !ok {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return "", 0, false
	}
	return ns, n, true
}

func selectInScope(diags []rules.Diagnostic, scope string, inScope []string) []rules.Diagnostic {
	scope = strings.Trim(filepath.ToSlash(scope), "/")
	allowed := map[string]bool{}
	for _, p := range inScope {
		allowed[p] = true
	}
	var out []rules.Diagnostic
	for _, d := range diags {
		if strings.HasPrefix(d.Path, "Decisions/") || allowed[d.Path] ||
			d.Path == scope || strings.HasPrefix(d.Path, scope+"/") {
			out = append(out, d)
			continue
		}
		for _, imp := range d.Implicated {
			if allowed[imp] {
				out = append(out, d)
				break
			}
		}
	}
	return out
}
