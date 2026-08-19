---
title: "{{TITLE}}"
type: phase
plan: "{{PLAN}}"
phase: {{PHASE_NUM}}
status: planned
created: {{DATE}}
updated: {{DATE}}
deliverable: "{{DELIVERABLE}}"
tasks: []
# Each task entry should include:
#   id: "X.Y"            # or "X.Ya" to insert without renumbering later tasks
#   title: "Task title"
#   status: planned
#   verification: "How we know this complete, bisectable task revision passes"
#   justifies: "Why this task exists — the FR/NFR/AC/D ids it serves, or the
#     concrete failure it prevents. Not a restatement of the title."
#   depends_on: ["X.Z"]  # optional
---

# Phase {{PHASE_NUM}}: {{TITLE}}

<!-- /validate format contract:
- Store this document as UTF-8 with LF line endings and keep the YAML
  frontmatter as a mapping between standalone `---` delimiters.
- Keep `title`, `type`, `status`, `created`, and `updated`; use a phase status
  of `planned`, `in-progress`, `complete`, `blocked`, or `deferred`; dates use
  `YYYY-MM-DD`. Task status uses the same vocabulary.
- `plan`, `phase`, and `deliverable` are required. `tasks` is a YAML list of
  mappings; each task requires nonempty `id`, `title`, `status`,
  `verification`, and `justifies`. Task IDs are unique within the plan and use
  exactly `<phase>.<digits>`; status uses the task status vocabulary.
- `justifies` states why the task exists — the FR-NN/NFR-NN/AC-NN/D-NNNN ids it
  serves, or the concrete failure it prevents. It must not restate the task
  title, and placeholder justifications ("for completeness", "might need it
  later", "part of the architecture", "TBD") are rejected.
- Optional `depends_on` is a YAML list of task IDs from this plan. It contains
  no unknown IDs, self-dependencies, or dependency cycles.
- Every task mapping has one matching H2 beginning `## <task-id>:` (a space in
  place of the colon is also accepted) with exact H3 headings `Subtasks`,
  `Notes`, and `Completion Evidence` inside it.
- Each task is one clean, complete, independently bisectable native SCM
  revision/checkpoint boundary. Its subtasks are mechanical
  steps inside that boundary; the repository remains buildable and named
  verification passes when the task lands. `Notes` states the complete
  behavior/capability delivered at the boundary and excludes unrelated feature
  slices. Clearly labeled Git adapter guidance may call that revision a commit;
  governing workflow language must not assume every SCM has Git commits.
- FR-NN, NFR-NN, AC-NN, and D-NNNN citations must resolve through the plan's
  `related` graph and applicable decision ledger. For approved/active/complete
  plans, task verification/detail and phase Acceptance Criteria collectively
  cite every requirement and acceptance criterion from every related spec.
- A complete phase has only complete tasks, no unchecked `- [ ]` Acceptance
  Criteria, populated task and phase completion evidence, concise completed-task
  native revision/checkpoint references, and a persisted final `Aligned` phase
  review across all four /code-review lanes. Needs changes or Blocked forbids
  completion; every material post-review code fix gets a new planned task id and
  complete task revision before a fresh full four-lane review.
-->

## Overview
Brief description of what this phase delivers.

## {{PHASE_NUM}}.1: {{TASK_TITLE}}

### Subtasks
- [ ] {{SUBTASK}}

### Notes
Revision boundary: {{COMPLETE_BEHAVIOR_OR_CAPABILITY_THIS_TASK_LANDS}}

### Completion Evidence

<!-- Keep the exact pending line until completion. Populated evidence uses the
exact labels and table formats stated under Phase Completion Evidence below. -->
Pending — not complete.

<!-- Optional per task — include only when the task has a known tempting-but-wrong shortcut:
### Trap
{{THE_SHORTCUT_A_HASTY_MODEL_WOULD_TAKE_AND_WHY_IT_IS_WRONG}}
-->

## Acceptance Criteria
- [ ]

## Phase Completion Evidence

<!-- Keep the exact `Pending — not complete.` line until completion. Evidence
uses the exact labels `Verified`, `Repository`, `VCS`, `Revision / checkpoint`,
and `Identity recheck`, each exactly once visibly as `- <label>: <value>`.
`Verified` is `YYYY-MM-DD`; `Repository` is the exact resolved target root,
recorded home-anchored (`~/...`) — never a literal `/home/<user>` prefix, and
no credentials or user-identifying paths anywhere in evidence
(shared/frontmatter-schema.md § Sensitive Data);
`VCS` identifies a validated SCM adapter; record the tested native SCM
 revision/checkpoint. Git adapter: phase `Revision / checkpoint` is a full
 40-hex native Git commit and may be a validated integration merge. The
 non-merge rule applies only to atomic task implementation evidence; commit the
 feature slice before recording evidence, then commit only lifecycle/evidence
 bookkeeping separately. Dirty Git, no-SCM, and unsupported SCM adapters remain
 non-complete.
The exact table columns are
`Command | Working directory | Result | Observable evidence` or
`Tool / inspection | Context | Result | Observable evidence`. Command results
use `PASS (exit 0)` — or `PASS (exit N, expected)` for a deliberate
expected-failure run; tool/inspection results use `PASS`. `Identity recheck`
names the tool, an ISO date/time through minutes, and a matched/matching
identity. Follow shared/completion-evidence.md for identity, concise child
gate, and durability rules. On phase completion, add `- Final aligned review:
<persisted review artifact path>; frozen: <revision/range>` for the final
`Aligned`, four-lane review exactly once; `frozen` must exactly equal that
review's `rev`.
For each completed task, also add `Focused review` in strict syntax: for Git,
exactly `git show <full40>` for final-commit review or `git diff
<full40>..<full40>` for range review in backticks before `; complete task diff
reviewed for correctness, scope, tests, maintainability, and task boundary`, then
`Reviewed candidate / final` with the exact native SCM identity (Git adapter:
the task full commit or `diff: <full40>..<full40>` whose distinct base is that
task commit's direct first parent and whose endpoint is that revision; the
command has no extra operands), and
`Review result: PASS/Aligned`. For a completed phase, add `### Completed task
identities` with one `- <task id>: <native revision/checkpoint>` line for every
completed task. Record required independent phase rows; do not add child-evidence
rollup sections. -->
Pending — not complete.
