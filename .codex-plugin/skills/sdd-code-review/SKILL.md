---
name: sdd-code-review
description: "Review implementation code against a plan, specification, or design for drift, correctness, missing requirements, and blind spots."
---

# Implementation Code Review

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, derive
`<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`, and read
`shared/agent-runtime.md`, `shared/path-resolution.md`, `shared/vcs-detection.md`,
`shared/completion-evidence.md`, and `shared/review-lanes.md` in place. Never
copy or symlink plugin material into a working directory, target repository, or
planning root.

## Review Lanes

Use four independent lenses. When fresh non-inheriting contexts are available,
dispatch all four in parallel; otherwise run affected lanes serially and record
the accurate `mixed` or `single-agent review` mode. Do not claim independent
corroboration for serial lanes.

| Lens | Dispatch identifier | Inputs | Purpose |
|---|---|---|---|
| Plan drift | `review_plan_drift` | Diff, plan, phase, prior debriefs | Missing work, scope creep, approach drift |
| Quality | `review_quality` | Diff and code only | Correctness, safety, maintainability, tests, needless complexity |
| Spec compliance | `review_spec_compliance` | Diff, specs, designs | Requirements coverage and contract violations |
| Blind spots | `review_blind_spots` | Diff and changed-code context only | Adversarial edge cases, production failures, security, concurrency |

Do not pass plan, spec, or design material to the quality or blind-spot lanes.
Use exactly these runtime-neutral identifiers when the runtime exposes a task
name or description field; do not request an agent or model.

## Synthesis Rules

- Only independently run lanes can corroborate findings; serial lanes do not corroborate, including in mixed or single-agent review mode.
- Surface lane disagreements with their evidence rather than silently resolving them in synthesis.
- Label a finding raised only by the blind-spots lane as `blind-spot-only`.
- Do not manufacture primary-agent findings in synthesis; consolidate only findings actually reported by a lane.

## Process

1. Identify the active plan, phase, target repository, and concrete diff range.
   A phase-completion gate requires a frozen durable native SCM revision/range;
   dirty or no-SCM work is not eligible.
2. Check completion evidence. Missing, pending, vague, failing, or
   source-identity-mismatched evidence is a plan-drift finding, not proof.
3. Render and run all four lanes, preserving their input isolation. Consolidate
   only actual findings and give `Aligned`, `Needs changes`, `Blocked`, or `No
   reviewable diff` with actual verification results.
4. Persist the review using `shared/templates/review.md`. A phase gate is
   driven through the binary: `sdd review scaffold <phase-path> --frozen
   <base>..<endpoint>` creates it open and unfrozen with `review_of` set to the
   phase, `review_scope: phase`, `verdict: Aligned`,
   `reviewed_planning_revision` set to the exact full planning Git commit
   containing the reviewed phase and plan README, a valid `review_mode`, and
   exactly four `lane_results`. Record each lane's specific observation with
   `sdd review evidence set <review-path> --lane <id>`, then close the gate
   with `sdd review resolve <review-path>`, which verifies every lane occurs
   once with `PASS/Aligned` and a `reviewed_identity` equal to `rev` and sets
   `frozen: true` + `status: resolved` atomically. Never hand-edit those two
   fields; a resolved review is immutable.
5. Validate the persisted gate with `sdd validate --scope Plans/<PlanName>
   --format json` before handing the result back to `sdd-implement`, and cite
   the gate from phase evidence as `- Final aligned review: <artifact
   path>; frozen: <exact rev>`. The Git target adapter uses a full immutable
   `<base>..<endpoint>` range whose endpoint is the clean phase checkpoint. The
   planning Git adapter loads phase and plan content at
   `reviewed_planning_revision`; only lifecycle-only changes may follow review.
   Write the review in-flow. At phase close, it belongs unchanged at planning
   `HEAD` in the planning-root atomic commit with the debrief, evidence/status,
   and plan phase-array update. Record a repo-owned ledger in that commit when
   it shares the root; otherwise record its root once at the same boundary
   (D-0024). In Git, commit only in commit-capable workflows where commits are
   authorized. Unsupported planning or target SCM adapters keep the phase
   non-complete.
6. Material findings create new planned tasks. Implement each as a complete,
   reviewed native revision, then freeze and rerun all four lanes.

## Output

```markdown
## Code Review: <plan or scope>
**Review mode:** Independent lanes | Mixed | Single-agent review
**Diff:** <exact range or command>
**Verdict:** Aligned | Needs changes | Blocked | No reviewable diff
### Findings
| Severity | Lens | Location | Issue | Recommendation |
|---|---|---|---|---|
### Verification
- `<command>`: <actual result>
```
