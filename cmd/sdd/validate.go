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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/digest"
	greview "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/review"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
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
	// WaivedReason is present only on waived findings, so a consumer reading
	// the JSON can always see why an error was excused without cross-checking
	// the artifact.
	WaivedReason string `json:"waived_reason,omitempty"`
}

// outDoc mirrors main()'s successful-run JSON dict, field order alphabetical
// for the same reason as outDiagnostic.
type outDoc struct {
	ArtifactsInScope   []string        `json:"artifacts_in_scope"`
	ArtifactsInspected int             `json:"artifacts_inspected"`
	Diagnostics        []outDiagnostic `json:"diagnostics"`
	PlanningRoot       string          `json:"planning_root"`
	Valid              bool            `json:"valid"`
	// Waived counts findings excused by accepted exceptions. It is reported
	// separately from Valid so a green run that rests on waivers is
	// distinguishable, in one field, from a green run that does not.
	Waived int `json:"waived"`
}

// validateOpts is what `sdd validate` needs from the command line.
//
// Format and JSON coexist deliberately: validate predates FR-04's uniform
// --json and spells it --format json, so the alias makes the uniform flag work
// without breaking the existing spelling or the callers using it. NoWaivers
// asks the opposite question from the default — what does this root violate
// with nothing excused — which is what a release gate or an audit wants, and
// what makes the waiver mechanism auditable rather than load-bearing.
type validateOpts struct {
	Root      string
	Scope     string
	Format    string
	JSON      bool
	NoWaivers bool
}

func cmdValidate(o validateOpts) error {
	format := o.Format
	if format == "" {
		format = "text"
	}
	if o.JSON {
		format = "json"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("validate: --format must be text or json")
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	resolved, repoRoot, err := resolveRoots(wd, o.Root)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
		return fmt.Errorf("validate: planning root %q is not a directory", resolved)
	}

	r, err := rules.LoadRootRepo(resolved, repoRoot)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if len(r.Artifacts) == 0 {
		return fmt.Errorf("validate: planning root contains no discoverable SDD artifacts")
	}

	evaluate := rules.RunWithWaiversChecked
	if o.NoWaivers {
		evaluate = rules.RunChecked
	}
	diags, err := evaluate(r)
	if err != nil {
		// Operational: the VCS could not be consulted, so nothing below is
		// authoritative. Not a refusal (exit 1) and never a clean run.
		return fmt.Errorf("validate: could not complete: %w", err)
	}
	graphDiags, err := graphCompleteButUnclosedDiagnostics(r, resolved, repoRoot)
	if err != nil {
		return fmt.Errorf("validate: could not complete: %w", err)
	}
	diags = append(diags, graphDiags...)
	rules.SortDiagnostics(diags)

	artifactsInScope := make([]string, 0, len(r.Artifacts))
	for _, a := range r.Artifacts {
		artifactsInScope = append(artifactsInScope, a.Rel)
	}
	if o.Scope != "" {
		artifactsInScope = filterInScope(artifactsInScope, o.Scope)
		diags = selectInScope(diags, o.Scope, artifactsInScope)
	}
	sort.Strings(artifactsInScope)

	valid := true
	operational := false
	waived := 0
	for _, d := range diags {
		if d.Severity.Invalidating() {
			valid = false
		}
		if d.Severity == rules.Operational {
			operational = true
		}
		if d.Severity == rules.Waived {
			waived++
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
			WaivedReason: d.WaivedReason,
		}
	}

	if format == "json" {
		doc := outDoc{
			ArtifactsInScope:   artifactsInScope,
			ArtifactsInspected: len(r.Artifacts),
			Diagnostics:        out,
			PlanningRoot:       resolved,
			Valid:              valid,
			Waived:             waived,
		}
		if err := printJSON(doc); err != nil {
			return err
		}
	} else {
		printValidateReport(resolved, o.Scope, len(r.Artifacts), len(artifactsInScope), out, valid, waived)
	}

	if operational {
		return fmt.Errorf("validate: could not complete: an operational finding was reported")
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
		if rules.Severity(d.Severity).Invalidating() {
			n++
		}
	}
	return n
}

// printValidateReport mirrors main()'s text-format branch exactly: a one-line
// summary, then one two-line block per diagnostic, then (only when there were
// none) a closing "Checked ..." line.
func printValidateReport(root, scope string, inspected, inScope int, diags []outDiagnostic, valid bool, waived int) {
	status := "Valid"
	if !valid {
		status = "Invalid"
	}
	scopeSummary := ""
	if scope != "" {
		scopeSummary = fmt.Sprintf(", %d in scope", inScope)
	}
	// A valid-with-waivers run says so on the headline. Anyone reading only the
	// first line of output should not come away believing the root passed
	// cleanly when part of that pass was granted rather than earned.
	waivedSummary := ""
	if waived > 0 {
		waivedSummary = fmt.Sprintf(", %d waived", waived)
	}
	fmt.Printf("%s: %s (%d artifacts inspected%s%s)\n", status, root, inspected, scopeSummary, waivedSummary)
	for _, d := range diags {
		fmt.Printf("%s %s %s:%d: %s\n", strings.ToUpper(d.Severity), d.Code, d.Path, d.Line, d.Message)
		if d.Severity == string(rules.Waived) {
			// The rationale replaces the correction: the finding stands, and
			// what a reader needs is the argument for accepting it.
			fmt.Printf("  Accepted exception: %s\n", d.WaivedReason)
			continue
		}
		fmt.Printf("  Required correction: %s\n", d.Correction)
	}
	if len(diags) == 0 {
		fmt.Println("Checked structure, frontmatter, paths, identifiers, hierarchy, dependencies, reviews, decisions, and completion-evidence shape.")
	}
}

