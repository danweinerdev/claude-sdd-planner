package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// exampleRoot materializes an example's files and loads the root without
// running the rules.
func exampleRoot(t *testing.T, ex Example) *Root {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range ex.Files {
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

// findByCode returns the diagnostics of one code, sorted by message.
func findByCode(diags []Diagnostic, code string) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Message < out[j].Message })
	return out
}

// TestSDD161ScopesToRealizedSpecs pins the cross-spec verdicts: two related
// specs share FR-01/NFR-01/AC-01, and the design declares it realizes only
// the second. Design-side coverage demands nothing for the first spec, the
// design's bare ids resolve to the second, and no other traceability or
// citation rule fires — the plan/design pair is finding-free end to end.
func TestSDD161ScopesToRealizedSpecs(t *testing.T) {
	for _, ex := range Get("SDD161").Good {
		if !strings.HasPrefix(ex.Name, "design-scoped") && !strings.HasPrefix(ex.Name, "design-declared") && !strings.HasPrefix(ex.Name, "graph-plan-scoped") {
			continue
		}
		t.Run(ex.Name, func(t *testing.T) {
			diags := runExample(t, ex)
			for _, code := range []string{"SDD160", "SDD161", "SDD162", "SDD122"} {
				if got := findByCode(diags, code); len(got) != 0 {
					t.Errorf("%s must stay quiet, got %+v", code, got)
				}
			}
		})
	}
}

// TestSDD161ConflationRefusedPerSpec: a design realizing both overlapping
// specs with bare citations covers neither — one finding per spec, naming
// the spec whose requirement went uncited.
func TestSDD161ConflationRefusedPerSpec(t *testing.T) {
	var ex Example
	for _, candidate := range Get("SDD161").Bad {
		if candidate.Name == "design-conflates-same-numbered-id" {
			ex = candidate
		}
	}
	if ex.Name == "" {
		t.Fatal("example missing")
	}
	got := findByCode(runExample(t, ex), "SDD161")
	var messages []string
	for _, d := range got {
		messages = append(messages, d.Message)
	}
	want := []string{
		"Related designs never cite `FR-01` from `Specs/Other/README.md`.",
		"Related designs never cite `FR-01` from `Specs/Sample/README.md`.",
		"Related designs never cite `NFR-01` from `Specs/Other/README.md`.",
		"Related designs never cite `NFR-01` from `Specs/Sample/README.md`.",
	}
	if strings.Join(messages, "\n") != strings.Join(want, "\n") {
		t.Fatalf("per-spec findings:\nwant %q\n got %q", want, messages)
	}
	// Qualifying the citations resolves each to its own spec: the same
	// design realizing both specs is then finding-free.
	ex.Files["Designs/Sample/README.md"] = strReplace(ex.Files["Designs/Sample/README.md"],
		"Realizes FR-01 and NFR-01.", "Realizes Sample:FR-01, Sample:NFR-01, Other:FR-01 and Specs/Other:NFR-01.")
	if got := findByCode(runExample(t, ex), "SDD161"); len(got) != 0 {
		t.Fatalf("qualified citations must satisfy both specs, got %+v", got)
	}
}

// TestResolvedCitationsQualifiedIdentity exercises the prose resolver
// directly: bare ids resolve when unique, qualified spellings resolve to the
// named source, and an ambiguous bare id resolves to nothing.
func TestResolvedCitationsQualifiedIdentity(t *testing.T) {
	a := &Artifact{Rel: "Specs/A/README.md", Meta: map[string]any{"type": "spec"},
		Body: "- **FR-01**: a one.\n- **FR-02**: a two.\n"}
	b := &Artifact{Rel: "Specs/B/README.md", Meta: map[string]any{"type": "spec"},
		Body: "- **FR-01**: b one.\n"}
	x := buildCitationIndexFrom([]*Artifact{a, b})
	got := resolvedCitations(x, "Covers FR-01, FR-02, A:FR-01 and Specs/B:FR-01; FR-99 and C:FR-01 resolve nowhere.")
	var keys []string
	for k := range got {
		keys = append(keys, strings.ReplaceAll(k, "\x00", "#"))
	}
	sort.Strings(keys)
	want := []string{"Specs/A/README.md#FR-01", "Specs/A/README.md#FR-02", "Specs/B/README.md#FR-01"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("want %v, got %v", want, keys)
	}
}

// TestUnrelatedSourceHint: a qualified citation whose qualifier names a
// real design outside the index is explained; bare, unknown, and undefined
// citations are not.
func TestUnrelatedSourceHint(t *testing.T) {
	ex := Example{Files: map[string]string{
		"Plans/Sample/README.md":   tracePlan(""),
		"Plans/Sample/01-One.md":   tracePhase("Covers FR-01 and NFR-01."),
		"Specs/Sample/README.md":   strReplace(validSpecTemplate, "related: []", "related: [Designs/Sample]"),
		"Designs/Sample/README.md": designWithDecisions("### DD-1 — Only decision\n"),
	}}
	root := exampleRoot(t, ex)
	plan := root.ByPath["Plans/Sample/README.md"]
	x := BuildCitationIndex(root, plan)
	if _, ok := x.Resolve("Designs/Sample:DD-1"); ok {
		t.Fatal("a spec's back-link to its design must not make the design citable from the plan")
	}
	for citation, want := range map[string]string{
		"Designs/Sample:DD-1": "Designs/Sample/README.md",
		"Sample:DD-1":         "Designs/Sample/README.md",
		"DD-1":                "",
		"Designs/Sample:DD-9": "",
		"Designs/Ghost:DD-1":  "",
		"Sample:FR-01":        "", // the spec IS reachable; FR-01 resolves, no hint
	} {
		if got := x.UnrelatedSource(root, citation); got != want {
			t.Errorf("%s: want %q, got %q", citation, want, got)
		}
	}
}
