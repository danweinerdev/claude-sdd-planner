---
name: sdd-research
description: "Investigate a topic and produce a structured research document. research this, investigate, look into"
---

# Investigate a Topic

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
When you need to gather and synthesize information about a topic before making decisions. Good for technology evaluations, understanding existing systems, or exploring unknowns.

## Process

1. **Define Scope**
   - If the user hasn't already specified it, ask what topic to research and what questions need answering
   - Determine if this is codebase research, external research, or both

2. **Gather Information**
   - Dispatch a collaboration subagent rendering `shared/agent-prompts/researcher.md` (or perform that prompt yourself if collaboration is unavailable) with the topic and questions
   - The agent will scan existing artifacts, codebase, and web as needed

3. **Synthesize**
   - Create `Research/<topic-slug>.md` using `shared/templates/research.md` (`<topic-slug>` is lowercase kebab-case, e.g., `auth-token-rotation`)
   - Organize findings into Context, Key Insights, Sources, Analysis
   - Highlight implications and recommendations
   - List open questions that remain

4. **Link**
   - Add cross-references to related artifacts in the `related` frontmatter field

5. **Finalize**
   - Set `status: active` in the frontmatter once the document is complete and presented to the user. Then re-read the frontmatter and confirm it parses as YAML and includes `title`, `type`, `status`, `created`, `updated`, `tags`, `related`.

## Output
```
Research/<topic-slug>.md
```

## Document Structure
See `shared/templates/research.md`:
- **Context**: Why this research is needed
- **Findings**: Key insights and sources
- **Analysis**: Implications and recommendations
- **Open Questions**: What remains unknown

## Context
- Orchestration: `shared/orchestration.md`
- Template: `shared/templates/research.md`
- Schema: `shared/frontmatter-schema.md`
- Agent: the researcher prompt (`shared/agent-prompts/researcher.md`)
