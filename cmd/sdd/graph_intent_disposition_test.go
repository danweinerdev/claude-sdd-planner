package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	gstore "github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/store"
)

// dispositionDecisionStatement is the plan decision the fixture's "decision"
// node cites; its id is content-addressed from this text (decisions.IDFor).
const dispositionDecisionStatement = "An accepted truth."

// dispositionFixture builds a root where every citation disposition is known:
// Sample defines AC-01/AC-02/FR-01, Other also defines AC-01 (so bare AC-01
// is ambiguous), and the plan's decisions file records one entry. The
// committed graph carries PASS nodes covering each disposition a derive pass
// must distinguish:
//
//	deleted          AC-99   (resolves nowhere — a requirement deleted since)
//	ambiguous        AC-01   (bare, defined by both Sample and Other)
//	decision         pd-…    (a recorded plan decision — legitimately exempt)
//	unknown-decision pd-…    (pd-shaped but not recorded)
//	unanchored       AC-02   (resolvable but never fingerprinted — split bug)
func dispositionFixture(t *testing.T) string {
	t.Helper()
	root := chdirTemp(t)
	writeArtifact(t, root, "Specs/Sample", "README.md", `---
title: "Sample Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Sample Spec

## Functional Requirements

- **FR-01**: The loader SHALL accept every documented key.

## Acceptance Criteria

- [ ] **AC-01**: A valid config loads with zero findings.
- [ ] **AC-02**: An unknown key names itself in the refusal.
`)
	writeArtifact(t, root, "Specs/Other", "README.md", `---
title: "Other Spec"
type: spec
status: approved
created: 2026-08-01
updated: 2026-08-01
tags: [spec]
related: []
---

# Other Spec

## Acceptance Criteria

- [ ] **AC-01**: The other spec's criterion one.
`)
	writeArtifact(t, root, "Plans/Demo", "README.md", `---
title: "Demo"
type: plan
status: draft
created: 2026-08-01
updated: 2026-08-01
tags: []
related: [Specs/Sample, Specs/Other]
phases: []
---

# Demo
`)
	planDir := filepath.Join(root, "Plans", "Demo")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decisionID := decisions.IDFor(dispositionDecisionStatement)
	entries := []decisions.Entry{{ID: decisionID, Date: "2026-08-01", Statement: dispositionDecisionStatement}}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisions.PathFor(planDir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	unknownDecisionID := decisions.IDFor("An unrecorded truth.")
	passNode := func(id string, justifies []string) model.Node {
		return model.Node{ID: id, Contract: "does " + id, Justifies: justifies,
			Gate: model.Gate{Type: model.GateTests,
				Tests: []model.Test{{ID: "test_" + id, File: "t.ext"}}},
			Hazards:      model.Hazards{},
			Estimate:     1,
			Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean},
		}
	}
	// The "decision" node is anchored with the recorded entry's own
	// fingerprint — a plan decision is citable and fingerprintable like any
	// requirement (Designs/PlanDecisions), so it stays GREEN through the same
	// hash-matching path as an AC/FR citation, not through an exemption.
	decisionNode := passNode("decision", []string{decisionID})
	g := &model.Graph{Version: model.SchemaVersion, Nodes: []model.Node{
		passNode("deleted", []string{"AC-99"}),
		passNode("ambiguous", []string{"AC-01"}),
		decisionNode,
		passNode("unknown-decision", []string{unknownDecisionID}),
		passNode("unanchored", []string{"AC-02"}),
	}}
	if err := gstore.Save(gstore.PathFor(planDir), g); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestGraphStatusReceivesIntentDispositions drives the REAL analytics path
// (`graph status` -> loadAnalytics -> compile.IntentSnapshot -> states.Derive)
// and asserts each citation disposition derives the right state. This is the
// end-to-end counterpart to the pure states unit tests: it proves the snapshot
// (hashes + decision exemptions) reaches Derive through the production wiring,
// not just a hand-built Inputs.
func TestGraphStatusIgnoresIntentContent(t *testing.T) {
	dispositionFixture(t)

	var status struct {
		States map[string]int `json:"states"`
		Nodes  []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "status", "--plan", "Demo", "--json")), &status); err != nil {
		t.Fatal(err)
	}
	if status.States["GREEN"] != 5 {
		t.Fatalf("intent content is not a state input, got %+v", status.States)
	}
	want := map[string]string{
		"deleted":          "GREEN",
		"ambiguous":        "GREEN",
		"decision":         "GREEN",
		"unknown-decision": "GREEN",
		"unanchored":       "GREEN",
	}
	got := map[string]string{}
	for _, n := range status.Nodes {
		got[n.ID] = n.State
	}
	for id, state := range want {
		if got[id] != state {
			t.Errorf("node %s = %q, want %q (all: %+v)", id, got[id], state, got)
		}
	}

	var show struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "graph", "show", "deleted", "--plan", "Demo", "--json")), &show); err != nil {
		t.Fatal(err)
	}
	if show.State != "GREEN" {
		t.Fatalf("show must remain GREEN after intent edits: %+v", show)
	}
}

// TestNextReceivesIntentDispositions drives the REAL frontier path
// (`next` -> graphNext) and asserts the accepted-decision node is not served
// (GREEN, off the frontier) while every fail-closed STALE node is. The
// disposition snapshot must reach the claim/frontier derivation, not just the
// analytics render.
func TestNextDoesNotServeNodesForIntentContent(t *testing.T) {
	dispositionFixture(t)

	var out struct {
		States   map[string]int `json:"states"`
		Frontier []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"frontier"`
	}
	if err := json.Unmarshal([]byte(runGraphVerb(t, "next", "Plans/Demo", "--json")), &out); err != nil {
		t.Fatal(err)
	}
	if out.States["GREEN"] != 5 {
		t.Fatalf("next states must all remain GREEN, got %+v", out.States)
	}
	frontier := map[string]bool{}
	for _, f := range out.Frontier {
		frontier[f.ID] = true
	}
	if len(frontier) != 0 {
		t.Errorf("GREEN nodes stay off the frontier: %+v", out.Frontier)
	}
}
