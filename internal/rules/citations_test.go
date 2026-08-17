package rules

import "testing"

// findingsFor loads a planning root from the given files and returns only the
// diagnostics carrying the wanted code.
func findingsFor(t *testing.T, code string, files map[string]string) []Diagnostic {
	t.Helper()
	var out []Diagnostic
	for _, d := range Run(rootFrom(t, files)) {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// DD ids belong to designs, so a plan cites them through its related design —
// the same graph FR/NFR/AC use, but resolving against a different kind.
func TestSDD122ResolvesDesignDecisions(t *testing.T) {
	design := designWithDecisions("### DD-1 — Heading form\n\n- **DD-2 — Bullet form**: text\n")
	cases := []struct {
		name, citation string
		wantFinding    bool
	}{
		{"heading-defined", "See DD-1.", false},
		{"bullet-defined", "See DD-2.", false},
		{"dangling", "See DD-7.", true},
		// A colon-qualified reference names another design's decision and is
		// not resolved locally.
		{"qualified-colon", "See OtherComponent:DD-7.", false},
		// The bare-space form does NOT qualify. It is indistinguishable from
		// prose ending in a capitalized word, and honoring it silently excused
		// 14 real dangling FR citations on a live planning root — measured,
		// not hypothetical. Authors write the colon form instead.
		{"space-is-not-qualification", "See ArkBootstrapApi DD-7.", true},
		// Ordinary prose must NOT qualify, or every dangling id would hide.
		{"prose-before-id", "As decided in DD-7.", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := findingsFor(t, "SDD122", map[string]string{
				"Research/r.md": replaceFirst(
					replaceFirst(validResearch, "related: []", "related: [Designs/Sample]"),
					"## Context\n\nText.", "## Context\n\n"+tc.citation),
				"Designs/Sample/README.md": design,
			})
			if got := len(diags) > 0; got != tc.wantFinding {
				t.Errorf("%q: finding=%v, want %v (%v)", tc.citation, got, tc.wantFinding, diags)
			}
		})
	}
}

// A design defines DD ids; it must not be asked to resolve its own.
func TestSDD122DesignDoesNotCiteOwnDecisions(t *testing.T) {
	diags := findingsFor(t, "SDD122", map[string]string{
		"Designs/Sample/README.md": designWithDecisions(
			"### DD-1 — First\n\nSee DD-1 and DD-2 above.\n\n### DD-2 — Second\n"),
	})
	if len(diags) != 0 {
		t.Errorf("a design must not resolve its own DD namespace: %v", diags)
	}
}

// The design remains a HOP for spec requirements while also being a source
// for its own decisions.
func TestSDD122DesignIsBothSourceAndHop(t *testing.T) {
	design := replaceFirst(
		designWithDecisions("### DD-1 — Only decision\n"),
		"related: []", "related: [Specs/Sample]")
	diags := findingsFor(t, "SDD122", map[string]string{
		"Research/r.md": replaceFirst(
			replaceFirst(validResearch, "related: []", "related: [Designs/Sample]"),
			"## Context\n\nText.", "## Context\n\nSee DD-1 and FR-01."),
		"Designs/Sample/README.md": design,
		"Specs/Sample/README.md":   validSpecTemplate,
	})
	if len(diags) != 0 {
		t.Errorf("DD via the design and FR through it must both resolve: %v", diags)
	}
}
