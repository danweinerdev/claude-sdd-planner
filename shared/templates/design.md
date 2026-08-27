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

<!-- Number each decision `DD-N` and never renumber it: plans, phases, and
     other designs cite these ids, and `sdd validate` resolves them against
     this section (SDD122). Cite another component's decision in qualified
     form — `ComponentName:DD-3` — so it resolves to that design, not this
     one. Declare each decision as a top-level bold bullet (`- **DD-N**: …`)
     — that is the declaration form `sdd apply` collects and allocates;
     indented sub-lines belong to the decision above them. -->

- **DD-1**: [Title].
  Context: [what forces this decision]. Options considered: (a) […]; (b) […].
  Decision: [chosen option]. Rationale: [why this over the alternatives].

## Error Handling
How errors are detected, reported, and recovered from.

## Testing Strategy
How the design will be validated.

### Structural Verification
Language-specific checks beyond tests (see `shared/language-verification.md`).

## Migration / Rollout
How to transition from current state to the new design.
