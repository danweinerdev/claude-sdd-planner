package convert

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gcompile "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/compile"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

const v1Plan = `---
title: "Legacy Plan"
type: plan
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/Sample, Designs/Sample]
phases:
  - id: 1
    title: "Foundations"
    status: in-progress
    doc: "01-Foundations.md"
  - id: 2
    title: "Assembly"
    status: planned
    doc: "02-Assembly.md"
    depends_on: [1]
---

# Legacy Plan
`

const v1PhaseOne = "---\n" +
	"title: \"Foundations\"\n" +
	"type: phase\nplan: \"LegacyPlan\"\nphase: 1\nstatus: in-progress\n" +
	"created: 2026-08-01\nupdated: 2026-08-01\n" +
	"deliverable: \"Foundations\"\n" +
	"tasks:\n" +
	"  - id: \"1.1\"\n    title: \"Build the widget store\"\n    status: complete\n" +
	"    verification: \"go test ./widget/... passes\"\n" +
	"    justifies: \"FR-01 (storage), DD-1; prevents data loss\"\n" +
	"  - id: \"1.2\"\n    title: \"Wire the widget API\"\n    status: planned\n" +
	"    verification: \"curl returns 200\"\n" +
	"    justifies: \"AC-01 and D-0001\"\n" +
	"    depends_on: [\"1.1\"]\n" +
	"---\n\n# Phase 1: Foundations\n\n" +
	"## 1.1: Build the widget store\n\n### Subtasks\n- [x] build it\n\n### Notes\nDone.\n\n" +
	"### Completion Evidence\n\n- Verified: 2026-08-10\n- Repository: `.`\n- VCS: `git`\n" +
	"- Revision / checkpoint: `aaaabbbbccccddddeeeeffff0000111122223333`\n\n" +
	"## 1.2: Wire the widget API\n\n### Subtasks\n- [ ] wire it\n\n### Notes\nPending.\n\n" +
	"### Completion Evidence\n\nPending — not complete.\n"

const v1PhaseTwo = "---\n" +
	"title: \"Assembly\"\n" +
	"type: phase\nplan: \"LegacyPlan\"\nphase: 2\nstatus: planned\n" +
	"created: 2026-08-01\nupdated: 2026-08-01\n" +
	"deliverable: \"Assembly\"\n" +
	"tasks:\n" +
	"  - id: \"2.1\"\n    title: \"Assemble the widgets\"\n    status: planned\n" +
	"    verification: \"assembled\"\n" +
	"    justifies: \"AC-02\"\n" +
	"    depends_on: [\"1.2\"]\n" +
	"---\n\n# Phase 2: Assembly\n\n" +
	"## 2.1: Assemble the widgets\n\n### Subtasks\n- [ ] assemble\n\n### Notes\nPending.\n\n" +
	"### Completion Evidence\n\nPending — not complete.\n"

const v1Spec = `---
title: "Sample Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Sample Spec

## Requirements

- **FR-01**: Widgets SHALL persist.

## Acceptance Criteria

- [ ] **AC-01**: The API answers.
- [ ] **AC-02**: Widgets assemble.
`

const v1Design = `---
title: "Sample Design"
type: design
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [design]
related: []
---

# Sample Design

## Design Decisions

- **DD-1**: Widgets are stored flat.
`

const v1Decisions = `---
title: "Decisions"
type: decision-log
status: active
created: 2026-08-01
updated: 2026-08-01
tags: []
related: []
decisions:
  - id: D-0001
    kind: decision
    status: accepted
    date: 2026-08-01
    decided_by: user
    statement: "An accepted truth."
    scope: []
---

# Decisions
`

func v1Root(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("planning-config.json", `{"planningRoot": "."}`)
	write("Plans/LegacyPlan/README.md", v1Plan)
	write("Plans/LegacyPlan/01-Foundations.md", v1PhaseOne)
	write("Plans/LegacyPlan/02-Assembly.md", v1PhaseTwo)
	write("Specs/Sample/README.md", v1Spec)
	write("Designs/Sample/README.md", v1Design)
	write("Decisions/decisions.md", v1Decisions)
	return root
}

