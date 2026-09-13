package rules

import (
	"strings"
	"testing"
)

// rootAlias is the single enumerated substitution permitted when comparing
// diagnostics produced under two different temporary roots. Fixtures are
// materialized in distinct t.TempDir()s, and a handful of messages embed the
// absolute fixture root (the {{REPO}} evidence label, messages naming the
// repository directory). Nothing else is normalized: paths, lines, codes,
// severities, corrections and implicated lists are compared as produced.
const rootAlias = "<ROOT>"

// reproducibilityCases are the representative examples this node compares: a
// non-Git example and two Git-backed ones whose Setup commits under the fixed
// fixture identity, so an independently constructed fixture must reach the
// same HEAD.
func reproducibilityCases(t *testing.T) map[string]Example {
	t.Helper()
	cases := map[string]struct {
		code  string
		index int
		name  string
		git   bool
	}{
		"SDD020": {code: "SDD020", index: 0},
		"SDD154": {code: "SDD154", index: 0, name: "spec-elements-removed", git: true},
		"SDD173": {code: "SDD173", index: 0, name: "dirty-target-after-review", git: true},
	}
	out := map[string]Example{}
	for label, c := range cases {
		r := ruleByCode(t, c.code)
		if len(r.Bad) <= c.index {
			t.Fatalf("%s has no Bad[%d]", c.code, c.index)
		}
		ex := r.Bad[c.index]
		if c.name != "" && ex.Name != c.name {
			t.Fatalf("%s Bad[%d] is %q, want %q", c.code, c.index, ex.Name, c.name)
		}
		if c.git && len(ex.Setup) == 0 {
			t.Fatalf("%s Bad[%d] has no Setup; a Git-backed fixture is required", c.code, c.index)
		}
		if !c.git && len(ex.Setup) != 0 {
			t.Fatalf("%s Bad[%d] has Setup; a non-Git fixture is required", c.code, c.index)
		}
		out[label] = ex
	}
	return out
}

// normalizeRootAliases replaces the fixture's absolute root with rootAlias in
// the only fields that can legitimately embed it. It works on a copy so the
// caller's diagnostics are untouched.
func normalizeRootAliases(diags []Diagnostic, root string) []Diagnostic {
	sub := func(s string) string { return strings.ReplaceAll(s, root, rootAlias) }
	out := make([]Diagnostic, len(diags))
	for i, d := range diags {
		d.Path = sub(d.Path)
		d.Message = sub(d.Message)
		d.Correction = sub(d.Correction)
		if d.Implicated != nil {
			imp := make([]string, len(d.Implicated))
			for j, s := range d.Implicated {
				imp[j] = sub(s)
			}
			d.Implicated = imp
		}
		out[i] = d
	}
	return out
}

// FR-05 / AC-02 / DD-8: two fixtures of the same example, constructed
// independently in different temporary roots, produce identical complete
// diagnostics after enumerated root-alias normalization — and, for Git-backed
// examples, identical commit identities, proving the construction itself (not
// merely a repeated evaluation of one fixture) is reproducible.
func TestIndependentFixtureReproducibility(t *testing.T) {
	for label, ex := range reproducibilityCases(t) {
		t.Run(label, func(t *testing.T) {
			first := prepareExample(t, ex)
			second := prepareExample(t, ex)
			if first.dir == second.dir {
				t.Fatal("the two fixtures share a root; they must be constructed independently")
			}

			_, firstDiags := evaluatePrepared(t, first)
			_, secondDiags := evaluatePrepared(t, second)
			if len(firstDiags) == 0 {
				t.Fatal("Bad example produced no diagnostics")
			}

			a := normalizeRootAliases(firstDiags, first.dir)
			b := normalizeRootAliases(secondDiags, second.dir)
			if ok, why := diagnosticsEqual(a, b); !ok {
				t.Fatalf("independently constructed fixtures disagree: %s", why)
			}

			// Normalization must be the enumerated substitution and nothing
			// more: with it withheld, a message embedding a distinct absolute
			// root still has to be caught, so the comparison is not a
			// tautology.
			if strings.Contains(strings.Join(messagesOf(a), "\n"), rootAlias) {
				if ok, _ := diagnosticsEqual(firstDiags, secondDiags); ok {
					t.Error("root-embedding diagnostics compared equal without normalization; the comparison is not complete")
				}
			}

			if len(ex.Setup) == 0 {
				return
			}
			firstSnap := snapshotFixture(t, first)
			secondSnap := snapshotFixture(t, second)
			firstHead, ok := firstSnap["git:HEAD"]
			if !ok {
				t.Fatal("Git-backed fixture snapshot lacks HEAD")
			}
			secondHead := secondSnap["git:HEAD"]
			if firstHead != secondHead {
				t.Fatalf("independently constructed fixtures reached different HEADs: %s vs %s", firstHead, secondHead)
			}
			if firstSnap["git:index"] != secondSnap["git:index"] {
				t.Fatalf("independently constructed fixtures have different indexes:\n%s\n%s",
					firstSnap["git:index"], secondSnap["git:index"])
			}
		})
	}
}

