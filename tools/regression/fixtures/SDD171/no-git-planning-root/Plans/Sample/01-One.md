---
title: Sample Phase
type: phase
status: complete
created: 2024-01-01
updated: 2024-01-01
plan: Sample
phase: "1"
deliverable: A thing.
tasks:
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
---

## Overview

Text.

## Acceptance Criteria

- [ ] Works.

## 1.1: First

### Subtasks

- [x] Step.

### Notes

None.

### Completion Evidence

- Verified: 2024-01-01
- Repository: {{REPO}}
- VCS: git
- Revision / checkpoint: 1c8a628dfcda49f5ff17b5a2c551228ea23de68a
- Identity recheck: `git cat-file -t 1c8a628dfcda49f5ff17b5a2c551228ea23de68a`; matched at 2024-01-01T00:00:00
- Focused review: `git show 1c8a628dfcda49f5ff17b5a2c551228ea23de68a`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: 1c8a628dfcda49f5ff17b5a2c551228ea23de68a
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
| --- | --- | --- | --- |
| `go test ./...` | . | PASS (exit 0) | ok |

## Phase Completion Evidence

Pending — not complete.
