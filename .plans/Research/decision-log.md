---
title: "Decision Log — tracking decided truth across Spec/Design/Plan"
type: research
status: draft
created: 2026-07-12
updated: 2026-07-13
tags: [decision-log, adr, architecture, memory, collision-detection]
related: [Decisions/decisions.md]
---

# Decision Log — tracking decided truth across Spec/Design/Plan

## Context

We want a persistent, machine-readable log of every decision the user makes during planning — design choices, concept definitions, answered design questions — that all sdd-planner skills consult as truth in future interactions, with collision detection that stops for user clarification when a new decision contradicts a recorded one.

This research covers three questions: (1) industry best practice for decision records, (2) what Claude Code's plugin/hook machinery actually supports for capturing and recalling decisions, (3) where the feature integrates into the existing sdd-planner architecture.

## Findings

### Key Insights

1. **The ADR tradition is the proven model** (as of 2026-07). Nygard-format and MADR (Markdown Any Decision Records, v4) records are append-only with status-only mutation: accepted decisions are never edited; a change is a *new* record that supersedes the old, with bidirectional links (`supersedes` / `superseded_by`). Rejected decisions are kept with reasons — they are negative truths that prevent relitigating. Sources: https://adr.github.io/madr/, https://martinfowler.com/bliki/ArchitectureDecisionRecord.html, AWS Prescriptive Guidance ADR process.
2. **Y-statements supply the collision-friendly shape.** The one-sentence template ("in context X, facing Y, we decided for A *and against B, C*, accepting Z") makes the *rejected alternatives* explicit — a later decision *for* a previously rejected option in the same scope is a deterministic, zero-LLM collision. Source: https://medium.com/olzzio/y-statements-10eb07b5a177.
3. **No mainstream tool does decision-vs-decision contradiction detection** (documented search as of 2026-07-12: adr-tools, log4brains, zircote/adr Claude plugin, GitHub spec-kit, AWS Kiro, OpenSpec, Cline Memory Bank). Existing approaches are manual supersession bookkeeping, silent precedence resolution (Kiro steering files), or per-run LLM sweeps (spec-kit `/speckit.analyze`). The research-grade architecture is a **candidate-pair funnel**: cheap structural/lexical filter → LLM judgment on top-k candidates only (arXiv 2504.00180). Building this is genuinely differentiating.
4. **spec-kit's constitution is the closest "consulted as truth" precedent**: a versioned file every command re-reads, with a Sync Impact Report emitted on every amendment listing downstream artifacts affected. The supersession-cascade idea (flag artifacts that cited a now-superseded decision) is worth copying.
5. **Claude Code has no hook that fires when the user answers a question** (verified against docs.anthropic.com / code.claude.com as of 2026-07-12). There is no PostToolUse capture of AskUserQuestion answers. Capture must therefore be *behavioral* — skill instructions at the moments decisions happen — exactly how sdd-planner already enforces intent isolation. Plugins CAN ship hooks (`hooks/hooks.json`); `SessionStart` and `UserPromptSubmit` hooks can inject `additionalContext`, which suits *recall*, not capture.
6. **sdd-planner has no machine-readable decision store today** — decision content is scattered prose: `## Design Decisions` (design template), `## Key Decisions` (plan README), `## Decisions Made` (debrief), `## Open Questions` (spec/research/plan). The open-questions approval gates in /specify, /design, /plan and the escalation rules in /implement are precisely where decisions get made — those are the write triggers.
7. **The researcher agent is the universal recall path.** Every lifecycle skill dispatches `sdd-planner:researcher` first, and its guidelines already say "flag conflicts between artifacts." Adding `Decisions/` to its scan gives every skill decision-log consultation without touching each skill's read logic.
8. **Intent isolation constrains distribution.** The decision log is intent/context: it may go to researcher, plan-reviewer, spec-reviewer — never to quality-scanner (intent-blind) or blind-spot-finder (diff-only), and it should stay out of drift-detector's and spec-compliance's curated bundles.

### Sources

