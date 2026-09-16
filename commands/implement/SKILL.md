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

The loop is: **claim → red → green → sync → merge**, repeated until the frontier is empty. SDD owns graph requirements and evidence validation, not test execution: the graph-walking agent invokes repository-owned test tooling, then supplies its untouched native report to `sdd`, which validates the required tests before recording an observation. You never assert an outcome; graph completion is derived from admitted observations, never narrated evidence.

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

Before editing, compose the test skills from the already resolved active plugin root, reading each `SKILL.md` in place rather than resolving it from the claimed workspace. Read the fixed-heading design card passed from planning; if it is missing or incompatible with the current node/source, run `skills/test-design/SKILL.md` to reconstruct it from cited intent and inspected code. Challenge it with `skills/test-assess/SKILL.md` (fresh non-inheriting context when useful and available; labeled same-context assessment otherwise). Only `ready` proceeds. Then run `skills/test-generate/SKILL.md`; reconcile any changed identity or newly discovered engineering obligation through the normal proposal/amendment path rather than silently substituting tests or expanding implementation scope.

Write **all named tests first** (both package and runner-visible id in the claim payload must match exactly). All declared source artifacts must exist before running them: add the minimum callable scaffolding needed to reach an actual failing assertion, not the intended implementation. A missing file or package build failure is not qualifying RED. Run the repository-owned tests and preserve their native report bytes. When any selected test declares `package`, supply Go test JSON: sync uses the strict package-qualified parser and rejects incomplete, ambiguous, or mismatched package/test execution. When the runner emits Go test JSON and the gate has package-qualified tests, pass its captured real process exit with `--report-exit <N>`; never invent an exit code. Assess the raw report and distinguish intended assertion failure from discovery, setup, build, timeout, or capture failure.

Sync the usable failing report, classifying it explicitly:

```
sdd graph sync --plan <Name> --node <id> --by <identity> --report red.json --report-exit <N> --red-kind baseline
# or, for an isolated sensitivity run:
sdd graph sync --plan <Name> --node <id> --by <identity> --report red.json --report-exit <N> --red-kind sensitivity --fault <injected-fault>
```

A red run is a **successful** sync — recording the failure is the point. `red_seqs` retains the first failing sequence for each test id, which arms red-before-green: a hazard-discharging test that has never been observed failing will refuse the later green ("a test that passes against both correct and broken code guards nothing"). Submitting identical report bytes again for the same node is a no-op and does not advance sequence.

### 3. Green — implement, commit, sync the pass

Implement inside the workspace until the named tests pass. Do not weaken assertions, add implementation outside the node contract, or hardcode the generated cases. Run the selected tests against the final bytes and assess the untouched native report; setup/capture failure is not GREEN. Commit those same tested bytes without modifying them, then have the coordinator sync the report—do not duplicate the run merely to attach a commit:

```
git commit <the complete tested slice using the repository's normal non-interactive workflow>
sdd graph sync --plan <Name> --node <id> --by <identity> --report <report.json> [--report-exit <captured-N>]
```

A passing sync for a claimed workspace requires that workspace to be clean. It records a new observation sequence with the node's `contract_rev`, result, isolation, report identity, and VCS provenance; then it clears the claim and releases the workspace. A shared-dirty pass is provisional and derives STALE until the walker deliberately produces and syncs a clean run. There is no assert path: `--command-exit` needs a real exit code, and asserted isolation is refused by default.

**Then integrate the slice into the mainline.** The merging sync completes the *claim*; the VCS integration is a separate deliberate act because it can conflict, and conflicts are judgment. On Git targets the workspace branch survives the release — `git branch --list 'graph/<id>-*'` finds the branch name (the suffix is random). After `git merge --ff-only` below, delete the branch and run `sdd graph gc` so branches and worktrees never accumulate; `sdd graph release` now deletes an empty claim branch itself. Integrate by rebase then fast-forward, never a merge commit, one node branch at a time:

1. Run `sdd doctor` in the target worktree once so the `post-rewrite` capture hook is in place.
2. `git rebase <primary>` on the node branch. Git's hook captures the old→new commit map.
3. `git merge --ff-only <node-branch>` on the primary branch.
4. `sdd graph status --plan <Name>`. Rebase and fast-forward do not change graph state, even when conflict resolution changes files. Nothing is re-verified unless the walker deliberately runs the gate and syncs a new observation.
5. `sdd graph remap-revisions --plan <Name> --map <captured-map> --dry-run`, then apply with the printed `--expect-digest`, so the recorded provenance follows the rewritten commits. Do this before worktree release removes the Git-private map. Lineage is identity, never proof; it neither grants nor withdraws GREEN.

Follow `shared/vcs-detection.md` § Git integration of parallel graph nodes for the safeguards. Integrate after every logical merge, before the next claim of dependent work. Integration itself neither grants nor withdraws GREEN.

**Commit cadence.** The workspace commit above is the only per-node commit. `<Name>-Graph.json`, rendered views, and the plan README change on every verb and are committed at phase boundaries — when a review gate greens, or the plan opens or closes — never per sync (`shared/autonomy.md` § SCM boundary cadence).

### 4. Between Rounds

- `sdd graph status --plan <Name>` between claims; `sdd graph path` when choosing what to unblock first.
- **Command gates**: run the gate's command, capture output, `sdd graph sync --node <id> --command-exit <N> --command-log out.txt`.
- **Review nodes** on the frontier (`role: review`; claimable once every reviewed dep is GREEN, like any node): claim it, run `sdd graph show <id> --plan <Name> --brief` for the self-contained brief (reviewed contracts, declared artifacts, required lanes), run the four-lane review flow (`sdd review scaffold <phase-doc> --frozen <base>..<endpoint>` → `sdd review evidence set` per lane → findings via the normal write path → `sdd review resolve`), then record it:

  ```
  sdd graph review --plan <Name> --node <id> --artifact <frozen review path>
  ```

  The artifact must be `resolved` + `frozen: true` (a reopened review is not evidence), must review a document of **this plan**, and supplies evidence exactly **once** — reusing another node's artifact refuses naming it. Its verdict decides what `graph review` does: **`Aligned`** (every finding terminal) greens the node; **`Amend`** (every open finding classified `action: revise` or `extend`) is the frozen findings report that `graph amend` applies. `sdd review resolve` freezes either; it refuses a verdict of `Aligned` with open findings, an `Amend` with an unclassified open finding, and an `Amend` with nothing open. When a lane found material worth an Amend rather than a pass, record that truthfully with `sdd review evidence set --result changes-required` — a lane's result never has to falsely claim `PASS/Aligned` to make the review resolvable. Only an `Aligned` review can complete a phase. Record the review **after** the scope's work is integrated into the mainline. Its observation binds each reviewed node's `contract_rev` and observation sequence; a scope change, revision advance, or newer reviewed observation makes the review stale and requires a deliberate new review.

  **With zero open findings**, the review node goes GREEN. **With any open finding**, nothing is written to the node — instead the tool prints an amendment preview (per finding: `revise` shows the node and its normative-field diff; `extend` shows the proposed node), an `expect-digest` for the graph, and an `expect-report-digest` for the review artifact. Show the user the preview before applying it, then:

  ```
  sdd graph amend --plan <Name> --node <id> --from-review <artifact path> --expect-digest <digest> --expect-report-digest <report-digest> --by <identity>
  ```

  `--dry-run` re-prints the preview without writing. A successful revise advances `contract_rev` and clears that node's `red_seqs`; any hazard-discharging test must therefore be observed failing again before a later pass can count. Extended nodes likewise need their intended RED before GREEN. Revised and extended nodes re-enter the frontier as ordinary work, deps of the review node. Walk them through red → green → sync, then deliberately re-claim and re-review the stale review node. `integration-acceptance` nodes depend on review nodes, so closure requires current review observations.

### Stopping Rules

- **2 consecutive failures** on one node → the node is probably too big: propose `sdd graph split --plan <Name> --node <id> --file children.json` (each child one red→green cycle; the split is compile-gated and retires the parent id).
- **3 consecutive failures** → **stop and escalate to the user.** Do not grind a third variant of the same approach into the same node.

