---
name: sdd-implement
description: "Execute a plan. Graph plans walk the frontier: claim → red → green → sync → merge, observation-gated end to end. v1 markdown plans keep the wave protocol until converted. Use when asked to implement an active plan, execute a phase, or walk a plan graph."
---

# Walk the Plan Graph

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md`, `<plugin-root>/shared/path-resolution.md`, `<plugin-root>/shared/vcs-detection.md`, `<plugin-root>/shared/autonomy.md`, `<plugin-root>/shared/completion-evidence.md`, and `<plugin-root>/shared/language-verification.md` with the matching `<plugin-root>/shared/language-specs/` reference file.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## Routing: Graph Plans vs v1 Plans

Route by graph presence:

- **`Plans/<Name>/<Name>-Graph.json` exists** → the walk loop below. States derive from observations; completion is sync-only; the `sdd` binary refuses everything a narrated protocol used to let drift.
- **No graph** → the plan is a v1 markdown plan and keeps the v1 protocol (§ v1 Plans below) until converted with `sdd graph convert`.

## The Walk Loop

The loop is **claim → red → green → sync → merge**, repeated until the frontier is empty. Every arrow is a CLI call whose refusal text names the fix. You never assert an outcome — you show the tool a report and it records what the report says.

1. **Preconditions.** Plan README `status` is `approved` or `active` (flip `approved` → `active` when starting). `sdd graph status --plan <Name>` for the derived picture: state counts, closure, claims.
2. **Claim.** `sdd next Plans/<Name> --claim --by <identity>` claims the heaviest claimable frontier node, records a lease, and allocates an isolated workspace (git targets: a worktree on its own branch). The printed payload carries contract, cited requirement text, named tests, hazard triage, and workspace path — don't re-read plan documents for what it already states. When nothing is claimable, react to the refusal's actual reason (capacity, BLOCKED/RED nodes, or a genuinely finished walk).
3. **Red — prove the tests can fail.** Write the node's **named tests first** (runner-visible ids must match the payload exactly), run them in the workspace against the unimplemented or broken state, and sync the failing report: `sdd graph sync --plan <Name> --node <id> --by <identity> --report red.xml`. A red run is a **successful** sync — it stamps `red_seq`, which arms red-before-green: a hazard-discharging test never observed failing will refuse the later green.
4. **Green — implement, commit, sync the pass.** Implement inside the workspace until the named tests pass. Commit the complete slice in the workspace first — sync refuses a passing report from a dirty worktree, because the revision anchor must name the tested bytes. Then sync the passing report. A clean pass by the claim holder **merges atomically**: observation recorded, claim cleared, workspace released. A shared-dirty pass records provisionally instead (STALE, never GREEN) until a clean re-verify. There is no assert path.
5. **Integrate the slice into the mainline.** The merging sync completes the *claim*; the VCS integration is a separate deliberate act because it can conflict, and conflicts are judgment. On git targets the workspace branch survives the release (`git branch --list 'graph/<id>-*'`) — merge it into the mainline checkout now. Until the bytes land on the mainline the node honestly derives STALE from the shared tree; integration self-heals it to GREEN. Integrate after every merge, before the next claim of dependent work.
6. **Between rounds.** `sdd graph status` between claims; `sdd graph path` when choosing what to unblock. **Command gates:** run the gate's command and `sdd graph sync --node <id> --command-exit <N> --command-log out.txt`. **Review gates:** run the four-lane review flow (`sdd review scaffold` → fill lanes → `sdd review resolve`), then `sdd graph review --plan <Name> --node <gate> --artifact <frozen review path>`. The artifact must be `resolved` + `frozen: true` + verdict `Aligned` (all three — a reopened review is not evidence), must review a document of **this plan**, and greens exactly **one** gate. Findings that name scope nodes demote them to RED in the same write — the finding doing its job, not an error to route around. Record gates **after** the scope's work is integrated into the mainline.

### Stopping Rules

- **2 consecutive failures** on one node → the node is probably too big: propose `sdd graph split --plan <Name> --node <id> --file children.json` (each child one red→green cycle; the split is compile-gated and retires the parent id).
- **3 consecutive failures** → **stop and escalate to the user.** Do not grind a third variant of the same approach into the same node.

### Reaction Protocol

- **INTENT-STALE:** a cited requirement's fingerprint changed. Re-read the *diff of that requirement only*; then judge — cosmetic change → recompile refreshes the hash; behavioral change → rework the node; contract invalidated → replan. A judgment step; never auto-pick.
- **Finding demotion:** demoted nodes re-enter the workable set like any red node. Rework them; the gate going seq-stale after rework is the system asking for re-review.
- **Lease expiry / crashes:** an expired claim's workspace is preserved as post-mortem evidence. Inspect if useful, then `sdd graph gc --plan <Name>`; the node returns to the frontier. A stale claimant's late sync is refused.
- **Abandoning a node:** `sdd graph release <id> --by <identity>` — never squat on a claim you aren't working.

### Evidence Language

For graph plans, **observation records and rendered views are the completion record.** You never write completion-evidence prose as a gate input, never edit `<Name>-Graph.json` by hand (every mutation goes through a verb), and never edit rendered `NN-*.md` views (the renderer overwrites them, or refuses when frozen). "Where are we?" is answered by `sdd graph status` / `sdd graph export --format plan` output — derived truth, not narration.

### Delegating Node Execution

Node work may be dispatched through collaboration when available. When the runtime provides a task name or description field, set it to exactly `implement_task` unchanged. Each dispatch supplies the **claim payload verbatim** — node id, contract, cited requirement text, named tests, hazards and the shapes their discharging tests must take, workspace path, VCS label — plus the red-first rule. The dispatched work happens **inside the claimed workspace**; the deliverables are the red report file, the green report file, and the workspace commit. The coordinator (the claim holder) runs every `sync` itself — observation recording stays with the identity that holds the lease. Do not request an agent, worker type, provider, or model; when collaboration is unavailable, implement the node the same way yourself. Reject any report that asserts outcomes without report files.

## v1 Plans (no graph)

v1 markdown plans keep this protocol until converted.

**Preconditions.** Read the active plan and phase frontmatter. Read the decision ledger's frontmatter, if one exists (resolve per `shared/decision-log.md` § Ledger location), and note `accepted` entries scoped to this plan or its related specs/designs — pass the relevant statements to implementation dispatches as constraints, but never to intent-isolated review lanes. Confirm the target repository, task dependencies, acceptance criteria, and verification commands. If the plan contradicts the codebase, has an unresolved external dependency, or lacks required clarification, stop and surface the mismatch rather than silently changing scope.

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
9. Write the task status, checkboxes, and completion evidence in-flow as each
   fact becomes known; do not commit these individual writes (D-0024). A
   task-related decision-ledger entry is written in-flow only **after** the
   user has explicitly approved its exact, unmodified text
   (`shared/decision-log.md` write gate). Do not commit at task closeout:
   every accumulated lifecycle update is recorded once per affected SCM root
   at phase close, alongside the final review and debrief
   (`shared/autonomy.md` § SCM boundary cadence).
   That lifecycle record must preserve every task's complete state and must not
   mix source or another feature slice. Re-read the recorded artifact and run
   `sdd validate --scope Plans/<PlanName> --format json` from the target
   repository root, resolving any diagnostics on the tasks just completed; a
   committed-copy check reported as pending is expected mid-phase, and phase
   completion is not finalized while either the implementation or the
   lifecycle record lacks its durable native SCM identity. A planning root
   without approved durable lifecycle transport may preserve handoff state but
   remains non-complete.

   **Git adapter:** in a commit-capable workflow where commits are authorized,
   make the task-close commit once per affected root after the immutable
   implementation commit. Never create intermediate SDD-only commits for
   plan/evidence/checklist/status/decision edits. Evidence continues to name the
   tested implementation commit, avoiding a self-referential lifecycle SHA; the
   strict planning-`HEAD` durability requirement still applies.
   **Unsupported transport:** no validated durable lifecycle adapter currently
   exists for Perforce or no-SCM planning roots, so leave the task non-complete
   and report that limitation rather than claiming lifecycle completion.

When a v1 plan keeps generating drift the evidence rules exist to catch, offer conversion instead of more discipline: `sdd graph convert --plan <Name>` (its sentinels are real judgments to resolve through the payload path, never defaults). After a converted plan compiles, run the on-ramp before walking: history grants nothing, so every completed v1 task is an unverified node until observations exist — `sdd graph reverify --plan <Name> --report <suite report>` (add `--command-exit`/`--command-log` for command gates) folds one real run against every foldable node in dependency order, and the frontier then offers the genuinely remaining work instead of the already-done past.

## Escalate

Ask the user before destructive or production-impacting operations, when requirements are ambiguous, when implementation reveals unplanned scope (for graph plans the remedy is a new proposal payload, never silent extra work inside a claim), when a graph node hits its third consecutive failure, or after two failed attempts to resolve a blocking verification failure.

**Record escalation resolutions.** When the user answers an escalation with a choice that constrains future work — an ambiguity resolved, scope accepted or cut, an approach picked for a blocked task — record it in the decision ledger per `shared/decision-log.md` (apply its admission test — the binding has to reach past the task that raised the escalation; collision check before appending; a collision is itself a stop). If the fresh answer collides with an accepted entry, use the ledger's **one-step supersession**: "this supersedes D-NNNN — confirm?" — don't make the user relitigate what they just decided. Scope the entry to the plan. Pure one-off dispositions ("retry it", "skip for now") are events, not decisions — don't log them.

## Output

- Graph plans: observations and claims land in `<Name>-Graph.json` through the verbs; views re-render at the next compile; code lands in workspaces, merges via sync, and integrates to the target repository's mainline. Report per node: the red and green report evidence, the workspace commit, and the sync/merge results — never a narrated completion.
- v1 plans: for each completed task report files changed, verification commands with actual results, the populated completion-evidence section, status updates, and any deferred findings. Deferred review findings are tracked per `shared/review-artifacts.md` (plan tasks or `FU-NN` follow-ups) — never left only in conversation. Keep the plan artifact as the source of truth.
