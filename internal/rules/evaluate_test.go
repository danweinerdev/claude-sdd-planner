package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Fixtures: a research doc whose missing sections fire SDD020, once with a
// live waiver, once with a stale one, once archived (so reporting demotes
// its findings), plus a second clean artifact so per-artifact counts are
// distinguishable from per-root counts.
const (
	researchWaivedHead = "---\n" +
		"title: \"Topic\"\ntype: research\nstatus: draft\n" +
		"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n" +
		"waivers:\n  - code: SDD020\n    reason: \"Predates the section convention; archived context.\"\n---\n\n# Topic\n\n## Summary\n\nText.\n"
	researchComplete = "\n## Context\n\nText.\n\n## Findings\n\nText.\n\n## Analysis\n\nText.\n\n## Open Questions\n\nText.\n"
	researchArchived = "---\n" +
		"title: \"Old\"\ntype: research\nstatus: archived\n" +
		"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n---\n\n# Old\n\n## Summary\n\nText.\n"
	researchClean = "---\n" +
		"title: \"Clean\"\ntype: research\nstatus: draft\n" +
		"created: \"2026-01-01\"\nupdated: \"2026-01-01\"\ntags: [\"a\"]\nrelated: []\n---\n\n# Clean\n\n## Summary\n\nText.\n" + researchComplete
)

func materializeRoot(t *testing.T, files map[string]string) (string, *Root) {
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
	return dir, freshRoot(t, dir)
}

func freshRoot(t *testing.T, dir string) *Root {
	t.Helper()
	root, err := LoadRoot(dir)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	return root
}

func ruleByCode(t *testing.T, code string) *Rule {
	t.Helper()
	for _, r := range All() {
		if r.Code == code {
			return r
		}
	}
	t.Fatalf("rule %s not registered", code)
	return nil
}

// FR-07 / DD-6: one ordinary evaluation runs each root rule once and each
// artifact rule once per artifact, on both the strict and the reporting
// path, even though the waiver-bookkeeping rules are in the list.
func TestOrdinaryEvaluationOnce(t *testing.T) {
	files := map[string]string{
		"Research/waived.md": researchWaivedHead,
		"Research/clean.md":  researchClean,
	}
	for _, mode := range []struct {
		name string
		run  func(*Root, []*Rule) []Diagnostic
	}{{"strict", runWith}, {"reporting", runWithWaiversWith}} {
		t.Run(mode.name, func(t *testing.T) {
			_, root := materializeRoot(t, files)
			rootCalls, artifactCalls := 0, map[string]int{}
			counting := &Rule{
				Code: "SDD999", Severity: Error,
				CheckRoot: func(*Root, func(Diagnostic)) { rootCalls++ },
				Check:     func(a *Artifact, _ func(Diagnostic)) { artifactCalls[a.Rel]++ },
			}
			rules := []*Rule{counting, ruleByCode(t, "SDD020"), ruleByCode(t, "SDD176"), ruleByCode(t, "SDD177")}
			mode.run(root, rules)
			if rootCalls != 1 {
				t.Errorf("root rule invoked %d times, want exactly 1", rootCalls)
			}
			for _, a := range root.Artifacts {
				if artifactCalls[a.Rel] != 1 {
					t.Errorf("artifact rule invoked %d times on %s, want exactly 1", artifactCalls[a.Rel], a.Rel)
				}
			}
			if len(artifactCalls) != len(root.Artifacts) {
				t.Errorf("artifact rule saw %d artifacts, root has %d", len(artifactCalls), len(root.Artifacts))
			}
		})
	}
}

// oracleOrdinary is the independent definition of an evaluation's ordinary
// findings: every non-bookkeeping rule evaluated alone, each on its own
// freshly loaded root, so no rule can observe another's work. The sweep
// under test must produce exactly this set.
func oracleOrdinary(t *testing.T, dir string, rules []*Rule) []Diagnostic {
	t.Helper()
	var out []Diagnostic
	emit := func(d Diagnostic) { out = append(out, d) }
	for _, rule := range rules {
		if waiverRuleCodes[rule.Code] {
			continue
		}
		root := freshRoot(t, dir)
		if rule.CheckRoot != nil {
			rule.CheckRoot(root, emit)
		}
		for _, a := range root.Artifacts {
			if rule.Check != nil {
				rule.Check(a, emit)
			}
		}
	}
	return out
}

// oracleWaiverFindings defines the bookkeeping findings as a function of the
// evaluation's own ordinary findings: a waiver is stale exactly when nothing
// in THIS evaluation matched it.
func oracleWaiverFindings(t *testing.T, dir string, ordinary []Diagnostic, rules []*Rule) []Diagnostic {
	t.Helper()
	wanted := map[string]bool{}
	for _, rule := range rules {
		if waiverRuleCodes[rule.Code] {
			wanted[rule.Code] = true
		}
	}
	scratch := append([]Diagnostic(nil), ordinary...)
	var out []Diagnostic
	for _, d := range applyWaivers(freshRoot(t, dir), scratch) {
		if wanted[d.Code] {
			out = append(out, d)
		}
	}
	return out
}

// DD-6 (derives-state): strict and reporting results equal the oracle's
// per-rule-in-isolation definition, including the case where the waiver
// rules must derive from the local rule list rather than the global
// registry.
func TestStrictAndReportingSemanticsPreserved(t *testing.T) {
	fixtures := map[string]map[string]string{
		"live-waiver":  {"Research/topic.md": researchWaivedHead, "Research/clean.md": researchClean},
		"stale-waiver": {"Research/topic.md": researchWaivedHead + researchComplete, "Research/clean.md": researchClean},
		"archived":     {"Research/old.md": researchArchived, "Research/clean.md": researchClean},
	}
	ruleSets := map[string][]*Rule{
		"registry":          All(),
		"waiver-rules-only": {ruleByCode(t, "SDD176"), ruleByCode(t, "SDD177")},
	}
	for fname, files := range fixtures {
		for sname, rules := range ruleSets {
			t.Run(fname+"/"+sname, func(t *testing.T) {
				dir, _ := materializeRoot(t, files)
				ordinary := oracleOrdinary(t, dir, rules)

				wantStrict := append(append([]Diagnostic(nil), ordinary...), oracleWaiverFindings(t, dir, ordinary, rules)...)
				sortStrict(wantStrict)
				gotStrict := runWith(freshRoot(t, dir), rules)
				if !reflect.DeepEqual(gotStrict, wantStrict) {
					t.Errorf("strict mismatch\n got: %+v\nwant: %+v", gotStrict, wantStrict)
				}

				wantReporting := append([]Diagnostic(nil), ordinary...)
				wantReporting = append(wantReporting, applyWaivers(freshRoot(t, dir), wantReporting)...)
				wantReporting = demoteRetiredFindings(freshRoot(t, dir), wantReporting)
				SortDiagnostics(wantReporting)
				gotReporting := runWithWaiversWith(freshRoot(t, dir), rules)
				if !reflect.DeepEqual(gotReporting, wantReporting) {
					t.Errorf("reporting mismatch\n got: %+v\nwant: %+v", gotReporting, wantReporting)
				}
				if sname == "waiver-rules-only" && fname == "live-waiver" && !hasCode(gotStrict, "SDD177") {
					t.Errorf("a waiver whose code is outside the evaluated rule list must read as stale; got %v", codesOf(gotStrict))
				}
			})
		}
	}
}

func hasCode(ds []Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}
