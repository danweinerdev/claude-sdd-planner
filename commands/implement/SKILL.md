---
name: implement
description: "Execute a plan. Graph plans walk the frontier: claim → red → green → sync → merge, observation-gated end to end. v1 markdown plans keep the wave-orchestration protocol until converted. Triggers: /implement, implement this, start phase, execute plan, build this"
---

# /implement — Walk the Plan Graph

## Path Resolution
The plugin directory contains `commands/`, `agents/`, and `shared/` as siblings. Find it by globbing for `**/commands/research/SKILL.md` in both the current directory and `~/.claude/plugins/cache/`; if multiple versions match, sort them as **semantic versions** (like `sort -V`) and use the highest, then strip `commands/research/SKILL.md` from the match. Resolve the planning root (artifacts) and target repository per `shared/path-resolution.md` in the plugin directory.

## Routing: Graph Plans vs v1 Plans

Route by graph presence:

- **`Plans/<Name>/<Name>-Graph.json` exists** → the walk loop below. States derive from observations; completion is sync-only; the binary refuses everything a narrated protocol used to let drift.
- **No graph** → the plan is a v1 markdown plan and keeps the v1 protocol (§ v1 Plans below) until converted with `sdd graph convert`.

## The Walk Loop

The loop is: **claim → red → green → sync → merge**, repeated until the frontier is empty. Every arrow is a CLI call whose refusal text names the fix. You never assert an outcome — you show the tool a report and it records what the report says (DD-5, D-0022).

### 0. Preconditions

- Plan README `status` is `approved` or `active`; flip `approved` → `active` when starting.
- `sdd graph status --plan <Name>` for the current picture: derived state counts, closure, claims. Nothing here is cached — every read derives fresh from structure plus observations.

### 1. Claim

```
sdd next Plans/<Name> --claim --by <identity>
```

Claims the heaviest claimable frontier node (critical-path-first), records a lease, and allocates an isolated workspace (git targets: a worktree on its own branch). The printed payload carries everything needed to start — contract, the cited requirements' inlined text, named tests, hazard triage, workspace path — by design; don't re-read the plan documents for what the payload already states.

When nothing is claimable, the refusal explains the frontier (state counts, active claims, capacity). React to what it says: capacity reached → finish or release something first; everything BLOCKED → work the RED/STALE nodes it names; frontier genuinely empty with all nodes GREEN → the walk is done.

### 2. Red — prove the tests can fail

Write the node's **named tests first** (the ids in the claim payload; the runner-visible ids must match exactly), run them in the workspace against the unimplemented or broken state, and sync the failing report:

```
sdd graph sync --plan <Name> --node <id> --by <identity> --report red.xml
```

A red run is a **successful** sync — recording the failure is the point. It stamps `red_seq` for each failing test, which is what arms red-before-green: a hazard-discharging test that has never been observed failing will refuse the later green ("a test that passes against both correct and broken code guards nothing").

### 3. Green — implement, commit, sync the pass

Implement inside the workspace until the named tests pass. **Commit the complete slice in the workspace first** — the revision anchor must name the tested bytes, and sync refuses a passing report from a dirty worktree. Then:

```
sdd graph sync --plan <Name> --node <id> --by <identity> --report green.xml
```

A clean pass by the claim holder **merges atomically**: observation recorded (with artifact digests, report digest, isolation, VCS provenance), claim cleared, workspace released. A pass with shared-dirty isolation records provisionally instead — the node derives STALE, never GREEN, until a clean re-verify. There is no assert path: `--command-exit` needs a real exit code, asserted isolation is refused by default.

**Then integrate the slice into the mainline.** The merging sync completes the *claim*; the VCS integration is a separate deliberate act because it can conflict, and conflicts are judgment. On git targets the workspace branch survives the release (`git branch --list 'graph/<id>-*'`) — merge it into the mainline checkout now. Until the bytes land on the mainline, the node honestly derives STALE from the shared tree's perspective (the recorded digests name bytes mainline doesn't have); integration self-heals it to GREEN. Don't stack un-integrated branches: integrate after every merge, before the next claim of dependent work.

### 4. Between Rounds

- `sdd graph status --plan <Name>` between claims; `sdd graph path` when choosing what to unblock first.
- **Command gates**: run the gate's command, capture output, `sdd graph sync --node <id> --command-exit <N> --command-log out.txt`.
- **Review gates** on the frontier: run the four-lane review flow (`sdd review scaffold` → fill lanes → `sdd review resolve`), then record it:

  ```
  sdd graph review --plan <Name> --node <gate> --artifact <frozen review path>
  ```

  The artifact must be `resolved` + `frozen: true` + verdict `Aligned` (all three — a reopened review is not evidence), must review a document of **this plan**, and greens exactly **one** gate — reusing another gate's artifact refuses naming it. Findings that name scope nodes demote them to RED in the same write; that is the finding doing its job, not an error to route around. Record the gate **after** the scope's work is integrated into the mainline: the gate's observation digests the aggregate scope diff from the shared tree, and reviewing bytes that aren't there yet records an anchor of nothing.

### Stopping Rules

- **2 consecutive failures** on one node → the node is probably too big: propose `sdd graph split --plan <Name> --node <id> --file children.json` (each child one red→green cycle; the split is compile-gated and retires the parent id).
- **3 consecutive failures** → **stop and escalate to the user.** Do not grind a third variant of the same approach into the same node.

