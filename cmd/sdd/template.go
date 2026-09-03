package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/proposal"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

// `sdd template` generates an artifact template from its schema (task 6.2).
//
// The templates in shared/templates/ and the schemas in internal/schema/ state
// the same structure twice: the required headings, their order and depth, and
// the frontmatter fields an author fills in. Two statements of one fact drift,
// and the drift is silent — a template can grow a heading the schema does not
// declare, and nothing fails until an author uses it and `sdd apply` refuses a
// document that looked correct.
//
// Generating from the schema makes the schema the single source. `make
// check-templates` regenerates and diffs, so drift fails the build instead of
// surfacing as a confusing refusal later.

const templateUsage = `sdd template <type> [--out PATH] [--for-apply]
sdd template --check

Generates an artifact template from its schema.

By default the output is a complete artifact: every frontmatter field is
present, including the ones the tool owns (type, status, created, updated).
That is the form to write to disk and fill in by hand.

--for-apply omits the tool-owned fields, producing a payload that sdd apply
accepts. apply sets those fields itself and refuses a payload that carries a
conflicting value, so the default form is not valid apply input.

--check regenerates every committed template and fails if any differs.`

type templateOpts struct {
	Out      string
	Check    bool
	Dir      string
	ForApply bool
	Schema   bool
	JSON     bool
}

// graphProposalType is the one non-markdown template: a JSON payload skeleton
// plus its JSON Schema, both generated from one source in
// internal/graph/proposal (Designs/SddGraph DD-12).
const graphProposalType = "graph-proposal"

func cmdTemplate(artifactType string, o templateOpts) error {
	if o.Check {
		return checkTemplates(o.Dir, o.JSON)
	}
	if artifactType == "" {
		return fmt.Errorf("template: expected exactly one artifact type\n\n%s", templateUsage)
	}

	var body string
	switch {
	case artifactType == graphProposalType:
		if o.ForApply {
			return fmt.Errorf("template: --for-apply applies to markdown artifact types; a graph proposal is already the payload `sdd graph propose` accepts")
		}
		if o.Schema {
			body = string(proposal.SchemaJSON())
		} else {
			raw, err := proposal.ExemplarJSON()
			if err != nil {
				return fmt.Errorf("template: %w", err)
			}
			body = string(raw)
		}
	case o.Schema:
		return fmt.Errorf("template: --schema applies only to %s", graphProposalType)
	default:
		var err error
		body, err = renderTemplateFor(artifactType, o.ForApply)
		if err != nil {
			return fmt.Errorf("template: %w", err)
		}
	}
	if o.JSON {
		res := templateResult{OK: true, Type: artifactType, ForApply: o.ForApply, Body: body}
		if o.Out != "" {
			if dir := filepath.Dir(o.Out); dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("template: creating %s: %w", dir, err)
				}
			}
			if err := os.WriteFile(o.Out, []byte(body), 0o644); err != nil {
				return fmt.Errorf("template: %w", err)
			}
			res.Path, res.Wrote = relPath(o.Out), true
		}
		return writeJSON(res)
	}
	if o.Out == "" {
		fmt.Print(body)
		return nil
	}
	// Create the parent directory. Artifact layouts are nested by convention
	// (Specs/<Feature>/README.md), so the first write of a new artifact almost
	// always targets a directory that does not exist yet; failing with a bare
	// ENOENT made the tool look broken at the exact moment an author was
	// starting work.
	if dir := filepath.Dir(o.Out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("template: creating %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(o.Out, []byte(body), 0o644); err != nil {
		return fmt.Errorf("template: %w", err)
	}
	fmt.Printf("wrote %s\n", o.Out)
	return nil
}

// renderTemplate builds one artifact type's template from its schema.
func renderTemplate(artifactType string) (string, error) {
	return renderTemplateFor(artifactType, false)
}

