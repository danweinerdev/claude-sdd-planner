---
title: "Execution Loop"
type: phase
plan: "SddGraph"
phase: 3
status: in-progress
created: 2026-08-31
updated: 2026-09-01
deliverable: "The walkable graph: derived states, claim/lease scheduling, per-VCS workspace providers, report-driven sync with digest anchoring, and the merge gate including red-before-green — sync-only completion live with its protections"
tasks:
  - id: "3.1"
    title: "Derived node states with three-way staleness"
    status: complete
    verification: "go test ./internal/graph/states/... -count=1 — table tests covering: deps-not-all-GREEN => BLOCKED, deps-all-GREEN never-verified => READY, last-fail => RED even with a non-GREEN dep (RED outranks BLOCKED), pass with no newer ancestor seq => GREEN; seq staleness (ancestor re-verified => descendant STALE), digest staleness (on-disk artifact digest differs from observation => STALE), intent staleness (cited requirement text no longer matches embedded hash => INTENT-STALE reported distinctly); workable = {READY, RED, STALE} but frontier excludes workable nodes with non-GREEN deps; states recomputed on read — no state field exists to store. Single topological pass over a 1000-node fixture completes under one second."
    justifies: "DD-3 (derived state cannot drift), DD-4 (INTENT-STALE), D-0022 (GREEN as assumed closure). Prevents the stored-status lie: a state edited outside the tool."
  - id: "3.2"
    title: "sdd next --claim: frontier, leases, claim records"
    status: complete
    verification: "go test ./cmd/sdd/ -run TestGraphNext -count=1 — next (read-only) lists the frontier critical-path-first and is allowed for read-only agents; next --claim atomically selects, records {by, lease_expires, workspace}, and returns the node with inlined cited AC/DD text, tests, hazards, and workspace handle; a second concurrent --claim never receives the same node (lock-serialized); claim on an empty frontier explains why (BLOCKED counts, RED counts); lease TTL defaults to 30 minutes via planning-config graphLeaseTtlMinutes; any store-touching verb by the claim holder renews the lease; expiry returns the node to the frontier and preserves the workspace file; sdd graph release clears a claim explicitly."
    justifies: "DD-10 (claims and leases in the master graph under the store lock; double-claim prevention independent of claim.by), D-0022. Prevents two agents working one node — the race dispatch discipline cannot structurally close."
    depends_on: ["3.1"]
  - id: "3.3"
    title: "Workspace providers: git, p4, plain"
    status: planned
    verification: "go test ./internal/graph/provider/... -count=1 — capacity answers: git N (worktree allocation/cleanup round-trip against a fixture repo), p4 1 (single shared client, plan/phase CL number recorded as provenance; opened-file list captured), plain 1 (digests only); provider handles are opaque to the graph (states/sync tests pass with a stub provider); allocation failure refuses the claim leaving the node unclaimed (named provider error); effective parallelism = min(artifact-disjoint frontier, capacity) with an artifact-overlap test; isolation classification: worktree => clean, serial-one-CL => clean by construction, concurrent shared-tree => shared-dirty."
    justifies: "DD-6 (digest anchor universal, VCS provenance supplementary), DD-7 (isolation as observation), DD-8 (parallelism is provider capacity, correctness holds at 1). Prevents a green report from a contaminated tree counting as clean evidence."
    depends_on: ["3.2"]
  - id: "3.4"
    title: "sdd graph sync --report: parsers, buckets, observations"
    status: planned
    verification: "go test ./internal/graph/sync/... -count=1 — JUnit XML and go test -json parsers produce identical observation semantics on equivalent fixtures; four buckets reported: updated, unresolved (declared, absent), untracked (present, unclaimed — decomposition warning), ambiguous (same id passed AND failed — never guessed, node stays unverified); parametrized folding: bare id passes only if every case passed, one failing case fails it, any skipped/xfailed case withholds the fold; observations record result, seq (monotonic from seq_counter), artifact_digests (SHA-256 of declared artifacts at sync time), report_digest, isolation from the provider, provenance; per-test first-failure red_seq recorded; command-gate observations capture exit code + output digest with full output teed to Plans/<Plan>/.graph/logs/<node>.log; sync of a review-verdict report is refused here (review gates are phase 4)."
    justifies: "DD-5 (the only path to GREEN is a parsed mechanical artifact), DD-6, D-0022. Prevents narrated completion — there is no verb by which an agent asserts a pass."
    depends_on: ["3.3"]
  - id: "3.5"
    title: "Merge gate and node lifecycle verbs: release, split, set-tests, gc"
    status: planned
    verification: "go test ./internal/graph/claims/... ./cmd/sdd/ -run 'TestMergeGate|TestGraphLifecycle' -count=1 — merge (the sync-completion path) refuses on: non-clean isolation (asserted refused by default, shared-dirty accepted only provisionally landing STALE-not-GREEN), unresolved declared tests, digest mismatch between report time and merge time, missing/dirty git revision anchor when provider is git, hazard-discharging test with no recorded red_seq earlier than its green seq (red-before-green); refusal names the failing condition and leaves the claim intact; successful merge is one atomic sequence under the lock: verify -> merge observation -> delete workspace file -> clear claim; split retires a node into children preserving id-retirement; set-tests edits a node's declared tests under the lock; gc reaps expired-claim workspace files listing what it removed."
    justifies: "DD-5 (red-before-green ships with sync-only completion, per the design's rollout ordering), DD-7, DD-10, D-0022. Prevents a tautological test greening a hazard-carrying node and a crashed agent stranding a claim."
    depends_on: ["3.4"]
  - id: "3.6"
    title: "Multi-process concurrency stress and race verification"
    status: planned
    verification: "go test ./internal/graph/... -run TestConcurrentWalk -count=1 — N separate OS processes (test-spawned sdd binaries) claiming from one shared frontier produce zero double-claims and a parseable graph after every interleaving (atomic-rename torn-read-free property), asserted on Windows and POSIX runners; go test ./internal/graph/... -race -count=1 clean across store, claims, states, sync; a lease-expiry takeover mid-walk preserves the previous workspace file and the takeover itself is lock-serialized."
    justifies: "Prevents the two concrete corruption failures the design's Testing Strategy names: double-claim under parallel dispatch and torn reads of the committed graph. DD-10, DD-3."
    depends_on: ["3.5"]
