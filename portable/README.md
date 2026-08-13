# SDD Planner — Portable Tree (OpenCode / Codex)

> **Generated artifact.** This tree is produced by `sdd plugin sync` from the
> canonical Claude plugin at the repository root of
> [claude-sdd-planner](https://github.com/danweinerdev/claude-sdd-planner).
> Do not edit files here — edit the canonical tree (or its `*.portable.md`
> variants / `portable-overrides/`) and regenerate. `make test` fails when
> this tree drifts from what a fresh sync would produce.

`sdd-planner` is a runtime-neutral agent-skills plugin for spec-driven
development. It creates and maintains Markdown planning artifacts with YAML
frontmatter for research, specifications, designs, implementation plans, code
review, debriefs, and validation.

Every capability is a plain `SKILL.md` skill with shared resources under
`shared/` — no slash commands, named subagent types, runtime hooks, or
model-specific instructions — so it works in any agent runtime that discovers
`SKILL.md` skills, with any model. Public skill names use the `sdd-` prefix
(`sdd-plan`, `sdd-implement`, `sdd-code-review`, and so on) to avoid
collisions in global skill directories.

## Prerequisite: the `sdd` binary

All skills drive one cross-platform Go binary for deterministic validation,
artifact writes, and lifecycle transitions. Install it once per machine:

```bash
go install github.com/danweinerdev/claude-sdd-planner/cmd/sdd@latest
```

`sdd-setup` verifies the binary against the manifest's `minSddVersion` before
touching anything and stops with the exact install command when it is missing
or too old.

## Installation

- **OpenCode**: point a skills discovery path at this tree (for example
  `ln -s <this-tree> ~/.agents`, so the skills resolve as
  `~/.agents/skills/<name>/SKILL.md`), or mount it there in a container.
- **Codex**: install via a marketplace that carries this tree
  (`.codex-plugin/plugin.json` is the manifest). Start a new thread after
  installation so the skills are available.
- Any other runtime that loads the `.agents/skills` convention or
  directory-sourced skills works the same way.

Ask your agent for a workflow naturally: "set up spec-driven planning in this
repository", "write a specification for this feature", or "review this
implementation against the active plan."

## Dispatch model

Skills are runtime-neutral: delegation uses whatever collaboration mechanism
the runtime provides, or none (see `shared/agent-runtime.md`). Stable semantic
dispatch identifiers let runtime adapters route work without the skills naming
agents or models:

- `sdd-code-review` exposes `review_plan_drift`, `review_quality`,
  `review_spec_compliance`, and `review_blind_spots` for its four independent
  review lanes, with a labeled serial single-agent fallback.
- `sdd-implement` uses `implement_task` for implementation dispatches, with a
  transparent primary-agent fallback.

Planning artifacts live in the root configured by `planning-config.json`. The
plugin uses `AGENTS.md` for optional repository guidance and does not create
tool-specific files or launchers. Completion is evidence-gated at task, phase,
and plan level per `shared/completion-evidence.md`, enforced deterministically
by `sdd validate`.
