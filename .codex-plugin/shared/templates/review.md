---
title: "{{REVIEW_TYPE}}: {{TARGET_TITLE}}"
type: review
status: open
created: {{DATE}}
updated: {{DATE}}
tags: [review]
related: [{{TARGET_PATH}}]
review_of: "{{TARGET_PATH}}"
rev: "{{REV}}"
# For a phase-completion review, set review_scope: phase, frozen: true, verdict:
# Aligned, a valid review_mode, and exactly one result for each stable lane below.
# Every reviewed_identity exactly equals rev and every evidence value is a specific
# concrete observation naming inspected paths, behaviors, or observations, not a
# generic conclusion such as "passed", "ok", "aligned", "success", or "no findings".
# Its nonempty rev must exactly match the `frozen:` identity in phase completion
# evidence. `reviewed_planning_revision` is the exact full native planning Git
# commit at which the phase and plan intent were reviewed. Other reviews may omit
# these fields or record their actual non-phase state.
# review_scope: phase
# frozen: true
# verdict: Aligned
# reviewed_planning_revision: "<full40 planning Git commit>"
# review_mode: independent  # independent | mixed | single-agent
# lane_results:
#   - lane: review_plan_drift
#     result: PASS/Aligned
#     reviewed_identity: "{{REV}}"
#     evidence: "<nonempty auditable lane result>"
#   - lane: review_quality
#     result: PASS/Aligned
#     reviewed_identity: "{{REV}}"
#     evidence: "<nonempty auditable lane result>"
#   - lane: review_spec_compliance
#     result: PASS/Aligned
#     reviewed_identity: "{{REV}}"
#     evidence: "<nonempty auditable lane result>"
#   - lane: review_blind_spots
#     result: PASS/Aligned
#     reviewed_identity: "{{REV}}"
#     evidence: "<nonempty auditable lane result>"
findings: []
# Each finding entry:
#   id: F-01              # stable within this file; never renumbered
#   severity: critical    # critical | major | minor | question
#   title: "One-line finding"
#   status: open          # open | fixed | deferred | rejected | answered
followups: []
# Each follow-up entry (created when a finding is deferred without a plan task):
#   id: FU-01
#   finding: F-01
#   summary: "What still needs doing"
#   tracked_in: ""        # task id once landed in a plan; empty = floating
---

# {{REVIEW_TYPE}}: {{TARGET_TITLE}}

**Reviewed state:** {{REV}}
**Review mode:** {{MODE}}

## Findings

### F-01 — [Severity] One-line finding
**Impugns:** [FR-NN / AC-NN / task N.M / D-NNNN / file:line]
**Scenario:** The concrete sequence of events, inputs, or conditions that exposes the flaw.
**Why it matters:** Impact if unaddressed.
**Recommendation:** Concrete mitigation or the question to resolve.

## Resolution Log

<!-- Append-only; one entry per disposition. Update the finding's status in
     findings[] to match. See shared/review-artifacts.md.

### F-01 — fixed (YYYY-MM-DD)
What was decided and done; governing facts by id; commit/task/D-NNNN links.
-->