func TestConvertMapsMechanicsAndMarksJudgments(t *testing.T) {
	root := v1Root(t)
	res, err := Run(root, root, "LegacyPlan")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Nodes != 3 || res.Phases != 2 || res.CompletedCarried != 1 {
		t.Fatalf("unexpected summary: %+v", res)
	}

	raw, err := os.ReadFile(res.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	p, err := model.DecodeProposal(raw)
	if err != nil {
		t.Fatalf("converted payload must decode strictly: %v", err)
	}
	byID := map[string]model.Node{}
	for _, n := range p.Nodes {
		byID[n.ID] = n
	}

	store := byID["task-1-1"]
	if !strings.HasPrefix(store.Contract, model.NeedsContractPrefix) {
		t.Fatalf("contracts carry the sentinel: %q", store.Contract)
	}
	if store.Gate.Type != model.GateUnspecified {
		t.Fatalf("gates land unspecified, got %q", store.Gate.Type)
	}
	if store.Hazards != nil {
		t.Fatal("hazards land untriaged (nil)")
	}
	if !reflect.DeepEqual(store.Justifies, []string{"FR-01", "DD-1"}) {
		t.Fatalf("justifies extraction: %v", store.Justifies)
	}
	if !strings.Contains(store.History, "complete as v1 task 1.1") ||
		!strings.Contains(store.History, "verified 2026-08-10") ||
		!strings.Contains(store.History, "aaaabbbbccccddddeeeeffff0000111122223333") {
		t.Fatalf("completed task must carry its provenance: %q", store.History)
	}
	if store.Verification != nil {
		t.Fatal("no retroactive observations, ever")
	}

	api := byID["task-1-2"]
	if !reflect.DeepEqual(api.Deps, []string{"task-1-1"}) {
		t.Fatalf("task depends_on must map: %v", api.Deps)
	}
	if api.History != "" {
		t.Fatal("non-complete tasks carry no history")
	}
	if !reflect.DeepEqual(api.Justifies, []string{"AC-01", "D-0001"}) {
		t.Fatalf("justifies extraction: %v", api.Justifies)
	}

	// Phase-level depends_on densifies: every phase-2 node depends on every
	// phase-1 node, merged with its own task deps, sorted.
	assemble := byID["task-2-1"]
	if !reflect.DeepEqual(assemble.Deps, []string{"task-1-1", "task-1-2"}) {
		t.Fatalf("phase order must become deps: %v", assemble.Deps)
	}
	if assemble.Phase != "02-Assembly" {
		t.Fatalf("phase label is the doc stem: %q", assemble.Phase)
	}
}

// TestConvertedGraphRefusesToCompile is DD-15's half of the bargain: the
// conversion stages cleanly, and compile lists every unresolved sentinel.
func TestConvertedGraphRefusesToCompile(t *testing.T) {
	root := v1Root(t)
	if _, err := Run(root, root, "LegacyPlan"); err != nil {
		t.Fatalf("convert: %v", err)
	}
	_, findings, err := gcompile.Run(root, root, "LegacyPlan")
	if err != nil {
		t.Fatalf("compile must refuse with findings, not fail: %v", err)
	}
	joined := ""
	for _, f := range findings {
		joined += f.String() + "\n"
	}
	for _, node := range []string{"task-1-1", "task-1-2", "task-2-1"} {
		for _, sentinel := range []string{
			"gate is unspecified (conversion sentinel)",
			"contract is the conversion sentinel",
			"hazards are untriaged",
		} {
			if !strings.Contains(joined, node+": "+sentinel) {
				t.Errorf("missing %s finding for %s in:\n%s", sentinel, node, joined)
			}
		}
	}
	// And the structural obligations still apply on top: no full review
	// gate covers the converted nodes yet.
	if !strings.Contains(joined, "covered by no full review gate") {
		t.Errorf("coverage invariant must still hold for converted graphs:\n%s", joined)
	}
}

func TestConvertRefusesAnEmptyPlan(t *testing.T) {
	root := v1Root(t)
	if err := os.WriteFile(filepath.Join(root, "Plans", "LegacyPlan", "README.md"),
		[]byte(strings.Replace(v1Plan, "phases:", "phases: []\nunused:", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(root, root, "LegacyPlan")
	if err == nil || !strings.Contains(err.Error(), "no phase tasks") {
		t.Fatalf("an empty plan must refuse helpfully: %v", err)
	}
}
