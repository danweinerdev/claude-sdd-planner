---
title: "Authority core review"
type: review
status: open
created: 2026-09-10
updated: 2026-09-11
tags: [review]
related: ["Plans/ForkAwareDecisionLedgers/03-consumers.md"]
review_of: "Plans/ForkAwareDecisionLedgers/03-consumers.md"
rev: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..84bed37805399dac1162c6c1f1e4047b62ce469e"
review_scope: phase
frozen: false
verdict: Needs changes
reviewed_planning_revision: "84bed37805399dac1162c6c1f1e4047b62ce469e"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..84bed37805399dac1162c6c1f1e4047b62ce469e"
    evidence: "Reviewed core ancestor contracts and recorded observations, accepted the config-only risk exception, and found no core plan drift. Hook/workflow and final-release tasks were excluded from the core verdict."
  - lane: review_quality
    result: Findings
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..84bed37805399dac1162c6c1f1e4047b62ce469e"
    evidence: "Found the core sound with concrete gaps: stale restore blocked by CLI, committed barrier cleanup over-checking sources, unbounded journal writes versus bounded reads, decision-ID width mismatch, and outcome/error taxonomy. Digest/semantic binding and local-only publication checked."
  - lane: review_spec_compliance
    result: FAIL/Needs changes
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..84bed37805399dac1162c6c1f1e4047b62ce469e"
    evidence: "Reported noncompliance in missing-selector fallback, repository-anchored source adoption, owner-aware scope invocation, legacy citation root qualification and capability JSON naming/coverage."
  - lane: review_blind_spots
    result: FAIL/Needs changes
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..84bed37805399dac1162c6c1f1e4047b62ce469e"
    evidence: "Challenged missing-config/manual repair, symlinked root support, Windows publication containment, POSIX no-overwrite behavior, adoption source barriers, scoped Root capture loss, approval-vs-digest distinction, reserved path aliases and journal directory modes."
findings:
  - id: F-01
    severity: major
    title: "Authority context and scope/citation checks are not preserved consistently"
    status: fixed
  - id: F-02
    severity: major
    title: "Publication and recovery need root/barrier/bounds hardening"
    status: fixed
  - id: F-03
    severity: major
    title: "Public CLI omits supported source/restore/capability cases"
    status: fixed
  - id: F-04
    severity: minor
    title: "Rare missing-config window requires manual staging restoration"
    status: rejected
followups: []
---

# Authority core review

## Findings
### F-01: Authority context and scope/citation checks are not preserved consistently

Preserve `DecisionView` and `DecisionDiagnostics` in `ScopeToPlan`/`ScopeToDoc`; invoke owner-aware `CheckScopes` using consuming-repository context; require explicit root-qualified legacy inventory for bare citations. Detect a missing selector when operation staging or available history proves prior fork authority, without selecting staged files as authority. Locations: `internal/rules/scope.go`, `internal/rules/decisions.go`, `internal/rules/citations.go`, `internal/decisionview/consumer.go` and selection. Spec and blind-spots lanes corroborate context/authority discrepancies. Impugns FR-05, FR-12, FR-15 and the shared validation/context consumers.

### F-02: Publication and recovery need root/barrier/bounds hardening

Blind-spot-only findings include rejecting normal symlinked filesystem ancestors, source barriers omitted during adoption, Windows path-based publication and POSIX no-overwrite differences. Quality identified oversized writable-but-unreadable journals and a committed operation whose remaining barrier cleanup can be blocked by later source changes. Fix concrete supported-path and barrier/bounds defects with regressions; retain containment and truthful outcomes. Do not silently broaden the threat model or weaken ledger protections. Locations: `transaction.go`, `journal.go`, `recovery.go`, platform publishers and their tests. Impugns FR-16, NFR-03 and AC-13.

### F-03: Public CLI omits supported source/restore/capability cases

Permit valid repository-anchored adoption sources and stale-override restoration through the same validated backends; align `decision_forks` capability output with the contract while keeping partial release status honest. Use consistent accepted decision-ID grammar and retain operation/outcome information on errors. The digest proves content binding, not independent human consent: workflow instructions must obtain explicit approval before calling apply. No new authentication system is requested. Locations: `cmd/sdd/decide_fork_write.go`, `decide_fork_read.go`, and `internal/decisionview/lifecycle.go`. Quality and spec lanes raised these gaps; bare historical CLI lookup without an artifact context must not guess a namespace.

### F-04: Rare missing-config window requires manual staging restoration

Quality and blind spots requested automatic recovery for the remove-old/rename interval. The user explicitly accepted this rarely changed config's missing-path/crash risk and the design documents manual repair from retained staging bytes. This request is rejected as outside that accepted exception; no atomic-replacement investigation or extra automatic recovery machinery will be added for it. Silent selection of a different authority when prior fork state is known remains covered by F-01, not excused.

## Resolution Log
- 2026-09-10: Four independent lanes completed. Plan drift was Aligned; the other lanes reported concrete gaps. The aggregate remains Needs changes. Core fixes require tracked implementation slices and a fresh four-lane review; no frozen Aligned observation was recorded.
- 2026-09-11: F-01 fixed by `76b0832` (`fix(rules): retain scoped authority and explicit citation ownership`).
- 2026-09-11: F-02 fixed by `77c584d` (`fix(decisionview): harden publication roots barriers and journal bounds`).
- 2026-09-11: F-03 fixed by `f7ff600` (`fix(decide): complete source restore capability and ID contracts`). F-04 remains rejected.
