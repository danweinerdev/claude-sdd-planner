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

	Register(&Rule{
		// SDD157 is Python's code for two unrelated _headings/_completed_identities
		// findings (a legacy rollup heading, and a complete phase's missing/wrong
		// `### Completed task identities` section) — both folded into this one
		// Check since Register panics on a second Rule claiming the same code.
		Code: "SDD157", Severity: Error, PyFunc: "_headings",
		What: "a phase carries a legacy `### ... Evidence Rollup` heading, or a complete phase's `### Completed task identities` section is missing or wrong",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "phase" {
				return
			}
			visible := visibleMarkdown(a.Body)
			if loc := legacyEvidenceRollupRe.FindStringIndex(visible); loc != nil {
				line := a.BodyLine + strings.Count(visible[:loc[0]], "\n")
				emit(Diagnostic{
					Code: "SDD157", Severity: Error, Path: a.Rel, Line: line,
					Message:    "Legacy `Evidence Rollup` headings are not permitted.",
					Correction: "Replace the rollup with the required concise completed-identity section.",
				})
			}
			completedTaskIdentitiesCheck(a, emit)
		},
		Bad: []Example{
			{Name: "legacy-rollup", Files: map[string]string{
				"Plans/Sample/01-One.md": strings.Replace(validPhase(), "## Phase Completion Evidence", "### Evidence Rollup\n\nOld-style rollup text.\n\n## Phase Completion Evidence", 1),
			}},
			{Name: "missing-completed-task-identities", Files: map[string]string{
				"Plans/Sample/01-One.md": checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
			}},
		},
		Good: []Example{{Name: "no-legacy-heading", Files: map[string]string{
			"Plans/Sample/01-One.md": validPhase(),
		}}},
	})
}

var completedTaskIdentityRe = regexp.MustCompile("^\\s*-\\s+`([^`\\s]+)`: `([^`;]+)`\\s*$")

// completedTaskIdentitiesCheck ports Validator._completed_identities' `kind
// == "task"` call from _phase: a complete phase must carry exactly one
// `### Completed task identities` section whose entries exactly match every
// completed task's recorded `Revision / checkpoint`, one line each, in the
// `- \`<task id>\`: \`<checkpoint>\`` shape.
func completedTaskIdentitiesCheck(a *Artifact, emit func(Diagnostic)) {
	if a.Status() != "complete" {
		return
	}
	tasks, ok := a.Meta["tasks"].([]any)
	if !ok {
		return
	}
	secs := sections(a, 2)
	phaseEvidence, ok := secs["Phase Completion Evidence"]
	if !ok {
		return
	}
	expected := map[string]string{}
	for _, t := range tasks {
		m := planEntry(t)
		if m == nil {
			continue
		}
		if metaStr(m, "status") != "complete" {
			continue
		}
		id := metaStr(m, "id")
		heading := taskHeadingFor(secs, id)
		revision := ""
		if heading != "" {
			blocks := headingBodies(secs[heading].Body, 3, "Completion Evidence")
			if len(blocks) == 1 {
				revision = markdownScalar(evidenceValue(blocks[0], "Revision / checkpoint"))
			}
		}
		expected[id] = revision
	}
	blocks := headingBodies(phaseEvidence.Body, 3, "Completed task identities")
	invalid := len(blocks) != 1
	entries := map[string]string{}
	if len(blocks) == 1 {
		for _, line := range strings.Split(blocks[0], "\n") {
			m := completedTaskIdentityRe.FindStringSubmatch(line)
			if m == nil {
				invalid = true
				continue
			}
			if _, dup := entries[m[1]]; dup {
				invalid = true
			}
			entries[m[1]] = m[2]
		}
	}
	if len(entries) != len(expected) {
		invalid = true
	}
	for id := range entries {
		if _, ok := expected[id]; !ok {
			invalid = true
		}
	}
	for id, v := range expected {
		if entries[id] != v {
			invalid = true
		}
	}
	if invalid {
		emit(Diagnostic{
			Code: "SDD157", Severity: Error, Path: a.Rel, Line: phaseEvidence.Line,
			Message:    "Completed task identities must contain one exact identity entry for every completed task.",
			Correction: "Use exactly one `### Completed task identities` section with each completed task's checkpoint.",
		})
	}
}
