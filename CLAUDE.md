# SDD Planner

A spec-driven development toolchain: structured project planning with lifecycle skills, intent-isolated review agents, and a deterministic Go validator (`sdd`). This repository is the **single source for every supported harness**:

- **Repo root** — the canonical, hand-edited Claude Code plugin (`commands/`, `agents/`, `skills/`, `shared/`, `hooks/`).
- **`.codex-plugin/` and `.opencode-plugin/`** — generated plugin trees for Codex and OpenCode, produced by `sdd plugin sync`. Never hand-edited; drift fails `make test`.

## Directory Structure

```
claude-sdd-planner/               # Repository root = canonical Claude plugin root
├── .claude-plugin/
│   └── plugin.json               # Canonical manifest — single source of version + minSddVersion
├── commands/                     # Slash commands (auto-namespaced /sdd-planner:*); each is a dir with SKILL.md
│   └── <name>/SKILL.portable.md  #   optional hand-maintained portable variant (code-review, implement, setup)
├── skills/                       # Model-only reference skills (auto-loaded by description, not /-invocable)
│   ├── <lang>-specifications/    #   per-language structural verification (cpp, rust, go, python, typescript, java, swift)
│   ├── decision-log/             #   ad-hoc decision capture outside lifecycle skills
│   └── sdd-cli/                  #   how to drive the sdd binary — task→command map + write-path discipline
├── agents/                       # Subagent definitions — also the source for the portable role prompts
├── hooks/                        # Wrappers that locate the binary; hooks.json is NOT shipped —
│   ├── sdd-hook.sh               #   `sdd provision` generates it per-platform (see below)
│   └── sdd-hook.ps1
├── shared/                       # Conventions + templates, shipped to every harness
│   ├── frontmatter-schema.md     #   single source of truth for artifact metadata
│   ├── completion-evidence.md    #   evidence-gated completion — what `complete` requires, per level
│   ├── decision-log.md           #   decision ledger — entry schema, admission test, collision procedure
│   ├── decision-framework.md     #   universal decision discipline for all skills and agents
│   ├── path-resolution.md        #   planning-root / plugin-dir / target-repo resolution (marker-managed)
│   ├── orchestration.md          #   orchestration model + portable prompt catalog (marker-managed)
│   ├── vcs-detection.md          #   VCS detection algorithm + operations table (git / p4 / plain)
│   ├── autonomy.md               #   cross-skill autonomy table — what runs solo vs stops for the user
│   ├── review-lanes.md           #   project-supplied review-lane socket (+ .portable.md variant)
│   ├── review-artifacts.md       #   persisted review contract incl. the phase-completion gate
│   ├── language-verification.md  #   language-specific verification — what good looks like
│   ├── agent-runtime.md          #   portable runtime conventions (resolution, delegation, resource boundary)
│   └── templates/                #   document templates (+ agents-md-*, claude-md-*, custom-reviewer variants)
├── cmd/sdd/                      # The `sdd` binary — validation, artifact writes, hooks, plugin sync
├── internal/                     # Binary internals (rules, dlg, compile, hook, provision, schema, vcs, portable)
├── tools/                        # genfixtures + the frozen regression corpus (run by `go test ./...`)
├── .codex-plugin/                # GENERATED Codex plugin tree — never hand-edit
├── .opencode-plugin/             # GENERATED OpenCode plugin tree (same content) — never hand-edit
├── portable-overrides/           # Hand-maintained portable-only sources (the portable README)
├── .plans/                       # This repo's own planning artifacts (planning-config.json → ".plans")
│   └── Decisions/decisions.md    #   the decision ledger — D-NNNN entries are standing constraints
├── Makefile                      # build / test / plugins / bump-*
└── bump-version.py               # patch|minor|major bumps, `set X.Y.Z`, `set-floor X.Y.Z`
```

## The Two-Tree Architecture

The repo root is hand-edited; `.codex-plugin/` and `.opencode-plugin/` are compiled from it by `sdd plugin sync` (`make plugins`, engine in `internal/portable`). The portable trees re-express the same content in the layout the other harnesses install: `skills/sdd-<name>/SKILL.md`, role prompts instead of agent definitions, `plugin.json` at each tree root, runtime-neutral wording per `shared/agent-runtime.md`.

Four mechanisms, in order of preference:

