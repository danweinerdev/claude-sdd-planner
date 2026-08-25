package rules

import (
	"path"
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
		// SDD158 is Python's code for two unrelated checks, the plan-side
		// mirror of SDD157: a legacy Evidence Rollup heading (_headings), and
		// a complete plan's `### Completed phase identities` section
		// (_completed_identities). Both folded into this one registration,
		// since one code is one rule.
		Code: "SDD158", Severity: Error, PyFunc: "_headings",
		What: "a plan carries a legacy `### ... Evidence Rollup` heading, or a complete plan's `### Completed phase identities` section is missing or wrong",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "plan" {
					continue
				}
				visible := visibleMarkdown(a.Body)
				if loc := legacyEvidenceRollupRe.FindStringIndex(visible); loc != nil {
					line := a.BodyLine + strings.Count(visible[:loc[0]], "\n")
					emit(Diagnostic{
						Code: "SDD158", Severity: Error, Path: a.Rel, Line: line,
						Message:    "Legacy `Evidence Rollup` headings are not permitted.",
						Correction: "Replace the rollup with the required concise completed-identity section.",
					})
				}
				completedPhaseIdentitiesCheck(r, a, emit)
			}
		},
		Bad: []Example{
			{Name: "legacy-rollup", Files: map[string]string{
				"Plans/Sample/README.md": validPlan(true),
			}},
			{Name: "missing-completed-phase-identities", Files: map[string]string{
				"Plans/Sample/README.md": replaceFirst(
					replaceFirst(planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: complete
    doc: 01-One.md
`), "status: draft", "status: complete"),
					"## Plan Completion Evidence\n\nPending — not complete.",
					"## Plan Completion Evidence\n\n- Verified: 2024-01-01\n"),
				"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", " []"),
			}},
		},
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
		Good: []Example{
			{Name: "no-legacy-heading", Files: map[string]string{
				"Plans/Sample/01-One.md": validPhase(),
			}},
			// The layout the tool's own workflow produces: identity entries
			// (optionally annotated), a blank separator, and the `Final
			// aligned review` entry that `review resolve`'s completion hint
			// appends to the same section — all inside the depth-3 block
			// because no heading follows. None of it may invalidate.
			{Name: "conventional-identities-layout", Files: map[string]string{
				"Plans/Sample/01-One.md": strings.Replace(
					strings.Replace(checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
						"### Completion Evidence\n\nPending — not complete.",
						"### Completion Evidence\n\n- Revision / checkpoint: `1111111111111111111111111111111111111111`",
						1),
					"## Phase Completion Evidence\n\nPending — not complete.",
					"## Phase Completion Evidence\n\n"+
						"- Verified: 2024-01-02\n\n"+
						"### Completed task identities\n"+
						"- `1.1`: `1111111111111111111111111111111111111111` (task fix commit)\n\n"+
						"- Final aligned review: `reviews/01-sample-code-review-abc.md`; frozen: `a..b`\n",
					1),
			}},
		},
	})
}

// completedTaskIdentityRe accepts an optional trailing parenthetical
// annotation after the checkpoint — "- `1.1`: `<sha>` (pull-forward series
// endpoint)" is the established completed-phase convention and the
// annotation carries no identity content.
var completedTaskIdentityRe = regexp.MustCompile(
	"^\\s*-\\s+`([^`\\s]+)`: `([^`;]+)`(?:\\s+\\([^()]*\\))?\\s*$")

// identityCheckpoint extracts the checkpoint token from a `Revision /
// checkpoint` evidence value for identity comparison: the whole value when
// it is a single backticked scalar, else the LEADING backticked token when
// the value carries a trailing annotation — the established convention for
// pull-forward and re-verification notes, e.g.
// "`<sha>` (pull-forward series `a..b`); continuously re-verified through
// `<sha2>`". Without this, an annotated checkpoint could never match its
// (bare) identity entry.
func identityCheckpoint(value string, ok bool) string {
	if !ok {
		return ""
	}
	v := strings.TrimSpace(value)
	if strings.HasPrefix(v, "`") {
		if end := strings.Index(v[1:], "`"); end >= 0 {
			return strings.TrimSpace(v[1 : 1+end])
		}
	}
	return markdownScalar(value, ok)
}

// finalAlignedReviewLineRe recognizes the `Final aligned review` evidence
// entry that SDD166 requires in the SAME `Phase Completion Evidence`
// section — `review resolve`'s own completion hint tells the author to
// append it there, which by convention lands after the identity entries
// and therefore INSIDE the `### Completed task identities` block (the
// block runs to the next heading). The identity scan must tolerate it, or
// the two rules contradict each other on the layout the tool itself
// produces.
var finalAlignedReviewLineRe = regexp.MustCompile(`^\s*-\s+Final aligned review:`)

// completedTaskIdentitiesCheck ports Validator._completed_identities' `kind
// == "task"` call from _phase: a complete phase must carry exactly one
// `### Completed task identities` section whose entries exactly match every
// completed task's recorded `Revision / checkpoint`, one line each, in the
// "- `<task id>`: `<checkpoint>`" shape. Blank lines, a trailing
// parenthetical annotation on an entry, and the section's `Final aligned
// review` entry (see finalAlignedReviewLineRe) are tolerated; any other
// non-entry line still invalidates.
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
				revision = identityCheckpoint(evidenceValue(blocks[0], "Revision / checkpoint"))
			}
		}
		expected[id] = revision
	}
	blocks := headingBodies(phaseEvidence.Body, 3, "Completed task identities")
	invalid := len(blocks) != 1
	entries := map[string]string{}
	if len(blocks) == 1 {
		for _, line := range strings.Split(blocks[0], "\n") {
			if strings.TrimSpace(line) == "" || finalAlignedReviewLineRe.MatchString(line) {
				continue
			}
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

// completedPhaseIdentityRe is the phase form of the identity line: a phase
// records both its checkpoint and the final review that closed it, where a
// task records only a checkpoint. Like the task form, an optional trailing
// parenthetical annotation is tolerated.
var completedPhaseIdentityRe = regexp.MustCompile(
	"^\\s*-\\s+`([^`\\s]+)`: `([^`;]+)`; review: `([^`;]+)`(?:\\s+\\([^()]*\\))?\\s*$")

// completedPhaseIdentitiesCheck is SDD158's second emission site, the plan-side
// analogue of completedTaskIdentitiesCheck. Python reaches both through one
// _completed_identities helper and picks the code from `kind`; here they are
// two functions because the expected values differ in shape — a phase's entry
// is (checkpoint, review path), a task's is a checkpoint alone.
func completedPhaseIdentitiesCheck(r *Root, a *Artifact, emit func(Diagnostic)) {
	if a.Status() != "complete" {
		return
	}
	phases, ok := a.Meta["phases"].([]any)
	if !ok {
		return
	}
	planEvidence, ok := sections(a, 2)["Plan Completion Evidence"]
	if !ok {
		return
	}

	type phaseIdentity struct{ checkpoint, review string }
	expected := map[string]phaseIdentity{}
	for _, p := range phases {
		m := planEntry(p)
		if m == nil || metaStr(m, "status") != "complete" {
			continue
		}
		doc, ok := m["doc"].(string)
		if !ok {
			continue
		}
		target := r.ByPath[path.Join(path.Dir(a.Rel), doc)]
		if target == nil {
			continue
		}
		body := sections(target, 2)["Phase Completion Evidence"].Body
		review := ""
		if reviewPath, _, ok := parseFinalAlignedReview(
			markdownScalar(evidenceValue(body, "Final aligned review")), true); ok {
			review = reviewPath
		}
		expected[metaStr(m, "id")] = phaseIdentity{
			checkpoint: identityCheckpoint(evidenceValue(body, "Revision / checkpoint")),
			review:     review,
		}
	}

	blocks := headingBodies(planEvidence.Body, 3, "Completed phase identities")
	invalid := len(blocks) != 1
	entries := map[string]phaseIdentity{}
	if len(blocks) == 1 {
		for _, line := range strings.Split(blocks[0], "\n") {
			if strings.TrimSpace(line) == "" || finalAlignedReviewLineRe.MatchString(line) {
				continue
			}
			m := completedPhaseIdentityRe.FindStringSubmatch(line)
			if m == nil {
				invalid = true
				continue
			}
			if _, dup := entries[m[1]]; dup {
				invalid = true
			}
			entries[m[1]] = phaseIdentity{checkpoint: m[2], review: m[3]}
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
			Code: "SDD158", Severity: Error, Path: a.Rel, Line: planEvidence.Line,
			Message:    "Completed phase identities must contain one exact identity entry for every completed phase.",
			Correction: "Use exactly one `### Completed phase identities` section with each completed phase's checkpoint and final review path.",
		})
	}
}