func messagesOf(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Message+"\n"+d.Correction+"\n"+d.Path)
	}
	return out
}

func reversedRules(rules []*Rule) []*Rule {
	out := make([]*Rule, len(rules))
	for i, r := range rules {
		out[len(rules)-1-i] = r
	}
	return out
}

// FR-05 / AC-02 / DD-8 (hazard order-sensitive): the canonical diagnostics do
// not depend on the order the evaluator happens to see its inputs in. Both
// permutations are the REVERSE of the semantic order the evaluator would
// otherwise walk — the registration order of the rule list, and the sorted
// path order of the artifacts — so a result that silently inherited input
// order would differ here. The global registry is never mutated: the reversed
// list is a copy handed to runWith.
func TestRuleOrderIndependence(t *testing.T) {
	for label, ex := range reproducibilityCases(t) {
		t.Run(label, func(t *testing.T) {
			p := prepareExample(t, ex)

			registryOrder := All()
			reversed := reversedRules(registryOrder)
			if len(reversed) != len(registryOrder) {
				t.Fatalf("reversed rule list has %d rules, want %d", len(reversed), len(registryOrder))
			}
			if len(registryOrder) > 1 && reversed[0].Code == registryOrder[0].Code {
				t.Fatal("reversed rule list starts with the same rule as registry order")
			}
			// A copy, not a permutation of the registry's own slice.
			for i, r := range All() {
				if r.Code != registryOrder[i].Code {
					t.Fatalf("registry order changed under evaluation at %d: %s vs %s", i, r.Code, registryOrder[i].Code)
				}
			}

			forward, err := runWith(freshRoot(t, p.dir), registryOrder)
			if err != nil {
				t.Fatalf("runWith(registry order): %v", err)
			}
			if len(forward) == 0 {
				t.Fatal("Bad example produced no diagnostics")
			}
			backward, err := runWith(freshRoot(t, p.dir), reversed)
			if err != nil {
				t.Fatalf("runWith(reversed order): %v", err)
			}
			if ok, why := diagnosticsEqual(forward, backward); !ok {
				t.Fatalf("reversed rule order changed the canonical diagnostics: %s", why)
			}
		})
	}

	// The example fixtures above are single-artifact (SDD154's is deleted from
	// the worktree outright), so artifact order is only exercisable on a root
	// built to hold several. Reversing both axes at once is the strongest
	// permutation available: rules in reverse registration order over
	// artifacts in reverse sorted-path order.
	t.Run("artifact-order", func(t *testing.T) {
		files := map[string]string{
			"Research/a-waived.md":   researchWaivedHead,
			"Research/m-clean.md":    researchClean,
			"Research/z-archived.md": researchArchived,
		}
		_, root := materializeRoot(t, files)
		if len(root.Artifacts) != len(files) {
			t.Fatalf("root loaded %d artifacts, want %d", len(root.Artifacts), len(files))
		}
		forward, err := runWith(root, All())
		if err != nil {
			t.Fatal(err)
		}
		if len(forward) == 0 {
			t.Fatal("multi-artifact fixture produced no diagnostics")
		}

		permutedRoot := freshRoot(t, root.Dir)
		before := relsOf(permutedRoot.Artifacts)
		as := permutedRoot.Artifacts
		for i, j := 0, len(as)-1; i < j; i, j = i+1, j-1 {
			as[i], as[j] = as[j], as[i]
		}
		after := relsOf(permutedRoot.Artifacts)
		if before[0] == after[0] {
			t.Fatalf("reversing artifacts left the first artifact unchanged: %s", before[0])
		}
		// ByPath is a lookup keyed by path; reordering the slice must not
		// disturb it.
		for _, rel := range after {
			if got := permutedRoot.ByPath[rel]; got == nil || got.Rel != rel {
				t.Fatalf("ByPath[%q] is wrong after reordering Artifacts", rel)
			}
		}

		permuted, err := runWith(permutedRoot, reversedRules(All()))
		if err != nil {
			t.Fatal(err)
		}
		if ok, why := diagnosticsEqual(forward, permuted); !ok {
			t.Fatalf("reversed rule and artifact order changed the canonical diagnostics: %s", why)
		}
	})
}

func relsOf(as []*Artifact) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Rel)
	}
	return out
}
