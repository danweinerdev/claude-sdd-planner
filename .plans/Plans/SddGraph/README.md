---
title: "SddGraph Implementation"
type: plan
status: active
created: 2026-08-31
updated: 2026-08-31
tags: [graph, execution, tdd, sdd-cli]
related: [Designs/SddGraph]
waivers:
  - code: SDD160
    reason: "Specs/SDD-Toolchain is reachable only transitively through Designs/SddGraph; its FR/NFR set is implemented and evidence-gated by the complete Plans/SDD-Toolchain. This plan cites only the toolchain requirements its scope actually touches (FR-17, FR-18, FR-28, FR-36, FR-44) and owes no coverage of the rest."
    accepted: "2026-08-31"
  - code: SDD161
    reason: "Designs/SddGraph deliberately cites only the toolchain FRs it amends or extends (FR-17, FR-28, FR-36, FR-44..FR-47); full design-side coverage of Specs/SDD-Toolchain belongs to that spec's own implementation lineage, not to this design."
    accepted: "2026-08-31"
  - code: SDD162
    reason: "Same transitive-reachability situation as the SDD160 waiver: Specs/SDD-Toolchain acceptance criteria are satisfied and evidenced by the complete Plans/SDD-Toolchain; this plan's acceptance criteria trace to Designs/SddGraph DD ids and ledger entries D-0022/D-0023 instead."
    accepted: "2026-08-31"
phases:
  - id: 1
    title: "Payload Schema and Templates"
    status: complete
    doc: "01-Payload-Schema-And-Templates.md"
    depends_on: []
  - id: 2
    title: "Graph Store, Compiler, Convert"
    status: complete
    doc: "02-Graph-Store-Compiler-Convert.md"
    depends_on: [1]
  - id: 3
    title: "Execution Loop"
    status: complete
    doc: "03-Execution-Loop.md"
    depends_on: [2]
  - id: 4
    title: "Review Gates and Analytics"
    status: complete
    doc: "04-Review-Gates-And-Analytics.md"
    depends_on: [3]
  - id: 5
    title: "Skill Rewrites and Self-Hosting"
    status: in-progress
    doc: "05-Skill-Rewrites-And-Self-Hosting.md"
    depends_on: [4]
---

# SddGraph Implementation

## Overview

Implements `Designs/SddGraph`: the plan-as-graph, observation-gated execution
model. A plan compiles from JSON payloads into a committed dependency graph of
test-gated nodes; node state (BLOCKED / READY / RED / GREEN / STALE) is derived
on read from structure plus recorded observations; the only path to GREEN is a
mechanical artifact synced through the `sdd` binary (D-0022); review weight
attaches to feature gates whose full validation cycle *truly closes* their
dependency closure (DD-9). Plan and phase markdown become rendered views of
the graph (DD-1, DD-2).

The five phases mirror the design's own rollout section: each is
independently shippable, and the mechanism protecting each honesty property
lands in the same phase as the property itself (e.g., red-before-green ships
with sync-only completion in phase 3, per DD-5).

## Non-Goals

Carried forward from `Designs/SddGraph` § Non-Goals, plus planning-time
boundaries:

- **Not a source-control system.** Observations store digests, anchors, and
  file lists — never diffs.
- **No parallel verification for Perforce.** The p4 provider ships at
  capacity 1 (single client, single pending CL) per DD-8; shelve-windows and
  narrow clients are out of scope.
- **No new gate plugins.** The gate vocabulary is closed: `tests`,
  `command`, `review` (DD-9).
- **The prose intent layer is untouched.** Specs and designs continue
  through `sdd apply` (DD-1); no work in this plan modifies the FR-17
  Markdown-proposal compiler beyond guard-surface entries.
