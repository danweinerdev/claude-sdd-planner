---
title: "Cross-Platform Go SDD Validator"
type: spec
status: superseded
superseded_by: Specs/SDD-Toolchain
created: 2026-08-02
updated: 2026-08-31
tags: [validation, golang, cross-platform, testing]
related: [Specs/SDD-Toolchain]
---

# Cross-Platform Go SDD Validator

> **Superseded by [`Specs/SDD-Toolchain`](../SDD-Toolchain/README.md).** The Go
> port is now layer 1 of a larger toolchain that also makes `sdd` the only
> writer of SDD artifacts. Every requirement below is carried forward with its
> substance intact, except that the two-executable command surface
> (`sdd-validate` + `sdd-decision`, ten bundled binaries) is replaced by a single
> `sdd` binary with subcommands across five OS/architecture targets. The
> normative parity manifest `current-test-coverage.csv` moved to the superseding
> spec's directory. This document is retained as the origin record of the parity
> discipline; do not implement from it.

## Overview
Replace `scripts/sdd_validate.py` and `scripts/sdd_decision_validate.py` with
the native `sdd-validate` and `sdd-decision` commands, built from one shared Go
implementation and runnable without Python or PyYAML on Windows, macOS, and Linux.
The migration preserves the current deterministic contracts and every current
test case, closes the large characterization gaps around untested diagnostics,
and deliberately corrects related-path resolution and split-task identifiers.

The Python implementations remain the behavioral oracle during migration,
except for the documented-conformance correction in FR-14 and the two reported
bug fixes in FR-16 and FR-17.

## Goals
- Ship self-contained full-artifact and focused-ledger commands for Windows, macOS, and Linux.
- Preserve both the full-artifact and focused decision-ledger validation modes.
- Give every one of the 74 current Python tests a traceable Go counterpart.
- Characterize every implemented diagnostic and material message branch before removing the Python oracle.
- Preserve machine-consumable CLI, output, exit-code, path, YAML, Markdown, and Git behavior unless this specification explicitly changes it.
- Fix planning-root-relative `related` resolution and support letter-suffixed task splits.

## Non-Goals
- Add semantic or model-judgment checks to the deterministic validator.
- Execute commands recorded in completion evidence.
- Rewrite `hooks/load-decisions.sh`; it is a separate permissive session-context loader, not a validator entry point.
- Add completion adapters for SCMs that the Python validator does not already support.
- Redesign artifact, evidence, review, or decision-ledger schemas beyond the task-id grammar change in FR-17.
- Preserve Python as a runtime dependency or retain Python wrappers after migration.

## Requirements