### Reaction Protocol

- **Newer direct dependency observation:** A new observation on a node stales only its DIRECT consumers whose pass predates it; it does not recursively stale their descendants. No file or declaration edit causes staleness. Re-run a stale consumer's gate when you need current proof for it (before claiming work that depends on it, and before a review/closure) — the walker decides.
- **Contract revision advanced:** the node's earlier observation no longer proves its current contract. Re-establish any required RED, implement the revised obligation, and sync a new pass at the current `contract_rev`.
- **Isolation stale:** a shared-dirty passing observation is not GREEN. Produce a clean claimed-workspace run and sync it.
- **Review stale:** a reviewed scope change, reviewed `contract_rev` advance, or newer reviewed observation invalidates the old review binding. Run the review lanes again and record a new review observation.
- **Changing declarations:** inputs and artifacts are review-visible declarations, not freshness keys. `sdd graph set-inputs --plan <Name> --node <id> --file inputs.json [--dry-run]` replaces inputs on an unclaimed node: on a verified node it advances `contract_rev` and keeps compatible `red_seqs`; an unverified node with red observations is refused. `sdd graph set-artifacts --plan <Name> --node <id> --file artifacts.json` replaces the artifact set, while `--add <path> [--remove <path>]` updates it incrementally; add `--by <identity>` when the node is claimed, because only its holder may edit it then. It re-renders views unless `--no-render`. Neither command substitutes for deliberate test execution.
- **Revised or extended nodes from an amendment:** revise clears `red_seqs`; new or revised hazard-discharging tests need an actual intended failure before GREEN can count. The review node goes GREEN again only after its dependencies do and a reviewer deliberately records a new review.
- **Lease expiry / crashes**: an expired claim's workspace is preserved as post-mortem evidence. Inspect it if useful, then `sdd graph gc --plan <Name>` — gc persists the expiry and reaps the workspace; the node returns to the frontier. A stale claimant's late sync is refused by claim discipline.
- **Abandoning a node**: `sdd graph release <id> --by <identity>` — never squat on a claim you aren't working.

Digests appearing in amend/apply flags are compare-and-swap write fences, never freshness evidence.

### Evidence Language

For graph plans, **observation records and rendered views are the completion record**. You never write completion-evidence prose as a gate input, never edit `<Name>-Graph.json` by hand (the guard denies it; every mutation goes through a verb), and never edit rendered `NN-*.md` views (the renderer overwrites them, or refuses when they are frozen). When the user asks "where are we?", the answer is `sdd graph status` / `sdd graph export --format plan` output — derived truth, not narration.

### Delegating Node Execution

Node work may be dispatched to `sdd-planner:code-implementer` agents under the unchanged `implement_task` routing. The dispatch carries the **claim payload verbatim** — contract, cited requirement text, package-qualified named tests, hazards and the shapes their tests must take, workspace path, VCS label — plus the assessed design card and red-first rule. The agent composes test generation, invokes repository-owned tooling, implements, and assesses untouched native reports **inside the claimed workspace**. It returns generated-test findings, RED/GREEN reports, assessment, and the workspace commit. The coordinator (the claim holder) runs every `sync`; graph writes and admission stay with the lease holder. Reject asserted outcomes or model-created execution facts.

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
2. **Spec ambiguity** — the spec/design doesn't cover an encountered case; reconciliation that changes the obligation belongs here too.
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
- Test composition: `skills/test-design/SKILL.md`, `skills/test-generate/SKILL.md`, `skills/test-assess/SKILL.md`; quality standard: `docs/TDD-TEST-DESIGN.md`
- Evidence rules (v1): `shared/completion-evidence.md`; review gate: `shared/review-artifacts.md`
- Hazard vocabulary: `sdd graph hazards`; state model: `sdd graph status`
- Plan decisions discipline: `shared/decision-log.md`
- Agents: `sdd-planner:code-implementer`, `sdd-planner:quality-scanner`
