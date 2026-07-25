# Review Lanes — the project-supplied reviewer socket

`/code-review` always runs four built-in, intent-isolated lanes (`drift-detector`, `quality-scanner`, `spec-compliance`, `blind-spot-finder`). This file defines the **socket** that lets a project add its own specialized review lanes — a SQL-migration reviewer, a Terraform reviewer, an accessibility reviewer — **without touching the plugin**. The plugin grows a socket; projects bring the plug.

This file is the single source of truth for the convention. `/code-review` reads it; project authors copy `shared/templates/custom-reviewer.md` and follow it.

## The contract

> The four built-in lanes are the floor and always run. Project lanes are **additive**: they can add findings and coverage, but they never remove, replace, or weaken a built-in lane's inputs or its dispatch, and their failure never aborts the four (a `required` lane's failure blocks the *verdict*, not the run). "Additive" is about *findings* — it does not mean *free*: extra lanes cost latency, tokens, and rate-limit headroom, so gate them with `appliesTo` and keep them read-only.

Two consequences the rest of this file makes good on:
- A project lane that fails is **reported, not hidden** — and if it was *declared and didn't run*, that visibly degrades the verdict headline so a green check can never sit over an un-run lane.
- A project lane is **read-only** and **intent-scoped** so it cannot corrupt what the built-ins review or what the synthesis concludes.

## Trust boundary (read before enabling)

Discovered lanes are **agent instructions that live in the target repo**, executed with the session's tool access. When you run `/code-review` against a branch you don't fully control — a contractor's branch, an open-source PR, anything where someone else could have added files under `.claude/agents/` — a malicious `*-reviewer.md` can carry instructions to read secrets, call MCP servers, or emit a misleading "no issues." Treat the socket the same way you treat running the repo's own code:

- Enable project lanes only against repos you trust. The orchestrator **must list discovered lanes to the user before dispatching**, and **must ask for confirmation** when the target repo is not the session's own project.
- The plugin's `hooks/reviewer-bash-guard.py` PreToolUse guard covers only the plugin's **own** read-only agents — a project lane's agent name isn't known to it, so project lanes get no mechanical Bash screening. The trust gate above is their only protection; that asymmetry is another reason to keep it strict.
- Lanes are **read-only** (see below) and should ship a restrictive `tools:` allowlist. The plugin cannot enforce a project agent's tool list, so the template defaults to read-only and the convention requires it.

## Discovery

`/code-review` discovers project lanes by globbing, **non-recursively**, two locations and de-duplicating by the agent's bare `name`:

1. The **target repo's** top-level `.claude/agents/*-reviewer.md` (the repo whose code is under review — where domain/language reviewers naturally live).
2. The **user's** `~/.claude/agents/*-reviewer.md` (always registered for dispatch; useful for personal cross-project reviewers).

**De-duplication precedence:** if the same bare `name` appears in **both** locations, the **target repo's** file wins and the user-level file is reported as *shadowed* (a quiet note, not Malformed). Two files in the **same** location resolving to the same bare `name` are **Malformed** — ambiguous dispatch; neither is dispatched until the names are fixed.

It is **not** recursive: per-package `.claude/agents/` directories in a monorepo are out of scope. From each match it reads **only the socket fields** it mechanically needs — `reviewLane`, `lane`, `appliesTo`, `required` — and keeps every file whose `reviewLane` is boolean `true`.

