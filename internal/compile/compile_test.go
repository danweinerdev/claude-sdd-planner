package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
)

func load(t *testing.T) *schema.Schema {
	t.Helper()
	s, err := schema.Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// payload builds a minimally valid spec proposal, with overrides applied to
// named sections.
func payload(over map[string]string) string {
	sec := map[string]string{
		"## Overview":                     "A thing.",
		"## Goals":                        "- do it",
		"## Non-Goals":                    "- not that",
		"## Requirements":                 "",
		"### Functional Requirements":     "- **FR-01**: the thing works",
		"### Non-Functional Requirements": "- **NFR-01**: it is fast",
		"## User Stories":                 "- As a user, I want it.",
		"## Acceptance Criteria":          "- [ ] **AC-01**: it works",
		"## Constraints":                  "- none",
		"## Dependencies":                 "- none",
	}
	order := []string{
		"## Overview", "## Goals", "## Non-Goals", "## Requirements",
		"### Functional Requirements", "### Non-Functional Requirements",
		"## User Stories", "## Acceptance Criteria", "## Constraints", "## Dependencies",
	}
	var b strings.Builder
	b.WriteString("---\ntitle: \"Thing\"\ntags: [x]\nrelated: []\n---\n\n# Thing\n\n")
	for _, h := range order {
		v := sec[h]
		if o, ok := over[h]; ok {
			v = o
		}
		if v == "SKIP" {
			continue
		}
		b.WriteString(h + "\n" + v + "\n\n")
	}
	for h, v := range over {
		if _, known := sec[h]; !known && v != "SKIP" {
			b.WriteString(h + "\n" + v + "\n\n")
		}
	}
	return b.String()
}

func compileNew(t *testing.T, p string) *Result {
	t.Helper()
	return Compile(load(t), p, Options{Today: "2026-08-04"})
}

func mustOK(t *testing.T, r *Result) {
	t.Helper()
	if !r.OK() {
		for _, ref := range r.Refusals {
			t.Logf("refusal: %s", ref)
		}
		t.Fatal("compile refused, want success")
	}
}

func codes(r *Result) []string {
	var out []string
	for _, ref := range r.Refusals {
		out = append(out, ref.Code)
	}
	return out
}

func hasCode(r *Result, code string) bool {
	for _, c := range codes(r) {
		if c == code {
			return true
		}
	}
	return false
}

func TestCompileMinimalSpec(t *testing.T) {
	r := compileNew(t, payload(nil))
	mustOK(t, r)
	for _, want := range []string{"type: spec", "status: draft", "updated: 2026-08-04", "# Thing", "## Overview"} {
		if !strings.Contains(r.Output, want) {
			t.Errorf("output missing %q\n---\n%s", want, r.Output)
		}
	}
}

// FR-24: emission is byte-idempotent. Compiling the tool's own output must
// reproduce it exactly and report no corrections.
//
// The second compile passes Existing, because re-applying a canonical artifact
// is a *revision*, not a creation. Without it the payload's tool-owned
// frontmatter has nothing to be verified against and FR-18 correctly refuses —
// which is the contract, not a bug.
func TestByteIdempotence(t *testing.T) {
	first := compileNew(t, payload(nil))
	mustOK(t, first)
	second := Compile(load(t), first.Output, Options{
		Today:    "2026-08-04",
		Existing: artifact.Parse(first.Output),
	})
	mustOK(t, second)
	if second.Output != first.Output {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first.Output, second.Output)
	}
	if len(second.Corrections) != 0 {
		t.Errorf("re-compiling normalized output reported corrections: %v", second.Corrections)
	}
}

// FR-19 auto-corrections: normalize where intent is unambiguous.
func TestNearMissAutoCorrections(t *testing.T) {
	cases := []struct {
		name, heading, wantFrag string
	}{
		{"lowercase", "## overview", "case"},
		{"trailing colon", "## Overview:", "decoration"},
		{"bold instead of heading", "## **Overview**", "decoration"},
		{"depth off by one", "### Overview", "depth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := strings.Replace(payload(nil), "## Overview", tc.heading, 1)
			r := compileNew(t, p)
			mustOK(t, r)
			joined := strings.Join(r.Corrections, "; ")
			if !strings.Contains(joined, tc.wantFrag) {
				t.Errorf("corrections = %q, want one mentioning %q", joined, tc.wantFrag)
			}
			if !strings.Contains(r.Output, "## Overview\n") {
				t.Errorf("output did not canonicalize the heading:\n%s", r.Output)
			}
		})
	}
}

