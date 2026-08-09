---
title: "Adversarial Review: SDD Toolchain — Phase 1 spike findings"
type: review
status: resolved
created: 2026-08-04
updated: 2026-08-04
tags: [review, adversarial, spike, golang]
related: [Specs/SDD-Toolchain, Plans/SDD-Toolchain]
review_of: "Specs/SDD-Toolchain"
rev: "25e8905-spike"
findings:
  - id: F-01
    severity: critical
    title: "FR-18 as written made every revision impossible — tool-owned frontmatter is present in any round-trip payload"
    status: fixed
  - id: F-02
    severity: critical
    title: "Emitter silently dropped prose between the H1 and the first section"
    status: fixed
  - id: F-03
    severity: major
    title: "Grouping subheadings orphaned every identifier beneath them, emptying the available-identifier list"
    status: fixed
  - id: F-04
    severity: minor
    title: "apply rejected the flag-after-path argument order every comparable CLI accepts"
    status: fixed
  - id: F-05
    severity: minor
    title: "Measured binary size is roughly a quarter of the estimate that justified rejecting prebuilt distribution"
    status: deferred
followups:
  - id: FU-01
    finding: F-05
    summary: "Revisit D-0015's prebuilt rejection against the measured multi-target size once the tool is feature-complete"
    tracked_in: "5.4"
---

# Adversarial Review: SDD Toolchain — Phase 1 spike findings

**Reviewed state:** 25e8905-spike (Phase 1 spike, working tree)
**Review mode:** executable — findings produced by running the spike compiler against this repository's own spec artifacts, not by reading

This review records what the Phase 1 spike found by execution. It complements
`01-sdd-toolchain-adversarial-review-25e8905-dirty.md`, which was produced by
reading; every finding here was invisible to that pass and to the spike's own 61
unit tests, and surfaced only when the compiler was pointed at real artifacts.

## Findings

### F-01 — [Critical] FR-18 made every revision impossible
**Impugns:** FR-18, FR-24, FR-45, AC-15

**Scenario:** `sdd apply .plans/Specs/Go-Validator/README.md --dry-run` on the
unmodified artifact, feeding the artifact itself as the payload — the exact shape
of a revision. Refused: `SPK021 line 6: frontmatter key "updated" is tool-owned
and may not appear in a payload`. FR-18 as written forbade a tool-owned field
from appearing in a payload at all, and every round-trip payload necessarily
carries `updated`, `created`, `status`, and `type`.

**Why it matters:** This is finding F-01 of the reading review recurring in a
second place. That one established that identifiers could not be stripped from a
revision payload without silently re-binding citations; this is the same problem
for frontmatter, and the presence-only rule made `apply` a create-only operation
for the second time. It was invisible to the unit suite because the fixtures
built payloads by hand, without tool-owned frontmatter — exactly the case a real
caller never produces.

**Resolution:** FR-18 now treats a tool-owned field as a **verified assertion**,
checked against the artifact on disk. Equal to disk applies; differing refuses
naming the owning subcommand; absent applies; present with no artifact on disk
refuses. Both halves of the original guarantee survive: a matching value is
verified rather than honored, and a differing value refuses loudly rather than
being silently discarded.

Verification is deliberately against **disk**, not against the value the tool
would stamp. A stamp-comparison rule was implemented first and passes the unit
suite, but `updated` is restamped on every write and so can never equal a fresh
stamp — it would refuse every revision made on a later date than the original,
which is every real revision. The distinction is invisible to same-day tests.

`status` remains unchangeable through `apply`; FR-21's gated verbs are its only
writer. No `--retire`-style escape hatch is warranted, and that asymmetry with
FR-45 is principled: identifiers are author-authored content the tool merely
numbers, while `status`/`created`/`updated` are tool state with no legitimate
author-side edit.

### F-02 — [Critical] The emitter silently dropped preamble prose
**Impugns:** FR-17, FR-19, FR-24, NFR-02

**Scenario:** `Go-Validator/README.md` carries a ten-line blockquote between its
H1 and its first `##` section — the superseded notice added earlier in this
project. The emitter wrote frontmatter, then the H1, then each matched slot in
declared order. Prose belonging to no slot was never written. The dry-run diff
showed those ten lines as pure deletions.

**Why it matters:** Silent content loss is the most dangerous defect available to
a tool that owns the bytes of your files, and `--dry-run` is the only reason it
was caught rather than committed. Had the write guard (FR-28) been enabled with
this bug present, the compiler would have been the *only* permitted writer while
quietly discarding text on every revision. The unit fixtures all placed the first
heading immediately after the H1, so all 61 tests passed.

**Resolution:** The emitter now preserves preamble prose after the H1. Beyond the
fix, this is the strongest available argument for FR-34/AC-28's requirement that
every existing artifact dry-run clean before the compiler is trusted, and for
FR-47's insistence that the migration prove an identical validation diagnostic
set before and after: a normalizer's failure mode is subtraction, and subtraction
does not announce itself.

### F-03 — [Major] Grouping subheadings orphaned identifiers
**Impugns:** FR-14, FR-20, FR-23, FR-29, FR-45

