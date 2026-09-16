# Review Artifacts

Single source of truth for **persisted review artifacts** — the durable, trackable record of adversarial and code reviews. Every `/poke-holes` and `/code-review` run writes its findings to a review file instead of leaving them in conversation scrollback, and every finding is driven to an explicit disposition in a Resolution Log. Findings that live only in a chat transcript get lost; findings with ids in a tracked file do not.

## Location — reviews live with what they review

Precedence: **Plans > Designs > Specs.** The review file goes in a `reviews/` directory beside the reviewed artifact's README:

| Reviewed material | Review home |
|---|---|
| Implementation vs a plan (`/code-review`) | `Plans/<Plan>/reviews/` |
| A plan or phase (`/poke-holes`) | `Plans/<Plan>/reviews/` |
| A design | `Designs/<Component>/reviews/` |
| A spec | `Specs/<Feature>/reviews/` |
| A flat artifact (brainstorm, research) | the nearest governed home via its `related` frontmatter (one hop, same precedence); if none resolves, present findings inline and say the review was not persisted |

Create the `reviews/` directory on first use (with a `.gitkeep`-style placeholder only if the VCS needs one).

## Naming

```
<NN>-<target-slug>-<review-type>-<rev>.md
```

- `NN` — next zero-padded sequence number within that `reviews/` directory.
- `target-slug` — lowercase kebab-case of the reviewed artifact (plan/design/spec name).
- `review-type` — `adversarial-review` (`/poke-holes`) or `code-review`.
- `rev` — the state the review examined: for `/code-review`, the reviewed repo's VCS short revision (append `-dirty` when the tree wasn't frozen); for `/poke-holes`, the planning root's short revision, falling back to `YYYY-MM-DD` when the planning root isn't versioned.

Example: `01-arkagent-adversarial-review-a1b2c3d.md`.

## File Format

Template: `shared/templates/review.md`. Frontmatter is type `review` with a machine-readable `findings[]` array (same structured-list convention as `phases[]`/`tasks[]`/`decisions[]`):

```yaml
findings:
  - id: F-01                 # stable within this file; never renumbered
    severity: critical       # critical | major | minor | question
    title: "One-line finding"
    status: open             # open | fixed | deferred | rejected | answered
```

The body carries one section per finding — the concrete scenario, why it matters, the recommended mitigation, and the artifact/code ids it impugns (`FR-NN`, `AC-NN`, task `N.M`, `pd-<hex>`) — followed by the Resolution Log. Findings and lane evidence follow `shared/frontmatter-schema.md` § Sensitive Data: repo-relative paths, no credentials, no `/home/<user>`-style absolute paths in pasted output.

### Findings against a graph plan's review node

When the reviewed target is a graph plan's `review`-role node (`sdd graph review`), every finding with `status: open` additionally carries an `action`, one of `revise` or `extend`. Missing or unknown `action` on an open finding refuses the whole artifact.

**`revise`** — the reviewed node's promise was wrong or unproven. Names the affected node(s) and the normative change:

```yaml
findings:
  - id: F-03
    severity: major
    title: "Manifest query ignores archived variants"
    status: open
    action: revise
    nodes: [manifest-query]
    revise:
      gate: { type: tests, tests: [TestManifestQuery_SelectedPipelineVariant, TestManifestQuery_ExcludesArchived] }
```

`nodes` must name at least one node in the reviewing node's scope, and `revise:` must change at least one of `contract`, `gate`, `justifies`, or `inputs`. A revise that changes nothing is refused — a finding with no graph consequence is a comment, and belongs in the body with `status: answered` or `rejected`.

**`extend`** — the reviewed node's promise held but something is missing. Proposes a new node sourced by the finding:

```yaml
findings:
  - id: F-04
    severity: minor
    title: "No audit event on cross-tenant read refusal"
    status: open
    action: extend
    node:
      id: manifest-query-audit-refusal
      contract: "Emit an audit event when a manifest read is refused for tenant mismatch."
      deps: [manifest-query]
      gate: { type: tests, tests: [TestManifestQuery_AuditOnRefusal] }
      artifacts: [internal/catalog/manifest_audit.go]
```