// resolveRoots ports sdd_validate.py's resolve_roots(Path.cwd(), args.root):
// the planning root and the repository (Python's Validator.repo, the
// directory a project's own planning-config.json lives beside) are resolved
// together but are not the same directory in general — a plan's completion
// evidence targets code that may live outside the planning root entirely
// (shared/path-resolution.md's Target Repository chain), and repoRoot here is
// where that resolution starts from (internal/rules.RepoForArtifact walks
// from it via planning-config.json's `planMapping`).
func resolveRoots(cwd, explicit string) (root, repoRoot string, err error) {
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", "", err
	}
	vcsRoot := gitRoot(cwd)
	repo := cwd
	if vcsRoot != "" {
		repo = vcsRoot
	}
	if explicit != "" {
		p := explicit
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		resolved, err := filepath.Abs(p)
		if err != nil {
			return "", "", err
		}
		return filepath.Clean(resolved), repositoryForExplicitRoot(cwd, filepath.Clean(resolved), repo, vcsRoot), nil
	}
	current := cwd
	for {
		cfgPath := filepath.Join(current, "planning-config.json")
		if info, statErr := os.Stat(cfgPath); statErr == nil && !info.IsDir() {
			raw, readErr := os.ReadFile(cfgPath)
			if readErr != nil {
				return "", "", fmt.Errorf("cannot parse %s: %w", cfgPath, readErr)
			}
			var cfg struct {
				PlanningRoot *string `json:"planningRoot"`
			}
			if jsonErr := json.Unmarshal(raw, &cfg); jsonErr != nil {
				return "", "", fmt.Errorf("cannot parse %s: %w", cfgPath, jsonErr)
			}
			value := "."
			if cfg.PlanningRoot != nil {
				value = *cfg.PlanningRoot
			}
			resolvedRoot := value
			if !filepath.IsAbs(resolvedRoot) {
				resolvedRoot = filepath.Join(current, resolvedRoot)
			}
			resolvedRoot, err = filepath.Abs(resolvedRoot)
			if err != nil {
				return "", "", err
			}
			repoForConfig := current
			if vcsRoot != "" {
				repoForConfig = vcsRoot
			}
			return filepath.Clean(resolvedRoot), repoForConfig, nil
		}
		if (vcsRoot != "" && current == vcsRoot) || filepath.Dir(current) == current {
			return repo, repo, nil
		}
		current = filepath.Dir(current)
	}
}

// repositoryForExplicitRoot preserves validate's legacy explicit-root rule:
// repository scope is the VCS root (or cwd outside VCS), independently of
// the selected planning root.
func repositoryForExplicitRoot(cwd, planningRoot, legacyRepo, vcsRoot string) string {
	return legacyRepo
}

