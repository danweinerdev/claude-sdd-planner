---
name: implement
description: "Execute a plan phase — implement tasks, track progress, update statuses. Triggers: /implement, implement this, start phase, execute plan, build this"
---

# /implement — Execute Plan Phase

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) and target repository per `shared/path-resolution.md` in the plugin directory. Also read `shared/completion-evidence.md` — no task, phase, or plan flips to `complete` without conforming retrospective evidence.

## When to Use
When a plan is approved and you're ready to implement a phase. This skill **coordinates** implementation: it delegates actual code work to `code-implementer` agents, runs them in parallel where dependencies allow, triggers `quality-scanner` agents after each task for a fast intent-blind quality check, and manages the review-fix cycle. It bridges the gap between `/plan` (which defines *what* to build) and `/debrief` (which captures *what happened*).

Per-task reviews during `/implement` dispatch `quality-scanner` directly (not the full four-lane review) because the relevant question after a single task is "is this code any good?" not "does the whole phase still align with the plan?". The full four-lane orchestrated review happens at end-of-phase via `/code-review`, which dispatches `drift-detector`, `quality-scanner`, `spec-compliance`, and `blind-spot-finder` in parallel and synthesizes their reports.

## Process

### 1. Select Phase
- Scan `Plans/` for plans whose README frontmatter `status` is `approved` or `active` (skip `draft`, `complete`, and `archived`)
- Ask which plan and phase to implement (or infer from context)
- Read the plan README to understand overall context and phase dependencies
- Read the target phase document to get the task list
- Verify prerequisites:
  - Plan status must be `approved` or `active`
  - Phase status must be `planned` or `in-progress` (not `complete`, `blocked`, or `deferred`)
  - Any phases in `depends_on` must be `complete`
- If plan status is `approved`, update the README frontmatter `status` to `active`
- If phase status is `planned`, update it to `in-progress`

### 2. Locate Target Codebase
- Resolve the target repository via the chain in `shared/path-resolution.md` (`planMapping[<PlanName>]` → repo key → `planning-config.local.json` `repositories.<key>.path`). If any link is missing or the path doesn't exist, stop and ask the user for the target directory — never guess, never clone.
- Detect the target repo's VCS per `shared/vcs-detection.md` (`git`, `git-worktree`, `perforce`, or `none`). You will pass this label to every implementer and scanner dispatch — they must not re-detect.

### 3. Load Context
- Extract spec and design **paths** from the plan README's `related` frontmatter — do not read their bodies in the primary context; the implementer agents read what they need (they have all tools). Pass the paths in each dispatch.
- Skim any previous phase debriefs in `Plans/<PlanName>/notes/` for constraints and gotchas that affect task dispatch — these are short and orchestration-relevant, so a primary-context read is appropriate.
- Read the decision ledger's frontmatter, if one exists (resolve per `shared/decision-log.md` § Ledger location — for external planning roots this is the target repo's `DECISIONS.md`), and note `accepted` entries scoped to this plan or its related specs/designs — pass the relevant statements to implementer dispatches as constraints. Never pass ledger content to `quality-scanner` — it is intent context and the scanner is intent-blind.
- What you need in the primary context is just enough to scope and dispatch: the phase's deliverable, task list, dependencies, and any prior-phase warnings.

### 4. Verify Task Readiness

Before executing any tasks, audit every task in the phase for a `verification` field in its frontmatter entry. Every task must answer the question: **"How do we know this work is good and complete?"**

Also read `shared/language-verification.md` and detect the target project language. Verify that the phase includes the language-appropriate structural checks (sanitizers, static analysis, type checking) — either in individual task verification fields or as a dedicated verification task. If missing, flag this alongside any tasks missing verification criteria.