`node` is a proposal-shaped node fragment (same strict decoding as `sdd graph propose`); its `deps` must include at least one node in the reviewing node's scope. `justifies` is filled by the binary with the qualified finding citation (`<plan-relative artifact path>:<finding id>`, e.g. `Reviews/2026-09-11-catalog-read-path.md:F-04`), which the citation index resolves once the artifact is frozen.

Findings with `status: fixed | deferred | rejected | answered` produce no amendment; `deferred` is recorded in the review node's `history`. `sdd graph review` with zero open findings records a pass; with any open finding it writes nothing and instead prints the amendment preview plus an `expect-digest` for the graph and an `expect-report-digest` for the reviewed artifact. Pass both to `sdd graph amend`; if either changes, re-preview rather than applying stale findings. See `commands/implement/SKILL.md` for the full claim → review → amend flow.

A passing graph review observation binds the reviewed scope and each reviewed node's `contract_rev` and observation sequence. A scope change, reviewed revision advance, or newer reviewed observation makes that review node stale. Declared artifacts and inputs remain visible to reviewers but are not hashed into the binding. Re-verification is a deliberate new review, never an automatic consequence of file changes.

Artifact `status`: `open` while the review is being written; `resolved` when the closing gate holds; `superseded` when a newer review of the same target replaces it (link both ways). Two verdicts resolve: **`Aligned`**, where every finding has a terminal disposition (`fixed`, `deferred`, `rejected`, `answered`) — the only verdict that completes a phase or greens a review node; and **`Amend`**, where every `open` finding carries an `action` (`revise` or `extend`) and at least one is open — the frozen findings report `sdd graph amend` consumes. An open finding without an action never resolves under either verdict, and a review whose findings would need both shapes is two reviews.

## Phase-completion review gate

When a review is used to complete a phase, it is a persisted final gate, not a
general advisory review. Freeze a concrete native-SCM phase revision/range, run
all four `/code-review` lanes, and record these frontmatter fields (this is the
**resolved** end state — see the lifecycle below for how a review gets here):

```yaml
review_scope: phase
frozen: true
verdict: Aligned
reviewed_planning_revision: "<full planning Git commit reviewed>"
review_mode: independent # independent | mixed | single-agent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "<exact rev>"
    evidence: "<nonempty auditable result>"
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "<exact rev>"
    evidence: "<nonempty auditable result>"
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "<exact rev>"
    evidence: "<nonempty auditable result>"
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "<exact rev>"
    evidence: "<nonempty auditable result>"
```

The `lane` values are **stable lane identifiers** — the data-layer names the
validator checks. They map to the plugin's agents: `review_plan_drift` →
`drift-detector`, `review_quality` → `quality-scanner`, `review_spec_compliance`
→ `spec-compliance`, `review_blind_spots` → `blind-spot-finder`. Frontmatter
always uses the stable identifiers, whatever agent ran the lane.

`lane_results` is the auditable four-lane record: it contains exactly these four
lanes once, every `reviewed_identity` exactly equals `rev`, and every evidence
value is a specific concrete observation (not blank or a generic conclusion
such as `passed`, `ok`, `aligned`, `success`, `No findings`, or `No blocking
findings`). Record inspected paths, behaviors, or observations even when a
lane is clean. `review_mode` records how the lanes ran: `independent`
(fresh-context agents), `mixed`, or `single-agent`.

Each lane's `result` is one of two truthful tokens, chosen by what that lane
actually found: `PASS/Aligned` when it found nothing that needs a change, or
`CHANGES/Amend` when it flagged material the review is reporting. Under
`verdict: Aligned` every lane must be `PASS/Aligned` — the phase-completion
gate's strict all-pass requirement never relaxes. Under `verdict: Amend` a
lane may report either token, but at least one lane's `CHANGES/Amend` (with
an open, actioned finding backing it) is what makes the report an Amend
rather than a no-op; all-pass lanes with every finding already closed is
refused as an Aligned review misfiled under the wrong verdict. `sdd review
evidence set --result pass|changes-required` writes the corresponding token
alongside the lane's evidence — see the lifecycle below.

