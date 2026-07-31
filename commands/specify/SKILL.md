---
name: specify
description: "Write a requirements specification for a feature. Do NOT enter plan mode — this skill produces a spec artifact directly. Triggers: /specify, write spec, specify requirements, requirements for"
---

# /specify — Write Requirements Specification

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) and target repository per `shared/path-resolution.md` in the plugin directory.

## When to Use
When you need to define the requirements for a feature before designing or implementing it. Produces a testable, reviewable specification.

## Process

1. **Gather Context**
   - If the user hasn't already specified it, ask what feature to specify
   - Invoke the `sdd-planner:researcher` agent to gather context from existing artifacts and codebase
   - Review any related research or brainstorm documents

2. **Draft Specification**
   - Create `Specs/<FeatureName>/README.md` using `shared/templates/spec.md`
   - Write: overview, goals, non-goals, requirements (functional + non-functional), user stories, acceptance criteria, constraints, dependencies
   - **Number requirements and acceptance criteria with stable ids** (`FR-NN`/`NFR-NN`/`AC-NN` per `shared/frontmatter-schema.md` § Stable Identifiers) — downstream designs and plan tasks cite these ids, so they are append-only and never renumbered
   - When the spec captures an **external contract** (a third-party API, protocol, wire format, or another team's interface), pin the source: link the authoritative doc and record its version and as-of date in the spec. Downstream implementation is only allowed to derive external-contract behavior from this captured source — never from model memory — so the pin is load-bearing.
   - Set status to `draft`

3. **Review**
   - Set `status: review` when dispatching the reviewer
   - Invoke the `sdd-planner:spec-reviewer` agent to review the specification
   - Address critical and major issues

4. **Present for Approval**
   - Show the user the review results and final spec
   - **Open questions gate approval.** Before setting `status: approved`, every remaining open question must be either resolved or explicitly marked **non-blocking** with a one-line rationale for why the requirements hold regardless of its answer. A question whose answer could change in-scope requirements blocks approval — leave the spec at `review` and name the question to the user. A "⚠️ pending confirmation" annotation is not a gate.
   - After findings are addressed and the user explicitly approves, set `status: approved`. If the user declines or defers, leave it at `review`.
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
- Agents: `sdd-planner:researcher`, `sdd-planner:spec-reviewer`
