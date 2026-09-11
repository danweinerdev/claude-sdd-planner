---
name: sdd-setup
description: "Set up spec-driven planning in a repository by creating planning-config.json, artifact directories, ignore entries, and optional AGENTS.md guidance. Use when asked to initialize or configure planning for a repository or worktree."
---

# Configure Spec-Driven Planning

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md`, `<plugin-root>/shared/path-resolution.md`, `<plugin-root>/shared/vcs-detection.md`, `<plugin-root>/shared/templates/agents-md-full.md`, and `<plugin-root>/shared/templates/agents-md-snippet.md`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## Process

0. **Verify the `sdd` binary.** Every skill drives the cross-platform `sdd`
   toolchain (validation, artifact writes, lifecycle transitions). Run
   `sdd version` and compare against the `minSddVersion` in the plugin
   manifest (`plugin.json` at the plugin root). If the binary is
   missing or below the floor, stop and report the exact remedy — the plugin
   never compiles, downloads, or installs anything:

   ```bash
   go install github.com/danweinerdev/claude-sdd-planner/v2/cmd/sdd@latest
   ```

   Run `sdd decide capabilities --json` too. If the target's existing config
   selects a fork, require canonical `decision_forks` with schema 1,
   `entry-v1` canonicalization, and transaction support. Missing/old support
   stops with the install guidance; never fall back to a conventional ledger.

1. Determine the target directory. Use the user-provided path verbatim where possible; otherwise use the current directory. Stop for a bare git repository.
2. Determine `planningRoot`: explicit user value, existing `planning-config.json`, inherited worktree value, then `"."`. Preserve the chosen value exactly in config; resolve relative paths only for filesystem operations.
3. Write or preserve `<target>/planning-config.json`. Include optional `title`, and `description` only when requested. Preserve every existing `repositoryId`, `decisionLog`, and unrelated key: the represented repository's config owns decision selection and its configured planning root stores the selected collection. Setup never adopts, retargets, detaches, repairs, or creates a second repo-root selector.
   Existing-config replacement may use the accepted staged remove-and-rename path. Warn that a rare interruption can leave config absent. Manual restoration is permitted: inspect retained staging bytes and, only after explicit user confirmation of those exact bytes, rename that stage into place. Never synthesize config or promise automatic/no-gap recovery; use fork inspect/recover after config is readable if needed.
4. Create missing planning directories: `Plans/`, `Research/`, `Brainstorm/`, `Specs/`, `Designs/`, and `Decisions/`.
5. Repository mappings and paths belong in `planning-config.json`; do not create a local companion config.
6. Offer, but do not unprompted create, `AGENTS.md` guidance. Use the full template for a dedicated planning repository; append the snippet for an existing project. Preserve existing user instructions.
7. Do not create Claude launchers, `CLAUDE.md`, `.claude/` folders, plugin symlinks, or copies of the plugin, skills, or shared resources. The runtime discovers and reads the installed plugin through its own skill-discovery paths; setup writes only SDD configuration, artifact directories, ignore entries, and user-approved `AGENTS.md` guidance to the target.

## Output

Report target path, detected VCS, stored planning root, created directories, config/ignore changes, and whether `AGENTS.md` guidance was added.
