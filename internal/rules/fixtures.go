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

// decisionLog returns a minimal, structurally valid decision-log document
// whose `decisions:` block is the caller-supplied raw YAML.
func decisionLog(decisionsBlock string) string {
	return `---
title: Decision Log
type: decision-log
status: active
created: 2024-01-01
updated: 2024-01-01
decisions:` + decisionsBlock + `
---

Ledger body.
`
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
