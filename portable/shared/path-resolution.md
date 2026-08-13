# Path Resolution

Single source of truth for how sdd-planner skills and agents resolve the three roots they work from. Plugin-directory discovery stays inline in each skill (you need the plugin directory before you can read this file); everything else is defined here once.

## Planning Root (artifacts)

Artifacts (`Research/`, `Brainstorm/`, `Specs/`, `Designs/`, `Plans/`, `Decisions/`) are read from and written to the **planning root**. Legacy `Retro/` and `Diagrams/` directories remain readable but are no longer created.

1. Find `planning-config.json`: look in the current working directory; if absent, walk up parent directories to the repository root.
2. If no `planning-config.json` exists anywhere, the planning root is the repository root (treat `planningRoot` as `"."`).
3. Otherwise resolve its `planningRoot` field:
   - `"."` or absent → the directory containing `planning-config.json`
   - Relative path (e.g., `"Planning"`) → resolved against the directory containing `planning-config.json`
   - Absolute path (e.g., `"/home/user/planning-repo"`) → used as-is (an external planning directory shared by multiple repos)

## Plugin resources

Locate bundled resources as described in `shared/agent-runtime.md`. The `shared/` directory belongs to the installed plugin and is read in place; never copy or symlink it, the plugin, or skill files into the planning root or target repository. Templates under `shared/` may be rendered into generated SDD artifacts, but the template files remain under `<plugin-root>/shared/`.

## Target Repository (code)

Plans may target code in a different repository. Resolution chain:

1. `planning-config.json` → `planMapping["<PlanName>"]` → repository key
2. `planning-config.local.json` (gitignored, sibling of `planning-config.json`) → `repositories.<key>.path` → local filesystem path
3. Verify the path exists on disk.

If any link in the chain is missing — no `planMapping` entry, no local path for the key, or the path doesn't exist — stop and ask the user for the target directory. Never guess, and never clone.
