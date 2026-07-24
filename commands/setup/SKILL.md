---
name: setup
description: "Set up a directory for sdd-planner — generates planning-config.json, bootstraps planning directories, creates launcher scripts. Triggers: /setup, setup repo, configure repo, setup worktree, initialize planner, bootstrap planner"
---

# /setup — Configure a Directory for SDD Planner

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Setup *writes* the config the other skills resolve from — `shared/path-resolution.md` defines how the `planningRoot` written here is later interpreted.

## When to Use
When configuring a directory (an existing repo, a fresh `git init`, a Perforce workspace, or an empty directory) to work with the sdd-planner plugin. **Idempotent and safe to re-run**: overwrites `planning-config.json` only when the resolved `planningRoot` differs, overwrites launcher scripts, and creates only missing directories.

What setup does **not** do: resolve paths (user-given paths are stored verbatim — relative stays relative, absolute stays absolute), initialize a VCS, copy plugin files/templates/schemas into the target (the plugin reads its own files in place), write CLAUDE.md unprompted, or handle bare git repos (run it on a worktree instead).

## Arguments
- **target path** — directory to configure (defaults to cwd). A non-path name is looked up in the current directory's `planning-config.local.json` under `repositories.<name>.path`.
- **--planning-root `<path>`** — where planning artifacts live; stored as-given.
- **--dashboard** — set `"dashboard": true` for the companion `sdd-dashboard` plugin.

## Process

### 1. Target and VCS
Determine the target directory (see Arguments; verify it exists). Detect its VCS per `shared/vcs-detection.md`: `git`, `git-worktree`, `git-bare`, `perforce`, or `none`. For `git-bare`, **stop**: "This is a bare git repository. Run setup on individual worktrees instead." Everything else proceeds; the VCS only affects the ignore-file step.

### 2. Resolve Planning Root
Priority order — the chosen value is stored **verbatim**, never resolved to absolute:
1. `--planning-root` flag.
2. Existing `<target>/planning-config.json` — reuse its `planningRoot` and report `planning-config.json: OK (existing planningRoot preserved)`.
3. Sibling inheritance (`git-worktree` only): check `git -C <target> worktree list --porcelain` siblings for a `planning-config.json` and inherit its `planningRoot`, reporting which sibling.
4. Ask (skip when context makes it obvious): at the target root (`"."`, default), a relative subdirectory (e.g., `Planning`), or an absolute external path. **Never default to the plugin directory** — the marketplace cache is deleted on plugin updates.

Also ask about the dashboard opt-in unless `--dashboard` was passed or inherited.

### 3. Write planning-config.json
```json
{ "planningRoot": "<verbatim>" }
```
With the dashboard opt-in, also include `"dashboard": true`, `"title"`, and `"description"` (read by the companion `sdd-dashboard` plugin; ignored otherwise). Overwrite only when `planningRoot` differs from an existing config — but still add/update `dashboard` when explicitly requested.

### 4. Bootstrap Planning Directories
Resolve the planning root for this step only (relative → joined with target; absolute → as-is) and `mkdir -p`:
```
Plans/  Research/  Brainstorm/  Specs/  Designs/  Decisions/
```
Plan lifecycle is tracked in each plan README's frontmatter `status` — `Plans/` stays flat. Report created vs already-existing.

### 5. Write Launcher Scripts
Create both launchers in the target unconditionally, passing the `planningRoot` value through verbatim (`claude --add-dir` accepts relative and absolute paths).

`claude.sh` (make executable):
```bash
#!/usr/bin/env bash
# Launch Claude Code with planning context
exec claude --add-dir="<planning-root verbatim>" "$@"
```

`claude.cmd`:
```cmd
@echo off
claude --add-dir="<planning-root verbatim>" %*
```

### 6. Ignore File
`git`/`git-worktree` → `.gitignore`; `perforce` → `.p4ignore` (its syntax: one path per line, no leading slash); `none` → skip. Ensure these entries exist without duplicating: `Dashboard/` (generated HTML, harmless if the dashboard plugin isn't installed) and `planning-config.local.json` (local filesystem paths).

### 7. Offer CLAUDE.md Guidance (optional)
Ask before writing anything. For a dedicated planning-only repo, instantiate `shared/templates/claude-md-full.md`; for an existing project, append `shared/templates/claude-md-snippet.md`. Skip silently if declined.

### 8. Report
Summarize: target path (as given), detected VCS, planning root (verbatim, noting relative/absolute), dashboard flag, files created/updated (config, directories, launchers, ignore file, CLAUDE.md guidance created/appended/skipped), and the next step: `cd <target> && ./claude.sh`.

## Context
- VCS detection: `shared/vcs-detection.md`
- Config: `planning-config.json`, `planning-config.local.json`
