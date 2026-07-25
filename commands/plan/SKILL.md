---
name: plan
description: "Create or expand a structured implementation plan with phases, tasks, subtasks, and verification criteria. Re-running on an existing plan deepens it — adds tasks, fills gaps, refines subtasks. Do NOT enter plan mode — this skill produces plan artifacts directly. Triggers: /plan, create a plan, plan this, implementation plan, expand plan, add detail, break down, breakdown, expand phase"
---

# /plan — Create or Expand an Implementation Plan

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) and target repository per `shared/path-resolution.md` in the plugin directory. Also read `shared/completion-evidence.md` — plans carry evidence sections from birth.

## When to Use
- When you need to break down a feature, project, or initiative into an actionable plan with phases, tasks, subtasks, and verification criteria.
- When you want to deepen an existing plan — add tasks, fill in missing verification, expand subtask checklists, or refine acceptance criteria as you learn more.

Both cases run through the same process below. The skill detects whether the named plan already exists and switches into **Revise mode** automatically. High-risk plans can optionally be **rehearsed** — dry-run in a scratch worktree to shake out plan bugs — before approval (step 6).

## Process

### 1. Determine Mode (Create vs Revise)

- If the user hasn't already specified it, ask what they want to plan (feature name, scope, goals).
- If `Plans/<PlanName>/README.md` already exists, switch to **Revise mode** — load the README and only the phase docs in scope for this revision into context (delegate a full-plan sweep to the researcher step instead of reading every phase doc yourself) and proceed to step 2.
- Otherwise, you're in **Create mode** — proceed to step 2 with no existing plan to load.

If the existing plan's `status` is `complete` or `archived`, confirm with the user before revising it — those plans are usually frozen.

### 2. Gather Context (delegated to `sdd-planner:researcher`)

Invoke the `sdd-planner:researcher` agent and ask it to return a **structured** summary, not freeform notes:

- **Relevant requirements** — spec items under `Specs/` that this plan should cover
- **Architectural constraints** — design decisions in `Designs/`, component boundaries, interfaces, contracts that constrain implementation
- **Background** — research, brainstorms, and prior debriefs bearing on this work
- **Related plans** — other plans in `Plans/` that touch the same area (filter by `status` — usually `active`, `approved`, and plans completed within roughly the last three months; tell the researcher explicitly to include the latter, since it skips `complete` plans by default)
- **Existing code** — implementations already present in the target repo that this plan would extend, modify, or replace
- **Current coverage and gaps** — in Revise mode, what the existing tasks and subtasks already address, and what's missing, vague, or contradicted by the latest specs/designs. In Create mode, this comes through as "which spec requirements have no plan covering them yet."

Use this structured summary as the input to step 3 — every drafting decision should trace back to something the researcher surfaced.

### 3. Draft Plan Structure

**Create mode:**
- Determine the plan name (PascalCase, no spaces).
- Break work into phases with clear deliverables — typically 3-7 for a substantial feature. A small feature may legitimately be a single phase; never pad with filler phases.
- Each phase gets 2-6 tasks.
- Identify dependencies between phases.

**Revise mode:**
- Review the existing phase list against the researcher's gap analysis.
- Identify: new tasks to add, existing tasks that need refinement (vague verification, missing subtasks, outdated notes), missing phases, and **tasks that should no longer exist** — work the researcher found already implemented in the target repo, superseded by a later decision, made redundant by another task or plan, or whose motivating requirement was cut. Revision is not append-only: a plan that only ever grows drifts away from the work that is actually left.
- **Retiring a task.** A non-complete task judged unnecessary is retired, not silently deleted: remove it from the `tasks[]` array and strike its body heading with a one-line reason (`~~1.3: Old Title~~ — removed: already implemented by <path>` / `superseded by D-NNNN`). Its id stays retired and is never reused, so existing cross-references cannot silently re-bind (`shared/frontmatter-schema.md` § Stable Identifiers). Completed tasks are never retired — the preservation rule below wins.
- **Preserve completed work.** Never delete or rewrite tasks that are already `complete` or referenced in a phase debrief under `notes/`. Refinements to completed tasks should be additive (new acceptance criteria, follow-up tasks) or noted as future work.
- Preserve existing task IDs. Keep completed task ordering immutable, but reorder non-complete work through its dependency order and phase placement when that yields smaller complete units. Split a non-complete task only into cohesive, independently verifiable task boundaries, assigning each new task the next available ID in its phase; never renumber existing IDs.

