package rules

import "regexp"

// Family (i cont'd): Validator._brainstorm — SDD078, the sole candidate
// (non-blocking) diagnostic ported so far: a brainstorm that never considers
// a do-nothing/status-quo baseline. Idea 0 is the baseline every other idea
// has to beat; omitting it is legitimate when inaction is genuinely
// impossible, so this is a candidate finding rather than an error.

// baselineIdeaRe ports sdd_validate.py's BASELINE_IDEA: a `###` heading that
// either numbers a zero-slot idea/option/approach, or leads with a
// do-nothing/status-quo phrase (optionally behind a short label like
// "Baseline:" or "Idea A -").
var baselineIdeaRe = regexp.MustCompile(`(?im)^[ \t]{0,3}###[ \t]+(?:(?:idea|option|approach)[ \t]*0\b|(?:[^\n:—-]{0,24}[:—-][ \t]*)?(?:do[ \t-]*nothing|status[ \t-]*quo|no[ \t]+change|leave[ \t]+(?:it|as)[ \t]+is|baseline)\b)`)

func init() {
	Register(&Rule{
		Code: "SDD078", Severity: Candidate, PyFunc: "_brainstorm",
		What: "a brainstorm's `## Ideas` section never considers a do-nothing baseline",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "brainstorm" {
				return
			}
			ideas, ok := sections(a, 2)["Ideas"]
			if !ok {
				return // A missing `## Ideas` section is SDD020's finding.
			}
			if baselineIdeaRe.MatchString(ideas.Body) {
				return
			}
			emit(Diagnostic{
				Code: "SDD078", Severity: Candidate, Path: a.Rel, Line: ideas.Line,
				Message: "Brainstorm considers no do-nothing baseline.",
				Correction: "Add `### Idea 0: Do nothing / status quo` and evaluate it on the same " +
					"criteria — it is the baseline the other ideas must beat. Omit it only " +
					"when inaction is impossible (hard deadline, compliance obligation, " +
					"active outage), and say which in the Recommendation.",
			})
		},
		Bad: []Example{{Name: "no-baseline", Files: map[string]string{
			"Brainstorm/sample.md": validBrainstorm(`### Idea 1: Build a widget

Text.
`),
		}}},
		Good: []Example{{Name: "has-baseline", Files: map[string]string{
			"Brainstorm/sample.md": validBrainstorm(`### Idea 0: Do nothing / status quo

Text.

### Idea 1: Build a widget

Text.
`),
		}}},
	})
}