**Read only the socket fields — never the `description` or body at discovery.** `/code-review`'s contract requires the orchestrator's pre-dispatch reads to stay shallow so plan/intent framing can't leak into the `blind-spot-finder` synthesis. A reviewer's `description` is intent-laden and is **not needed to dispatch** — do not read it. (The `appliesTo` globs do leak a little domain signal, e.g. "this diff touches `billing/`"; that's mechanically unavoidable and bounded. The free-text `description` is not, so it stays un-read.)

The `*-reviewer.md` filename is only a discovery hint; **`reviewLane: true` is the actual opt-in.** This matters because the same glob also matches a project's *overrides* of the plugin's own `plan-reviewer` / `spec-reviewer` agents (which a project may legitimately drop into `.claude/agents/` to tighten tool access). Those overrides must **not** carry `reviewLane` — without the marker they are correctly ignored as code-review lanes. The marker is load-bearing; honor it strictly.

**Dispatch registration caveat (honest about the harness).** Claude Code registers project agents from `.claude/agents/` for `Task` dispatch by their bare `name`. This works transparently when the target repo *is* the session's project (the common `planningRoot: "."` case) and for user-level agents. When planning artifacts live in an external repo and the target repo is a different directory, that repo's agents may not be registered for dispatch — see Failure domains for how that is reported (precisely, and without pretending to know the cause).

## Frontmatter fields

A project reviewer is an ordinary Claude Code agent file. The socket adds four fields, only the first of which is required.

| Field | Required | Meaning |
|---|---|---|
| `reviewLane` | **yes** | Boolean `true` marks this agent as a review lane. The only required socket field. |
| `appliesTo` | no | List of globs matched against the changed file paths. Controls **whether/when** the lane is dispatched. |
| `lane` | no | A lane class. Controls **what inputs** the lane receives and **how it is grouped** in synthesis. |
| `required` | no | Boolean `true` means: if this lane is discovered but does not successfully run, the overall verdict is forced to **BLOCKED**. Default `false`. |

`appliesTo` and `lane` are **independent axes** — one gates dispatch, the other governs inputs and grouping. They do not interact.

**`reviewLane` truthiness is strict.** Only a value that **parses to YAML boolean true** enables a lane; `true` is the documented form — write that. (Whether bare `yes`/`on` parse to boolean depends on the YAML version — 1.1 says yes, 1.2 says no — so don't rely on them.) A present value that parses to anything else (`"true"`, `1`, an arbitrary string) is a likely authoring mistake: it is reported as **Malformed**, never silently dropped.

**A project lane's bare `name` must not equal a built-in's** (`drift-detector`, `quality-scanner`, `spec-compliance`, `blind-spot-finder`). A collision is reported as Malformed and the lane is not dispatched — otherwise it could shadow a built-in's identity and poison the synthesis sections that key on it. Two discovered files **in the same location** that resolve to the same bare `name` are likewise Malformed (ambiguous dispatch); a cross-location duplicate is resolved by precedence instead — see Discovery above.

### `appliesTo` — whether/when the lane runs

- **Present (non-empty list):** the orchestrator runs the lane only if at least one glob matches a changed file path. Cheap pre-filter; saves spawning a lane that has nothing to do.
- **Absent:** the lane is **always dispatched**, and the agent is expected to determine its own relevance (and no-op if nothing it cares about changed). "It knows how to gather its own context." Prefer `appliesTo` gating over always-on — always-on lanes add latency to every review.
- **Empty list (`[]`):** matches nothing — the lane is always **Skipped**. (If you mean "always run," omit the field.)
- **A scalar string instead of a list** (`appliesTo: "**/*.sql"`) is coerced to a one-element list and a note is emitted; authors should write a list.

**Glob semantics (pin these — ambiguity here causes silent Skips):**
- Dialect: **minimatch with globstar** (`**` spans directories). `*` and `?` do not cross `/`.
- Anchoring: globs match **repo-root-relative POSIX paths**, matched **verbatim** against the name-only listing. A bare `*.sql` matches only root-level files; use `**/*.sql` to match at any depth, and `**/migrations/**` (not `migrations/**`) to match a `migrations/` dir at any depth.
- Case-**sensitive**.
- The name-only listing is produced with the working directory set to the **target repo path** (not the planning root), so paths are target-repo-relative. For renames, the **new** path is matched. See "Getting changed file paths" below for the VCS commands and Perforce depot-path normalization.

When a lane is **Skipped**, the report shows the changed paths it was tested against, so an author can see *why* a match failed (a Skip is a silent coverage hole otherwise).

### `lane` — what inputs it gets, and how it groups

`lane` is matched **exact-lowercase after trimming whitespace**. A value that differs from a reserved word only by case or whitespace (`Code`, `code `) is treated as a likely-typo and **warned** — because silently demoting an intended `code` lane to a self-gathering standalone lane would quietly strip the plugin-enforced isolation the author meant to opt into.

| `lane` value | Inputs the orchestrator hands it | Isolation enforced by |
|---|---|---|
| `code` (recognized) | Base inputs only — same bundle as `quality-scanner`. No plan/spec/design. | **Plugin** |
| `spec` (recognized) | Base inputs + spec paths + design paths — same as `spec-compliance`. No plan/phase. | **Plugin** |
| `plan` (recognized) | Base inputs + plan path + phase doc + prior debrief paths — same as `drift-detector`. No specs/designs. | **Plugin** |
| `diff-only` (recognized) | Base inputs and nothing else — same as `blind-spot-finder`. | **Plugin** |
| *absent* | Base inputs only. A **standalone lane**, on its own in synthesis. | **Project** (agent self-gathers any other context it wants) |
| *unrecognized* (e.g. `security`) | Base inputs only. **Grouped** with any other lanes that declare the same value. | **Project** |

**Base inputs** (every lane, built-in or custom, always gets these): target repo path, detected VCS label, resolved diff command.

**Recognized lanes** opt into a plugin-curated, intent-isolated bundle. The orchestrator hands them exactly what the matching built-in gets and no more, so the plugin's isolation guarantee covers them. A recognized-lane reviewer must treat its bundle as **exactly this and only this** — e.g. a `lane: spec` reviewer is not given the plan and must not assert claims about it. Synthesis demotes a recognized-lane reviewer's claims about artifacts it was not granted to **Questions**.

**Standalone / unrecognized lanes** get only the base inputs. The orchestrator has no curated bundle to hand them, so it hands nothing intent-flavored and the agent gathers whatever else it needs itself — it may read the plan, specs, repo history, or call MCP servers if its `tools:` allow. **Intent isolation for these lanes is the project's responsibility, not the plugin's.** This is a deliberate choice: a project that wants plugin-enforced isolation declares a recognized `lane`; a project that wants a self-directed reviewer leaves `lane` off and owns the consequences.

`code` / `spec` / `plan` / `diff-only` are **reserved** lane names. Any other value is free-form project namespace and is treated as an unrecognized (grouped) lane.

### Grouping unrecognized lanes

When two or more reviewers declare the **same unrecognized** `lane` value, they form one lane group:

- They are presented under a single heading in synthesis (e.g. a coherent "Security" section), not as disconnected one-off sections.
- They are treated as one logical unit in the dispatched-reviewers rollup.
- Agreement *between* members is **within-lane corroboration**, not independent cross-lane triangulation. For the "Confirmed by N reviewers" tally, **a lane group counts as one source** — two `security` peers agreeing is one corroborated finding from the Security lane, not "confirmed by 2 reviewers."

This lets a project ship, say, `security-sql-reviewer.md` and `security-api-reviewer.md` both with `lane: security` and have them read as one Security lane.

### `required` — let a project hard-gate on a lane

By default a project lane is best-effort: if it doesn't run, the review proceeds and the verdict is *degraded* but not blocked. A project that depends on a lane (a security or migration gate) can set `required: true`. Then:

- The four built-ins **still run and still synthesize** — a required lane's failure never aborts or fails the four. (This preserves the rule that an external lane's failure is not a failure of the core four.)
- But if a `required` lane is discovered and does **not** successfully run (failed to dispatch, errored, or was malformed), the overall **verdict is forced to BLOCKED** with a loud banner naming the lane. A green check can never appear over an un-run required gate.
- A required lane that is legitimately **Skipped** (its `appliesTo` matched no changed path) does *not* block — it had nothing to review.

## Read-only requirement

Review lanes **must be read-only.** They read the diff and the repo and emit findings; they do not modify files, stage changes, or run write-shaped commands. This is not cosmetic: all lanes dispatch in parallel and the built-ins resolve the diff against the live working tree, so a lane that writes during the review can shift the ground under `quality-scanner` and `blind-spot-finder` — the exact cross-lane contamination the four-lane design exists to prevent.

- The template ships a read-only `tools:` allowlist (`Read`, `Grep`, `Glob`, and `Bash` *for read-only inspection only* — no writes, no `git add`/`git commit`/`git checkout`, no formatters).
- The plugin cannot enforce another agent's tool list, so this is a convention plus a default. To reduce the blast radius, `/code-review` resolves the diff to a fixed base/head reference (a commit SHA where the VCS supports it) and hands all lanes that frozen reference, rather than re-deriving from a tree a peer might mutate.

## Failure domains

There are two, and they behave differently on purpose.

**Built-in four — fatal.** Governed by the `/code-review` hard contract: if any of the four fails to dispatch, `/code-review` stops with a loud error and does **not** self-synthesize. The four are the floor; the review is invalid without them.

**Project lanes — best-effort, never abort the four.** A project lane that fails for any reason is recorded and skipped; synthesis proceeds over the four built-ins plus whatever project lanes did return. Distinguish the outcomes precisely — the orchestrator read each lane's frontmatter at discovery, so it knows the file exists and what `name` it declared, and must not guess at causes it can't see:

| Outcome | Cause | Reported as |
|---|---|---|
| **Skipped** | `appliesTo` matched no changed path (or was `[]`) | Quiet note + the paths it was tested against (working as intended, not a warning) |
| **Failed to dispatch** | `Task` could not resolve the lane's bare `name` | Warning naming the discovered file and its declared `name`, stating the dispatch failed and that the cause is *either* a name/file mismatch *or* this repo's agents not being registered in this session. **Do not assert which.** |
| **Errored** | Dispatched but the agent threw | Warning, with the error |
| **Oversized** | Returned an unreasonably large report (rule of thumb: more than ~2,000 lines) | Truncated to the leading findings with a note; the truncated head is still synthesized. The lane counts as **run** and does not degrade the verdict. If truncation leaves no parseable findings, reclassify as **Errored** (which does). |
| **Malformed** | `reviewLane` present but not boolean `true`; `lane` of an invalid type; bare `name` collides with a built-in or another lane | Warning at discovery; lane not dispatched |

**Lane responsiveness is the project's responsibility.** The orchestrator dispatches lanes and waits for what returns — it does not impose a timeout. A lane that hangs or makes slow external calls will hold up the review; keeping a lane fast, read-only, and well-behaved is on the project, not the plugin. The socket just triggers them. (Always-on self-gathering lanes that make slow external calls are the usual cause of a sluggish review — another reason to prefer `appliesTo` gating.)

**Verdict degradation (so failure is never silent).** The Overall Verdict and critical-issue counts are computed from the four built-ins plus any *successfully returned* project lanes. On top of that headline:
- If any **declared** project lane did not successfully run (Failed to dispatch / Errored / Malformed — *not* Skipped, and *not* Oversized: a truncated lane still ran), the verdict carries a mandatory **`(DEGRADED — N declared lane(s) did not run: <names>)`** suffix.
- If a **`required`** lane did not run, the verdict is forced to **`BLOCKED — required lane <name> did not run`**, overriding any otherwise-passing headline.
- A purely **Skipped** lane does not degrade anything — it had no matching changes.

A project-lane failure never marks the four built-ins failed or the review *incomplete in the four-lane sense*; it degrades or blocks the *verdict* so the gap is visible.

## Getting changed file paths (VCS-aware, paths only)

`appliesTo` matching needs the *names* of changed files, not their contents. Use the VCS-appropriate name-only listing, run with the working directory set to the **target repo** so paths are target-repo-relative. This is paths, not hunks, so it stays inside `/code-review`'s rule against reading diff content in the primary context.

| VCS | Changed-path listing | Notes |
|---|---|---|
| `git` / `git-worktree` | `git -C <target> diff --name-only <base>..<head>` (plus `git -C <target> status --short` for working/staged paths if in scope) | For `status --short`, strip the 2-char status code; for a rename line `R old -> new`, match the **new** path. Paths are repo-root-relative POSIX. |
| `perforce` | `p4 opened` (unsubmitted); `p4 describe -s <change>` (summary, file list only) for submitted changelists | These emit **depot** paths (`//depot/proj/...`). Normalize to client/repo-relative paths with `p4 where` before matching `appliesTo`, so author globs like `**/*.sql` behave as expected. |
| `none` | No changed-path list available | `appliesTo`-gated lanes are Skipped; `appliesTo`-absent lanes still dispatch. |

See `shared/vcs-detection.md` for detection and the full operations table.