func TestListMarkerNormalized(t *testing.T) {
	r := compileNew(t, payload(map[string]string{"## Goals": "* starred item"}))
	mustOK(t, r)
	if !strings.Contains(r.Output, "- starred item") {
		t.Errorf("list marker not normalized:\n%s", r.Output)
	}
}

// FR-19 refusals: refuse where intent is ambiguous, and report every violation
// in one result rather than one at a time.
func TestRefusesMissingRequiredSection(t *testing.T) {
	r := compileNew(t, payload(map[string]string{"## Constraints": "SKIP"}))
	if r.OK() {
		t.Fatal("compile succeeded with a required section absent")
	}
	if !hasCode(r, "SPK012") {
		t.Errorf("codes = %v, want SPK012", codes(r))
	}
	if !strings.Contains(r.Refusals[0].Message, "## Constraints") {
		t.Errorf("refusal does not name the section: %s", r.Refusals[0].Message)
	}
}

func TestRefusesDuplicateSlot(t *testing.T) {
	p := payload(nil) + "\n## Overview\nsecond one\n"
	r := compileNew(t, p)
	if r.OK() {
		t.Fatal("compile succeeded with two sections mapping to one slot")
	}
	if !hasCode(r, "SPK011") {
		t.Errorf("codes = %v, want SPK011", codes(r))
	}
}

func TestReportsAllViolationsAtOnce(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Constraints":  "SKIP",
		"## Dependencies": "SKIP",
		"## User Stories": "SKIP",
	}))
	if r.OK() {
		t.Fatal("compile succeeded")
	}
	if len(r.Refusals) < 3 {
		t.Errorf("got %d refusals, want at least 3 reported together: %v", len(r.Refusals), codes(r))
	}
}

// A heading more than one level off its slot is not an unambiguous near miss.
func TestDepthTwoLevelsOffIsNotCorrected(t *testing.T) {
	p := strings.Replace(payload(nil), "## Overview", "#### Overview", 1)
	r := compileNew(t, p)
	if r.OK() {
		t.Fatal("compile succeeded; a two-level depth error should not auto-correct")
	}
	if !hasCode(r, "SPK012") {
		t.Errorf("codes = %v, want SPK012 (required section absent)", codes(r))
	}
}

// FR-18: a tool-owned frontmatter field in the payload is an error naming the
// owning subcommand, neither honored nor silently dropped.
func TestRefusesToolOwnedFrontmatter(t *testing.T) {
	for _, key := range []string{"status", "updated", "created", "type"} {
		t.Run(key, func(t *testing.T) {
			p := strings.Replace(payload(nil), "tags: [x]", key+": whatever\ntags: [x]", 1)
			r := compileNew(t, p)
			if r.OK() {
				t.Fatalf("compile succeeded with tool-owned %q in the payload", key)
			}
			if !hasCode(r, "SPK021") {
				t.Errorf("codes = %v, want SPK021", codes(r))
			}
			if !strings.Contains(r.Refusals[0].Message, key) {
				t.Errorf("refusal does not name %q: %s", key, r.Refusals[0].Message)
			}
			if r.Refusals[0].Correction == "" {
				t.Error("refusal has no correction; FR-29 requires naming the correct form")
			}
		})
	}
}

func TestRefusesMissingRequiredFrontmatter(t *testing.T) {
	p := strings.Replace(payload(nil), "tags: [x]\n", "", 1)
	r := compileNew(t, p)
	if r.OK() {
		t.Fatal("compile succeeded without a required author field")
	}
	if !hasCode(r, "SPK022") {
		t.Errorf("codes = %v, want SPK022", codes(r))
	}
}

// FR-20: an item with no identifier is new and gets the next value above the
// high-water mark.
func TestAllocatesIdentifierForNewItem(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- a brand new requirement",
	}))
	mustOK(t, r)
	if len(r.Allocations) != 1 {
		t.Fatalf("allocations = %v, want exactly 1", r.Allocations)
	}
	if !strings.Contains(r.Allocations[0], "FR-02") {
		t.Errorf("allocated %q, want FR-02", r.Allocations[0])
	}
	if !strings.Contains(r.Output, "**FR-02**: a brand new requirement") {
		t.Errorf("output missing the allocated identifier:\n%s", r.Output)
	}
}