**Both modes:**
- **Plan tasks are native-SCM revision boundaries.** Split and reorder work into the smallest atomic units needed to establish clear dependency order, but only where each resulting task remains a cohesive, complete, independently bisectable feature or internal capability. At every task boundary the repository must build, relevant tests must pass, and the native SCM revision/checkpoint must deliver an observable behavior or a complete internal capability that later tasks can safely depend on. Do not use a task for an arbitrary layer, file batch, or half-wired scaffold that requires a later task to restore correctness. Conversely, do not combine independently complete feature slices merely because they share a phase.
- **Every task is sourced.** A task exists because something demands it — a spec requirement (`FR-NN`/`NFR-NN`), an acceptance criterion (`AC-NN`), an accepted decision (`D-NNNN`), or a concrete failure it prevents. Record that demand in the task's `justifies` field (`shared/frontmatter-schema.md` § Task-Level Fields). If the only available justification is "might need it later", "for completeness", or symmetry with a neighboring task, the task does not go in the plan. This is the counterweight to the phase/task counts above: `2-6 tasks` is a shape, not a quota — a phase with three sourced tasks is finished, and `never pad with filler phases` binds at task level too.

  Two limits keep this from cutting the wrong things. **Necessity is about sourcing, not size** — the test is "what demands this?", never "does this feel like a lot of work"; a large sourced task stays. And **correctness is never speculative**: error handling, edge cases, tests, rollback, and observability are load-bearing even when no requirement names them, and `justifies` for that work names the failure it prevents (e.g., "prevents silent data loss on partial write"). This rule targets speculative capability — the interface with one implementation, the config surface nothing varies, the extension point no requirement asks for.
- **Subtasks stay inside the boundary.** Subtasks describe mechanical steps needed to complete the task; they are not revision points and must not encourage incomplete intermediate states. If a proposed task cannot be recorded as a complete, verified native SCM revision/checkpoint, split or reorder it before approval.
- **Every task must have a `verification` field** — a specific answer to "how do we know this work is good and complete?" that names specific behaviors to cover (e.g., "parser handles valid, malformed, and empty input", "endpoint returns 200 with valid payload and 400 with missing fields"). Vague criteria like "works correctly" or test counts are not acceptable — verification means each new or changed behavior has a corresponding check. Wherever the check is commandable, `verification` also names the exact command to run and the expected observable output (e.g., `cargo test auth::` — 14 tests pass, including the new refresh-expiry case), not just prose criteria. Prose-only criteria are acceptable only when no command can observe the behavior. "Works correctly" is never acceptable. In Revise mode, audit existing tasks and add `verification` to any that lack it. Where a task satisfies a spec acceptance criterion or requirement, its `verification` (or body section) cites the `AC-NN`/`FR-NN` id (`shared/frontmatter-schema.md` § Stable Identifiers) — phase-level Acceptance Criteria likewise cite the spec ids they roll up.
- **Prospective criteria are not retrospective evidence.** Every task section, phase document, and plan README carries the completion-evidence section from `shared/completion-evidence.md`, initialized to exactly `Pending — not complete.` — `/implement` replaces it with what actually ran before any status flips to `complete`.
- **Include structural verification:** Read `shared/language-verification.md` and detect the target project language. Include the language-appropriate structural checks (sanitizers, static analysis, type checking) in verification fields where relevant — either per-task or as a dedicated verification task in each phase.
- **Gated scope.** A task whose correctness depends on an unanswered question only an external party can answer (the user, a stakeholder, a vendor, another team) must NOT be created provisionally. A "⚠️ pending confirmation" note is not a gate — a model will implement past it. Instead: either cut the work from scope, or create the phase with `status: blocked` naming the open question in the phase doc, and record the question in the plan README's Open Questions. A plan cannot move to `approved` while any in-scope task is gated on an unanswered external question.
- Present the structure (phases, tasks, refinements) to the user for feedback before writing files.