### Functional Requirements
- **FR-01**: The repository SHALL provide `sdd-validate` for full-artifact validation and `sdd-decision` for focused decision-ledger validation. Both commands SHALL be built from one shared Go implementation without requiring duplicated validation logic.
- **FR-02**: `sdd-validate [--root PATH] [--scope SCOPE] [--format text|json] [--identity-mode auto|current|historical]` SHALL preserve the full validator's current options and defaults. `sdd-decision LEDGER [--format text|json] [--no-history]` SHALL preserve the focused validator's current positional argument, options, and defaults. Unknown options, missing values, invalid choices, extra positional arguments, and cross-command options SHALL print command-specific usage to stderr and exit `2`. Usage and invocation-error prefixes SHALL canonically name the new executables; replacing Python script names is an explicit compatibility delta.
- **FR-03**: Both modes SHALL preserve existing exit semantics: `0` for a valid result including candidate-only findings, `1` for authoritative validation errors without an operational failure, and `2` when invocation or validation cannot run.
- **FR-04**: For all behavior except FR-02 command naming, FR-12 focused-ledger path aliases, FR-14, FR-16, FR-17, and malformed-YAML parser prose under FR-08, the Go commands SHALL preserve each diagnostic's code, severity, path, line, message, correction, implicated paths when applicable, multiplicity, and deterministic sort order.
- **FR-05**: The Go tool SHALL preserve normal and operational text and JSON response shapes, stdout/stderr routing, JSON key ordering, indentation, escaping, and trailing-newline behavior so existing command consumers continue to parse results unchanged.
- **FR-06**: Full-artifact mode SHALL preserve discovery, parsing, common schema, required-heading, stable-ID, artifact-specific, hierarchy, citation, dependency-graph, traceability, decision-link, completion-evidence, review-gate, repository-identity, and lifecycle-durability checks represented by every `SDD` code and emission branch in the source-derived diagnostic manifest. Numeric gaps are unused or reserved, not implied diagnostics.
- **FR-07**: Focused-ledger mode SHALL preserve canonical-ledger and archive discovery, parsing, schema, sequencing, supersession, collision-candidate, Git immutability, and concurrent-edit checks represented by every `DLG` code and emission branch in the source-derived diagnostic manifest. Numeric gaps are unused or reserved, not implied diagnostics.
- **FR-08**: YAML handling SHALL preserve the two current parsing contracts: full mode's safe-load scalar behavior and focused-ledger mode's duplicate-key rejection. It SHALL preserve relevant scalar types, date distinctions, source line marks, mapping/list/string distinctions, aliases and merge behavior, and source spans used by lifecycle normalization. For malformed YAML, the diagnostic code, severity, path, line, validation criterion, and correction SHALL remain stable, but parser-specific message prose MAY be normalized rather than reproducing PyYAML exception text byte-for-byte.
- **FR-09**: Markdown inspection SHALL preserve visibility rules for HTML comments, fenced code, indented code, headings, sections, checkboxes, evidence labels and tables, citations, open questions, brainstorm baselines, justification phrases, and failing-evidence text. Regex rewrites required by Go's engine SHALL be proven equivalent by shared fixtures rather than assumed equivalent.
- **FR-10**: Git and SCM inspection SHALL preserve repository selection, exact revision grammar, ancestry and parent checks, staged/worktree comparisons, clean-worktree checks, linked-worktree behavior, lifecycle normalization, byte comparisons, and the distinction between current and historical identity modes. External commands SHALL be invoked without a shell.
- **FR-11**: Scope handling SHALL preserve safe planning-root-relative input, direct file and directory forms, `/README.md` and `.md` alternatives, bare-name ambiguity reporting, plan ownership, transitive explicit `related` expansion, malformed-file diagnostics, and decision-diagnostic visibility.
- **FR-12**: Internal artifact references and diagnostic paths SHALL use stable forward-slash paths on every operating system. Planning-root artifacts use planning-root-relative paths. Files in a configured repository use `@repo:<repository-key>/<repository-relative-path>`; the detected owning repository uses `@repo:root/<repository-relative-path>` when no configured key applies. A focused ledger outside known roots uses `@ledger/<path-relative-to-the-primary-ledger-directory>`. Native paths remain accepted for CLI roots, ledger arguments, current working directories, and configured repositories but SHALL NOT appear in diagnostics. These focused-ledger aliases intentionally replace the Python validator's machine-specific absolute paths.
- **FR-13**: File discovery SHALL identify the same physical ledger only once on case-insensitive filesystems while preserving its actual on-disk spelling, and SHALL retain genuinely distinct case-variant ledgers on filesystems where both can coexist.
- **FR-14**: Repository and planning-root resolution SHALL follow `shared/path-resolution.md`, including upward configuration discovery, relative or absolute `planningRoot`, and target paths from `planning-config.local.json`. This is an explicit documented-conformance delta: fixtures SHALL record the Python validator's current incorrect lookup in committed `planning-config.json` and the Go commands' expected local-config result.
- **FR-15**: The migration SHALL update every plugin command, shared convention, template, Make target, dependency declaration, and maintenance document that invokes Python, names PyYAML, documents the old entry points, or constrains validator behavior.
- **FR-16**: A `related` entry SHALL be checked as a safe path from the already-resolved planning root, regardless of the invoking working directory or whether `planningRoot` is `.`, relative, or absolute. It SHALL resolve when the contained target is an existing discoverable artifact, Markdown resource, other file, or directory. Only discoverable SDD artifacts participate in transitive graph expansion, citation resolution, and `artifacts_in_scope`; a valid resource reference proves existence but is not reported as a validated artifact. The regression fixture SHALL reproduce the four false positives from `Plans/ArkPlatformV2/03-OrganizationManagement.md`: `Resources/UI-Redesign-Handoff/README.md`, `Resources/UI-Redesign-Handoff/ark-product-portal.md`, `Resources/UI-Redesign-Handoff/ark-product-manager.md`, and `Resources/design_handoff_ark_web_redesign/screenshots/product-portal`, with `planningRoot: .plans` and invocation from the repository root. The same fixture SHALL cover `.`, relative, and absolute planning-root forms, while missing targets and paths escaping the planning root still emit `SDD041`.
- **FR-17**: Task IDs SHALL use the owning phase followed by a dot, decimal task number, and an optional single lowercase ASCII letter suffix: `<phase>.<digits>[a-z]?`. IDs such as `3.1`, `3.1a`, and `3.2b` SHALL be valid and MAY coexist as independent tasks; no parent/child, replacement, sequence, or gap semantics are inferred from their shared numeric stem. Exact IDs remain append-only and unique within a plan. IDs assigned to another phase or outside this grammar SHALL continue to emit `SDD064`. Dependency, heading, citation, evidence, history, review, sorting, and lifecycle logic SHALL treat each suffixed or unsuffixed value as one exact opaque ID.
- **FR-18**: The plan-writing workflow, phase template, schema examples, `agents/plan-reviewer.md`, and diagnostics SHALL document the FR-17 grammar before plans can be approved. `/plan` SHALL run scoped validation after authoring or revising phase task IDs and SHALL stop before review on invalid or duplicate IDs; suffix selection itself remains an authoring choice rather than a deterministic allocation algorithm.
- **FR-19**: Every existing Python test method SHALL have exactly one traceable named Go test case in the checked-in parity manifest `current-test-coverage.csv`. Before Go validation logic begins, the six current Python test files SHALL also be frozen as non-executable oracle-source fixtures with SHA-256 checksums, preserving every literal and generated subtest input after executable Python tests are removed. The mapping SHALL record the Python test's fully qualified name, helper or CLI path, direct or incidental coverage, expected diagnostic or helper result, and unchanged or approved-delta status. A mechanical AST-based inventory check SHALL enumerate methods and subtests from the frozen sources and fail on a missing current test, changed checksum, duplicate mapping, missing Go case, or Go case that omits a frozen subtest ID/input.
- **FR-20**: The parity suite SHALL run the Python oracle and Go candidate over shared black-box fixtures and compare exit code, stdout, stderr, parsed JSON, diagnostic order, line, path, multiplicity, severity, message, correction, and implicated paths. The comparison SHALL permit differences only when a fixture explicitly cites FR-02, FR-12, FR-14, FR-16, FR-17, or FR-08's malformed-YAML prose exception and freezes both old and new expected results.
- **FR-21**: Before candidate validation logic is implemented, a source-derived manifest SHALL assign every diagnostic callsite a branch ID formed from source file, enclosing function, diagnostic code, and callsite ordinal; distinct conditional message templates at one callsite SHALL receive named variant IDs. Any intentionally unreachable or duplicate branch requires a checked-in exemption with rationale. Shared fixtures SHALL run against Python and freeze at least one valid, one failing, and every input boundary named by that branch's predicates. The migration test gate SHALL fail on an unlisted source emission, stale exemption, duplicate branch row, or branch without frozen Python output. The complete manifest, frozen corpus, source scan, and tests SHALL exist and pass in the first parent of the first native-SCM revision that adds Go validation logic; a history-aware migration gate SHALL verify that revision boundary. The Go implementation SHALL then satisfy that independently frozen corpus; only rows explicitly citing FR-02, FR-12, FR-14, FR-16, FR-17, or FR-08 malformed-YAML prose may carry different expected Go output.
- **FR-22**: The suite SHALL include dedicated compatibility corpora for YAML, Markdown visibility, CLI output, path safety and ambiguity, Git history/worktrees, completion evidence, review artifacts, decision archives, case-sensitive/case-insensitive discovery, spaces, and non-ASCII filesystem names.
- **FR-23**: Once FR-19 through FR-22 pass through `make test`, the Python scripts, Python-only unit imports, PyYAML dependency, and virtualenv-based test bootstrap SHALL be removed. Frozen oracle outputs, equivalent Go tests, and black-box fixtures SHALL remain as the authoritative regression suite.
- **FR-24**: Release packaging SHALL bundle both commands at `bin/<goos>-<goarch>/sdd-validate[.exe]` and `bin/<goos>-<goarch>/sdd-decision[.exe]` for every NFR-03 target. Lifecycle skills SHALL select the directory matching the host OS and architecture, use `.exe` on Windows, and report an operational unsupported-platform error rather than building or downloading at runtime. Release users SHALL require neither Go nor network access.

