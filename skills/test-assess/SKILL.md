---
name: test-assess
description: "Assess either a prospective test design card or real test execution facts. Returns located quality findings and routing, distinguishing intended behavior failures from test, fixture, setup, capture, and implementation failures."
disable-model-invocation: true
---

# Test Assessment

## Resources

Use the caller's resolved active plugin root. Independently follow symlinks in this loaded file's real path and verify that it is `<plugin-root>/skills/test-assess/SKILL.md` beneath the same sibling `skills/` and `shared/` directories and matching plugin manifest. Read `<plugin-root>/docs/TDD-TEST-DESIGN.md` in place as the standard. Never resolve resources from the target repository's current working directory or copy the plugin into it.

This is semantic judgment, not execution or graph authority. Never supply an exit code, invent a run, turn prose into observed evidence, write graph state, or replace the existing completion review.

For a **design challenge**, read the complete card, cited intent, current subject, and existing tests. Ask why each test, assertion, and level is needed; which relevant defect it detects; whether the real subject and seam are exercised; whether the oracle follows from authority; and whether the case adds value beyond existing coverage. A fresh non-inheriting context is optional, not mandatory. If unavailable, perform the same challenge in the current context and label it same-context assessment.

For a **run assessment**, require the untouched native report plus repository-produced metadata and deterministic validator facts when available. Quote those facts verbatim and identify the report/metadata files; keep them separate from the model's quality judgment. Never fill missing metadata, infer an exit code, or reconstruct output/content snapshots after the fact. Distinguish selected-test assertion failure from build/setup failure, missing discovery, timeout, crash, overflow, incomplete capture, and report ambiguity. Classify as `expected-behavior` only an executed assertion failure corresponding to absent intended behavior, a valid isolated sensitivity run whose selected test detects its named injected fault, or a deliberate compiler-rejection harness test whose harness succeeds and compiler diagnostic matches its oracle. Accidental build, import, syntax, setup, discovery, or capture failure never becomes behavioral RED.

Classify run failures as `expected-behavior`, `test-or-fixture-defect`, `implementation-defect`, or `unresolved`. Assessment does not admit evidence, repair incomplete producer metadata, or grant legacy reports `reported-v1` provenance.

Return exactly this shape:

## Assessment
Write exactly `Assessment: ready`, `Assessment: changes-required`, or `Assessment: blocked`. For run assessment, follow it with the report/metadata paths (or `legacy report — no reported-v1 metadata`) and available validator facts quoted verbatim.

## Located findings
For each finding: card case/test identity, root-relative source or output location, concrete defect or confirming evidence, and run classification when applicable. State `None` only after checking every case.

## Next action
One route: proceed to graph proposal/generation/implementation/admission, return to design, correct test or fixture generation, diagnose implementation, reconcile scope, or remain blocked. Graph mutations and evidence admission stay coordinator-owned.
