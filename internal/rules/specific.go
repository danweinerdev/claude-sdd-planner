package rules

import (
	"regexp"
	"strings"
)

var nonBlockingRe = regexp.MustCompile(`(?i)\*\*non-blocking\*\*`)
var openQuestionBulletRe = regexp.MustCompile(`^\s*-\s*(.*?)\s*$`)

// openQuestionItems ports open_question_items(): every bullet (with wrapped
// continuation lines folded in) under an Open Questions section, excluding a
// bare "none"/"n/a" placeholder.
func openQuestionItems(body string) []string {
	value := strings.TrimSpace(visibleMarkdown(body))
	if value == "" || isNoneOrNA(value) {
		return nil
	}
	var items []string
	var current *string
	for _, line := range strings.Split(value, "\n") {
		if m := openQuestionBulletRe.FindStringSubmatch(line); m != nil {
			if current != nil {
				items = append(items, *current)
			}
			c := m[1]
			current = &c
			continue
		}
		continuation := strings.TrimSpace(line)
		if continuation == "" {
			continue
		}
		if current == nil {
			items = append(items, continuation)
		} else {
			c := strings.TrimSpace(*current + " " + continuation)
			current = &c
		}
	}
	if current != nil {
		items = append(items, *current)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !isNoneOrNA(item) {
			out = append(out, item)
		}
	}
	return out
}

func isNoneOrNA(s string) bool {
	v := strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "."))
	return v == "none" || v == "n/a"
}

// Family (i): Validator._specific and its dispatch targets that don't need a
// Git adapter — SDD050 (spec defines no FR/NFR/AC element), SDD051 (a shared
// required-field check for phase/debrief/review kinds), SDD153 (an approved
// artifact's Open Questions must be resolved or marked non-blocking), and
// SDD080/SDD081/SDD082 (review `review_of`/`findings`/`followups` shape).