1. **Transforms** (free — prefer these): skill rename (`plan` → `sdd-plan`), slash-title and description cleanup, `## Path Resolution` → stock `## Resources` swap, agent-dispatch phrase rewrites onto the stable identifiers (`implement_task`, `review_plan_drift`, `review_quality`, `review_spec_compliance`, `review_blind_spots`), path/term rewrites.
2. **Harness markers** (paragraph-level divergence in one file): `<!-- claude-only -->…<!-- /claude-only -->` blocks are dropped from portable output; `<!-- portable-only … -->` blocks are uncommented. Used in `path-resolution.md`, `orchestration.md`, `templates/quality-scan-prompt.md`.
3. **Variants** (whole-file divergence): a `*.portable.md` sibling replaces the generated transform of its canonical file. Current variants: `commands/{code-review,implement,setup}/SKILL.portable.md`, `shared/review-lanes.portable.md`, `shared/templates/custom-reviewer.portable.md`. **Editing a canonical file that has a variant means checking whether the variant needs the same change.**
4. **Overrides** (portable-only files, no canonical sibling): `portable-overrides/` (currently just the portable README).

**Derived prompts**: `shared/agent-prompts/` and `shared/review-prompts/` in the portable trees are generated from `agents/*.md` (`internal/portable/prompts.go`) — frontmatter and Path Resolution dropped, a `{{PLACEHOLDER}}` Inputs block merged in, standard transforms applied. An agent edit propagates to both harnesses automatically. `code-implementer` deliberately has no prompt: implementation dispatches carry the task inline under `implement_task` (D-0009).

**Gates**: `internal/portable`'s tests run in `make test` and fail on (a) drift — either tree differing from a fresh generation — and (b) leaks — any Claude-ism (`sdd-planner:`, `the Task tool`, `~/.claude`, `## Path Resolution`, …) reaching portable output. The generated manifests take `version`/`minSddVersion` from `.claude-plugin/plugin.json`; `make bump-*` re-syncs them inside the bump commit so all trees release together (D-0016).

`sdd plugin status` prints the generated/variant/override provenance of every portable file.

## Conventions

### Frontmatter
All artifacts use YAML frontmatter as the machine-readable data layer. See `shared/frontmatter-schema.md` for the complete schema. Frontmatter is the only interface tools read — no markdown table parsing.

### Sensitive data
Artifacts never capture credentials, private hostnames/IPs, usernames, or machine-specific absolute paths (`/home/<user>/...`, `C:\Users\...`). Paths are repo/planning-root-relative by default; `~/...`/`$HOME/...` are acceptable generic forms where a path outside the root is needed; scrub pasted command output. Single source of truth: `shared/frontmatter-schema.md` § Sensitive Data.

### VCS-agnostic operations
The plugin works with git, git worktrees, Perforce, and unversioned directories. Skills that inspect files or history detect the VCS first using the algorithm in `shared/vcs-detection.md`, then use the corresponding command from that file's operations table. Don't hard-code `git` in skills. Likewise, path resolution (planning root, plugin directory, target repo) is defined once in `shared/path-resolution.md` — don't re-derive it in skills.

### Decision framework
Every context this plugin runs in — primary-context skills and all agents, on any model — follows the universal decision discipline in `shared/decision-framework.md` (premise checks before complying, run-to-verify for commandable claims, documented searches behind absence claims, verbatim failure reporting, no downscoping by imagined effort). Each agent embeds the framework's canonical digest block verbatim as a `## Decision Framework` section. When editing the framework, re-sync the embedded block in every agent (the portable prompts then follow automatically via generation).

### Sourced Necessity (the scope counterweight)
Every gap-detection mechanism in the plugin is additive, so this rule keeps plans from growing monotonically. It is traceability, not size: **every task carries the demand that motivates it, or it is cut**. Task frontmatter carries a required `justifies` field (the `FR-NN`/`NFR-NN`/`AC-NN`/`D-NNNN` ids it serves, or the concrete failure it prevents — never a restatement of the title); `/plan` refuses unsourced tasks and can retire tasks in Revise mode; `plan-reviewer` carries a Scope lens; plans and designs carry `## Non-Goals`. `sdd validate` enforces the mechanically checkable part (SDD063 absent, SDD076 placeholder, SDD077 title-echo). The counterweight starts at `/brainstorm`: **Idea 0 is always "do nothing / status quo"**, scored in the same matrix as every other option (SDD078 surfaces a missing baseline as a candidate diagnostic). Two limits bound the rule: necessity is about sourcing, not size, and **correctness is never speculative** — error handling, edge cases, tests, rollback, and observability stay even when no requirement names them.

