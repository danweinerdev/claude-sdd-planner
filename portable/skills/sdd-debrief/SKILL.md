---
name: sdd-debrief
description: "Write after-action notes for a completed plan phase. debrief phase, after-action, phase complete"
---

# After-Action Phase Notes

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
When a plan phase has been completed (or substantially completed) and you want to capture what happened: decisions made, deviations from plan, lessons learned, and impact on future phases.

## Process

1. **Identify Target**
   - Scan `Plans/` for plans whose README frontmatter `status` is `active` and that have in-progress or completed phases (debriefs happen during active work)
   - Ask which plan and phase to debrief (or infer from context)
   - Read the phase document to understand what was planned
   - Read the plan README for overall context

2. **Gather Information**
   - Review the phase's tasks and subtasks for completion status
   - Read every task's `### Completion Evidence` — an absent or pending section on a `complete` task is a legacy evidence gap (`shared/completion-evidence.md`); report it in the debrief, never treat it as proof
   - Read related designs from `Designs/` to identify deviations from intended architecture
   - Read related specs from `Specs/` to assess requirements coverage
   - If more than ~3 related documents are involved, delegate the sweep to the researcher prompt (`shared/agent-prompts/researcher.md`) instead of reading them all yourself
   - Ask the user about:
     - Key decisions made during implementation
     - What deviated from the original plan or design
     - Problems encountered and how they were resolved
     - Insights to carry forward

3. **Spot Skill Opportunities**
   Review the phase for repeated actions that would benefit from being enshrined as a reusable skill. Look for:
   - Manual sequences you (or the user) ran more than once — multi-step git workflows, recurring investigations, file-munging pipelines, check-lists that were applied by hand
   - Repeated workflow sequences that always went together
   - Codebase operations that lacked a helper/script and had to be redone in each task
   - Checks or validations that should have been automated but were done mentally

   For each opportunity, capture: what the repeated action was, where the skill should live (new sdd-planner skill, a project-level skill, a codebase helper, a shell script, a Makefile target), why a skill would help, and a rough shape (inputs, outputs, when to invoke).

   Ask the user to confirm or extend the list before writing — they may have noticed patterns you didn't.

4. **Write Debrief**
   - Create `Plans/<PlanName>/notes/<NN>-<Phase-Name>.md` using `shared/templates/debrief.md`
   - Fill in the frontmatter: set `created` and `updated` to today, `tags` to themes from the phase, `related` to the specs/designs consulted in step 2, and choose `status` — `draft` if the debrief is being written incrementally and will be revisited, `complete` when finalized in one sitting
   - Fill in all sections: Decisions Made, Requirements Assessment, Deviations, Risks & Issues, Lessons Learned, Impact on Subsequent Phases, **Skill Opportunities**
   - The filename mirrors the phase doc number (e.g., `01-Core-Setup.md` -> `notes/01-Core-Setup.md`)

5. **Backfill the Decision Ledger**
   - For each item in Decisions Made that the user confirmed (step 2), isn't already in `Decisions/decisions.md`, and passes the **admission test** in `shared/decision-log.md` § Capture, append an entry per that convention — collision check first; a collision stops for the user. Items that only explain how this phase went stay in the debrief. Scope entries to the plan. This is the safety net for decisions made mid-implementation that escaped capture.

6. **Update Phase Status**
   - A status backfill here is subject to the same gate as `sdd-implement`: every task `complete` with conforming completion evidence, every acceptance criterion checked, `## Phase Completion Evidence` populated, and a persisted frozen four-lane `Aligned` review cited (`shared/completion-evidence.md`, `shared/review-artifacts.md` § Phase-completion review gate). If any of that is missing, leave the status alone and report exactly what's outstanding — a debrief documents the phase, it doesn't wave it through
   - When the gate holds, set the phase status to `complete` in both:
     - The phase doc frontmatter
     - The plan README's `phases[]` array
   - Update `updated` dates
   - If this was the final phase and all phases are now complete (including populated `## Plan Completion Evidence`), set the plan README frontmatter `status` to `complete`

## Output
```
Plans/<PlanName>/notes/<NN>-<Phase-Name>.md
```

## Document Structure
See `shared/templates/debrief.md`:
- **Decisions Made**: Key choices with rationale
- **Requirements Assessment**: Acceptance criteria met/not met
- **Deviations**: What changed from plan and why
- **Risks & Issues Encountered**: Problems and resolutions
- **Lessons Learned**: Insights for the future
- **Impact on Subsequent Phases**: Downstream changes needed
- **Skill Opportunities**: Repeated actions that should become reusable skills

## Context
- Template: `shared/templates/debrief.md`
- Schema: `shared/frontmatter-schema.md`
- Target plan: `Plans/<PlanName>/` (status: `active`)
- Related specs: `Specs/`
- Related designs: `Designs/`