### Reaction Protocol

- **INTENT-STALE** (a cited requirement's fingerprint no longer matches): re-read the *diff of that requirement only* — not the whole spec. Then judge: cosmetic change → recompile refreshes the hash; behavioral change → rework the node; contract invalidated → replan. This is a judgment step; never auto-pick.
- **Finding demotion**: a recorded review demoted nodes to RED — they re-enter the workable set like any red node. Rework them; the gate goes seq-stale when the rework re-verifies, which is the system asking for re-review, not a malfunction.
- **Lease expiry / crashes**: an expired claim's workspace is preserved as post-mortem evidence. Inspect it if useful, then `sdd graph gc --plan <Name>` — gc persists the expiry and reaps the workspace; the node returns to the frontier. A stale claimant's late sync is refused by claim discipline.
- **Abandoning a node**: `sdd graph release <id> --by <identity>` — never squat on a claim you aren't working.

### Evidence Language (D-0022)

For graph plans, **observation records and rendered views are the completion record**. You never write completion-evidence prose as a gate input, never edit `<Name>-Graph.json` by hand (the guard denies it; every mutation goes through a verb), and never edit rendered `NN-*.md` views (the renderer overwrites them, or refuses when they are frozen). When the user asks "where are we?", the answer is `sdd graph status` / `sdd graph export --format plan` output — derived truth, not narration.

### Delegating Node Execution

Node work may be dispatched to `sdd-planner:code-implementer` agents. The dispatch carries the **claim payload verbatim** — contract, cited requirement text, named tests, hazards and the shapes their tests must take, workspace path, VCS label — plus the red-first rule. The agent writes tests and implementation **inside the claimed workspace** and returns the report files and the workspace commit; the coordinator (the claim holder) runs every `sync` — observation recording stays with the identity that holds the lease. Reject any agent report that asserts outcomes without report files; the tool would refuse them anyway.

## v1 Plans (no graph)

v1 markdown plans keep their protocol until converted (D-0022's v1 clause). The non-negotiables, with their normative homes:

- **Statuses and waves**: plan `approved/active`, phase `planned → in-progress`, tasks dispatched in dependency-ordered waves to `sdd-planner:code-implementer` agents (one clean, complete, bisectable native-SCM revision per task — a task that cannot land that way is a plan defect, not an implementation detail).
- **Evidence-gated completion** per `shared/completion-evidence.md`: no status flips to `complete` without conforming retrospective evidence; reject evidence-free success reports — a success report contains the verification commands actually run and their pasted output, never "tests should pass".
- **Per-task quality scan** (`sdd-planner:quality-scanner`, intent-blind, via `shared/templates/quality-scan-prompt.md`); max 2 review-fix cycles, then block and escalate.
- **Phase gate** per `shared/review-artifacts.md`: every task complete with evidence, clean worktree, frozen revision range, a persisted resolved frozen **Aligned** four-lane review, populated Phase Completion Evidence, and `sdd validate` passing.
- **Lifecycle bookkeeping** in separate scoped commits, never mixed into implementation revisions.

When a v1 plan keeps generating drift the evidence rules exist to catch, offer conversion instead of more discipline: `sdd graph convert --plan <Name>`. After a converted plan compiles, run the on-ramp before walking: history grants nothing, so every completed v1 task is an unverified node until observations exist — `sdd graph reverify --plan <Name> --report <suite report>` (add `--command-exit`/`--command-log` for command gates) folds one real run against every foldable node in dependency order, and the frontier then offers the genuinely remaining work instead of the already-done past.

## Escalation Rules (both modes)

Stop and ask the user when:

1. **Stopping rule fires** — 3 consecutive failures on a node (graph) or a task blocked after its one resume (v1).
2. **Spec ambiguity** — the spec/design doesn't cover an encountered case; INTENT-STALE resolutions that amount to replanning belong here too.
3. **Scope expansion** — implementation reveals work no node/task covers. Flag it; for graph plans the remedy is a new proposal payload, never silent extra work inside a claim.
4. **Destructive action** — anything deleting data, touching production config, or affecting shared systems.
5. **Plan-vs-reality mismatch** — the plan names files, APIs, or prerequisites the codebase contradicts. Planning bug; don't patch around it in dispatch.

Everything else is autonomous. Record escalation resolutions that constrain future work in the decision ledger per `shared/decision-log.md` (admission test, collision check, one-step supersession offer); pure one-off dispositions are events, not decisions.

## Output

- Graph plans: observations and claims land in `<Name>-Graph.json` through the verbs; views re-render at the next compile; code lands in workspaces and merges to the target repo's mainline.
- v1 plans: task/phase statuses, checklists, and evidence sections update in place per the v1 rules.

## Context
- Orchestration and role prompts: `shared/orchestration.md`
- Evidence rules (v1): `shared/completion-evidence.md`; review gate: `shared/review-artifacts.md`
- Hazard vocabulary: `sdd graph hazards`; state model: `sdd graph status`
- Decision ledger discipline: `shared/decision-log.md`
- Agents: `sdd-planner:code-implementer`, `sdd-planner:quality-scanner`
