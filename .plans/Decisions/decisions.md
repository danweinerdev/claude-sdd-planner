---
title: "Decision Ledger"
type: decision-log
status: active
created: 2026-07-13
updated: 2026-08-31
tags: [decisions]
related: [Research/decision-log.md]
decisions:
  - id: D-0001
    kind: decision
    status: superseded
    superseded_by: D-0004
    date: 2026-07-13
    decided_by: user
    statement: "User decisions are tracked as durable truth in a single canonical ledger, Decisions/decisions.md, with a machine-readable decisions[] frontmatter array."
    rejected: [one-MADR-file-per-decision as the primary store, per-plan ledgers, MCP-server-backed store]
    rationale: "One file to read/grep suits LLM consumption; the decisions[] array follows the existing phases[]/tasks[] frontmatter-as-machine-layer convention; decisions like concept definitions cross plans, so the ledger is global with per-entry scope[] filtering."
    scope: []
    tags: [decision-log, architecture]
    reversibility: two-way
  - id: D-0002
    kind: decision
    status: accepted
    date: 2026-07-13
    decided_by: user
    statement: "The decision log ships as three layers: a shared convention doc (shared/decision-log.md) wired into the lifecycle skills' capture points, a model-only skill (skills/decision-log/) for ad-hoc conversational decisions, and a /sdd-planner:decide command for manual record/lookup/reconcile."
    rejected: [hook-based capture, capture via an MCP server]
    rationale: "Claude Code has no hook that fires when a user answers a question (verified against docs 2026-07-12), so capture must be behavioral — matching how the plugin already enforces its contracts."
    scope: []
    tags: [decision-log, architecture, claude-code]
    reversibility: two-way
  - id: D-0003
    kind: decision
    status: accepted
    date: 2026-07-13
    decided_by: user
    statement: "A new decision that contradicts or supersedes an accepted ledger entry always stops for user reconciliation — collisions are never auto-resolved and never settled by recency."
    rejected: [precedence-based silent resolution, recency-wins]
    rationale: "Every surveyed tool that resolves conflicts silently hides real contradictions; the ADR tradition insists the human owns supersession."
    scope: []
    tags: [decision-log, collision-detection]
    reversibility: one-way
  - id: D-0004
    kind: decision
    status: accepted
    date: 2026-07-13
    decided_by: user
    supersedes: D-0001
    statement: "Decisions live with the repo they represent: the ledger is <planning-root>/Decisions/decisions.md when the planning root is inside the repo, and <repo-root>/DECISIONS.md when the planning root is external — there is no cross-repo global ledger."
    rejected: [cross-repo global ledger in an external planning root, per-repo scoping tags inside one shared ledger]
    rationale: "Each repo's truths stay versioned with its code; one repo's decisions never bleed into another. Supersedes D-0001's single-canonical-ledger wording; the decisions[] format and in-repo common case are unchanged."
    scope: []
    tags: [decision-log, architecture, multi-repo]
    reversibility: two-way
  - id: D-0005
    kind: decision
    status: superseded
    superseded_by: D-0013
    date: 2026-07-13
    decided_by: user
    statement: "The plugin ships a SessionStart hook in v1 (hooks/hooks.json + hooks/load-decisions.sh) that injects accepted ledger entries as additionalContext at session start."
    rejected: [deferring the hook until behavioral capture proves out]
    rationale: "Guarantees recall of accepted decisions even in sessions that skip onboarding, at a small per-session context cost; the script is a silent no-op when no ledger exists."
    scope: []
    tags: [decision-log, hooks, claude-code]
    reversibility: two-way
  - id: D-0006
    kind: decision
    status: accepted
    date: 2026-07-13
    decided_by: user
    statement: "The sdd-dashboard companion plugin gets no decisions view for now; /decide list and search cover lookup needs."
    rejected: [dashboard decisions panel in v1]
    rationale: "The ledger is small and text-first; revisit if ledgers grow or users ask for visual browsing."
    scope: []
    tags: [decision-log, dashboard]
    reversibility: two-way
  - id: D-0007
    kind: decision
    status: accepted
    date: 2026-07-23
    decided_by: user
    statement: "The plugin carries a compact core of 12 lifecycle skills; /diagram, /excavate, /retro, /simplify, and /tend are cut, retro and diagram artifact types are read-only legacy, and /tend's decision hygiene lives in /decide check."
    rejected: [keeping the full 16-skill roster, moving cut skills to an optional module]
    rationale: "The user's sharpened runtime-neutral fork (~/.agents/plugins/sdd-planner) proved the cut skills added surface without proportional value; both plugins should carry the same compact core."
    scope: []
    tags: [skills, compact-core]
    reversibility: two-way
  - id: D-0008
    kind: decision
    status: superseded
    date: 2026-07-24
    decided_by: user
    statement: "Completion is evidence-gated at task, phase, and plan level per shared/completion-evidence.md: each plan task is one clean, independently bisectable native-SCM revision; retrospective evidence with exact commands and revision identity must be populated before any status flips to complete; phase completion requires a persisted frozen four-lane Aligned review; lifecycle bookkeeping lands in separate scoped commits."
    rejected: [status flips on assertion, evidence collected only at phase level, mixing lifecycle bookkeeping into implementation commits]
    rationale: "Ported from the sharpened fork's evidence pass: prospective verification says how work will be judged, retrospective evidence records what actually ran — without the gate, complete is a claim, not a fact."
    scope: []
    tags: [completion-evidence, implement, code-review]
    reversibility: two-way
    superseded_by: D-0022
  - id: D-0009
    kind: decision
    status: superseded
    superseded_by: D-0012
    date: 2026-07-24
    decided_by: user
    statement: "Deterministic validation ships as scripts/sdd_validate.py and scripts/sdd_decision_validate.py, copied verbatim from the sharpened fork and surfaced as /validate; review-artifact frontmatter uses the stable lane identifiers (review_plan_drift, review_quality, review_spec_compliance, review_blind_spots) mapped to the four reviewer agents, so the validator runs unmodified."
    rejected: [forking the validator to use agent names in lane_results, model-only validation without a script]
    rationale: "Keeping the scripts byte-identical to the fork means fixes flow between the two plugins without divergence; the stable identifiers decouple the data layer from whichever agent or runtime executes a lane."
    scope: []
    tags: [validate, review-lanes, scripts]
    reversibility: two-way
  - id: D-0010
    kind: decision
    status: superseded
    date: 2026-07-24
    decided_by: user
    statement: "sdd-planner does not integrate beads (sdd-beads) for issue tracking; plan/phase/task frontmatter remains the only work-tracking layer."
    rejected: [sdd-beads integration]
    rationale: "The user tried the integration in the sharpened fork and removed it — it added more complexity than it helped."
    scope: []
    tags: [tracking, integrations]
    reversibility: two-way
    superseded_by: D-0023
  - id: D-0011
    kind: decision
    status: superseded
    superseded_by: D-0014
    date: 2026-07-24
    decided_by: user
    statement: "The read-only guarantee of the seven reviewer/researcher agents is enforced mechanically by a PreToolUse hook (hooks/reviewer-bash-guard.py) that allowlists read-only git/p4 subcommands and denies write- or network-shaped Bash; it fails open for all other agents and covers only plugin-owned agents, not project review lanes."
    rejected: [prompt-only behavioral guarantee, removing Bash from reviewer allowlists, per-agent Bash(pattern) frontmatter restrictions]
    rationale: "Reviewers need a shell for diffs and test runs, agent tools: frontmatter cannot express argument patterns, and a prompt-level read-only promise is not a permission boundary; the hook closes the gap without breaking the lanes' validation duties. Defense-in-depth against sloppiness, not adversarial sandboxing."
    scope: []
    tags: [agents, hooks, security, review-lanes]
    reversibility: two-way
  - id: D-0012
    kind: decision
    status: accepted
    date: 2026-08-03
    decided_by: user
    supersedes: D-0009
    statement: "Deterministic validation moves to the Go sdd tool. scripts/sdd_validate.py and scripts/sdd_decision_validate.py are deprecated on arrival of the port, retained only as the parity oracle, and deleted once sdd reaches proven parity. The stable review lane identifiers (review_plan_drift, review_quality, review_spec_compliance, review_blind_spots) from D-0009 remain in force unchanged."
    rejected: [keeping the Python validators as a shipped runtime fallback, rewriting without a differential parity gate, deleting the scripts before parity is proven]
    rationale: "Python plus PyYAML is a hard runtime dependency across six skill files and the PreToolUse hook, with a documented degradation path when it is missing; a static binary deletes that failure mode rather than mitigating it. Accepted cost: this permanently severs D-0009's fork-sync channel, so validator fixes no longer flow byte-identically between this plugin and the sharpened fork. Deletion is gated on the parity corpus rather than on a date."
    scope: [Specs/SDD-Toolchain]
    tags: [validation, golang, migration, tooling]
    reversibility: one-way
  - id: D-0013
    kind: decision
    status: accepted
    date: 2026-08-03
    decided_by: user
    supersedes: D-0005
    statement: "SessionStart ledger injection moves from hooks/load-decisions.sh into the Go binary as `sdd hook sessionstart`, removing the plugin's POSIX-shell dependency. It is a silent no-op in two cases: when no ledger exists, and when no sdd binary is yet provisioned."
    rejected: [keeping the shell script alongside the binary, making the hook a hard error when unprovisioned]
    rationale: "The shell script never worked on native Windows, so moving it into the binary fixes an existing cross-platform gap rather than only consolidating code. The second no-op case is new and load-bearing: because the binary is built at /setup, a fresh install has no binary, and a hook that errored there would break the session it is meant to enrich. Failing open silently is the correct trade for a context-enrichment hook."
    scope: [Specs/SDD-Toolchain]
    tags: [decision-log, hooks, golang, cross-platform]
    reversibility: two-way
  - id: D-0014
    kind: decision
    status: accepted
    date: 2026-08-03
    decided_by: user
    supersedes: D-0011
    statement: "The reviewer read-only guard moves into the Go binary as `sdd hook pretooluse`, keeping the git/p4 read-only allowlist and fail-open-for-everyone-else behavior, and gains two duties: denying Write/Edit on schema-recognized artifact paths, and allowlisting sdd's own subcommands so the read-only agents may run sdd validate/show/list/next/version/doctor but never apply, section set, decide add, evidence add, or any lifecycle transition."
    rejected: [porting the guard without covering sdd's own mutating subcommands, accepting the sdd-mutation path as an acceptable weakening, denying sdd entirely to reviewer agents]
    rationale: "Introducing a mutating CLI while the guard denies only an enumerated set of command heads would have handed the read-only agents a sanctioned path to rewrite planning artifacts — a larger hole than the Write/Edit denial closes. Allowlisting sdd subcommands the same way git subcommands are already allowlisted keeps the guarantee net-stronger than D-0011's. One residual weakening is accepted and unavoidable: because the binary is built at /setup, no guard runs at all until the plugin is provisioned, where previously it was active from the first session. Still defense-in-depth against sloppiness, not adversarial sandboxing."
    scope: [Specs/SDD-Toolchain]
    tags: [agents, hooks, security, golang, review-lanes]
    reversibility: two-way
  - id: D-0015
    kind: decision
    status: superseded
    superseded_by: D-0021
    date: 2026-08-03
    decided_by: user
    statement: "The sdd binary is provisioned exclusively by the user running `go install github.com/danweinerdev/claude-sdd-planner/cmd/sdd@v<version>` before the plugin is invoked. The plugin never ships prebuilt binaries, never compiles, and never invokes go; /setup only verifies the binary and copies it to the plugin root for hooks.json. Admission is by a minSddVersion floor declared in plugin.json — not exact version equality — and that floor is advanced deliberately, never by `make bump-*`."
    rejected: [committing prebuilt per-platform binaries to the plugin payload, building from source during /setup, exact plugin-version/binary-version lockstep, /setup installing a Go toolchain via package manager]
    rationale: "Prebuilt bundling would add roughly 50 MB of incompressible blobs to repository history per release, since Claude Code installs plugins by cloning. Building at /setup removed that cost but made the plugin responsible for toolchain detection, module fetching, and network failure modes it cannot handle well. Delegating to `go install` puts those failures in the user's hands with Go's own diagnostics. The floor rather than lockstep is load-bearing: `go install` yields one unversioned binary per machine, so equality would break every working install on a wording-only plugin release and would prevent two plugin versions from coexisting. The plugin-root copy survives regardless, because hooks.json can interpolate only CLAUDE_PLUGIN_ROOT and hook processes often run without $GOBIN on PATH."
    scope: [Specs/SDD-Toolchain]
    tags: [golang, distribution, versioning, setup, hooks]
    reversibility: two-way
  - id: D-0016
    kind: decision
    status: superseded
    date: 2026-08-12
    decided_by: user-approved
    statement: "The plugin ships from one repository: the repo root is the canonical hand-edited Claude plugin, and portable/ is a generated OpenCode/Codex tree produced by 'sdd plugin sync' (transforms + .portable.md variants + portable-overrides/), drift- and leak-gated by make test, with version/minSddVersion synced from .claude-plugin/plugin.json. The standalone sdd-planner repo is retired."
    rejected: [keep two repos in manual sync, author a harness-neutral core tree and generate both plugins from it, bash cp-fanout mirrors like code-graph-mcp]
    rationale: "Two hand-synced repos duplicated every content change and had already drifted (Python-era skills vs Go binary). Generation from one canonical tree keeps the harness-specific mechanics (Task dispatch vs collaboration-subagent idiom) while making divergence a build failure instead of a maintenance task."
    scope: [portable/, internal/portable, Makefile]
    tags: [architecture, portable, plugin, release]
    reversibility: two-way
    superseded_by: D-0017
  - id: D-0017
    kind: decision
    status: accepted
    supersedes: D-0016
    date: 2026-08-13
    decided_by: user-approved
    statement: "The plugin ships from one repository: the repo root is the canonical hand-edited Claude plugin, and .codex-plugin/ and .opencode-plugin/ are generated per-harness install trees produced by 'sdd plugin sync' (transforms + markers + .portable.md variants + portable-overrides/), drift- and leak-gated by make test, with version/minSddVersion synced from .claude-plugin/plugin.json. The standalone sdd-planner repo is retired."
    rejected: [edit D-0016's scope in place, leave the stale portable/ scope and waive the diagnostic]
    rationale: "Supersedes D-0016 to correct its scope: the generated tree was published as portable/ when D-0016 was recorded, and is now emitted as the two per-harness roots .codex-plugin/ and .opencode-plugin/, which is what Codex marketplaces and OpenCode .agents discovery actually install from. D-0016's scope names a path that no longer exists, and an accepted entry's scope is immutable, so the correction is a superseding entry rather than an edit."
    scope: [internal/portable, Makefile]
    tags: [architecture, portable, plugin, release]
    reversibility: two-way
  - id: D-0018
    kind: decision
    status: accepted
    date: 2026-08-13
    decided_by: user
    statement: "No LLM edits a decision ledger, for any reason, without the user's explicit approval of the exact, unmodified text of the change, shown in full beforehand — writes on assumption or non-objection are forbidden."
    rejected: [Autonomous append of collision-free entries after the user decides]
    rationale: "The ledger is durable truth; an unapproved or paraphrased write silently forks it. The user, not the agent, owns both the decision and the record of it."
    confirmation: "shared/decision-log.md carries the write-gate callout and shared/autonomy.md lists ledger writes under 'Stops for the user'."
    tags: [decision-ledger, autonomy, approval]
    reversibility: two-way
  - id: D-0019
    kind: decision
    status: accepted
    date: 2026-08-16
    decided_by: user-approved
    statement: "SDD lifecycle records commit once per affected root at closeout boundaries: commit the verified feature slice first, then record planning artifacts and ledgers in one boundary commit per root at task closeout, at the end of a contiguous spec/design/plan session, or at phase/plan close — never per edit and never as intermediate SDD bookkeeping commits. Artifacts are still written in-flow as facts become known; writing never implies an immediate commit."
    rejected: []
    rationale: "Backfill: the plugin's skills and templates have cited D-0019 for this rule since it was adopted, but the entry was never recorded in the ledger. Recording it under the cited id repairs the dangling references without re-binding them."
    scope: []
    tags: []
    reversibility: two-way
  - id: D-0020
    kind: decision
    status: accepted
    date: 2026-08-16
    decided_by: user-approved
    statement: "Phase-gate reviews freeze at resolution, never at creation: sdd review scaffold writes frozen: false + status: open; only sdd review resolve sets frozen: true and status: resolved, atomically, after verifying the SDD167 gate. A frozen review is immutable through every supported command; post-resolution changes supersede it with a fresh scaffolded review."
    rejected: []
    rationale: "Freezing at scaffold time made status: open permanently unresolvable — SPK050 blocked every write path and no transition verb existed, so a valid final review could never close its phase without a forbidden hand edit."
    scope: []
    tags: []
    reversibility: two-way
  - id: D-0021
    kind: decision
    status: accepted
    date: 2026-08-17
    decided_by: user
    supersedes: D-0015
    statement: "The sdd binary is provisioned exclusively by the user running `go install github.com/danweinerdev/claude-sdd-planner/v2/cmd/sdd@v<version>` before the plugin is invoked. The plugin never ships prebuilt binaries, never compiles, and never invokes go; /setup only verifies the binary and copies it to the plugin root for hooks.json. Admission is by a minSddVersion floor declared in plugin.json — not exact version equality — and that floor is advanced deliberately, never by `make bump-*`."
    rejected: [keeping the unversioned Go module path after v2.0.0, committing prebuilt per-platform binaries to the plugin payload, building from source during /setup, exact plugin-version/binary-version lockstep, /setup installing a Go toolchain via package manager]
    rationale: "Go semantic import versioning rejects v2+ tags whose go.mod module path lacks the /v2 suffix, causing @latest to resolve v1.16.0 from before cmd/sdd existed. The /v2 module path aligns Go's tag resolution with the binary's 2.x release line while retaining user-run go install as the sole provisioning mechanism."
    confirmation: "A released v2 tag resolves with `go list -m github.com/danweinerdev/claude-sdd-planner/v2@v<version>`, and every in-repo Go import and install instruction uses the /v2 path."
    scope: [go.mod, README.md, AGENTS.md, CLAUDE.md, commands/setup/SKILL.portable.md, skills/sdd-cli/SKILL.md, internal/provision/provision.go]
    tags: [golang, distribution, versioning, setup, modules]
    reversibility: two-way
  - id: D-0022
    kind: decision
    status: accepted
    supersedes: D-0008
    date: 2026-08-31
    decided_by: user-approved
    statement: "For graph-executed plans per Designs/SddGraph, completion is observation-gated: node state is derived on read from structure plus recorded observations, and the only path to GREEN is a mechanical artifact (parsed test report, command exit/output, or frozen review artifact) synced through the sdd binary — never narrated evidence. A node's GREEN is assumed closure, sufficient for dependants to build on; completion-grade closure is the derived predicate 'GREEN and covered by a GREEN frozen full review gate whose aggregate diff digest still matches.' The frozen Aligned four-lane review obligation is preserved but scoped to feature review gates rather than phase cuts; phase completion is the projection 'every node in the phase is closed'; review findings naming scope nodes demote them to RED mechanically. Bisectability is per-VCS: git — one node = one clean revision anchored in its observation; p4 — one plan/phase = one changelist, with per-node history in observation records; plain trees anchor by digest only. v1 markdown plans not yet converted remain governed by shared/completion-evidence.md's evidence-table discipline until conversion."
    rejected: [status flips on assertion, narrated completion evidence as the gate for graph plans, full four-lane review per phase cut at graph granularity, full review per node, stored status fields in the graph]
    rationale: "Approved via Designs/SddGraph (DD-3, DD-5, DD-9). Observation-gated closure removes every write path by which an LLM concludes; two-axis closure (assumed vs. closed) keeps flow while review weight attaches to feature integrators proportional to integration risk. Supersedes D-0008's letter on status flips, per-task revision granularity, and phase-scoped review; preserves its intent that completion is a fact, not a claim."
    scope: []
    tags: [completion-evidence, execution, graph, review]
    reversibility: two-way
  - id: D-0023
    kind: decision
    status: accepted
    supersedes: D-0010
    date: 2026-08-31
    decided_by: user-approved
    statement: "Planning-root artifacts are the only work-tracking layer: plan/phase/task frontmatter for v1 markdown plans, and the committed plan graph (Plans/<Plan>/<Plan>-Graph.json per Designs/SddGraph) for graph-executed plans. No external issue tracker is integrated."
    rejected: [external issue-tracker integration (sdd-beads), graph or work-tracking state stored outside the planning root]
    rationale: "Narrows D-0010 to its intent: the beads rejection was about external trackers adding complexity, not about in-repo planning artifacts. The graph is a committed planning-root artifact versioned with the plan it schedules."
    scope: []
    tags: [tracking, integrations, graph]
    reversibility: two-way
---









# Decision Ledger

Machine-readable record of decided truths — design choices, concept definitions, answered design questions. The frontmatter `decisions[]` array is canonical; see `shared/decision-log.md` in the plugin for the entry schema, lifecycle rules, and collision procedure.

Entries are append-only: an accepted entry is never edited except to mark it superseded. A change of mind is a new entry that supersedes the old one.
