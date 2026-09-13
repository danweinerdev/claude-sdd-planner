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

The loop is: **claim → red → green → sync → merge**, repeated until the frontier is empty. Every arrow is a CLI call whose refusal text names the fix. You never assert an outcome — you show the tool a report and it records what the report says (DD-5); graph completion is derived from those observations, never from narrated evidence.

### 0. Preconditions

- Plan README `status` is `approved` or `active`; flip `approved` → `active` when starting.
- `sdd graph status --plan <Name>` for the current picture: derived state counts, closure, claims. Nothing here is cached — every read derives fresh from structure plus observations.

### 1. Claim

```
sdd next --plan <Name> --claim --by <identity>
```

(the positional `Plans/<Name>` form still works but is CWD-relative). Claims the heaviest claimable frontier node (critical-path-first), records a lease, and allocates an isolated workspace (git targets: a worktree on its own branch). The printed payload carries everything needed to start — contract, the cited requirements' inlined text, named tests, hazard triage, workspace path — by design; don't re-read the plan documents for what the payload already states. Add `--node <id>` to claim that exact node instead of the frontier head — it refuses with the derived reason (not READY, claimed by someone else, a review gate, ...) if it isn't claimable. `sdd next --plan <Name> --show --by <identity>` reprints the holder's current claim payload without claiming — re-running `--claim` claims another node.

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

**Then integrate the slice into the mainline.** The merging sync completes the *claim*; the VCS integration is a separate deliberate act because it can conflict, and conflicts are judgment. On Git targets the workspace branch survives the release — `git branch --list 'graph/<id>-*'` finds the branch name (the suffix is random). After `git merge --ff-only` below, delete the branch and run `sdd graph gc` so branches and worktrees never accumulate; `sdd graph release` now deletes an empty claim branch itself. Integrate by rebase then fast-forward, never a merge commit, one node branch at a time:

1. Run `sdd doctor` in the target worktree once so the `post-rewrite` capture hook is in place.
2. `git rebase <primary>` on the node branch. Git's hook captures the old→new commit map.
3. `git merge --ff-only <node-branch>` on the primary branch.
4. `sdd graph status --plan <Name>`. **Proof is keyed on artifact digests, not on commit ids**: a node whose files came through the rebase byte-identical stays GREEN and needs nothing. Only a node whose files changed in the rebase (conflict resolution) derives STALE by digest, and only that node re-runs its gate and syncs again.
5. `sdd graph remap-revisions --plan <Name> --map <captured-map> --dry-run`, then apply with the printed `--expect-digest`, so the recorded provenance follows the rewritten commits. Do this before worktree release removes the Git-private map. Lineage is identity, never proof; it neither grants nor withdraws GREEN.

Until the bytes land on the mainline, the node honestly derives STALE from the shared tree's perspective (the recorded digests name bytes mainline doesn't have); the fast-forward self-heals it. Follow `shared/vcs-detection.md` § Git integration of parallel graph nodes for the safeguards. Integrate after every logical merge, before the next claim of dependent work.

**Commit cadence.** The workspace commit above is the only per-node commit. `<Name>-Graph.json`, rendered views, and the plan README change on every verb and are committed at phase boundaries — when a review gate greens, or the plan opens or closes — never per sync (`shared/autonomy.md` § SCM boundary cadence).

### 4. Between Rounds

- `sdd graph status --plan <Name>` between claims; `sdd graph path` when choosing what to unblock first.
- **Command gates**: run the gate's command, capture output, `sdd graph sync --node <id> --command-exit <N> --command-log out.txt`.
- **Review nodes** on the frontier (`role: review`; claimable once every reviewed dep is GREEN, like any node): claim it, run `sdd graph show <id> --plan <Name> --brief` for the self-contained brief (reviewed contracts, artifact digests, required lanes), run the four-lane review flow (`sdd review scaffold <phase-doc> --frozen <base>..<endpoint>` → `sdd review evidence set` per lane → findings via the normal write path → `sdd review resolve`), then record it:

  ```
  sdd graph review --plan <Name> --node <id> --artifact <frozen review path>
  ```

  The artifact must be `resolved` + `frozen: true` (a reopened review is not evidence), must review a document of **this plan**, and supplies evidence exactly **once** — reusing another node's artifact refuses naming it. Its verdict decides what `graph review` does: **`Aligned`** (every finding terminal) greens the node; **`Amend`** (every open finding classified `action: revise` or `extend`) is the frozen findings report that `graph amend` applies. `sdd review resolve` freezes either; it refuses a verdict of `Aligned` with open findings, an `Amend` with an unclassified open finding, and an `Amend` with nothing open. Only an `Aligned` review can complete a phase. Record the review **after** the scope's work is integrated into the mainline: the review's observation digests the aggregate reviewed set from the shared tree, and reviewing bytes that aren't there yet records an anchor of nothing.

  **With zero open findings**, the review node goes GREEN. **With any open finding**, nothing is written to the node — instead the tool prints an amendment preview (per finding: `revise` shows the node and its normative-field diff; `extend` shows the proposed node), an `expect-digest` for the graph, and an `expect-report-digest` for the review artifact. Show the user the preview before applying it, then:

  ```
  sdd graph amend --plan <Name> --node <id> --from-review <artifact path> --expect-digest <digest> --expect-report-digest <report-digest> --by <identity>
  ```

  `--dry-run` re-prints the preview without writing. A successful amend bumps `contract_rev` and clears red bookkeeping on every revised node (it owes a fresh RED before it can green again) and adds any extended node — both re-enter the frontier as ordinary work, deps of the review node. The review node stays BLOCKED behind them; walk those nodes through the usual red → green → sync cycle, then re-claim the review node and re-review. `integration-acceptance` nodes depend on review nodes, so closure requires the review to have passed on the bytes that ship.