### Decision Ledger
User decisions — design choices, concept definitions, answered design questions — are recorded as durable truth in a `decision-log` artifact (`decisions[]` frontmatter array): `Decisions/decisions.md` under the planning root when the root is inside the repo, or the target repo's own `DECISIONS.md` when the planning root is external. Only *durable* decisions qualify — `shared/decision-log.md` § Capture carries the admission test (outlives its document / not already carried by it / a real alternative lost). **No LLM edits the ledger, for any reason, without the user's explicit approval of the exact, unmodified text of the change, shown in full beforehand** — no writes on assumptions, and non-objection is not approval. A new decision that contradicts an `accepted` entry always stops for user reconciliation — never auto-resolve. `shared/decision-log.md` is the single source of truth: entry schema, capture triggers, collision check, and distribution rules (the ledger is intent context: researcher/plan-reviewer/spec-reviewer yes; quality-scanner/blind-spot-finder never). Not to be confused with the decision *framework* above, which is the reasoning discipline.

### Plan Hierarchy
```
Plan (README.md)       <- like a Jira Project
 └── Phase (01-*.md)   <- a milestone-sized slice of the plan
      └── Task          <- defined in phase frontmatter
           └── Subtask  <- checklist items in body
```

### Status Values
| Level | Statuses |
|-------|----------|
| plan | `draft`, `approved`, `active`, `complete`, `archived` |
| phase | `planned`, `in-progress`, `complete`, `blocked`, `deferred` |
| task | `planned`, `in-progress`, `complete`, `blocked`, `deferred` |

| Artifact type | Statuses |
|------|----------|
| research / brainstorm | `draft`, `active`, `archived` |
| spec / design | `draft`, `review`, `approved`, `implemented`, `superseded` |
| debrief | `draft`, `complete` |

### Completion Evidence
`complete` is evidence-gated at every level. Prospective `verification` says how work will be judged; retrospective completion evidence records what actually ran — exact commands, native-SCM revision identity, focused review, observable results. Plan tasks are native-SCM revision boundaries: each lands as one clean, complete, independently bisectable commit (git adapter), with lifecycle bookkeeping in a separate scoped commit. Phase completion additionally requires a persisted, frozen, four-lane `Aligned` review (`shared/review-artifacts.md` § Phase-completion review gate). `shared/completion-evidence.md` is the single source of truth; `sdd validate` (surfaced as `/validate`) enforces it deterministically. Graph plans (a committed `<Name>-Graph.json`) tighten all of this mechanically: states derive from observations (never stored), completion is sync-only (a parsed report, never an assertion), hazard-discharging tests must be observed red before a green counts, review gates green only from frozen `Aligned` review artifacts, and closure is the derived closed predicate (D-0022). v1 plans without graphs keep the markdown protocol until converted.

### Plan Lifecycle
Plans live flat under `Plans/<PlanName>/`; lifecycle is the README frontmatter `status`, never a directory move. `/plan` creates `draft` and sets `approved` after review; `/implement` sets `active`, then `complete` when the final phase finishes (evidence-gated); `/debrief` backfills a missed transition subject to the same gate. AI commands filter by `status` to scope what they read.

### File Naming & Templates
Plans: `Plans/<PlanName>/README.md` with zero-padded phase docs (`01-Phase-Name.md`); specs/designs: `<Name>/README.md`; research/brainstorm: flat files. Always create artifacts from `shared/templates/`, replacing `{{PLACEHOLDERS}}` — the templates, `shared/frontmatter-schema.md`, and `sdd validate` are kept in lockstep by `make test`.

## Skills

| Skill | Purpose |
|-------|---------|
| `/sdd-planner:research` | Investigate a topic → `Research/<topic>.md` |
| `/sdd-planner:brainstorm` | Explore possibilities → `Brainstorm/<topic>.md` |
| `/sdd-planner:specify` | Write requirements → `Specs/<feature>/README.md` |
| `/sdd-planner:design` | Technical architecture → `Designs/<component>/README.md` |
| `/sdd-planner:plan` | Decompose work into an executable plan graph → `Plans/<Name>/` + `<Name>-Graph.json` (interview → payload → compile → silhouette; extends on re-run; v1 plans keep the old protocol until converted) |
| `/sdd-planner:implement` | Walk the plan graph — claim → red → green → sync → merge, observation-gated (v1 plans keep the wave protocol) |
| `/sdd-planner:code-review` | Review code against the plan — drift, gaps, blind spots |
| `/sdd-planner:debrief` | After-action notes for completed phases |
| `/sdd-planner:decide` | Record, look up, audit, or reconcile decided truths → `Decisions/decisions.md` |
| `/sdd-planner:poke-holes` | Adversarial critical analysis of any artifact |
| `/sdd-planner:validate` | Deterministic + semantic validation (read-only, wraps `sdd validate`) |
| `/sdd-planner:setup` | Set up a repo — config, directories, binary verification, launcher |