### 4. Write Plan Files

**Create mode:**
- Create `Plans/<PlanName>/README.md` using `shared/templates/plan-readme.md` with `status: draft`.
- Create numbered phase docs using `shared/templates/plan-phase.md`.
- Create `Plans/<PlanName>/notes/` directory for future debriefs. Drop a `.gitkeep` (or VCS-equivalent placeholder) inside so the empty directory survives cloning.
- Populate frontmatter with all phase/task metadata.

**Revise mode:**
- Update the existing README and phase doc frontmatter (`updated` date, new phase/task entries, refined `verification`).
- Add missing completion-evidence sections to every non-complete task, phase, and plan in scope. For already-complete artifacts with missing or pending evidence, report a legacy evidence gap; never fabricate evidence or silently downgrade the status.
- Create new phase files only when new phases are introduced.
- Leave existing `notes/` debriefs untouched.

**Both modes — body content depth is mandatory:**

For each task, write a `## <ID>: Task Title` section that includes:
- **`### Subtasks`** — a checklist (`- [ ]`) of the concrete implementation steps the implementer will work through. Not "implement X" — the actual steps a person would tick off (e.g., "add migration", "wire the handler", "cover the empty-input case in tests").
- **`### Notes`** — implementation guidance, edge cases, references to specific design sections, gotchas the researcher surfaced. If a task can't be broken into subtasks because it depends on research the implementer will do, say that explicitly here — don't leave the section blank. State the task's **revision boundary**: the complete behavior or capability that will exist, the verification that keeps the revision green, and any files or later concerns explicitly outside that revision. (For git targets this boundary is a focused implementation commit; don't imply every SCM has git commits.)
- **`### Completion Evidence`** — required, initially exactly `Pending — not complete.`; `/implement` replaces it with what actually ran before changing task status to `complete`.
- **`### Trap` (optional)** — for any task with a known tempting-but-wrong shortcut, name the shortcut and why it's wrong (e.g., "You will want to mock the clock here — don't; the race being tested lives in the real timer path."). Traps are written for a hasty model reading the task in isolation. Don't invent traps for tasks that have none.

Plus:
- Phase-level **Acceptance Criteria** as a checklist.
- Phase-level **Phase Completion Evidence**, initially pending.
- Plan README sections: **Overview**, **Architecture** (with Mermaid diagrams where structure helps — prefer `graph TD` / `flowchart LR` over ASCII art), **Key Decisions**, **Dependencies**, **Plan Completion Evidence** (initially pending), and **Open Questions** (only when gated scope produced any — see step 3; a retained question on an approved plan must use the exact form `- <question> — **non-blocking** — <why the plan holds regardless>`).

Shallow tasks with no subtasks or notes are not acceptable output — they're the failure mode this skill exists to prevent.

### 5. Review

- Run the deterministic validator first: `python3 <plugin-dir>/scripts/sdd_validate.py --scope Plans/<PlanName> --format json` (see `/validate`). Fix structural diagnostics before dispatching the reviewer — don't spend a review cycle on findings a script catches.
- Invoke the `sdd-planner:plan-reviewer` agent to review the complete plan.
- Address any issues raised by the reviewer.
- Treat a task that cannot land as a clean, complete, bisectable native SCM revision/checkpoint as a plan-structure defect, not an implementation detail — split or reorder it into smaller complete dependency-ordered units before approval.

### 6. Rehearse (optional dry run)

