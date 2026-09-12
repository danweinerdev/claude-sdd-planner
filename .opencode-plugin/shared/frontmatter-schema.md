# Frontmatter Schema

Single source of truth for all artifact metadata in this project.

## Sensitive Data — never captured in artifacts

SDD artifacts are committed, shared, and often pushed to remotes. No artifact — frontmatter, body, evidence table, pasted command output, or review finding — may capture:

- **Credentials of any kind**: tokens, API keys, passwords, connection strings, signed URLs
- **Machine-specific absolute paths** that identify a user or host: `/home/<user>/...`, `/Users/<user>/...`, `C:\Users\...`
- Private hostnames, internal IPs, usernames, email addresses, or customer data that the target repo itself doesn't already publish

Path form, in order of preference: **relative to the repo or planning root** (`.`, `./services/api`, `Specs/Auth`) — the default for all artifact content; `~/...` or `$HOME/...` where a path outside the current root is genuinely needed (both are generic — they name no user); literal `/home/<user>`-style prefixes never. Machine-specific absolute paths belong only in the gitignored `planning-config.local.json`.

When pasting command output as evidence, scrub what the tool printed (replace the repo root with `.` or the home prefix with `~`); the observable result — exit status, counts, assertions — is the evidence, not the noise around it. Where the completion-evidence contract requires a resolvable repository identity, `~/...` is the accepted form (see `shared/completion-evidence.md`). No hygiene pass reliably catches leaks after the fact — the rule is enforced at write time, by every skill and agent that produces an artifact.

## Common Fields

