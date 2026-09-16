---
name: sdd-test-generate
description: "Generate focused tests from an assessed fixed-heading design card using actual project fixtures and runner conventions. Reports exact edits and discoveries without implementing production behavior or claiming execution outcomes."
disable-model-invocation: true
---

# Test Generation

## Resources

Use the caller's resolved active plugin root. Independently follow symlinks in this loaded file's real path and verify that it is `<plugin-root>/skills/sdd-test-generate/SKILL.md` beneath the same sibling `skills/` and `shared/` directories and matching plugin manifest. Read `<plugin-root>/shared/test-design.md` in place. Never resolve resources from the target repository's current working directory or copy the plugin into it.

Read the complete design card, cited intent, and current source/tests. Confirm every planned symbol, fixture, seam, file, test identity, and discovery convention against the current workspace before editing. A changed expectation returns to design assessment; a missing engineering obligation or declaration mismatch is an unresolved finding, not silent implementation scope.

Author the smallest focused tests and necessary test-only scaffolding described by the card. If the approved card requires a new interface so the tests compile, a minimal interface-only stub may be added in the real source; it must contain no production behavior or validation, must not make the intended RED disappear, and must never overwrite existing working code. Preserve exact declared runner-visible identities. Exercise the real subject, keep the oracle independent of unfinished implementation logic, and verify fault injection reaches the intended boundary. Do not implement production behavior, add unrelated implementation, hardcode a pseudo-outcome, weaken an assertion to make it pass, or substitute a renamed test silently.

Do not claim RED, GREEN, discovery, or admissible evidence from generated text. Execution and native report production happen through repository-owned tooling after generation; `sdd` validates the supplied report and never supplies missing execution facts.

Return exactly these headings:

## Files changed
Root-relative test, test-support, and approved interface-stub source files actually changed, or `None`.

## Test identities
Package, runner-visible test ID, and file for every authored or reused selected test, matched to its card case.

## Scaffolding introduced
Report `Test-only:` fixtures/helpers/stubs introduced and why each is necessary, then `Interface-only production stubs:` each approved declaration-only source stub and why the card requires it. State `None` separately for either category when absent.

## Unresolved findings
Located mismatches, missing seams, changed identities, ambiguous oracles, discovered implementation work, or `None`. Never resolve one by expanding production scope.
