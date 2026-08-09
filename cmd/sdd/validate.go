// Command sdd, subcommand `validate`, is a native, schema-driven validator
// over a whole planning root.
//
// PRE-PARITY: this is explicitly not a replacement for scripts/sdd_validate.py.
// It does not yet cover the Python validator's full rule set (evidence gates,
// review-artifact freeze checks, decision-ledger collision detection, and
// more), and its diagnostic codes are its own except for the four SDD codes
// below, whose exact semantics were verified against the Python validator's
// actual output. scripts/sdd_validate.py remains authoritative until the
// FR-30/FR-32 parity gate runs.
//
// Diagnostic codes:
//
//	SDD020  required heading/section absent (verified parity with Python)
//	SDD041  a `related` entry does not resolve to an existing file/directory
//	SDD064  a task id is not <phase>.<digits>[a-z]? or belongs to another phase
//	SDD122  an FR/NFR/AC/D-NNNN citation does not resolve
//
//	VLD001  required frontmatter field absent
//	VLD002  frontmatter field not declared by the schema
//	VLD003  duplicate top-level frontmatter key
//	VLD004  status value not in the schema's enum
//	VLD005  `updated` predates `created`
//	VLD006  a date field is not YYYY-MM-DD
//	VLD007  `type:` names an artifact type with no embedded schema
//	VLD008  duplicate identifier within a namespace
//	VLD009  identifier numbering gap (candidate — legal after a retirement)
//	VLD010  a plan phase entry's `doc` does not resolve
//	VLD011  a phase doc's plan/phase/title disagrees with its README entry
//	VLD012  a phase doc in the plan directory is not listed by the README
//	VLD013  depends_on names an unknown phase, itself, or forms a cycle
//	VLD014  `updated` is more than 30 days old (candidate)
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/internal/ymlite"
)

// Diagnostic is one validator finding.
type Diagnostic struct {
	Code       string `json:"code"`
	Severity   string `json:"severity"` // "error" | "candidate"
	Path       string `json:"path"`
	Line       int    `json:"line,omitempty"`
	Message    string `json:"message"`
	Correction string `json:"correction,omitempty"`
}

// vArtifact is one discovered planning-root document with frontmatter.
type vArtifact struct {
	Path   string // root-relative, forward slashes
	Abs    string
	Type   string
	Doc    *artifact.Doc
	Schema *schema.Schema // nil when the type has no embedded schema
}

// nestedKeys names the top-level frontmatter keys each type carries as a
// block sequence, modeled by ymlite rather than the flat Frontmatter field
// list — so the unknown-field check (VLD002) doesn't flag them.
var nestedKeys = map[string][]string{
	"plan":         {"phases"},
	"phase":        {"tasks"},
	"plan-phase":   {"tasks"},
	"decision-log": {"decisions"},
	"review":       {"findings", "lane_results"},
}

func cmdValidate(args []string) error {
	fs2 := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs2.SetOutput(os.Stderr)
	root := fs2.String("root", "", "planning root (default: resolved from planning-config.json)")
	scope := fs2.String("scope", "", "limit to one artifact file or one directory")
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

	arts, err := collectArtifacts(resolved)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	var scopeAbs string
	inScope := func(*vArtifact) bool { return true }
	if *scope != "" {
		scopeAbs, err = filepath.Abs(*scope)
		if err != nil {
			return fmt.Errorf("validate: %w", err)
		}
		inScope = func(a *vArtifact) bool {
			return a.Abs == scopeAbs || strings.HasPrefix(a.Abs, scopeAbs+string(filepath.Separator))
		}
	}

	diags := runChecks(resolved, arts)

	inScopeCount := 0
	var filtered []Diagnostic
	byPath := map[string]*vArtifact{}
	for i := range arts {
		byPath[arts[i].Abs] = &arts[i]
	}
	for _, a := range arts {
		if inScope(&a) {
			inScopeCount++
		}
	}
	for _, d := range diags {
		a, ok := byPath[filepath.Join(resolved, d.Path)]
		if ok && !inScope(a) {
			continue
		}
		filtered = append(filtered, d)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Path != filtered[j].Path {
			return filtered[i].Path < filtered[j].Path
		}
		if filtered[i].Line != filtered[j].Line {
			return filtered[i].Line < filtered[j].Line
		}
		return filtered[i].Code < filtered[j].Code
	})

	valid := true
	for _, d := range filtered {
		if d.Severity == "error" {
			valid = false
			break
		}
	}

	if *format == "json" {
		out := struct {
			Root             string       `json:"root"`
			Scope            string       `json:"scope,omitempty"`
			ArtifactsTotal   int          `json:"artifacts_inspected"`
			ArtifactsInScope int          `json:"artifacts_in_scope"`
			Valid            bool         `json:"valid"`
			Diagnostics      []Diagnostic `json:"diagnostics"`
		}{relPath(resolved), *scope, len(arts), inScopeCount, valid, filtered}
		if err := writeJSON(out); err != nil {
			return err
		}
	} else {
		printValidateReport(relPath(resolved), *scope, len(arts), inScopeCount, filtered)
	}

	if !valid {
		return &refusedError{n: countErrors(filtered)}
	}
	return nil
}