- MADR v4 format and statuses — https://adr.github.io/madr/ (as of 2026-07-12)
- Nygard ADRs, supersession convention — https://martinfowler.com/bliki/ArchitectureDecisionRecord.html; adr-tools `supersede` behavior — https://github.com/npryce/adr-tools
- AWS ADR lifecycle (immutable-once-accepted, keep rejections) — https://docs.aws.amazon.com/prescriptive-guidance/latest/architectural-decision-records/adr-process.html
- Y-statements — https://medium.com/olzzio/y-statements-10eb07b5a177
- spec-kit constitution + Sync Impact Report — https://github.com/github/spec-kit (templates/commands/constitution.md)
- Kiro steering files (precedence resolution) — https://kiro.dev/docs/specs/
- OpenSpec ADR-alongside-specs schema — https://github.com/Fission-AI/OpenSpec/
- zircote/adr Claude Code plugin (closest prior art; code-vs-ADR compliance agent, no decision-vs-decision detection) — https://github.com/zircote/adr
- Contradiction-detection funnel (similarity filter → NLI/LLM judge) — https://arxiv.org/html/2504.00180v1
- Claude Code hooks: plugin delivery, event list, additionalContext capabilities; absence of AskUserQuestion-answer hook — https://code.claude.com/docs (verified 2026-07-12)
- Repo integration points — shared/frontmatter-schema.md, shared/orchestration.md (Session Onboarding), shared/autonomy.md, shared/path-resolution.md, agents/researcher.md, commands/{specify,design,plan,implement,tend}/SKILL.md

## Analysis

### Implications

- Capture cannot be automated at the harness level; it must be woven into the skills at their existing decision moments (approval gates, open-question resolution, /implement escalations). This matches the plugin's existing behavioral-enforcement philosophy.
- A hybrid storage shape fits both the LLM (one file to read/grep) and the artifact conventions (first-class type, frontmatter as machine layer): a single ledger whose frontmatter carries a `decisions[]` array, following the `phases[]`/`tasks[]` precedent.
- Collision detection must never auto-resolve — every tool that resolves silently is criticized for it; the ADR tradition insists the human owns supersession. A detected collision is a new stop-for-user condition in the autonomy table.
- Naming hazard: "decision framework" already means the reasoning discipline (shared/decision-framework.md). Use "decision log" / "decision record" consistently.

### Recommendations

Adopt the architecture below (detail in the design doc to follow):

1. **Storage**: new artifact type `decision`, single canonical ledger `Decisions/decisions.md` with a `decisions[]` frontmatter array; per-entry fields: `id` (`D-NNNN`, zero-padded sequential), `kind` (decision | definition | answered-question | assumption), `status` (proposed | accepted | rejected | superseded), `statement`, `question`, `rejected[]` (anti-choices), `rationale`, `scope[]` (governed artifacts), `tags[]`, `date`, `decided_by`, `supersedes` / `superseded_by`, `reversibility` (one-way | two-way).
2. **Capture (write triggers)**: /specify, /design, /plan record decisions when open questions resolve at approval gates; /implement records escalation resolutions; /brainstorm records the accepted recommendation; /debrief backfills "Decisions Made". Writes are autonomous (template-following artifact writes); *making* the decision remains a user stop.
3. **Recall (read path)**: researcher scans `Decisions/` and emits a "Recorded Decisions" section; session-onboarding read order in shared/orchestration.md gains a decision-ledger step; post-compaction re-read list includes it. Optional later: a small `SessionStart` hook injecting only `accepted` entries scoped to the active plan.
4. **Collision detection (three layers, cheapest first)**: (a) structural — same scope/tags with new choice ∈ existing entry's `rejected[]`, or directly opposing statements on the same `question`; (b) lexical filter — grep tags/scope for top-k related entries at append time; (c) LLM judgment on candidate pairs only: contradicts | refines | supersedes | unrelated. On contradicts/supersedes → STOP, present both entries, user reconciles (supersede with links, amend, or withdraw). Never auto-resolve.
5. **Hygiene**: new `/tend decisions` mode — collisions among accepted entries, orphaned scope references, unrecorded prose decisions (backfill), superseded-but-still-cited entries.
6. **Isolation**: ledger goes to researcher, plan-reviewer, spec-reviewer only; add it to the explicit prohibition in the quality-scan prompt; never in diff-lane bundles.
7. **Plumbing**: add `Decisions/` to shared/path-resolution.md, /setup bootstrap, CLAUDE.md directory tree; add `decision` type + statuses to shared/frontmatter-schema.md; new `shared/templates/decision-log.md`.

## Open Questions

- Scope granularity of the ledger: one global ledger per planning root vs per-plan ledgers (recommend global, with `scope[]` filtering — decisions like concept definitions cross plans).
- Should heavyweight decisions (real options analysis) get promoted to per-decision MADR files `Decisions/D-NNNN-slug.md` with a stub row in the ledger, or is a `details` body section in the ledger enough at this scale?
- Does the dashboard plugin need a decisions view (new statuses need color-map rows either way)?
- Whether to ship the optional SessionStart hook in v1 or defer until the behavioral capture path proves out.
