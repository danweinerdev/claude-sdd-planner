---
title: "{{TITLE}}"
type: plan
status: draft
created: {{DATE}}
updated: {{DATE}}
tags: []
related: []
phases: []
# Replace `phases: []` with mappings in this exact shape:
#   - id: 1
#     title: "Phase title"
#     status: planned
#     doc: "01-Phase-Title.md"
#     depends_on: []  # optional; phase ids from this plan only
---

# {{TITLE}}

<!-- /validate format contract:
- Store this document as UTF-8 with LF line endings and keep the YAML
  frontmatter as a mapping between standalone `---` delimiters.
- Keep `title`, `type`, `status`, `created`, `updated`, `tags`, and `related`;
  dates use `YYYY-MM-DD` and status is one of `draft`, `approved`, `active`,
  `complete`, or `archived`.
- Keep every H2 heading supplied by this template with exactly the shown text.
- Keep `tags`, `related`, `phases`, and every `depends_on` value as YAML lists.
  Related values are nonempty planning-root-relative artifact paths that
  resolve; do not use absolute paths, backslashes, `.` segments, or `..`.
- Every phase mapping requires nonempty `id`, `title`, `status`, and `doc`.
  Phase IDs are unique; status uses the phase status vocabulary; `doc` is
  relative to this README and resolves to a `type: phase` document whose
  `plan`, `title`, `phase`, and `status` match this plan and entry. Every phase
  document is listed exactly once by the README in its physical plan directory.
  Dependencies reference phase IDs in this plan and contain no unknown IDs,
  self-dependencies, or cycles.
- An approved, active, or complete plan must cite every FR-NN, NFR-NN, and
  AC-NN from every related spec in phase task verification/detail or phase
  Acceptance Criteria. Directly related designs collectively cite every
  FR-NN and NFR-NN from those specs. Requirement citations must resolve through
  the `related` graph; live artifacts do not cite rejected/superseded D-NNNN
  ids.
- A complete plan has only complete phases and populated completion evidence.
-->

## Overview
What this plan delivers and why it matters.

## Architecture
High-level technical approach. Use Mermaid diagrams where visual structure helps (e.g., `graph TD` for component relationships, `flowchart LR` for data flow).

## Key Decisions
Major choices made and their rationale.

## Dependencies
External dependencies, prerequisites, and assumptions.

## Plan Completion Evidence

<!-- Keep the exact `Pending — not complete.` line until completion. Evidence
uses the exact labels `Verified`, `Repository`, `VCS`, `Revision / checkpoint`,
and `Identity recheck`, each exactly once visibly as `- <label>: <value>`.
`Verified` is `YYYY-MM-DD`; `Repository` is the exact resolved target root;
`VCS` identifies a validated SCM adapter; record the tested native SCM
 revision/checkpoint. Git adapter: plan `Revision / checkpoint` is a full
 40-hex native Git commit and may be a validated integration merge. The
 non-merge rule applies only to atomic task implementation evidence; commit
 implementation before recording evidence, then commit only lifecycle/evidence
 bookkeeping separately. Dirty Git, no-SCM, and unsupported SCM adapters remain
 non-complete. The exact table columns are `Command | Working directory | Result | Observable evidence`
or `Tool / inspection | Context | Result | Observable evidence`. Command
results use `PASS (exit 0)`; tool/inspection results use `PASS`. `Identity
recheck` names the tool, an ISO date/time through minutes, and a
matched/matching identity. Follow shared/completion-evidence.md for identity and
durability rules. A complete plan uses `### Completed phase identities` with one
`- <phase id>: <phase checkpoint>; review: <final review path>` line per phase.
Record required independent plan rows; do not add child-evidence rollup
sections. -->
Pending — not complete.

<!-- Optional — include only when questions are open; a plan cannot be
     approved/active/complete while a blocking question remains. Every retained
     question goes in an `## Open Questions` section and uses the exact form
     `- {{QUESTION}} — **non-blocking** — {{WHY_THE_PLAN_HOLDS_REGARDLESS}}`. -->
