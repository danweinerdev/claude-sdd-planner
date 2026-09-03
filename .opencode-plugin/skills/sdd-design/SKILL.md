---
name: sdd-design
description: "Create a technical architecture and design document. Do NOT enter plan mode — this skill produces a design artifact directly. design this, architecture for, technical design"
---

# Technical Architecture Document

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
When you need to define the technical architecture for a component or system before implementation. Produces a reviewable design document with architecture decisions.

## Process

1. **Gather Context**
   - If the user hasn't already specified it, ask what component to design
   - Dispatch a collaboration subagent rendering `shared/agent-prompts/researcher.md` (or perform that prompt yourself) with the component and its constraints; it scans all artifact directories and the codebase per its own definition.
   - Review any related research documents

2. **Draft Design**
   - Create `Designs/<ComponentName>/README.md` using `shared/templates/design.md`
   - Document: overview, non-goals, architecture (components, data flow, interfaces), design decisions (with alternatives considered), error handling, testing strategy, migration plan
   - **Non-Goals bound the component.** Name the responsibilities that belong to neighboring components, the generality this design declines to build, and any extension point deliberately left out. An unstated boundary gets built past
   - Where a section realizes a spec requirement, cite its id inline (`FR-NN`/`NFR-NN` — `shared/frontmatter-schema.md` § Stable Identifiers) so coverage is greppable
   - **Use Mermaid diagrams** for architecture, data flow, and component relationships — prefer `graph TD`, `flowchart LR`, or `sequenceDiagram` over ASCII art or prose-only descriptions
   - **Testing strategy must include structural verification:** Read `shared/language-verification.md` and include the language-appropriate structural checks (sanitizers, static analysis, type checking) in the Testing Strategy section. These define what "structurally correct" means for this component beyond passing tests.
   - Set status to `draft`

3. **Review**
   - Run `sdd design submit <design-path>` when dispatching the reviewer — statuses move through the binary's transition verbs, never by editing frontmatter
   - Dispatch a collaboration subagent in a fresh non-inheriting context rendering `shared/agent-prompts/plan-reviewer.md` (if collaboration is unavailable, perform the review yourself following that prompt and label it **self-review**) to review the design
   - Address critical and major issues

4. **Present for Approval**
   - Show the user the review results and final design
   - **Open questions gate approval.** Before approving, every remaining open question must be either resolved or explicitly marked **non-blocking** with a one-line rationale for why the design holds regardless of its answer. A question whose answer could change the architecture blocks approval — leave the design at `review` and name the question to the user. A "⚠️ pending confirmation" annotation is not a gate. (`sdd design approve` enforces the mechanically checkable part: it refuses on an introduced SDD153 finding.)
   - After findings are addressed and the user explicitly approves, run `sdd design approve <design-path>`. If the user declines or defers, leave it at `review`. (Later transitions: `sdd design implement` once built; `sdd design supersede --by <successor>` when replaced.)
   - Then re-read the frontmatter and confirm it parses as YAML and includes `title`, `type`, `status`, `created`, `updated`, `tags`, `related`.

5. **Record Decisions**
   - After approval, take each Design Decision the user weighed in on (its rejected options go in the entry's `rejected[]`) and each user-resolved open question, run it through the **admission test** in `shared/decision-log.md` § Capture, and record only those that pass — a decision that governs only this component's internals is the design's content, while one that constrains callers, other components, or later implementation earns an entry. Run the collision check before each append — a collision stops for the user. Scope entries to `Designs/<ComponentName>`, and **cite each new entry's id inline** in the governed Design Decision section (e.g., "(D-0012)") — the bidirectional link is what makes supersession detection work. Design Decisions the user never engaged with are the design's own content — don't promote them as `accepted`.

## Output
```
Designs/<ComponentName>/README.md
```
Plus decision-ledger entries in `Decisions/decisions.md` for user-made design choices and resolved questions. Write in flow; commit once at the end of the session, never per artifact or per amendment (`shared/autonomy.md` § SCM boundary cadence, D-0024).

## Document Structure
See `shared/templates/design.md`:
- **Overview**: Component role in the system
- **Non-Goals**: What the component deliberately does not do, and which responsibilities belong elsewhere
- **Architecture**: Components, data flow, interfaces
- **Design Decisions**: Each a top-level bold bullet `- **DD-N**: Title` with context, options, decision, rationale on its indented continuation lines — the bold-bullet form is the declaration `sdd apply` collects and allocates (heading-form decisions are not registered at apply time). The ids are stable and append-only (`shared/frontmatter-schema.md` § Stable Identifiers): plans and phases cite them, and `sdd validate` resolves each citation against the related design (SDD122). Cite another component's decision in qualified form — `ComponentName:DD-3`.
- **Error Handling**: Detection, reporting, recovery
- **Testing Strategy**: How to validate
- **Migration / Rollout**: Transition plan

## Context
- Orchestration: `shared/orchestration.md`
- Template: `shared/templates/design.md`
- Schema: `shared/frontmatter-schema.md`
- Agents: the researcher prompt (`shared/agent-prompts/researcher.md`), the plan-review prompt (`shared/agent-prompts/plan-reviewer.md`)
