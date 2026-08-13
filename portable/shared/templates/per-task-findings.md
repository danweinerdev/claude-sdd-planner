## Task {{TASK_ID}} — Quality Findings (change `{{CHANGE_REF}}`)

| # | Severity | Lens | Location | Finding |
|---|---|---|---|---|
{{FINDINGS_TABLE_ROWS}}

{{QUESTIONS_SECTION}}

**Verdict:** {{VERDICT}}

## Recommendation

{{RECOMMENDATION_PARAGRAPH}}

{{OPTIONS_LIST}}

<!--
Placeholder reference:

- TASK_ID            — e.g., "2.1"
- CHANGE_REF         — from the implementer's report: short commit SHA
                       (git), changelist number (perforce), or "n/a"
                       (no VCS)
- FINDINGS_TABLE_ROWS
                     — one row per finding, lifted verbatim from the
                       quality-scanner output's table; preserve order.
                       Severity and lens vocabulary are defined in
                       `shared/templates/quality-scan-output-format.md`
                       (Critical / Major / Minor / Question;
                       Correctness / Safety / Maintainability /
                       Testing / Over-Engineering).
- QUESTIONS_SECTION  — optional. If the scanner raised questions or
                       unverified suspicions, render them here as:
                           **Questions raised:** ...
                       Otherwise omit the entire block (delete the
                       placeholder line, including the surrounding
                       blank lines).
- VERDICT            — one line summarizing the scanner's recommendation
                       plus the test/clippy state, e.g.:
                       "Accept with follow-ups. No Critical/Major. 192/192
                       tests pass, clippy clean."
- RECOMMENDATION_PARAGRAPH
                     — 1–3 sentences naming what to do next and why,
                       calibrated to the severity of the findings:
                         - Critical → "Send back to the implementer; <fix>"
                         - Major    → "Send back to the implementer; <fix>"
                                      (or, if user judgment is needed,
                                      "Recommend sending back; ask user.")
                         - Minor/Question → "Accept-with-followups" or
                                      "Defer to phase-end cleanup"
- OPTIONS_LIST       — optional. When the recommendation is non-obvious
                       or the user has a real choice, render 2–3
                       alphabetized options, e.g.:
                           **(a)** ...  **(b)** ...  **(c)** ...
                       Mark the recommended option **(recommended)**.
                       Omit when the recommendation is unambiguous.

Rendering rules:
- Use Markdown tables; preserve the column header row exactly.
- File:line references in the Location column are clickable in most
  terminals — keep the `path:line` format the scanner produces.
- Do NOT add plan/spec/design context to the rendered output;
  per-task findings are intent-blind by construction.
-->
