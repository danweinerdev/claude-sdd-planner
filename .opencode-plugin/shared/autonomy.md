# Autonomy Table

Cross-skill view of what runs autonomously versus what stops for the user. Each skill's own Escalation Rules section is the operational text; this table is the consolidated pattern — when writing or revising a skill, keep its rules consistent with this table.

## Runs autonomously (never ask)

| Work | Notes |
|---|---|
| Reads, searches, agent dispatch | Including parallel waves and resumes |
| Artifact writes that follow templates and evidence-gated status transitions | `sdd-plan` writing `draft`, `sdd-implement` recording completion evidence before flipping task statuses, etc. **Decision-ledger writes are the exception** — see below |
| Wave-to-wave progression in `sdd-implement` | Unless unresolved critical findings are pending end-of-wave escalation |
| Retries within budget | One resume with clarified guidance after a failure (2 attempts total) |
| Non-critical review findings | Collected and presented at end of wave, work continues |

## Stops for the user (always ask)

| Decision | Where it's enforced |
|---|---|
| Destructive actions — deleting data, prod config, shared systems | `sdd-implement` escalation rules; `code-implementer` |
| Approval transitions — spec/design/plan `approved` | `sdd-specify`, `sdd-design`, `sdd-plan`; explicit user sign-off only |
| Completion without durable evidence | `shared/completion-evidence.md` — task, phase, and plan stay non-complete |
| Gated scope — in-scope work depends on an unanswered external question | `sdd-plan`, `sdd-specify`, `sdd-design`; reviewers flag as Critical |
| Decision-ledger writes — **every** mutation (new entry incl. `proposed`, acceptance, supersession flip, hygiene repair) requires explicit user approval of the exact, unmodified text, shown in full first | `shared/decision-log.md` write gate; `sdd-decide`, the `decision-log` skill, and every lifecycle capture point; never written on assumption or non-objection |
| Decision collision — a new decision contradicts or supersedes an `accepted` ledger entry | `shared/decision-log.md` collision procedure; `sdd-decide` and every capture point; never auto-resolved, never picked by recency |
| Plan-vs-reality mismatch — the plan describes a codebase that doesn't exist as written | `code-implementer` STOPs; `sdd-implement` surfaces, never patches around it |
| Spec amendment — a contract test can only pass by weakening the assertion | `code-implementer` STOPs; `spec-compliance` flags as Critical |
| Scope expansion discovered mid-implementation | `sdd-implement` escalation rules |
| Critical findings unresolved after 2 review-fix cycles | `sdd-implement` — task `blocked`, no next wave without a decision |
| Target repo unresolvable | `shared/path-resolution.md` — ask, never guess or clone |
| External review lanes on a repo that isn't the session's project | `sdd-code-review` trust gate |
| Rehearsal opt-in for high-risk plans | `sdd-plan` — costs real implementation spend |

## SCM boundary cadence

Planning artifacts are **written in flow and committed at boundaries** (D-0024).
Writing never implies a commit. In a commit-capable Git workflow where commits
are authorized, the only lifecycle commits are:

| Boundary | One commit carrying |
|---|---|
| Phase (or plan) open | The plan/phase status flip and any interview or payload output |
| Phase close | Every task status, checked subtask, completion evidence, spec/design amendment, decision entry, review artifact, debrief, and plan phase-array update written during the phase |
| End of a contiguous spec/design/plan session outside implementation | Every artifact and ledger entry the session produced |
| A `Not-Aligned` gate round | The resolved review plus the tasks it routed and the amendments it required — then implementation resumes; the next round is a fresh review |

Implementation commits stay pure: one clean, complete, bisectable revision per
task with no planning-artifact bytes. Between boundaries a complete task's
record stays uncommitted — `sdd validate` asks for the committed copy only once
the phase is complete, and the whole planning root counts as lifecycle after a
frozen review endpoint, so amendments do not need their own pre-review commit.
Never commit per task (`in-progress` flips, `complete with evidence`), per
amendment, per decision, or per review. The expected shape of a phase's history
is one lifecycle commit, N implementation commits, one lifecycle commit.
External planning roots record once at the same boundary. A Not-Aligned round
is the one legitimate extra: its routed tasks change the phase doc's intent, so
they must be committed before the next `reviewed_planning_revision`.
