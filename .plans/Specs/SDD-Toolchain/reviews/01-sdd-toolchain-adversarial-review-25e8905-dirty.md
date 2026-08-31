---
title: "Adversarial Review: SDD Toolchain — Single Go Binary, Artifact Compiler, and Validator"
type: review
status: resolved
created: 2026-08-03
updated: 2026-08-03
waivers:
  - code: SDD121
    reason: "Frozen review records the decisions as they stood at review time; later supersessions (D-0021, D-0022) do not rewrite frozen review artifacts."
    accepted: "2026-08-31"
tags: [review, adversarial, golang, tooling]
related: [Specs/SDD-Toolchain]
review_of: "Specs/SDD-Toolchain"
rev: "25e8905-dirty"
findings:
  - id: F-01
    severity: critical
    title: "No specified way to revise an existing artifact — apply and section set both reject the ids the artifact already contains"
    status: fixed
  - id: F-02
    severity: critical
    title: "go install provisioning makes /setup skip placing the plugin-root copy, silently disabling both hooks forever"
    status: fixed
  - id: F-03
    severity: critical
    title: "First normalization rewrites completed artifacts' bytes, with no exemption or migration plan for frozen review identity"
    status: fixed
  - id: F-04
    severity: major
    title: "Building at /setup requires network access for module fetch — an undeclared dependency prebuilt binaries did not have"
    status: fixed
  - id: F-05
    severity: major
    title: "Free-prose sections are still subject to FR-23 id resolution, so rationale cannot mention retired or prospective ids"
    status: fixed
  - id: F-06
    severity: major
    title: "Atomic writes give no isolation; parallel /implement waves can silently lose evidence and status updates"
    status: fixed
  - id: F-07
    severity: major
    title: "The only external-contract pin points at a document this spec marked superseded and do-not-implement-from"
    status: fixed
  - id: F-08
    severity: major
    title: "Schema-generated templates will invalidate the frozen parity corpus that was built from hand-maintained templates"
    status: fixed
  - id: F-09
    severity: major
    title: "Binary resolution is uncached and executes an arbitrary PATH binary named sdd to interrogate it"
    status: fixed
  - id: F-10
    severity: minor
    title: "Version lockstep depends on a Python script, while the spec's headline claim is deleting Python"
    status: fixed
  - id: F-11
    severity: minor
    title: "AC-28 asserts every artifact passes dry-run, which is unsatisfiable during FR-36's per-type rollout"
    status: fixed
  - id: F-12
    severity: minor
    title: "The size estimate that justifies rejecting prebuilt distribution has no source, as do the 300 ms and 2 minute bounds"
    status: fixed
  - id: F-13
    severity: question
    title: "Can a project schema override shrink the set of paths the write guard recognizes as artifacts?"
    status: answered
  - id: F-14
    severity: question
    title: "Does FR-42's ban on model-judgment fallback disable /validate's intended semantic layer?"
    status: answered
followups: []
---

# Adversarial Review: SDD Toolchain — Single Go Binary, Artifact Compiler, and Validator

**Reviewed state:** 25e8905-dirty (spec uncommitted at review time)
**Review mode:** single-agent, primary context; related-context sweep done directly against `Specs/Go-Validator`, `Designs/Go-Validator`, `commands/implement/SKILL.md`, `hooks/reviewer-bash-guard.py`, `shared/review-artifacts.md`, and `Decisions/decisions.md`

## Findings

### F-01 — [Critical] No specified way to revise an existing artifact
**Impugns:** FR-17, FR-20, FR-22, AC-15

**Scenario:** `Specs/SDD-Toolchain/README.md` exists and contains `**FR-01**:` through `**FR-44**:`. A reviewer files a finding and the author needs to reword FR-19. Per FR-17, `apply` takes the whole document as its payload. That payload necessarily contains `FR-NN` declarations — and AC-15 states that "a payload containing `status:`, `updated:`, or an `FR-NN` declaration is refused." So whole-document `apply` of any artifact that already has identifiers is refused. The narrower path is no better: FR-22 requires `section set` payloads to reject "identifier declarations in a namespace the tool owns," so the Requirements section cannot be set either.

