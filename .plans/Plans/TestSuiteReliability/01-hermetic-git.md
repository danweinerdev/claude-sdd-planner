---
title: "01-hermetic-git"
type: phase
plan: "TestSuiteReliability"
phase: 1
status: planned
created: 2026-09-13
updated: 2026-09-13
deliverable: "Graph view: 2 node(s) under phase label 01-hermetic-git"
tasks: []
---

# Phase 1: 01-hermetic-git

<!-- GENERATED VIEW — source of truth: TestSuiteReliability-Graph.json. Regenerate with `sdd compile --plan TestSuiteReliability`. Edits here are overwritten. -->

## Overview

Rendered view of 2 node(s) from the plan graph (schema v1, seq 0).
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
- Observation: none yet
- Closure: open — state READY

### child-validation-hermetic

- Contract: Under a hostile ambient Git configuration, in-process validation in internal/rules and tools/regression and a child sdd validate process leave the sentinel untouched and use only the fixture repository, because each package's TestMain installs the policy before parallel tests and child processes receive an explicit environment.
- Justifies: `Specs/TestSuiteReliability:FR-01`, `Specs/TestSuiteReliability:FR-03`, `Specs/TestSuiteReliability:AC-01`, `Designs/TestSuiteReliability:DD-1`
- Depends on: `test-git-policy`
- Gate: tests — `TestInProcessValidationHermetic` in internal/rules/hermetic_test.go; `TestChildValidationHermetic` in cmd/sdd/hermetic_test.go (satisfies user-entrypoint)
- Hazards: user-entrypoint
- Artifacts: internal/rules/main_test.go, internal/rules/hermetic_test.go, cmd/sdd/main_test.go, cmd/sdd/hermetic_test.go, tools/regression/main_test.go, internal/vcs/main_test.go
- Estimate: 2
- Observation: none yet
- Closure: open — state BLOCKED

## Acceptance Criteria

- [ ] Every node in this phase is truly closed: a passing observation, and
      coverage by a passing frozen full review gate (derived from the graph;
      never checked off by hand).

## Phase Completion Evidence

Pending — not complete.
