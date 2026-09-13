package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Pure transformations of the rules package, at their current package seam
// (DD-8). Every test here runs with no repository, no SCM binary and no
// subprocess: the inputs are strings and in-memory structures, or at most a
// plain directory of files that no rule in the local rule list consults the
// repository for.
//
// They are named TestPure* because that prefix IS the selector `make
// test-pure` uses; inventory_test.go pins the executed set and proves the
// selection runs with nothing on PATH. A case added here that starts a
// process would fail there, which is the guard that keeps the prefix
// meaningful.

// pureRoot builds a Root over a plain directory of artifact files. No git
// is initialized, so only rules that read artifact content can be evaluated
// against it.
func pureRoot(t *testing.T, files map[string]string) *Root {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := LoadRoot(dir)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	return root
}

// pureArtifact models raw bytes as an artifact, the ParseArtifactBytes seam
// historical reconstruction uses.
func pureArtifact(t *testing.T, rel, source string) *Artifact {
	t.Helper()
	a := ParseArtifactBytes([]byte(source), rel)
	if a == nil {
		t.Fatalf("ParseArtifactBytes(%s) returned nil", rel)
	}
	return a
}

// TestPureParseArtifactBytesStageLadder: the parse gate is a ladder, and
// which rung a document stops on decides which rules ever see it. CRLF is
// the one non-fatal rung — it records SDD003 and still models the document.
func TestPureParseArtifactBytesStageLadder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		wantStage string
		wantMeta  bool
	}{
		{"valid", validResearch, "", true},
		{"no-opening-delimiter", "# Heading\n\nbody\n", "SDD004", false},
		{"no-closing-delimiter", "---\ntitle: x\ntype: research\n", "SDD005", false},
		{"invalid-yaml", "---\ntitle x\ntype: research\n---\nbody\n", "SDD006", false},
		{"not-a-mapping", "---\n- a\n- b\n---\nbody\n", "SDD007", false},
		{"crlf-is-not-fatal", strCRLF(validResearch), "SDD003", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := pureArtifact(t, "Research/x.md", tc.source)
			if a.ParseStage != tc.wantStage {
				t.Errorf("ParseStage = %q, want %q", a.ParseStage, tc.wantStage)
			}
			if got := a.Meta != nil; got != tc.wantMeta {
				t.Errorf("modeled frontmatter = %v, want %v", got, tc.wantMeta)
			}
			if tc.wantStage == "SDD003" && !a.HasCRLF {
				t.Error("CRLF source did not set HasCRLF")
			}
		})
	}

	// Invalid UTF-8 is SDD002, reported without an underlying read error.
	bad := ParseArtifactBytes([]byte("---\ntitle: x\n---\n\xff\xfe"), "Research/bad.md")
	if bad.ParseStage != "SDD002" || bad.ParseDetail == "" {
		t.Errorf("invalid UTF-8: stage=%q detail=%q, want SDD002 with a detail", bad.ParseStage, bad.ParseDetail)
	}

	// A modeled artifact carries the body offset its diagnostics' line
	// numbers are computed from.
	ok := pureArtifact(t, "Research/ok.md", validResearch)
	if ok.BodyLine <= 1 {
		t.Errorf("BodyLine = %d, want the first line after the closing delimiter", ok.BodyLine)
	}
	if want := ok.Line("## Findings", true); want <= ok.BodyLine {
		t.Errorf("body line lookup = %d, want a line inside the body (after %d)", want, ok.BodyLine)
	}
}