The result is that `apply` can only ever create a *net-new* artifact with no identifiers, and no specified operation can revise a section that carries them.

**Why it matters:** Revision is the dominant operation, not creation. `/specify` step 3 addresses reviewer findings, `/plan` re-run deepens existing plans, and this review's own step 8 updates the reviewed artifact in place. The spec defines a compiler that cannot perform its most common job. It also leaves two things undefined that FR-20 depends on: the payload syntax an author uses to mean "allocate an identifier here," and where retired identifiers are recorded so that "never reuse a retired value" is enforceable across edits.

Note FR-22 and AC-15 additionally disagree with each other about whether the id-declaration lint applies to `apply` or only to `section set` — so the contradiction is internal as well as behavioral.

**Recommendation:** Decide and specify the round-trip contract. The workable shape is that existing identifiers in an `apply` payload are *required* and treated as assertions the tool verifies against the artifact's current identifier set (unknown id → refuse; missing id that exists on disk → refuse as an attempted retirement unless an explicit retire flag is passed), while a new item is written with no id and the tool allocates one. Then AC-15 must be narrowed to *tool-owned frontmatter fields* only, and FR-22's lint reworded to reject only identifier declarations that do not already exist. Add an explicit retired-identifier register and say where it lives.

### F-02 — [Critical] `go install` provisioning silently disables both hooks, permanently
**Impugns:** FR-05, FR-27, FR-37, FR-40, FR-41, FR-42, AC-33, D-0013, D-0014

**Scenario:** A user follows the FR-41 path: `go install github.com/danweinerdev/claude-sdd-planner/cmd/sdd@v1.16.0`, then runs `/setup`. FR-40's first ordered step is "resolve an existing usable binary per FR-05 and skip building if one is found." FR-05 candidate 3 finds the `PATH` binary at a matching version, so setup skips the build. But FR-40 places both copies only in the *otherwise* branch — so `${CLAUDE_PLUGIN_ROOT}/bin/sdd` is never created. `hooks/hooks.json` names only that static path (FR-27), the file is absent, and per FR-27 and FR-42 the hook fails open **silently**.

Steady state: every lifecycle skill works perfectly, while the reviewer Bash guard and the SessionStart ledger injection never run, in any session, indefinitely. `sdd doctor` reports the binary as present and admitted, because FR-42 has it evaluate the FR-05 candidate list — which does not include the hook's static path.

**Why it matters:** This silently voids both guarantees the ledger just recorded. D-0014's mechanical read-only enforcement for the seven reviewer agents is gone, so `drift-detector` and friends regain unguarded `Bash`. D-0013's accepted-decision injection is gone, so sessions lose the standing constraints. Nothing anywhere reports a problem, and AC-33 actively bakes the defect in: it asserts that setup "skips the build and reports the resolved path" without asserting that the hook path exists afterward.

**Why it survives a pushback:** it is not enough to say "setup should just always place the copy" — that is exactly the fix, but as written FR-40's ordering makes the skip and the copy mutually exclusive, and no acceptance criterion covers the gap.

**Recommendation:** Make the plugin-root copy unconditional: `/setup` places or refreshes `${CLAUDE_PLUGIN_ROOT}/bin/sdd[.exe]` on every run, including the skip-the-build path, copying from whichever candidate resolved. Add the hook's static path as an explicitly checked item in `sdd doctor` output, distinct from the FR-05 candidates. Extend AC-33 to assert the hook path exists and executes after each `/setup` outcome, and add a case for the `go install`-then-`/setup` ordering.

### F-03 — [Critical] First normalization rewrites completed artifacts, with no exemption or migration plan
**Impugns:** FR-10, FR-24, FR-34, FR-36, AC-28, D-0008

