# {{TITLE}}

{{DESCRIPTION}}

This repository holds planning artifacts managed by the `sdd-planner` Claude Code plugin. All commands below are provided by the plugin and are namespaced as `/sdd-planner:*`.

## Directory Structure

```
{{PLANNING_ROOT}}/
├── CLAUDE.md                     # This file
├── planning-config.json          # Planning configuration
├── .gitignore
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
├── Decisions/                    # Decision ledger (single canonical file)
│   └── decisions.md              # decisions[] frontmatter array of decided truths
└── Dashboard/                    # Generated HTML (gitignored, written by the optional sdd-dashboard plugin)
```

## Conventions

### Frontmatter
All artifacts use YAML frontmatter as the machine-readable data layer. See the sdd-planner plugin's `shared/frontmatter-schema.md` (in the plugin directory, not this repo) for the complete schema. Companion tools like the optional `sdd-dashboard` plugin read exclusively from frontmatter — no markdown table parsing.

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

### Plan Lifecycle
Plans live flat under `Plans/<PlanName>/`. Lifecycle is tracked in the plan README's frontmatter `status` field (`draft`, `approved`, `active`, `complete`, `archived`) — not by moving directories. Commands update `status` as plans progress:
- `/sdd-planner:plan` creates the plan with `status: draft`, then sets `status: approved` after review
- `/sdd-planner:implement` sets `status: active` when starting work
- `/sdd-planner:implement` sets `status: complete` when the final phase finishes; `/sdd-planner:debrief` backfills it if missed

AI commands filter by `status` to scope what they read.

### File Naming
- Plans: `Plans/<PlanName>/README.md`, `01-Phase-Name.md`
- Phases numbered with zero-padded prefixes: `01-`, `02-`, etc.
- Specs/Designs: `<Name>/README.md`
- Decisions: `Decisions/decisions.md` (single canonical ledger)

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
| `/sdd-planner:setup` | Set up a repo — generates planning-config.json, bootstraps directories, creates launcher |

If the optional `sdd-dashboard` plugin is installed:

| Skill | Purpose |
|-------|---------|
| `/sdd-dashboard:dashboard` | Regenerate HTML dashboard into `Dashboard/` |
| `/sdd-dashboard:status` | Quick text status summary (read-only) |

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

### Custom review lanes
`/sdd-planner:code-review` always runs the four intent-isolated reviewers (`drift-detector`, `quality-scanner`, `spec-compliance`, `blind-spot-finder`) in parallel and synthesizes their reports. Projects can add their own specialized reviewers — e.g. a SQL-migration or Terraform reviewer — as **additional** lanes by dropping a `.claude/agents/*-reviewer.md` with `reviewLane: true` in its frontmatter. See the plugin's `shared/review-lanes.md` (in the plugin directory, not this repo) for the full convention.

## Workflow Lifecycle

The typical flow through skills:
```
/sdd-planner:setup → /sdd-planner:research → /sdd-planner:brainstorm → /sdd-planner:specify → /sdd-planner:design → /sdd-planner:plan → /sdd-planner:implement → /sdd-planner:code-review → /sdd-planner:debrief
```
Use `/sdd-planner:poke-holes` before approving any artifact. Use `/sdd-planner:decide` to record, look up, or audit decided truths at any point.
If the `sdd-dashboard` plugin is installed, use `/sdd-dashboard:dashboard` or `/sdd-dashboard:status` at any point to check progress.

## Artifact Status Values

| Type | Statuses |
|------|----------|
| research | `draft`, `active`, `archived` |
| brainstorm | `draft`, `active`, `archived` |
| spec | `draft`, `review`, `approved`, `implemented`, `superseded` |
| design | `draft`, `review`, `approved`, `implemented`, `superseded` |
| debrief | `draft`, `complete` |

## Configuration

### planning-config.json
The planning root's `planning-config.json` drives all path resolution:
- `planningRoot`: where artifacts live. Use `"."` if planning lives at the repo root (the default), a relative subdirectory name (e.g., `"Planning"`) if it lives inside a project, or an absolute path if it lives elsewhere on disk.
- `repositories`: map of external repo keys to GitHub URLs (used when plans target code in other repos)
- `planMapping`: map of plan names to target repos
- `planRepository`: key for the planning repo itself
- `dashboard` (optional): `true` to enable HTML generation by the companion `sdd-dashboard` plugin (off by default; ignored if the plugin isn't installed)

### planning-config.local.json (gitignored)
Local filesystem paths for external repositories:
```json
{ "repositories": { "repo-key": { "path": "/absolute/path" } } }
```

## Dashboard

The HTML dashboard is provided by the optional companion plugin [`sdd-dashboard`](https://github.com/danweinerdev/sdd-dashboard-plugin). Set `"dashboard": true` in `planning-config.json` to opt in, then run `/sdd-dashboard:dashboard` from Claude. Output lands in `Dashboard/` (gitignored).