// TestPureFrontmatterScalarNormalization: rules compare frontmatter against
// string literals, so YAML's native typing must be flattened — dates and
// numbers as written, booleans kept as bool, null as the empty string, and
// a quoted "true" still a string.
func TestPureFrontmatterScalarNormalization(t *testing.T) {
	a := pureArtifact(t, "Research/x.md", `---
title: Sample
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
version: 1
enabled: true
quoted: "true"
empty:
tags: [a, b]
related: []
---

## Context

Text.
`)
	if a.Meta == nil {
		t.Fatal("frontmatter did not parse")
	}
	for field, want := range map[string]any{
		"created": "2024-01-01",
		"version": "1",
		"enabled": true,
		"quoted":  "true",
		"empty":   "",
	} {
		if got := a.Meta[field]; !reflect.DeepEqual(got, want) {
			t.Errorf("Meta[%q] = %#v (%T), want %#v", field, got, got, want)
		}
	}
	tags, ok := a.Meta["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "a" {
		t.Errorf("Meta[tags] = %#v, want a two-element []any of strings", a.Meta["tags"])
	}
	if related, ok := a.Meta["related"].([]any); !ok || len(related) != 0 {
		t.Errorf("Meta[related] = %#v, want an empty []any", a.Meta["related"])
	}
	if a.Kind() != "research" || a.Status() != "draft" {
		t.Errorf("Kind()/Status() = %q/%q, want research/draft", a.Kind(), a.Status())
	}
}

// TestPureMarkdownVisibilityStripsFencesAndComments: the visible rendering
// is what every heading and citation scan reads. Content inside a fenced
// block or an HTML comment must not be visible, or an example in a code
// block would be mistaken for the document's own structure.
func TestPureMarkdownVisibilityStripsFencesAndComments(t *testing.T) {
	body := "" +
		"Real text.\n" +
		"<!-- hidden FR-99 -->\n" +
		"```\n" +
		"## Not A Heading\n" +
		"fenced FR-98\n" +
		"```\n" +
		"After the fence.\n" +
		"<!-- multi\n" +
		"line FR-97 comment -->\n" +
		"Tail text.\n"

	visible := visibleMarkdown(body)
	for _, hidden := range []string{"hidden FR-99", "## Not A Heading", "fenced FR-98", "line FR-97 comment"} {
		if strings.Contains(visible, hidden) {
			t.Errorf("visible rendering leaked %q:\n%s", hidden, visible)
		}
	}
	for _, shown := range []string{"Real text.", "After the fence.", "Tail text."} {
		if !strings.Contains(visible, shown) {
			t.Errorf("visible rendering dropped %q:\n%s", shown, visible)
		}
	}

	// Line count is preserved: diagnostics compute lines by counting
	// newlines in the visible text, so stripping must not shift them.
	if got, want := strings.Count(visible, "\n"), strings.Count(body, "\n"); got != want {
		t.Errorf("visible rendering has %d newlines, source has %d; line numbers would shift", got, want)
	}

	// noComments is the non-fence-aware form the citation body uses; it
	// strips comments wherever they appear, fenced or not.
	if strings.Contains(noComments(body), "hidden FR-99") {
		t.Error("noComments left an HTML comment's content in place")
	}

	// A tilde fence closes only on tildes, and a longer closer is still a
	// closer.
	tilde := "~~~\ninside\n~~~~\nafter\n"
	if v := visibleMarkdown(tilde); strings.Contains(v, "inside") || !strings.Contains(v, "after") {
		t.Errorf("tilde fence handling = %q", v)
	}
}

// TestPureSectionsAreDelimitedBySameDepthHeadings: a section's body runs to
// the next heading of the SAME depth, so a nested `###` belongs to its
// parent `##` and a fenced pseudo-heading never splits anything.
func TestPureSectionsAreDelimitedBySameDepthHeadings(t *testing.T) {
	a := pureArtifact(t, "Research/x.md", `---
title: Sample
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Context

Context body.

### Nested

Nested body.

## Findings

Findings body.

## Analysis

`+"```\n## Fenced Not A Section\n```"+`

## Open Questions

None.
`)
	secs := sections(a, 2)
	for _, want := range []string{"Context", "Findings", "Analysis", "Open Questions"} {
		if _, ok := secs[want]; !ok {
			t.Errorf("section %q missing; got %v", want, keysOfSections(secs))
		}
	}
	if _, leaked := secs["Fenced Not A Section"]; leaked {
		t.Error("a fenced pseudo-heading was indexed as a real section")
	}
	if _, leaked := secs["Nested"]; leaked {
		t.Error("a level-3 heading was indexed among level-2 sections")
	}
	if body := secs["Context"].Body; !strings.Contains(body, "Nested body.") {
		t.Errorf("the nested subsection was cut from its parent section: %q", body)
	}
	if body := secs["Findings"].Body; strings.Contains(body, "Context body.") {
		t.Errorf("section bodies bled together: %q", body)
	}
	if secs["Context"].Order >= secs["Findings"].Order {
		t.Error("Order does not follow document order; first-match consumers would pick the wrong heading")
	}
	if secs["Context"].Line >= secs["Findings"].Line {
		t.Error("section lines are not in document order")
	}

	// Level 3 sees the nested heading and nothing from level 2.
	if _, ok := sections(a, 3)["Nested"]; !ok {
		t.Error("level-3 lookup did not find the nested heading")
	}

	// headingBodies returns every occurrence, which is how a duplicated
	// completion-evidence section is detected.
	dup := "## Evidence\n\nfirst\n\n## Evidence\n\nsecond\n"
	if got := headingBodies(dup, 2, "Evidence"); len(got) != 2 {
		t.Errorf("headingBodies over a duplicated heading = %d sections, want 2", len(got))
	}
}