// renderTemplateFor builds one artifact type's template.
//
// forApply omits the fields the tool owns. The two consumers need opposite
// documents: a skill writing an artifact by hand wants the complete file,
// tool-owned fields included; an `sdd apply` payload must omit them, because
// apply sets them itself and refuses a payload that carries a conflicting
// value. Emitting one document for both is what made `sdd template spec`
// output invalid as `sdd apply` input — the template's unexpanded
// `created: {{DATE}}` is not a date, so apply read it as a conflict.
//
// The full form stays the default: it is what shared/templates/ contains and
// what `--check` compares against.
func renderTemplateFor(artifactType string, forApply bool) (string, error) {
	s, err := schema.Load(artifactType)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("---\n")
	for _, f := range s.Frontmatter {
		if forApply && f.Ownership() == schema.Tool {
			continue
		}
		b.WriteString(f.Key + ": " + templateFieldValue(f, artifactType) + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString("# {{TITLE}}\n")
	for _, h := range s.Headings {
		b.WriteString("\n" + h.Text + "\n\n")
		if h.TemplateBody != "" {
			b.WriteString(h.TemplateBody + "\n")
			continue
		}
		if h.DefaultBody != "" {
			b.WriteString(h.DefaultBody + "\n")
			continue
		}
		b.WriteString("{{" + placeholderFor(h.Title()) + "}}\n")
	}
	return b.String(), nil
}

// templateFieldValue is the value a fresh artifact starts with: the schema's
// fixed value where it declares one, its default where it declares one, and a
// placeholder otherwise.
func templateFieldValue(f schema.Field, artifactType string) string {
	switch {
	case f.Fixed != "":
		return f.Fixed
	case f.Key == "type":
		return artifactType
	case f.Key == "created" || f.Key == "updated":
		// Quoted: an unquoted {{DATE}} parses as a YAML flow mapping with an
		// unhashable key, which makes the document unwritable by `sdd apply`.
		return `"{{DATE}}"`
	case f.Key == "title":
		return `"{{TITLE}}"`
	case f.Default != "":
		return f.Default
	case len(f.Enum) > 0:
		return f.Enum[0]
	case f.Entry != nil:
		// A block-sequence field (decisions[], phases[], tasks[], findings[])
		// is a list, so its empty form is `[]`. Emitting `""` produced a
		// scalar that the first `sdd decide add` could not splice into: it
		// appended a second `decisions:` key, and the duplicate made the
		// ledger unparseable YAML on the very first write.
		return "[]"
	default:
		return `""`
	}
}

// placeholderFor turns a heading into an uppercase snake-case placeholder.
func placeholderFor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(heading) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// checkTemplates regenerates every type that has a committed template and
// reports the ones that differ.
//
// It compares STRUCTURE, not bytes: a committed template carries authored
// guidance prose the schema cannot know, and demanding byte equality would
// force that prose out of the templates or into the schema, neither of which
// is an improvement. What must not drift is the set and order of headings and
// the frontmatter keys.
// templateResult is the machine-readable outcome of rendering a template.
type templateResult struct {
	// OK is present on every result shape the binary emits, so a caller can
	// branch on one field regardless of which command produced the document.
	OK       bool   `json:"ok"`
	Type     string `json:"type"`
	ForApply bool   `json:"for_apply"`
	Path     string `json:"path,omitempty"`
	Wrote    bool   `json:"wrote,omitempty"`
	Body     string `json:"body"`
}

// templateCheckResult reports the drift gate's verdict. --check is a CI-shaped
// command, so a pipeline should be able to read which templates drifted and
// why without parsing the rendered report.
type templateCheckResult struct {
	OK      bool                 `json:"ok"`
	Checked int                  `json:"checked"`
	Drifted []templateDriftEntry `json:"drifted,omitempty"`
	Remedy  string               `json:"remedy,omitempty"`
}

type templateDriftEntry struct {
	Path       string `json:"path"`
	Difference string `json:"difference"`
}

func checkTemplates(dir string, jsonOut bool) error {
	var drifted []string
	checked := 0

	for _, t := range schema.Types() {
		path := filepath.Join(dir, t+".md")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // no committed template for this type
		}
		checked++
		generated, err := renderTemplate(t)
		if err != nil {
			return fmt.Errorf("template --check: %s: %w", t, err)
		}
		loaded, err := schema.Load(t)
		if err != nil {
			return fmt.Errorf("template --check: %s: %w", t, err)
		}
		requiredKeys = map[string]bool{}
		for _, f := range loaded.Frontmatter {
			if f.Required {
				requiredKeys[f.Key] = true
			}
		}
		requiredHeadings = map[string]bool{}
		for _, h := range loaded.Headings {
			if h.Required {
				requiredHeadings[h.Text] = true
			}
		}
		// A schema that declares no headings and allows additional sections
		// makes no claim about body structure — plan-phase and decision-log
		// are frontmatter-plus-prose by design. Comparing headings there would
		// report drift against a statement the schema never made.
		allowExtraHeadings = len(loaded.Headings) == 0 && loaded.AdditionalSections == "allowed"
		if diff := structuralDiff(string(raw), generated); diff != "" {
			drifted = append(drifted, path+": "+diff)
		}
	}

	// The graph-proposal pair is generated wholesale — no authored prose to
	// preserve — so unlike the markdown templates it is compared by content
	// bytes (CRLF-normalized for autocrlf checkouts), not by structure.
	for _, gen := range []struct {
		name   string
		render func() ([]byte, error)
	}{
		{graphProposalType + ".json", proposal.ExemplarJSON},
		{graphProposalType + ".schema.json", func() ([]byte, error) { return proposal.SchemaJSON(), nil }},
	} {
		path := filepath.Join(dir, gen.name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // no committed copy of this generated file
		}
		checked++
		want, err := gen.render()
		if err != nil {
			return fmt.Errorf("template --check: %s: %w", gen.name, err)
		}
		if !bytes.Equal(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), want) {
			drifted = append(drifted, path+": content differs from the generated form")
		}
	}

	const remedy = "regenerate with `sdd template <type> --out <path>`, " +
		"or correct the schema if the template is right"

	if jsonOut {
		res := templateCheckResult{OK: len(drifted) == 0, Checked: checked}
		for _, d := range drifted {
			path, difference, _ := strings.Cut(d, ": ")
			res.Drifted = append(res.Drifted, templateDriftEntry{Path: path, Difference: difference})
		}
		if !res.OK {
			res.Remedy = remedy
		}
		if err := writeJSON(res); err != nil {
			return err
		}
		if !res.OK {
			return &refusedError{n: len(drifted)}
		}
		return nil
	}

	if len(drifted) > 0 {
		var b strings.Builder
		b.WriteString("template --check: committed templates drifted from their schemas:\n")
		for _, d := range drifted {
			b.WriteString("  " + d + "\n")
		}
		b.WriteString("  fix: " + remedy)
		return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}
	fmt.Printf("%d templates match their schemas\n", checked)
	return nil
}

