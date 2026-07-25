---
name: brainstorm
description: "Explore possibilities for a problem or opportunity with structured evaluation. Do NOT enter plan mode — this skill produces a brainstorm artifact directly. Triggers: /brainstorm, brainstorm, explore options, what are our options"
---

# /brainstorm — Explore Possibilities

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) and target repository per `shared/path-resolution.md` in the plugin directory.

## When to Use
When you need to generate and evaluate multiple approaches to a problem before committing to one. Good for architecture decisions, feature approaches, or tool selection.

## Process

1. **Define Problem**
   - If the user hasn't already specified it, ask what problem or opportunity to explore
   - Clarify constraints and evaluation criteria

2. **Gather Context**
   - Invoke `sdd-planner:researcher` with the problem statement and constraints; it scans all artifact directories and the codebase per its own definition.
   - The agent returns a structured summary of relevant context

3. **Generate Ideas**
   - Build on the context gathered by the researcher
   - **Idea 0 is always "do nothing / status quo"** — who absorbs this problem today and at what concrete cost. Extending or configuring something the researcher found already in the repo belongs here too. It is a real candidate evaluated on the same criteria, not a formality: every other idea has to beat it. Drop it only when inaction is genuinely impossible (a hard external deadline, a compliance obligation, an active outage), and name which in the Recommendation
   - Brainstorm multiple approaches beyond the baseline (aim for 3-5)
   - For each idea, document: description, pros, cons, effort level
   - Consider both conventional and creative approaches
   - `3-5` is a range, not a quota. Do not invent filler options to reach it — three real approaches beat three real ones plus two strawmen, and a strawman that makes the recommendation look inevitable is worse than no option at all

4. **Evaluate**
   - Create `Brainstorm/<topic-slug>.md` using `shared/templates/brainstorm.md` (`<topic-slug>` is lowercase kebab-case, e.g., `auth-token-rotation`)
   - Build a comparison matrix against the criteria, scoring Idea 0 in the same columns as everything else
   - Where architectural approaches are compared, use Mermaid diagrams to illustrate key differences
   - Make a recommendation with rationale, stating explicitly **why the recommendation beats Idea 0** — what the status quo costs that this approach removes. A recommendation that never engages the baseline hasn't been evaluated, only asserted
   - **"Do nothing" is a legitimate recommendation.** If no idea clearly beats the baseline, say so and recommend Idea 0; a brainstorm that concludes the problem isn't worth solving yet has succeeded. Do not manufacture a winner to justify the exercise

5. **Link**
   - Add cross-references to related research or specs in `related` frontmatter

6. **Finalize**
   - Set `status: active` in the frontmatter once the document is complete and presented to the user. Then re-read the frontmatter and confirm it parses as YAML and includes `title`, `type`, `status`, `created`, `updated`, `tags`, `related`.
   - **If the user explicitly accepts the recommendation**, record it in the decision ledger per `shared/decision-log.md` — the rejected ideas go in the entry's `rejected[]`; run the collision check first (a collision stops for the user). A recommendation merely presented, not endorsed, is either left unrecorded or logged as `status: proposed` — never `accepted`.

## Output
```
Brainstorm/<topic-slug>.md
```

## Document Structure
See `shared/templates/brainstorm.md`:
- **Problem Statement**: What we're solving
- **Ideas**: Idea 0 (do nothing / status quo) plus each alternative, with description, pros, cons, effort
- **Evaluation**: Comparison matrix
- **Recommendation**: Which approach, why, and why it beats the status quo
- **Next Steps**: What to do with the decision

## Context
- Orchestration: `shared/orchestration.md`
- Template: `shared/templates/brainstorm.md`
- Schema: `shared/frontmatter-schema.md`
- Agent: `sdd-planner:researcher`
