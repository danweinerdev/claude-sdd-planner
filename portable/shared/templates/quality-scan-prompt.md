Review change `{{CHANGE_REF}}` in `{{REPO_PATH}}` (VCS: `{{VCS}}`). A single commit/changelist labeled `{{CHANGE_MESSAGE}}`.

Run **intent-blind**. Do not read any plan, spec, design, research, debrief, or decision-ledger artifact. Evaluate strictly on code quality.

## Scope
- Just change `{{CHANGE_REF}}`. View it with `{{SHOW_COMMAND}}`.
- If `{{VCS}}` is `none`, there is no change reference — review the files listed below directly.
- Files touched:
{{FILES_LIST}}

## What the change does (per its own message)
{{CLAIMED_CHANGES}}

## Specific things to verify
{{FOCUS_LIST}}

## Output

Render as a markdown table — `sdd-implement`'s per-task-findings rendering needs the compact shape:

| # | Severity | Lens | Location | Finding |

Location cells use `path:line` format. Each Finding cell contains the concrete defect plus the validation evidence (what you read/ran to confirm it).

Severity vocabulary, lens vocabulary, validation discipline, and the recommendation block are defined in `shared/templates/quality-scan-output-format.md`. The summary: severities are **Critical / Major / Minor / Question**; lenses are **Correctness / Safety / Maintainability / Testing / Over-Engineering**; if you can't validate a finding, downgrade to Question rather than reporting a defect.

After the table, give a one-paragraph **Recommendation** (block / fix-then-accept / accept-with-followups / accept).

Do not include any plan-aware reasoning. Do not say "this matches the spec" or "this satisfies the verification" — you don't have the spec.

<!--
Placeholder reference:

- CHANGE_REF         — the change reference from the implementer's
                       report: short commit SHA (git), changelist
                       number (perforce), or "n/a" (no VCS)
- REPO_PATH          — absolute path to the target repo on disk
- VCS                — the label `sdd-implement` detected in step 2:
                       git | git-worktree | perforce | none
- SHOW_COMMAND       — the resolved, ready-to-run command to view the
                       change, e.g. `git show <sha> --stat` (per-file:
                       `git show <sha> -- <file>`) for git, or
                       `p4 describe -s <changelist>` for perforce.
                       Omit the Scope bullet for `none`.
- CHANGE_MESSAGE     — the commit/changelist's own subject line,
                       including the task id suffix if present (e.g.,
                       "ark-core: DependencyState (2.1)")
- FILES_LIST         — bullet list of file paths touched by the
                       change. One bullet per file. The scanner uses
                       this to scope its read budget.
- CLAIMED_CHANGES    — the implementer's report of what changed,
                       paraphrased into 2–6 sentences. The scanner
                       reads this to know what to verify but stays
                       intent-blind on plan/spec context.
- FOCUS_LIST         — the orchestrator's curated list of risk areas
                       in this specific diff, rendered as numbered or
                       bulleted concerns. This is the only part of
                       the dispatch that materially varies run-to-run
                       — write it carefully. Examples:
                         1. Does the new validate_x function handle
                            null inputs?
                         2. The implementer claims they renamed all
                            call sites of foo(); spot-check several.
                         3. The new test asserts X but the production
                            change implies Y — read both and confirm
                            consistency.
                       Aim for 4–8 concrete, testable concerns. Avoid
                       vague prompts like "review for correctness".

-->

Rendering rules:
- Send the rendered prompt to a collaboration subagent in a fresh
  context that does not inherit the conversation; when collaboration is
  unavailable, perform the pass yourself following this template and
  label the result **self-review**. `shared/review-prompts/quality.md`
  carries the reviewer's own rules for output format and validation
  discipline; the template provides the framing.
- Do NOT include plan/spec/design/research/debrief content in
  CLAIMED_CHANGES or FOCUS_LIST. Reference symbol names, file paths,
  and concrete behaviors only.
- The "Output" block is normative — do not modify the table headers
  or severity/lens vocabulary. The per-task-findings.md template
  expects this exact shape.
