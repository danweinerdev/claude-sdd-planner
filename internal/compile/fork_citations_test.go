package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkCompileCitationsFailClosed(t *testing.T) {
	const collection = decisionview.CollectionID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	const known = "ledger:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee:D-0001"
	capture := &decisionview.ConsumerCapture{
		Declared: true,
		View: &decisionview.ResolvedView{Resolution: decisionview.ResolutionComplete, Records: []decisionview.ResolvedDecision{{
			ID: known, CollectionID: collection, OriginalStatus: "accepted", Applicability: "binding",
			Original: map[string]any{"id": "D-0001", "status": "accepted", "statement": "known"},
		}}},
	}
	compile := func(citation string) *Result {
		body := "Uses " + citation + "."
		return Compile(load(t), payload(map[string]string{"## Overview": body}), Options{
			Today: "2026-09-10", DecisionView: capture, ArtifactPath: "Specs/Thing/README.md",
		})
	}
	if got := compile(known); !got.OK() {
		t.Fatalf("known qualified authority refused: %+v", got.Refusals)
	}
	for _, citation := range []string{
		"ledger:99999999-9999-9999-9999-999999999999:D-0001",
		"ledger:99999999-9999-9999-9999-999999999999:D-00001",
		"D-0001",
		"D-00001",
		"prefix-D-00001-suffix",
	} {
		got := compile(citation)
		if got.OK() || !hasCode(got, "SPK040") {
			t.Fatalf("unknown/ambiguous fork citation %q did not fail closed: %+v", citation, got.Refusals)
		}
	}
	capture.LegacyContexts = []decisionview.LegacyContext{{
		Root: decisionview.SourceRootRepository, Path: "Specs/Thing/README.md", Namespace: collection, LocalIDs: []string{"D-0001"},
	}}
	legacyPayload := payload(map[string]string{"## Overview": "Uses D-0001 and D-0001."})
	legacyExisting := artifact.Parse(legacyPayload)
	wrongRoot := Compile(load(t), legacyPayload, Options{
		Today: "2026-09-10", Existing: legacyExisting, DecisionView: capture, ArtifactPath: "Specs/Thing/README.md", ArtifactRoot: decisionview.SourceRootPlanning,
	})
	if wrongRoot.OK() || !hasCode(wrongRoot, "SPK040") {
		t.Fatalf("legacy context with a different source root was accepted: %+v", wrongRoot.Refusals)
	}
	rightRoot := Compile(load(t), legacyPayload, Options{
		Today: "2026-09-10", Existing: legacyExisting, DecisionView: capture, ArtifactPath: "Specs/Thing/README.md", ArtifactRoot: decisionview.SourceRootRepository,
	})
	if !rightRoot.OK() {
		t.Fatalf("matching repository-root legacy context refused: %+v", rightRoot.Refusals)
	}
	commented := compile("<!-- ledger:99999999-9999-9999-9999-999999999999:D-0001 -->")
	for _, refusal := range commented.Refusals {
		if refusal.Code == "SPK040" && strings.Contains(refusal.Message, "decision citation") {
			t.Fatalf("comment citation became authoritative prose: %+v", commented.Refusals)
		}
	}
}

func TestForkHistoricalCitationWrites(t *testing.T) {
	const id = decisionview.QualifiedID("ledger:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee:D-0001")
	for _, status := range []string{"accepted", "proposed", "rejected", "superseded"} {
		for _, kind := range []string{"research", "debrief"} {
			for _, artifactStatus := range []string{"draft", "archived", "superseded", `"archived"`, `'superseded'`} {
				t.Run(status+"/"+kind+"/"+artifactStatus, func(t *testing.T) {
					capture := &decisionview.ConsumerCapture{View: &decisionview.ResolvedView{Resolution: decisionview.ResolutionComplete, Records: []decisionview.ResolvedDecision{{ID: id, OriginalStatus: status, Applicability: "historical", Original: map[string]any{"status": status}}}}}
					source := "---\ntype: \"" + kind + "\"\nstatus: " + artifactStatus + "\n---\n\n## Context\n\nHistorical reference " + string(id) + ".\n"
					got := ValidateDecisionAuthority(source, Options{DecisionView: capture})
					want := kind == "debrief" || artifactStatus != "draft" || (status != "rejected" && status != "superseded")
					if got.OK() != want {
						t.Fatalf("writable = %v, want %v: %+v", got.OK(), want, got.Refusals)
					}
				})
			}
		}
	}
}
