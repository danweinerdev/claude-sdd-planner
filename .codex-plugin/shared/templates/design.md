---
title: "{{TITLE}}"
type: design
status: draft
created: {{DATE}}
updated: {{DATE}}
tags: []
related: []
---

# {{TITLE}}

## Overview
Brief description of the component and its role in the system.

## Non-Goals
What this component deliberately does not do, and why. Name the responsibilities that belong to neighboring components, the generality this design declines to build, and any extension points intentionally left out. Bounding the component is part of designing it.

## Architecture

Use Mermaid diagrams to illustrate structure and flow — prefer over ASCII art or prose-only descriptions.

### Components
Describe the major components and their responsibilities.

```mermaid
graph TD
    A[Component A] --> B[Component B]
```

### Data Flow
How data moves through the system.

```mermaid
flowchart LR
    Input --> Processing --> Output
```

### Interfaces
Public APIs, events, or contracts.

## Design Decisions

### Decision 1: [Title]
**Context:**
**Options Considered:**
1.
2.
**Decision:**
**Rationale:**

## Error Handling
How errors are detected, reported, and recovered from.

## Testing Strategy
How the design will be validated.

### Structural Verification
Language-specific checks beyond tests (see `shared/language-verification.md`).

## Migration / Rollout
How to transition from current state to the new design.
