---
title: Repository Test Evidence
type: design
status: superseded
superseded_by: "Designs/SequenceFreshness/README.md"
created: 2026-09-15
updated: 2026-09-16
tags: [testing, evidence, graph, ownership]
related: [Designs/SddGraph/README.md, Designs/VerificationFreshness/README.md]
supersedes: Designs/TestEvidencePipeline/README.md
---

# Repository Test Evidence

## Overview
SDD owns graph requirements and evidence validation, not test execution. The graph-walking agent invokes repository-owned test tooling and supplies reports to sdd, which validates that the required tests were present, executed, and satisfied the gate before recording an observation.

This is the exact expectation recorded as `SddGraph:pd-769e9114`. The earlier TestEvidencePipeline design describes an experimental evidence-gathering pilot, not an adopted production execution responsibility. This successor reconciles that boundary without discarding useful test-quality work or historical measurements. Its detailed wire contract is [Repository-owned test reports](../../../docs/TEST-REPORT-CONTRACT.md).

## Non-Goals
- A production `sdd test` command, hidden execution flag, test-runner service, or SDD-issued run ticket.
- Optimization, branch integration redesign, universal runner support, or automatic dependency discovery.
- Authentication of a dishonest evidence producer or semantic proof that assertions are adequate.
- Rewriting completed graph history, granting richer guarantees to legacy imports, or treating experiment results as production approval.

## Architecture

### Components
- The graph declares required package-qualified tests, source/support membership, and a report profile.
- The graph-walking agent composes test design, generation, and assessment using the existing quality guide.
- Repository-owned tooling selects and executes commands, captures native output, samples before/after context, and produces metadata.
- Read-only `graph evidence-context` exports current requirements, claim identity, and content binding. It executes/probes nothing, renews nothing, and issues no receipt.
- SDD parses reports and validates metadata, current content, compatible red, and publication conditions before recording observations.

### Data Flow
Graph requirements -> agent invokes repository tooling -> reports supplied to SDD -> SDD validates the gate -> observation recorded.

Author all selected tests and minimum callable scaffolding before baseline RED; missing source or build/setup failure is not a qualifying assertion failure. Repository tooling captures before context, performs a real run, and captures after context. The coordinator assesses results and supplies both native output and metadata to sync. After implementation, repeat for GREEN, commit identical tested bytes, admit the pass, then follow existing rebase/fast-forward integration and frozen review rules. No assessment verdict substitutes for the mechanical evidence.

### Interfaces
`reported-v1` tests gates declare `report: {format, runner, environment_keys, test_support_inputs, test_support_artifacts}` and explicit test package identities. They do not declare an execution profile. The initial native decoder is Go test JSON; repository tooling, not SDD, owns the Go command and environment.

`sdd graph evidence-context --plan P --node N --by H --json` returns current binding without writing. `sdd graph sync --plan P --node N --by H --report native.json --metadata metadata.json` is the admission path. Both files are mandatory; metadata is forbidden on other protocols/gates. The strict versioned metadata contains before/after contexts, phase/red purpose, ordered timestamps, logical runner/environment identities, complete execution facts and explicit exit code, and the exact report digest. The technical contract specifies fields, limits, hashing, refusal cases, and producer trust boundaries.

## Design Decisions
- **DD-1**: Preserve the exact ownership boundary recorded in `SddGraph:pd-769e9114`; SDD validates requirements and evidence, while repository tooling executes tests.
- **DD-2**: Replace production attempt capture/admission with explicit reported-v1 native report and metadata ingestion, without an optional executor or a downgrade fallback.
- **DD-3**: Keep context export read-only and content-based, not an issued ticket. Before/after/current identities must agree; repository tooling is responsible for truthful contemporaneous capture.
- **DD-4**: Retain strict package/test execution completeness, failure attribution, and red-before-green validation. Parser results, not model outcomes or an exit code alone, determine the observation.
- **DD-5**: Bind red compatibility to test/hazard/support/report/runner identity rather than implementation bytes or revision number alone. Revalidate source, intent, claim, compatible red, and clean passing workspaces on every publication CAS attempt.
- **DD-6**: Preserve compact reported-evidence consumption and red links atomically with the observation. Exact report/metadata replay acknowledges history without changing sequence, claim, or latest result.
- **DD-7**: Retain observed-v1 historical readability but retire new authoring/admission. Active experimental gates require explicit amendment and fresh reported evidence; legacy evidence keeps its actual semantics.
- **DD-8**: Retain composed test-quality skills and developer-only experiments, and verify the corrected flow before considering further optimization.

## Error Handling
Missing, skipped, ambiguous, repeated, wrong-package, incomplete, build-failed, or unaccounted test output refuses admission. Invalid metadata, changed source/intent/support, expired or replaced claims, missing compatible red, and dirty passing workspaces also refuse without observation mutation. No fallback repairs evidence by assertion. Historical acknowledgement is read-only, including after a successful observation whose workspace release failed. Producer clock and environment identity inconsistencies require diagnosis, not invented metadata.

## Testing Strategy
Run a real temporary-repository exercise with repository-owned tooling: behavioral RED, build-failure refusal, GREEN against unchanged tests, byte-identical commit and fast-forward integration, and idempotent old-RED/GREEN replay. Poison the Go executable on the context/admission path to detect unintended execution or probing. Use deterministic tests for source/intent/support drift, claim takeover/expiry, publication races, malformed metadata and persisted links, protocol routing, path boundaries, amendment carry-over, and historical reads.

### Structural Verification
- Run focused uncached Go tests for decoder, report validation, graph operations/state/sync, CLI, and experimental tooling.
- Run `make plugins`, `make plugins-check`, and the full `make test` gate with the documented Windows CGO/compiler environment.
- Validate this design and its supersession through `sdd validate`; never edit decision files, committed graphs, or lifecycle status by hand.
- Independent code review checks the correction against the technical contract; test success alone is not architectural acceptance.

## Migration / Rollout
1. Preserve the prior design as superseded experimental history; use this successor and the exact recorded expectation for current work.
2. Remove the production test command and new attempt admission; keep executable experiment code outside production dependencies.
3. Align graph schemas, validator, CLI, walking instructions, and generated portable resources with reported-v1.
4. Require explicit amendment and fresh evidence for active experimental gates, without rewriting their observations or automatically inheriting red.
5. Exercise and review the corrected flow, run the full repository gate, and retain local changes for user review. No publication or optimization is part of this correction.

## Open Questions
- Additional strict native report decoders are non-blocking follow-up work; this correction promises only the specified Go JSON format.
- Input completeness and semantic assertion adequacy remain design/review responsibilities, not guarantees obtainable from report metadata.