func TestAllocatesCheckboxItemInPlace(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Acceptance Criteria": "- [ ] **AC-01**: one\n- [ ] a new criterion",
	}))
	mustOK(t, r)
	if !strings.Contains(r.Output, "- [ ] **AC-02**: a new criterion") {
		t.Errorf("checkbox allocation malformed:\n%s", r.Output)
	}
}

func TestIndentedItemsAreNotAllocated(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n  - a subpoint, not a requirement",
	}))
	mustOK(t, r)
	if len(r.Allocations) != 0 {
		t.Errorf("allocations = %v, want none for an indented subpoint", r.Allocations)
	}
}

// --- FR-45 round-trip contract ---

func existing(t *testing.T) *artifact.Doc {
	t.Helper()
	r := compileNew(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- **FR-02**: second",
	}))
	mustOK(t, r)
	return artifact.Parse(r.Output)
}

func revise(t *testing.T, p string, retire ...string) *Result {
	t.Helper()
	rm := map[string]bool{}
	for _, id := range retire {
		rm[id] = true
	}
	return Compile(load(t), p, Options{Today: "2026-08-04", Existing: existing(t), Retire: rm})
}

func TestRoundTripUnchangedPreservesIdentifiers(t *testing.T) {
	base := compileNew(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- **FR-02**: second",
	}))
	mustOK(t, base)
	r := revise(t, base.Output)
	mustOK(t, r)
	if r.Output != base.Output {
		t.Errorf("round-trip changed bytes:\n--- was ---\n%s\n--- now ---\n%s", base.Output, r.Output)
	}
	if len(r.Allocations) != 0 {
		t.Errorf("round-trip allocated identifiers: %v", r.Allocations)
	}
}

func TestRoundTripEditKeepsIdentifier(t *testing.T) {
	r := revise(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first, reworded\n- **FR-02**: second",
	}))
	mustOK(t, r)
	if !strings.Contains(r.Output, "**FR-01**: first, reworded") {
		t.Errorf("edit did not land:\n%s", r.Output)
	}
	if len(r.Allocations) != 0 {
		t.Errorf("editing prose allocated a new identifier: %v", r.Allocations)
	}
}

func TestRoundTripRefusesUnknownIdentifier(t *testing.T) {
	r := revise(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- **FR-02**: second\n- **FR-77**: invented",
	}))
	if r.OK() {
		t.Fatal("compile succeeded with an invented identifier")
	}
	if !hasCode(r, "SPK030") {
		t.Errorf("codes = %v, want SPK030", codes(r))
	}
	var found bool
	for _, ref := range r.Refusals {
		if ref.Code == "SPK030" && strings.Contains(ref.Correction, "FR-01") && strings.Contains(ref.Correction, "FR-02") {
			found = true
		}
	}
	if !found {
		t.Error("refusal does not list the live identifiers; FR-29 requires it")
	}
}

func TestRoundTripRefusesUnintendedRetirement(t *testing.T) {
	r := revise(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first",
	}))
	if r.OK() {
		t.Fatal("compile succeeded while silently dropping FR-02")
	}
	if !hasCode(r, "SPK031") {
		t.Errorf("codes = %v, want SPK031", codes(r))
	}
	if !strings.Contains(r.Refusals[0].Correction, "--retire FR-02") {
		t.Errorf("refusal does not name the remedy: %s", r.Refusals[0].Correction)
	}
}

func TestRoundTripExplicitRetireSucceeds(t *testing.T) {
	r := revise(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first",
	}), "FR-02")
	mustOK(t, r)
	if len(r.Retired) != 1 || r.Retired[0] != "FR-02" {
		t.Errorf("Retired = %v, want [FR-02]", r.Retired)
	}
}

// FR-20: allocation never fills a gap left by a retirement.
func TestAllocationDoesNotReissueRetiredValue(t *testing.T) {
	retired := compileNew(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- ~~**FR-02**~~: retired — removed",
	}))
	mustOK(t, retired)
	r := Compile(load(t), payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- ~~**FR-02**~~: retired — removed\n- a new one",
	}), Options{Today: "2026-08-04", Existing: artifact.Parse(retired.Output)})
	mustOK(t, r)
	if len(r.Allocations) != 1 || !strings.Contains(r.Allocations[0], "FR-03") {
		t.Errorf("allocations = %v, want FR-03 (must not reissue FR-02)", r.Allocations)
	}
}