### Non-Functional Requirements
- **NFR-01**: `go test ./...` SHALL run the complete platform-independent suite from a clean checkout without Python, PyYAML, network access, or generated files outside the test temporary directory.
- **NFR-02**: `make test` SHALL be the authoritative repository test entry point. It SHALL run `go test ./...`, run the Python-versus-Go differential suite while the oracle exists, cross-compile both commands for every NFR-03 target, detect the host GOOS/GOARCH, and execute the matching host-native `sdd-validate` and `sdd-decision` against their smoke and integration fixtures. It SHALL never attempt to execute a foreign-platform binary and SHALL fail clearly when the host has no supported bundled target.
- **NFR-03**: `make test` and release build automation SHALL cross-compile both commands for `windows/amd64`, `darwin/amd64`, `darwin/arm64`, `linux/amd64`, and `linux/arm64`. Successful compilation is the required check for non-host targets; only the matching host target is executed locally.
- **NFR-04**: The executable SHALL have no runtime dependency on Python, a shell, PyYAML, or a package manager. Git or Perforce MAY be invoked only for checks that already depend on the detected SCM.
- **NFR-05**: Validation SHALL remain read-only: it SHALL not modify artifacts, repository state, indexes, worktrees, configuration, or network state.
- **NFR-06**: All path and process handling SHALL support spaces and non-ASCII characters and SHALL not construct shell command strings.

