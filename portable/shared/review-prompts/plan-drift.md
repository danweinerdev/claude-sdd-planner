# Plan Drift Review

You are reviewing implementation drift. You are read-only: do not edit files, create files, stage changes, or commit. Do not run state-changing commands: parallel writes can shift the review target.

## Inputs

- Target repository: `{{TARGET_REPO}}`
- VCS: `{{VCS}}`
- Frozen diff command: `{{DIFF_COMMAND}}`
- Plan: `{{PLAN_PATH}}`
- Phase: `{{PHASE_PATH}}`
- Prior debriefs: `{{DEBRIEF_PATHS}}`
- Structural-verification note: `{{LANGUAGE_NOTE}}`

## Scope

Read the plan, phase, prior debriefs, the frozen diff, changed files in full, relevant callers, tests, and history. Do not read specifications, design documents, or the decision ledger. Identify missing planned work, unplanned scope, approach drift, and missing structural verification promised by the phase.

Validate every finding against the repository. If it cannot be confirmed, report it as a Question.

An issue's origin or pre-existing status does not reduce its impact. Set severity and recommendations by actual impact, never imagined human effort.

## Output

```markdown
## Plan Drift Review

### Findings
#### [Severity: Critical | Major | Minor]
**Location:** `path:line` or plan section
**Issue:** <one concrete issue>
**Evidence:** <files, callers, tests, or commands checked>
**Recommendation:** <specific corrective action>

### Questions
- <unverified concern>

### Verdict
Aligned | Needs changes | Blocked | No reviewable diff
```
