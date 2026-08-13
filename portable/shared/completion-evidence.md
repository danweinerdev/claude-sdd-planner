# Completion Evidence

Prospective `verification` says how work will be judged. Retrospective
completion evidence records what ran and what it proved. A task, phase, or plan
may not transition to `complete` until its required evidence is populated and
durably recorded.

## Required sections

- Every task body has `### Completion Evidence` within its `## <task-id>` section.
- Every phase document has `## Phase Completion Evidence`.
- Every plan README has `## Plan Completion Evidence`.

Incomplete work uses the literal line `Pending — not complete.`. A `complete`
status and a pending or absent evidence section are contradictory.

## Task evidence

Before setting a task to `complete`, replace the pending marker with:

```markdown
### Completion Evidence

- Verified: YYYY-MM-DD
- Repository: `<repository root>`
- VCS: `git | perforce | <validated SCM kind>`
- Revision / checkpoint: `<exact tested native SCM revision/checkpoint>`
- Identity recheck: `<exact command/tool, timestamp, and matching revision/checkpoint>`
- Focused review: `<exact command/tool>`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `<exact Revision / checkpoint identity>`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `<exact command>` | `<path>` | PASS (`exit 0`) | `<specific output or behavior observed>` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `<tool or exact procedure>` | `<paths/environment>` | PASS | `<specific observation>` |
```

**Sensitive data (`shared/frontmatter-schema.md` § Sensitive Data):** evidence
is committed artifact content — repo-root-relative paths are the default
everywhere. Record `Working directory` cells relative to the repo root (`.`,
`./services/api`). `Repository` is the one label that must resolve to the
target root, so it uses the generic `~/...` form (the validator expands `~`);
never a literal `/home/<user>` prefix. Scrub user-identifying paths and
credentials from any pasted output before it lands in an evidence row.

Record the exact command, working directory, exit status, and observable result.
At least one command or tool/inspection row is required. Every final check must
pass. Every evidence label shown above occurs exactly once as a visible list
item; labels in comments and fenced blocks do not count. The focused review is
of the complete task diff, not a later phase gate.

## Native SCM completion

Each plan task is one clean, complete, independently bisectable feature or
internal-capability slice: implement and verify it, review its
complete diff, then record it as one scoped native SCM revision/checkpoint.
Subtasks are mechanical steps within that boundary.

Native SCM is the sole durable source identity. SDD does not capture source
files, hashes, manifests, or parallel source stores. Dirty Git, no-SCM, and any
state without a durable native revision/checkpoint remain non-complete.

### Git adapter

The tested identity is one clean, full, non-merge implementation commit. Record
that exact commit as `Revision / checkpoint`; a dirty suffix or worktree state
is not an identity, and neither is a dirty-worktree snapshot. Later unrelated
worktree edits do not invalidate an already recorded clean immutable task
commit. Current phase completion still requires the final reviewed target
worktree to be clean; do not apply that current-completion requirement as a
global historical worktree-clean check. For focused review use exactly `git show
<full40>` or `git diff <full40>..<full40>` followed by the required review
statement. A ranged review uses the task commit's direct first parent and ends
at that task commit.

Record completion status and evidence through the planning root's validated
lifecycle SCM adapter in a separate scoped lifecycle commit. The current Git
planning adapter is strict: lifecycle artifacts and final phase reviews must be
committed at planning `HEAD`. Perforce or another SCM may complete only after a
validated native revision and lifecycle adapter exists. No-SCM cannot complete.

## Phase evidence

Before setting a phase to `complete`, record common verification fields and
phase-level checks, then list concise task identities:

```markdown
### Completed task identities
- `1.1`: `<native implementation revision/checkpoint>`
```

Include `- Final aligned review: <persisted review artifact path>; frozen:
<revision/range>` for the resolved, frozen four-lane `Aligned` review. All tasks
and acceptance criteria must be complete. Material review fixes create new tasks
and require a fresh frozen review. The common labels and `Final aligned review`
each occur exactly once visibly. Record required independent phase command/tool
evidence and concise task identities; do not add child-evidence rollup sections.

## Plan evidence

Before setting a plan to `complete`, record common verification fields and
plan-level checks, then list concise completed phase identities:

```markdown
### Completed phase identities
- `1`: `<phase completion revision/checkpoint>`; review: `<final review artifact path>`
```

Every phase must be complete and every required final check must pass. The
common labels occur exactly once visibly. Record required independent plan
command/tool evidence and concise phase identities; do not add child-evidence
rollup sections.
For Git/git-worktree evidence, each completed phase checkpoint and the plan
checkpoint must exist in the same target repository, and each phase checkpoint
must be an ancestor of (or equal to) the plan checkpoint.

## Legacy completed artifacts

Artifacts already marked `complete` without conforming evidence are legacy gaps.
Do not fabricate evidence or infer commands from checkboxes; rerun verification
or provide a contemporaneous durable record tied to the native revision.