## User Stories
- As a plugin user on Windows, macOS, or Linux, I want validation to run from bundled native commands so that I do not need to install Python packages.
- As a plugin maintainer, I want every old test and diagnostic branch mapped to the Go suite so that a rewrite cannot silently drop validation behavior.
- As an artifact author, I want `related` paths interpreted from the documented planning root so that existing artifacts are not falsely reported as missing.
- As a plan author, I want to split a task into `3.1a` and `3.1b` and receive ID feedback during planning so that separately verifiable work retains a stable relationship to its original task.
- As an automation consumer, I want stable output and exit contracts so that changing implementation language does not break repository tests or lifecycle commands.

## Acceptance Criteria
- [ ] **AC-01**: `sdd-validate --format json` produces the same full-mode exit code and byte-equivalent stdout/stderr as the Python oracle for every unchanged shared fixture; malformed-YAML fixtures may differ only in normalized parser prose while retaining FR-08's stable diagnostic fields and criteria.
- [ ] **AC-02**: `sdd-decision` produces the same exit code and byte-equivalent stdout/stderr as `sdd_decision_validate.py` for every unchanged shared fixture, with only FR-02 command naming, FR-12 path aliases, and FR-08 malformed-YAML prose allowed to differ.
- [ ] **AC-03**: A checked-in parity manifest accounts for all 74 current Python test methods exactly once, references a passing Go test/case for each, and has no duplicate or unmapped entries; the frozen-source inventory also proves that every current subtest ID/input appears in the corresponding Go case.
- [ ] **AC-04**: A machine-checked diagnostic matrix accounts for every emitted `SDD` and `DLG` code and every enumerated shared-code message branch; each row points to passing positive, negative, and boundary tests as applicable.
- [ ] **AC-05**: A scrubbed fixture matching FR-16's four `Resources/...` entries validates without `SDD041` from the repository root and from outside the planning root when `planningRoot` is `.`, relative, or absolute; these resources do not appear in `artifacts_in_scope`, and a missing or escaping target emits `SDD041` at the citing line.
- [ ] **AC-06**: Phase fixtures containing `3.1`, `3.1a`, and `3.2b` pass all ID-dependent checks, while `2.1a` in phase 3, `3.a`, `3.1A`, and `3.1aa` emit `SDD064` with updated author-facing correction text.
- [ ] **AC-07**: Validator fixtures allow base and suffixed IDs to coexist, preserve exact suffixed dependencies and citations, and reject invalid grammar or exact collisions. Static workflow checks prove that `/plan`, `shared/frontmatter-schema.md`, `shared/templates/plan-phase.md`, and `agents/plan-reviewer.md` present the same `<phase>.<digits>[a-z]?` grammar and that `/plan` runs scoped validation before reviewer dispatch.
- [ ] **AC-08**: `make test` passes from a clean checkout after both Python validator scripts and PyYAML have been removed, including `go test ./...`, every cross-compilation target, and host-native execution of both commands.
- [ ] **AC-09**: A `make test` run proves that both commands compile for all five NFR-03 targets, executes only the binaries matching the local GOOS/GOARCH, and verifies valid full-artifact and focused-ledger JSON from host-native smoke fixtures.
- [ ] **AC-10**: Golden CLI tests prove exit `0`, `1`, and `2`; text and JSON modes; candidate-only validity; normal and operational payloads; stdout/stderr routing; deterministic diagnostic sorting; trailing-newline behavior; and canonical new command names for unknown options, missing values, invalid choices, extra positionals, repeated options, `--option=value`, `--`, and values beginning with `-`.
- [ ] **AC-11**: YAML compatibility tests prove duplicate-key differences between modes, dates, YAML 1.1 scalar cases used by PyYAML, nulls, numbers, flow collections, multiline scalars, aliases, merges, invalid UTF-8, CRLF, BOM, and parser line marks.
- [ ] **AC-12**: Git compatibility tests prove no-repository and no-HEAD behavior, normal repositories, linked worktrees, staged/worktree divergence, merge commits, detached or non-descendant history, lifecycle normalization, paths with spaces/non-ASCII, and historical identity mode.
- [ ] **AC-13**: Validation leaves artifact bytes, SCM status, index state, configuration, and working tree unchanged in read-only guard tests.
- [ ] **AC-14**: A source scan and machine-checked branch manifest account for every Python diagnostic callsite/variant, predicate boundary, and approved exemption; every branch row has frozen oracle output, and SCM history proves this gate passed in the parent revision before the first Go validation-logic revision.
- [ ] **AC-15**: Repository search finds no runtime invocation of either Python validator, no PyYAML dependency, and no documentation that tells users to invoke the removed scripts.
- [ ] **AC-16**: Release contents include both command names at every FR-24 location; lookup tests select the path for the simulated host OS/architecture, and local integration runs execute the actual host path while unsupported combinations fail clearly without network access.
- [ ] **AC-17**: Focused-ledger fixtures prove planning-relative, configured `@repo:<key>`, fallback `@repo:root`, and standalone `@ledger` diagnostic paths on POSIX and Windows inputs without emitting machine-specific absolute paths.