**Scenario:** `Specs/SDD-Toolchain/README.md` groups its requirements under
`#### A. Executable, packaging, and command surface` and similar, nested inside
`### Functional Requirements`. The parser opened each H4 as its own section, so
every `FR-NN` declaration beneath one never reached the enclosing slot's body.
Result: 31 refusals reading `citation FR-13 does not resolve` with
`available FR identifiers:` — an empty list — and, once FR-18 was fixed, 49
refusals claiming each of `FR-01` through `FR-49` "does not exist in the
artifact."

**Why it matters:** Two distinct failures with one cause. First, an empty
available-identifier list is a refusal message that actively misleads: FR-29
requires naming the correct form, and this named nothing. Second, and worse under
FR-45, the tool concluded the artifact had no identifiers and treated every one
of them as invented — so a correct payload was refused for asserting identifiers
that demonstrably exist. A schema that models only leaf depths would have made
real specs uncompilable.

**Resolution:** A shared `FoldDeeper` helper folds headings deeper than any
declared slot back into the enclosing section's body, used by both the payload
path and the existing-artifact path — the duplication was the reason the first
partial fix left the existing-artifact side still broken. The schema implication
for Phase 6: only headings at declared depths are slots, and everything deeper is
body content. That rule needs stating in the schema format itself rather than
living in matcher code.

### F-04 — [Minor] Flag-after-path argument order was rejected
**Impugns:** FR-02, FR-29

**Scenario:** `sdd apply <path> --dry-run` failed with
`unexpected extra argument "--dry-run"`, because Go's stdlib `flag` stops parsing
at the first non-flag argument. `git`, `gh`, and every comparable CLI accept
flags after positionals.

**Why it matters:** Small, but it lands on the ergonomics FR-29 exists to
protect. An automated caller that writes the natural order gets a usage error
whose message does not explain that argument order is the problem, which is
exactly the retry-loop-versus-one-turn-correction distinction the compiler design
is betting on.

**Resolution:** Positionals are split out before flag parsing, so either order
works. Phase 2's real CLI should treat this as a requirement on the parser, not a
per-subcommand fix.

### F-05 — [Minor] The size estimate behind rejecting prebuilt distribution is 4× high
**Impugns:** Open Questions, D-0015, NFR-07

**Scenario:** `go build -ldflags="-s -w" -trimpath` produces a **2,517,138 byte**
(2.40 MB) binary. The spec's Open Questions entry estimated "roughly 10 MB each"
and "about 50 MB per release" across five targets; the measured figure is
approximately 12 MB for all five.

**Why it matters:** That estimate is the stated arithmetic behind rejecting
prebuilt bundling, and 12 MB per release is a materially different trade from
50 MB. Note this is a stdlib-only spike; adding the CommonMark and YAML libraries
Phase 4 requires will raise it, so 12 MB is a floor rather than a final figure.

**Resolution — deferred, tracked as task 5.4 (FU-01).** The user's call: the prebuilt question does not
matter until the tool is feature-complete, and D-0015 rests independently on the
judgment that the plugin should not be in the compiling business, which the
measurement does not disturb. Revisit against a measurement taken with real
dependencies linked in.

Also measured, replacing NFR-07's provisional bound with evidence: mean **5.65 ms**
per `apply` over 20 invocations against a 270-line spec on darwin/arm64. NFR-07's
300 ms target has roughly 50× headroom, though a real CommonMark parser will
consume some of it.

## Resolution Log

### F-01 — fixed (2026-08-04)
FR-18 rewritten to the verified-assertion rule, with the rationale for
disk-comparison over stamp-comparison recorded inline. New **FR-50** records that
the same check doubles as a stale-read tripwire and that it must **not** be
treated as a substitute for FR-48's digest precondition, since two body-only
edits on the same date leave `updated` identical — both checks are required and
neither may later be removed as redundant. **AC-15** replaced with the four-case
matrix, explicitly including that a canonical artifact re-applied as a revision
succeeds. New **AC-48** proves the tripwire and its insufficiency.

`TestByteIdempotence` was also corrected rather than the contract: it re-applied
without `Existing`, i.e. as a creation. Re-applying a canonical artifact is a
revision, and without `Existing` the payload's tool-owned frontmatter has nothing
to be verified against, so FR-18 correctly refuses.

### F-02 — fixed (2026-08-04)
Emitter preserves prose between the H1 and the first section. Verified against
`Go-Validator/README.md`, whose superseded blockquote now survives; the artifact's
only remaining delta is the restamped `updated` and one section reordered into
declared slot order.

### F-03 — fixed (2026-08-04)
Added `artifact.FoldDeeper`, used by both the payload matcher and
existing-artifact identifier collection. Both spec artifacts now compile with
zero refusals.

### F-04 — fixed (2026-08-04)
`apply` splits positionals from flags before parsing.

### F-05 — deferred (2026-08-04)
Tracked as FU-01 against plan task 5.4, to be revisited once the tool is feature-complete and its real
dependencies are linked. D-0015 is unaffected: it rests on the plugin not being
in the compiling business, not on payload arithmetic.

### Verification state at close
`go vet ./...` clean; `go test ./... -count=1` passing with 61 test functions and
subtests; both spec artifacts dry-run with zero refusals; SHA-256 of both
artifacts identical before and after the sweep, confirming `--dry-run` wrote
nothing.