// gitRoot walks up from start looking for a `.git` entry (file or directory,
// so a linked worktree's gitdir-pointer file counts), mirroring
// sdd_validate.py's git_root(). Returns "" when none is found.
func gitRoot(start string) string {
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
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

// selectInScope keeps the diagnostics that bear on the requested scope.
//
// A diagnostic qualifies when it is reported against an in-scope artifact, or
// when it implicates one.
func selectInScope(diags []rules.Diagnostic, scope string, inScope []string) []rules.Diagnostic {
	scope = strings.Trim(filepath.ToSlash(scope), "/")
	allowed := map[string]bool{}
	for _, p := range inScope {
		allowed[p] = true
	}
	var out []rules.Diagnostic
	for _, d := range diags {
		if allowed[d.Path] || d.Path == scope || strings.HasPrefix(d.Path, scope+"/") {
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

// graphCompleteButUnclosedDiagnostics is SDD199/SDD200: a graph plan's
// README can say `status: complete` while its committed graph disagrees —
// CLAUDE.md's graph-plan discipline that "closure is a derived predicate",
// never an assertion. Judged the LIVE tree, that disagreement is unusable in
// CI: the moment any later commit touches a file a closed node owns, or any
// later edit changes a cited requirement's text, a legitimately completed
// plan goes red forever (P-23). For an already-complete plan every kind of
// later drift is post-completion drift, not a reopening — so closure here is
// judged twice:
//
//   - Observation-only (SDD199, Error): the same states.Derive + review.Closed
//     `sdd plan complete` used, but with every staleness axis that compares
//     current content against recorded content disabled (artifact digests,
//     dependency digests, intent hashes, input hashes all nil/unset) so
//     every byte and every cited requirement reads as unchanged — "was the
//     graph closed by its recorded observations?", stable across later
//     commits and later requirement edits alike. A node never verified, a
//     review gate never passed, or a node RED at its latest observation
//     still fails this and blocks.
//   - Live (SDD200, Warning/informational): the ordinary current-tree
//     derive `sdd graph status` uses (all four axes active). When
//     observation-only closure holds but live closure does not, that is
//     post-completion drift — later maintenance touched a closed node's
//     artifact, a dependency's artifact, a cited requirement, or a declared
//     input — reported as information, never as reopening the plan, naming
//     up to five nodes per drift kind.
//
// This lives here rather than in internal/rules because internal/rules
// cannot import internal/graph/{states,review} without an import cycle
// (those packages already depend on internal/rules-adjacent artifact
// parsing); it is a post-rules check over the same Root instead.
func graphCompleteButUnclosedDiagnostics(r *rules.Root, resolved, repoRoot string) ([]rules.Diagnostic, error) {
	var out []rules.Diagnostic
	for _, a := range r.Artifacts {
		if a.Kind() != "plan" || a.Status() != "complete" {
			continue
		}
		planDir := filepath.Dir(a.AbsPath)
		plan := filepath.Base(planDir)
		graphPath := gstore.PathFor(planDir)
		if _, statErr := os.Stat(graphPath); statErr != nil {
			if os.IsNotExist(statErr) {
				continue // v1 plan: SDD059/SDD070/... own this shape instead.
			}
			return nil, fmt.Errorf("checking for a committed graph in %s: %w", planDir, statErr)
		}
		g, err := gstore.Load(graphPath)
		if err != nil {
			continue // a malformed/unreadable graph is the graph subsystem's own refusal to report, not validate's.
		}
		sources, err := gcompile.NewSources(resolved, repoRoot, plan)
		if err != nil {
			return nil, err
		}
		snap := sources.IntentSnapshot()
		digester := digest.New(repoRoot)
		inputHashes := sources.InputResolver().GraphHashes(g)

		// Observation-only: every current-vs-recorded comparison axis is
		// disabled, so only the recorded observations themselves (pass/fail,
		// review scope, seq ordering) can withhold closure.
		stObs := states.Derive(states.Inputs{Graph: g})
		closedObs := greview.Closed(g, stObs)
		var open []string
		for _, n := range g.Nodes {
			if !closedObs[n.ID] {
				open = append(open, n.ID)
			}
		}
		if len(open) > 0 {
			sort.Strings(open)
			out = append(out, rules.Diagnostic{
				Code: "SDD199", Severity: rules.Error, Path: a.Rel, Line: 1,
				Message:    "Plan status is `complete` but its graph was never closed by its own recorded observations: " + strings.Join(open, ", ") + ".",
				Correction: "Close every node (sdd graph status) or move status off complete until the graph agrees.",
			})
			continue
		}

		st := states.Derive(states.Inputs{Graph: g, ArtifactDigest: digester.Artifact,
			CurrentIntentHashes: snap.Hashes(), CurrentInputHashes: inputHashes})
		closed := greview.Closed(g, st)

		drift := map[string][]string{"artifact": nil, "dependency": nil, "intent": nil, "input": nil}
		for _, n := range g.Nodes {
			if closed[n.ID] {
				continue
			}
			ns := st[n.ID]
			if len(ns.DigestStale) > 0 {
				drift["artifact"] = append(drift["artifact"], n.ID)
			}
			if len(ns.DependencyStale) > 0 {
				drift["dependency"] = append(drift["dependency"], n.ID)
			}
			if len(ns.IntentStale) > 0 {
				drift["intent"] = append(drift["intent"], n.ID)
			}
			if len(ns.InputStale) > 0 {
				drift["input"] = append(drift["input"], n.ID)
			}
		}
		total := 0
		var parts []string
		for _, kind := range []string{"artifact", "dependency", "intent", "input"} {
			nodes := drift[kind]
			if len(nodes) == 0 {
				continue
			}
			sort.Strings(nodes)
			total += len(nodes)
			named := nodes
			if len(named) > 5 {
				named = named[:5]
			}
			parts = append(parts, fmt.Sprintf("%s: %s", kind, strings.Join(named, ", ")))
		}
		if total == 0 {
			continue // closure disagrees for a reason none of the four axes explain (e.g. a claim); not this diagnostic's shape.
		}
		when := "an unrecorded revision (this plan completed before completed_at was tracked)"
		if g.CompletedAt != nil {
			when = g.CompletedAt.Revision
		}
		out = append(out, rules.Diagnostic{
			Code: "SDD200", Severity: rules.Warning, Path: a.Rel, Line: 1,
			Message: fmt.Sprintf("Post-completion drift: %d node(s) changed since the plan closed at %s (%s).",
				total, when, strings.Join(parts, "; ")),
			Correction: "Informational only — the plan closed by its recorded observations; no action required unless the drift is unexpected.",
		})
	}
	return out, nil
}