func init() {
	Register(&Rule{
		Code: "SDD050", Severity: Error, PyFunc: "_specific",
		What: "a spec defines no `FR-NN`/`NFR-NN`/`AC-NN` element",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "spec" {
				return
			}
			ids := specDefinedIDs(a)
			for _, family := range []string{"FR", "NFR", "AC"} {
				if len(ids[family]) > 0 {
					continue
				}
				emit(Diagnostic{
					Code: "SDD050", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Spec defines no `" + family + "-NN` element.",
					Correction: "Number applicable elements with stable `" + family + "-NN` ids.",
				})
			}
		},
		Bad: []Example{{Name: "no-requirements", Files: map[string]string{
			"Specs/Sample/README.md": strings.Replace(validSpecTemplate, "- **FR-01**: Does a thing.\n", "", 1),
		}}},
		Good: []Example{{Name: "has-requirements", Files: map[string]string{
			"Specs/Sample/README.md": validSpecTemplate,
		}}},
	})

	Register(&Rule{
		Code: "SDD051", Severity: Error, PyFunc: "_specific",
		What: "a phase/debrief/review artifact is missing one of its kind-specific required fields",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			var fields []string
			switch a.Kind() {
			case "phase":
				fields = []string{"plan", "phase", "deliverable"}
			case "debrief":
				fields = []string{"plan", "phase", "phase_title"}
			case "review":
				fields = []string{"review_of", "rev"}
			default:
				return
			}
			for _, field := range fields {
				if !isEmptyMeta(a.Meta[field]) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD051", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Required `" + a.Kind() + "` field `" + field + "` is missing.",
					Correction: "Add a nonempty `" + field + "` value.",
				})
			}
		},
		Bad: []Example{{Name: "debrief-missing-phase", Files: map[string]string{
			"Retro/sample-debrief.md": debriefMissingField("phase"),
		}}},
		Good: []Example{{Name: "debrief-complete", Files: map[string]string{
			"Retro/sample-debrief.md": validDebrief(),
		}}},
	})

	Register(&Rule{
		Code: "SDD153", Severity: Error, PyFunc: "_open_questions",
		What: "an approved/active spec, design, or plan has a blocking or unexplained open question",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			gated := (a.Kind() == "spec" || a.Kind() == "design") && (a.Status() == "approved" || a.Status() == "implemented")
			gated = gated || (a.Kind() == "plan" && (a.Status() == "approved" || a.Status() == "active" || a.Status() == "complete"))
			if !gated {
				return
			}
			secs := sections(a, 2)
			info, ok := secs["Open Questions"]
			if !ok {
				return
			}
			for _, question := range openQuestionItems(info.Body) {
				markers := nonBlockingRe.FindAllStringIndex(question, -1)
				var prompt, rationale string
				valid := len(markers) == 1
				if valid {
					m := markers[0]
					prompt = strings.Trim(strings.TrimSpace(question[:m[0]]), " \t:—-")
					rationale = strings.Trim(strings.TrimSpace(question[m[1]:]), " \t:—-")
					valid = prompt != "" && rationale != ""
				}
				if !valid {
					emit(Diagnostic{
						Code: "SDD153", Severity: Error, Path: a.Rel, Line: info.Line,
						Message:    "Approved artifact contains a blocking or unexplained open question.",
						Correction: "Resolve it or mark the bullet `**non-blocking** — <rationale>`.",
					})
				}
			}
		},
		Bad: []Example{{Name: "blocking-question", Files: map[string]string{
			"Specs/Sample/README.md": strings.Replace(
				strings.Replace(validSpecTemplate, "status: draft", "status: approved", 1),
				"## Open Questions\n\nNone.\n", "## Open Questions\n\n- Should we do this at all?\n", 1),
		}}},
		Good: []Example{{Name: "non-blocking-question", Files: map[string]string{
			"Specs/Sample/README.md": strings.Replace(
				strings.Replace(validSpecTemplate, "status: draft", "status: approved", 1),
				"## Open Questions\n\nNone.\n", "## Open Questions\n\n- Should we do this? **non-blocking** — deferred to phase 2.\n", 1),
		}}},
	})

	Register(&Rule{
		Code: "SDD080", Severity: Error, PyFunc: "_review",
		What: "a review's `review_of` does not resolve",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "review" {
					continue
				}
				reviewOf, ok := a.Meta["review_of"].(string)
				if !ok || resolveRelated(r, reviewOf) != nil {
					continue
				}
				emit(Diagnostic{
					Code: "SDD080", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "`review_of` `" + reviewOf + "` does not resolve.",
					Correction: "Point it at the reviewed artifact.",
				})
			}
		},
		Bad: []Example{{Name: "unresolved-review-of", Files: map[string]string{
			"Retro/sample-review.md": validReview(`review_of: "Specs/Nope/README.md"`),
		}}},
		Good: []Example{{Name: "resolved-review-of", Files: map[string]string{
			"Retro/sample-review.md": validReview(`review_of: "Specs/Sample/README.md"`),
			"Specs/Sample/README.md": validSpecTemplate,
		}}},
	})

	Register(&Rule{
		Code: "SDD081", Severity: Error, PyFunc: "_review",
		What: "a review's `findings` frontmatter field is not a list",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			if _, ok := a.Meta["findings"].([]any); ok {
				return
			}
			emit(Diagnostic{
				Code: "SDD081", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "`findings` must be a list.",
				Correction: "Use `findings: []` when empty.",
			})
		},
		Bad: []Example{{Name: "findings-not-list", Files: map[string]string{
			"Retro/sample-review.md": validReview(`findings: none`),
		}}},
		Good: []Example{{Name: "findings-list", Files: map[string]string{
			"Retro/sample-review.md": validReview(""),
		}}},
	})

	Register(&Rule{
		Code: "SDD082", Severity: Error, PyFunc: "_review",
		What: "a review's `followups` frontmatter field is not a list",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "review" {
				return
			}
			if _, ok := a.Meta["findings"].([]any); !ok {
				return // SDD081's finding; don't double-report
			}
			if _, ok := a.Meta["followups"].([]any); ok {
				return
			}
			emit(Diagnostic{
				Code: "SDD082", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "`followups` must be a list.",
				Correction: "Use `followups: []` when empty.",
			})
		},
		Bad: []Example{{Name: "followups-not-list", Files: map[string]string{
			"Retro/sample-review.md": validReview(`followups: none`),
		}}},
		Good: []Example{{Name: "followups-list", Files: map[string]string{
			"Retro/sample-review.md": validReview(""),
		}}},
	})
}