// TestPureSpecDefinedIDsPerFamily: the definition patterns decide what a
// spec or design is considered to define. A struck-through or commented id
// is deliberately NOT a definition — that is what makes retirement safe.
func TestPureSpecDefinedIDsPerFamily(t *testing.T) {
	spec := pureArtifact(t, "Specs/S/README.md", `---
title: S
type: spec
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Requirements

- **FR-01**: One.
- **FR-02**: Two.
- ~~**FR-03**~~: Retired.
<!-- - **FR-04**: Commented out. -->

## Acceptance Criteria

- [ ] **AC-01**: Unchecked.
- [x] **AC-02**: Checked.

## Constraints

- **NFR-01**: Fast.
`)
	defined := specDefinedIDs(spec)
	if !reflect.DeepEqual(sortedSet(defined["FR"]), []string{"FR-01", "FR-02"}) {
		t.Errorf("FR ids = %v, want [FR-01 FR-02] (struck-through and commented ids are not definitions)", sortedSet(defined["FR"]))
	}
	if !reflect.DeepEqual(sortedSet(defined["AC"]), []string{"AC-01", "AC-02"}) {
		t.Errorf("AC ids = %v, want both checkbox states", sortedSet(defined["AC"]))
	}
	if !reflect.DeepEqual(sortedSet(defined["NFR"]), []string{"NFR-01"}) {
		t.Errorf("NFR ids = %v", sortedSet(defined["NFR"]))
	}
	if len(defined["DD"]) != 0 {
		t.Errorf("a spec defines no DD ids, got %v", sortedSet(defined["DD"]))
	}

	// The exported view must agree with the internal index and be a copy,
	// so a caller cannot poison the memoized set.
	got := DefinedIdentifiers(spec, "FR")
	if !reflect.DeepEqual(sortedSet(got), []string{"FR-01", "FR-02"}) {
		t.Errorf("DefinedIdentifiers = %v", sortedSet(got))
	}
	got["FR-99"] = true
	if specDefinedIDs(spec)["FR"]["FR-99"] {
		t.Error("DefinedIdentifiers returned the live set; a caller mutated the memoized index")
	}

	// Designs write decisions as headings or bold bullets, with a letter
	// suffix allowed.
	design := pureArtifact(t, "Designs/D/README.md", `---
title: D
type: design
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Design Decisions

### DD-1 — Heading form

Text.

- **DD-2 — Bullet form**: Text.

#### DD-3a — Sub-decision

Text.
`)
	if want := []string{"DD-1", "DD-2", "DD-3a"}; !reflect.DeepEqual(sortedSet(specDefinedIDs(design)["DD"]), want) {
		t.Errorf("DD ids = %v, want %v", sortedSet(specDefinedIDs(design)["DD"]), want)
	}

	if got := IdentifierFamilies(); !reflect.DeepEqual(got, []string{"FR", "NFR", "AC", "DD"}) {
		t.Errorf("IdentifierFamilies() = %v", got)
	}
}