In the portable trees the same skills ship as `sdd-research`, `sdd-plan`, …, selected by description matching rather than slash invocation.

### Model-only reference skills (`skills/`)
Not user-invocable (`disable-model-invocation: true`); the model auto-loads them by description. `<lang>-specifications` (7 languages) supplies structural-verification tools when planning/implementing/reviewing that language, coordinated by `shared/language-verification.md`; `decision-log` carries the capture + collision discipline for ad-hoc decisions; `sdd-cli` carries the binary's task→command map and write-path discipline (statuses move through transitions, evidence records what ran, exit 1 is a working gate — never hand-edit around it) for `sdd` use outside a lifecycle skill. In the portable trees the language skills flatten to `shared/language-specs/<lang>.md`; `decision-log` and `sdd-cli` ship as skills. Restricted agents read skill bodies as plain files when they need them.

## Agents

| Agent | Model | Role |
|-------|-------|------|
| `researcher` | Sonnet | Gathers context from artifacts, codebase, and web |
| `plan-reviewer` | Sonnet | Reviews plans for completeness, feasibility, conventions |
| `spec-reviewer` | Haiku | Reviews specs for testability, completeness, ambiguity |
| `code-implementer` | Opus | Implements code from plan tasks in the target codebase |
| `drift-detector` | Sonnet | Diff + plan only — missing work, scope creep, approach drift |
| `quality-scanner` | Sonnet | Diff + code only (intent-blind) — correctness, safety, maintainability, over-engineering |
| `spec-compliance` | Sonnet | Diff + specs/designs only — requirements coverage, contract violations |
| `blind-spot-finder` | Sonnet | Diff only — adversarial fresh-eyes reviewer |

These files are dual-purpose: Claude dispatches them as subagent types, and the portable trees derive their role-prompt files from the same bodies.

### Code Review Architecture

`/code-review` orchestrates the four specialized reviewers **from the primary context** (Claude Code does not allow subagents to spawn subagents):

1. The primary context identifies only references — plan path, phase doc path, target repo path, diff scope — plus the plan's `related` frontmatter and a concrete diff range. It does **not** read plan/spec/design bodies or diff contents.
2. Four reviewers are dispatched in parallel via `Task` using plugin-namespaced names (`sdd-planner:drift-detector`, …), each receiving only its lane's inputs. Intent isolation is the product: `blind-spot-finder` sees the diff and nothing else.
3. The primary context synthesizes the reports — agreements, disagreements, and blind-spot-only findings.

**Hard contract**: `/code-review` MUST dispatch the four lanes via Task and MUST NOT read the bodies and self-synthesize — that is a single-pass review cosplaying as a four-lane review. A failed built-in dispatch is a loud STOP, never a silent fallback. `drift-detector`, `quality-scanner`, and `blind-spot-finder` validate every finding against the full file and calling context, because diffs lie by omission.

**Project-supplied review lanes (the socket).** A project can add its own read-only lanes by dropping `.claude/agents/*-reviewer.md` with `reviewLane: true` (optional `appliesTo` globs, `lane` input-bundle selector, `required` verdict gate). Project lanes are additive and best-effort — they never weaken the built-in four, and a declared lane that doesn't run degrades the verdict (a `required` one forces BLOCKED). Lanes are trust-gated: `/code-review` confirms discovered lanes before dispatch when the target repo isn't the session's own project. Convention: `shared/review-lanes.md`; template: `shared/templates/custom-reviewer.md`.

`/implement` dispatches `quality-scanner` directly for per-task reviews. In the portable trees, the four lanes run as rendered prompts under the stable identifiers (`review_plan_drift`, `review_quality`, `review_spec_compliance`, `review_blind_spots`) with a labeled single-agent fallback — serial lanes never claim independent corroboration.

### MCP Server Inheritance

Agents without `tools:` frontmatter (`researcher`, `code-implementer`, `quality-scanner`, `plan-reviewer`, `spec-reviewer`) inherit all session tools including MCP servers — doc lookups, ticket cross-checks. The three intent-isolated lanes (`drift-detector`, `spec-compliance`, `blind-spot-finder`) carry a restricted allowlist with no MCP access on purpose: external context would erode the lane. All lanes keep `Bash` because the diff is their input; it is guarded behaviorally (prompts forbid writes) and mechanically by the `sdd hook pretooluse` PreToolUse hook, which allowlists read-only `git`/`p4` subcommands and `sdd`'s own read-only subcommands for the seven read-only agents, denies write- or network-shaped commands, and fails open for everything else. Projects wanting stricter guarantees can override any agent via `.claude/agents/<name>.md`.

