---
name: code-review
description: "Review implementation code against the plan for drift, gaps, and blind spots. Triggers: /code-review, review code, check implementation, compare to plan, code vs plan"
---

# /code-review — Implementation Code Review

## ⚠️ CONTRACT — READ FIRST

This skill's entire value comes from dispatching four intent-isolated sub-agents in parallel and synthesizing their independent reports. You are the orchestrator.

**You MUST:**
1. Dispatch all four sub-agents via the Task tool, using their **plugin-namespaced** names: `sdd-planner:drift-detector`, `sdd-planner:quality-scanner`, `sdd-planner:spec-compliance`, `sdd-planner:blind-spot-finder`.
2. Dispatch them **in parallel** — a single message containing the four built-in Task tool calls, plus one additional Task call for each matching project review lane (see Step 2e and Step 3).
3. Give each sub-agent only the inputs for its lane. See Step 3 below for the exact input map. Passing extra context destroys the intent isolation that makes the review worthwhile.
4. Wait for all dispatched lanes (the four built-ins plus any project lanes) to return, then synthesize.

**You MUST NOT:**
1. Read the full diff, the phase doc contents, spec contents, or design contents in the primary context and write findings yourself. That is a single-pass review cosplaying as a four-lane review. It is the bug this contract exists to prevent.
2. Skip the Task dispatch because "you already know the answer" after loading context. The answer you'd produce is exactly the single-pass review this skill exists to replace.
3. Fall back to self-synthesis if a Task dispatch fails. If dispatch fails, **STOP** and return a loud error to the user describing which sub-agent failed and why. Do not silently continue.
4. Use bare sub-agent names (`drift-detector`, `quality-scanner`, etc.) for the **four built-in** lanes in Task calls — plugin agents require the `sdd-planner:` prefix or they will not resolve. (Project lanes are the exception — they resolve by bare name; see the external-lanes block below.)