Every artifact includes these fields (one exception: `phase` docs omit `tags` and `related` — they inherit the plan's):

```yaml
title: "Human-readable title"
type: research | brainstorm | spec | design | plan | phase | debrief | review
status: <type-specific, see below>
created: YYYY-MM-DD
updated: YYYY-MM-DD
tags: [tag1, tag2]
related: [Specs/FeatureName, Research/topic-slug.md]
```

`related` entries are planning-root-relative: use the **directory** path for specs, designs, and plans (`Specs/FeatureName`, `Designs/ComponentName`, `Plans/PlanName`), and the **file** path for flat artifacts (`Research/topic-slug.md`, `Brainstorm/topic-slug.md`). Legacy `Retro/YYYY-MM-DD-slug.md` and `Diagrams/slug.md` references remain valid for read compatibility; artifacts of the retired `retro` and `diagram` types are ignored by validation — they still resolve as references but are never checked and are no longer created. Consumers that need the document behind a directory entry append `/README.md`.

Any artifact may additionally declare an optional `refresh_when` field — a list of event-shaped trigger descriptions that force a refresh (e.g., `refresh_when: ["dependency X ships v3", "Specs/Payments changes", "vendor answers the webhooks question"]`). A fired trigger makes the artifact stale regardless of its `updated` date (lifecycle skills honor known-fired triggers); demonstrably-unfired triggers exempt it from the default 30-day staleness rule.

## Status Values by Type

| Type | Statuses |
|------|----------|
| research | `draft`, `active`, `archived` |
| brainstorm | `draft`, `active`, `archived` |
| spec | `draft`, `review`, `approved`, `implemented`, `superseded` |
| design | `draft`, `review`, `approved`, `implemented`, `superseded` |
| plan | `draft`, `approved`, `active`, `complete`, `archived` |
| phase | `planned`, `in-progress`, `complete`, `blocked`, `deferred` |
| task | `planned`, `in-progress`, `complete`, `blocked`, `deferred` |
| debrief | `draft`, `complete` |
| review | `open`, `resolved`, `superseded` |

## Plan Schema

### Plan README.md

```yaml
---
title: "Plan Title"
type: plan
status: active
created: YYYY-MM-DD
updated: YYYY-MM-DD
tags: [tag1, tag2]
related: [Specs/FeatureName, Designs/ComponentName]
phases:
  - id: 1
    title: "Phase Title"
    status: planned
    doc: "01-Phase-Title.md"
  - id: 2
    title: "Phase Title"
    status: planned
    doc: "02-Phase-Title.md"
    depends_on: [1]
---
```

Body contains: Overview, Architecture, Key Decisions, Dependencies, Open Questions (omit when empty — a plan cannot be `approved` while an in-scope question is unanswered).
No status tables in the body — consumers read phases from frontmatter.

### Phase Doc (01-Phase-Title.md)

```yaml
---
title: "Phase Title"
type: phase
plan: PlanName
phase: 1
status: in-progress
created: YYYY-MM-DD
updated: YYYY-MM-DD
deliverable: "What this phase delivers"
tasks:
  - id: "1.1"
    title: "Task title"
    status: planned
    verification: "How we know this task is good and complete"
  - id: "1.2"
    title: "Task title"
    status: planned
    depends_on: ["1.1"]
    verification: "Specific criteria to confirm correctness"
---
```

#### Task Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Task identifier: `<phase>.<digits>` with an optional single lowercase letter suffix — `1.1`, `1.12`, `1.2a`. The suffix exists so a task can be inserted between `1.2` and `1.3` without renumbering the tasks after it; renumbering would break the append-only identity that completion evidence, completed-task-identity roll-ups, and frozen reviews all cite. Ids are **opaque**: the suffix orders and identifies, it carries no meaning, and `1.2a` is not "a sub-part of 1.2" |
| `title` | yes | Human-readable task title |
| `status` | yes | Task status (see status values above) |
| `depends_on` | no | List of task IDs this task depends on |
| `verification` | yes | How we know the work is good and complete — name each new or changed behavior to cover, not test counts. Where the check is commandable, include the exact command and expected observable output (e.g., `cargo test auth:: — 14 pass incl. the new refresh-expiry case`); prose-only criteria are for behavior no command can observe |
| `justifies` | yes | Why this task exists — the demand that motivates it, not what it does. Either cite the ids it serves (`FR-NN`, `NFR-NN`, `AC-NN`, `DD-N`, `pd-<hex>`, `<review qualifier>:F-NN`) or name the concrete failure it prevents (e.g., "prevents silent data loss when a partial write is retried"). `verification` says how we know it is done; `justifies` says why it should be started. Restating the title, or a placeholder like "required for completeness", "might need it later", or "part of the architecture", does not justify a task — an unsourced task is cut, not annotated |

Body contains task detail sections keyed by task ID as headings:

```markdown
## 1.1: Task Title

### Subtasks
- [ ] Subtask one
- [ ] Subtask two

### Notes
Implementation notes...

### Trap
Optional — only for tasks with a known tempting-but-wrong shortcut. Names
the shortcut a hasty implementer would take and why it's wrong. /implement
passes it verbatim to the implementer's dispatch.
```

## Stable Identifiers & Traceability

Numbered elements carry stable, per-document identifiers so artifacts can cite each other precisely and reconciliation is greppable:

| Element | Id format | Lives in |
|---|---|---|
| Functional requirement | `FR-NN` | spec Requirements |
| Non-functional requirement | `NFR-NN` | spec Requirements |
| Acceptance criterion | `AC-NN` | spec Acceptance Criteria |
| Phase / task | `N` / `N.M` | plan frontmatter (existing convention) |
| Design decision | `DD-N` | design (`## Design Decisions`) |
| Plan decision | `pd-<hex>` | `Plans/<Name>/<Name>-Decisions.json` |
| Review finding | `F-NN` | review artifact |
| Review follow-up | `FU-NN` | review artifact |

Rules:

- **Ids are append-only and never renumbered.** Removing an item leaves its id retired (strike the line or note "removed — see <reason/citation>") so existing cross-references never silently re-bind to a different item.
- **Cross-artifact citations are qualified.** An id belonging to *another* artifact is written `<ArtifactName>:FR-NN` (e.g. `ProductSystemV2:FR-23`, `ArkBootstrapApi:DD-4`). The qualifier exempts it from local resolution — both `sdd apply` (SPK040) and `sdd validate` (SDD122) — while keeping the reference greppable; never backtick-escape an external reference, that drops it from the link graph. The space-separated form (`ArkBootstrapApi DD-4`) is also accepted when the qualifier reads as an artifact name, but the colon form is canonical and is what the templates and skills write.
- **Supersession is checked, both ways.** A spec, design, or plan carrying `superseded_by` must have `status: superseded`, and vice versa (SDD178) — use `sdd spec|design supersede <path> --by <successor>`, which sets both atomically and requires the successor to exist. A **live** artifact that `related`-links a superseded spec/design/plan is an error (SDD179): planning work against retired intent is drift that looks correct until it ships. Relate the replacement, or retire the citing artifact too.
- **Retired artifacts do not block.** Findings on a `superseded` or `archived` artifact are reported as `waived` rather than `error`: history is not editable, and holding it to the current schema buries the findings on live work. The supersession rules themselves stay errors, otherwise the retirement metadata could never be corrected.
- **Where each family resolves.** `FR-NN`/`NFR-NN`/`AC-NN` resolve against the **specs** reachable through the citing artifact's `related` graph; `DD-N` resolves against the **designs** on that same graph. A design is both a citation source (it owns `DD`) and a hop on the way to the specs it realizes, so a plan related to a design can cite that design's decisions and the spec's requirements alike.
- **Design discovery is direct, never through a spec's back-link.** The `related` walk follows plan → plan/design/review hops and design → design/spec hops; a **spec is a terminal** — its own `related` (including the back-link it writes to the design that realizes it) is never traversed on another artifact's behalf. So a plan that cites `Designs/X:DD-N` must list `Designs/X` in its own `related`; relating only the spec that `Designs/X` realizes leaves the design unreachable and the citation refused (`sdd compile`, `sdd graph split`, and SDD122 agree, and the refusal names the design to relate). Following back-links would let every related spec silently widen the citation registry with its realizing designs and every spec those designs relate. The same walk is shared by the validator and the graph compiler, seeded once per artifact and cycle-safe.
- **Design-side coverage is scoped to what a design realizes (SDD161).** A design declares the specs it realizes through its own `related` specs or a spec's `related` back-link to it; a design that declares neither is held to every spec on the plan's demand. Each realizing design's citations resolve with qualified identity — `Channels:FR-01` counts for the channels spec only, and a bare `FR-01` in a design that realizes two specs both defining it counts for neither (ambiguous, the same verdict the compiler gives a bare node citation). A plan may therefore relate two specs with overlapping id ranges and a design that realizes only one of them: the design is held to that spec's requirements, and the other spec — realized by no related design — puts nothing on the design-side demand. This is scoping, not waiver: a design that declares a spec and fails to cite one of its requirements still fails.
- **Cross-reference by id.** A plan task's `verification` (or its body section) names the `AC-NN`/`FR-NN` ids it satisfies; a design section that realizes a requirement cites its `FR-NN`; a graph node's `justifies` cites a plan decision (`pd-<hex>` or `<Plan>:pd-<hex>`) per `shared/decision-log.md`, or a frozen review finding (`<review qualifier>:F-NN`) for an `extend` amendment. These citations are what make drift detectable — without them every reconciliation check is blind.
- **Changing a numbered element is a reconciliation event**: after editing it, grep the other artifacts for its id and update or flag every citing site. A plan decision is never edited — a change of mind is a fresh entry with `supersedes`, per `shared/decision-log.md`. `sdd-validate` audits for unnumbered elements and dangling id citations.

## Graph Node Roles and Amendment Fields

A plan's compiled graph node carries a `role` field (proposal input): `implementation` (default when absent), `shared-mechanism`, `review`, or `integration-acceptance`. A `review`-role node's `gate.type` must be `review`; a `review`-gate node must have `role: review`. `contract_rev` (an integer, starting at 1) and `origin` (present only on a node added by an `extend` amendment: `{ review: <artifact path>, finding: <id> }`) are tool-owned — refused in proposal payloads, like `intent_hashes` and `claim`.

A review artifact's `findings[]` entries add an `action` field, required whenever `status: open`:

```yaml
findings:
  - id: F-03
    status: open
    action: revise
    nodes: [manifest-query]
    revise:
      gate: { type: tests, tests: [TestManifestQuery_ExcludesArchived] }
  - id: F-04
    status: open
    action: extend
    node:
      id: manifest-query-audit-refusal
      contract: "Emit an audit event when a manifest read is refused for tenant mismatch."
      deps: [manifest-query]
      gate: { type: tests, tests: [TestManifestQuery_AuditOnRefusal] }
      artifacts: [internal/catalog/manifest_audit.go]
```

`action: revise` names at least one node in the reviewing node's scope in `nodes` and changes at least one of `contract`/`gate`/`justifies`/`inputs` under `revise:`. `action: extend` supplies a proposal-shaped node fragment under `node:`, whose `deps` must include a node in the reviewing node's scope; its `justifies` is filled by the binary with the qualified finding citation. See `shared/review-artifacts.md` for the full contract.

## Review Artifact Schema

A review artifact (`<target-home>/reviews/…`, type `review`) carries `findings[]` and `followups[]` frontmatter arrays — the machine layer for review tracking. Entry fields, location/naming, the Resolution Log, disposition rules, and follow-up tracking are defined in `shared/review-artifacts.md` — the single source of truth for this artifact.

Per-finding statuses (entry-level, not artifact statuses): `open`, `fixed`, `deferred`, `rejected`, `answered`. The artifact is `resolved` only when no finding is `open`; `superseded` links to the newer review of the same target.

A phase-completion review additionally requires `review_scope: phase`,
`frozen: true`, `verdict: Aligned`, and `review_mode` of `independent`, `mixed`,
or `single-agent`. `frozen` starts `false` at scaffold time and is set to
`true` only by `sdd review resolve`, atomically with `status: resolved` — the
required values here describe the resolved review the phase gate reads. Its `lane_results` is exactly four mappings, one for every
stable lane: `review_plan_drift`, `review_quality`, `review_spec_compliance`,
and `review_blind_spots` (the data-layer identifiers for `drift-detector`,
`quality-scanner`, `spec-compliance`, and `blind-spot-finder`). Each mapping has
`lane`, `result: PASS/Aligned`, `reviewed_identity` exactly equal to the
review's `rev`, and nonempty `evidence`. It also requires
`reviewed_planning_revision`: the full planning-Git commit at which the phase
and plan README were reviewed — the validator loads both artifacts at that
native revision and compares lifecycle-normalized content to the current
artifacts, allowing lifecycle-only changes. The complete example and
Git-specific frozen-identity adapter are in `shared/review-artifacts.md`.

## Plan Decisions Schema

A plan's decisions file, `Plans/<Name>/<Name>-Decisions.json`, is a flat JSON array — not a frontmatter-bearing artifact. Entry fields (`id`, `date`, `statement`, optional `supersedes`, optional `source`), the write protocol, cross-plan citation, and conflict reconciliation are defined in `shared/decision-log.md` — the single source of truth for this file.

## Debrief Schema

Debriefs live at `Plans/<PlanName>/notes/<NN>-Phase-Name.md` and add three fields to the common set:

```yaml
---
title: "Phase N Debrief: Phase Title"
type: debrief
status: complete        # draft while being written incrementally
plan: PlanName          # the plan directory name
phase: 1                # the phase number this debrief covers
phase_title: "Phase Title"
created: YYYY-MM-DD
updated: YYYY-MM-DD
tags: []
related: []
---
```

## Status Color Mapping

Inline Mermaid diagrams in artifacts use this status styling (`classDef` colors); frontmatter-reading tools may adopt the same mapping:

- `complete` / `approved` / `implemented` -> green
- `in-progress` / `active` / `review` -> amber
- `planned` / `draft` -> gray
- `blocked` -> red
- `deferred` / `archived` / `superseded` -> muted

(Decision-ledger *entry* statuses are not artifact statuses; their rendering mapping lives in the Decision Ledger Schema section above.)

## Accepted Exceptions (`waivers`)

A validator gate with no exception path gets bypassed some other way — by
loosening the rule for everyone, by hand-editing artifacts until the check
passes, or by dropping the validator from CI. Each of those trades a narrow,
attributable exception for a broad, silent one. `waivers` makes the narrow one
the cheapest honest option.

```yaml
waivers:
  - code: SDD173
    reason: "Why this finding does not apply here, in a sentence."
    accepted: "2026-08-12"   # optional
```

**Waiving is never quieter than fixing.** A waived finding is still reported —
as `WAIVED`, with its reason — and the run headline says `N waived`. It stops
making the root invalid; it does not stop being visible.

Rules the mechanism enforces on itself:

| Constraint | Why |
|---|---|
| Waivers live in the artifact's own frontmatter | Attributable to a commit and an author, greppable, visible in review. There is deliberately no config-file or CLI form — either would let a pipeline silence findings without changing a tracked file. |
| A waiver is scoped to its own artifact and code | An exception's blast radius should be readable from the file it is written in. |
| A reason is required and must be substantive | An unexplained exception is what this mechanism exists to prevent. Placeholders (`TBD`, `n/a`) and too-short text are refused — **SDD176**. |
| Parse-stage codes (SDD002–007) cannot be waived | The validator could not model the file, so every rule below it silently did not run. Waiving that asserts an unreadable artifact is fine — **SDD176**. |
| An unknown code cannot be waived | A typo or retired code yields a permanent no-op — **SDD176**. |
| A waiver matching nothing is reported | Stale exceptions accumulate into a standing suppression list nobody re-reads, and would silently excuse the finding if it returned — **SDD177**. |

`sdd validate --no-waivers` ignores the mechanism entirely and reports the
unexcused state. Use it in release gates and audits; the waivered run is the
day-to-day signal.

Waivers are excluded from lifecycle normalization, so declaring one does not
invalidate the phase review that surfaced the finding. Every other byte is
still compared — a scope edit made in the same commit is still caught.

A plan's decisions file has no waiver mechanism: it is append-only content
addressed by digest, so there is no id-sequence or ordering condition to
excuse — malformed entries and competing successors (SDD190, SDD192) are
refused and reconciled, not waived.
