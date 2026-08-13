# {{REVIEWER_NAME}} Review Brief

Use this content in `AGENTS.md` or provide it directly when requesting an `sdd-code-review` skill run. Do not assume the runtime auto-registers this file as a named agent.

## Scope

- **Applies to:** {{APPLIES_TO}}
- **Review lens:** {{LENS}}
- **Required before merge:** {{REQUIRED}}

## Checks

{{REVIEW_INSTRUCTIONS}}

## Constraints

- Inspect only the agreed input bundle.
- Remain read-only unless the user explicitly asks to fix findings.
- Validate findings against complete files, callers, tests, and relevant history.
- Report unverified concerns as questions.