- **No plan/phase markdown compiler.** The FR-36 plan/phase leg is
  superseded (recorded in `Specs/SDD-Toolchain` FR-36's amendment); building
  it is a scope violation.
- **No artifact directory re-layout.** Spec/design naming stays as-is
  (design OQ-1); rendered views keep `README.md` + numbered phase docs.
- **No browser/UI companion.** Analytics are CLI subcommands with `--json`;
  a live dashboard is future work no requirement demands.
- **No autonomous frontier dispatch policy.** `next --claim` serves one node
  to the caller; multi-agent orchestration policy lives in skills, not the
  binary (`shared/autonomy.md` boundaries hold).

## Architecture

```mermaid
graph TD
    subgraph "Phase 1"
        MODEL[internal/graph/model<br/>strict decoding]
        HAZ[internal/graph/hazards]
        TPL[sdd template graph-proposal<br/>skeleton + schema, one source]
    end
    subgraph "Phase 2"
        STORE[internal/graph/store<br/>reuses internal/store lock]
        COMPILE[sdd compile: coverage,<br/>intent hashes, rendered views]
        CONVERT[sdd graph convert<br/>v1 -> sentinels]
        GUARD[hook guard entries<br/>D-0014 / FR-28]
    end
    subgraph "Phase 3"
        STATES[internal/graph/states<br/>derived on read]
        NEXT[sdd next --claim<br/>leases]
        PROV[internal/graph/provider<br/>git / p4 / plain]
        SYNC[sdd graph sync --report<br/>merge gate, red-before-green]
    end
    subgraph "Phase 4"
        REVIEW[review gates: scope,<br/>closed predicate, demotion]
        ALGO[path / risk / shape / export]
    end
    subgraph "Phase 5"
        SKILLS[plan + implement skill rewrites]
        SELF[self-hosting pilot]
    end
    MODEL --> STORE --> STATES
    HAZ --> COMPILE
    TPL --> COMPILE
    COMPILE --> CONVERT
    STATES --> NEXT & SYNC & REVIEW
    PROV --> NEXT & SYNC
    STATES --> ALGO
    REVIEW --> SKILLS
    ALGO --> SKILLS
    SKILLS --> SELF
```

New code lands under `internal/graph/` (model, hazards, store, compile,
states, algorithms, sync, claims, provider) with CLI surfaces in `cmd/sdd/`
(`graph.go` verb tree; `next.go` extended). `internal/store`'s lock/atomic
primitives, `internal/vcs`'s adapters, `internal/schema`'s
template-coverage-test pattern, and `internal/hook`'s guard are reused, not
duplicated.

## Key Decisions

- **Phase order follows the design's rollout (DD-1..DD-16)** — notably
  red-before-green enforcement lands in phase 3 *with* sync-only completion,
  not after it (DD-5), and guard entries land in the same revision as each
  new verb (D-0014, FR-44 discipline).
- **`internal/graph/` is a sibling package family, not an extension of
  `internal/compile`** — the markdown-artifact compiler and the graph
  compiler share the store lock and schema-gate patterns but no payload
  code; conflating them would couple the FR-17 surface this plan must not
  touch.
- **Report formats for v1 sync: JUnit XML and `go test -json`.** This repo
  is the self-hosting target, so the Go-native format is required; JUnit
  covers pytest/ctest/gradle emitters generically. pytest-JSON is deferred
  until a consumer exists (necessity rule).
- **Lease TTL default 30 minutes**, key `graphLeaseTtlMinutes` in
  `planning-config.json` (resolves design OQ-2 — any value satisfies
  correctness; renewal is implicit on claim-holder store activity per
  DD-10).
- **Self-hosting pilot (task 5.4): this plan itself.** Once phase 3 lands,
  `graph convert` this plan and execute the remaining phase-5 work under
  `next`/`sync` — the design's own acceptance test.

## Dependencies

- Go toolchain + `staticcheck` (structural verification per
  `shared/language-verification.md`).
- Existing `make test` gates (regression corpus, template gate, portable
  drift/leak) stay green throughout — every phase's acceptance includes it.
- `sdd` v2.3.6+ installed for dogfooding validation runs.
- No external services, vendors, or unanswered stakeholder questions — no
  gated scope.

## Plan Completion Evidence

<!-- Keep the exact `Pending — not complete.` line until completion. Evidence
uses the exact labels `Verified`, `Repository`, `VCS`, `Revision / checkpoint`,
and `Identity recheck`, each exactly once visibly as `- <label>: <value>`. -->
Pending — not complete.
