package rules

import "strings"

// Shared example fixtures. Kept minimal but genuinely valid, so a rule's Good
// example proves the rule stays quiet on ordinary artifacts rather than on
// something contrived.

const validResearch = `---
title: Sample Research
type: research
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Context

Text.

## Findings

Text.

## Analysis

Text.

## Open Questions

None.
`

const validSpecTemplate = `---
title: Sample Spec
type: spec
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Overview

Text.

## Goals

Text.

## Non-Goals

Text.

## Requirements

- **FR-01**: Does a thing.

## User Stories

Text.

## Acceptance Criteria

- [ ] **AC-01**: Verifies the thing.

## Constraints

- **NFR-01**: Is fast.

## Dependencies

None.

## Open Questions

None.
`

// validBrainstorm returns a minimal, structurally valid brainstorm document
// whose `## Ideas` section body is the caller-supplied text.
func validBrainstorm(ideasBody string) string {
	return `---
title: Sample Brainstorm
type: brainstorm
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Problem Statement

Text.

## Ideas

` + ideasBody + `
## Evaluation

Text.

## Next Steps

Text.
`
}

// validPlan returns a minimal, structurally valid plan README. When legacy is
// true it also carries a legacy `### ... Evidence Rollup` heading (SDD158).
func validPlan(legacy bool) string {
	extra := ""
	if legacy {
		extra = "\n### Evidence Rollup\n\nOld-style rollup text.\n"
	}
	return `---
title: Sample Plan
type: plan
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
phases: []
---

## Overview

Text.

## Non-Goals

Text.

## Architecture

Text.

## Key Decisions

Text.
` + extra + `
## Dependencies

None.

## Plan Completion Evidence

Pending — not complete.
`
}

// planWithPhasesRaw returns a plan README whose `phases:` frontmatter block is
// the caller-supplied raw YAML (including the `phases:` key itself).
func planWithPhasesRaw(phasesBlock string) string {
	return `---
title: Sample Plan
type: plan
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
` + phasesBlock + `---

## Overview

Text.

## Non-Goals

Text.

## Architecture

Text.

## Key Decisions

Text.

## Dependencies

None.

## Plan Completion Evidence

Pending — not complete.
`
}

// planWithPhase returns a plan README with a single phases[] entry built from
// fields (id/title/status/doc, any subset). The trailing args are accepted
// and ignored so call sites can note the phase doc path they expect to exist
// alongside it in the example's Files map.
func planWithPhase(fields map[string]string, _ ...string) string {
	block := "phases:\n  - "
	first := true
	for _, k := range []string{"id", "title", "status", "doc"} {
		v, ok := fields[k]
		if !ok {
			continue
		}
		if !first {
			block += "    "
		}
		block += k + ": " + quoteIfNumeric(v) + "\n"
		first = false
	}
	return planWithPhasesRaw(block)
}

func quoteIfNumeric(v string) string {
	for _, c := range v {
		if c < '0' || c > '9' {
			return v
		}
	}
	if v == "" {
		return v
	}
	return `"` + v + `"`
}

// planStatus returns a plan README with the given plan status and a single
// phases[] entry built from fields.
func planStatus(status string, fields map[string]string) string {
	return replaceFirst(planWithPhase(fields), "status: draft", "status: "+status)
}

