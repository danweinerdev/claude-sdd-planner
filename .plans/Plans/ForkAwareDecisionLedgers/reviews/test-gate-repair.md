---
title: "Test-gate repair review"
type: review
status: archived
created: 2026-09-10
updated: 2026-09-11
tags: [review]
related: ["Plans/ForkAwareDecisionLedgers/04-integration.md", "Specs/ForkAwareDecisionLedgers/README.md"]
review_of: "Plans/ForkAwareDecisionLedgers/04-integration.md"
rev: "17e0f127fc60114b7719614622940bd0eab395bc..7a6c7b866b4f1b23aaea518a738b74147f5d8f50"
frozen: false
verdict: Needs changes
reviewed_planning_revision: "ad82c0c41d6f04f56783594adacff4f079552a30"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "17e0f127fc60114b7719614622940bd0eab395bc..7a6c7b866b4f1b23aaea518a738b74147f5d8f50"
    evidence: "Inspected the two-file diff against the test-gate-fresh-execution contract and recorded red/green observations; all package/template/corpus/portable checks remain enabled. Raised CRLF robustness as low severity."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "17e0f127fc60114b7719614622940bd0eab395bc..7a6c7b866b4f1b23aaea518a738b74147f5d8f50"
    evidence: "Inspected Makefile suffix expansion, matching build/invocation paths, fresh tests and environment-isolated dry runs. Raised LF-only matching and silent missing-make coverage as minor robustness defects."
  - lane: review_spec_compliance
    result: FAIL/Needs changes
    reviewed_identity: "17e0f127fc60114b7719614622940bd0eab395bc..7a6c7b866b4f1b23aaea518a738b74147f5d8f50"
    evidence: "Compared the diff with NFR-05/AC-15 and native-platform requirements. Reported LF-only matching at tools/testgate/makefile_test.go:22 as a high-severity Windows CRLF checkout failure; the current LF checkout passes but dry runs do not establish native execution on other hosts."
  - lane: review_blind_spots
    result: FAIL/Needs changes
    reviewed_identity: "17e0f127fc60114b7719614622940bd0eab395bc..7a6c7b866b4f1b23aaea518a738b74147f5d8f50"
    evidence: "Adversarially inspected suffix/shadowing, GNU make dry-run targets and environment isolation without intent inputs. Required CRLF-tolerant matching; also raised invisible missing-make skips, non-GNU tooling, rationale/comment drift and build variant caveats."
findings:
  - id: F-01
    severity: major
    title: "Gate tripwire fails on CRLF Windows checkouts"
    status: fixed
  - id: F-02
    severity: minor
    title: "Dynamic make checks silently pass when unavailable"
    status: fixed
  - id: F-03
    severity: minor
    title: "Tooling and rationale caveats need precise documentation"
    status: fixed
followups: []
---

# Test-gate repair review

Scope is the frozen test-gate repair only, not completion of the entire integration phase.

The original failed phase-review observations are retained below as ordinary historical finding records, not completion evidence. Only fresh frozen `Aligned` artifacts govern closure.

## Findings
### F-01: Gate tripwire fails on CRLF Windows checkouts

All four lanes identified the literal LF-only match in `tools/testgate/makefile_test.go:22`. Plan drift and quality considered it minor; spec compliance considered it high and blind spots required a fix. The coordinator treats it as a material supported-platform regression: normalize line endings in the test helper and add an explicit LF/CRLF regression. This is corroborated across independent lanes, not blind-spot-only. Impugns graph node `test-gate-fresh-execution`, NFR-05 and AC-15.

### F-02: Dynamic make checks silently pass when unavailable

Quality and blind spots noted that logging then returning reports PASS when the dry-run half never ran. Report an explicit skip and distinguish supported GNU make from other tools. A GNU make fallback name may be used, without modifying process-global configuration.

### F-03: Tooling and rationale caveats need precise documentation

Blind-spot-only observations include non-GNU make handling, dry-run scope safety, the accepted uncached-runtime cost, historical comment loss, and debug-build flag provenance. These are not regressions in authority semantics. Record precise scope of dry-run checks, retain full fresh execution, and do not claim the upstream cache defect is fixed. No cleanup of unrelated pre-existing binaries or redesign of CI is authorized by this review.

## Resolution Log
- 2026-09-10: Four independent lanes completed the frozen repair review. Verdict remains Needs changes; the lane severity disagreement is retained above. A dedicated portability task and a fresh four-lane review are required before this gate closes. No Aligned graph observation was recorded.
- 2026-09-11: F-01, F-02, and F-03 fixed by `ab73469` (`fix(test): make gate tripwires checkout-portable`), which includes the CRLF regression, explicit GNU-make skip behavior, and precise fresh-execution/dry-run rationale.
- 2026-09-11: This review retains its historical verdict, lanes, and open/unfrozen state. A fresh review supersedes this historical review's closure evidence; no closure assertion is recorded here.

### F-01 — fixed (2026-09-11)
The CRLF Windows checkout tripwire finding was fixed by `ab73469` (`fix(test): make gate tripwires checkout-portable`), including the recorded NFR-05 and AC-15 CRLF regression.

### F-02 — fixed (2026-09-11)
The dynamic make availability finding was fixed by `ab73469` (`fix(test): make gate tripwires checkout-portable`) with the recorded explicit GNU-make skip behavior.

### F-03 — fixed (2026-09-11)
The tooling and rationale caveats finding was fixed by `ab73469` (`fix(test): make gate tripwires checkout-portable`) with the recorded fresh-execution and dry-run rationale.
