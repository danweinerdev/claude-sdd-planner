# Orchestration Model

The primary context acts as a **tech lead** — it reads enough to make informed decisions about what to delegate, then delegates execution to agents. Only summarized results return to the primary context.

## Roles

**Primary context** handles:
- User interaction (questions, confirmations, approvals)
- Scoping and delegation decisions (what work to do, which agent does it)
- Reviewing agent results and deciding next steps
- Lightweight reads needed to make delegation decisions (e.g., reading a plan README to know which phase to implement)

**Agents** handle:
- Heavy reading (scanning many artifacts, reading large codebases)
- Analysis (complexity analysis, decision-ledger audits, adversarial review prep)
- Code changes (implementation, fixes)
- Reviews (code review, plan review, spec review)

## Principles

1. **Delegate execution, keep decisions.** If the work is reading 15 files and summarizing, that's an agent's job. If the work is choosing between two approaches based on a summary, that's the primary context's job.

2. **Not everything gets delegated.** Lightweight reads, user conversations, and creative judgment stay in the primary context. The overhead of spinning up an agent isn't worth it for a single file read or a quick decision.

3. **Agents return summaries, not raw content.** When an agent reads artifacts or code, it returns structured findings — not the full file contents. The primary context works from these summaries.

4. **Parallelize independent work.** When multiple agents can work independently (e.g., implementing tasks in a wave, scanning different artifact types), launch them concurrently.

5. **Resume agents for follow-ups.** When an agent's work needs a correction or continuation, resume it rather than starting fresh — preserves context and avoids re-reading.

6. **The decision framework binds the primary context too.** `shared/decision-framework.md` is the universal decision discipline — premise checks before complying, run-to-verify for any commandable claim, documented searches behind absence claims, verbatim failure reporting, no downscoping by imagined effort. Agents carry its digest in their bodies; the primary context applies the full framework directly, especially when synthesizing agent reports (don't launder an agent's unverified claim into a verified-sounding summary).

## Bundled role prompts

Multi-agent behavior is expressed as bundled, runtime-neutral role prompts that any skill can render and dispatch when collaboration subagents are available:

- `shared/agent-prompts/researcher.md` — context gathering across artifacts, codebase, and docs. Used by `sdd-specify`, `sdd-design`, and `sdd-plan`; `sdd-brainstorm`, `sdd-research`, and `sdd-debrief` may render it instead of writing an ad-hoc scan prompt.
- `shared/agent-prompts/spec-reviewer.md` — independent specification review (testability, completeness, ambiguity, scope, gated work) with an Approve/Revise verdict.
- `shared/agent-prompts/plan-reviewer.md` — independent plan/design review (completeness, feasibility, conventions, gaps, gated work) with an Approve/Revise verdict.
- `shared/review-prompts/` — the four code-review lanes (see `shared/review-lanes.md`).

Dispatch rules: substitute every `{{PLACEHOLDER}}` before dispatch; reviewer dispatches get a fresh context that does not inherit the primary conversation, so the artifact is judged as written rather than as intended. Skills must work without collaboration subagents: fall back to a transparent single-agent workflow, label reviewer fallbacks **self-review**, and never claim independent review or corroboration when none occurred.

## Session Onboarding

Orientation read order at the start of a planning session — frontmatter answers most orientation questions; read bodies only when the decision at hand needs them:

1. `planning-config.json` — planning root, repository mappings
2. The decision ledger's **frontmatter**, if it exists (`Decisions/decisions.md` under the planning root, or `<repo-root>/DECISIONS.md` for external planning roots — `shared/decision-log.md` § Ledger location) — `accepted` entries are standing constraints on all planning work
3. The active plan's README **frontmatter** — status, `phases[]`, `related` (not the body)
4. The current phase doc — task list, statuses, verification fields, traps
5. The latest debrief in `Plans/<PlanName>/notes/` — constraints and gotchas discovered last time

## After a Context Compaction

Summaries drop operational detail and misremember statuses. Before resuming work after compaction, re-read from disk:

- The current phase doc's `tasks[]` statuses — the frontmatter is the source of truth for what's done; never trust the summary's recollection of it
- The plan README frontmatter
- The decision ledger frontmatter (`Decisions/decisions.md`) — summaries misremember which decision won; the ledger's `accepted` entries are the truth
- Any escalation or question that was presented to the user and not yet answered

Do **not** re-read spec/design bodies wholesale after compaction — delegate that to agents, same as always.

## Tool Routing for Context Economy

- **Frontmatter-first**: when only statuses or fields are needed, stop reading at the end of the frontmatter.
- **Glob to check existence, Grep to locate, agent to comprehend** — don't read a file to answer a question its path or one line can answer.
- **Statuses scope reads**: skills filter artifacts by frontmatter `status` before reading anything (active plans, approved specs) — never scan a directory's bodies indiscriminately.