---

# Phase 3: Execution Loop

## Overview

Makes the compiled graph walkable and honest: states derive on every read;
`next --claim` schedules under the store lock with leases; providers supply
per-VCS workspaces, provenance, and isolation; `sync --report` converts real
test-runner output into observations (digest-anchored, red_seq-tracked); and
the merge gate enforces clean isolation and red-before-green in the same
phase that makes sync the only path to GREEN. After this phase a converted or
authored graph can be executed end to end by an agent that never asserts
completion — it only shows reports.

## 3.1: Derived node states with three-way staleness

### Subtasks
- [x] Create `internal/graph/states`: one topological pass computing
      `effective[n]` (highest verification seq among n and ancestors) and
      each node's state; export the workable/frontier distinction.
- [x] Implement precedence exactly: RED (recorded failure) checked before
      the deps-all-GREEN test; frontier re-gates on deps independently.
- [x] Digest staleness: recompute declared-artifact digests on read (cheap
      content hash; cache by mtime within one invocation) and compare to the
      observation.
- [x] Intent staleness: recheck embedded intent hashes via 2.3's shared
      normalizer against the related spec/design text; report INTENT-STALE
      as its own diagnostic with the requirement-diff payload.
- [x] Table tests per the verification field plus the 1000-node performance
      fixture.