// TestPureQualifiedCitationRequiresColon: only `Artifact:DD-4` names another
// artifact's identifier. The space-separated form is prose and must stay
// unqualified, or ordinary sentences ending in a capitalized word would
// silently excuse dangling citations.
func TestPureQualifiedCitationRequiresColon(t *testing.T) {
	for _, tc := range []struct {
		text          string
		wantQualifier string
		wantQualified bool
	}{
		{"ArkBootstrapApi:DD-4", "ArkBootstrapApi", true},
		// A directory-qualified citation yields its BASENAME: the index
		// registers `M1:AC-01` alongside the full directory spelling.
		{"See Specs/M1:AC-01 for detail", "M1", true},
		{"ReleaseControlService FR-08", "", false},
		{"Bare FR-01 citation", "", false},
		{"FR-01", "", false},
		{"lowercase:DD-4", "", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			start := firstCitationStart(tc.text)
			if start < 0 {
				t.Fatalf("no citation found in %q", tc.text)
			}
			qualifier, qualified := citationQualifier(tc.text, start)
			if qualified != tc.wantQualified {
				t.Errorf("qualified = %v, want %v", qualified, tc.wantQualified)
			}
			if qualifier != tc.wantQualifier {
				t.Errorf("qualifier = %q, want %q", qualifier, tc.wantQualifier)
			}
			if got := qualifiedCitation(tc.text, start); got != tc.wantQualified {
				t.Errorf("qualifiedCitation = %v, want %v", got, tc.wantQualified)
			}
		})
	}
}

// TestPureSortDiagnosticsOrdersByPathLineCodeMessage: the report order is
// part of the output contract — consumers and the frozen corpus compare
// against it — so it is total and stable across all four keys.
func TestPureSortDiagnosticsOrdersByPathLineCodeMessage(t *testing.T) {
	in := []Diagnostic{
		{Code: "SDD020", Path: "b.md", Line: 1, Message: "b"},
		{Code: "SDD020", Path: "a.md", Line: 9, Message: "later line"},
		{Code: "SDD030", Path: "a.md", Line: 2, Message: "z"},
		{Code: "SDD010", Path: "a.md", Line: 2, Message: "a"},
		{Code: "SDD010", Path: "a.md", Line: 2, Message: "b"},
		{Code: "SDD010", Path: "a.md", Line: 1, Message: "first"},
	}
	want := []string{
		"a.md:1:SDD010:first",
		"a.md:2:SDD010:a",
		"a.md:2:SDD010:b",
		"a.md:2:SDD030:z",
		"a.md:9:SDD020:later line",
		"b.md:1:SDD020:b",
	}

	sorted := append([]Diagnostic(nil), in...)
	SortDiagnostics(sorted)
	if got := diagnosticKeys(sorted); !reflect.DeepEqual(got, want) {
		t.Errorf("SortDiagnostics order:\n got %v\nwant %v", got, want)
	}

	// sortStrict is the strict-mode spelling of the same order.
	strict := append([]Diagnostic(nil), in...)
	sortStrict(strict)
	if got := diagnosticKeys(strict); !reflect.DeepEqual(got, want) {
		t.Errorf("sortStrict order:\n got %v\nwant %v", got, want)
	}

	// Sorting an already-sorted list must be a no-op, and sorting is
	// independent of input order.
	again := append([]Diagnostic(nil), sorted...)
	SortDiagnostics(again)
	if !reflect.DeepEqual(diagnosticKeys(again), want) {
		t.Error("SortDiagnostics is not idempotent")
	}
	reversed := make([]Diagnostic, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		reversed = append(reversed, in[i])
	}
	SortDiagnostics(reversed)
	if !reflect.DeepEqual(diagnosticKeys(reversed), want) {
		t.Error("SortDiagnostics depends on input order")
	}
}

