package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
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

// TestNext_PlanFlag_ResolvesLikeGraphStatus: `--plan <Name>` resolves the
// plan directory against the planning root the same way `graph status
// --plan` does, so `sdd next --plan <Name>` works from the repository root
// regardless of the caller's CWD-relative path (P-04).
func TestNext_PlanFlag_ResolvesLikeGraphStatus(t *testing.T) {
	dispositionFixture(t)

	out, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"next", "--plan", "Demo", "--json"})
		return root.Execute()
	})
	if err != nil {
		t.Fatalf("next --plan Demo: %v\n%s", err, out)
	}
	var got struct {
		Plan string `json:"plan"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil {
		t.Fatalf("unmarshal %q: %v", out, jsonErr)
	}
	if got.Plan != "Demo" {
		t.Fatalf("plan = %q, want %q", got.Plan, "Demo")
	}
}

// TestNext_PositionalPathNotFound: a positional path that does not resolve
// must say so and suggest --plan, not the misleading "no committed graph"
// message that reads as though a graph command failed internally (P-04).
func TestNext_PositionalPathNotFound(t *testing.T) {
	chdirTemp(t)

	_, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"next", "Plans/DoesNotExist"})
		return root.Execute()
	})
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "--plan") {
		t.Fatalf("error must say the path was not found and suggest --plan, got: %v", err)
	}
	if strings.Contains(err.Error(), "no committed graph") {
		t.Fatalf("error must not claim a graph problem for a path that doesn't exist: %v", err)
	}
}

// TestNext_PlanFlagAndPositional_MutuallyExclusive: --plan and a positional
// path are two ways to say the same thing; combining them is ambiguous.
func TestNext_PlanFlagAndPositional_MutuallyExclusive(t *testing.T) {
	dispositionFixture(t)

	_, err := captureStdout(t, func() error {
		root := newRootCmd()
		root.SetArgs([]string{"next", "Plans/Demo", "--plan", "Demo"})
		return root.Execute()
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected a mutually-exclusive error, got: %v", err)
	}
}

// claimableFixture is dispositionFixture with its GREEN nodes replaced by
// one claimable node, so --claim (and therefore --show) has a live claim to
// exercise without a git repository.
func claimableFixture(t *testing.T) string {
	t.Helper()
	root := dispositionFixture(t)
	graphPath := gstore.PathFor(filepath.Join(root, "Plans", "Demo"))
	if _, err := gstore.Update(graphPath, func(g *model.Graph) error {
		g.Nodes = []model.Node{{
			ID: "candidate", Contract: "does the thing", Justifies: []string{"D-0001"},
			Gate:    model.Gate{Type: model.GateCommand, Command: "true"},
			Hazards: model.Hazards{}, Estimate: 1,
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestNext_Show_ReprintsHolderPayloadWithoutClaiming: --show must not claim
// a node; it only reprints the payload of what the holder already has
// (P-06b) — re-running --claim to re-read a payload otherwise claims a
// second node.
func TestNext_Show_ReprintsHolderPayloadWithoutClaiming(t *testing.T) {
	claimableFixture(t)

	if handled, err := graphNext("Plans/Demo", true, "tester", true); !handled || err != nil {
		t.Fatalf("claim: handled=%v err=%v", handled, err)
	}

	graphPath := gstore.PathFor(filepath.Join("Plans", "Demo"))
	before, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		handled, err := graphNextShow("Plans/Demo", "tester", true)
		if !handled {
			t.Fatal("graphNextShow must handle a plan with a committed graph")
		}
		return err
	})
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}

	after, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("--show must not mutate the graph (no new claim)")
	}

	var got struct {
		Plan  string `json:"plan"`
		By    string `json:"by"`
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"nodes"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil {
		t.Fatalf("unmarshal %q: %v", out, jsonErr)
	}
	if got.Plan != "Demo" || got.By != "tester" || len(got.Nodes) != 1 || got.Nodes[0].Node.ID != "candidate" {
		t.Fatalf("show payload = %+v", got)
	}
}

// TestNext_Show_NoClaim_ReportsAndExitsZero: a holder with no claim gets a
// plain statement, not an error — --show is a read, and "nothing held" is a
// legitimate answer, not a failure (P-06b).
func TestNext_Show_NoClaim_ReportsAndExitsZero(t *testing.T) {
	claimableFixture(t)

	out, err := captureStdout(t, func() error {
		handled, err := graphNextShow("Plans/Demo", "nobody", true)
		if !handled {
			t.Fatal("graphNextShow must handle a plan with a committed graph")
		}
		return err
	})
	if err != nil {
		t.Fatalf("show with no claim must exit 0, got: %v\n%s", err, out)
	}
	var got struct {
		Plan  string `json:"plan"`
		By    string `json:"by"`
		Nodes []any  `json:"nodes"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil {
		t.Fatalf("unmarshal %q: %v", out, jsonErr)
	}
	if got.Plan != "Demo" || got.By != "nobody" || len(got.Nodes) != 0 {
		t.Fatalf("show payload = %+v", got)
	}
}