func countErrors(ds []Diagnostic) int {
	n := 0
	for _, d := range ds {
		if d.Severity == "error" {
			n++
		}
	}
	return n
}

func printValidateReport(root, scope string, total, inScope int, diags []Diagnostic) {
	fmt.Printf("validating %s", root)
	if scope != "" {
		fmt.Printf(" (scope: %s)", scope)
	}
	fmt.Println()
	fmt.Printf("artifacts: %d inspected, %d in scope\n\n", total, inScope)
	for _, d := range diags {
		loc := d.Path
		if d.Line > 0 {
			loc = fmt.Sprintf("%s:%d", d.Path, d.Line)
		}
		fmt.Printf("[%s] %s %s: %s\n", d.Severity, d.Code, loc, d.Message)
		if d.Correction != "" {
			fmt.Printf("    fix: %s\n", d.Correction)
		}
	}
	if len(diags) == 0 {
		fmt.Println("no diagnostics")
	}
	fmt.Println()
	fmt.Println("pre-parity: scripts/sdd_validate.py remains authoritative until the FR-30/FR-32 gate runs")
}

// collectArtifacts walks the planning root for every Markdown file carrying a
// `type:` frontmatter key.
func collectArtifacts(root string) ([]vArtifact, error) {
	var out []vArtifact
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		doc := artifact.Parse(string(b))
		if !doc.HasFrontmatter {
			return nil
		}
		typ, ok := doc.FM("type")
		if !ok || strings.TrimSpace(typ) == "" {
			return nil
		}
		typ = strings.Trim(strings.TrimSpace(typ), `"`)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		a := vArtifact{Path: filepath.ToSlash(rel), Abs: path, Type: typ, Doc: doc}
		if s, err := schema.Load(typ); err == nil {
			a.Schema = s
		}
		out = append(out, a)
		return nil
	})
	return out, err
}

func runChecks(root string, arts []vArtifact) []Diagnostic {
	var diags []Diagnostic

	// Global identifier registry for cross-document citation resolution
	// (SDD122): namespace -> set of numbers declared live or retired by ANY
	// artifact whose schema declares that namespace.
	global := map[string]map[int]bool{}
	for i := range arts {
		a := &arts[i]
		if a.Schema == nil {
			continue
		}
		folded := artifact.FoldDeeper(a.Doc.Sections, a.Schema.MaxSlotDepth())
		for _, h := range a.Schema.Headings {
			if h.IDNamespace == "" {
				continue
			}
			for _, sec := range folded {
				if sec.Heading != h.Text {
					continue
				}
				for _, vl := range artifact.VisibleLines(sec.Body) {
					m := idDeclRe.FindStringSubmatch(vl.Text)
					if m == nil {
						continue
					}
					ns, num, ok := splitIdent(m[2])
					if !ok || ns != h.IDNamespace {
						continue
					}
					if global[ns] == nil {
						global[ns] = map[int]bool{}
					}
					global[ns][num] = true
				}
			}
		}
	}

	for i := range arts {
		a := &arts[i]
		if a.Schema == nil {
			diags = append(diags, Diagnostic{
				Code: "VLD007", Severity: "candidate", Path: a.Path,
				Line:    a.Doc.FMLine("type"),
				Message: fmt.Sprintf("type %q has no embedded schema; structure was not checked", a.Type),
			})
			continue
		}
		diags = append(diags, checkHeadings(a)...)
		diags = append(diags, checkFrontmatter(a)...)
		diags = append(diags, checkRelated(root, a)...)
		diags = append(diags, checkTaskIDs(a)...)
		diags = append(diags, checkIdentifiers(a)...)
		diags = append(diags, checkCitations(a, global)...)
	}

	byPath := map[string]*vArtifact{}
	for i := range arts {
		byPath[arts[i].Abs] = &arts[i]
	}
	for i := range arts {
		if arts[i].Type == "plan" {
			diags = append(diags, checkPlanGraph(root, &arts[i], byPath)...)
		}
	}

	return diags
}