## Constraints
- The Python implementation is the compatibility oracle only until the differential suite is complete; it is not shipped as a fallback.
- FR-02 command naming, FR-12 focused-ledger path aliases, FR-14, FR-16, FR-17, and FR-08's malformed-YAML prose normalization are intentional compatibility exceptions and must not be normalized away as parity failures.
- Diagnostic identifiers remain stable. The two fixes update behavior under existing `SDD041` and `SDD064` codes rather than minting replacement codes.
- Planning artifacts and diagnostics must not record machine-specific absolute user paths.
- Tests that require filesystem case sensitivity, Git, or a target operating system must declare and verify that prerequisite rather than silently passing.

## Dependencies
- A supported Go toolchain capable of building the target matrix.
- A Go YAML implementation or small compatibility layer proven against the PyYAML characterization corpus; library selection is a design decision, not an assumed compatibility claim.
- Git for Git-specific integration tests and runtime identity checks.
- GNU Make or a compatible `make` implementation for the authoritative repository test entry point.

## Current Test And Coverage Baseline

The baseline was executed on 2026-08-02 with `make test`: 74 tests ran, 73
passed, and one case-sensitive-filesystem case skipped on the case-insensitive
macOS volume. The suite currently runs these files through Python `unittest`:

| Current test file | Tests | Current coverage | Required Go mapping |
|---|---:|---|---|
| `tests/test_citations.py` | 6 | Task-frontmatter decision and requirement citations, YAML line reporting, and the completed-task verification/justification scan boundary; directly exercises `SDD120` and `SDD122`. | Six named cases plus new direct `SDD121` and broader citation characterization under FR-21. |
| `tests/test_decision_ledger_discovery.py` | 4 | Physical-file deduplication, real on-disk case preservation, and distinct case variants; emits no `DLG` diagnostic directly. | Four named filesystem cases plus direct focused-ledger diagnostic coverage. |
| `tests/test_failing_evidence.py` | 15 | Failure-token recognition and table-row suppression helpers; does not drive full evidence validation. | Fifteen named helper cases plus end-to-end `SDD072`/`SDD073` fixtures. |
| `tests/test_final_aligned_review.py` | 4 | Final-review scalar parsing and the double-scalar regression; does not drive phase-gate diagnostics. | Four named parser cases plus end-to-end `SDD166`, `SDD168`, `SDD173`, and `SDD174` fixtures. |
| `tests/test_plan_scope_validation.py` | 19 | Template-valid smoke path, required headings/Non-Goals, task justification, and brainstorm baseline; directly exercises `SDD020`, `SDD063`, `SDD076`, `SDD077`, and `SDD078`. | Nineteen named cases, including equivalent real-template black-box coverage. |
| `tests/test_task_justification.py` | 26 | Justification citation, placeholder, regex anchoring, title-echo, stemming, and Non-Goals helper behavior; directly exercises `SDD076` and `SDD077` in isolation. | Twenty-six named table cases preserving every accepted/rejected phrase boundary. |
| **Total** | **74** | Direct positive diagnostic coverage is limited to `SDD020`, `SDD063`, `SDD076`, `SDD077`, `SDD078`, `SDD120`, and `SDD122`; helper coverage touches failure evidence and final-review parsing. No test directly asserts a `DLG` diagnostic. | Exactly 74 manifest rows plus the characterization expansion required by FR-21 and FR-22. |

The normative per-method inventory is `current-test-coverage.csv` beside this
spec. The existing baseline is necessary but insufficient for migration safety. Most
configuration, parsing, schema, hierarchy, graph, completion, review, Git,
scope, and ledger branches have no direct test. The diagnostic matrix required
by FR-21 is therefore part of parity, not optional new feature coverage.

## Open Questions
- None. Both commands, bundled delivery, base/suffix coexistence, and the concrete SDD041 regression fixture are resolved.