- Offer rehearsal when the plan is high-risk: multiple phases touching unfamiliar code, external API integration, data migrations, or the user asks for it. Otherwise skip this step silently.
- **Mechanics:** create a scratch git worktree of the target repo (or a full copy for non-git targets) — never rehearse against the live tree. Dispatch `sdd-planner:code-implementer` agents into the scratch tree to execute the plan's tasks literally, wave by wave, with the same evidence rules as `/implement` (verification output pasted, STOP on plan-vs-reality mismatch).
- **The product is plan bugs, not code:** wrong file paths, impossible task order, missing prerequisites, underspecified tasks, verification commands that don't run as written, traps that were missed. Collect them, discard the scratch tree and all code, and fix the plan before it moves to `approved`.
- **Cost:** rehearsal roughly doubles the implementation spend for the rehearsed scope — that's why it's opt-in and aimed at high-risk plans.

### 7. Approve

- Before setting `status: approved`, confirm no in-scope work is gated on an unanswered external question — blocking questions must be resolved or the affected phase marked `blocked`.
- Once review passes (and any rehearsal findings are fixed):
  - **Create mode:** update the plan README frontmatter `status` to `approved`.
  - **Revise mode:** if `status` is `draft`, set it to `approved` once the review passes (same as Create mode — a re-run on a never-approved plan must not strand it in `draft`); otherwise leave `status` as-is.
- Then re-read the frontmatter and confirm it parses as YAML and includes `title`, `type`, `status`, `created`, `updated`, `tags`, `related`.
- **Record decisions**: after approval, record each user-resolved open question and each Key Decision the user actually made (not ones merely drafted for them) in the decision ledger per `shared/decision-log.md` — collision check before each append; a collision stops for the user. Scope entries to `Plans/<PlanName>` (or to the governing spec/design when the decision really lives there), and **cite each new entry's id inline** in the plan's Key Decisions section (e.g., "(D-0012)") — the bidirectional link is what makes supersession detection work.

## Output
```
Plans/<PlanName>/
├── README.md              # Plan overview with phases in frontmatter
├── 01-Phase-Name.md       # Phase 1 with tasks in frontmatter, subtasks + notes in body
├── 02-Phase-Name.md       # Phase 2
├── ...
└── notes/                 # Empty (Create) or pre-existing debriefs (Revise)
```

Plan lifecycle (`draft` → `approved` → `active` → `complete`) is tracked in the README frontmatter `status` field. The plan directory stays put.

## Document Structure

### README.md
See `shared/frontmatter-schema.md` for the plan frontmatter schema. Body contains:
- **Overview**: What the plan delivers and why
- **Non-Goals**: What the plan deliberately does not deliver — the related specs' Non-Goals carried forward, plus boundaries decided during planning. A task that implements something listed here is a scope violation
- **Architecture**: High-level technical approach, with Mermaid diagrams
- **Key Decisions**: Major choices and rationale
- **Dependencies**: External prerequisites
- **Open Questions**: Unanswered external questions gating `blocked` phases (omit when there are none)
- **Plan Completion Evidence**: Pending until every phase is complete; then the exact aggregate evidence required by `shared/completion-evidence.md`

### Phase Docs
See `shared/frontmatter-schema.md` for the phase frontmatter schema. Body contains:
- **Overview**: What the phase delivers
- **Task sections**: Each headed by task ID (e.g., `## 1.1: Task Title`) with:
  - `### Subtasks` — checklist of concrete implementation steps
  - `### Notes` — implementation guidance, edge cases, design references, and the complete native-SCM revision boundary
  - `### Completion Evidence` — pending until populated with exact retrospective evidence
  - `### Trap` — optional; the known tempting-but-wrong shortcut for this task and why it's wrong (omit when a task has none)
- **Acceptance Criteria**: Phase-level completion criteria as a checklist
- **Phase Completion Evidence**: Pending until every task and phase criterion is complete

## Context
- Orchestration: `shared/orchestration.md`
- Templates: `shared/templates/plan-readme.md`, `shared/templates/plan-phase.md`
- Schema: `shared/frontmatter-schema.md`
- Existing plans: `Plans/` (status in each plan's `README.md` frontmatter)
- Related specs: `Specs/`
- Related designs: `Designs/`
- Agents: `sdd-planner:researcher`, `sdd-planner:plan-reviewer`, `sdd-planner:code-implementer` (rehearsal only)
