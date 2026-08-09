package rules

import (
	"regexp"
	"strings"
)

// requiredHeadings mirrors sdd_validate.py's REQUIRED_HEADINGS.
var requiredHeadings = map[string][]string{
	"research":   {"Context", "Findings", "Analysis", "Open Questions"},
	"brainstorm": {"Problem Statement", "Ideas", "Evaluation", "Next Steps"},
	"spec":       {"Overview", "Goals", "Non-Goals", "Requirements", "User Stories", "Acceptance Criteria", "Constraints", "Dependencies", "Open Questions"},
	"design":     {"Overview", "Non-Goals", "Architecture", "Design Decisions", "Error Handling", "Testing Strategy", "Migration / Rollout"},
	"plan":       {"Overview", "Non-Goals", "Architecture", "Key Decisions", "Dependencies", "Plan Completion Evidence"},
	"phase":      {"Overview", "Acceptance Criteria", "Phase Completion Evidence"},
	"review":     {"Findings", "Resolution Log"},
	"debrief":    {"Decisions Made", "Requirements Assessment", "Deviations", "Risks & Issues Encountered", "Lessons Learned", "Impact on Subsequent Phases", "Skill Opportunities"},
}

var legacyEvidenceRollupRe = regexp.MustCompile(`(?im)^ {0,3}#{3}[ \t]+.*\bEvidence[ \t]+Rollup\b(?:[ \t]+#+)?[ \t]*$`)

func init() {
	Register(&Rule{
		Code: "SDD020", Severity: Error, PyFunc: "_headings",
		What: "a required `##` section is missing, or a plan/phase's completion-evidence section is duplicated",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			secs := sections(a, 2)
			for _, heading := range requiredHeadings[a.Kind()] {
				if _, ok := secs[heading]; !ok {
					emit(Diagnostic{
						Code: "SDD020", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Required section `## " + heading + "` is missing.",
						Correction: "Add a nonempty `## " + heading + "` section.",
					})
				}
			}
			if a.Kind() != "plan" && a.Kind() != "phase" {
				return
			}
			heading := "Plan Completion Evidence"
			if a.Kind() == "phase" {
				heading = "Phase Completion Evidence"
			}
			evidenceSections := headingBodies(a.Body, 2, heading)
			if len(evidenceSections) > 1 {
				emit(Diagnostic{
					Code: "SDD020", Severity: Error, Path: a.Rel, Line: a.Line("## "+heading, true),
					Message:    "Parent completion-evidence section `## " + heading + "` is duplicated.",
					Correction: "Keep exactly one visible `## " + heading + "` section.",
				})
			}
		},
		Bad: []Example{{Name: "missing-section", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "## Analysis\n\nText.\n\n", "", 1),
		}}},
		Good: []Example{{Name: "all-sections", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD158", Severity: Error, PyFunc: "_headings",
		What: "a plan carries a legacy `### ... Evidence Rollup` heading",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "plan" {
				return
			}
			visible := visibleMarkdown(a.Body)
			loc := legacyEvidenceRollupRe.FindStringIndex(visible)
			if loc == nil {
				return
			}
			line := a.BodyLine + strings.Count(visible[:loc[0]], "\n")
			emit(Diagnostic{
				Code: "SDD158", Severity: Error, Path: a.Rel, Line: line,
				Message:    "Legacy `Evidence Rollup` headings are not permitted.",
				Correction: "Replace the rollup with the required concise completed-identity section.",
			})
		},
		Bad: []Example{{Name: "legacy-rollup", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(true),
		}}},
		Good: []Example{{Name: "no-legacy-heading", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
		}}},
	})
}
