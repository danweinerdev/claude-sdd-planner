# SDD Planner

A Claude Code plugin for spec-driven development — structured project planning with lifecycle skills and review agents. The optional HTML dashboard lives in a companion plugin: [sdd-dashboard](https://github.com/danweinerdev/sdd-dashboard-plugin).

## Directory Structure

```
sdd-planner/                      # Repository root = plugin root
├── .claude-plugin/
│   └── plugin.json               # Plugin manifest (name: "sdd-planner")
├── CLAUDE.md                     # This file
├── hooks/
│   ├── hooks.json                # Plugin hooks — SessionStart decision-ledger injection
│   └── load-decisions.sh         # Emits accepted ledger entries as additionalContext
├── Makefile                      # make bump-patch / bump-minor / bump-major
├── planning-config.json          # Planning configuration
├── .gitignore
├── commands/                     # Slash commands (auto-namespaced /sdd-planner:*); each is a dir with SKILL.md inside
├── skills/                       # Model-only reference skills (auto-loaded by description, not /-invocable)
│   └── <lang>-specifications/    # Per-language structural verification (cpp, rust, go, python, typescript, java, swift)
├── agents/                       # Subagent definitions
├── scripts/                      # Deterministic validators (read-only; PyYAML per requirements.txt)
│   ├── sdd_validate.py           # Full artifact validator — structure, graph, evidence, ledger
│   └── sdd_decision_validate.py  # Focused decision-ledger validator
├── requirements.txt              # Python deps for scripts/ (PyYAML)
├── shared/
│   ├── frontmatter-schema.md     # Single source of truth for artifact metadata
│   ├── completion-evidence.md    # Evidence-gated completion — what `complete` requires, per level
│   ├── decision-log.md           # Decision ledger — entry schema, capture triggers, collision procedure
│   ├── path-resolution.md        # Canonical planning-root / plugin-dir / target-repo resolution
│   ├── vcs-detection.md          # VCS detection algorithm + operations table (git / p4 / plain)
│   ├── orchestration.md          # Orchestration model, session onboarding, post-compaction re-reads
│   ├── autonomy.md               # Cross-skill autonomy table — what runs solo vs stops for the user
│   ├── decision-framework.md     # Universal decision discipline for all skills and agents, any model
│   ├── review-lanes.md           # Project-supplied review-lane socket convention
│   ├── language-verification.md  # Language-specific verification — what good looks like
│   ├── languages/                # Per-language verification references
│   └── templates/                # Document templates
├── Research/                     # Research artifacts (flat)
├── Brainstorm/                   # Brainstorm artifacts (flat)
├── Specs/                        # Specs (subdirectory per feature)
│   └── <feature>/README.md
├── Designs/                      # Designs (subdirectory per component)
│   └── <component>/README.md
├── Plans/                        # Implementation plans (flat — status lives in frontmatter)
│   └── <PlanName>/
│       ├── README.md             # Frontmatter with status, phases[], overview
│       ├── 01-Phase-Name.md      # Frontmatter with tasks[], details
│       └── notes/                # After-action notes
│           └── 01-Phase-Name.md  # Debrief for Phase 1
└── Decisions/                    # Decision ledger (single canonical file)
    └── decisions.md              # decisions[] frontmatter array — see shared/decision-log.md
```

## Conventions

### Frontmatter
All artifacts use YAML frontmatter as the machine-readable data layer. See `shared/frontmatter-schema.md` for the complete schema. The companion `sdd-dashboard` plugin reads exclusively from frontmatter — no markdown table parsing.

### Sensitive data
Artifacts never capture credentials, private hostnames/IPs, usernames, or machine-specific absolute paths (`/home/<user>/...`, `C:\Users\...`). Paths are repo/planning-root-relative by default; `~/...`/`$HOME/...` are acceptable generic forms where a path outside the root is needed; scrub pasted command output. Single source of truth: `shared/frontmatter-schema.md` § Sensitive Data.

### VCS-agnostic operations
The plugin works with git, git worktrees, Perforce, and unversioned directories. Skills that inspect files or history detect the VCS first using the algorithm in `shared/vcs-detection.md`, then use the corresponding command from that file's operations table (`git mv` / `p4 move` / plain `mv`, `git diff` / `p4 diff2`, etc.). Don't hard-code `git` in skills. Likewise, path resolution (planning root, plugin directory, target repo) is defined once in `shared/path-resolution.md` — the single source of truth; don't re-derive it in skills.

### Decision framework
Every context this plugin runs in — primary-context skills and all agents, on any model — follows the universal decision discipline in `shared/decision-framework.md` (premise checks before complying, run-to-verify for commandable claims, documented searches behind absence claims, verbatim failure reporting, no downscoping by imagined effort). Each agent embeds the framework's canonical digest block verbatim as a `## Decision Framework` section; `shared/orchestration.md` binds the primary context. When editing the framework, re-sync the embedded block in every agent.

### Decision Ledger
User decisions — design choices, concept definitions, answered design questions — are recorded as durable truth in a `decision-log` artifact (`decisions[]` frontmatter array): `Decisions/decisions.md` under the planning root when the root is inside the repo, or the target repo's own `DECISIONS.md` when the planning root is external — decisions live with the repo they represent, never in a cross-repo global ledger. `shared/decision-log.md` is the single source of truth: entry schema, capture triggers, the append-time collision check (a contradiction with an `accepted` entry always stops for the user — never auto-resolve), and distribution rules (the ledger is intent context: researcher/plan-reviewer/spec-reviewer yes; quality-scanner/blind-spot-finder never). Not to be confused with the decision *framework* above, which is the reasoning discipline.

### Plan Hierarchy
```
Plan (README.md)       <- like a Jira Project
 └── Phase (01-*.md)   <- like a Jira Epic
      └── Task          <- defined in phase frontmatter
           └── Subtask  <- checklist items in body
```

### Status Values
| Level | Statuses |
|-------|----------|
| plan | `draft`, `approved`, `active`, `complete`, `archived` |
| phase | `planned`, `in-progress`, `complete`, `blocked`, `deferred` |
| task | `planned`, `in-progress`, `complete`, `blocked`, `deferred` |

### Completion Evidence
`complete` is evidence-gated at every level. Prospective `verification` says how work will be judged; retrospective completion evidence (`### Completion Evidence` per task, `## Phase Completion Evidence`, `## Plan Completion Evidence`) records what actually ran — exact commands, native-SCM revision identity, focused review, observable results. A task, phase, or plan may not transition to `complete` while its evidence section is pending or nonconforming. Plan tasks are native-SCM revision boundaries: each lands as one clean, complete, independently bisectable commit (git adapter), with lifecycle bookkeeping in a separate scoped commit. Phase completion additionally requires a persisted, frozen, four-lane `Aligned` review (`shared/review-artifacts.md` § Phase-completion review gate). `shared/completion-evidence.md` is the single source of truth; `scripts/sdd_validate.py` (surfaced as `/validate`) enforces it deterministically.

### Plan Lifecycle
Plans live flat under `Plans/<PlanName>/`. Lifecycle is tracked in the plan README's frontmatter `status` field (`draft`, `approved`, `active`, `complete`, `archived`) — not by moving directories. Commands update `status` as plans progress:
- `/plan` creates the plan with `status: draft`, then sets `status: approved` after review
- `/implement` sets `status: active` when starting work
- `/implement` sets `status: complete` when the final phase finishes (evidence-gated — see above); `/debrief` backfills it if missed, subject to the same gate

AI commands filter by `status` to scope what they read.

### File Naming
- Plans: `Plans/<PlanName>/README.md`, `01-Phase-Name.md`
- Phases numbered with zero-padded prefixes: `01-`, `02-`, etc.
- Specs/Designs: `<Name>/README.md`

### Templates
Always use templates from `shared/templates/` when creating new artifacts. Replace `{{PLACEHOLDERS}}` with actual values.

## Skills

| Skill | Purpose |
|-------|---------|
| `/sdd-planner:research` | Investigate a topic → `Research/<topic>.md` |
| `/sdd-planner:brainstorm` | Explore possibilities → `Brainstorm/<topic>.md` |
| `/sdd-planner:specify` | Write requirements → `Specs/<feature>/README.md` |
| `/sdd-planner:design` | Technical architecture → `Designs/<component>/README.md` |
| `/sdd-planner:plan` | Create or expand an implementation plan → `Plans/<Name>/` (deepens existing plans via gap analysis on re-run) |
| `/sdd-planner:implement` | Execute a plan phase — implement tasks, track progress |
| `/sdd-planner:code-review` | Review code against the plan — drift, gaps, blind spots |
| `/sdd-planner:debrief` | After-action notes for completed phases |
| `/sdd-planner:decide` | Record, look up, audit, or reconcile decided truths → `Decisions/decisions.md` |
| `/sdd-planner:poke-holes` | Adversarial critical analysis of any artifact |
| `/sdd-planner:validate` | Deterministic + semantic validation of artifacts, evidence, and ledger (read-only) |
| `/sdd-planner:setup` | Set up a repo — generates planning-config.json, bootstraps directories, creates launcher |

### Model-only reference skills (`skills/`)

The `skills/` directory holds reference skills that are **not** user-invocable slash commands. Each carries `disable-model-invocation: true`, so it never appears in the `/sdd-planner:*` namespace, but the model auto-loads its body (progressive disclosure) when the task matches its `description`. They package on-demand reference that lifecycle skills used to read inline.

| Skill | Loads when |
|-------|-----------|
| `<lang>-specifications` (`cpp`, `rust`, `go`, `python`, `typescript`, `java`, `swift`) | Planning, implementing, or reviewing code in that language — supplies structural-verification tools and quality patterns. Coordinated by `shared/language-verification.md`. |
| `decision-log` | The user makes a design choice, defines a concept, or answers a design question — in any conversation, inside or outside a lifecycle skill. Carries the capture + collision discipline; convention lives in `shared/decision-log.md`. |

Restricted agents (e.g. the intent-isolated reviewers) don't get the main session's auto-loading, so they read a skill's `SKILL.md` body as a plain file when they need it.

## Agents

| Agent | Model | Role |
|-------|-------|------|
| `researcher` | Sonnet | Gathers context from artifacts, codebase, and web |
| `plan-reviewer` | Sonnet | Reviews plans for completeness, feasibility, conventions |
| `spec-reviewer` | Haiku | Reviews specs for testability, completeness, ambiguity |
| `code-implementer` | Opus | Implements code from plan tasks in the target codebase |
| `drift-detector` | Sonnet | Diff + plan only — flags missing work, scope creep, approach drift |
| `quality-scanner` | Sonnet | Diff + code only (intent-blind) — correctness, safety, maintainability, over-engineering |
| `spec-compliance` | Sonnet | Diff + specs/designs only — requirements coverage, contract violations |
| `blind-spot-finder` | Sonnet | Diff only — adversarial fresh-eyes reviewer |

### Code Review Architecture

`/code-review` orchestrates the four specialized reviewers **from the primary context**. Claude Code does not allow subagents to spawn subagents, so the orchestration has to live in the slash command (which runs in the primary context), not in an orchestrator subagent.

1. **`/code-review` (primary context)** identifies the minimum references — plan path, phase doc path, target repo path, diff scope — and resolves just enough planning metadata to dispatch with the right inputs (plan's `related` frontmatter for spec/design paths, a concrete git diff range). It does **not** read plan/spec/design bodies or diff contents.
2. **Four specialized reviewers** are dispatched in parallel via `Task` (a single message with four tool calls) using the plugin-namespaced form: `sdd-planner:drift-detector`, `sdd-planner:quality-scanner`, `sdd-planner:spec-compliance`, `sdd-planner:blind-spot-finder`. Each runs in its own fresh context. Intent isolation is enforced by what they're given:
   - `drift-detector` sees diff + plan (no specs/designs)
   - `quality-scanner` sees diff + code only (intent-blind)
   - `spec-compliance` sees diff + specs/designs (no plan)
   - `blind-spot-finder` sees diff only (no context at all)
3. **Primary context** synthesizes the four reports — highlighting agreements, disagreements, and findings only `blind-spot-finder` caught — and presents the unified review to the user.

**Hard contract**: `/code-review` MUST dispatch the four sub-agents via Task. It MUST NOT read the plan body, spec bodies, design bodies, or full diff and write findings itself — that would collapse the intent isolation and produce a single-pass review cosplaying as a four-lane review. If any dispatch fails, `/code-review` returns a loud error and stops; it does not fall back to self-synthesis.

`drift-detector`, `quality-scanner`, and `blind-spot-finder` are required to validate every finding against the actual code (full file + calling context), not just the diff hunk, because diffs lie by omission.

**Project-supplied review lanes (the socket).** The four built-in lanes are a floor, not a ceiling. A project can plug in its own specialized reviewers — e.g. a SQL-migration or Terraform reviewer — by dropping a `.claude/agents/*-reviewer.md` with `reviewLane: true` in its frontmatter, without touching the plugin. `/code-review` globs for these (Step 2e), matches each against the diff's shape, and dispatches the matches as **additional** parallel lanes. The convention is defined in `shared/review-lanes.md` (single source of truth) with a copyable template at `shared/templates/custom-reviewer.md`. Three frontmatter fields: `reviewLane: true` (required; the opt-in marker), `appliesTo` (optional path globs gating dispatch — absent means always dispatched and self-scoping), and `lane` (optional — a recognized value `code`/`spec`/`plan`/`diff-only` gets that built-in's intent-isolated input bundle with isolation **plugin-enforced**; an absent value makes it a standalone lane and an unrecognized value groups same-named lanes together, both getting base inputs only and self-gathering further context, so their isolation is the **project's** responsibility). Project lanes are strictly **additive and best-effort**: they never replace or weaken the built-in four, and a project lane that fails to dispatch, errors, or is malformed is recorded and skipped — never fatal (the socket imposes no timeout; keeping a lane responsive is the project's responsibility). The strict "STOP on dispatch failure" rule applies to the four built-ins only; the Overall Verdict is computed from the four plus any successful project lanes. Failure is **never silent**, though: a declared lane that doesn't run **degrades the verdict headline**, and an opt-in `required: true` lane that doesn't run **forces the verdict to BLOCKED** (the four still run — `required` gates the verdict, not the floor). Lanes must be **read-only** and are **trust-gated** — `/code-review` confirms discovered lanes before dispatch when the target repo isn't the session's own project, since they execute repo-supplied instructions with session tool access. (The discovery glob also matches a project's `plan-reviewer`/`spec-reviewer` overrides; those must **not** carry `reviewLane` — the marker is what disambiguates a lane from an override.)

`/implement` dispatches `quality-scanner` directly (via `sdd-planner:quality-scanner`) for per-task reviews. It bypasses the full four-lane review because the question after a single task is local to the code at hand.

### MCP Server Inheritance

Agents fall into two groups based on how they handle MCP servers:

**Inherit all session tools (no `tools:` frontmatter)** — these agents automatically pick up whatever MCP servers the user's project has configured (e.g., `context7` for docs, Linear/Jira MCPs for tickets, Slack, Notion, etc.):
- `researcher` — uses doc-lookup MCPs for library research; falls back to WebFetch/WebSearch
- `code-implementer` — uses doc-lookup MCPs to verify current API syntax while writing code
- `quality-scanner` — uses doc-lookup MCPs when judging whether diff code uses a library correctly
- `plan-reviewer` — uses doc-lookup MCPs to verify planned APIs are real and current; uses ticket/KB MCPs to cross-check the plan against linked tickets
- `spec-reviewer` — uses ticket/KB MCPs to compare a spec against its source-of-truth ticket; uses doc-lookup MCPs to verify external API contracts the spec assumes

**Restricted allowlist (`tools:` frontmatter)** — these three review agents have no MCP access on purpose. Their value comes from intent isolation: each is shown only what its lane needs, and MCP access would let intent leak in through external sources:
- `drift-detector` — diff + plan only. Reading specs/designs (or fetching tickets that *are* specs) would erase the lane.
- `spec-compliance` — diff + specs/designs only. Reading the plan via tickets would muddy what counts as "the spec."
- `blind-spot-finder` — diff only. Any external context erodes the diff-only adversarial guarantee.

The allowlists block MCP/Write/Edit, but `Bash` remains a residual read channel — the isolation is therefore enforced behaviorally: the orchestrator curates each lane's inputs, and each lane's prompt explicitly forbids reading planning config or out-of-lane artifacts.

The inheriting agents carry behavioral guardrails in their bodies (`researcher`, `quality-scanner`, `plan-reviewer`, and `spec-reviewer` are read-only even though they could technically inherit Write/Edit and write-shaped MCP calls from the session). Projects that want stricter guarantees can drop overrides into `.claude/agents/<name>.md` at the project level — those take precedence over plugin-provided agents.

## Workflow Lifecycle

The typical flow through skills:
```
/sdd-planner:setup → /sdd-planner:research → /sdd-planner:brainstorm → /sdd-planner:specify → /sdd-planner:design → /sdd-planner:plan → /sdd-planner:implement → /sdd-planner:code-review → /sdd-planner:debrief
```
Install the companion [`sdd-dashboard`](https://github.com/danweinerdev/sdd-dashboard-plugin) plugin to add `/sdd-dashboard:dashboard` (HTML dashboard) and `/sdd-dashboard:status` (quick text summary) for checking progress.
Use `/sdd-planner:poke-holes` before approving any artifact. Use `/sdd-planner:decide` to record, look up, or audit decided truths at any point (`/sdd-planner:decide check` is the periodic hygiene net for the ledger). Use `/sdd-planner:validate` before implementation, before any completion transition, or in CI — lifecycle skills also invoke its validator script at their own gates.

## Artifact Status Values

| Type | Statuses |
|------|----------|
| research | `draft`, `active`, `archived` |
| brainstorm | `draft`, `active`, `archived` |
| spec | `draft`, `review`, `approved`, `implemented`, `superseded` |
| design | `draft`, `review`, `approved`, `implemented`, `superseded` |
| diagram (legacy) | `draft`, `active`, `archived` |
| debrief | `draft`, `complete` |
| retro (legacy) | `draft`, `complete` |

## Configuration

### planning-config.json
The planning root's `planning-config.json` drives all path resolution:
- `planningRoot`: where artifacts live, as a path. Use `"."` if planning artifacts are at the repo root, a relative subdirectory name (e.g., `"Planning"`) if they live inside a project, or an absolute path (e.g., `"/home/user/Code/my-planning-repo"`) if they live in an external directory shared by multiple repos. There is no other distinction — the path is just a path.
- `repositories`: map of external repo keys to GitHub URLs (used when plans target code in other repos)
- `planMapping`: map of plan names to target repos
- `planRepository`: key for the planning repo itself

The companion `sdd-dashboard` plugin reads two additional fields when generating its HTML dashboard: `dashboard: true` (opt-in switch), and `title` / `description` for the page chrome. They are ignored if the plugin isn't installed.

### planning-config.local.json (gitignored)
Local filesystem paths for external repositories:
```json
{ "repositories": { "repo-key": { "path": "/absolute/path" } } }
```

## Dashboard

The HTML dashboard previously lived here has moved to a companion plugin: [`sdd-dashboard`](https://github.com/danweinerdev/sdd-dashboard-plugin). Install it alongside `sdd-planner` to get `/sdd-dashboard:dashboard` (HTML) and `/sdd-dashboard:status` (text summary). The dashboard is opt-in via `"dashboard": true` in `planning-config.json`.

## Maintenance Rules

When adding, removing, or renaming skills (`commands/`), agents (`agents/`), or modifying user-facing behavior, update these files to stay in sync:
- **`README.md`** — command/agent counts, tables, Mermaid diagrams, directory listing
- **`CLAUDE.md`** — skill table, agent table, workflow lifecycle
- **`shared/templates/claude-md-full.md`** — full CLAUDE.md template for a planning-only repo (skill table, agent table, workflow lifecycle)
- **`shared/templates/claude-md-snippet.md`** — embeddable section to drop into an existing project's CLAUDE.md (skill table only)
- **Templates ↔ schema ↔ validator** — when changing any template, `shared/frontmatter-schema.md`, `shared/completion-evidence.md`, or `shared/review-artifacts.md`, verify every template still satisfies the schema AND `scripts/sdd_validate.py`'s contract (required fields and exact headings present, statuses valid, evidence labels intact). The validator is the enforcement layer; docs and script must not drift apart
- **`shared/decision-framework.md` ↔ agents** — when changing the framework, update the canonical agent block in that file and re-sync the identical `## Decision Framework` section in all eight `agents/*.md`

## Versioning

The plugin uses `vMAJOR.MINOR.PATCH` semver. The version is declared in `.claude-plugin/plugin.json`. Claude Code caches plugins by version — **users will not see changes unless the version is bumped**.

| Bump | When | Command |
|------|------|---------|
| **patch** | Bug fix, wording tweak, small correction | `make bump-patch` |
| **minor** | New or completed feature, new skill, meaningful behavior change | `make bump-minor` |
| **major** | Breaking changes to artifact format, config schema, or skill interface | `make bump-major` |

Each `make bump-*` target updates `plugin.json`, creates a commit (`v1.2.3`), and adds a git tag (`v1.2.3`). Always bump before pushing a release.
