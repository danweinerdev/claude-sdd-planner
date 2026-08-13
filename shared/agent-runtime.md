# Agent Runtime Conventions

This plugin provides skills, not slash commands or plugin-defined subagent types. A skill is selected from its description when the user asks for its workflow in natural language. The skills are runtime-neutral: they work in any agent runtime that loads `SKILL.md` skills (via a plugin installation or the `.agents/skills` discovery convention) and with any model.

## Terminology

Throughout this plugin, *collaboration subagent* means whatever delegation mechanism the current runtime provides — a task tool, a subagent, a collaboration agent, or a forked session. If the runtime provides none, the primary agent performs the work itself.

## Plugin resources

`shared/...` is a plugin-root-qualified logical path. It is never relative to the process working directory, planning root, target repository, or `skills/<skill-name>/` directory.

**Resource boundary:** The installed plugin tree is read-only workflow input. Never copy, sync, vendor, scaffold, or symlink the plugin, its `skills/`, any `SKILL.md`, or its `shared/` resources into the working directory, target repository, or planning root. Read all bundled files in place from `<plugin-root>`. Bundled templates may produce generated SDD artifacts and SDD configuration/guidance in the workspace; the template source stays in the plugin. Skills whose declared purpose includes source changes may still edit target code, but no skill may install plugin material into the target.

If a bundled resource cannot be read from the resolved plugin root, stop and report the missing or inaccessible installation. Copying the resource into the workspace is not a fallback.

Resolve `<plugin-root>` in this order:

1. Use the absolute path of the loaded skill when the runtime exposes it. Canonicalize the path by following symlinks before ascending from `<plugin-root>/skills/<skill-name>/SKILL.md` to the directory containing the sibling `skills/` and `shared/` directories. OpenCode installations commonly expose `$HOME/.agents/skills/<skill-name>` as a symlink into `$HOME/.agents/plugins/<plugin>/skills/<skill-name>`; the latter identifies the plugin root.
2. Otherwise search the runtime's active skill locations for `skills/<skill-name>/SKILL.md`, canonicalizing every candidate before deriving its root. Standard locations include repository and user Agent Skills roots (`.agents/` and `$HOME/.agents/`), OpenCode plugin sources under `$HOME/.agents/plugins/<plugin>/`, runtime-configured skill roots, and Codex's installed plugin cache at `${CODEX_HOME:-$HOME/.codex}/plugins/cache/<marketplace>/<plugin>/<version>/`.
3. Accept a candidate only when it contains all of `skills/<skill-name>/SKILL.md`, `shared/agent-runtime.md`, and `.codex-plugin/plugin.json`, and the manifest's `name` is `sdd-planner`.

After resolving the root, expand every `shared/<path>` reference to `<plugin-root>/shared/<path>`. Pass these resolved absolute paths to collaboration subagents; do not make a subagent repeat discovery.

If multiple candidates remain and the loaded skill path or active runtime configuration does not identify one, stop and report the ambiguity. Never choose by current directory, cache ordering, modification time, or highest-looking version.

## Delegation

Use collaboration subagents only when they are available and a task is independent enough to benefit from parallel work. Do not depend on plugin-defined agent names, specific models, tool allowlists, or any particular delegation API. A skill must remain correct when performed by the primary agent alone.

Skills may expose stable semantic dispatch identifiers for independent work.
When a runtime provides a task name or description field, pass the identifier
through unchanged so an external runtime adapter can select a worker. The
identifier is not an agent name and does not change the required fallback to a
transparent single-agent workflow.

Implementation dispatches use the stable identifier `implement_task` (D-0009).
Pass it unchanged in the runtime task name or description field when available;
do not request an agent, worker type, provider, or model.

## Project guidance

Use `AGENTS.md` for optional repository-level guidance. Do not create, update, or discover `CLAUDE.md`, `.claude/`, or `~/.claude/` paths.
