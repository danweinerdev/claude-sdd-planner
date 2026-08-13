# Orchestration Model

The primary agent owns user communication, approvals, scope decisions, and final synthesis. It may delegate independent reading, implementation, or review work to collaboration subagents when they are available.

## Principles

1. Delegate only independent, bounded work. Keep cross-cutting decisions and artifact status changes with the primary agent.
2. Parallelize tasks only when their expected file ownership and dependencies do not overlap.
3. Treat agent output as evidence to validate, not as proof. Preserve actual command output for verification claims.
4. Skills must work without collaboration agents. Fall back to a transparent single-agent workflow and never claim independent review when none occurred.
5. After compaction, re-read plan and phase frontmatter plus unresolved user questions; do not rely on remembered status.

## Bundled role prompts

Multi-agent behavior is expressed as bundled, runtime-neutral role prompts that any skill can render and dispatch when collaboration subagents are available:

- `shared/agent-prompts/researcher.md` — context gathering across artifacts, codebase, and docs. Used by `sdd-specify`, `sdd-design`, and `sdd-plan`; `sdd-brainstorm`, `sdd-research`, and `sdd-debrief` may render it instead of writing an ad-hoc scan prompt.
- `shared/agent-prompts/spec-reviewer.md` — independent specification review (testability, completeness, ambiguity, scope, gated work) with an Approve/Revise verdict.
- `shared/agent-prompts/plan-reviewer.md` — independent plan/design review (completeness, feasibility, conventions, gaps, gated work) with an Approve/Revise verdict.
- `shared/review-prompts/` — the four code-review lanes (see `shared/review-lanes.md`).

Dispatch rules: substitute every `{{PLACEHOLDER}}` before dispatch; reviewer dispatches get a fresh context that does not inherit the primary conversation, so the artifact is judged as written rather than as intended. Without collaboration subagents, the primary agent performs the same pass by following the prompt and labels the result **self-review** — never claiming independent corroboration.

## Session orientation

Read `planning-config.json`, active plan frontmatter, current phase task status, and the latest debrief before resuming implementation. Also read the decision ledger's **frontmatter**, if it exists (`Decisions/decisions.md` under the planning root, or `<repo-root>/DECISIONS.md` for external planning roots — `shared/decision-log.md` § Ledger location) — `accepted` entries are standing constraints on all planning work, and after compaction the ledger is the truth about which decision won, not the summary's memory. Read full artifact bodies only when the current decision needs them.