func checkHeadings(a *vArtifact) []Diagnostic {
	var out []Diagnostic
	folded := artifact.FoldDeeper(a.Doc.Sections, a.Schema.MaxSlotDepth())
	present := map[string]bool{}
	for _, sec := range folded {
		if h := a.Schema.Heading(sec.Heading); h != nil {
			present[h.Text] = true
		}
	}
	for _, h := range a.Schema.Headings {
		if h.Required && !present[h.Text] {
			out = append(out, Diagnostic{
				Code: "SDD020", Severity: "error", Path: a.Path,
				Message:    fmt.Sprintf("required section %q is absent", h.Text),
				Correction: fmt.Sprintf("add %q at depth %d, or run `sdd migrate`", h.Text, h.Depth),
			})
		}
	}
	return out
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func checkFrontmatter(a *vArtifact) []Diagnostic {
	var out []Diagnostic

	seen := map[string]int{}
	nested := map[string]bool{}
	for _, k := range nestedKeys[a.Type] {
		nested[k] = true
	}
	for _, e := range a.Doc.Frontmatter {
		if prev, dup := seen[e.Key]; dup {
			out = append(out, Diagnostic{
				Code: "VLD003", Severity: "error", Path: a.Path, Line: e.Line,
				Message:    fmt.Sprintf("frontmatter key %q is declared twice (also line %d)", e.Key, prev),
				Correction: "remove the duplicate",
			})
			continue
		}
		seen[e.Key] = e.Line
		if nested[e.Key] {
			continue
		}
		if a.Schema.Field(e.Key) == nil && a.Schema.FieldByAlias(e.Key) == nil {
			out = append(out, Diagnostic{
				Code: "VLD002", Severity: "error", Path: a.Path, Line: e.Line,
				Message:    fmt.Sprintf("frontmatter key %q is not declared by the %s schema", e.Key, a.Type),
				Correction: "remove the key, or add it to the schema if it belongs there",
			})
		}
	}

	for _, f := range a.Schema.Frontmatter {
		v, ok := a.Doc.FM(f.Key)
		if !ok {
			if f.Required {
				out = append(out, Diagnostic{
					Code: "VLD001", Severity: "error", Path: a.Path,
					Message:    fmt.Sprintf("required frontmatter key %q is absent", f.Key),
					Correction: fmt.Sprintf("add %s:, or run `sdd migrate`", f.Key),
				})
			}
			continue
		}
		v = strings.Trim(v, `"`)
		if f.Key == "status" && len(f.Enum) > 0 {
			ok := false
			for _, e := range f.Enum {
				if v == e {
					ok = true
					break
				}
			}
			if !ok {
				out = append(out, Diagnostic{
					Code: "VLD004", Severity: "error", Path: a.Path, Line: a.Doc.FMLine("status"),
					Message:    fmt.Sprintf("status %q is not one of %v", v, f.Enum),
					Correction: fmt.Sprintf("use one of: %s", strings.Join(f.Enum, ", ")),
				})
			}
		}
		if f.Key == "created" || f.Key == "updated" {
			if !dateRe.MatchString(v) {
				out = append(out, Diagnostic{
					Code: "VLD006", Severity: "error", Path: a.Path, Line: a.Doc.FMLine(f.Key),
					Message:    fmt.Sprintf("%s %q is not a YYYY-MM-DD date", f.Key, v),
					Correction: "use ISO date format YYYY-MM-DD",
				})
			}
		}
	}

	created := strings.Trim(func() string { v, _ := a.Doc.FM("created"); return v }(), `"`)
	updated := strings.Trim(func() string { v, _ := a.Doc.FM("updated"); return v }(), `"`)
	if dateRe.MatchString(created) && dateRe.MatchString(updated) && updated < created {
		out = append(out, Diagnostic{
			Code: "VLD005", Severity: "error", Path: a.Path, Line: a.Doc.FMLine("updated"),
			Message:    fmt.Sprintf("updated (%s) predates created (%s)", updated, created),
			Correction: "correct whichever date is wrong",
		})
	}
	if t, err := time.Parse("2006-01-02", updated); err == nil {
		if time.Since(t) > 30*24*time.Hour {
			out = append(out, Diagnostic{
				Code: "VLD014", Severity: "candidate", Path: a.Path, Line: a.Doc.FMLine("updated"),
				Message: fmt.Sprintf("updated %s is more than 30 days old", updated),
			})
		}
	}

	return out
}

func checkRelated(root string, a *vArtifact) []Diagnostic {
	var out []Diagnostic
	v, ok := a.Doc.FM("related")
	if !ok {
		return nil
	}
	item := ymlite.Item{"related": v}
	for _, entry := range item.List("related") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if resolvesRelated(root, entry) {
			continue
		}
		out = append(out, Diagnostic{
			Code: "SDD041", Severity: "error", Path: a.Path, Line: a.Doc.FMLine("related"),
			Message:    fmt.Sprintf("related entry %q does not resolve to an existing file or directory", entry),
			Correction: "fix the path, or remove the entry",
		})
	}
	return out
}

