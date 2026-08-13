---
name: sdd-poke-holes
description: "Adversarial critical analysis of plans, specs, or designs. poke holes, find gaps, challenge this, what could go wrong, devil's advocate"
---

# Adversarial Critical Analysis

## Resources

Before opening `shared/...`, follow symlinks in this loaded file's path, then derive `<plugin-root>` from `<plugin-root>/skills/<name>/SKILL.md`; fallback search roots are repository/user `.agents/` (including `$HOME/.agents/plugins/*/`), Codex `${CODEX_HOME:-$HOME/.codex}/plugins/cache/*/*/*/`, and runtime-configured skill roots. Accept only a root containing this skill, `shared/agent-runtime.md`, and the matching plugin manifest; never use the working directory. Then read `<plugin-root>/shared/agent-runtime.md` and `<plugin-root>/shared/path-resolution.md`, and resolve every `shared/<path>` reference in this skill against `<plugin-root>`.

**Resource boundary:** Read the plugin, all `SKILL.md` files, and `shared/` resources in place. Never copy or symlink them into the working directory, target repository, or planning root. Only generated SDD outputs may be materialized from bundled resources.

## When to Use
When you need an **adversarial** critical review of a planning artifact before committing to it. Good for stress-testing plans before approval, finding gaps in specs, and challenging design assumptions. This is not a structural review (the plan-review prompt (`shared/agent-prompts/plan-reviewer.md`) and the spec-review prompt (`shared/agent-prompts/spec-reviewer.md`) handle that). This skill actively tries to break the thinking.

Think of it as the planning-artifact counterpart to the `review_blind_spots` lane (`shared/review-prompts/blind-spots.md`) (which does the same thing against code diffs): fresh eyes, hostile framing, deliberately looking for what the author *didn't* think about.

## Process

1. **Identify Target**
   - Ask which artifact to analyze (plan, spec, design, or brainstorm)
   - Confirm the artifact path before proceeding

2. **Gather Context**
   Read the full target artifact yourself in the primary context (a single-artifact read is lightweight and the raw wording is the attack surface). Dispatch the researcher prompt (`shared/agent-prompts/researcher.md`) only for the *related-context sweep*:
   - Read all documents referenced in the target's `related` frontmatter
   - Search for any specs, designs, or plans that reference this artifact (by filename or title)
   - Return a structured summary containing:
     - Related context that informs analysis (from `related` docs and reverse references)
     - Cross-references, dependencies, and any conflicts between documents

3. **Adversarial Analysis**
   Apply the lenses below to the full artifact text you read, informed by the researcher's related-context summary. The posture is adversarial — you are not checking whether the artifact is well-structured, you are trying to find the thing that will blow up in six weeks. For every finding, produce a **concrete scenario**: a sequence of events, inputs, or conditions that exposes the flaw. "This is risky" is not a finding. "If the downstream service returns a 503 during step 3, the transaction is orphaned and the plan has no cleanup story" is a finding.

   **The Hostile Reader Lens**
   - Read the artifact assuming the author is wrong. Where does the argument crack first?
   - Which claims are asserted without evidence? What happens if each one turns out to be false?
   - What load-bearing assumption is so obvious the author didn't even write it down?

   **The Unsourced Claims Lens**
   - Any statistic or factual claim in the artifact without a source and as-of date is a finding (severity Minor, or Major if a decision rests on it).
   - Any absence claim ("no library does X", "nothing depends on Y") without a documented search trail is a finding.
   - Any external-contract detail that doesn't trace to a pinned source is a finding.

   **The Missing Scenarios Lens**
   - What concrete error cases aren't handled? (Name them: "what if the DB is unreachable at step 2?")
   - What edge cases are dismissed or ignored?
   - What happens under load, at scale, over time, at the boundary, with pathological input?
   - What happens when each external dependency fails independently? When two fail at once?
   - What happens during deploy, rollback, partial rollout, or while the previous version is still running?

   **The Scope Trap Lens**
   - Which tasks look trivial but actually touch many files, systems, or teams?
   - Where is the integration work hidden that no phase accounts for?
   - What dependencies between tasks are implicit?
   - What has to happen in a specific order but isn't marked as ordered?

   **The Alternatives Lens**
   - Is there a dramatically simpler approach the author didn't consider?
   - What would a cynical senior engineer suggest instead?
   - Is this over-engineered for the actual problem? Under-engineered?
   - Is there prior art (internal or external) that the artifact ignores?

   **The Operational Reality Lens**
   - How does this fail in production at 3 AM?
   - What does the on-call runbook look like? Does one exist? Can it?
   - What's the rollback story — is it real, or "revert the deploy and pray"?
   - What monitoring, alerting, or observability is missing?
   - Who maintains this six months from now, with none of the current context?
   - What happens to in-flight data during migration or cutover?

   **The Contradiction Lens**
   - Does anything in the artifact contradict anything in a related doc the researcher surfaced?
   - Does the plan assume something the spec doesn't guarantee?
   - Does the design promise something the plan doesn't schedule work for?

