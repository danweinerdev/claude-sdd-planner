package rules

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Family (g cont'd): Validator._traceability — SDD162: a plan whose related
// specs define acceptance criteria must cite every `AC-NN` id somewhere in
// its phase task verification/detail text or phase acceptance criteria. The
// full function also carries SDD160/161 (FR/NFR coverage in the plan and in
// related designs); those are out of scope for this pass.

var acTaskHeadingRe = regexp.MustCompile(`^\d+(?:[A-Z])?(?:-[A-Z])?\.\d+(?:\s*:|\s|$)`)

var completionEvidenceHeadingRe = regexp.MustCompile(`^###\s+Completion Evidence\s*$`)
var anyH3HeadingRe = regexp.MustCompile(`^###\s+`)

// stripCompletionEvidence ports strip_completion_evidence(): it drops a task
// section's `### Completion Evidence` subsection (retrospective evidence text
// isn't part of what a task's citations count as coverage) up to the next
// level-3 heading or the end of the text.
func stripCompletionEvidence(text string) string {
	lines := strings.Split(noComments(text), "\n")
	var out []string
	skipping := false
	for _, l := range lines {
		if !skipping && completionEvidenceHeadingRe.MatchString(l) {
			skipping = true
			continue
		}
		if skipping {
			if anyH3HeadingRe.MatchString(l) {
				skipping = false
			} else {
				continue
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func sortedSetSlice(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// traceabilityFinding is one uncited identifier, before filtering to a code.
type traceabilityFinding struct {
	Code       string
	Plan       string
	Message    string
	Correction string
	Implicated []string
}

// traceabilityScan ports the identifier-coverage half of Validator._traceability:
// every FR/NFR/AC a related spec defines must be cited somewhere in the plan
// hierarchy (SDD160/162), and every FR/NFR must additionally be cited by a
// related design when the plan declares one (SDD161).
//
// The three codes come from one scan so they agree on what "the plan text"
// and "the design text" are. Splitting them would let the plan-side and
// design-side rules disagree about which phases they read.
func traceabilityScan(r *Root) []traceabilityFinding {
	var out []traceabilityFinding
	for _, plan := range r.Artifacts {
		if plan.Meta == nil || plan.Kind() != "plan" {
			continue
		}
		status := plan.Status()
		if status != "approved" && status != "active" && status != "complete" {
			continue
		}
		specs := relatedSpecs(r, plan)
		if len(specs) == 0 {
			continue
		}
		var designs []*Artifact
		if related, ok := plan.Meta["related"].([]any); ok {
			for _, ref := range related {
				s, ok := ref.(string)
				if !ok {
					continue
				}
				target := resolveRelated(r, s)
				if target != nil && target.Kind() == "design" {
					designs = append(designs, target)
				}
			}
		}
		var planTextParts []string
		for _, p := range asAnyList(plan.Meta["phases"]) {
			m := planEntry(p)
			if m == nil {
				continue
			}
			doc, ok := m["doc"].(string)
			if !ok {
				continue
			}
			target, ok := r.ByPath[path.Join(path.Dir(plan.Rel), doc)]
			if !ok {
				continue
			}
			for _, t := range asAnyList(target.Meta["tasks"]) {
				tm := planEntry(t)
				if tm == nil {
					continue
				}
				if v, ok := tm["verification"].(string); ok {
					planTextParts = append(planTextParts, v)
				}
			}
			secs := sections(target, 2)
			if acc, ok := secs["Acceptance Criteria"]; ok {
				planTextParts = append(planTextParts, acc.Body)
			}
			for heading, info := range secs {
				if acTaskHeadingRe.MatchString(heading) {
					planTextParts = append(planTextParts, stripCompletionEvidence(info.Body))
				}
			}
		}
		planText := strings.Join(planTextParts, "\n")

		// Graph plans carry their citations in node `justifies` inside the
		// committed <Plan>-Graph.json — rendered views hold `tasks: []` by
		// design, so the v1 harvest above is empty for them. The citations
		// resolve through the compiler's own CitationIndex opinion, PER
		// SPEC: a citation resolving to one spec never satisfies another
		// spec's same-numbered id, and an ambiguous bare citation covers
		// neither (compile refuses it anyway).
		graphPlan := false
		graphCited := map[string]bool{}
		if justifies, ok := planGraphJustifies(r, plan); ok {
			graphPlan = true
			// Match graph compile's coverage boundary: transitive sources stay
			// citable, but only directly related specs demand implementation.
			specs = nil
			for _, source := range DirectRelatedSources(r, plan) {
				if source.Kind() == "spec" {
					specs = append(specs, source)
				}
			}
			index := BuildCitationIndex(r, plan)
			for _, j := range justifies {
				if hit, ok := index.Resolve(j); ok {
					graphCited[CitationKey(hit.SourceRel, hit.ID)] = true
				}
			}
		}
		graphCites := func(specRel, id string) bool {
			return graphPlan && graphCited[CitationKey(specRel, id)]
		}
		citeFix := func(v1 string) string {
			if graphPlan {
				return v1 + " For a graph plan, cite it in a node's `justifies` (qualified `Spec:ID` when related specs share id ranges)."
			}
			return v1
		}

		// SDD161 is scoped per design to the specs that design REALIZES,
		// and resolves the design's citations with qualified identity:
		//
		//   - A design declares what it realizes through its own `related`
		//     specs, or through a spec's `related` back-link to it. A design
		//     that declares nothing realizes every spec on the plan's demand
		//     (the pre-scoping behavior — an undeclared design never escapes
		//     the check by staying silent).
		//   - Each design's prose is resolved through a CitationIndex over the
		//     specs it realizes, so `Channels:FR-01` counts for the channels
		//     spec only, and a bare `FR-01` in a design realizing two specs
		//     that both define it counts for neither (it is ambiguous, the
		//     same verdict compile gives a bare node citation).
		//   - A spec no related design realizes puts nothing on the
		//     design-side demand: the plan may relate a design that realizes
		//     one of its specs without that design being held to the other
		//     spec's requirements.
		//
		// Python joins each design's comment-stripped body with a JSON dump of
		// its frontmatter, so a requirement cited only in a design's metadata
		// still counts as covered.
		type realization struct {
			design *Artifact
			specs  map[string]bool // spec rel -> realized
			cited  map[string]bool // CitationKey(spec rel, id)
		}
		var realizations []realization
		for _, d := range designs {
			realized := realizedSpecs(r, d)
			if len(realized) == 0 {
				realized = specs
			}
			rz := realization{design: d, specs: map[string]bool{}}
			for _, spec := range realized {
				rz.specs[spec.Rel] = true
			}
			index := buildCitationIndexFrom(realized)
			rz.cited = resolvedCitations(index, noComments(d.Body)+"\n"+metaJSONText(d.Meta))
			realizations = append(realizations, rz)
		}

		for _, spec := range specs {
			implicatedSet := map[string]bool{spec.Rel: true}
			var realizers []realization
			for _, rz := range realizations {
				if rz.specs[spec.Rel] {
					realizers = append(realizers, rz)
					implicatedSet[rz.design.Rel] = true
				}
			}
			implicated := sortedSetSlice(implicatedSet)
			designCites := func(id string) bool {
				for _, rz := range realizers {
					if rz.cited[CitationKey(spec.Rel, id)] {
						return true
					}
				}
				return false
			}
			ids := specDefinedIDs(spec)
			for _, family := range []string{"FR", "NFR"} {
				for _, id := range sortedSetSlice(ids[family]) {
					if !strings.Contains(planText, id) && !graphCites(spec.Rel, id) {
						out = append(out, traceabilityFinding{
							Code: "SDD160", Plan: plan.Rel,
							Message:    "Plan hierarchy never cites `" + id + "` from `" + spec.Rel + "`.",
							Correction: citeFix("Cite the requirement in task verification/detail or phase acceptance criteria, or explicitly narrow the related specifications."),
							Implicated: implicated,
						})
					}
					if len(realizers) > 0 && !designCites(id) {
						out = append(out, traceabilityFinding{
							Code: "SDD161", Plan: plan.Rel,
							Message:    "Related designs never cite `" + id + "` from `" + spec.Rel + "`.",
							Correction: "Cite the requirement in a realizing design (qualified `Spec:ID` when the design realizes specs that share id ranges), or remove an incorrect design relationship.",
							Implicated: implicated,
						})
					}
				}
			}
			for _, id := range sortedSetSlice(ids["AC"]) {
				if strings.Contains(planText, id) || graphCites(spec.Rel, id) {
					continue
				}
				out = append(out, traceabilityFinding{
					Code: "SDD162", Plan: plan.Rel,
					Message:    "Plan hierarchy never cites `" + id + "` from `" + spec.Rel + "`.",
					Correction: citeFix("Cite the acceptance criterion in task verification/detail or phase acceptance criteria."),
					Implicated: implicated,
				})
			}
		}
	}
	return out
}

// realizedSpecs returns the specs a design declares it realizes: every spec
// its own `related` names directly, plus every spec whose `related` names
// the design (the back-link a spec writes toward its design). One hop in
// each direction, no traversal — realization is a first-order claim between
// a design and a spec, not something inherited through a neighbouring
// design or plan — so the result is cycle-free by construction. Order is
// deterministic (design-side declarations in frontmatter order, then
// back-links in root path order); an empty result means the design declares
// nothing and the caller decides the fallback.
func realizedSpecs(r *Root, d *Artifact) []*Artifact {
	seen := map[string]bool{}
	var out []*Artifact
	for _, src := range DirectRelatedSources(r, d) {
		if src.Kind() == "spec" && !seen[src.Rel] {
			seen[src.Rel] = true
			out = append(out, src)
		}
	}
	rels := make([]string, 0, len(r.ByPath))
	for rel := range r.ByPath {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		spec := r.ByPath[rel]
		if spec.Meta == nil || spec.Kind() != "spec" || seen[spec.Rel] {
			continue
		}
		for _, target := range DirectRelatedSources(r, spec) {
			if target.Rel == d.Rel {
				seen[spec.Rel] = true
				out = append(out, spec)
				break
			}
		}
	}
	return out
}

// traceabilityCheckRoot runs the shared scan and keeps one code.
func traceabilityCheckRoot(code string) func(*Root, func(Diagnostic)) {
	return func(r *Root, emit func(Diagnostic)) {
		for _, f := range traceabilityScan(r) {
			if f.Code != code {
				continue
			}
			emit(Diagnostic{
				Code: f.Code, Severity: Error, Path: f.Plan, Line: 1,
				Message: f.Message, Correction: f.Correction, Implicated: f.Implicated,
			})
		}
	}
}

func init() {
	Register(&Rule{
		Code: "SDD162", Severity: Error, PyFunc: "_traceability",
		What:      "a plan hierarchy never cites an `AC-NN` id from a related spec",
		CheckRoot: traceabilityCheckRoot("SDD162"),
		Bad: []Example{{Name: "uncited-ac", Files: map[string]string{
			"Plans/Sample/README.md": strings.Replace(
				strings.Replace(planWithPhase(map[string]string{
					"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
				}), "status: draft", "status: approved", 1),
				"related: []", "related: [\"Specs/Sample\"]", 1),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: Does the thing.
    justifies: FR-01
`),
			"Specs/Sample/README.md": validSpecTemplate,
		}}},
		Good: []Example{{Name: "cited-ac", Files: map[string]string{
			"Plans/Sample/README.md": strings.Replace(
				strings.Replace(planWithPhase(map[string]string{
					"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
				}), "status: draft", "status: approved", 1),
				"related: []", "related: [\"Specs/Sample\"]", 1),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: Verifies AC-01.
    justifies: FR-01
`),
			"Specs/Sample/README.md": validSpecTemplate,
		}}, {Name: "graph-plan-justifies-ac", Files: map[string]string{
			// The committed graph's node justifies satisfy the AC demand
			// without any phase-doc citation text.
			"Plans/Sample/README.md":         tracePlan(""),
			"Plans/Sample/01-One.md":         tracePhase("Does the thing."),
			"Plans/Sample/Sample-Graph.json": traceGraphJSON,
			"Specs/Sample/README.md":         validSpecTemplate,
		}}},
	})
}

// traceGraphJSON is the examples' committed graph: one node whose justifies
// cover the fixture spec's whole surface, bare ids (single spec, no
// ambiguity).
const traceGraphJSON = `{"version":1,"seq_counter":0,"nodes":[{"id":"n1","contract":"c","justifies":["FR-01","NFR-01","AC-01"],"gate":{"type":"tests","tests":[{"id":"t","file":"f.ext"}]},"hazards":[],"estimate":1}]}`

// metaJSONText renders frontmatter the way Python's
// json.dumps(meta, default=str) does, for the design-side coverage search.
//
// Only substring containment of an identifier is ever asked of the result, so
// key order does not matter; what matters is that every value appears. Go's
// encoder sorts map keys, which makes the output deterministic — a difference
// from Python that cannot change any answer here.
func metaJSONText(meta map[string]any) string {
	b, err := json.Marshal(jsonSafe(meta))
	if err != nil {
		return ""
	}
	return string(b)
}

// jsonSafe converts values the encoder would reject into strings, the role
// json.dumps's `default=str` plays.
func jsonSafe(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = jsonSafe(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			out = append(out, jsonSafe(val))
		}
		return out
	case string, bool, float64, int, nil:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// tracePlan builds an approved plan related to the sample spec, plus any
// extra related references the example needs.
func tracePlan(extraRelated string) string {
	related := `related: ["Specs/Sample"` + extraRelated + `]`
	return replaceFirst(
		replaceFirst(planWithPhase(map[string]string{
			"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
		}), "status: draft", "status: approved"),
		"related: []", related)
}

// tracePhase is a phase whose task verification cites the given text, which is
// what the plan-side coverage search reads.
func tracePhase(verification string) string {
	return phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: `+verification+`
    justifies: FR-01
`)
}

func init() {
	Register(&Rule{
		Code: "SDD160", Severity: Error, PyFunc: "_traceability",
		What:      "a plan hierarchy never cites an `FR-NN`/`NFR-NN` id from a related spec",
		CheckRoot: traceabilityCheckRoot("SDD160"),
		Bad: []Example{{Name: "uncited-requirement", Files: map[string]string{
			"Plans/Sample/README.md": tracePlan(""),
			"Plans/Sample/01-One.md": tracePhase("Does the thing."),
			"Specs/Sample/README.md": validSpecTemplate,
		}}},
		Good: []Example{{Name: "cited-requirement", Files: map[string]string{
			"Plans/Sample/README.md": tracePlan(""),
			"Plans/Sample/01-One.md": tracePhase("Covers FR-01 and NFR-01."),
			"Specs/Sample/README.md": validSpecTemplate,
		}}, {Name: "graph-plan-justifies", Files: map[string]string{
			// A graph plan's citations live in node justifies, not phase
			// text: the committed graph satisfies traceability by itself.
			"Plans/Sample/README.md":         tracePlan(""),
			"Plans/Sample/01-One.md":         tracePhase("Does the thing."),
			"Plans/Sample/Sample-Graph.json": traceGraphJSON,
			"Specs/Sample/README.md":         validSpecTemplate,
		}}},
	})

	Register(&Rule{
		Code: "SDD161", Severity: Error, PyFunc: "_traceability",
		What:      "a related design never cites an `FR-NN`/`NFR-NN` id from a related spec",
		CheckRoot: traceabilityCheckRoot("SDD161"),
		// SDD161 fires only when the plan declares a design, so both examples
		// carry one; they differ in whether that design cites the ids.
		Bad: []Example{{Name: "design-omits-requirement", Files: map[string]string{
			"Plans/Sample/README.md":   tracePlan(`, "Designs/Sample"`),
			"Plans/Sample/01-One.md":   tracePhase("Covers FR-01 and NFR-01."),
			"Specs/Sample/README.md":   validSpecTemplate,
			"Designs/Sample/README.md": validDesign("Text."),
		}}, {Name: "design-conflates-same-numbered-id", Files: map[string]string{
			// The design realizes BOTH overlapping specs but cites a bare
			// FR-01: ambiguous between them, so it covers neither spec's
			// requirement. One spec's citation never satisfies the other's.
			"Plans/Sample/README.md":   tracePlan(`, "Specs/Other", "Designs/Sample"`),
			"Plans/Sample/01-One.md":   tracePhase("Covers FR-01 and NFR-01."),
			"Specs/Sample/README.md":   validSpecTemplate,
			"Specs/Other/README.md":    otherSpec,
			"Designs/Sample/README.md": strReplace(validDesign("Realizes FR-01 and NFR-01."), "related: []", "related: [Specs/Sample, Specs/Other]"),
		}}, {Name: "realized-spec-uncited", Files: map[string]string{
			// Genuine gap: the design declares it realizes the spec and
			// never cites its requirements — scoping does not excuse it.
			"Plans/Sample/README.md":   tracePlan(`, "Designs/Sample"`),
			"Plans/Sample/01-One.md":   tracePhase("Covers FR-01 and NFR-01."),
			"Specs/Sample/README.md":   validSpecTemplate,
			"Designs/Sample/README.md": strReplace(validDesign("Text."), "related: []", "related: [Specs/Sample]"),
		}}},
		Good: []Example{{Name: "design-cites-requirement", Files: map[string]string{
			"Plans/Sample/README.md":   tracePlan(`, "Designs/Sample"`),
			"Plans/Sample/01-One.md":   tracePhase("Covers FR-01 and NFR-01."),
			"Specs/Sample/README.md":   validSpecTemplate,
			"Designs/Sample/README.md": validDesign("Realizes FR-01 and NFR-01."),
		}}, {Name: "design-scoped-to-realized-spec", Files: map[string]string{
			// Two related specs share id ranges; the design declares (via its
			// own `related`) that it realizes only the second. Its bare
			// FR-01/NFR-01 resolve to that spec alone, and the first spec —
			// which no design realizes — puts nothing on the design demand.
			"Plans/Sample/README.md":   tracePlan(`, "Specs/Other", "Designs/Sample"`),
			"Plans/Sample/01-One.md":   tracePhase("Covers FR-01, NFR-01 and AC-01."),
			"Specs/Sample/README.md":   validSpecTemplate,
			"Specs/Other/README.md":    otherSpec,
			"Designs/Sample/README.md": strReplace(validDesign("Realizes FR-01 and NFR-01."), "related: []", "related: [Specs/Other]"),
		}}, {Name: "design-declared-by-spec-backlink", Files: map[string]string{
			// The realization can also be declared from the spec side: the
			// spec's `related` names the design, the design's is empty. The
			// design then cites the spec's ids qualified (SDD122 resolves a
			// design's bare ids only through the design's own `related`).
			"Plans/Sample/README.md":   tracePlan(`, "Specs/Other", "Designs/Sample"`),
			"Plans/Sample/01-One.md":   tracePhase("Covers FR-01, NFR-01 and AC-01."),
			"Specs/Sample/README.md":   validSpecTemplate,
			"Specs/Other/README.md":    strReplace(otherSpec, "related: []", "related: [Designs/Sample]"),
			"Designs/Sample/README.md": validDesign("Realizes Other:FR-01 and Other:NFR-01."),
		}}, {Name: "graph-plan-scoped-design", Files: map[string]string{
			// A graph plan over two overlapping specs, with the design
			// realizing one of them and citing it with qualified spellings:
			// per-spec identity holds on both the plan and design sides.
			"Plans/Sample/README.md":         tracePlan(`, "Specs/Other", "Designs/Sample"`),
			"Plans/Sample/01-One.md":         tracePhase("Does the thing."),
			"Plans/Sample/Sample-Graph.json": traceTwoSpecGraphJSON,
			"Specs/Sample/README.md":         validSpecTemplate,
			"Specs/Other/README.md":          otherSpec,
			"Designs/Sample/README.md":       strReplace(validDesign("Realizes Other:FR-01 and Specs/Other:NFR-01."), "related: []", "related: [Specs/Other]"),
		}}},
	})
}

// otherSpec is a second spec sharing the sample's id ranges (FR-01, NFR-01,
// AC-01) — the cross-spec collision surface.
var otherSpec = strReplace(validSpecTemplate, "title: Sample Spec", "title: Other Spec")

// traceTwoSpecGraphJSON covers both overlapping specs with qualified
// citations: a bare id would be ambiguous and cover neither.
const traceTwoSpecGraphJSON = `{"version":1,"seq_counter":0,"nodes":[{"id":"n1","contract":"c","justifies":["Specs/Sample:FR-01","Specs/Sample:NFR-01","Specs/Sample:AC-01","Other:FR-01","Other:NFR-01","Other:AC-01"],"gate":{"type":"tests","tests":[{"id":"t","file":"f.ext"}]},"hazards":[],"estimate":1}]}`