**Scenario:** A plan has phases 1 and 2 at `status: complete`, each carrying completion evidence with native-SCM revision identity and a frozen four-lane review recording `reviewed_planning_revision: <full40>`. FR-36 rolls the compiler out for `plan` and `phase`. FR-34 and AC-28 require every existing artifact to pass `apply --dry-run` "with normalization deltas only" — which concedes that existing artifacts are *not* normalized. FR-24 then requires that normalized output be byte-idempotent, so the first real `apply` or `section set` touching those phase docs rewrites them: LF endings, canonical heading order, canonical identifier formatting, forward-slash frontmatter paths (NFR-05).

Meanwhile FR-10 requires preserving "lifecycle-durability checks... byte comparisons" and the current-versus-historical identity distinction, and D-0008 makes each completed task a bisectable revision whose recorded identity is load-bearing.

**Why it matters:** Completed, evidence-gated work is the plugin's strongest integrity claim, and normalization mutates the bytes that claim is anchored to at a revision later than the frozen one. Either lifecycle-durability validation starts failing on already-complete phases, or it keeps passing because the anchor was never really byte-sensitive — and the spec does not say which, so nobody has checked. Separately, the migration itself is unscheduled: the day the compiler lands for a type, every artifact of that type wants a whole-file rewrite, which is one enormous commit touching completed work — precisely the shape D-0008 forbids for lifecycle bookkeeping.

**Recommendation:** Add a requirement that resolves both halves. First, analyze and state which lifecycle-durability and review-gate checks are byte-sensitive, and either exempt `complete`/`frozen` artifacts from normalization or define how their recorded identity is re-anchored. Second, specify the normalization migration explicitly: a one-time, separately-committed, per-type normalization pass with its own scoped commit and a before/after validation run, rather than letting the first incidental `apply` rewrite history-bearing files.

### F-04 — [Major] Building at `/setup` requires network access, which is undeclared
**Impugns:** FR-40, FR-43, NFR-08, Open Questions, and the since-retired cold/warm-cache build criterion

**Scenario:** A new user on a locked-down corporate laptop has Go installed and a cold module cache. `/setup` passes the toolchain check and runs the build, which tries to fetch goldmark and the YAML library from the module proxy. The fetch is blocked. FR-43 enumerates exactly two stop conditions — absent toolchain and too-old toolchain — so a module-fetch failure has no defined message, no defined exit behavior, and falls outside FR-43's promise that setup leaves nothing partial. The cold/warm-cache acceptance criterion (since retired) asserted that "a cold-cache and a warm-cache `/setup` build both complete," which quietly presumes network access.

**Why it matters:** Prebuilt binaries were rejected to avoid repository payload growth, but the replacement introduces a dependency the rejected option did not have. The Open Questions entry presents the trade as payload-versus-toolchain when it is actually payload-versus-toolchain-plus-network. Air-gapped and proxy-restricted environments are common in exactly the enterprise settings where a planning plugin gets used.

**Recommendation:** Commit vendored dependencies (`go mod vendor`) and require `/setup` to build with `-mod=vendor`, making the build fully offline for a few MB of text rather than ~50 MB of binaries. If vendoring is rejected, add network as an explicit dependency, add module-fetch failure as a third named stop condition in FR-43 with its own message, and correct the cold/warm-cache criterion to state its network precondition.

### F-05 — [Major] Free-prose sections still can't mention retired or prospective identifiers
**Impugns:** FR-19, FR-20, FR-23

**Scenario:** A design's free-prose "Alternatives Rejected" section contains "we considered a streaming variant, which would have needed a functional requirement we never allocated, but rejected it" — where the author naturally wants to write the unallocated number literally — or "this supersedes the approach the now-retired requirement originally described." FR-23 resolves identifier-shaped tokens against the artifact and its `related` artifacts and refuses the apply on any unresolvable citation. FR-19 exempts free-prose sections from structural enforcement "beyond FR-22" — and FR-22 is the payload lint, not FR-23 — so identifier resolution still applies to exactly the section designed for unconstrained reasoning.