4. **Validate Each Finding**
   Before including a finding, validate it against the **artifact text itself**; use the researcher's summary only for related-document checks:
   - Is the scenario you described actually reachable given what the artifact says?
   - Is the gap you found already addressed in a related document the researcher surfaced?
   - Would the finding hold up if the author pushed back?

   Drop findings you can't defend. A sharp, defensible critique is worth more than a long list of handwaved concerns.

5. **Rate Findings**
   Categorize each finding:
   - **Critical**: Could cause project failure or major rework
   - **Major**: Significant risk that should be addressed before proceeding
   - **Minor**: Worth noting but won't block progress
   - **Question**: Ambiguity that needs clarification, not necessarily a problem

   Never downscope severity by estimating how long a fix would take a human. Agents are not constrained by human development timelines. Severity reflects the impact of the flaw, not the cost of the fix. The right fix is right; surface it and let the user decide.

6. **Persist the Review**
   Write the findings to a review artifact per `shared/review-artifacts.md`: `<target-home>/reviews/<NN>-<target-slug>-adversarial-review-<rev>.md` from `shared/templates/review.md` (Plans > Designs > Specs precedence; a flat artifact with no resolvable home stays inline — say so). Number findings `F-NN`, mirror them in the `findings[]` frontmatter, cite the ids each finding impugns (`FR-NN`, `AC-NN`, task `N.M`, `D-NNNN`), and set `status: open`.

7. **Present Results**
   Show findings grouped by severity, referencing the review file. Each finding includes: the concrete scenario, why it matters, and a concrete mitigation or question to resolve.

8. **Offer to Resolve**
   Ask the user if they want to act on findings now. If yes, follow `shared/review-artifacts.md`: classify each finding (mechanical fix → apply directly citing the governing fact; design decision → present options, discuss, record the outcome in the decision ledger); append Resolution Log entries; run the reconciliation sweep for every changed numbered element; track deferred work as plan tasks or `FU-NN` entries. Other options: create a research document for unknowns (the `sdd-research` workflow), or proceed as-is with the review left `open`.

## Output
```
<target-home>/reviews/<NN>-<target-slug>-adversarial-review-<rev>.md
```
Findings live in the review artifact, not just the conversation. If the user acts on findings, the reviewed artifact is updated in place and dispositions land in the review's Resolution Log.

## Context
- Review artifacts: `shared/review-artifacts.md`
- Template: `shared/templates/review.md`
- Orchestration: `shared/orchestration.md`
- Agent: the researcher prompt (`shared/agent-prompts/researcher.md`)

## What This Is NOT
- Not a structural review (that's what the plan-review prompt (`shared/agent-prompts/plan-reviewer.md`) and the spec-review prompt (`shared/agent-prompts/spec-reviewer.md`) agents do)
- Not a code review (this operates on planning artifacts, not code — use `sdd-code-review` for code, which dispatches the `review_blind_spots` lane (`shared/review-prompts/blind-spots.md`) for the code equivalent of this skill's lens)
- Not a blocker — findings are advisory, the user decides what to act on