func replaceFirst(s, old, new string) string {
	idx := indexOf(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// phaseDoc returns a minimal, structurally valid phase document for the given
// plan, phase id, title, and status.
func phaseDoc(planName, phaseID, title, status string) string {
	return `---
title: ` + title + `
type: phase
status: ` + status + `
created: 2024-01-01
updated: 2024-01-01
plan: ` + planName + `
phase: "` + phaseID + `"
deliverable: A thing.
tasks: []
---

## Overview

Text.

## Acceptance Criteria

- [ ] Works.

## Phase Completion Evidence

Pending — not complete.
`
}

// phaseWithTasks returns a minimal, structurally valid phase document whose
// `tasks:` block is the caller-supplied raw YAML (indented block sequence).
// phaseWithTasks returns a minimal, structurally valid phase document. opts,
// when given, are (omitBodySection, omitNotesSubsection) — both default false.
func phaseWithTasks(phaseID, planName, tasksBlock string, opts ...bool) string {
	omitBodySection := len(opts) > 0 && opts[0]
	omitNotes := len(opts) > 1 && opts[1]

	taskSection := ""
	if !omitBodySection {
		notes := "### Notes\n\nNone.\n\n"
		if omitNotes {
			notes = ""
		}
		taskSection = `
## ` + phaseID + `.1: First

### Subtasks

- [ ] Step.

` + notes + `### Completion Evidence

Pending — not complete.
`
	}

	return `---
title: Sample Phase
type: phase
status: planned
created: 2024-01-01
updated: 2024-01-01
plan: ` + planName + `
phase: "` + phaseID + `"
deliverable: A thing.
tasks:` + tasksBlock + `
---

## Overview

Text.

## Acceptance Criteria

- [ ] Works.
` + taskSection + `
## Phase Completion Evidence

Pending — not complete.
`
}

// phaseWithTasksRaw returns a phase document whose `tasks:` frontmatter block
// is the caller-supplied raw YAML (including the `tasks:` key itself).
func phaseWithTasksRaw(tasksBlock string) string {
	return `---
title: Sample Phase
type: phase
status: planned
created: 2024-01-01
updated: 2024-01-01
plan: Sample
phase: "1"
deliverable: A thing.
` + tasksBlock + `---

## Overview

Text.

## Acceptance Criteria

- [ ] Works.

## Phase Completion Evidence

Pending — not complete.
`
}

// phaseStatus returns a phase document with the given phase status and
// task list, unchecked acceptance criteria (so SDD069 examples control it
// via checkedPhase instead).
func phaseStatus(status, phaseID, planName, tasksBlock string) string {
	return replaceFirst(phaseWithTasks(phaseID, planName, tasksBlock), "status: planned", "status: "+status)
}

// checkedPhase is phaseStatus with its Acceptance Criteria and Subtasks boxes
// checked.
func checkedPhase(status, phaseID, planName, tasksBlock string) string {
	s := phaseStatus(status, phaseID, planName, tasksBlock)
	s = replaceFirst(s, "- [ ] Works.", "- [x] Works.")
	s = replaceFirst(s, "- [ ] Step.", "- [x] Step.")
	return s
}

// validPhase returns a minimal, structurally valid phase document.
func validPhase() string {
	return `---
title: Sample Phase
type: phase
status: planned
created: 2024-01-01
updated: 2024-01-01
plan: Sample
phase: "1"
deliverable: A thing.
tasks: []
---

## Overview

Text.

## Acceptance Criteria

- [ ] Works.

## Phase Completion Evidence

Pending — not complete.
`
}

// validDebrief returns a minimal, structurally valid debrief document.
func validDebrief() string {
	return `---
title: Sample Debrief
type: debrief
status: draft
created: 2024-01-01
updated: 2024-01-01
plan: Sample
phase: "1"
phase_title: Sample Phase
---

## Decisions Made

Text.

## Requirements Assessment

Text.

## Deviations

None.

## Risks & Issues Encountered

None.

## Lessons Learned

Text.

## Impact on Subsequent Phases

None.

## Skill Opportunities

None.
`
}

// debriefMissingField returns validDebrief() with the given top-level
// frontmatter field's line removed.
func debriefMissingField(field string) string {
	lines := strings.Split(validDebrief(), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.HasPrefix(l, field+":") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// validReview returns a minimal, structurally valid review document.
// extraFrontmatter, when nonempty, replaces the default `review_of`/`findings`
// lines so callers can exercise one field at a time; it must supply whichever
// of "review_of:"/"findings:"/"followups:" it wants to override.
func validReview(extraFrontmatter string) string {
	reviewOf := `review_of: "Specs/Sample/README.md"`
	findings := "findings: []"
	followups := "followups: []"
	for _, line := range strings.Split(extraFrontmatter, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "review_of:"):
			reviewOf = line
		case strings.HasPrefix(line, "findings:"):
			findings = line
		case strings.HasPrefix(line, "followups:"):
			followups = line
		}
	}
	return `---
title: Sample Review
type: review
status: open
created: 2024-01-01
updated: 2024-01-01
` + reviewOf + `
rev: 1
` + findings + `
` + followups + `
---

## Findings

None.

## Resolution Log

None.
`
}

// AnchorArtifact is a minimal artifact known to produce no diagnostics, used
// by tools/genfixtures to make a generated fixture root discoverable. It is
// the same document the rules' own Good examples use, exported rather than
// copied so a schema change cannot leave the parity corpus anchored to a
// document that no longer validates clean.
const AnchorArtifact = validResearch

// --- phase-completion review gate fixtures ---------------------------------
//
// The gate only runs on a phase that is `complete` and carries a Phase
// Completion Evidence section, so these build that state. They deliberately do
// not try to satisfy every other evidence rule: the parity oracle compares
// per-code, so unrelated diagnostics from the same fixture are matched on both
// sides and do not obscure the code under test.

// completePhaseNoReview is a complete phase whose evidence section carries no
// `Final aligned review` entry at all.
func completePhaseNoReview() string {
	return replaceFirst(
		checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
		"## Phase Completion Evidence\n\nPending — not complete.",
		"## Phase Completion Evidence\n\n- Reviewed by: someone\n")
}

// phaseGateReview returns a phase-completion review artifact. aligned=false
// flips `verdict` so the review stops satisfying the four-lane gate while
// still resolving as a review artifact.
func phaseGateReview(rev string, aligned bool) string {
	verdict := "Aligned"
	if !aligned {
		verdict = "Diverged"
	}
	lane := func(name string) string {
		return `
  - lane: ` + name + `
    result: PASS/Aligned
    reviewed_identity: "` + rev + `"
    evidence: Checked the migration ordering in store/atomic.go.`
	}
	return `---
title: Phase Review
type: review
status: resolved
created: 2024-01-01
updated: 2024-01-01
review_of: "Plans/Sample/01-One.md"
review_scope: phase
frozen: true
verdict: ` + verdict + `
rev: "` + rev + `"
reviewed_planning_revision: "1111111111111111111111111111111111111111"
review_mode: independent
lane_results:` + lane("review_plan_drift") + lane("review_quality") +
		lane("review_spec_compliance") + lane("review_blind_spots") + `
findings: []
followups: []
tags: []
related: []
---

## Findings

None.

## Resolution Log

None.
`
}

// phaseGateFiles builds a complete phase plus its final review.
// revMatches=false makes the evidence entry's `frozen:` disagree with the
// review's `rev` (SDD168); aligned=false makes the review fail the four-lane
// gate (SDD167).
func phaseGateFiles(revMatches, aligned bool) map[string]string {
	const rev = "r-2024-01-01-01"
	frozen := rev
	if !revMatches {
		frozen = "r-9999-12-31-99"
	}
	phase := replaceFirst(
		checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
		"## Phase Completion Evidence\n\nPending — not complete.",
		"## Phase Completion Evidence\n\n- Final aligned review: Retro/phase-review.md; frozen: "+frozen+"\n")
	return map[string]string{
		"Plans/Sample/01-One.md": phase,
		"Retro/phase-review.md":  phaseGateReview(rev, aligned),
	}
}

// fixtureBaseCommit is the SHA of a first commit containing only code.txt,
// under rules_test.go's fixed author/committer identity and timestamp. It is
// reproducible because its content does not depend on any file that embeds a
// SHA — which is why the range fixture below freezes on this commit rather
// than on one containing the phase documents.
const fixtureBaseCommit = "84cbb4f2f837120e7d06649764d28e51b60faa2b"

// phaseGateRangeFiles is phaseGateFiles whose frozen identity is a full 40-hex
// Git range, which parseGitFrozenIdentity requires before the post-review
// state gate (SDD173) engages at all.
//
// It carries a non-lifecycle file (code.txt) so that a later commit touching
// it is material rather than permitted lifecycle bookkeeping.
func phaseGateRangeFiles() map[string]string {
	rangeID := fixtureBaseCommit + ".." + fixtureBaseCommit
	files := map[string]string{
		"code.txt":              "code\n",
		"Retro/phase-review.md": phaseGateReview(rangeID, true),
	}
	files["Plans/Sample/01-One.md"] = replaceFirst(
		replaceFirst(
			checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
			"## Phase Completion Evidence\n\nPending — not complete.",
			"## Phase Completion Evidence\n\n- Final aligned review: Retro/phase-review.md; frozen: "+rangeID+"\n"),
		"", "")
	return files
}

// withPlanReadme adds the plan README a phase's gate rules need to resolve,
// so a fixture exercises the branch under test rather than stopping at the
// missing-plan check.
func withPlanReadme(files map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		out[k] = v
	}
	out["Plans/Sample/README.md"] = validPlan(false)
	return out
}

// validDesign returns a structurally valid design document whose Architecture
// body is the caller's text, which is what the design-side traceability
// search reads.
func validDesign(architecture string) string {
	return `---
title: Sample Design
type: design
status: draft
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
---

## Overview

Text.

## Non-Goals

None.

## Architecture

` + architecture + `

### Components

Text.

### Data Flow

Text.

### Interfaces

Text.

## Design Decisions

Text.

## Error Handling

Text.

## Testing Strategy

Text.

### Structural Verification

Text.

## Migration / Rollout

Text.

## Open Questions

None.
`
}

// designWithDecisions returns a valid design whose `## Design Decisions`
// section carries the caller's text — the DD ids SDD122 resolves against.
func designWithDecisions(decisions string) string {
	return replaceFirst(validDesign("Text."),
		"## Design Decisions\n\nText.\n", "## Design Decisions\n\n"+decisions)
}

// --- graph-conversion fixtures (SDD156/SDD164 exemption) --------------------
//
// The append-only rules exempt a v1 PHASE document whose removal a deliberate
// graph conversion explains: the owning same-plan graph accounts for every
// task id (live node or retired register) and the plan README declares a
// generated replacement view. These helpers build that state so the Good/Bad
// examples exercise the exemption instead of hand-rolled YAML.

// graphNode renders one strictly-valid committed-graph node.
func graphNode(id string) string {
	return `{"id":"` + id + `","contract":"contract for ` + id + `","gate":{"type":"tests","tests":[{"id":"t-` + id + `","file":"t.ext"}]},"hazards":[],"estimate":1}`
}

// graphPlanJSON renders a committed graph (schema v1) with the given live node
// ids and retired ids. The node shape is strictly decodable — the same shape
// the model's DecodeGraph demands.
func graphPlanJSON(live, retired []string) string {
	parts := make([]string, 0, len(live))
	for _, id := range live {
		parts = append(parts, graphNode(id))
	}
	out := `{"version":1,"seq_counter":0,"nodes":[` + strings.Join(parts, ",") + `]`
	if len(retired) > 0 {
		quoted := make([]string, 0, len(retired))
		for _, id := range retired {
			quoted = append(quoted, `"`+id+`"`)
		}
		out += `,"retired":[` + strings.Join(quoted, ",") + `]`
	}
	return out + `}`
}

// generatedPhaseView renders a compile-generated phase view for the given plan
// and phase ordinal: a `type: phase` document carrying the source-of-truth
// marker (the exact spelling the renderer emits, keyed on the plan name), with
// the sections a phase artifact requires so it contributes no unrelated noise.
func generatedPhaseView(planName, phaseOrdinal, title string) string {
	return `---
title: "` + title + `"
type: phase
plan: "` + planName + `"
phase: ` + phaseOrdinal + `
status: planned
created: 2024-01-01
updated: 2024-01-01
deliverable: "Graph view"
tasks: []
---

# Phase ` + phaseOrdinal + `: ` + title + `

` + GeneratedViewMarkerPrefix + planName + `-Graph.json. Regenerate with ` + "`sdd compile --plan " + planName + "`. Edits here are overwritten. -->" + `

## Overview

Rendered.

## Acceptance Criteria

- [ ] Every node in this phase is closed (derived from the graph).

## Phase Completion Evidence

Pending — not complete.
`
}

// completeGeneratedPhaseView renders a compile-generated phase view like
// generatedPhaseView, but `status: complete` with no `### Completed task
// identities` / `Final aligned review` evidence — exactly the shape a
// closed graph plan's rendered view has, which SDD070/SDD157/SDD166/SDD167
// must not flag (their v1 completion-evidence protocol does not apply to
// graph plans; the graph's own sync-only closure is the record instead).
func completeGeneratedPhaseView(planName, phaseOrdinal, title string) string {
	return replaceFirst(
		replaceFirst(generatedPhaseView(planName, phaseOrdinal, title),
			"status: planned", "status: complete"),
		"## Phase Completion Evidence\n\nPending — not complete.",
		"## Phase Completion Evidence\n\n- Verified: 2024-01-01\n")
}

// completeGraphPlanReadme renders a compile-generated plan README naming a
// single complete phase — the plan-level counterpart of
// completeGeneratedPhaseView, for SDD059/SDD070/SDD158's plan-side checks.
func completeGraphPlanReadme(planName, doc, title string) string {
	return `---
title: "` + planName + `"
type: plan
status: complete
created: 2024-01-01
updated: 2024-01-01
tags: []
related: []
phases:
  - id: 1
    title: "` + title + `"
    status: complete
    doc: "` + doc + `"
---

## Overview

Rendered.

## Non-Goals

None.

## Architecture

Rendered.

## Key Decisions

None.

## Dependencies

None.

## Plan Completion Evidence

- Verified: 2024-01-01
`
}

// v1PhaseWithTasks renders a v1 phase document for the given plan and phase
// ordinal whose tasks[] declares each given task id.
func v1PhaseWithTasks(planName, phaseOrdinal string, taskIDs ...string) string {
	block := ""
	for _, id := range taskIDs {
		block += "\n  - id: \"" + id + "\"\n    title: Task " + id + "\n    status: planned\n    verification: x\n    justifies: FR-01\n"
	}
	return phaseWithTasks(phaseOrdinal, planName, block)
}

// planReadmeWithPhaseDoc renders a plan README whose phases[] lists a single
// entry with the given doc filename.
func planReadmeWithPhaseDoc(doc string) string {
	return planWithPhase(map[string]string{"id": "1", "title": "One", "status": "planned", "doc": doc})
}

// conversionRemoveSetup is the git setup shared by the graph-conversion
// examples: commit the fixture as the baseline, then stage the v1 phase doc's
// removal. A function rather than a package var so it is built at example
// registration time, after appendOnlySetup is initialized.
func conversionRemoveSetup() [][]string {
	return afterCommit([]string{"git", "rm", "-q", "Plans/Sample/01-One.md"})
}