**External review lanes (best-effort, additive).** Beyond the four, a project can plug in its own specialized lanes via the socket defined in `shared/review-lanes.md` (e.g. a read-only `.claude/agents/sql-reviewer.md` carrying `reviewLane: true`). These are **additive, read-only, and never abort the four**:
- The four built-ins are the floor and always run. A project lane only ever *adds* findings — never removes, replaces, or weakens a built-in lane's inputs or dispatch.
- A project lane that fails to dispatch, errors, or is malformed is **recorded and dropped** — never aborts the four. The strict "STOP on dispatch failure" rule in this contract applies to the **four built-ins only**. Synthesis proceeds over the four plus whatever project lanes returned. (Lane *responsiveness* is the project's responsibility — the socket dispatches and waits; it imposes no timeout, so a lane that hangs will hold up the review.)
- Failure is **not silent**: a declared lane that didn't run **degrades the verdict** (the Output Format's `Lane status` line — Alignment itself always reflects the returned lanes' findings), and an opt-in `required: true` lane that didn't run **forces Lane status to BLOCKED** (it gates the verdict, not the floor). The verdict is computed from the four plus any *successful* project lanes.
- Lanes execute repo-supplied instructions with session tool access, so `/code-review` **confirms discovered lanes** with the user before dispatch when the target repo isn't the session's own project.

If you find yourself reading a spec file or running `git diff` against the full patch in the primary context, stop. That work belongs in the sub-agents, not here. (Listing changed file *paths* with `--name-only` for `appliesTo` matching is allowed — that's paths, not content.)

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) and target repository per `shared/path-resolution.md` in the plugin directory.

## When to Use
During or after implementation of a plan phase, when you want to verify that the actual code matches what was planned. This skill dispatches four intent-isolated sub-reviewers against the diff and synthesizes their reports into a single review.

Use this:
- Mid-phase: to course-correct before implementation drifts further
- End-of-phase: before running `/debrief`, to have a clear picture of what happened vs. what was planned
- After resuming work: to re-orient on where things stand
- Before merging: to ensure the branch delivers what the plan promised
- **Phase-gate mode**: as the persisted final gate `/implement` requires before a phase flips to `complete` (see `shared/review-artifacts.md` § Phase-completion review gate). Phase-gate reviews carry extra obligations, flagged inline in Steps 2c and 7 below.

## Why four sub-agents

A single reviewer juggling plan, specs, designs, code, and adversarial perspective inevitably forgives code for one reason while missing flaws visible from another angle. Four intent-restricted reviewers preserve the perspective each lane needs:

| Agent | Sees | Doesn't see | Role |
|---|---|---|---|
| `sdd-planner:drift-detector` | Diff + plan + phase doc + prior debriefs | Specs, designs, code-quality heuristics | Missing work, scope creep, approach drift |
| `sdd-planner:quality-scanner` | Diff + code | Plan, specs, designs | Correctness, safety, maintainability (including comment quality — WHAT-restating, PR-time-context, and tombstone comments are flagged as noise), testing, over-engineering — intent-blind |
| `sdd-planner:spec-compliance` | Diff + specs + designs | Plan, phase doc | Requirements coverage, contract violations |
| `sdd-planner:blind-spot-finder` | Diff only | Everything else | Adversarial fresh eyes — scenarios the author didn't consider |

Each runs in its own fresh context. The whole point is that one reviewer's framing cannot contaminate another's.

Projects can add their own specialized lanes on top of these four via the socket in `shared/review-lanes.md` (drop a `.claude/agents/*-reviewer.md` with `reviewLane: true`). Project lanes are **additive** — they never replace or weaken the built-in four, and their failure never fails the review.

**`blind-spot-finder`'s diff-only guarantee is the sharpest and most fragile of these.** If you, as the primary orchestrator, read the plan before you dispatch it, your synthesis of its output will inevitably carry plan-aware framing back into the "blind-spot" conclusions. Keep your own reads shallow — file paths only, not contents — until after the sub-agents return.

## Process

### 1. Identify the Review Target

Your goal in this step is to produce four strings: plan path, phase doc path, target repo path, and diff scope. Do NOT read the contents of the plan or phase doc — just their paths.

- **Plan and phase**: ask the user, or infer from context. If inferring, use `Glob` on `Plans/*/README.md` (filenames only — do **not** `Read` them) and filter to those whose frontmatter `status` is `active` (read only the frontmatter, not the body). If there is a single active plan, pick it; if there are multiple, ask. Capture:
  - Plan path (e.g., `Plans/<PlanName>/README.md`)
  - Phase doc path (e.g., `Plans/<PlanName>/<NN>-<Phase-Name>.md`)
- **Target repo path**: read `planning-config.json` for `planMapping` to find the repo key for this plan, then read `planning-config.local.json` for `repositories.<key>.path` to get the absolute local path. If that doesn't resolve, ask the user where the code lives.
- **Diff scope**: if the user specified a branch, commit range, or "working + staged", record that verbatim. Otherwise, record "determine from phase created date" — **you** will resolve it in Step 2c before dispatch; the sub-agents receive an already-resolved diff command.

### 2. Gather Only What Dispatch Needs

To dispatch the sub-agents with the right inputs, you need a little bit of information from the planning artifacts and the target repo. Keep this step as narrow as possible — read frontmatter, not bodies.

**a. Plan README frontmatter only.** Use `Read` on the plan README, but stop after the frontmatter. Extract the `related` field to get the list of spec paths (under `Specs/`) and design paths (under `Designs/`); `related` entries in directory form get `/README.md` appended so the lanes receive file paths. **Do not** read the body of the README — the sub-agents will do that.

**b. Prior debrief paths.** Use `Glob` on `Plans/<PlanName>/notes/*.md` to get the list of prior debrief paths. Do not read their contents — `drift-detector` will do that.

**c. Resolve the diff scope.** First, detect the target repo's VCS using `shared/vcs-detection.md`. Then orient using the VCS-appropriate "working-tree status" and "recent history" commands from that file (e.g., `git status` + `git log --oneline -20` for git, `p4 opened` + `p4 changes -m 20` for perforce). If the user gave an explicit range, use it. If the user said "determine from phase created date", resolve a base:
   - **git, work on a feature branch**: prefer `git merge-base <default-branch> HEAD` as the base — it is exact and timezone-proof.
   - **git, otherwise**: read only the frontmatter of the phase doc to get the `created` date, find the first commit on or after it (`git log --since=<created> --reverse --format=%H | head -1`), and use that commit's **parent** (`<sha>^`) as the base so the first commit's own changes are included. If it is the root commit, use the empty-tree hash.
   - **perforce**: use the changelist immediately **before** the first changelist on or after the date.

   If you still can't resolve (or the VCS is `none`, in which case there is no history), ask the user for a base. Capture the scope as a concrete diff command (`git diff <base>..<head>` for git, `p4 diff2 -dw //path/...@<base> //path/...@<head>` for perforce) plus any unsubmitted/working coverage the user requested. If working/staged changes are in scope, note in every dispatch that the tree is not frozen — and where the review matters, recommend the user commit first. **Do not** run the diff and read the patch content — the sub-agents will. Pass the detected VCS and the resolved diff command to each sub-agent so they don't have to re-detect.

   **Phase-gate mode:** uncommitted or moving work is not eligible. Require a clean target worktree and freeze an exact `<full40>..<full40>` range whose commits exist in the target repo, whose base is an ancestor of the endpoint, and whose endpoint equals the phase evidence's `Revision / checkpoint` commit. Record the exact range — it becomes the review's `rev` and the phase evidence's `frozen` value. Also capture the planning repo's current full commit (`git -C <planning-root> rev-parse HEAD`) for `reviewed_planning_revision`; if the planning root isn't git-versioned, the phase cannot complete (report the adapter diagnostic — the review may still run as advisory). Only the *code* endpoint must be committed before the review: spec/design amendments, decisions, and the review artifact itself are written in flow and ride in the phase-close commit (`shared/autonomy.md` § SCM boundary cadence, D-0024). The one exception is a `Not-Aligned` round that routes new tasks — those change the phase doc's intent and must be committed before the next round's `reviewed_planning_revision`.

**d. Language-verification note (optional).** If `shared/language-verification.md` exists and the project language warrants structural checks, pass a one-line note to `drift-detector` and `quality-scanner` so they can flag missing sanitizers/static-analysis/type-checking work. You do not need to read the full language-verification doc — just detect the project language from a quick file-extension glance at the target repo and include it as context.

**e. Discover project review lanes (optional socket).** Glob, **non-recursively**, the **target repo's** top-level `.claude/agents/*-reviewer.md` and the user's `~/.claude/agents/*-reviewer.md`. If the glob matches nothing, this step is a no-op — skip straight to Step 3. If it matches anything, read `shared/review-lanes.md` — it is the full convention; this is its operational summary. From each match read **only the socket fields** — `reviewLane`, `lane`, `appliesTo`, `required` — and **not** the `description` or body. (Reading intent-laden frontmatter pre-dispatch would violate the contents-blindness rule above; the `description` is not needed to dispatch.) Then classify every file that declares a `reviewLane` key at all (the filename is only a discovery hint — the marker is the opt-in, and it is what keeps a project's `plan-reviewer`/`spec-reviewer` *overrides*, which carry no `reviewLane`, from being mistaken for lanes):
   - `reviewLane` parses to boolean `true` → **candidate lane**, continue below.
   - `reviewLane` present with any other value → **Malformed** (report in Step 5; do not dispatch). Never silently drop it.
   - **De-duplicate by bare `name`**: the same `name` in both locations → the target repo's file wins, the user-level file is noted as *shadowed*; the same `name` twice in one location → both **Malformed** (ambiguous dispatch). A `name` equal to a built-in (`drift-detector`/`quality-scanner`/`spec-compliance`/`blind-spot-finder`) → **Malformed**.
   - `lane` of an invalid type (non-string) → **Malformed**.

   For each surviving candidate:
   - **Dispatch gate.** If it declares `appliesTo` (a list of minimatch/globstar globs, repo-root-relative, case-sensitive), get the changed file *paths* via the VCS-appropriate name-only listing run with **cwd = target repo** (`git -C <target> diff --name-only <base>..<head>`, or `p4` + `p4 where` for depth→relative paths — see `shared/review-lanes.md`). Paths only, never hunk content. Keep the lane only if some glob matches; otherwise mark it **Skipped** and remember the tested paths. `appliesTo: []` always Skips; absent `appliesTo` always dispatches and self-scopes.
   - **Input bundle.** Match `lane` exact-lowercase. Recognized values (`code`/`spec`/`plan`/`diff-only`) get the **same** curated bundle as the matching built-in. An absent or unrecognized `lane` gets base inputs only (target repo path, VCS label, diff command) and self-gathers the rest. Group reviewers sharing the same unrecognized `lane`. Record any `required: true`.
   - **Trust gate.** The target repo counts as the session's own project iff its resolved absolute path equals (or contains) the session working directory's repo root. If it is **not**, these are externally-supplied instructions executing with session tool access — **list the discovered lanes and ask the user to confirm** before dispatching. Even when it is the session project, name the discovered lanes to the user.

   If there are no project reviewers, this step is a no-op and the review runs the four built-ins exactly as before.

At the end of Step 2, you should have:
- Plan path, phase doc path
- Spec paths (list), design paths (list)
- Prior debrief paths (list)
- Target repo path
- Resolved diff scope as a concrete git command/range
- Optional language-verification note
- Project review lanes to dispatch (list) — each with its resolved input bundle, lane grouping, and `required` flag
- Project lanes Skipped or Malformed at discovery (with reason + the paths a Skip was tested against), to report in Step 5
- Changed file paths (name-only listing, target-repo-relative) — only if some project lane declared `appliesTo`

You should NOT have read any spec contents, design contents, diff hunks, the body of the phase doc, or any project reviewer's `description`/body. (Changed file *paths* from a `--name-only` listing are fine — they are not hunk content.)

### 3. Dispatch All Lanes in Parallel

**This is the step the contract at the top of this file is about.** Send a single message containing the four built-in Task tool calls, plus one Task call per matching project lane from Step 2e. The four built-ins use the plugin-namespaced name. Each lane receives only the inputs for its lane.

**Task call 1 — `sdd-planner:drift-detector`**
- Plan path, phase doc path
- Prior debrief paths
- Target repo path
- Detected VCS label (`git`, `git-worktree`, `perforce`, `none`)
- Resolved diff command for that VCS
- Language-verification note (if applicable)
- ❌ Do NOT pass spec paths, design paths, or any diff content.

**Task call 2 — `sdd-planner:quality-scanner`**
- Target repo path
- Detected VCS label
- Resolved diff command
- `mode: review`
- Language-verification note (if applicable)
- ❌ Do NOT pass plan path, phase path, spec paths, or design paths. Intent-blindness is the point.

**Task call 3 — `sdd-planner:spec-compliance`**
- Spec paths, design paths
- Target repo path
- Detected VCS label
- Resolved diff command
- ❌ Do NOT pass plan path or phase path.

**Task call 4 — `sdd-planner:blind-spot-finder`**
- Target repo path
- Detected VCS label
- Resolved diff command
- ❌ Do NOT pass anything else. Not the plan, not the specs, not the designs, not the phase doc, not even the language-verification note. The diff-only guarantee is this reviewer's entire contribution.

**Task calls 5..N — project review lanes (if any, from Step 2e).** In the *same* message, add one Task call per matching project lane. For each:
- `subagent_type` is the reviewer's **bare `name`** (project agents resolve by bare name — do **not** add the `sdd-planner:` prefix).
- Pass the input bundle its `lane` resolved to: a recognized `lane` (`code`/`spec`/`plan`/`diff-only`) gets the **same** inputs as the matching built-in above; an absent or unrecognized `lane` gets only the base inputs (target repo path, detected VCS label, resolved diff command) and is left to gather anything else itself.
- ❌ Do NOT hand a standalone or unrecognized lane the plan, specs, or designs — it gets base inputs only; if it wants intent, it reads it itself. ✅ Do hand a recognized-lane reviewer exactly the bundle that lane names, and nothing more.
- Hand every lane the **frozen** diff reference (fixed base/head — a commit SHA where the VCS allows) so a lane can't shift what the built-ins review. Project lanes are **read-only**; they must not write to the repo.

Wait for all dispatched lanes to return before continuing. The socket imposes no timeout on project lanes — keeping them fast is the project's responsibility (a hung lane will hold up the review).

**If a built-in Task call (one of the four above) fails or returns an error** (e.g., "unknown subagent_type"), stop immediately and return a loud error to the user:

```
ERROR: /code-review could not dispatch sub-agent `sdd-planner:<name>`.
Reason: <the exact error from Task>
The four-lane review cannot proceed. Fix the dispatch issue and re-run.
```

**Do NOT** fall back to self-synthesis. A single-pass review pretending to be a four-lane review is worse than no review at all — it gives the user false confidence in an un-triangulated report.

**Project-lane failures are different — never abort the four.** If a project lane (Task calls 5..N) fails, do **not** stop. Record its outcome and continue synthesizing the rest. Classify each per `shared/review-lanes.md`: **Skipped** (no `appliesTo` match — quiet, show the tested paths), **Failed to dispatch**, **Errored**, **Oversized** (truncate with a note), or **Malformed**. For **Failed to dispatch**, name the discovered file and its declared `name` and state the cause is *either* a name/file mismatch *or* this repo's agents not being registered in this session — **do not assert which** (you read the frontmatter, so you know the file exists, but not why `Task` couldn't resolve it). The four built-ins remain the floor; a broken project lane degrades coverage, it does not fail the four — but the degradation must surface in the verdict (Step 4h/Step 5), never only in a sub-section.

### 4. Synthesize the Reports

Once all dispatched lanes have returned, produce a single unified review over **all** their reports — the four built-ins plus any project lanes that returned. Synthesis is the whole value-add of this step — it is not concatenation.

**a. Build a findings table.** Enumerate every finding from all returned reports (built-in and project lanes). For each, record: source agent, severity, location, one-line summary.

**b. Detect agreements.** Findings that multiple reviewers hit independently are high-confidence. Flag them as **Confirmed by N reviewers**. When `drift-detector` says a task is missing and `spec-compliance` says the requirement is uncovered, that's the same hole seen from two angles — strong signal.

**c. Detect disagreements.** When reviewers contradict each other, the disagreement is itself a finding:
- `drift-detector` says the task was completed; `spec-compliance` says the requirement is uncovered. → Plan and spec are out of sync. Surface this.
- `quality-scanner` says the code is fine; `blind-spot-finder` flags a concurrency scenario. → The code is locally correct but globally fragile. Surface both perspectives.
- `drift-detector` says the code is unplanned scope creep; `spec-compliance` says it satisfies a spec requirement. → The plan missed a requirement. Surface as a planning gap.

Disagreements get their own section in the output. Never quietly reconcile them by picking a winner.

**d. Highlight unique blind-spot findings.** Any `blind-spot-finder` finding that **no other reviewer caught** gets promoted into a dedicated "Blind Spots Only `blind-spot-finder` Caught" section. This is explicit recognition of the value the adversarial reviewer adds. If `blind-spot-finder` had zero unique findings, say so — it's a signal the other reviewers were thorough, not a reason to omit the section.

**e. Cross-validate questionable findings.** If a reviewer flagged something as a **Question** (unverified suspicion), check whether any other reviewer's findings corroborate or rule it out. Promote corroborated questions to findings; drop the ones that other reviewers ruled out.

**f. Deduplicate.** When multiple reviewers report the same issue from the same angle, collapse them into one entry and list the sources. Don't double-count in severity tallies.

**g. Fold in project lanes.** Treat each project lane's findings like any built-in lane's for agreement, disagreement, and dedup, with three rules:
- **Within-lane grouping.** Reviewers sharing the same unrecognized `lane` are peers in one lane — present them under one heading, and for the "Confirmed by N reviewers" tally **count the lane group as a single source** (two `security` peers agreeing is one corroborated Security finding, not "confirmed by 2").
- **Identity by dispatch, not by string.** Key the "Blind Spots Only `blind-spot-finder` Caught" section and the per-lane sections on the **dispatched identity** (the namespaced built-in, or the project lane's resolved name), never a bare matched string. A project lane cannot be mistaken for a built-in — a name collision was already rejected as Malformed in Step 2e.
- **Recognized lanes stay in their bundle.** If a recognized-lane reviewer (`code`/`spec`/`plan`/`diff-only`) asserts a finding about an artifact it was not granted (e.g. a `spec` lane claiming "the plan requires X"), demote that claim to a **Question** — it spoke outside its inputs.

**h. Compute the verdict, then degrade it visibly.** The Overall Verdict and counts come from the four built-ins plus any *successfully returned* project lanes — a lane that failed to run is a coverage gap, not a finding, and never drags the verdict down on its own.

Compute **Alignment** by worst-of mapping across the returned lanes' own verdicts: any lane at its worst tier (drift `Weak`, quality `Concerning`, compliance `Weak`, hidden-risk `High`, or any Critical finding) → **Weak**; otherwise any lane at its middle tier (`Moderate` / `Acceptable` / `Partial` / `Elevated`) → **Moderate**; otherwise **Strong**.

Lane failures never rewrite Alignment — they are carried on the separate **Lane status** line of the Output Format:
- If any **declared** project lane did not run (Failed to dispatch / Errored / Malformed — **not** Skipped, and **not** Oversized: a truncated lane still ran; only if truncation leaves no parseable findings does it reclassify as Errored), set Lane status to `DEGRADED — N declared lane(s) did not run: <names>`.
- If a **`required`** lane did not run, set Lane status to `BLOCKED — required lane <name> did not run`, overriding DEGRADED. The four still ran and their findings still stand; BLOCKED reflects that a gate the project declared mandatory was not satisfied.
- A purely **Skipped** lane (no matching changed paths) degrades nothing — it had nothing to review. Otherwise Lane status is `OK`.

**Do NOT introduce new findings of your own during synthesis.** Your job is to synthesize what the four sub-agents returned, not to add findings based on your own reading. If you notice something none of the four agents caught, it means one of the agents needs to be improved — note it in an "Orchestrator Observations" addendum rather than silently inserting it as a finding.

### 5. Present the Unified Review to the User

Render the synthesis in the output format below. Include the raw sub-reports verbatim in `<details>` blocks so the user can drill in (for an Oversized lane, the block contains the truncated text plus the truncation note). Do not re-summarize the sub-reports — the synthesis is the summary, the raw reports are the evidence.

### 6. Offer Next Steps
Based on findings, suggest appropriate actions:

**If alignment is strong:**
- Proceed to `/debrief` to capture the phase outcome
- Note any minor items for the debrief

**If drift is detected:**
- Fix the code to match the plan, OR
- Update the plan to reflect intentional changes (scope was wrong)
- Document the deviation rationale

**If planning blind spots are found:**
- Update specs/designs to account for discovered complexity
- Add tasks to the current or a future phase
- Create a research document for unknowns that need investigation

**If open questions remain:**
- Present the Open Questions section for the user to verify
- Flag any that could cause issues if wrong

Never use "pre-existing" to justify deferring or hiding a finding. "Pre-existing" describes origin, not impact. Present findings by what they do to the user, not when they were introduced. The user decides what is worth fixing.

Never downscope a finding, recommendation, or fix by estimating how long it would take a human. Agents are not constrained by human development timelines. The right fix is right; surface it. Prefer a smaller change only when it is genuinely better on its own merits — clearer, lower risk, smaller surface area — never because a larger one would "take too long." The user decides what is worth fixing; don't pre-decide for them on time grounds.

### 7. Persist the Review
Write the unified review to a review artifact per `shared/review-artifacts.md`: `Plans/<PlanName>/reviews/<NN>-<plan-slug>-code-review-<rev>.md` from `shared/templates/review.md`, with `rev` = the reviewed repo's short revision (`-dirty` when the tree wasn't frozen). Number the consolidated findings `F-NN`, mirror them in `findings[]`, and set `status: open`. The raw sub-reports stay in the conversation; the review artifact carries the synthesized findings — the durable record.

**Phase-gate mode:** `rev` is the frozen `<full40>..<full40>` range from Step 2c, and the artifact is driven through the binary's review transition chain: `sdd review scaffold <phase-path> --frozen <base>..<endpoint>` creates it open and unfrozen with `review_scope: phase`, `review_mode` (`independent` when all four lanes ran as fresh-context agents), `reviewed_planning_revision`, and exactly four `lane_results` under the **stable lane identifiers** — `review_plan_drift` (drift-detector), `review_quality` (quality-scanner), `review_spec_compliance` (spec-compliance), `review_blind_spots` (blind-spot-finder). Record each lane's observation with `sdd review evidence set <review-path> --lane <id>` — `result: PASS/Aligned`, `reviewed_identity` exactly equal to `rev`, and specific concrete evidence naming inspected paths, behaviors, or observations (never a generic `No findings`). Then close the gate with `sdd review resolve <review-path>`, which sets `frozen: true` and `status: resolved` atomically; never hand-edit those fields, and never edit a resolved review. Only an all-lanes-Aligned review may carry `verdict: Aligned`; anything else forbids phase completion, and material fixes become new planned tasks followed by a fresh scaffolded review of the new frozen range. When the planning root is git-versioned, commit the exact cited artifact at planning-root `HEAD` with the lifecycle record. Validate with `sdd validate --scope Plans/<PlanName> --format json` before handing the gate result back to `/implement`.

### 8. Acting on Findings
If the user asks to address findings, follow `shared/review-artifacts.md`: mechanical fixes (fully determined by hard facts — an accepted `D-NNNN`, approved artifact text, a verifiable fact) apply directly with the fact cited; design decisions stop for user discussion and land in the decision ledger; every disposition gets a Resolution Log entry; changed numbered elements trigger the reconciliation sweep; deferred findings become plan tasks or tracked `FU-NN` follow-ups.

## Output Format

```markdown
## Code Review: [Plan Name] — Phase [N]: [Phase Title]

### Overall Verdict
**Alignment:** Strong | Moderate | Weak
**Lane status:** OK | DEGRADED (N declared lane(s) did not run: …) | BLOCKED (required lane … did not run)
**Critical issues:** [count]
**Top items to address:** [prioritized list of 3–7]

### Diff Scope
- Commits reviewed: [range, count]
- Files changed: [count]
- Reviewers dispatched: sdd-planner:drift-detector, sdd-planner:quality-scanner, sdd-planner:spec-compliance, sdd-planner:blind-spot-finder[, plus any project lanes by name]

### Project Review Lanes
[Include this section only if the project supplied lanes via the socket. Omit it entirely if there were none — never show an empty section.]
- **Ran:** [lane name(s) that returned a report; mark any `required` ones]
- **Skipped (no matching changed paths):** [name — tested against: path, path, … | "none"]
- **Failed to dispatch:** [name (declared `name: X`) — name/file mismatch *or* not registered this session | "none"]
- **Errored:** [name + error | "none"]
- **Oversized (truncated):** [name | "none"]
- **Malformed:** [name + reason, e.g. "name collides with built-in" | "none"]

### Confirmed Findings (agreed by 2+ reviewers)
These findings were caught independently by multiple lanes — high confidence.

#### [Severity] — [one-line summary]
**Caught by:** drift-detector, spec-compliance
**Location:** `path/to/file.ext:line`
**Detail:** Synthesized description drawing from each reviewer's angle.
**Recommendation:** …

[Repeat]

### Disagreements (reviewers contradict each other)
When reviewers see the same code differently, the disagreement is itself a finding.

#### [one-line summary of the disagreement]
- **drift-detector says:** …
- **spec-compliance says:** …
- **What this means:** e.g., "The plan and the spec are out of sync — one of them needs to be updated to reflect reality."
- **Recommendation:** …

[Repeat]

### Blind Spots Only `blind-spot-finder` Caught
Findings from the adversarial reviewer that no other lane surfaced. Pay these special attention — they exist precisely because intent-aware reviewers forgave them.

#### [Severity] — [one-line summary]
**Location:** `path/to/file.ext:line`
**Scenario:** …
**Recommendation:** …

[Repeat. If there are none, say: "None — the other reviewers covered everything blind-spot-finder surfaced. Either the code is in very good shape or the adversarial reviewer should push harder next time."]

### Drift (from drift-detector)
[Unique findings from drift-detector that weren't confirmed or disagreed elsewhere]

### Quality (from quality-scanner)
[Unique findings from quality-scanner]

### Spec Compliance (from spec-compliance)
[Unique findings from spec-compliance]

### Project Lanes
[Unique findings from each project lane that weren't confirmed or disagreed elsewhere. One sub-heading per lane; reviewers sharing an unrecognized `lane` are grouped under that lane's name. Omit this section if there were no project lanes.]

### Open Questions
Findings raised as unverified suspicions by one or more reviewers that couldn't be cross-validated. Surface so the user can decide.

- [Question, source agent, context]

### Raw Reports
<details>
<summary>drift-detector report</summary>

[paste the full drift-detector report]
</details>

<details>
<summary>quality-scanner report</summary>

[paste the full quality-scanner report]
</details>

<details>
<summary>spec-compliance report</summary>

[paste the full spec-compliance report]
</details>

<details>
<summary>blind-spot-finder report</summary>

[paste the full blind-spot-finder report]
</details>

<details>
<summary>[project-lane-name] report</summary>

[paste each project lane's full report — one block per lane that returned. Omit if there were none.]
</details>
```

## Output
```
Plans/<PlanName>/reviews/<NN>-<plan-slug>-code-review-<rev>.md
```
The inline review is presented to the user; the same findings are persisted to the review artifact (step 7), which is the durable record. Beyond the Resolution Log (step 8):
- Significant drift or blind spots should also be captured in the phase debrief (via `/debrief`)
- Planning gaps should be addressed by updating the relevant spec, design, or plan — a reconciliation event per `shared/frontmatter-schema.md` § Stable Identifiers
- Unresolved questions can be added as open questions in the relevant artifact

## What This Is NOT
- Not a general code review (style, formatting, best practices) — focuses specifically on plan alignment
- Not `/poke-holes` (which analyzes planning artifacts, not code)
- Not a substitute for tests — assumes the test suite validates correctness independently

## Context
- Review artifacts: `shared/review-artifacts.md` (template: `shared/templates/review.md`)
- Orchestration: `shared/orchestration.md`
- Project review-lane socket: `shared/review-lanes.md` (template: `shared/templates/custom-reviewer.md`)
- Schema: `shared/frontmatter-schema.md`
- Target plan: `Plans/<PlanName>/` (status: `active`)
- Related specs: `Specs/`
- Related designs: `Designs/`
- Prior debriefs: `Plans/<PlanName>/notes/`
- Local repo paths: `planning-config.local.json`
- Sub-agents (dispatched via Task from the primary context): `sdd-planner:drift-detector`, `sdd-planner:quality-scanner`, `sdd-planner:spec-compliance`, `sdd-planner:blind-spot-finder`