### Notes
Revision boundary: the states package with no CLI surface (3.2 exposes it).
Nothing here persists — the package doc-comment restates DD-3's rule
("nothing derivable is stored") so future contributors don't add a cache
field to the model. INTENT-STALE remediation is a judgment call routed to
the LLM (design § Error Handling); this task only detects and reports.
Design references: § Node states, DD-3, DD-4, D-0022.

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `e5e9ed859d14e91b99e2709c419215ecfd547136`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `e5e9ed859d14e91b99e2709c419215ecfd547136`
- Focused review: `git show e5e9ed859d14e91b99e2709c419215ecfd547136`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `e5e9ed859d14e91b99e2709c419215ecfd547136`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./internal/graph/... -count=1` | `.` | PASS (`exit 0`) | `ok across all ten internal/graph packages; state table covers deps-not-all-GREEN => BLOCKED, never-verified-with-GREEN-deps => READY, RED outranks BLOCKED (failed node with non-GREEN dep reports RED), pass with no newer ancestor => GREEN, direct and transitive seq staleness via effective[n], digest staleness naming exactly the drifted artifacts (including one declared after the observation) with the axis disabled when no digest source is supplied, INTENT-STALE naming exactly the drifted or unresolvable citation and attributed distinctly from the other axes; workable = {READY,RED,STALE} with the frontier excluding workable nodes with non-GREEN deps; cycle members derive BLOCKED and flagged, never workable; Derive is pure (a caller's mutation cannot leak into a later pass); a 1000-node derive completes under one second; digest helpers agree between File and Bytes, memoize within one run, and report missing artifacts as the distinct empty value` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

### Trap
You will want to store computed states back into the graph JSON "as a
cache". Don't — a stored state drifts the moment code is edited outside the
tool, which is the exact failure the design exists to remove. If read-time
derivation is ever too slow, the sanctioned fix is a faster pass, never a
persisted state.

## 3.2: sdd next --claim: frontier, leases, claim records

### Subtasks
- [x] Extend `cmd/sdd/next.go`: graph-aware mode when a graph exists for the
      plan (v1 markdown behavior untouched otherwise).
- [x] Read path: frontier listing, critical-path-first ordering (estimate
      sums via a minimal longest-path helper; full analytics land in 4.2),
      `--json`.
- [x] `--claim`: under the store lock select frontier head (respecting
      artifact-disjointness against outstanding claims and provider
      capacity), write the claim record, allocate the provider workspace
      (3.3 interface; stub provider until it lands), return the full context
      payload with inlined cited requirement text.
- [x] Lease mechanics: `graphLeaseTtlMinutes` (default 30) from
      planning-config; implicit renewal on any claim-holder store-touching
      verb; expiry-at-read returns the node to the frontier preserving the
      workspace file.
- [x] `sdd graph release <node>` for graceful abandonment.
- [x] Guard entries land in this task's revision (D-0014 discipline —
      `next --claim` mutating, bare `next` read-only).

### Notes
Revision boundary: claimable frontier with leases, against a stub provider
if 3.3 has not merged (interface defined here, implementations there —
order the two tasks by that seam). The context payload is the token-economy
core of the design's workflow: an agent gets everything needed to work the
node from one call. Design references: DD-10, § The execution loop, design
OQ-2 (resolved here: 30-minute default).

### Completion Evidence

- Verified: 2026-09-01
- Repository: `.`
- VCS: `git`
- Revision / checkpoint: `f6fe384534017e87733746548e46513bef5d5580`
- Identity recheck: `git rev-parse HEAD` at 2026-09-01 00:00 matched `f6fe384534017e87733746548e46513bef5d5580`
- Focused review: `git show f6fe384534017e87733746548e46513bef5d5580`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `f6fe384534017e87733746548e46513bef5d5580`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
|---|---|---|---|
| `go test ./cmd/sdd/ ./internal/graph/... -count=1` | `.` | PASS (`exit 0`) | `ok across cmd/sdd 25.1s and all eleven internal/graph packages; claim picks the heaviest frontier node (critical-path-first) and commits the record to the store; artifact-overlap and provider-capacity screens verified (capacity refusal names itself); expiry-at-read returns a lapsed node to the frontier, reports the reclaim, and preserves the workspace; renew and release are holder-only with --force takeover; allocation failure refuses the claim with no record written; empty-frontier refusals carry state counts and active-claim totals; lease TTL flows from planning-config graphLeaseTtlMinutes` |
| `go vet ./...` | `.` | PASS (`exit 0`) | `no findings` |
| `staticcheck ./internal/graph/...` | `.` | PASS (`exit 0`) | `no findings` |

