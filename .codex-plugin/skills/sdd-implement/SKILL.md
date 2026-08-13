---
name: sdd-implement
description: "Execute an approved spec-driven implementation plan phase, update task statuses, and verify code changes. Use when asked to implement an active plan or execute a specific phase or task."
---

# Execute a Plan Phase

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md`, `<plugin-root>/shared/path-resolution.md`, `<plugin-root>/shared/vcs-detection.md`, `<plugin-root>/shared/autonomy.md`, `<plugin-root>/shared/completion-evidence.md`, and `<plugin-root>/shared/language-verification.md` with the matching `<plugin-root>/shared/language-specs/` reference file.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## Preconditions

Read the active plan and phase frontmatter. Read the decision ledger's frontmatter, if one exists (resolve per `shared/decision-log.md` § Ledger location), and note `accepted` entries scoped to this plan or its related specs/designs — pass the relevant statements to implementation dispatches as constraints, but never to intent-isolated review lanes. Confirm the target repository, task dependencies, acceptance criteria, and verification commands. If the plan contradicts the codebase, has an unresolved external dependency, or lacks required clarification, stop and surface the mismatch rather than silently changing scope.

## Process

1. Select unfinished tasks whose dependencies are complete. Group independent tasks only when their expected file ownership does not overlap.
2. For each task, confirm it defines one clean, complete, independently
   bisectable feature or internal-capability slice and an explicit native SCM
   revision/checkpoint boundary (D-0014, D-0015). Split or reorder it when a
   smaller complete dependency-ordered unit is available. If it is a horizontal
   half-feature, combines independently complete slices, or cannot leave the
   repository buildable and passing at its boundary, stop and revise the plan
   rather than improvising revision grouping. Then inspect the relevant
   specification/design, code conventions, and test infrastructure and
   implement the task and its tests as that one focused change.
3. Run the required verification and relevant project checks. Fix failures caused by the change. Report pre-existing failures distinctly, with actual output.
4. Optionally dispatch each independent implementation task through collaboration when available. When the runtime provides a task name or description field, set that field to exactly `implement_task` unchanged. Each dispatch supplies exactly one plan task, its target paths, acceptance criteria, `### Trap` content (or that no trap is present), relevant accepted-decision statements as constraints, and verification requirements. Do not request an agent, worker type, provider, or model. Do not depend on any delegation API; when collaboration is unavailable, the primary agent implements the same task transparently. (D-0009)
5. Before finalizing a task's native SCM revision/checkpoint, perform a focused
   code review of its complete diff for correctness, scope, tests,
   maintainability, and whether it is a complete bisectable feature slice.
   Inspect the SCM status and relevant diff views; exclude unrelated work. The
   focused review is required for every completed task. When a fresh
   non-inheriting collaboration context is available, render
   `shared/templates/quality-scan-prompt.md` for the task diff and dispatch it
   as an intent-blind quality pass; otherwise perform the review yourself and
   label it self-review. Use `sdd-code-review`
   for phase-level review or material risk; its four-lane phase gate is not a
   substitute for this task review (D-0014).
6. Record the reviewed, verified task as the detected SCM's focused native
   revision/checkpoint. It contains the complete task implementation and tests,
   not another task and not SDD lifecycle/evidence bookkeeping. Confirm the
   recorded revision/checkpoint contains the exact verified bytes and the
   repository remains buildable and testable at that identity. Do not defer the
   native revision/checkpoint merely to collect evidence.

   **Git adapter:** in a commit-capable Git workflow where commits are
   authorized, this native revision is one immutable scoped implementation
   commit (D-0016, D-0017, D-0018). A dirty worktree cannot complete; commit
   the complete slice before recording completion evidence.