// TestPureStringDuplicatesAreSortedAndUnique: duplicate-id reporting must
// be deterministic, so the helper behind it returns each repeated value
// once, in sorted order, regardless of how often it repeats.
func TestPureStringDuplicatesAreSortedAndUnique(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{"none", []string{"a", "b", "c"}, nil},
		{"empty", nil, nil},
		{"one-repeat", []string{"b", "a", "b"}, []string{"b"}},
		{"repeated-thrice-reported-once", []string{"a", "a", "a"}, []string{"a"}},
		{"sorted-not-first-seen", []string{"z", "z", "a", "a"}, []string{"a", "z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := stringDuplicates(tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("stringDuplicates(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestPureWaiversParseWellFormedEntries: a well-formed waiver parses into
// its code, reason and declaration line, with the code normalized so
// `sdd020` and `SDD020` are the same exception.
func TestPureWaiversParseWellFormedEntries(t *testing.T) {
	a := pureArtifact(t, "Research/x.md", `---
title: Sample
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
waivers:
  - code: sdd020
    reason: "This research artifact predates the section convention."
    accepted: "2024-02-01"
---

## Context

Text.
`)
	ws := Waivers(a)
	if len(ws) != 1 {
		t.Fatalf("parsed %d waivers, want 1", len(ws))
	}
	w := ws[0]
	if w.Invalid != "" {
		t.Errorf("well-formed waiver rejected: %s", w.Invalid)
	}
	if w.Code != "SDD020" {
		t.Errorf("Code = %q, want the upper-cased SDD020", w.Code)
	}
	if !strings.HasPrefix(w.Reason, "This research artifact") {
		t.Errorf("Reason = %q", w.Reason)
	}
	if w.Accepted != "2024-02-01" {
		t.Errorf("Accepted = %q", w.Accepted)
	}
	if w.Line <= 1 {
		t.Errorf("Line = %d, want the waiver's own declaration line", w.Line)
	}
	if w.Used {
		t.Error("a freshly parsed waiver must not be marked Used")
	}

	// No waivers block at all is nil, not an error.
	if got := Waivers(pureArtifact(t, "Research/y.md", validResearch)); got != nil {
		t.Errorf("Waivers on an artifact with no block = %v, want nil", got)
	}
}

// TestPureWaiverValidationRejects: a waiver must be evaluable. Every shape
// that is not — unknown code, a parse-stage code no rule below it ran for,
// a placeholder reason, a reason too short to carry an argument — is
// returned carrying Invalid rather than silently dropped, because a
// vanished exception is exactly what the mechanism exists to prevent.
func TestPureWaiverValidationRejects(t *testing.T) {
	waiverDoc := func(entry string) string {
		return `---
title: Sample
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
waivers:
` + entry + `---

## Context

Text.
`
	}
	for _, tc := range []struct {
		name     string
		entry    string
		wantWord string
	}{
		{"unknown-code", "  - code: SDD999\n    reason: \"A reason long enough to count.\"\n", "not a diagnostic code"},
		{"no-code", "  - reason: \"A reason long enough to count.\"\n", "declares no `code`"},
		{"unwaivable-parse-code", "  - code: SDD006\n    reason: \"The frontmatter is intentionally malformed.\"\n", "cannot be waived"},
		{"placeholder-reason", "  - code: SDD020\n    reason: \"TBD\"\n", "states no reason"},
		{"empty-reason", "  - code: SDD020\n    reason: \"\"\n", "states no reason"},
		{"reason-too-short", "  - code: SDD020\n    reason: \"known issue\"\n", "too short"},
		{"not-a-mapping", "  - just a string\n", "not a mapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := Waivers(pureArtifact(t, "Research/x.md", waiverDoc(tc.entry)))
			if len(ws) != 1 {
				t.Fatalf("parsed %d waivers, want 1", len(ws))
			}
			if ws[0].Invalid == "" {
				t.Fatalf("waiver was accepted; want it rejected as %s", tc.wantWord)
			}
			if !strings.Contains(ws[0].Invalid, tc.wantWord) {
				t.Errorf("Invalid = %q, want it to mention %q", ws[0].Invalid, tc.wantWord)
			}
		})
	}

	// A `waivers` value that is not a list is one invalid entry, not a
	// silent no-op.
	notAList := Waivers(pureArtifact(t, "Research/x.md", strings.Replace(
		validResearch, "related: []", "related: []\nwaivers: nonsense", 1)))
	if len(notAList) != 1 || notAList[0].Invalid == "" {
		t.Errorf("non-list waivers = %#v, want one entry carrying Invalid", notAList)
	}

	// Every parse-stage code is unwaivable, for the same reason: no rule
	// below it ran.
	for code := range unwaivableCodes {
		ws := Waivers(pureArtifact(t, "Research/x.md", waiverDoc(
			"  - code: "+code+"\n    reason: \"A reason long enough to count as one.\"\n")))
		if len(ws) != 1 || !strings.Contains(ws[0].Invalid, "cannot be waived") {
			t.Errorf("%s: waiver = %#v, want it refused as unwaivable", code, ws)
		}
	}
}

// TestPureApplyWaiversExcusesOnlyMatchingArtifact: a waiver's blast radius
// is its own artifact and its own code. It re-tags a matching error as
// Waived, carrying the rationale, and leaves everything else alone —
// including the same code on a different file and a Candidate that was
// never invalidating.
func TestPureApplyWaiversExcusesOnlyMatchingArtifact(t *testing.T) {
	const reason = "This artifact predates the section convention and is frozen."
	root := pureRoot(t, map[string]string{
		"Research/waived.md": strings.Replace(validResearch, "related: []",
			"related: []\nwaivers:\n  - code: SDD020\n    reason: \""+reason+"\"", 1),
		"Research/other.md": validResearch,
	})

	diags := []Diagnostic{
		{Code: "SDD020", Severity: Error, Path: "Research/waived.md", Line: 1, Message: "matched"},
		{Code: "SDD020", Severity: Error, Path: "Research/other.md", Line: 1, Message: "other artifact"},
		{Code: "SDD030", Severity: Error, Path: "Research/waived.md", Line: 1, Message: "other code"},
		{Code: "SDD020", Severity: Candidate, Path: "Research/waived.md", Line: 2, Message: "candidate"},
	}
	book := applyWaivers(root, diags)

	if diags[0].Severity != Waived {
		t.Errorf("the matching finding is %q, want Waived", diags[0].Severity)
	}
	if diags[0].WaivedReason != reason {
		t.Errorf("WaivedReason = %q, want the declared rationale", diags[0].WaivedReason)
	}
	if diags[1].Severity != Error {
		t.Error("a waiver reached another artifact's finding of the same code")
	}
	if diags[2].Severity != Error {
		t.Error("a waiver excused a different code on its own artifact")
	}
	if diags[3].Severity != Candidate {
		t.Error("a waiver re-tagged a Candidate, which was never invalidating")
	}

	// The matched waiver is used, so no staleness finding is booked.
	for _, d := range book {
		if d.Code == "SDD177" {
			t.Errorf("a waiver that matched was reported stale: %s", d.Message)
		}
	}

	// A waiver matching nothing books SDD177; the artifact stays reported.
	stale := pureRoot(t, map[string]string{
		"Research/stale.md": strings.Replace(validResearch, "related: []",
			"related: []\nwaivers:\n  - code: SDD020\n    reason: \""+reason+"\"", 1),
	})
	book = applyWaivers(stale, []Diagnostic{
		{Code: "SDD030", Severity: Error, Path: "Research/stale.md", Line: 1, Message: "unrelated"},
	})
	if !hasCode(book, "SDD177") {
		t.Errorf("an unmatched waiver booked %v, want SDD177", codesOf(book))
	}

	// A malformed waiver books SDD176 and excuses nothing.
	malformed := pureRoot(t, map[string]string{
		"Research/bad.md": strings.Replace(validResearch, "related: []",
			"related: []\nwaivers:\n  - code: SDD020\n    reason: \"TBD\"", 1),
	})
	target := []Diagnostic{{Code: "SDD020", Severity: Error, Path: "Research/bad.md", Line: 1, Message: "still an error"}}
	book = applyWaivers(malformed, target)
	if !hasCode(book, "SDD176") {
		t.Errorf("a malformed waiver booked %v, want SDD176", codesOf(book))
	}
	if target[0].Severity != Error {
		t.Error("a malformed waiver excused a finding anyway")
	}
}

// TestPureDemoteRetiredFindingsKeepsSupersessionErrors: findings on an
// archived or superseded artifact stop blocking, because holding history to
// the current schema means a root can never go green. The two exclusions
// are what keep that from being a loophole — the supersession rules
// themselves stay errors, or retirement metadata could never be corrected.
func TestPureDemoteRetiredFindingsKeepsSupersessionErrors(t *testing.T) {
	archived := strings.Replace(validResearch, "status: draft", "status: archived", 1)
	superseded := strings.Replace(
		strings.Replace(validSpecTemplate, "status: draft", "status: superseded", 1),
		"related: []", "related: []\nsuperseded_by: Specs/New", 1)

	root := pureRoot(t, map[string]string{
		"Research/archived.md": archived,
		"Specs/Old/README.md":  superseded,
		"Research/live.md":     validResearch,
	})

	diags := []Diagnostic{
		{Code: "SDD020", Severity: Error, Path: "Research/archived.md", Line: 1, Message: "on archived"},
		{Code: "SDD020", Severity: Error, Path: "Specs/Old/README.md", Line: 1, Message: "on superseded"},
		{Code: "SDD178", Severity: Error, Path: "Specs/Old/README.md", Line: 1, Message: "supersession metadata"},
		{Code: "SDD179", Severity: Error, Path: "Specs/Old/README.md", Line: 1, Message: "supersession target"},
		{Code: "SDD020", Severity: Error, Path: "Research/live.md", Line: 1, Message: "on live work"},
		{Code: "SDD020", Severity: Candidate, Path: "Research/archived.md", Line: 2, Message: "advisory"},
	}
	out := demoteRetiredFindings(root, diags)

	if out[0].Severity != Waived || out[0].WaivedReason == "" {
		t.Errorf("finding on an archived artifact = %q/%q, want Waived with a reason", out[0].Severity, out[0].WaivedReason)
	}
	if out[1].Severity != Waived {
		t.Errorf("finding on a superseded artifact = %q, want Waived", out[1].Severity)
	}
	for _, i := range []int{2, 3} {
		if out[i].Severity != Error {
			t.Errorf("%s was demoted on a retired artifact; the supersession family must stay enforceable", out[i].Code)
		}
	}
	if out[4].Severity != Error {
		t.Error("a finding on live work was demoted")
	}
	if out[5].Severity != Candidate {
		t.Error("a Candidate was re-tagged")
	}

	// With nothing retired, the list is returned untouched.
	live := pureRoot(t, map[string]string{"Research/live.md": validResearch})
	untouched := []Diagnostic{{Code: "SDD020", Severity: Error, Path: "Research/live.md", Line: 1}}
	if got := demoteRetiredFindings(live, untouched); got[0].Severity != Error {
		t.Error("demotion ran on a root with no retired artifact")
	}
}

// TestPureEvaluateRunsEachRuleOncePerScope: the ordinary sweep is one root
// callback per rule and one artifact callback per (rule, artifact). Double
// evaluation would double every finding, and the waiver-bookkeeping codes
// must never sweep or they would recurse into the sweep that derives them.
func TestPureEvaluateRunsEachRuleOncePerScope(t *testing.T) {
	root := pureRoot(t, map[string]string{
		"Research/one.md": validResearch,
		"Research/two.md": validResearch,
	})
	if len(root.Artifacts) != 2 {
		t.Fatalf("loaded %d artifacts, want 2", len(root.Artifacts))
	}

	var rootCalls, artifactCalls, waiverRootCalls int
	counting := &Rule{
		Code: "SDD901", Severity: Error,
		CheckRoot: func(*Root, func(Diagnostic)) { rootCalls++ },
		Check:     func(*Artifact, func(Diagnostic)) { artifactCalls++ },
	}
	bookkeeping := &Rule{
		Code: "SDD176", Severity: Error,
		CheckRoot: func(*Root, func(Diagnostic)) { waiverRootCalls++ },
	}

	diags, err := evaluate(root, []*Rule{counting, bookkeeping})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if rootCalls != 1 {
		t.Errorf("root callback ran %d times, want 1", rootCalls)
	}
	if artifactCalls != len(root.Artifacts) {
		t.Errorf("artifact callback ran %d times, want %d (once per artifact)", artifactCalls, len(root.Artifacts))
	}
	if waiverRootCalls != 0 {
		t.Errorf("a waiver-bookkeeping rule swept %d times; it must derive from the sweep, not join it", waiverRootCalls)
	}
	if len(diags) != 0 {
		t.Errorf("counting rules emitted %v", codesOf(diags))
	}

	// Emissions are collected in rule order for root rules, then per
	// artifact in discovery order.
	emitting := &Rule{
		Code: "SDD902", Severity: Error,
		Check: func(a *Artifact, emit func(Diagnostic)) {
			emit(Diagnostic{Code: "SDD902", Severity: Error, Path: a.Rel, Line: 1, Message: "per artifact"})
		},
	}
	diags, err = evaluate(root, []*Rule{emitting})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(diags) != len(root.Artifacts) {
		t.Fatalf("emitted %d diagnostics, want one per artifact (%d)", len(diags), len(root.Artifacts))
	}
	for i, d := range diags {
		if d.Path != root.Artifacts[i].Rel {
			t.Errorf("diagnostic %d is for %s, want discovery order (%s)", i, d.Path, root.Artifacts[i].Rel)
		}
	}
}

// TestPureEvaluateOverPureRulesNeedsNoRepository: a rule list of purely
// content-reading rules evaluates over a plain directory that was never
// initialized as a repository, and the strict and reporting modes differ
// exactly as documented — strict ignores accepted exceptions, reporting
// applies them.
func TestPureEvaluateOverPureRulesNeedsNoRepository(t *testing.T) {
	const reason = "This artifact predates the section convention and is frozen."
	missingSection := strings.Replace(validResearch, "## Analysis\n\nText.\n\n", "", 1)
	root := pureRoot(t, map[string]string{
		"Research/bad.md": strings.Replace(missingSection, "related: []",
			"related: []\nwaivers:\n  - code: SDD020\n    reason: \""+reason+"\"", 1),
	})

	// SDD020 (required sections) and SDD010 (required fields) read the
	// artifact and nothing else; SDD176/177 derive from that same sweep.
	pure := []*Rule{
		ruleByCode(t, "SDD010"),
		ruleByCode(t, "SDD020"),
		ruleByCode(t, "SDD176"),
		ruleByCode(t, "SDD177"),
	}

	strict, err := runWith(root, pure)
	if err != nil {
		t.Fatalf("strict evaluation over a plain directory: %v", err)
	}
	if !hasCode(strict, "SDD020") {
		t.Fatalf("strict mode reported %v, want SDD020 on a document missing a section", codesOf(strict))
	}
	for _, d := range strict {
		if d.Code == "SDD020" && d.Severity != Error {
			t.Errorf("strict mode applied an accepted exception: SDD020 is %q", d.Severity)
		}
	}

	reporting, err := runWithWaiversWith(freshRoot(t, root.Dir), pure)
	if err != nil {
		t.Fatalf("reporting evaluation: %v", err)
	}
	var sawWaived bool
	for _, d := range reporting {
		if d.Code != "SDD020" {
			continue
		}
		if d.Severity != Waived {
			t.Errorf("reporting mode left SDD020 at %q, want Waived", d.Severity)
			continue
		}
		sawWaived = true
		if d.WaivedReason != reason {
			t.Errorf("WaivedReason = %q, want the declared rationale", d.WaivedReason)
		}
	}
	if !sawWaived {
		t.Errorf("reporting mode reported %v, want a Waived SDD020", codesOf(reporting))
	}

	// A waived finding is still reported: excused is not the same as gone.
	if len(reporting) == 0 {
		t.Error("the waived finding vanished from the report")
	}

	// Output is sorted, in both modes.
	if got := diagnosticKeys(strict); !isSortedKeys(got) {
		t.Errorf("strict output is unsorted: %v", got)
	}
	if got := diagnosticKeys(reporting); !isSortedKeys(got) {
		t.Errorf("reporting output is unsorted: %v", got)
	}
}

// --- helpers, pure by construction -------------------------------------

func keysOfSections(m map[string]sectionInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func diagnosticKeys(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Path+":"+itoa(d.Line)+":"+d.Code+":"+d.Message)
	}
	return out
}

func isSortedKeys(keys []string) bool {
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			return false
		}
	}
	return true
}

// firstCitationStart returns the byte offset of the first FR/NFR/AC/DD
// identifier in text, or -1.
func firstCitationStart(text string) int {
	best := -1
	for _, prefix := range []string{"FR-", "NFR-", "AC-", "DD-"} {
		if i := strings.Index(text, prefix); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}