func resolvesRelated(root, entry string) bool {
	entry = strings.TrimPrefix(entry, "./")
	candidates := []string{
		filepath.Join(root, entry),
		filepath.Join(root, entry, "README.md"),
		filepath.Join(root, entry+".md"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

var taskIDRe = regexp.MustCompile(`^(\d+)\.\d+[a-z]?$`)

func checkTaskIDs(a *vArtifact) []Diagnostic {
	if a.Type != "phase" && a.Type != "plan-phase" {
		return nil
	}
	var out []Diagnostic
	start, end, found := ymlite.Block(a.Doc.FrontmatterRaw, "tasks")
	if !found {
		return nil
	}
	phaseNum := strings.Trim(fmVal(a.Doc, "phase"), `"`)
	line := a.Doc.FMLine("tasks")
	for _, it := range ymlite.Items(a.Doc.FrontmatterRaw[start:end]) {
		id := it.Str("id")
		m := taskIDRe.FindStringSubmatch(id)
		if m == nil {
			out = append(out, Diagnostic{
				Code: "SDD064", Severity: "error", Path: a.Path, Line: line,
				Message:    fmt.Sprintf("task id %q is not of the form <phase>.<digits>[a-z]?", id),
				Correction: "rename the task id to match the phase it belongs to",
			})
			continue
		}
		if phaseNum != "" && m[1] != phaseNum {
			out = append(out, Diagnostic{
				Code: "SDD064", Severity: "error", Path: a.Path, Line: line,
				Message:    fmt.Sprintf("task id %q is assigned to phase %s, but this document is phase %s", id, m[1], phaseNum),
				Correction: "renumber the task, or move it to the phase it belongs to",
			})
		}
	}
	return out
}

// fmVal returns a frontmatter value or "" when the key is absent.
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

// checkIdentifiers finds duplicate declarations and numbering gaps within one
// artifact's own identifier-bearing sections.
func checkIdentifiers(a *vArtifact) []Diagnostic {
	var out []Diagnostic
	folded := artifact.FoldDeeper(a.Doc.Sections, a.Schema.MaxSlotDepth())
	for _, h := range a.Schema.Headings {
		if h.IDNamespace == "" {
			continue
		}
		seen := map[int]int{} // number -> first line
		var nums []int
		for _, sec := range folded {
			if sec.Heading != h.Text {
				continue
			}
			for _, vl := range artifact.VisibleLines(sec.Body) {
				m := idDeclRe.FindStringSubmatch(vl.Text)
				if m == nil {
					continue
				}
				ns, num, ok := splitIdent(m[2])
				if !ok || ns != h.IDNamespace {
					continue
				}
				line := sec.Line + 1 + vl.Offset
				if prev, dup := seen[num]; dup {
					out = append(out, Diagnostic{
						Code: "VLD008", Severity: "error", Path: a.Path, Line: line,
						Message:    fmt.Sprintf("%s is declared twice (also line %d)", m[2], prev),
						Correction: "renumber one of the two declarations",
					})
					continue
				}
				seen[num] = line
				nums = append(nums, num)
			}
		}
		if len(nums) < 2 {
			continue
		}
		sort.Ints(nums)
		for n := nums[0]; n < nums[len(nums)-1]; n++ {
			if _, ok := seen[n]; !ok {
				fmtNS := h.IDNamespace
				out = append(out, Diagnostic{
					Code: "VLD009", Severity: "candidate", Path: a.Path, Line: seen[nums[0]],
					Message: fmt.Sprintf("%s-%d is skipped between %s-%d and %s-%d (legal after a retirement)",
						fmtNS, n, fmtNS, nums[0], fmtNS, nums[len(nums)-1]),
				})
			}
		}
	}
	return out
}

var citeRe = regexp.MustCompile(`\b([A-Z]{1,4})-(\d{1,4})\b`)

func checkCitations(a *vArtifact, global map[string]map[int]bool) []Diagnostic {
	var out []Diagnostic
	for _, h := range a.Schema.Headings {
		if h.FreeProse {
			continue
		}
		sec := findFoldedSection(a, h.Text)
		if sec == nil {
			continue
		}
		for _, vl := range artifact.VisibleLines(sec.Body) {
			text := artifact.StripCodeSpans(vl.Text)
			for _, m := range citeRe.FindAllStringSubmatch(text, -1) {
				ns := m[1]
				if global[ns] == nil {
					continue // no schema declares this namespace: not a citation
				}
				num, err := strconv.Atoi(m[2])
				if err != nil || global[ns][num] {
					continue
				}
				out = append(out, Diagnostic{
					Code: "SDD122", Severity: "error", Path: a.Path, Line: sec.Line + 1 + vl.Offset,
					Message:    fmt.Sprintf("citation %s-%s does not resolve", ns, m[2]),
					Correction: "fix the identifier, or wrap a literal example in backticks",
				})
			}
		}
	}
	return out
}

func findFoldedSection(a *vArtifact, heading string) *artifact.Section {
	folded := artifact.FoldDeeper(a.Doc.Sections, a.Schema.MaxSlotDepth())
	for i := range folded {
		if folded[i].Heading == heading {
			return &folded[i]
		}
	}
	return nil
}

// checkPlanGraph validates a plan README's phases[] against the phase docs on
// disk: doc resolution, README/doc agreement, orphaned phase docs, and
// depends_on well-formedness (unknown targets, self-dependencies, cycles).
func checkPlanGraph(root string, plan *vArtifact, byPath map[string]*vArtifact) []Diagnostic {
	var out []Diagnostic
	start, end, found := ymlite.Block(plan.Doc.FrontmatterRaw, "phases")
	if !found {
		return nil
	}
	items := ymlite.Items(plan.Doc.FrontmatterRaw[start:end])
	planDir := filepath.Dir(plan.Abs)
	planDirName := filepath.Base(planDir)
	line := plan.Doc.FMLine("phases")

	ids := map[int]bool{}
	deps := map[int][]int{}
	referenced := map[string]bool{}

	for _, it := range items {
		id, _ := strconv.Atoi(it.Str("id"))
		ids[id] = true
	}
	for _, it := range items {
		id, _ := strconv.Atoi(it.Str("id"))
		docRel := it.Str("doc")
		docAbs := filepath.Clean(filepath.Join(planDir, docRel))
		referenced[docAbs] = true

		if _, err := os.Stat(docAbs); err != nil {
			out = append(out, Diagnostic{
				Code: "VLD010", Severity: "error", Path: plan.Path, Line: line,
				Message:    fmt.Sprintf("phase %d's doc %q does not resolve", id, docRel),
				Correction: "fix the doc path, or create the phase document",
			})
		} else if pd, ok := byPath[docAbs]; ok {
			wantTitle := it.Str("title")
			gotTitle := strings.Trim(fmVal(pd.Doc, "title"), `"`)
			if wantTitle != "" && gotTitle != "" && wantTitle != gotTitle {
				out = append(out, Diagnostic{
					Code: "VLD011", Severity: "error", Path: pd.Path, Line: pd.Doc.FMLine("title"),
					Message: fmt.Sprintf("phase doc title %q disagrees with the README entry %q", gotTitle, wantTitle),
				})
			}
			gotPhase := strings.Trim(fmVal(pd.Doc, "phase"), `"`)
			if gotPhase != "" && gotPhase != strconv.Itoa(id) {
				out = append(out, Diagnostic{
					Code: "VLD011", Severity: "error", Path: pd.Path, Line: pd.Doc.FMLine("phase"),
					Message: fmt.Sprintf("phase doc's phase %q disagrees with README id %d", gotPhase, id),
				})
			}
			gotPlan := strings.Trim(fmVal(pd.Doc, "plan"), `"`)
			if gotPlan != "" && gotPlan != planDirName {
				out = append(out, Diagnostic{
					Code: "VLD011", Severity: "error", Path: pd.Path, Line: pd.Doc.FMLine("plan"),
					Message: fmt.Sprintf("phase doc's plan %q disagrees with plan directory %q", gotPlan, planDirName),
				})
			}
		}

		for _, ds := range it.List("depends_on") {
			ds = strings.TrimSpace(ds)
			if ds == "" {
				continue
			}
			d, err := strconv.Atoi(ds)
			if err != nil {
				continue
			}
			if d == id {
				out = append(out, Diagnostic{
					Code: "VLD013", Severity: "error", Path: plan.Path, Line: line,
					Message: fmt.Sprintf("phase %d depends on itself", id),
				})
				continue
			}
			if !ids[d] {
				out = append(out, Diagnostic{
					Code: "VLD013", Severity: "error", Path: plan.Path, Line: line,
					Message: fmt.Sprintf("phase %d depends on unknown phase %d", id, d),
				})
				continue
			}
			deps[id] = append(deps[id], d)
		}
	}

	if cyc := findCycle(ids, deps); cyc != "" {
		out = append(out, Diagnostic{
			Code: "VLD013", Severity: "error", Path: plan.Path, Line: line,
			Message: fmt.Sprintf("phase dependency cycle: %s", cyc),
		})
	}

	// Phase docs present in the plan directory but not referenced by any
	// phases[] entry.
	ents, _ := os.ReadDir(planDir)
	phaseFileRe := regexp.MustCompile(`^\d+-.*\.md$`)
	for _, e := range ents {
		if e.IsDir() || !phaseFileRe.MatchString(e.Name()) {
			continue
		}
		abs := filepath.Join(planDir, e.Name())
		if !referenced[abs] {
			out = append(out, Diagnostic{
				Code: "VLD012", Severity: "error", Path: filepath.ToSlash(mustRel(root, abs)),
				Message:    "phase doc exists in the plan directory but is not listed by the README's phases[]",
				Correction: "add a phases[] entry, or remove/rename the orphaned file",
			})
		}
	}

	return out
}

func mustRel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}

// findCycle detects a cycle in the depends_on graph via DFS, returning a
// human-readable path description or "" when the graph is acyclic.
func findCycle(ids map[int]bool, deps map[int][]int) string {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := map[int]int{}
	var stack []int

	var visit func(n int) string
	visit = func(n int) string {
		state[n] = visiting
		stack = append(stack, n)
		for _, m := range deps[n] {
			switch state[m] {
			case visiting:
				stack = append(stack, m)
				return cyclePath(stack)
			case unvisited:
				if s := visit(m); s != "" {
					return s
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = done
		return ""
	}

	var order []int
	for id := range ids {
		order = append(order, id)
	}
	sort.Ints(order)
	for _, id := range order {
		if state[id] == unvisited {
			if s := visit(id); s != "" {
				return s
			}
		}
	}
	return ""
}

func cyclePath(stack []int) string {
	// Trim to the cycle itself: from the repeated node's first occurrence.
	last := stack[len(stack)-1]
	start := 0
	for i, n := range stack {
		if n == last {
			start = i
			break
		}
	}
	parts := make([]string, 0, len(stack)-start)
	for _, n := range stack[start:] {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, " -> ")
}
