# Path Resolution

Single source of truth for how sdd-planner skills and agents resolve the three roots they work from. The plugin-directory glob stays inline in each skill (you need the plugin directory before you can read this file); everything else is defined here once.

## Planning Root (artifacts)

Artifacts (`Research/`, `Brainstorm/`, `Specs/`, `Designs/`, `Plans/`, `Decisions/`) are read from and written to the **planning root**. Legacy `Retro/` and `Diagrams/` directories remain readable but are no longer created.

1. Find `planning-config.json`: look in the current working directory; if absent, walk up parent directories to the repository root.
2. If no `planning-config.json` exists anywhere, the planning root is the repository root (treat `planningRoot` as `"."`).
3. Otherwise resolve its `planningRoot` field:
   - `"."` or absent → the directory containing `planning-config.json`
   - Relative path (e.g., `"Planning"`) → resolved against the directory containing `planning-config.json`
   - Absolute path (e.g., `"/home/user/planning-repo"`) → used as-is (an external planning directory shared by multiple repos)

## Plugin Directory (templates, schema, shared conventions)

Templates and shared definitions (`shared/`) are read from the **plugin directory**, never from the planning root. The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`. If multiple matches are found (multiple cached plugin versions), sort them **as semantic versions** (like `sort -V`; a plain string sort puts `1.10.0` before `1.9.0` — wrong) and use the highest. Strip `commands/research/SKILL.md` from the matched path to get the plugin directory.

## Target Repository (code)

Plans may target code in a different repository. Resolution chain:

1. `planning-config.json` → `planMapping["<PlanName>"]` → repository key
2. `planning-config.local.json` (gitignored, sibling of `planning-config.json`) → `repositories.<key>.path` → local filesystem path
3. Verify the path exists on disk.

If any link in the chain is missing — no `planMapping` entry, no local path for the key, or the path doesn't exist — stop and ask the user for the target directory. Never guess, and never clone.