Scan the phase's `tasks[]` array and separate tasks into two lists:
- **Ready**: tasks that have a non-empty `verification` field with specific, observable criteria — ideally the exact command to run and its expected output (the standard `/plan` writes to)
- **Missing verification**: tasks where `verification` is absent, empty, or vague (e.g., "works correctly", "done", "it works"). Also flag (as advisory, not blocking) commandable work whose verification is prose-only — the implementer then has to invent the check, which is where hasty verification creeps in.

#### Forward-reference audit

For every task that DOES have verification, also scan its verification text for **forward references** — backtick-quoted identifiers (types, functions, concepts) that don't exist yet in the target codebase and aren't defined in earlier phases of this plan.

A forward reference looks like a verification clause that names a symbol introduced by a later phase. Example: a Phase 2 task whose verification says "tests cover: pre-existing **OnDemand** started service is rolled back" — when `OnDemand` is introduced in Phase 3. The task can't be fully verified at Phase 2 implementation time without a test-only seam or a deferred test.

The check:

1. **Extract candidate identifiers** from each verification field — backtick-quoted symbols (e.g., `OnDemand`, `start_service`, `DependencyState`), and capitalized type-like names if they're not already standard library types.
2. **For each candidate, check three places:**
   - The target codebase: use `Grep` for the literal symbol (rough — you're looking for "does this exist anywhere yet?"). If found, it's not a forward reference.
   - Earlier phase docs in this plan (`<NN>-*.md` files with phase number ≤ current). If a phase ≤ N defines it, it's not a forward reference.
   - This phase's own frontmatter or body. If this phase introduces it, it's not a forward reference.
3. **If a candidate is found ONLY in a later phase doc** (`<NN>-*.md` with phase number > current), or NOT found anywhere in the plan or codebase, treat it as a forward reference.

This is a heuristic, not a parser. Capitalized words like `Result`, `String`, `Vec`, `HashMap`, `Option` (and language-standard equivalents) are not forward references — skip them. When ambiguous, lean toward flagging — false positives the user can dismiss in 5 seconds; false negatives create implementation-time test seams.

**If all tasks are ready and no forward references are found** — proceed to step 5.

**If any tasks are missing verification OR have forward references** — present the combined list to the user:

```
## Verification Issues

### Missing verification (no defined criteria)

- **1.2: Add user authentication** — no `verification` field
- **1.4: Set up logging** — no `verification` field

Each task needs a specific answer to "how do we know this is done?"
Examples: "login returns a JWT and refresh flow works", "logs appear
in CloudWatch within 5s of a request"

### Forward references (verification names symbols defined in later phases)

- **2.3: Registry-wide rollback** — verification mentions `OnDemand`, defined in phase 3
- **2.3: Registry-wide rollback** — verification mentions `start_service`, defined in phase 3

Forward references force implementation-time compromises (test-only
seams, deferred tests). Either reword the verification to be self-
contained at this phase, or document the seam explicitly in the phase
doc with a marker like "(Phase N+1: <symbol> not yet introduced)".
```

Then ask the user to choose:
1. **Fix now** — pause and update verification fields (add criteria for missing; reword or annotate forward references) before continuing
2. **Proceed anyway** — acknowledge the issues and implement; for forward references, the implementer may need to insert test-only seams documented in the phase doc
3. **Abort** — stop implementation to fix the plan first

If the user chooses option 1, update each task's `verification` field, then proceed. If the user chooses option 2, proceed but include a warning in the wave summary for each affected task — and where forward references force a test-only seam, the implementer's report and the eventual debrief should call that seam out explicitly. If the user chooses option 3, stop.

### 5. Build Dependency Graph & Execute in Waves

Analyze the phase's task list and `depends_on` fields to identify **waves** — groups of tasks that can run concurrently:

```
Wave 1: Tasks with no dependencies (e.g., 1.1, 1.2, 1.3)
Wave 2: Tasks depending only on Wave 1 tasks (e.g., 1.4 depends on 1.1)
Wave 3: Tasks depending on Wave 2 tasks
...
```

#### Advisory Overlap Analysis

Before launching each wave, check whether two or more tasks in the same wave might touch the same files (based on their subtask descriptions and the target codebase structure). If overlap is likely, warn the user and offer to serialize those tasks instead of running them in parallel.

#### For Each Wave

**a. Launch implementer agents (parallel)**
- Update each wave task's status to `in-progress` in the phase frontmatter **before** launching — if the session dies mid-wave, resume logic must not see `planned` tasks that actually ran
- For each task in the wave, launch a `sdd-planner:code-implementer` agent via the Task tool (use the plugin-namespaced name — bare `code-implementer` will not resolve)
- Each agent receives: task ID, title, subtasks, **verification criteria** (the task's `verification` field), the task's `### Trap` note if the phase doc has one, plan name and phase name (for the commit message), spec/design paths from step 3, target codebase path, detected VCS label, any notes from prior task debriefs
- Launch all tasks in the wave as concurrent Task tool calls

**b. Collect results**
- As each agent completes, collect: files changed, test results, verification evidence (exact commands, working directories, exit statuses, observable results), the change reference (for git a **full 40-hex, clean, non-merge implementation commit** containing the complete task and its tests — nothing else; changelist number for perforce; "no VCS" plus file list otherwise), issues
- **Reject evidence-free success.** A success report must contain the verification command(s) actually run and their pasted output. If verification is asserted rather than shown ("tests should pass", "verified", a paraphrase of expected output), the task is **not done**: resume the agent to produce the evidence — this consumes its one retry.
- **The task boundary is a revision boundary.** If the implementer reports it could not land the task as one clean, complete, bisectable revision (half-wired scaffold, mixed feature slices), that is a plan-structure defect — surface it per Escalation Rule 3 rather than accepting a mixed or dirty change reference.
- If an agent reports failure/blockers → resume it **once** with clarified guidance; if it fails again, mark the task `blocked`, record the reason, and escalate per Escalation Rule 1
- If an agent reports a **plan-vs-reality mismatch** (the plan names files, APIs, or prerequisites that don't match the codebase) → do not re-dispatch with a workaround; surface the mismatch to the user per Escalation Rule 2/3 — it's a planning bug, and patching around it in dispatch hides it
- If an agent reports success with evidence → proceed to review

**c. Review completed tasks**
- For each successfully completed task, dispatch `sdd-planner:quality-scanner` via the Task tool (use the plugin-namespaced name — bare `quality-scanner` will not resolve)
- Render the dispatch prompt from `shared/templates/quality-scan-prompt.md`. The template handles the boilerplate framing (intent-blind framing, scope, output table); your job per-dispatch is to fill in `FOCUS_LIST` — a 4–8 item curated list of risk areas in this specific diff. That is where the orchestrator's judgment lives.
- Scope the review to that task's changes — pass the target repo path, the file list, the VCS label from step 2, and the change reference from the implementer's report (the implementer makes exactly one commit/changelist per task) via the template's placeholders. For `none`-VCS targets, scope the scan by the implementer's file list instead of a change reference.
- The scanner evaluates the code intent-blind: correctness, safety, maintainability, testing, over-engineering — including **comment quality** (flags WHAT-restating comments, PR-time context references, tombstones for deleted code, and any comment that doesn't earn its place by capturing non-obvious WHY)
- Do **not** pass plan/spec/design context — `quality-scanner` is deliberately intent-blind, and the full orchestrated `/code-review` at end-of-phase covers the plan/spec/design perspective

**d. Process review findings**
- **Critical findings** → resume the `sdd-planner:code-implementer` agent to address the issue, then re-review
- **Non-critical findings** (Major/Minor/Question) → collect and present to user after the wave completes
- Maximum 2 review-fix cycles per task. If critical issues remain after 2 cycles, mark the task `blocked` with the reason ("critical findings unresolved after 2 review-fix cycles"), finish the wave's other tasks, and escalate at end of wave per Escalation Rule 5.

Never use "pre-existing" to justify deferring or hiding a finding. "Pre-existing" describes origin, not impact. Present findings by what they do to the user, not when they were introduced. The user decides what is worth fixing.

Never downscope a finding, recommendation, or fix by estimating how long it would take a human. Agents are not constrained by human development timelines. The right fix is right; surface it. Prefer a smaller change only when it is genuinely better on its own merits — clearer, lower risk, smaller surface area — never because a larger one would "take too long." The user decides what is worth fixing; don't pre-decide for them on time grounds.

**e. Populate completion evidence, then finalize the wave**
- For each task that passed implementation and review, replace its `### Completion Evidence` pending marker per `shared/completion-evidence.md`: verification date, repository root, VCS, the exact native revision/checkpoint as the tested source identity, an immediate identity recheck, every exact command with working directory and exit status, and the observable results satisfying the task's prospective `verification`. Follow `shared/frontmatter-schema.md` § Sensitive Data: `Repository` in `~/...` form, working directories repo-relative, pasted output scrubbed of absolute paths and usernames. Record the focused review in strict syntax — for git, exactly `` `git show <full40>` `` (or `` `git diff <full40>..<full40>` `` for a range whose base is the task commit's direct first parent) followed by `; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary`, then `Reviewed candidate / final:` with the exact identity and `Review result: PASS/Aligned`. The per-task quality-scanner pass (step c) is that focused review; it must have ended with no unresolved critical findings before you may record PASS/Aligned.
- Re-read each task section: evidence present, no pending marker, at least one command or tool/inspection row, every required behavior covered, identity recheck matches. Only then set the task status to `complete` and check off its subtask checklists (`- [x]`). A task with absent, pending, vague, or failing evidence stays non-complete.
- Run `python3 <plugin-dir>/scripts/sdd_validate.py --scope Plans/<PlanName> --format json` and resolve any diagnostics on the tasks just completed.
- When the planning root is git-versioned, record the lifecycle update (statuses, checkboxes, evidence) as a **separate scoped lifecycle commit** containing only plan/evidence bookkeeping — never mixed into an implementation commit. The evidence names the tested implementation revision, which avoids a self-referential lifecycle SHA. For an unversioned planning root, the artifact update stands but note that durable lifecycle transport is unavailable.
- Update the `updated` date in the phase frontmatter
- Present non-critical findings summary to user using the `shared/templates/per-task-findings.md` template — render once per task with the scanner's table and your recommendation. Keeping the structure consistent across tasks lets the user compare findings at a glance.
- Ask user for decisions on any findings requiring human judgment
- Proceed to next wave

### 6. Phase Completion
Once all tasks are complete (or all remaining tasks are blocked):

**All tasks complete:**
- **Run the phase gate before flipping status.** Phase completion requires, per `shared/completion-evidence.md` and `shared/review-artifacts.md` § Phase-completion review gate:
  1. Every task `complete` with conforming evidence and every phase acceptance criterion checked
  2. A clean target worktree and a frozen native-SCM phase revision/range
  3. A persisted, resolved, frozen **Aligned** four-lane `/code-review` of that range (dispatch `/code-review` in phase-gate mode — it records `lane_results`, `review_mode`, and `reviewed_planning_revision`)
  4. `## Phase Completion Evidence` populated: common verification fields, `### Completed task identities` (one `- <id>: <revision>` line per task), and `- Final aligned review: <artifact path>; frozen: <exact rev>`
  5. `python3 <plugin-dir>/scripts/sdd_validate.py --scope Plans/<PlanName> --format json` passing for the phase
- Material review fixes get a **new planned task id**, land as complete task revisions, and force a fresh frozen four-lane review — never an unplanned patch on a gated phase
- Then update phase status to `complete` in both the phase doc and plan README, update `updated` dates, and commit the lifecycle record (separate scoped commit when the planning root is git-versioned)
- If all phases in the plan are now complete, populate `## Plan Completion Evidence` (common fields plus `### Completed phase identities` — `- <phase id>: <checkpoint>; review: <final review path>` per phase) and set the plan README frontmatter `status` to `complete`
- Suggest running `/debrief` to capture what happened

**Some tasks blocked:**
- Keep phase status as `in-progress`
- Present the blocked tasks and their blockers to the user
- Ask how to proceed:
  - Resolve blockers and continue
  - Defer blocked tasks and mark phase as `complete`
  - Mark phase as `blocked`

## Task Execution Details

### Working with Subtasks
Phase docs contain subtask checklists under each task heading:
```markdown
## 1.1: Task Title

### Subtasks
- [ ] Implement the data model
- [ ] Add validation logic
- [ ] Write unit tests
```

As agents complete each subtask, the coordinator checks them off:
```markdown
- [x] Implement the data model
```

This gives real-time progress visibility in the plan artifacts.

### Handling Dependencies
Tasks may have `depends_on` in their frontmatter. The coordinator builds waves from these:
1. Wave 1: Tasks with no dependencies
2. Wave 2: Tasks whose dependencies are all in Wave 1 (and will be complete)
3. Wave 3: Tasks whose dependencies are all in Waves 1-2
4. Skip tasks whose dependencies are `blocked` or `deferred`

### Resuming Interrupted Work
If a phase is already `in-progress` (from a previous session):
- Read the current state of all tasks
- Skip `complete` tasks
- Resume `in-progress` tasks (re-launch their agents)
- Continue with `planned` tasks in dependency order

## Escalation Rules

These conditions require stopping and asking the user:

1. **Blocked task**: An agent can't complete a task after 2 attempts (initial dispatch + one resume with clarified guidance). Present the issue and ask for guidance.
2. **Spec ambiguity**: The spec or design doesn't cover a case encountered during implementation. Ask the user to clarify rather than guessing.
3. **Scope expansion**: Implementation reveals work not captured in the plan. Flag it — don't silently expand scope.
4. **Destructive action**: Any action that would delete data, modify production config, or affect shared systems needs explicit approval.
5. **Unresolvable review findings**: `quality-scanner` flags critical issues that the implementer can't resolve after 2 review-fix cycles. Mark the task `blocked`, finish the wave's other tasks, then present the unresolved findings to the user at end of wave — do not start the next wave without the user's decision.
6. **File conflicts**: If parallel tasks in a wave produce conflicting changes to the same files, present the conflict to the user before proceeding.

Everything else is autonomous. Don't ask for confirmation between waves.

**Record escalation resolutions.** When the user answers an escalation (rules 1–5) with a choice that constrains future work — an ambiguity resolved, scope accepted or cut, an approach picked for a blocked task — record it in the decision ledger per `shared/decision-log.md` (apply its admission test — the binding has to reach past the task that raised the escalation; collision check before appending; a collision is itself a stop). If the fresh answer collides with an accepted entry, use the ledger's **one-step supersession**: "this supersedes D-NNNN — confirm?" — don't make the user relitigate what they just decided. Scope the entry to the plan. Pure one-off dispositions ("retry it", "skip for now") are events, not decisions — don't log them.

## Output
Updates existing plan artifacts in place:
- Phase doc: task statuses, subtask checklists, `updated` date
- Plan README: phase status, `updated` date

Code changes go to the target repository (not the planning root).

## Context
- Orchestration: `shared/orchestration.md`
- Schema: `shared/frontmatter-schema.md`
- Per-task findings template: `shared/templates/per-task-findings.md`
- Quality-scanner dispatch template: `shared/templates/quality-scan-prompt.md`
- Target plan: `Plans/<PlanName>/` (status: `approved` or `active`)
- Related specs: `Specs/`
- Related designs: `Designs/`
- Prior debriefs: `Plans/<PlanName>/notes/`
- Local repo paths: `planning-config.local.json`