7. While the task remains `in-progress`, create `### Completion Evidence` if a
   legacy task lacks it, then replace its pending content using
   `shared/completion-evidence.md`:
   record the verification date, the native SCM revision/checkpoint as canonical
   tested source identity, its immediate identity recheck, every exact
   command with working directory and exit status, every non-command inspection,
   and the observable results satisfying the task's prospective `verification`.
   Also record `Focused review` in strict syntax: for Git, exactly `git show
   <full40>` for final-commit review or `git diff <full40>..<full40>` for range
   review in backticks before `; complete task diff reviewed for correctness,
   scope, tests, maintainability, and task boundary`; then record `Reviewed
   candidate / final` as the exact native SCM identity. **Git review-identity
   adapter:** it is the task full commit or `diff: <full40>..<full40>` with
   distinct commits, a base that is the task revision's direct first parent, and
   an endpoint at that revision, with both commits present in the target
   repository; the command uses that identity with no extra operands. Record
   `Review result: PASS/Aligned`. Other SCMs use their native exact identity; do
   not claim unsupported alternate-diff validation. Do not invent a fallback
   source identity. Dirty Git, no-SCM, and unsupported SCM adapters remain
   non-complete until a durable native revision/checkpoint exists. Do not paste
   a claim unsupported by output read in this session or a linked
   contemporaneous durable record.
8. Re-read the task section. Confirm the evidence is present, no pending marker
   remains, at least one command or tool/inspection row exists, every required
   verification behavior is covered, the source-identity recheck still matches
   the tested content, and every final required check passed.
   Only then set the task status to `complete`, check completed subtasks, and
   update frontmatter. A task with absent, pending, vague, or failing evidence
   stays non-complete.
9. Write the task status, checkboxes, completion evidence, and any
   task-related decision-ledger entry in-flow as each fact becomes known; do
   not commit these individual writes (D-0019). At task closeout, record all
   accumulated task-related updates once per affected SCM root
   (`shared/autonomy.md` § SCM boundary cadence).
   This lifecycle record must preserve the task's complete state and must not
   mix source or another feature slice. Re-read the recorded artifact and run
   `sdd validate --scope Plans/<PlanName> --format json` from the target
   repository root, resolving any diagnostics on the tasks just completed;
   completion is not finalized while either the
   implementation or lifecycle record lacks its durable native SCM identity. A
   planning root without approved durable lifecycle transport may preserve
   handoff state but remains non-complete.

   **Git adapter:** in a commit-capable workflow where commits are authorized,
   make the task-close commit once per affected root after the immutable
   implementation commit. Never create intermediate SDD-only commits for
   plan/evidence/checklist/status/decision edits. Evidence continues to name the
   tested implementation commit, avoiding a self-referential lifecycle SHA; the
   strict planning-`HEAD` durability requirement still applies.
   **Unsupported transport:** no validated durable lifecycle adapter currently
   exists for Perforce or no-SCM planning roots, so leave the task non-complete
   and report that limitation rather than claiming lifecycle completion.

## Escalate

Ask the user before destructive or production-impacting operations, when requirements are ambiguous, when implementation reveals unplanned scope, or after two failed attempts to resolve a blocking verification failure.

**Record escalation resolutions.** When the user answers an escalation with a choice that constrains future work — an ambiguity resolved, scope accepted or cut, an approach picked for a blocked task — record it in the decision ledger per `shared/decision-log.md` (apply its admission test — the binding has to reach past the task that raised the escalation; collision check before appending; a collision is itself a stop). If the fresh answer collides with an accepted entry, use the ledger's **one-step supersession**: "this supersedes D-NNNN — confirm?" — don't make the user relitigate what they just decided. Scope the entry to the plan. Pure one-off dispositions ("retry it", "skip for now") are events, not decisions — don't log them.

## Output

For each completed task report files changed, verification commands with actual results, the populated completion-evidence section, status updates, and any deferred findings. Deferred review findings are tracked per `shared/review-artifacts.md` (plan tasks or `FU-NN` follow-ups) — never left only in conversation. Keep the plan artifact as the source of truth.
