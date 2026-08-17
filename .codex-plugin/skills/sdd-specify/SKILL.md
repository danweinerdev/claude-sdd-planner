---
name: sdd-specify
description: "Write a requirements specification for a feature. Do NOT enter plan mode — this skill produces a spec artifact directly. write spec, specify requirements, requirements for"
---

# Write Requirements Specification

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
When you need to define the requirements for a feature before designing or implementing it. Produces a testable, reviewable specification.

## Process

1. **Gather Context**
   - If the user hasn't already specified it, ask what feature to specify
   - Dispatch a collaboration subagent rendering `shared/agent-prompts/researcher.md` (or perform that prompt yourself if collaboration is unavailable) to gather context from existing artifacts and codebase
   - Review any related research or brainstorm documents

2. **Draft Specification** — author through `sdd`, never with Write/Edit

   The spec is a schema-governed artifact. Author it through the CLI so the
   structure is enforced at write time rather than discovered later by
   `sdd-validate`; the PreToolUse guard denies `Write` and `Edit` on artifact
   paths for exactly this reason.

   ```bash
   # Start from the schema, not a copied template — the two are kept in step
   # by `sdd template --check`, and the generated form is always current.
   sdd template spec --out Specs/<FeatureName>/README.md

   # Fill one section at a time. Everything outside the named heading stays
   # byte-identical, so a later edit cannot disturb an earlier one.
   sdd section set Specs/<FeatureName>/README.md --heading "## Overview" <<'EOF'
   <the overview prose>
   EOF
   ```

   - `sdd apply` recompiles a whole artifact from a Markdown payload when you
     are drafting the document in one pass; `sdd section set` edits one section
     when you are revising. Prefer `section set` after the first draft.
   - Pass `--expect <digest>` on a write when you read the artifact earlier in
     the turn: it refuses if the file changed underneath you and tells you to
     re-read and retry, instead of silently overwriting a concurrent edit.
   - Write: overview, goals, non-goals, requirements (functional + non-functional), user stories, acceptance criteria, constraints, dependencies
   - **Number requirements and acceptance criteria with stable ids** (`FR-NN`/`NFR-NN`/`AC-NN` per `shared/frontmatter-schema.md` § Stable Identifiers) — downstream designs and plan tasks cite these ids, so they are append-only and never renumbered
   - When the spec captures an **external contract** (a third-party API, protocol, wire format, or another team's interface), pin the source: link the authoritative doc and record its version and as-of date in the spec. Downstream implementation is only allowed to derive external-contract behavior from this captured source — never from model memory — so the pin is load-bearing.
   - Set status to `draft`

3. **Review**
   - Run `sdd spec submit <spec-path>` when dispatching the reviewer — statuses move through the binary's transition verbs, never by editing frontmatter
   - Dispatch a collaboration subagent in a fresh non-inheriting context rendering `shared/agent-prompts/spec-reviewer.md` (if collaboration is unavailable, perform the review yourself following that prompt and label it **self-review**) to review the specification
   - Address critical and major issues

4. **Present for Approval**
   - Show the user the review results and final spec
   - **Open questions gate approval.** Before approving, every remaining open question must be either resolved or explicitly marked **non-blocking** with a one-line rationale for why the requirements hold regardless of its answer. A question whose answer could change in-scope requirements blocks approval — leave the spec at `review` and name the question to the user. A "⚠️ pending confirmation" annotation is not a gate. (`sdd spec approve` enforces the mechanically checkable part: it refuses on an introduced SDD153 finding.)
   - After findings are addressed and the user explicitly approves, run `sdd spec approve <spec-path>`. If the user declines or defers, leave it at `review`. (Later transitions: `sdd spec implement` once built; `sdd spec supersede --by <successor>` when replaced.)
   - Then re-read the frontmatter and confirm it parses as YAML and includes `title`, `type`, `status`, `created`, `updated`, `tags`, `related`.

5. **Record Decisions**
   - After approval, run each user-resolved open question and each user-made scoping/requirements choice through the **admission test** in `shared/decision-log.md` § Capture, and record only those that pass — a choice whose whole effect is what this spec now says is the spec's content, not a ledger entry. What qualifies here is typically a scoping boundary or a definition other features must honor. Run the collision check before each append (a collision stops for the user). Scope entries to `Specs/<FeatureName>`, and **cite each new entry's id inline** in the governed spec section (e.g., "(D-0012)") — the bidirectional link is what makes supersession detection work. Skip questions marked non-blocking without a user answer — nothing was decided.

## Output
```
Specs/<FeatureName>/README.md
```
Plus decision-ledger entries in `Decisions/decisions.md` for user-resolved questions.

## Document Structure
See `shared/templates/spec.md`:
- **Overview**: Feature purpose
- **Goals / Non-Goals**: Scope boundaries
- **Requirements**: Functional and non-functional
- **User Stories**: As a [user], I want to...
- **Acceptance Criteria**: Testable pass/fail criteria
- **Constraints / Dependencies / Open Questions**

## Context
- Orchestration: `shared/orchestration.md`
- Template: `shared/templates/spec.md`
- Schema: `shared/frontmatter-schema.md`
- Agents: the researcher prompt (`shared/agent-prompts/researcher.md`), the spec-review prompt (`shared/agent-prompts/spec-reviewer.md`)
