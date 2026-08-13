# Path Resolution

## Planning root

Core artifacts (`Research/`, `Brainstorm/`, `Specs/`, `Designs/`, `Plans/`, and
`Decisions/`) are read from and written to the planning root. Existing legacy
artifact directories remain readable but are not created by the compact core.

1. Find `planning-config.json` in the current directory or its parents through the repository root.
2. If no config exists, use the repository root and treat `planningRoot` as `"."`.
3. If config exists, resolve `planningRoot` relative to the config's directory unless it is absolute. An absent value and `"."` mean the config directory.

## Plugin resources

Locate bundled resources as described in `shared/agent-runtime.md`. The `shared/` directory belongs to the installed plugin and is read in place; never copy or symlink it, the plugin, or skill files into the planning root or target repository. Templates under `shared/` may be rendered into generated SDD artifacts, but the template files remain under `<plugin-root>/shared/`.

## Target repository

Plans can target another repository:

1. Resolve `planning-config.json` `planMapping["<PlanName>"]` to a repository key.
2. Resolve the same `planning-config.json` file's `repositories.<key>.path`.
3. Verify that path exists.

If any part is missing, ask for the target directory. Never guess or clone.
