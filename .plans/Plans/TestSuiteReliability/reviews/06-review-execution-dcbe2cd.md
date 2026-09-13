---
title: "Phase review: 06-acceptance"
type: review
status: resolved
created: 2026-09-13
updated: 2026-09-13
tags: [review]
related: ["Plans/TestSuiteReliability/06-acceptance.md"]
review_of: "Plans/TestSuiteReliability/06-acceptance.md"
rev: "04c1e612439e3c2c036a90530e45e548e3a02c8d..dcbe2cde9a3de001f90b472ba9012695ae72993d"
review_scope: phase
frozen: true
verdict: Amend
reviewed_planning_revision: "dcbe2cde9a3de001f90b472ba9012695ae72993d"
review_mode: independent
lane_results:
  - lane: review_plan_drift
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..dcbe2cde9a3de001f90b472ba9012695ae72993d"
    evidence: "drift-detector (agent, delta ed86c14..dcbe2cd, HEAD unmoved, two single-concern commits citing F-01/F-02): propagation-validator rev 7 and propagation-graph-callers rev 5 land as contracted — RetirementProblemsChecked and the fail-closed lossy form in retirement.go, semanticFindings returning an error consumed by compile.Run before any write, RetireWithSource's apply and its direct VerifyRetirementSource call on the checked path; sha256sum of every recorded artifact digest matches HEAD; TestRetirementProblemsReturnsOperationalError and TestRetireAndCompileOperationalSourceExit pass live at their declared paths; go build, go vet and the rules, graph and cmd/sdd suites green. Minor: neither node's artifacts list internal/graph/ops/retirement.go, internal/graph/compile/compile.go or anchor.go, which dcbe2cd touched. Question resolved as not drift: anchor.go:85-99's Sources.Validate discard for split, amend, audit and reverify is self-documented and outside both contracts' literal wording, a residual gap. VERDICT: Aligned."
  - lane: review_quality
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..dcbe2cde9a3de001f90b472ba9012695ae72993d"
    evidence: "quality-scanner (agent, delta ed86c14..dcbe2cd; gofmt -l, go vet and staticcheck clean on rules, graph/... and cmd/sdd with the two SA4006 gone; go test -race -count=1 ok on rules 7.0s, all 16 graph subpackages and cmd/sdd 2.8s incl. both TestRetireAndCompileOperationalSourceExit subtests): compile.go:459 and ops/retirement.go:58 adopt RetirementProblemsChecked; RetireWithSource's :20-26 and :58-61 sites gate on ErrOperational before choosing raw error (exit 2 via root.go:692-701) or RefusedError; store.go:117-119 returns the callback error with no write and dry-run only loads; the cmd/sdd shim fixture resolves cat/chmod/git to absolute paths and threads PATH through cmd.Env, no leak; errors.Join with one error keeps the message. Minor: retirement.go:91 and :123 doc comments name callers that no longer use the lossy RetirementProblems, which has no production caller. VERDICT: Aligned."
  - lane: review_spec_compliance
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..dcbe2cde9a3de001f90b472ba9012695ae72993d"
    evidence: "spec-compliance (agent, delta ed86c14..dcbe2cd on internal/rules, internal/graph, cmd/sdd; both new tests run uncached and pass; errors.Join on an empty slice verified nil with a scratch go run): FR-16/AC-08/DD-10 — RetirementProblemsChecked at retirement.go:100-118 consumed by compile.go:107-110 and ops/retirement.go:20-23,58-62, reaching root.go:692-700's default exit 2, graph byte-identical on failure, genuine negatives still reported (operational_test.go:2113-2127 control). Major FR-16 gap in the same migration surface: anchor.go:92-99 Sources.Validate discards the error, so graph audit (audit.go:124-127) reports OK with empty findings and graph amend (amend.go:101,106) and graph split (ops.go:93,98) proceed blind; no test in audit_test.go or graph/ops tests covers it; neither spec nor design exempts those verbs; the anchor.go comment's 'reverify' claim is inaccurate (sync/reverify.go never calls Validate). VERDICT: Needs changes."
  - lane: review_blind_spots
    result: PASS/Aligned
    reviewed_identity: "04c1e612439e3c2c036a90530e45e548e3a02c8d..dcbe2cde9a3de001f90b472ba9012695ae72993d"
    evidence: "blind-spot-finder (agent, diff only ed86c14..dcbe2cd; read retirement.go, anchor.go, compile.go, ops/retirement.go, store.go Update/Load/Save, ops.go splitWith and introducedFindings, amend.go AmendFromReview in full; go build and go vet clean): both fixed sites are ordered before any write (apply runs before WriteAtomicExpecting; dry-run never reaches Update) and two sources with one operational and one genuinely bad are both reported. Major — anchor.go:92-99 Sources.Validate discards semanticFindings' error outright ('findings, _ :='), which unlike the lossy RetirementProblems fold means graph split (ops.go:93,98) and graph amend (amend.go:101,106), both calling it twice inside their CAS closure, can see an empty before/after delta and write on an unverified retirement source with no error or exit 2 — a strict regression versus the pre-diff fold for those callers. Minor — two lossy wrappers with different semantics and no doc distinction. VERDICT: Needs changes."
