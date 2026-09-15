---
name: sdd-test-design
description: "Design focused tests from cited intent, inspected code, existing coverage, hazards, and project conventions before implementation or test generation. Produces a fixed-heading test design card without changing graph state."
disable-model-invocation: true
---

# Test Design

## Resources

Use the caller's resolved active plugin root. Independently follow symlinks in this loaded file's real path and verify that it is `<plugin-root>/skills/sdd-test-design/SKILL.md` beneath the same sibling `skills/` and `shared/` directories and matching plugin manifest. Read `<plugin-root>/shared/test-design.md` in place as the quality standard. Never resolve resources from the target repository's current working directory or copy the plugin into it.

Inspect the cited intent, actual subject and collaborators, existing tests, discovery conventions, fixtures, and runner before designing cases. Do not invent an interface, oracle, hazard, or requirement to complete the card. Missing authority, a missing real seam, or an infeasible setup blocks readiness and returns to investigation or scope reconciliation.

Choose a representative success case and only the boundaries, failures, state transitions, interactions, and security cases justified by risk. Name the relevant defect each assertion would detect. Prefer the cheapest level that exercises the real subject and can establish the contract. Reuse adequate coverage; do not create tests to satisfy a count or coverage score.

Return exactly this fixed-heading Markdown card, retaining every heading and writing `Not applicable — <reason>` where needed:

## Context
Plan/task or node identity, cited intent, inspected root-relative source and test identities, and relevant project conventions.

## Behavior and source
Observable obligation and the requirement, contract, design decision, or reproduced defect that establishes it.

## Risk and level
Relevant failure mode, chosen unit/component/integration/contract/end-to-end level, and why that level is sufficient.

## Existing coverage
Coverage inspected and the concrete gap or deliberate strengthening. Name adequate tests to reuse.

## Cases and oracle
For each case: expected test ID and file, stimulus, observable expected outcome, independent oracle, and relevant defect detected.

## Subject and seam
Real entrypoint exercised, collaborators and fixtures, how injection or setup reaches the intended path, and any engineering setup that must be reconciled before generation.

## Red and sensitivity
Expected behavioral RED reason. Add a sensitivity experiment only for a named uncertainty the ordinary baseline cannot establish; describe isolation and restoration. Never prescribe a synthetic setup/build failure as RED.

## Execution
Selected test identities, targeted discovery/execution command or profile, surrounding regressions, and intended evidence mode. Mark runtime capabilities as unchecked until the coordinator checks them.

The card is working context, not a graph node or completion artifact. Do not write graph state. The same context may continue to generation; an optional fresh non-inheriting context may challenge the card when available, receiving this complete card and its referenced material.
