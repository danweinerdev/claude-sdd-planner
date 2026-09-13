---
title: "Hermetic Git"
type: phase
plan: "TestSuiteReliability"
phase: 1
status: complete
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 2 node(s) under phase label Hermetic Git"
tasks: []
---

# Phase 1: Hermetic Git

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

<!-- FROZEN VIEW — every node in this phase is closed: GREEN and covered by a passing frozen full review gate. This projection is history; a render that would change it is refused. -->

## Overview

Rendered view of 2 node(s) from the plan graph (schema v1, seq 494).
Observations shown are raw records; completion-grade closure derives from
full review gates and is never stored or hand-edited here.

## Nodes

### test-git-policy

- Contract: internal/testenv installs a hermetic Git policy for a test process: a hostile temporary system/global config and config-injection variables cannot enable a fsmonitor or hook sentinel, fixture identity/time/branch settings are explicit, the null config path is platform-correct, and an intentional explicit fixture setting still takes effect.
- Justifies: `Specs/TestSuiteReliability:FR-01`, `Specs/TestSuiteReliability:FR-02`, `Specs/TestSuiteReliability:FR-03`, `Designs/TestSuiteReliability:DD-1`
- Depends on: (nothing)
- Gate: tests — `TestHermeticGitPolicy` in internal/testenv/testenv_test.go; `TestIntentionalGitConfig` in internal/testenv/testenv_test.go; `TestNullConfigPathIsPlatformCorrect` in internal/testenv/testenv_test.go
- Hazards: none (explicit claim)
- Artifacts: internal/testenv/testenv.go, internal/testenv/testenv_test.go
- Estimate: 2
- Observation: **pass** at seq 484 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

### child-validation-hermetic

- Contract: Under a hostile ambient Git configuration, in-process validation in internal/rules and tools/regression and a child sdd validate process leave the sentinel untouched and use only the fixture repository, because each package's TestMain installs the policy before parallel tests and child processes receive an explicit environment; fixture setup commands run by the rules harness inherit the policy's global configuration instead of overriding it, so a prepared fixture reads core.fsmonitor=false and commit.gpgsign=false; every test package in the module that can spawn a process installs the same TestMain, and an inventory test refuses a package without it — the inventory resolves packages by import path only: a test package is spawning when any of its test sources import os/exec or the module's procexec package, or when the package or its test sources transitively import (within the module) a package whose production sources import either, so identifier names, string literals and comments play no part and wrappers, import aliases, concatenated binary names, variable-named spawners and helper packages cannot escape; a synthetic-module proof covers each of those shapes.
- Justifies: `Specs/TestSuiteReliability:FR-01`, `Specs/TestSuiteReliability:FR-03`, `Specs/TestSuiteReliability:AC-01`, `Designs/TestSuiteReliability:DD-1`
- Depends on: `test-git-policy`
- Gate: tests — `TestInProcessValidationHermetic` in internal/rules/hermetic_test.go; `TestChildValidationHermetic` in cmd/sdd/hermetic_test.go (satisfies user-entrypoint); `TestFixtureSetupInheritsPolicy` in internal/rules/hermetic_test.go; `TestCorpusSetupInheritsPolicy` in tools/regression/corpus_test.go; `TestGitSpawningTestPackagesInstallPolicy` in internal/testenv/adoption_test.go; `TestGitSpawnInventoryFollowsIndirection` in internal/testenv/adoption_test.go; `TestSpawnInventoryIsImportBased` in internal/testenv/adoption_test.go
- Hazards: user-entrypoint
- Artifacts: internal/rules/main_env_test.go, internal/rules/hermetic_test.go, cmd/sdd/main_env_test.go, cmd/sdd/hermetic_test.go, tools/regression/main_env_test.go, internal/vcs/main_env_test.go, internal/testenv/testenv.go, internal/rules/rules_test.go, internal/rules/harness_test.go, tools/regression/corpus.go, tools/regression/corpus_test.go, internal/graph/compile/main_env_test.go, internal/graph/convert/main_env_test.go, internal/graph/intent/main_env_test.go, internal/graph/model/main_env_test.go, internal/graph/review/main_env_test.go, internal/graph/ops/main_env_test.go, internal/graph/provider/main_env_test.go, internal/graph/sync/main_env_test.go, internal/hook/main_env_test.go, internal/provision/main_env_test.go, internal/testenv/adoption_test.go, internal/graph/proposal/main_env_test.go, tools/testgate/main_env_test.go, internal/procexec/helper_test.go
- Estimate: 2
- Observation: **pass** at seq 485 — isolation clean, provenance git 3554448493ec
- Closure: **closed** — GREEN and covered by a passing frozen full review gate

## Acceptance Criteria

- [x] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

- Verified: 2026-09-13
- Repository: /home/daniel/Development/Code/claude-sdd-planner
- VCS: git
- Revision / checkpoint: `3554448493ec74ae41630eb3f40727076c4224d9`
- Identity recheck: revision-exists probe for `3554448493ec74ae41630eb3f40727076c4224d9` at 2026-09-13T00:00:00 — matched

| Command | Working directory | Result | Observable evidence |
| --- | --- | --- | --- |
| `sdd graph status --plan TestSuiteReliability` | . | PASS (exit 0) | phase 1: 2/2 node(s) closed (GREEN, covered by a passing frozen full review gate) |

### Completed task identities