| Tool / inspection | Context | Result | Observable evidence |
|---|---|---|---|
| `sdd next read/claim/release smoke in a temp planning root` | `built binary at f6fe384` | PASS | `read lists the frontier critical-path-first with weights and claim annotations; claim printed the context payload with inlined FR-01 requirement text, gate tests, hazards, artifacts, and a 45-minute lease honoring the config key; read-after-claim showed claimed=1; holder release returned the node` |

## 3.3: Workspace providers: git, p4, plain

### Subtasks
- [ ] Define the provider interface in `internal/graph/provider`: capacity
      for an artifact set, allocate/release workspace, provenance for an
      observation, isolation classification.
- [ ] Git provider over `internal/vcs/git.go`: worktree per claim under
      `Plans/<Plan>/.graph/ws-<uuid7>/` (gitignored), branch from merged
      state, provenance = commit hash + worktree path, cleanup on
      merge/release.
- [ ] P4 provider over `internal/vcs/p4.go`: capacity 1, single shared
      client, plan/phase pending-CL number + opened-file list as provenance,
      serial execution => isolation clean by construction.
- [ ] Plain provider: capacity 1, digest-only provenance.
- [ ] Allocation-failure semantics: named provider error, claim refused,
      node stays on the frontier (design § Error Handling).
- [ ] Fixture tests per adapter (git fixture repo; p4 mocked at the
      command-runner seam consistent with existing `internal/vcs` tests).

### Notes
Revision boundary: three working providers behind one interface; 3.2's stub
swaps out. Correctness must hold at capacity 1 everywhere (DD-8) — the git
provider's N is a throughput optimization, and tests must pass with git
capacity forced to 1. Provenance is supplementary; the digest anchor (3.4)
is load-bearing. Design references: DD-6, DD-7, DD-8; `shared/vcs-detection.md`.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

### Trap
Do not make graph correctness depend on provider richness — no "if git then
we can skip the digest check" shortcuts. The digest anchor is universal
precisely so that p4 and plain trees are first-class; provider-conditional
correctness re-creates the second-class-VCS problem DD-6 was decided to
kill.

## 3.4: sdd graph sync --report: parsers, buckets, observations

### Subtasks
- [ ] Create `internal/graph/sync`: JUnit XML parser and `go test -json`
      parser normalizing to one internal report shape (id, outcome, case
      parameters).
- [ ] Bucket computation: updated / unresolved / untracked / ambiguous;
      ambiguous and unresolved never guessed at (node stays unverified).
- [ ] Parametrized folding per the design's rules (bare id or one exact
      case; all-pass to pass; skip/xfail withholds).
- [ ] Observation assembly: result, seq (increment `seq_counter` under the
      lock), artifact digests at sync time, report digest, isolation from
      the provider, provenance, per-test `red_seq` on first observed
      failure.
- [ ] Command-gate observations: run is external — sync accepts
      `--command-exit`, `--command-log` (or reads the teed log), records
      exit + output digest; full output tees to
      `Plans/<Plan>/.graph/logs/<node>.log` (gitignored).
- [ ] `sync` refuses review-verdict inputs with a pointer to phase 4's
      review-gate flow.

### Notes
Revision boundary: `sdd graph sync --node <id> --report <file>` records
observations for tests/command gates end to end. The parsers consume hostile
external input — keep them dependency-free and fuzz-ready (4.3 adds the fuzz
targets; structure for it now). The design's honesty rule is enforced here
by omission: there is no assert-pass verb, and `isolation: "asserted"`
records exist only via an explicitly rare `--assert` escape that the merge
gate refuses by default (document it as such). Design references: DD-5,
DD-6, § The execution loop (sync semantics), design OQ-3 resolution on
`command` output capture.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

