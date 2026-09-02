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
		if justifies, ok := planGraphJustifies(plan); ok {
			graphPlan = true
			index := BuildCitationIndex(r, plan)
			for _, j := range justifies {
				if hit, ok := index.Resolve(j); ok {
					graphCited[hit.SourceRel+"\x00"+hit.ID] = true
				}
			}
		}
		graphCites := func(specRel, id string) bool {
			return graphPlan && graphCited[specRel+"\x00"+id]
		}
		citeFix := func(v1 string) string {
			if graphPlan {
				return v1 + " For a graph plan, cite it in a node's `justifies` (qualified `Spec:ID` when related specs share id ranges)."
			}
			return v1
		}

		// Python joins each design's comment-stripped body with a JSON dump of
		// its frontmatter, so a requirement cited only in a design's metadata
		// still counts as covered.
		var designParts []string
		for _, d := range designs {
			designParts = append(designParts, noComments(d.Body)+"\n"+metaJSONText(d.Meta))
		}
		designText := strings.Join(designParts, "\n")

		for _, spec := range specs {
			implicatedSet := map[string]bool{spec.Rel: true}
			for _, d := range designs {
				implicatedSet[d.Rel] = true
			}
			implicated := sortedSetSlice(implicatedSet)
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
					if len(designs) > 0 && !strings.Contains(designText, id) {
						out = append(out, traceabilityFinding{
							Code: "SDD161", Plan: plan.Rel,
							Message:    "Related designs never cite `" + id + "` from `" + spec.Rel + "`.",
							Correction: "Cite the requirement in a realizing design or remove an incorrect design relationship.",
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
			"Plans/Sample/README.md":        tracePlan(""),
			"Plans/Sample/01-One.md":        tracePhase("Does the thing."),
			"Plans/Sample/Sample-Graph.json": traceGraphJSON,
			"Specs/Sample/README.md":        validSpecTemplate,
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
			"Plans/Sample/README.md":        tracePlan(""),
			"Plans/Sample/01-One.md":        tracePhase("Does the thing."),
			"Plans/Sample/Sample-Graph.json": traceGraphJSON,
			"Specs/Sample/README.md":        validSpecTemplate,
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
		}}},
		Good: []Example{{Name: "design-cites-requirement", Files: map[string]string{
			"Plans/Sample/README.md":   tracePlan(`, "Designs/Sample"`),
			"Plans/Sample/01-One.md":   tracePhase("Covers FR-01 and NFR-01."),
			"Specs/Sample/README.md":   validSpecTemplate,
			"Designs/Sample/README.md": validDesign("Realizes FR-01 and NFR-01."),
		}}},
	})
}
