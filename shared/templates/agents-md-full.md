# {{TITLE}}

{{DESCRIPTION}}

This repository keeps spec-driven development artifacts under `{{PLANNING_ROOT}}/`. Use the installed `sdd-planner` skills by asking the agent for the relevant activity in natural language.

## Artifact Layout

```text
{{PLANNING_ROOT}}/
├── planning-config.json
├── Research/
├── Brainstorm/
├── Specs/<feature>/README.md
├── Designs/<component>/README.md
├── Plans/<PlanName>/README.md
├── Plans/<PlanName>/<NN>-<Phase>.md
├── Plans/<PlanName>/notes/<phase>.md
└── Decisions/decisions.md
```

## Conventions

- Artifact metadata lives in YAML frontmatter. Status values are the source of truth.
- Plans stay under `Plans/<PlanName>/`; never move them to express lifecycle state.
- A phase owns task status and subtask checkboxes. Update them only after actual verification.
- Plan each task as one clean, complete, independently bisectable native SCM
  revision/checkpoint; subtasks are mechanical implementation steps inside that
  boundary, not presumed incomplete revisions. Review each complete task diff
  before finalizing its revision and preserve its complete state in lifecycle
  recording. **Git adapter:** this revision is a focused implementation commit.
- Every task, phase, and plan has a completion-evidence section. Record exact
  commands/tools, context, revision/checkpoint, result, and observable evidence
  before any `complete` transition; prospective verification criteria are not
  proof. Write artifacts in-flow, but never commit per edit; decision-ledger
  entries are never written without explicit user approval of the exact text.
  **Git adapter:** in commit-capable workflows where commits are authorized,
  commit the verified feature slice first and record lifecycle state once per
  affected root at phase close — never at task closeout, never per amendment or
  decision. Shared-root artifacts and ledgers use one boundary commit; external
  roots record once at the same boundary (D-0024). Dirty or no-SCM work
  remains non-complete until a durable native checkpoint exists.
- Use `planning-config.json` to resolve the planning root and any externally targeted repository paths. There is no local companion config.
- Consult the plugin's frontmatter schema, templates, and language-verification references when creating or changing artifacts.

## Lifecycle

Use these skills as needed: `sdd-setup`, `sdd-research`, `sdd-brainstorm`, `sdd-specify`, `sdd-design`, `sdd-plan`, `sdd-implement`, `sdd-code-review`, `sdd-poke-holes`, `sdd-debrief`, `sdd-decide`, `sdd-decision-log`, and `sdd-validate`.

The normal progression is: `sdd-setup` -> `sdd-research` -> `sdd-brainstorm` -> `sdd-specify` -> `sdd-design` -> `sdd-plan` -> `sdd-implement` -> `sdd-code-review` -> `sdd-debrief`. Use `sdd-validate` at lifecycle boundaries. It is valid to enter at any point when the corresponding artifacts already exist.

Plans come in two execution models, routed by graph presence. A plan with a committed `<Name>-Graph.json` executes as a graph walk (`sdd-plan` authors node payloads compiled into it; `sdd-implement` walks claim → red → green → sync → merge, with completion derived from observations, never narrated). A plan without a graph is a v1 markdown plan and keeps the wave protocol and evidence rules until converted with `sdd graph convert` — conversion emits blocking sentinels that are real judgments, never defaults.

For a contiguous specification, design, or planning session, write every
artifact and ledger update as it becomes known and record once per affected SCM
root at session close — never per artifact, skill, approval, review, or
decision. Phase close follows the same rule: one planning-root atomic commit
carries the final review, debrief, phase updates, plan phase-array update, and
— when final plan checks are ready — plan completion evidence/status (D-0024).
The expected shape of a phase's history is one lifecycle commit at open, N
implementation commits, one lifecycle commit at close.

## Review And Execution

Before implementation, confirm an approved plan has concrete verification criteria. During implementation, record command output for checks actually run. Before phase completion, freeze a concrete phase revision/range, persist an `Aligned` review from all four `sdd-code-review` lanes, and rerun all four after any material code change. For project-specific review requirements, keep a short trusted review brief in this `AGENTS.md`; do not rely on auto-discovered agent files.