Worse, FR-20 establishes that identifiers get *retired* and never reused, so discussing a retired identifier is a normal, expected thing for rationale prose to do. FR-23 never says whether a retired identifier resolves.

**Demonstrated during this review.** The first draft of this finding named a specific unallocated requirement number as its example. `sdd_validate.py` rejected this review artifact with `SDD122` — "citation does not resolve in a related spec" — three times over, and the sentences above had to be rewritten to describe the identifier instead of naming it. That is the exact failure mode this finding predicts, occurring under today's validator, before the compiler exists to make it a hard refusal at write time.

**Why it matters:** The free-prose section exists because structural uniformity is corrosive to thinking (FR-19's own rationale). Refusing an apply because the rationale names a hypothetical or retired identifier defeats that purpose, and the author's only workaround is to stop writing identifiers in prose — which degrades exactly the traceability the rest of the spec is built on.

**Recommendation:** Exempt inline code spans from FR-23 resolution, so that wrapping an identifier in backticks marks it as a literal rather than a citation and gives authors a deliberate escape. Note that today's validator does *not* grant that exemption — the second rewrite of this finding was rejected for naming an unallocated identifier inside a code span — so the escape has to be specified, not assumed. Also state explicitly that retired identifiers resolve. Consider exempting designated free-prose sections from FR-23 entirely, since FR-23's value lies in the sections that carry live traceability.

### F-06 — [Major] Atomic writes provide no isolation, and `/implement` is parallel
**Impugns:** FR-24, FR-21, FR-44, NFR-02

**Scenario:** `commands/implement/SKILL.md:118-122` launches all tasks in a wave as concurrent `code-implementer` agents. `code-implementer` holds all tools and is not among the seven read-only agents, so FR-44 does not restrict it from mutating subcommands. Two agents in the same wave each finish and record evidence against the same phase document: agent A reads the phase doc, agent B reads it, A writes, B writes. FR-24 guarantees the *write* is atomic via temp-file-plus-rename — it guarantees nothing about the read-modify-write cycle around it. B's rename lands last and A's evidence is silently gone. A subsequent `sdd task complete` for A's task then refuses for a reason that looks like a missing-evidence bug rather than a lost update.

**Why it matters:** Atomicity is not isolation, and the spec conflates them. The plugin's own primary execution path is concurrent by design, so this is the expected case, not an exotic one. Silent loss of completion evidence attacks the same integrity claim as F-03.

**Recommendation:** Require optimistic concurrency on every mutating subcommand: the caller supplies (or the tool captures and re-verifies) a hash of the artifact's pre-edit bytes, and the write refuses with a distinct "artifact changed under you, re-read and retry" diagnostic on mismatch. Add an acceptance criterion that interleaves two concurrent mutations of one artifact and proves neither is silently lost. Advisory file locking is an acceptable alternative but composes worse with agents that may die mid-operation.

### F-07 — [Major] The external-contract pin cites a superseded, do-not-implement-from document
**Impugns:** Requirements preamble comment, FR-09, FR-32, Dependencies

**Scenario:** An implementer starts layer 1 and needs the exact YAML library and version, because FR-09's PyYAML-compatibility contract and FR-32's differential comparison both depend on precise scalar and line-mark behavior. The spec's only pin (line 70) says the versions "are pinned in `Designs/Go-Validator`" — a document this spec marked `status: superseded` with an explicit header instruction not to implement from it. The implementer either follows the pin into a superseded artifact or obeys the header and proceeds with no pinned version at all.

**Why it matters:** The spec's own Dependencies section states that YAML library selection "is a design decision, not an assumed compatibility claim." Sourcing that decision from a retired document is the exact traceability failure the plugin's citation discipline exists to prevent, and an unpinned YAML version is a direct parity risk under a closed delta list (FR-07).

**Recommendation:** Restate the pins — library, version, and as-of review date — in this spec's preamble, or explicitly defer them to the owed new design and remove the reference to the superseded one. The same applies to the Claude Code hook JSON protocol, which the preamble also defers to "the design" without saying which.

### F-08 — [Major] Generated templates will invalidate the frozen parity corpus
**Impugns:** FR-15, FR-31, FR-32, AC-13, AC-06

**Scenario:** `tests/test_plan_scope_validation.py` builds a planning root from the real templates (per the repository's own maintenance rules), and FR-31 freezes the six Python test files as non-executable oracle sources with SHA-256 checksums, with FR-32 keeping the frozen fixtures as the authoritative regression suite after Python is gone. Layer 2 then lands FR-15, making `shared/templates/` schema-generated. If any generated template differs from its committed counterpart by a single byte — heading order, list marker, trailing newline — the frozen fixtures now encode template text the plugin no longer produces. They either fail for a reason unrelated to any real regression, or they keep passing while asserting behavior against content nobody authors anymore.

**Why it matters:** The frozen corpus is the entire safety argument for deleting the Python oracle. A silent divergence between "frozen forever" and "regenerated from schema" undermines the one artifact that makes FR-33's deletion safe, and it surfaces long after the deletion is irreversible.

**Recommendation:** Require the first generation pass to be byte-identical to the currently committed templates, making FR-15 initially a proof that the generator reproduces reality rather than a change to it. Then require any intended template change to be an explicit, separately-frozen delta with both old and new expected outputs recorded, exactly as FR-07 handles parity deltas.

### F-09 — [Major] Resolution is uncached and executes an arbitrary `PATH` binary
**Impugns:** FR-05, FR-38, NFR-07

**Scenario:** FR-05 candidate 3 admits "an `sdd` on `PATH` whose `sdd version` equals the invoking plugin's version." Determining that requires *executing* the binary. `sdd` is a short, generic name, so an unrelated tool — or a hostile one placed earlier in `PATH` — gets executed automatically by the plugin before any trust is established. Separately, nothing says resolution is cached: if every skill invocation walks the candidate list and shells out to `sdd version`, each logical operation pays two process spawns, against an NFR-07 budget of 300 ms whose stated purpose is that "routing document mutations through the tool does not impose a per-call latency cost."

**Why it matters:** NFR-07 measures the subcommand, not the resolution wrapper, so the spec can be satisfied while the user-visible cost is double the budget. The trust issue is modest — it is the user's own `PATH` — but it is a real name-collision hazard for a two-letter-prefix binary name, and the spec is silent on it.

**Recommendation:** Require resolution to be performed once per session and cached, with the resolved path recorded where `sdd doctor` can report it. Prefer the two plugin-owned locations before ever executing a `PATH` candidate (already the ordering, but state that a `PATH` candidate is executed only when both owned locations miss), and require the version probe to be a fixed, argument-free invocation with a bounded timeout.

### F-10 — [Minor] Version lockstep keeps a Python dependency
**Impugns:** FR-39, FR-35, Goals, Dependencies, AC-27

**Scenario:** FR-39 has the version reach the binary "through a generated, committed Go source file written by the existing `bump-version.py`," and `make bump-*` invokes `python3 bump-version.py`. A maintainer on a Python-less machine cannot cut a release.

**Why it matters:** No hard contradiction — NFR-01 scopes its claim to runtime and AC-27 scopes its repository search to *validator* invocations and PyYAML — but the Goals say the toolchain requires "no Python... thereafter," and the spec's headline is deleting Python. A reader will reasonably conclude Python is gone when it is not.

**Recommendation:** Either port version bumping into the binary (`sdd version bump patch|minor|major`, which also lets the tool own the generated file it consumes) or add maintainer-side Python to Dependencies and narrow the Goals wording to runtime.

### F-11 — [Minor] AC-28 is unsatisfiable during the FR-36 rollout
**Impugns:** AC-28, FR-34, FR-36

**Scenario:** FR-36 rolls schemas out one artifact type at a time, `spec` first. AC-28 requires that "every artifact under this repository's planning root passes `sdd apply --dry-run`." While only the `spec` schema exists, research, brainstorm, plan, phase, debrief, and review artifacts have no schema to be checked against, so the criterion cannot pass.

**Why it matters:** An acceptance criterion that is false for most of the project's life gets marked "not yet" indefinitely and stops being a signal.

**Recommendation:** Scope AC-28 to the artifact types whose schemas have landed, and add a separate final-state criterion for full coverage.

### F-12 — [Minor] The decision-bearing size estimate has no source
**Impugns:** Open Questions, NFR-07, NFN-08

**Scenario:** The rejection of prebuilt distribution rests on "five targets at roughly 10 MB each" and "about 50 MB... per release." No measurement, source, or as-of date is given, and no binary exists yet to measure. NFR-07's 300 ms and NFR-08's 2 minute bound are likewise unsourced, as is the claim of "parity with other AI/LLM tooling that requires `go install`," which names no such tool.

**Why it matters:** A decision rests on the size figure — it is the stated justification for a constraint the spec calls an "accepted regression" in user experience. If the real stripped binary is 4 MB, the payload argument weakens considerably and prebuilt distribution may be the better answer after all.

**Recommendation:** Build a throwaway binary with the intended dependencies and the FR-42 build flags, record the measured size with a date, and restate the trade against the real number. Derive the NFR-07 and NFR-08 bounds from a measurement on named hardware, or mark them provisional pending one.

### F-13 — [Question] Can a project override shrink the guard's artifact set?
**Impugns:** FR-16, FR-28

FR-28 denies `Write`/`Edit` on "schema-recognized artifact paths," and FR-16 lets a project layer schema overrides from disk. FR-16's prohibition list covers required headings, identifier grammars, status gates, and field ownership — it says nothing about which paths count as artifacts. If path recognition is schema-derived, a project override could narrow the guarded set and quietly re-open direct writes. Is path recognition override-able, and if not, where is that stated?

### F-14 — [Question] Does FR-42 disable `/validate`'s semantic layer?
**Impugns:** FR-42, Non-Goals

FR-42 requires that no skill "fall back to... model-judgment validation" when unprovisioned. But `/validate` is specified as deterministic *plus* semantic, and the semantic half is model judgment by design. Read literally, FR-42 forbids the semantic layer from running when the binary is missing, even though it is independently useful and does not pretend to be the deterministic layer. Is the intent narrower — that model judgment must not be *presented as* deterministic validation — and should the wording say so?

## Resolution Log

<!-- Append-only; one entry per disposition. Update the finding's status in
     findings[] to match. See shared/review-artifacts.md. -->

### F-01 — fixed (2026-08-03)
Round-trip contract specified as new **FR-45**: identifiers in an `apply` payload
are assertions the tool verifies, not declarations the author invents. Existing
identifier → preserved; unknown identifier → refused with the namespace's live
values listed; no identifier → allocated; existing identifier omitted from the
payload → refused as an unintended retirement unless `--retire <id>` names it,
in which case the tool writes the retirement note in the form
`shared/frontmatter-schema.md` § Stable Identifiers prescribes.

**FR-20** amended: allocation applies only to payload items carrying no
identifier, takes the next value above the namespace high-water mark, and never
fills gaps — so a retired value is never reissued. The high-water mark is the
maximum over live identifiers, retirement notes, and identifiers of that
namespace cited in `related` artifacts. This keeps the retirement register in the
artifact body, written by the tool, rather than adding a frontmatter field, which
this spec's Non-Goals forbid.

**FR-22** reworded: the blanket rejection of identifier declarations is removed
and replaced by a reference to FR-45, resolving the FR-22/AC-15 contradiction the
finding identified. **AC-15** narrowed to tool-owned *frontmatter* fields only,
with identifier handling moved to new **AC-40**, which proves the full round trip
including retirement and non-reissue.

### F-02 — fixed (2026-08-03)
**FR-40** restructured so placement is unconditional: both FR-37 locations are
populated or refreshed on every `/setup` run along *both* branches, from whichever
binary resolved or was built, and verification runs through the plugin-root path
specifically. The rationale is recorded inline — the failure has no symptom, so
the ordering has to make it unreachable rather than rely on someone noticing.

**FR-37** amended to state that a `go install`-provisioned binary discovered on
`PATH` still results in both copies existing. **FR-42** amended so `sdd doctor`
reports the hook static path as its own checked item, distinct from the FR-05
candidates, and names it as a defect with `/sdd-planner:setup` as the remedy when
absent — because a resolvable binary and a working hook are independent
conditions.

**AC-33** rewritten to assert that both FR-37 locations exist and the plugin-root
path executes after every non-stopping outcome; new **AC-41** tests the
`go install`-then-`/setup` ordering end to end and proves `sdd doctor` reports a
deleted plugin-root copy as a defect while FR-05 candidates still resolve.

### F-03 — fixed (2026-08-03)
Split into protection and migration. New **FR-46**: normalization may not rewrite
a history-bearing artifact as a side effect; the byte-sensitivity of every
lifecycle-durability, completion-evidence, and review-gate check must be
determined and recorded before the compiler is enabled for a type; and an
artifact at `status: complete` or carrying `frozen: true` is refused by `apply`
and `section set` with a diagnostic naming the freezing record.

New **FR-47**: conversion to canonical form is an explicit one-time migration per
artifact type — never an incidental first write — that records a `sdd validate`
result before starting, lands as its own scoped lifecycle revision with no
implementation or content change per D-0008, re-runs validation and requires an
identical diagnostic set (any difference stops for the user), and re-anchors any
identity the FR-46 determination found byte-sensitive.

**FR-36** amended so the FR-28 guard enables for a type only after that type's
migration completes, so no artifact is guarded while still non-canonical. New
**AC-42** proves the completed/frozen refusal and the existence of the
byte-sensitivity determination; new **AC-43** proves the migration's
identical-diagnostics property, its single-revision shape, and that a deliberately
introduced difference stops it.

No decision-ledger entries were created for these three. Each fix's whole effect
is what this spec now says, so all three fail the admission test in
`shared/decision-log.md` § Capture.

### F-04 — fixed (2026-08-03)
Dissolved by a scope change rather than patched. Per **D-0015**, the plugin no
longer builds: `go install`, performed by the user before the plugin is invoked,
is the only provisioning path (**FR-41**). Module fetching, network availability,
and Go-version enforcement all move to a step the user runs explicitly, with Go's
own diagnostics — so the plugin has no undeclared network dependency and no
undefined third stop condition.

**FR-40** now only resolves, verifies the floor, copies to the plugin root, and
verifies; **FR-43**'s stop conditions became *not found* and *below floor*;
**NFR-08** reframes toolchain and network as user-side provisioning prerequisites
the plugin never invokes. The cold/warm-module-cache criterion for a `/setup` build is
retired in the spec rather than reissued, since the behavior it described no
longer exists in the plugin's contract. Its identifier is struck in place and
this finding's Impugns line was updated to stop citing it, per the reconciliation
rule in `shared/frontmatter-schema.md` § Stable Identifiers — a struck identifier
no longer parses as a declaration, so live citations to it must be retired too.

Note that this raises the practical weight of **F-09**, which remains open:
`PATH` is now the primary discovery route rather than a fallback, so uncached
resolution and executing an arbitrary `PATH` binary named `sdd` are hit on every
invocation rather than rarely.

### F-05 — fixed (2026-08-03)
**FR-23** gained three exemptions, each load-bearing: an identifier inside an
inline code span or fenced block is a literal and never resolved; a retired
identifier resolves, since FR-20 guarantees retirements exist; and a
schema-designated free-prose section is exempt from resolution entirely. New
**AC-47** proves all four cases. The rationale is recorded inline — refusing an
apply over rationale prose would push authors to stop writing identifiers in
prose, degrading the traceability everything else depends on.

### F-06 — fixed (2026-08-03)
New **FR-48** requires every mutating subcommand to be isolated as well as
atomic: capture a digest at read time, refuse the write when on-disk bytes no
longer match, with a distinct re-read-and-retry diagnostic. Optimistic
concurrency was chosen over advisory locking because agents can die mid-operation
and leave a lock behind. New **AC-44** interleaves two concurrent mutations and
proves neither is lost, in the parallel-wave shape `/implement` actually produces.

### F-07 — fixed (2026-08-03)
The Requirements preamble no longer pins anything through the superseded design.
It now states explicitly that `Designs/Go-Validator` is not a live source and its
library notes are historical, and it places the obligation on the owed design to
pin the YAML implementation, the CommonMark implementation, and Claude Code's
hook JSON protocol with version and as-of date — with no FR-09 or FR-32 parity
work permitted against an unpinned library.

### F-08 — fixed (2026-08-03)
**FR-15** now requires the first generation pass to be byte-identical to the
currently committed templates, making template generation initially a proof that
the generator reproduces reality rather than a silent change to it. Any later
intended template change is an explicit, separately-frozen delta recording both
old and new expected output — the same discipline FR-07 applies to parity deltas.

### F-09 — fixed (2026-08-03)
New **FR-49**: resolution happens once per session and is cached, `sdd doctor`
reports the cached result, and a `PATH` candidate is probed only after the
plugin-owned location misses, using a fixed argument-free invocation with a
bounded timeout. New **AC-45** proves single-probe behavior, timeout handling
against a hanging fake `sdd`, and that `PATH` is never probed while the
plugin-root location resolves. This finding rose in practical weight when D-0015
made `PATH` the primary discovery route rather than a fallback.

### F-10 — fixed (2026-08-03)
Resolved by scoping the claim honestly rather than porting. Python is retained for
`bump-version.py` and the FR-39 generated version file, and is now declared in
Dependencies as **maintainer-only**. Porting it into `sdd version bump` was
considered and rejected: it would require `sdd` to exist in order to cut an `sdd`
release, adding a bootstrap dependency for no user-visible benefit. The Goals
wording now says users never need Python rather than implying nobody does.

### F-11 — fixed (2026-08-03)
**AC-28** scoped to artifact types whose schema has landed, so it is satisfiable
during the FR-36 rollout instead of false for most of the project's life. New
**AC-46** carries the full-coverage assertion as an explicit final-state
criterion.

### F-12 — fixed (2026-08-03)
**NFR-07**'s 300 ms bound is now marked provisional pending measurement, with the
first working binary required to replace it with a figure measured on named
hardware — adjusted as the measurement warrants rather than retained by default.
The binary-size estimate is no longer decision-bearing: D-0015 settled the
prebuilt question on the grounds that the plugin should not be in the compiling
business, not on payload arithmetic, so the unsourced figure no longer supports a
decision. The measurement arrives free with the first build regardless.

### F-13 — answered (2026-08-03)
No. **FR-16** now states that the set of guarded paths is plugin-owned and derived
from the embedded schemas only, and that an override cannot affect path
recognition — otherwise a project could disable FR-28's write guard by
configuration.

### F-14 — answered (2026-08-03)
The intent was narrower than the wording. **FR-42** now prohibits *presenting
model judgment as deterministic validation* rather than prohibiting judgment
itself: a skill's semantic layer may still run when clearly labeled as semantic
and not claiming to substitute for the mechanical checks.
