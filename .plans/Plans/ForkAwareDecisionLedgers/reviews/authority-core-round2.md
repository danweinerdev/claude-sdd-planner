---
title: "Authority core review, round 2"
type: review
status: open
created: 2026-09-11
updated: 2026-09-11
tags: [review]
related: ["Plans/ForkAwareDecisionLedgers/03-consumers.md"]
review_of: "Plans/ForkAwareDecisionLedgers/03-consumers.md"
rev: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..24c7c6253f16466ec633325d831f938b4f1d38ea"
review_scope: phase
frozen: false
verdict: Needs changes
reviewed_planning_revision: "8dd1b797d09ad34768960a784fb0e0add9f9b131"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..24c7c6253f16466ec633325d831f938b4f1d38ea"
    evidence: "Completed independently against the frozen identity; the plan lane was Aligned and reported no plan drift."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..24c7c6253f16466ec633325d831f938b4f1d38ea"
    evidence: "Approved the frozen aggregate with minor comments; no quality finding blocked the review at this identity."
  - lane: review_spec_compliance
    result: FAIL/Needs changes
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..24c7c6253f16466ec633325d831f938b4f1d38ea"
    evidence: "Failed the frozen aggregate for legacy owner recovery: initial unowned adoption recovery was not yet compliant at this identity."
  - lane: review_blind_spots
    result: FAIL/Needs changes
    reviewed_identity: "0549e6ce48e4dd032d57cf915f0f1cfafd79e354..24c7c6253f16466ec633325d831f938b4f1d38ea"
    evidence: "Needs changes at the frozen identity for repository/planning locator aliases and legacy root compatibility, with additional hardening concerns recorded below."
findings:
  - id: F-01
    severity: major
    title: "Initial unowned adoption recovery is not compliant"
    status: fixed
  - id: F-02
    severity: major
    title: "Repository/planning locator aliases are not resolved consistently"
    status: fixed
  - id: F-03
    severity: major
    title: "Legacy root compatibility is not preserved"
    status: fixed
  - id: F-04
    severity: minor
    title: "Automatic missing-config repair"
    status: rejected
followups: []
---

# Authority core review, round 2

This is an open, historical four-lane review of `Plans/ForkAwareDecisionLedgers/03-consumers.md` at frozen identity `0549e6ce48e4dd032d57cf915f0f1cfafd79e354..24c7c6253f16466ec633325d831f938b4f1d38ea`.

## Findings
### F-01: Initial unowned adoption recovery is not compliant

The spec-compliance lane found that initial recovery for an unowned adoption selector did not preserve the required legacy owner behavior at the reviewed identity. Status: fixed by `77fbfd5` (`fix(recovery): admit unowned legacy adoption selectors safely`).

### F-02: Repository/planning locator aliases are not resolved consistently

The blind-spots lane found that repository and planning-root locator aliases could lose the source-root association needed by fork diagnostics at the reviewed identity. Status: fixed by `44c0be4` (`fix(rules): retain source roots in fork diagnostics`).

### F-03: Legacy root compatibility is not preserved

The blind-spots lane found that legacy root compatibility could be lost while resolving fork authority at the reviewed identity. Status: fixed by `2c014f3` (`fix(validate): preserve legacy roots without borrowing fork authority`).

### F-04: Automatic missing-config repair

The request for automatic repair of the missing-config interval is rejected. The already accepted config-only manual-staging risk retains the staging bytes for manual restoration; this review does not authorize automatic repair machinery.

## Reviewer Concerns and Disposition
- **WriteExpected/SaveForkJournal coverage concern — answered.** Tests limited to these helpers do not by themselves prove absent live coverage. Real `Apply`, recovery, and CLI regressions exist, and no demonstrated bug requires routing them through a double-lock API.
- **Hardening questions — unresolved, not fixed.** Source-recovery lock breadth, local git-history checks for fork stores, the left citation boundary, and orphan Windows backup files remain questions for a fresh review. They are not demonstrated authority corruption.

## Resolution Log
- 2026-09-11: Four independent lanes completed at `0549e6ce48e4dd032d57cf915f0f1cfafd79e354..24c7c6253f16466ec633325d831f938b4f1d38ea`. Plan drift was Aligned and quality approved with minor comments; spec compliance and blind spots required changes. Verdict: Needs changes.
- 2026-09-11: F-01 fixed by `77fbfd5`; F-02 fixed by `44c0be4`; F-03 fixed by `2c014f3`. The latest aggregate `8dd1b797d09ad34768960a784fb0e0add9f9b131` passed `go test -json -count=1 -timeout=300s ./...` (`core-final-suite.json`).
- 2026-09-11: A fresh independent four-lane review is required. This historical round is not Aligned and records no frozen resolution or completion assertion.
