package main

import (
	"os"
	"strconv"
	"testing"
)

// chdirTemp creates a fresh planning root, chdirs into it for the duration
// of the test, and restores the previous working directory afterward — next
// resolves its planning root from os.Getwd().
func chdirTemp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeConfig(t, root)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	// Getwd() resolves symlinks (e.g. macOS /tmp -> /private/tmp); rebasing
	// on it keeps relPath's prefix comparison working for artifacts built
	// from the returned root.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func nextPlanReadme(status, phasesYAML string) string {
	return `---
title: "Thing"
type: plan
status: ` + status + `
created: 2026-07-01
updated: 2026-07-01
tags: [x]
related: []
phases:
` + phasesYAML + `
---

# Thing

## Overview
Overview.

## Non-Goals
None.

## Architecture
Architecture.

## Key Decisions
None.

## Dependencies
None.

## Plan Completion Evidence
Pending — not complete.
`
}

func nextPhaseDoc(num int, status string, tasksYAML string) string {
	return `---
title: "Phase ` + itoa(num) + `"
type: phase
status: ` + status + `
created: 2026-07-01
updated: 2026-07-01
plan: "Thing"
phase: ` + itoa(num) + `
deliverable: "something"
tasks:
` + tasksYAML + `
---

# Phase ` + itoa(num) + `

## Overview
Overview.

## Acceptance Criteria
- it works

## Phase Completion Evidence
Pending — not complete.
`
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// TestNext_RuleA_DraftPlan: a draft plan asks for review and approval.
func TestNext_RuleA_DraftPlan(t *testing.T) {
	root := chdirTemp(t)
	phases := `  - id: 1
    title: "Phase 1"
    status: planned
    doc: "01-First.md"
    depends_on: []`
	planPath := writeArtifact(t, root, "Plans/Thing", "README.md", nextPlanReadme("draft", phases))
	writeArtifact(t, root, "Plans/Thing", "01-First.md", nextPhaseDoc(1, "planned", `  - id: "1.1"
    title: "a task"
    status: planned`))

	e, err := nextForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if e.Command != "sdd validate --scope Plans/Thing" {
		t.Errorf("command = %q, want %q", e.Command, "sdd validate --scope Plans/Thing")
	}
}

// TestNext_RuleB_ApprovedNoInProgress: an approved plan with no in-progress
// phase starts the lowest-numbered planned phase.
func TestNext_RuleB_ApprovedNoInProgress(t *testing.T) {
	root := chdirTemp(t)
	phases := `  - id: 1
    title: "Phase 1"
    status: planned
    doc: "01-First.md"
    depends_on: []
  - id: 2
    title: "Phase 2"
    status: planned
    doc: "02-Second.md"
    depends_on: [1]`
	planPath := writeArtifact(t, root, "Plans/Thing", "README.md", nextPlanReadme("approved", phases))
	writeArtifact(t, root, "Plans/Thing", "01-First.md", nextPhaseDoc(1, "planned", `  - id: "1.1"
    title: "a task"
    status: planned`))
	writeArtifact(t, root, "Plans/Thing", "02-Second.md", nextPhaseDoc(2, "planned", `  - id: "2.1"
    title: "a task"
    status: planned`))

	e, err := nextForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if e.Phase != "Phase 1" {
		t.Errorf("phase = %q, want %q", e.Phase, "Phase 1")
	}
	if e.Command != "sdd apply Plans/Thing/01-First.md --dry-run" {
		t.Errorf("command = %q", e.Command)
	}
}

// TestNext_RuleC_InProgressPhasePlannedTask: an in-progress phase with a
// planned task reports that task as next.
func TestNext_RuleC_InProgressPhasePlannedTask(t *testing.T) {
	root := chdirTemp(t)
	phases := `  - id: 1
    title: "Phase 1"
    status: in-progress
    doc: "01-First.md"
    depends_on: []`
	planPath := writeArtifact(t, root, "Plans/Thing", "README.md", nextPlanReadme("active", phases))
	writeArtifact(t, root, "Plans/Thing", "01-First.md", nextPhaseDoc(1, "in-progress", `  - id: "1.1"
    title: "first task"
    status: complete
  - id: "1.2"
    title: "second task"
    status: planned`))

	e, err := nextForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if e.Task != "1.2" {
		t.Errorf("task = %q, want %q", e.Task, "1.2")
	}
	if e.Command != "sdd show Plans/Thing/01-First.md --json" {
		t.Errorf("command = %q", e.Command)
	}
}

// TestNext_RuleD_InProgressPhaseAllTasksComplete: an in-progress phase whose
// tasks are all complete needs its completion evidence and review gate.
func TestNext_RuleD_InProgressPhaseAllTasksComplete(t *testing.T) {
	root := chdirTemp(t)
	phases := `  - id: 1
    title: "Phase 1"
    status: in-progress
    doc: "01-First.md"
    depends_on: []`
	planPath := writeArtifact(t, root, "Plans/Thing", "README.md", nextPlanReadme("active", phases))
	writeArtifact(t, root, "Plans/Thing", "01-First.md", nextPhaseDoc(1, "in-progress", `  - id: "1.1"
    title: "first task"
    status: complete`))

	e, err := nextForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if e.Command != "sdd validate --scope Plans/Thing" {
		t.Errorf("command = %q", e.Command)
	}
	if e.Task != "" {
		t.Errorf("task should be empty, got %q", e.Task)
	}
}

// TestNext_RuleE_AllPhasesCompletePlanNot: every phase complete but the plan
// itself not complete means plan completion is pending.
func TestNext_RuleE_AllPhasesCompletePlanNot(t *testing.T) {
	root := chdirTemp(t)
	phases := `  - id: 1
    title: "Phase 1"
    status: complete
    doc: "01-First.md"
    depends_on: []`
	planPath := writeArtifact(t, root, "Plans/Thing", "README.md", nextPlanReadme("active", phases))
	writeArtifact(t, root, "Plans/Thing", "01-First.md", nextPhaseDoc(1, "complete", `  - id: "1.1"
    title: "a task"
    status: complete`))

	e, err := nextForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if e.Command != "sdd validate --scope Plans/Thing" {
		t.Errorf("command = %q", e.Command)
	}
	if e.Needs == "" || e.Phase != "" {
		t.Errorf("expected a plan-level needs with no phase, got needs=%q phase=%q", e.Needs, e.Phase)
	}
}

// TestNext_RuleF_PlanComplete: nothing to do.
func TestNext_RuleF_PlanComplete(t *testing.T) {
	root := chdirTemp(t)
	phases := `  - id: 1
    title: "Phase 1"
    status: complete
    doc: "01-First.md"
    depends_on: []`
	planPath := writeArtifact(t, root, "Plans/Thing", "README.md", nextPlanReadme("complete", phases))
	writeArtifact(t, root, "Plans/Thing", "01-First.md", nextPhaseDoc(1, "complete", `  - id: "1.1"
    title: "a task"
    status: complete`))

	e, err := nextForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if e.Needs != "nothing to do" {
		t.Errorf("needs = %q, want %q", e.Needs, "nothing to do")
	}
	if e.Command != "" {
		t.Errorf("command should be empty, got %q", e.Command)
	}
}
