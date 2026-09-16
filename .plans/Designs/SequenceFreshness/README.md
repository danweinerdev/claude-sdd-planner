---
title: Sequence-Based Freshness
type: design
status: draft
created: 2026-09-16
updated: 2026-09-16
tags: [graph, freshness, evidence, ownership]
related: [Designs/SddGraph/README.md, Designs/ReviewDrivenAmendment/README.md]
supersedes: [Designs/VerificationFreshness/README.md, Designs/RepositoryTestEvidence/README.md]
---

# Sequence-Based Freshness

## Overview
Node state derives from structure, observation sequence, contract revision, isolation, and red-before-green — never from content digests. This is the recorded decision `SddGraph:pd-d72f3bca`, which supersedes the digest-anchoring decisions of `SddGraph:DD-6` and the `VerificationFreshness` design. Ownership of test execution stays with repository tooling per `SddGraph:pd-769e9114`; SDD validates the parsed report and records the observation.

The motivating cost was churn: completed nodes went STALE because a neighbour, a formatter, or a dependency re-verified with unrelated byte changes touched a file in their declared surface, forcing re-verification that produced no new information. Under this design a GREEN node stays GREEN until something upstream is deliberately re-verified or its own contract changes.

## Non-Goals
- Detecting silent edits to a completed node's files. That guarantee is withdrawn deliberately; the phase review gate and the repository's own test suite carry it.
- Any content-bound evidence protocol (observed or reported attempts, before/after contexts, producer metadata). These are removed, not retained unused.
- A production test runner, hidden execution flag, or any SDD-issued run ticket.
- Rewriting committed graph history. Old graphs carrying digest fields still load; the fields are ignored.

## Architecture

### Components
- `internal/graph/states` derives state: a node with an observation is stale when a direct dependency has an observation with a higher sequence, when its own contract revision advanced past the observation, or when its isolation was not clean. Review nodes are stale when the reviewed scope changed, a reviewed node's contract revision advanced, or a reviewed node has a newer observation.
- `internal/graph/sync` records an observation from a parsed report or command result: result, sequence, contract revision, report digest (provenance and duplicate detection only), isolation, provenance, and the optional RED classification.
- `internal/testevidence` is the strict package-qualified Go test JSON parser used when a declared test carries `package`; the legacy fold remains for other reports.
- `internal/graph/model` strictly decodes committed graphs, tolerating and dropping removed digest and evidence keys, and refuses them in authored proposals.

### Data Flow
Graph requirements -> the walking agent invokes repository tooling -> the native report is supplied to `sdd graph sync` -> SDD parses it, checks the gate, red-before-green, claim, and workspace cleanliness -> the observation is recorded at the next sequence. A failing observation may carry `--red-kind baseline|sensitivity` and `--fault` for review visibility. Re-syncing a dependency records a new sequence and thereby stales its direct consumers; that is the only automatic staleness a byte change can cause, and it happens only because someone chose to re-verify.

### Interfaces
`sdd graph sync --plan P --node N --by H --report <file> [--report-exit N] [--red-kind baseline|sensitivity --fault <text>]` and `--command-exit/--command-log` for command gates. Duplicate report bytes for the same node, result, and contract revision are a no-op that burns no sequence. `sdd graph reverify --report` folds one run against every node the report fully covers. Removed verbs: `acknowledge`, `rehash`, `repair-intent`, `evidence-context`, `evidence-contract`; removed template `evidence-metadata`; removed `--cost` and `--metadata`. `set-inputs` replaces declarations without hash proofs.

## Design Decisions
- **DD-1**: Freshness is observation order plus contract revision; content digests are removed from state derivation and observations rather than kept unused. Supersedes `VerificationFreshness:DD-1` and `VerificationFreshness:DD-2`.
- **DD-2**: A dependency re-verified with a higher sequence stales only its direct consumers; transitive consumers stale as their own dependencies are re-observed. Ripple is deliberate, one hop per re-verification.
- **DD-3**: Review nodes bind to reviewed contract revisions and sequence, not artifact bytes. Narrows `ReviewDrivenAmendment:DD-9` to its contract-revision binding.
- **DD-4**: Artifacts and inputs remain declarations for review scope and workspace ownership; they are never hashed. Supersedes `VerificationFreshness:DD-4` narrowing proofs.
- **DD-5**: Report bytes are digested only to identify duplicates and to fence review-artifact replay; the digest never drives state.
- **DD-6**: Committed graphs carrying removed keys load with those keys ignored; authored proposals carrying them are refused with a removed-field diagnostic.
- **DD-7**: RED classification is two sync flags recorded on the failing observation, not a producer-authored document. Supersedes `RepositoryTestEvidence:DD-2`, `RepositoryTestEvidence:DD-3`, `RepositoryTestEvidence:DD-5` and `RepositoryTestEvidence:DD-6`.

## Error Handling
Unresolved, ambiguous, skipped, or wrong-package tests leave the node unverified with the report buckets explaining why. A pass with a claimed workspace still refuses on an unclean worktree. A red kind without a failing result, a sensitivity kind without a fault, or a baseline kind with a fault is refused. A claim that lands between evaluation and publication refuses the sync. A proposal carrying a removed field is refused naming the field.

## Testing Strategy
Unit tests pin the sequence rule (direct-consumer staleness, no automatic transitive ripple, byte-identical re-sync after revision records a new sequence, duplicate report no-op), review staleness by scope, revision, and sequence, decoder tolerance against every committed graph, proposal refusal of removed keys, RED classification validation, the in-CAS claim fence, and per-attempt CAS hygiene. The existing real-Git sync, claim, provider, and remap suites remain the integration bar.

### Structural Verification
- `make test`: Go suite, race gate, frozen regression corpus, template gate, portable drift and leak gates.
- `make plugins-check` after every canonical guidance change.
- `sdd validate` on the planning root; supersession of the prior designs recorded through `sdd design supersede`, never by editing status.

## Migration / Rollout
1. Record the decision (done: `SddGraph:pd-d72f3bca`) and supersede the two prior designs with this one.
2. Remove digest fields from model, state, sync, review, compile, and ops; tolerate them on decode.
3. Remove observed and reported evidence protocols, their verbs, templates, and experiment tooling.
4. Reconcile walking guidance, agent prompts, shared conventions, and portable trees.
5. Run the full gate; leave the change local for user review. No version bump or publication is part of this design.

## Open Questions
- Whether the phase review gate should surface "files changed since verification" as an advisory is deferred; the decision removes the hashes, so any such advisory would need its own source of truth.
