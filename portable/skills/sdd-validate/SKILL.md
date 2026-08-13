---
name: sdd-validate
description: "Validate SDD artifact structure, statuses, completion evidence, dependencies, identifiers, and decision-ledger consistency without modifying files. validate plan, check SDD integrity, audit completion evidence, validate artifacts"
---

# Validate SDD Artifacts

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
Before implementation, before any completion transition, before handoff, or in CI. Read-only: validation never edits, moves, creates, or deletes artifacts and never changes a status — it reports exact findings for a lifecycle skill or user-authorized repair to address.

## Process

### 1. Run the deterministic validator

From the target repository root:

```bash
sdd validate --format json
```

Pass `--scope <planning-root-relative-path-or-artifact-name>` when the user named a narrower scope. Scoped validation includes every discoverable artifact selected by that path/name and follows transitive explicit `related` links — a plan scope covers all plan-owned artifacts plus its governing related graph even when the scope names its README or one phase. Bare names that match multiple artifact roots are rejected as ambiguous; use the reported planning-root-relative path. JSON output lists the exact successfully parsed `artifacts_in_scope`; diagnostics for malformed files under the requested scope remain visible. Unresolved references remain diagnostics on the artifact that cites them; an existing but undiscoverable file is never claimed as validated.

If `sdd` is unavailable, report the error and stop rather than silently replacing deterministic checks with model judgment. Exit `0` means scripted checks passed, exit `1` means the JSON diagnostics are authoritative findings, and exit `2` means validation could not run. Never execute artifact-recorded evidence commands as part of validation.

For a direct decision-ledger write or focused ledger audit, `sdd decide validate <resolved-ledger> --format json` provides the stricter standalone format, archive, supersession, structural-candidate, and Git-backed immutability checks required by `shared/decision-log.md`. The full validator remains authoritative for cross-artifact scope resolution, citations, and related-graph checks.

Identity mode defaults to `auto`, which performs current target-worktree and governing lifecycle-content checks for every populated evidence section. Use the equivalent explicit `--identity-mode current` immediately before a completion transition. Use `--identity-mode historical` only for a confirmed historical audit where later legitimate work makes current-source comparison inappropriate.

### 2. Semantic pass (model judgment)

The script owns machine-decidable structure, schema, path, identifier, graph, review-state, decision-link, native-SCM identity, and evidence-shape checks. Do not repeat those checks manually or reinterpret a scripted failure. After it runs, use model analysis only for semantic sufficiency: whether prose has real content, evidence proves the cited behavior and acceptance criteria, aggregate evidence covers the deliverable, and differently worded decisions potentially conflict. Merge those semantic findings with the script diagnostics in the required output format.

For every scoped spec, design, or plan, resolve its explicit `related` graph and perform a cross-artifact semantic reconciliation after the script passes its citation matrix checks:

- Compare each `FR-NN`, `NFR-NN`, constraint, and accepted decision in the spec with the linked design. Report omitted behavior, incompatible contracts, and design choices that exceed or narrow approved scope.
- Compare the linked design with plan phases, task boundaries, dependencies, traps, and verification. Report architecture with no implementation task, tasks that contradict the design, and verification that cannot prove the governing `AC-NN` behavior.
- Compare the plan directly back to the spec so a shared design omission cannot make both downstream artifacts appear mutually consistent.
- Actively test the unhappy paths named by the governing artifacts: null/empty boundaries, ownership and cleanup, errors, concurrency, retries/timeouts, and cross-tenant or security boundaries where applicable.

The script proves citation presence, not semantic conformance. Never report the artifacts reconciled merely because every identifier appears somewhere. For a large scope (many plans or a deep related graph), delegate the semantic reconciliation reads to the researcher prompt (`shared/agent-prompts/researcher.md`) and validate its summary against the script diagnostics — don't skip the pass because the scope is big.

## What the script checks (summary)

- **Structure/frontmatter**: YAML parses; required fields; status vocabulary per `shared/frontmatter-schema.md`; required template sections with exact headings; `doc`/`related`/scope paths resolve.
- **Hierarchy/traceability**: README `phases[]` match phase docs; `depends_on` resolves acyclically; `FR/NFR/AC/F/FU/D` ids unique in their owning artifact and citations resolve through the `related` graph; complete parents contain no incomplete children; approved+ artifacts have no blocking Open Questions (retained bullets use the exact `**non-blocking** — <rationale>` form).
- **Review artifacts**: finding statuses match Resolution Log dispositions; deferred findings are tracked (`FU-NN` or plan task); supersession links bidirectional; phase-gate reviews carry the exact `review_scope`/`frozen`/`verdict`/`review_mode`/`lane_results`/`reviewed_planning_revision` contract from `shared/review-artifacts.md`.
- **Completion evidence**: applies `shared/completion-evidence.md` literally — evidence sections present; complete entities carry conforming retrospective evidence with native-SCM identity, identity recheck, focused review in strict syntax, and passing checks; Git ancestry and lifecycle-commit checks; legacy `complete` artifacts without evidence reported as legacy gaps, never backfilled.
- **Decision ledger**: field/status validity, id uniqueness across live+archive, bidirectional supersession, stale citations to superseded/rejected entries, deterministic collision candidates (nonfatal — user judgment resolves them).

## Output

Open with `Valid` or `Invalid`. For every finding include severity, artifact and section/line, violated rule, and the exact required correction. Include the files and checks inspected so absence claims have a search trail. A valid result names the scope and confirms each check class — it is not a generic "looks good."

## Context
- Validator: `sdd validate` (deterministic layer; read-only)
- Ledger validator: `sdd decide validate`
- Conventions enforced: `shared/frontmatter-schema.md`, `shared/completion-evidence.md`, `shared/review-artifacts.md`, `shared/decision-log.md`
- Path resolution: `shared/path-resolution.md`