### Stopping Rules

- **2 consecutive failures** on one node → the node is probably too big: propose `sdd graph split --plan <Name> --node <id> --file children.json` (each child one red→green cycle; the split is compile-gated and retires the parent id).
- **3 consecutive failures** → **stop and escalate to the user.** Do not grind a third variant of the same approach into the same node.

### Reaction Protocol

- **DEPENDENCY-STALE** (a direct dependency's artifact bytes differ from what this node's passing run exercised): the dependency changed underneath the proof. Re-run the gate and sync. A dependency merely re-verified against identical bytes never stales a consumer — proof is keyed on bytes, not on observation order. `SEQ-STALE` appears only for observations recorded before dependency digests existed; a re-sync replaces it.
- **INTENT-STALE** (the requirement text differs from what the passing run saw): re-read the *diff of that requirement only* — not the whole spec. Then judge: cosmetic change → re-run and sync (the new pass binds to the current text) and record the judgment with `sdd graph acknowledge --plan <Name> --node <id> --citation <ID> --expect-digest <D>`; behavioral change → rework the node; contract invalidated → replan. This is a judgment step; never auto-pick.
- **INPUT-STALE** (a declared read-only input's text differs from what the passing run saw): re-read the *diff of that input only* — the file or section the node anchored to. Judge like intent: cosmetic → re-run and `acknowledge --input <key>`; the drift is outside what the proof exercised → narrow the input to the exercised section with `sdd graph set-inputs` (allowed on a verified node when the file's bytes are unchanged since the run); behavioral → rework. `sdd next --claim` inlines each input's resolved text so you see exactly what the node read.
- **advisory** (`show` lists a citation or input under "advisory"): the text changed since the node's compile anchor but the node's latest run already saw the current text, so it is GREEN. A judgment is still owed: `sdd graph acknowledge` records it and clears the advisory. Acknowledge writes no observation and can never green a node.
- **Changing a node's declared inputs**: `sdd graph set-inputs --plan <Name> --node <id> --file inputs.json [--dry-run]` — a JSON array of `{"root", "path", "section?"}`. It resolves each input and owns the embedded `input_hashes`; eligibility is deliberately conservative (unclaimed, no red observations, and on a verified node only a narrowing of an already-declared file whose bytes the run saw unchanged), so a node with evidence is never re-pointed at different input text. Use `--dry-run` to preview; it refuses atomically on an unresolvable declaration or an ineligible node. `sdd graph set-artifacts --plan <Name> --node <id> --by <identity> --add <path> [--remove <path>]` edits the declared write set (holder-only) when a slice needs a collateral file the planner did not declare.
- **Missing fingerprints** (a node cites a fingerprintable requirement but carries no `intent_hashes` entry — e.g. children from an older `split`): `sdd graph repair-intent --plan <Name> [--node <id>] [--dry-run]` backfills only the *missing/empty* hashes on unclaimed, unverified nodes with no red observations. It never overwrites an existing hash and refuses atomically (no partial repair) on any claimed, verified, red-observed, or ambiguous/unresolved node — evidence is never re-blessed against today's text.
- **Revised or extended nodes from an amendment**: they enter the frontier like any red/new node — a revised node owes a fresh RED at its new `contract_rev` even if it keeps a test's name; an extended node starts its own red → green cycle. The review node they depend on goes GREEN again only after they do and the review is re-run — that is the system asking for re-review, not a malfunction.
- **Lease expiry / crashes**: an expired claim's workspace is preserved as post-mortem evidence. Inspect it if useful, then `sdd graph gc --plan <Name>` — gc persists the expiry and reaps the workspace; the node returns to the frontier. A stale claimant's late sync is refused by claim discipline.
- **Abandoning a node**: `sdd graph release <id> --by <identity>` — never squat on a claim you aren't working.

### Evidence Language