**Review lifecycle — freeze at resolution, never at birth.** The review is a
transition chain driven by the binary, mirroring `task|phase|plan complete`:

1. `sdd review scaffold <phase-path> --frozen <base>..<endpoint>` creates the
   artifact with `status: open` and `frozen: false`, each lane's evidence a
   placeholder the validator refuses. Open means writable: lane evidence goes
   in via `sdd review evidence set`, findings and Resolution Log entries via
   the normal write path (`apply` / `section set`).
2. `sdd review evidence set <review-path> --lane <id> [--evidence TEXT]
   [--result pass|changes-required]` records what one lane actually observed
   and, optionally, whether it passed or flagged material (default: leaves
   the lane's current result token untouched). It enforces the same
   evidence-quality bar as the validator: a placeholder, a blank, or a
   conclusory "no findings" is refused at write time.
3. `sdd review resolve <review-path>` is the closing transition. It refuses
   unless every lane carries real evidence and a result token valid for the
   declared verdict, the schema is valid, no follow-up floats untracked
   (`--accept-followups` only after the user explicitly accepts one), and the
   findings match the verdict: `Aligned` needs every finding terminal;
   `Amend` needs every open finding classified `revise` or `extend` with its
   `nodes`/`revise` or `node` block, and at least one open. It also refuses a
   review that carries a `frozen:` field (meant to gate a phase or a graph
   review node) but no reviewed range in `rev` — resolving it unfrozen would
   silently strand it, since `sdd graph amend`/`sdd graph review` require
   `frozen: true` to consume it. When the gate is met it sets `frozen: true`
   and `status: resolved` in one write. A phase completes only on an
   `Aligned` review; an `Amend` review feeds `sdd graph amend` and the
   re-review that follows is a fresh artifact.

**Review complete versus implementation accepted.** These are different
claims, and a frozen review only ever makes one of them:

- **Review complete** (frozen `Amend`): the findings are finalized — every
  open finding is classified `revise`/`extend` with its graph consequence —
  but one or more lanes may report `CHANGES/Amend`, and nothing about the
  reviewed work is accepted. The review exists to drive `sdd graph amend`,
  not to close anything.
- **Implementation accepted** (frozen `Aligned`): every lane reports
  `PASS/Aligned` and every finding has a terminal disposition. This is the
  only verdict that completes a phase (the strict all-pass phase-completion
  gate above) or greens a graph review node.

`frozen: true` therefore marks a *finished* review: from that moment the
artifact is immutable through every supported command (SPK050 refuses
rewrites, and `review scaffold --force` refuses to replace it). Material
changes after resolution never edit the frozen review — they supersede it with
a fresh scaffold of the new frozen range.

`reviewed_planning_revision` is required and is the full Git commit in the
planning repository at which the phase and plan README were reviewed. Before
phase completion, the validator loads both artifacts at that commit and compares
their lifecycle-normalized content with current artifacts. It permits only
lifecycle fields, completion evidence, and checklist state to change in those
two documents. Every other planning-root artifact — specs, designs, plan decisions files,
other phase docs, the review itself — is lifecycle after the frozen endpoint
and rides in the phase-close commit under `shared/autonomy.md` § SCM boundary cadence. This
binding uses the planning SCM identity directly; SDD stores no custom intent
hashes. A planning SCM without this validated adapter keeps the phase
non-complete with an explicit diagnostic.

The phase's `Final aligned review` evidence points to this artifact and its
frozen identity. `Needs changes` or `Blocked` forbids phase completion. Every
review-driven material code fix must receive a new planned task id and be
implemented as a complete, reviewable task revision, even when it is small.
Any material change after review (behavior, public contract,
architecture, security, concurrency, persistence, error handling, acceptance
coverage, or meaningful test logic) supersedes the review and requires a fresh
full four-lane review of the new frozen range. Repeat until the final review is
`Aligned` and its reviewed state is materially unchanged.

Use the exact phase-evidence syntax `- Final aligned review: <artifact path>;
frozen: <revision/range>`; the nonempty `frozen` value must exactly equal this
artifact's `rev`. **Git review-identity adapter:** `rev` and `frozen` are only an
exact `<full40>..<full40>` range with distinct endpoints; every named commit must
exist in the target repository, the base must be an ancestor of the endpoint, and
the endpoint must exactly equal the phase evidence's clean full `Revision /
checkpoint` commit. **Git lifecycle adapter:** the exact cited artifact must be
committed at planning-root `HEAD`, and the committed bytes/frontmatter must
still establish resolved status, this phase's `review_of`, `rev`, frozen phase
scope, `Aligned`, review mode, and all four lane results. These are adapter
rules, not universal SCM assumptions: no deterministic non-Git target
review-identity adapter or validated Perforce/no-SCM lifecycle adapter is
currently available, so those cases must leave the phase non-complete with the
adapter diagnostic.

## Resolution Log

When findings are acted on, append (never rewrite) entries under `## Resolution Log` at the bottom of the review file, one per finding disposition:

```markdown
### F-03 — fixed (2026-07-17)
Split task 2.4's migration into its own task 2.7 with a rollback step.
Governing fact: AC-04 requires zero-downtime cutover. Commit: abc1234.
```

- Every entry states **what was decided, what was done**, and — for `deferred`/`rejected` — **why**. Cite the governing facts by id (`pd-<hex>`, `FR-NN`, `AC-NN`, task ids, commits).
- Update the finding's `status` in `findings[]` to match; the frontmatter is the machine layer, the log entry is the narrative.
- Dispositions: `fixed` (change applied), `deferred` (tracked follow-up — see below), `rejected` (won't fix, rationale required), `answered` (a question resolved; if the answer constrains future work, it belongs in the plan's decisions file too — `shared/decision-log.md`).

## Acting on findings — the disposition rules

Classify each finding before touching anything:

- **Mechanical fix — apply directly.** The correction is fully determined by *hard facts*: a standing plan decision, the explicit text of an approved spec/design/plan, or an objectively verifiable fact (a path exists, a command's output, a pinned external contract). No judgment call remains. Apply the fix, cite the governing fact in the Resolution Log entry. This is a template-following write per `shared/autonomy.md` — no user stop.
- **Design decision — stop and discuss.** The fix requires choosing between viable approaches, changes the meaning or scope of an approved artifact, or touches anything a standing plan decision governs (or would supersede one). Present the options with trade-offs and let the user decide. When the outcome will bind work beyond the artifact being fixed, show the exact statement for approval and run `sdd decide add --plan <Name> --statement "..." [--supersedes <id>]` (`shared/decision-log.md`), then execute and log the resolution citing the new `pd-<hex>`. When it only settles this artifact, the Resolution Log entry is the whole record.
- **When the bucket is ambiguous, treat it as a design decision.** A false stop costs one confirmation; a wrongly-autonomous "fix" silently forks the truth.

## Reconciliation — after fixes land

A resolution that edits a numbered element (`FR-NN`, `AC-NN`, a task, a governed section) is a **reconciliation event** per `shared/frontmatter-schema.md` § Stable Identifiers: grep the other artifacts for the changed id and update or flag every citing site. Spec, design, plan, and phase docs must agree before the review is marked `resolved` — a fix applied to one artifact while its citations elsewhere still describe the old behavior is drift, not resolution.

## Follow-ups never float

`deferred` is only a valid disposition when the work is **tracked**: add it as a plan task (new task id, normal `verification` field) in the relevant phase — or, when no plan exists yet, record it in the review frontmatter as a follow-up entry:

```yaml
followups:
  - id: FU-01
    finding: F-05
    summary: "Add backpressure test for the retry path"
    tracked_in: ""        # filled with the task id (e.g. "3.4") once planned; empty = not yet landed
```

A review with any `followups[]` entry whose `tracked_in` is empty is not fully resolved — it may be `resolved` only if the user explicitly accepts the floating follow-up; later reviews of the same target keep flagging it until it lands in a plan. This is the net that keeps implementation follow-ups from getting lost.
