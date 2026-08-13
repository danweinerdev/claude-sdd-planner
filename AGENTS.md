# SDD Planner — Agent Development Guide

Runtime-neutral instructions for any coding agent working **on this repository**. (`CLAUDE.md` carries the same material with Claude Code specifics; where they disagree, fix both — that is a doc bug.)

## What this repository is

A spec-driven development toolchain published to multiple agent harnesses from one source:

- **Repo root** — the canonical, hand-edited Claude Code plugin: `commands/` (lifecycle skills), `agents/` (review/implementation agent definitions), `skills/` (model-loaded reference skills), `shared/` (conventions + templates), `hooks/`.
- **`.codex-plugin/` and `.opencode-plugin/`** — GENERATED plugin trees for Codex and OpenCode, identical content, produced by `sdd plugin sync`. **Never edit these by hand** — any hand edit is destroyed by the next sync and rejected by the drift gate.
- **`cmd/sdd` + `internal/`** — the cross-platform Go binary behind every skill: deterministic validation (`sdd validate`), artifact writes, lifecycle transitions, hooks, and the portable-tree generator (`internal/portable`).
- **`.plans/`** — this repo's own planning artifacts. `.plans/Decisions/decisions.md` is the decision ledger; its `accepted` D-NNNN entries are standing constraints on all work here. A change that contradicts one must stop for user reconciliation, never silently proceed. The ledger itself is never edited — for any reason — without the user's explicit approval of the exact, unmodified text of the change, shown in full beforehand.

## Build and test

```bash
make build          # compile the sdd binary into build/<os>-<arch>-debug/
make test           # THE gate: Go suite + frozen regression corpus + template gate + portable drift/leak gates
make plugins        # regenerate .codex-plugin/ and .opencode-plugin/ (sdd plugin sync)
make plugins-check  # fail if the generated trees are stale
sdd plugin status   # provenance of every portable file (generated / variant / override)
```

Run `make test` before claiming any change works. It fails on:
- unit or regression failures (`go test ./...`, including the corpus under `tools/`)
- templates drifting from the schema (`sdd template --check`)
- either generated tree differing from a fresh generation
- Claude-isms leaking into portable output (`sdd-planner:`, `the Task tool`, `~/.claude`, …)

## Editing rules (the ones that bite)

1. **Edit canonical, regenerate portable.** After touching `commands/`, `skills/`, `agents/`, `shared/`, or `.claude-plugin/plugin.json`, run `make plugins` and commit the regenerated trees with the change.
2. **Variants shadow generation.** `commands/{code-review,implement,setup}/SKILL.portable.md`, `shared/review-lanes.portable.md`, and `shared/templates/custom-reviewer.portable.md` replace the generated transform of their canonical siblings wholesale. When you edit a canonical file that has a variant, decide whether the variant needs the same change — nothing does this for you.
3. **Harness markers** handle paragraph-level divergence inside one file: `<!-- claude-only -->…<!-- /claude-only -->` is dropped from portable output; a `<!-- portable-only … -->` comment block is uncommented into it. Used in `shared/path-resolution.md`, `shared/orchestration.md`, `shared/templates/quality-scan-prompt.md`.
4. **Agent edits propagate automatically.** The portable `shared/agent-prompts/` and `shared/review-prompts/` files are derived from `agents/*.md` — do not edit them directly. `code-implementer` has no prompt by design (D-0009: `implement_task` dispatches carry the task inline).
5. **Templates ↔ schema ↔ validator move together.** Any change to `shared/templates/`, `shared/frontmatter-schema.md`, `shared/completion-evidence.md`, or `shared/review-artifacts.md` must keep all three consistent — `sdd validate` is the enforcement layer.
6. **Every validation rule carries `Good` and `Bad` examples** (`internal/rules/`); the registry meta-test fails otherwise. After rule changes, `make gen-fixtures` and commit the corpus.
7. **Never regenerate `tools/parity/frozen-expectations.json`.** It is the retired Python validator's last recorded verdict — the definition of "correct", not a test output.
8. **`shared/decision-framework.md` ↔ agents**: the framework's canonical digest block is embedded verbatim as `## Decision Framework` in all eight `agents/*.md`; re-sync them when it changes.
9. **Docs stay in sync**: `README.md`, `CLAUDE.md`, `AGENTS.md`, and the setup-generated guidance templates (`shared/templates/claude-md-*.md`, `agents-md-*.md`) mirror the skill/agent tables and directory layout.

## Conventions (single sources of truth)

The `shared/` documents are normative — read them before changing behavior they govern:

| Topic | Document |
|---|---|
| Artifact frontmatter + statuses + sensitive-data rules | `shared/frontmatter-schema.md` |
| Evidence-gated completion (task/phase/plan) | `shared/completion-evidence.md` |
| Decision ledger: schema, admission test, collisions | `shared/decision-log.md` |
| Decision discipline for every skill/agent | `shared/decision-framework.md` |
| Planning-root / plugin-dir / target-repo resolution | `shared/path-resolution.md` |
| VCS detection + git/p4/plain operations table | `shared/vcs-detection.md` |
| Orchestration + portable role-prompt catalog | `shared/orchestration.md` |
| Review lanes, four-lane isolation, project socket | `shared/review-lanes.md`, `shared/review-artifacts.md` |
| Portable runtime resolution + delegation contract | `shared/agent-runtime.md` |

Key invariants worth internalizing: plan tasks are single clean bisectable native-SCM revisions with lifecycle bookkeeping in separate scoped commits; `complete` is never set without conforming retrospective evidence; phase completion requires a persisted frozen four-lane `Aligned` review; every plan task carries a `justifies` source or is cut; artifacts never contain credentials or machine-specific absolute paths.

## Versioning

`.claude-plugin/plugin.json` is the single version source; `internal/version/version.go` and both portable manifests are generated from it.

```bash
make bump-patch|bump-minor|bump-major     # test-gated bump + commit + tag, syncs all trees
python3 bump-version.py set X.Y.Z         # explicit forward jump (no downgrades)
python3 bump-version.py set-floor X.Y.Z   # advance minSddVersion deliberately (D-0015)
```

Harnesses cache plugins by version — a content change without a bump is invisible to users.

## The `sdd` binary contract

Users install it themselves (`go install github.com/danweinerdev/claude-sdd-planner/cmd/sdd@latest`); the plugin never ships, compiles, or downloads binaries (D-0015). Setup skills verify `sdd version` against the manifest's `minSddVersion` and stop with the install command on failure. Exit codes: `0` success, `1` refused mutation / authoritative findings, `2` malformed invocation or could-not-run.