## Workflow Lifecycle

```
/sdd-planner:setup → research → brainstorm → specify → design → plan → implement → code-review → debrief
```
Use `poke-holes` before approving any artifact, `decide` to record or audit decided truths at any point (`decide check` is the ledger hygiene net), and `validate` before implementation, before any completion transition, or in CI.

## The `sdd` Binary

Every skill and both hooks drive one cross-platform Go binary. The plugin does not ship or build it — users install it once per machine:

```bash
go install github.com/danweinerdev/claude-sdd-planner/v2/cmd/sdd@latest
```

`/setup` verifies it (floor: `minSddVersion` in `plugin.json` — advanced deliberately via `bump-version.py set-floor`, never by `make bump-*`), copies it to `${CLAUDE_PLUGIN_ROOT}/bin/` for the hooks, and stops with the exact `go install` command when missing or too old (D-0015). Key subcommands: `validate`, `apply`, `section set`, `evidence add`, `task|phase|plan complete`, `plan approve|activate`, `spec|design submit|approve|implement|supersede`, `decide`, `review scaffold|evidence set|resolve`, `template` (incl. `graph-proposal`), `hook`, `provision`, `plugin sync|check|status`, `doctor` — plus the graph family: `compile`, `next --claim`, and `graph init|propose|assemble|convert|hazards|sync|reverify|review|release|split|set-tests|gc|retire|status|show|path|risk|shape|export`.

## Configuration

`planning-config.json` at the planning root drives path resolution: `planningRoot` (a path — `"."`, a relative subdirectory, or an absolute external directory), `repositories` (repo keys → GitHub URLs), `planMapping` (plan names → repo keys), `planRepository`, optional `title` / `description`. Local absolute paths live in gitignored `planning-config.local.json`. This repo's own planning root is `.plans/`.

## Maintenance Rules

When adding, removing, or renaming skills, agents, or user-facing behavior, keep these in sync:

- **`README.md`**, **`CLAUDE.md`**, **`AGENTS.md`** — counts, tables, diagrams, directory listings
- **`shared/templates/claude-md-full.md`** / **`claude-md-snippet.md`** and **`agents-md-full.md`** / **`agents-md-snippet.md`** — the setup-generated guidance must mirror the skill/agent tables
- **Run `make plugins` after touching `commands/`, `skills/`, `agents/`, `shared/`, or `.claude-plugin/plugin.json`** — the portable trees are generated from them; `make test` fails on drift. When a canonical file has a `*.portable.md` variant, decide whether the variant needs the same edit
- **Templates ↔ schema ↔ validator** — when changing any template, `shared/frontmatter-schema.md`, `shared/completion-evidence.md`, or `shared/review-artifacts.md`, verify every template still satisfies the schema AND `sdd validate`'s contract. The validator is the enforcement layer; docs and binary must not drift apart
- **`shared/decision-framework.md` ↔ agents** — when changing the framework, re-sync the identical `## Decision Framework` block in all eight `agents/*.md` (portable prompts regenerate from them)
- **Run `make test` after touching templates, the schema, or `internal/rules/`** — it runs the Go suite, the frozen regression corpus, the template gate, and the portable drift/leak gates
- **Every rule carries its own `Good` and `Bad` examples** — the registry meta-test fails when either is missing. `make gen-fixtures` materializes them into the corpus; commit the result
- **Never regenerate `tools/parity/frozen-expectations.json`** — it is the deleted Python validator's last recorded verdict; rewriting it would change the answer rather than test against it

## Versioning

`vMAJOR.MINOR.PATCH`, declared in `.claude-plugin/plugin.json` — the single source; `internal/version/version.go` and both portable manifests are regenerated from it. Claude Code caches plugins by version — **users will not see changes unless the version is bumped**.

| Action | Command |
|--------|---------|
| Bug fix / wording tweak | `make bump-patch` |
| New or changed feature, new skill | `make bump-minor` |
| Breaking artifact/config/skill-interface change | `make bump-major` |
| Explicit jump (e.g. release unification) | `python3 bump-version.py set X.Y.Z && make plugins` |
| Advance the binary floor (deliberate, D-0015) | `python3 bump-version.py set-floor X.Y.Z && make plugins` |

Each `make bump-*` target runs the test suite first, updates `plugin.json` + `version.go`, re-syncs the portable manifests, and creates the `vX.Y.Z` commit + tag. Always bump before pushing a release.