### Trap
The parsers will tempt you to resolve `ambiguous` by "last result wins" or
by rerun-ordering heuristics. The design forbids exactly this: ambiguity is
reported, never guessed at. An ambiguous id leaves the node unverified —
that is the correct behavior, not a missing feature.

## 3.5: Merge gate and node lifecycle verbs: release, split, set-tests, gc

### Subtasks
- [ ] Merge gate in `internal/graph/claims`: verify observation
      (clean-isolation default, declared tests resolved, digest match at
      merge time, git revision anchor clean when applicable,
      red-before-green for hazard-discharging tests) → merge → delete
      workspace file → clear claim, all under one lock hold.
- [ ] `shared-dirty` provisional acceptance: observation merges but the node
      derives STALE-not-GREEN until a clean re-verify (states already
      support it; wire the flag).
- [ ] `sdd graph split <node>`: retire into children (id retirement per the
      stable-identifier discipline), preserving deps and hazards triage
      state.
- [ ] `sdd graph set-tests <node>`: locked single-node test-list edit.
- [ ] `sdd graph gc`: reap workspace files of expired claims, listing
      removals; never touches unexpired claims.
- [ ] Refusal messages name the exact failing condition (design § Error
      Handling: merge-gate refusals).
- [ ] Guard entries for `sync|release|split|set-tests|gc` land in this
      task's revision (D-0014/FR-44 discipline: entries with verbs).

### Notes
Revision boundary: the complete claim→work→sync→merge lifecycle with every
protection the design requires live at the same time — red-before-green is
in this task, not a later hardening pass, per the design's rollout-ordering
decision (review finding: "the mechanism protecting sync-only completion
ships with sync-only completion"). Design references: DD-5, DD-7, DD-10,
D-0022; § Error Handling (lease expiry, merge-gate refusals).

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

## 3.6: Multi-process concurrency stress and race verification

### Subtasks
- [ ] Test harness spawning N `sdd` processes (the built binary) against one
      fixture graph: concurrent `next --claim` + `sync` + `gc` interleavings.
- [ ] Assertions: zero double-claims across all runs; every intermediate
      graph file parses (torn-read-free); final state consistent with the
      observation log.
- [ ] Run matrix: Windows and POSIX (CI-conditional where needed).
- [ ] `-race` across `internal/graph/...`.
- [ ] Lease-takeover interleaving: expire mid-walk, second process claims,
      first process's late sync refused (stale claim), workspace preserved.

### Notes
Revision boundary: the stress suite itself; no production code changes
except bugs it finds (fix-forward within this task if small, new task id if
material — plan-revision discipline). This is the design's named
concurrency acceptance. Multi-process (not just goroutine) coverage is the
point: the lock is advisory and cross-process, and `filelock` semantics
differ by OS. Design references: § Testing Strategy (concurrency), DD-3,
DD-10.

### Completion Evidence

<!-- Keep the exact pending line until completion. -->
Pending — not complete.

## Acceptance Criteria
- [ ] A fixture graph walks end to end — claim, red run, green run, sync,
      merge — with states correct at every step and no assert-pass path
      (DD-5, D-0022).
- [ ] RED-outranks-BLOCKED, workable≠frontier, and all three staleness
      triggers verified by table tests (DD-3, DD-4).
- [ ] Red-before-green refuses a never-failed hazard-discharging test at
      merge (DD-5, DD-13 mechanics).
- [ ] Providers: git worktrees at N and forced-1, p4 single-CL clean by
      construction, plain digest-only — all merge-gate compatible (DD-6,
      DD-7, DD-8).
- [ ] Zero double-claims and torn reads across the multi-process stress
      matrix; `-race` clean (DD-10).
- [ ] Guard entries cover every phase-3 verb; `make test` green.

## Phase Completion Evidence

<!-- Keep the exact `Pending — not complete.` line until completion. -->
Pending — not complete.