For graph plans, **observation records and rendered views are the completion record**. You never write completion-evidence prose as a gate input, never edit `<Name>-Graph.json` by hand (the guard denies it; every mutation goes through a verb), and never edit rendered `NN-*.md` views (the renderer overwrites them, or refuses when they are frozen). When the user asks "where are we?", the answer is `sdd graph status` / `sdd graph export --format plan` output — derived truth, not narration.

### Delegating Node Execution

Node work may be dispatched to `sdd-planner:code-implementer` agents. The dispatch carries the **claim payload verbatim** — contract, cited requirement text, named tests, hazards and the shapes their tests must take, workspace path, VCS label — plus the red-first rule. The agent writes tests and implementation **inside the claimed workspace** and returns the report files and the workspace commit; the coordinator (the claim holder) runs every `sync` — observation recording stays with the identity that holds the lease. Reject any agent report that asserts outcomes without report files; the tool would refuse them anyway.

## v1 Plans (no graph)

v1 markdown plans keep their protocol until converted. The non-negotiables, with their normative homes:

- **Statuses and waves**: plan `approved/active`, phase `planned → in-progress`, tasks dispatched in dependency-ordered waves to `sdd-planner:code-implementer` agents (one clean, complete, bisectable native-SCM revision per task — a task that cannot land that way is a plan defect, not an implementation detail).
- **Evidence-gated completion** per `shared/completion-evidence.md`: no status flips to `complete` without conforming retrospective evidence; reject evidence-free success reports — a success report contains the verification commands actually run and their pasted output, never "tests should pass".
- **Per-task quality scan** (`sdd-planner:quality-scanner`, intent-blind, via `shared/templates/quality-scan-prompt.md`); max 2 review-fix cycles, then block and escalate.
- **Phase gate** per `shared/review-artifacts.md`: every task complete with evidence, clean worktree, frozen revision range, a persisted resolved frozen **Aligned** four-lane review, populated Phase Completion Evidence, and `sdd validate` passing.
- **Lifecycle bookkeeping** at phase boundaries only (`shared/autonomy.md` § SCM boundary cadence): write statuses and evidence in flow, commit them once at phase close alongside the review and debrief — never per task, never per amendment, never mixed into implementation revisions. `sdd task complete` reports its committed-copy checks as pending until then; that is not a request to commit.

When a v1 plan keeps generating drift the evidence rules exist to catch, offer conversion instead of more discipline: `sdd graph convert --plan <Name>`. After a converted plan compiles, run the on-ramp before walking: history grants nothing, so every completed v1 task is an unverified node until observations exist — `sdd graph reverify --plan <Name> --report <suite report>` (add `--command-exit`/`--command-log` for command gates) folds one real run against every foldable node in dependency order, and the frontier then offers the genuinely remaining work instead of the already-done past. `reverify` skips fresh GREEN nodes unless `--all`; a node whose declared tests simply have not run yet is reported as "not yet run" without failing the command.

## Escalation Rules (both modes)

Stop and ask the user when:

1. **Stopping rule fires** — 3 consecutive failures on a node (graph) or a task blocked after its one resume (v1).
2. **Spec ambiguity** — the spec/design doesn't cover an encountered case; INTENT-STALE resolutions that amount to replanning belong here too.
3. **Scope expansion** — implementation reveals work no node/task covers. Flag it; for graph plans the remedy is a new proposal payload, never silent extra work inside a claim.
4. **Destructive action** — anything deleting data, touching production config, or affecting shared systems.
5. **Plan-vs-reality mismatch** — the plan names files, APIs, or prerequisites the codebase contradicts. Planning bug; don't patch around it in dispatch.

When the walk finishes (every node GREEN, acceptance closed), run `sdd decide render --plan <Name>` so the plan folder carries its generated `Design.md` before the closing commit, then run `sdd plan complete Plans/<Name>/README.md` as the closing transition — graph closure is the gate, and the renderer it reuses marks the phases complete.

Everything else is autonomous. When an escalation resolution binds work beyond the task at hand, capture it per `shared/decision-log.md`: write the statement, show it to the user verbatim, and once approved run `sdd decide add --plan <Name> --statement "..."` exactly once. A pure one-off disposition ("retry it", "skip that for now") is not a decision and stays undocumented outside the task notes.

## Output

- Graph plans: observations and claims land in `<Name>-Graph.json` through the verbs; views re-render at the next compile; code lands in workspaces and merges to the target repo's mainline.
- v1 plans: task/phase statuses, checklists, and evidence sections update in place per the v1 rules.

## Context
- Orchestration and role prompts: `shared/orchestration.md`
- Evidence rules (v1): `shared/completion-evidence.md`; review gate: `shared/review-artifacts.md`
- Hazard vocabulary: `sdd graph hazards`; state model: `sdd graph status`
- Plan decisions discipline: `shared/decision-log.md`
- Agents: `sdd-planner:code-implementer`, `sdd-planner:quality-scanner`