// structuralDiff reports the first structural difference between a committed
// template and its generated form, or "".
// requiredKeys is set per type by checkTemplates before each comparison.
var requiredKeys = map[string]bool{}

// allowExtraHeadings is set per type: true when the schema declares no body
// structure at all.
var allowExtraHeadings bool

// requiredHeadings is set per type: only a REQUIRED heading must appear in the
// committed template. An optional one (a design's `## Open Questions`) is
// offered by the schema, not demanded, and a template that omits it is making
// a legitimate authoring choice.
var requiredHeadings = map[string]bool{}

func structuralDiff(committed, generated string) string {
	// Compare only the DECLARED headings, in declared order. A template also
	// carries example instances under them — `### Idea 0`, `### Decision 1`,
	// `### F-01` — which are authoring guidance the schema neither knows nor
	// should. Requiring an exact heading list would force that guidance out of
	// the templates, which makes them worse for the humans who use them.
	//
	// What must not drift: every declared heading is present, spelled exactly,
	// and in the schema's order.
	if allowExtraHeadings {
		return frontmatterDiff(committed, generated)
	}
	committedHeads := headingsOf(committed)
	position := 0
	for _, want := range headingsOf(generated) {
		if !requiredHeadings[want] {
			continue
		}
		found := -1
		for i := position; i < len(committedHeads); i++ {
			if committedHeads[i] == want {
				found = i
				break
			}
		}
		if found < 0 {
			if containsString(committedHeads, want) {
				return fmt.Sprintf("heading %q appears out of the schema's order", want)
			}
			return fmt.Sprintf("is missing schema heading %q", want)
		}
		position = found + 1
	}
	return frontmatterDiff(committed, generated)
}

// frontmatterDiff compares the frontmatter halves alone.
func frontmatterDiff(committed, generated string) string {
	cKeys, gKeys := frontmatterKeysOf(committed), frontmatterKeysOf(generated)
	// Only REQUIRED keys must appear. An optional field a template omits is a
	// deliberate choice — a debrief's `phase_status` is filled in when the
	// phase closes, not when the template is instantiated — and demanding it
	// would push optional fields into every fresh artifact as clutter.
	for _, k := range gKeys {
		if requiredKeys[k] && !containsString(cKeys, k) {
			return fmt.Sprintf("frontmatter is missing required schema key %q", k)
		}
	}
	for _, k := range cKeys {
		if !containsString(gKeys, k) {
			return fmt.Sprintf("frontmatter has undeclared key %q", k)
		}
	}
	return ""
}

// headingsOf returns a document's H2 and deeper headings, excluding the H1
// title, which templates render from {{TITLE}} rather than from the schema.
func headingsOf(doc string) []string {
	var out []string
	inFrontmatter := false
	for i, line := range strings.Split(doc, "\n") {
		if strings.TrimSpace(line) == "---" {
			if i == 0 {
				inFrontmatter = true
				continue
			}
			inFrontmatter = false
			continue
		}
		if inFrontmatter {
			continue
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			out = append(out, strings.TrimRight(line, " \t"))
		}
	}
	return out
}

func frontmatterKeysOf(doc string) []string {
	var out []string
	lines := strings.Split(doc, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if i := strings.Index(line, ":"); i > 0 {
			out = append(out, line[:i])
		}
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