// --- FR-23 identifier resolution and its three exemptions ---

func TestRefusesUnresolvableCitation(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Constraints": "- bounded by FR-99, which does not exist",
	}))
	if r.OK() {
		t.Fatal("compile succeeded citing a nonexistent identifier")
	}
	if !hasCode(r, "SPK040") {
		t.Errorf("codes = %v, want SPK040", codes(r))
	}
	if !strings.Contains(r.Refusals[0].Correction, "FR-01") {
		t.Errorf("refusal does not list available identifiers: %s", r.Refusals[0].Correction)
	}
}

// A qualified citation names another artifact's identifier and is exempt
// from local resolution — previously `ProductSystemV2 FR-23` was
// indistinguishable from a dangling local citation and had to be
// backtick-escaped, dropping it from any link graph.
func TestQualifiedCitationExemptsExternalReference(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Constraints": "- must honor ProductSystemV2:FR-99 from the upstream spec",
	}))
	mustOK(t, r)
}

// A colon alone is not a qualifier: `: FR-99` and a line-leading token still
// resolve locally, so prose punctuation cannot smuggle a dangling citation.
func TestBareColonDoesNotQualifyCitation(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Constraints": "- see the following: FR-99 stays a local citation",
	}))
	if r.OK() || !hasCode(r, "SPK040") {
		t.Errorf("codes = %v, want SPK040 for an unqualified dangling citation", codes(r))
	}
}

func TestCodeSpanExemptsCitation(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Constraints": "- a hypothetical `FR-99` is a literal, not a citation",
	}))
	mustOK(t, r)
}

func TestFencedBlockExemptsCitation(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Constraints": "- see below\n\n```\nFR-99 appears only in a fence\n```",
	}))
	mustOK(t, r)
}

func TestFreeProseSectionExemptFromResolution(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Open Questions": "- could become FR-99 later — **non-blocking** — nothing depends on it",
	}))
	mustOK(t, r)
}

func TestRetiredIdentifierResolvesInProse(t *testing.T) {
	base := compileNew(t, payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- ~~**FR-02**~~: retired — superseded",
	}))
	mustOK(t, base)
	r := Compile(load(t), payload(map[string]string{
		"### Functional Requirements": "- **FR-01**: first\n- ~~**FR-02**~~: retired — superseded",
		"## Constraints":              "- supersedes the approach FR-02 described",
	}), Options{Today: "2026-08-04", Existing: artifact.Parse(base.Output)})
	mustOK(t, r)
}

func TestResolvesLiveCitation(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Constraints": "- bounded by FR-01 and AC-01",
	}))
	mustOK(t, r)
}

// Prose is otherwise unexamined: structurally valid nonsense compiles.
func TestProseIsOpaque(t *testing.T) {
	r := compileNew(t, payload(map[string]string{
		"## Overview": "Colorless green ideas sleep furiously; the moon is a hexagon.",
	}))
	mustOK(t, r)
	if !strings.Contains(r.Output, "hexagon") {
		t.Error("opaque prose did not survive compilation")
	}
}

// Additional sections are retained in place and reported, not refused, because
// the spec schema declares additionalSections=allowed.
func TestAdditionalSectionRetainedAndReported(t *testing.T) {
	r := compileNew(t, payload(map[string]string{"## Appendix": "extra material"}))
	mustOK(t, r)
	if len(r.Notes) == 0 {
		t.Error("additional section was not reported")
	}
	if !strings.Contains(r.Output, "## Appendix") {
		t.Errorf("additional section was dropped:\n%s", r.Output)
	}
}

func TestCRLFAndBOMReportedAsCorrections(t *testing.T) {
	p := "\ufeff" + strings.ReplaceAll(payload(nil), "\n", "\r\n")
	r := compileNew(t, p)
	mustOK(t, r)
	joined := strings.Join(r.Corrections, "; ")
	for _, want := range []string{"BOM", "CRLF"} {
		if !strings.Contains(joined, want) {
			t.Errorf("corrections = %q, want one mentioning %q", joined, want)
		}
	}
	if strings.Contains(r.Output, "\r") {
		t.Error("output contains CR; NFR-05 requires LF on every platform")
	}
}
