package rules

import (
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

func init() {
	Register(&Rule{
		Code: "SDD162", Severity: Error, PyFunc: "_traceability",
		What: "a plan hierarchy never cites an `AC-NN` id from a related spec",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
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
				for _, spec := range specs {
					implicatedSet := map[string]bool{spec.Rel: true}
					for _, d := range designs {
						implicatedSet[d.Rel] = true
					}
					implicated := sortedSetSlice(implicatedSet)
					for _, id := range sortedSetSlice(specDefinedIDs(spec)["AC"]) {
						if strings.Contains(planText, id) {
							continue
						}
						emit(Diagnostic{
							Code: "SDD162", Severity: Error, Path: plan.Rel, Line: 1,
							Message:    "Plan hierarchy never cites `" + id + "` from `" + spec.Rel + "`.",
							Correction: "Cite the acceptance criterion in task verification/detail or phase acceptance criteria.",
							Implicated: implicated,
						})
					}
				}
			}
		},
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
		}}},
	})
}
