package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/states"
)

func TestBriefUsesCurrentReviewScope(t *testing.T) {
	g := &model.Graph{Nodes: []model.Node{
		{ID: "work", Contract: "review this changed work", Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean}},
		{ID: "inner", Gate: model.Gate{Type: model.GateReview}, Deps: []string{"work"}, Verification: &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean}},
		{ID: "outer", Gate: model.Gate{Type: model.GateReview}, Deps: []string{"inner"}},
	}}
	current := states.Derive(states.Inputs{Graph: g})
	// Analytics has detected artifact/intent/input drift unavailable to a
	// graph-only scope calculation. The brief must not subtract this region.
	current["inner"] = states.NodeState{ID: "inner", State: states.Stale}
	var out bytes.Buffer
	printBrief(&out, g, g.NodeByID("outer"), current)
	if !strings.Contains(out.String(), "review this changed work") {
		t.Fatalf("brief hid work below a currently stale inner review:\n%s", &out)
	}
}

func TestGraphAmendExposesReportDigestFence(t *testing.T) {
	cmd := graphAmendCmd()
	if cmd.Flags().Lookup("expect-report-digest") == nil {
		t.Fatal("graph amend must expose --expect-report-digest for the previewed review artifact")
	}
}