findings:
  - id: F-01
    severity: critical
    title: "graph split, amend and audit gate on a validation that discards the operational retirement error"
    status: open
    action: revise
    nodes: [propagation-graph-callers]
    revise:
      contract: "Graph sync, the graph provider, remap-revisions, review evidence and evidence add use checked detection and surface a failed Clean() or probe as an operational exit 2, never as a clean tree, a plain-tree fallback or a skipped check; remap-revisions treats a RevisionExists error that is not ErrNotFound as an operational failure and never reports it as a revision that is not an available commit; every rules sweep run by a cmd/sdd verb — the lifecycle transition gate's before and after sweeps and the review-resolve freeze gate — goes through the checked evaluation entry points and exits 2 with the cause when the sweep is operational, so an operational diagnostic is never deduplicated or path-filtered into an empty result and no status or frozen flag is written; graph compile and graph retire consume the retirement-problems entry point's operational error, so an unanswered retirement-source verification refuses the compile or the retire with exit 2 and writes nothing; and the shared graph validation behind graph split, graph amend and graph audit returns that operational error rather than discarding it, so split and amend refuse with exit 2 before their before/after comparison and audit reports the operational failure instead of a clean result."
      gate:
        type: tests
        tests:
          - {id: TestSyncCleanFailureIsOperational, file: internal/graph/sync/operational_test.go}
          - {id: TestProviderDetectionFailureIsOperational, file: internal/graph/provider/operational_test.go}
          - {id: TestLifecycleVerbsOperationalExit, file: cmd/sdd/operational_test.go}
          - {id: TestRemapRevisionsQueryFailureIsOperational, file: internal/graph/ops/operational_test.go}
          - {id: TestTransitionGateOperationalSweepExits, file: cmd/sdd/operational_test.go}
          - {id: TestReviewResolveOperationalSweepExits, file: cmd/sdd/operational_test.go}
          - {id: TestRetireAndCompileOperationalSourceExit, file: cmd/sdd/operational_test.go}
          - {id: TestSplitAmendAuditOperationalSourceExit, file: cmd/sdd/operational_test.go}
  - id: F-02
    severity: minor
    title: "Undeclared artifacts and stale comments"
    status: answered
followups: []
---

# Phase review: 06-acceptance

Reviewed `Plans/TestSuiteReliability/06-acceptance.md` at frozen identity `04c1e612439e3c2c036a90530e45e548e3a02c8d..dcbe2cde9a3de001f90b472ba9012695ae72993d`.

## Findings
### F-01 — graph split, amend and audit gate on a validation that discards the operational retirement error

Every lane converged: `internal/graph/compile/anchor.go:97` `Sources.Validate` does `findings, _ := semanticFindings(...)`; `graph split` (`ops.go:93,98`) and `graph amend` (`amend.go:101,106`) call it twice inside their CAS callback and gate the write on `introducedFindings` (`ops.go:292-304`, a string-set diff), so a symmetric operational probe failure nets to an empty delta and the mutation lands unverified; `graph audit` (`audit.go:124-127`) sets `OK` from the same lossy list and exits 0. No test covers any of the three; the in-code scope comment is unsourced and names reverify, which never calls `Validate`.

### F-02 — Undeclared artifacts and stale comments

`internal/graph/compile/compile.go`, `anchor.go` and `internal/graph/ops/retirement.go` are declared by no node although dcbe2cd implements the node's contract in them; `retirement.go:91,123` doc comments name callers that moved.

## Resolution Log
### F-02 — answered

2026-09-13: The three files are added to `propagation-graph-callers` with `sdd graph set-artifacts --add` after this amendment applies, and the comments are corrected in the rev 6 commit.
