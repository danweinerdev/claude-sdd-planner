package states

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func TestObservedPassRequiresMatchingConsumedMarker(t *testing.T) {
	n := model.Node{ID: "n", Contract: "c", Hazards: model.Hazards{}, Estimate: 1, Gate: model.Gate{Type: model.GateTests, Evidence: model.EvidenceObservedV1, Tests: []model.Test{{ID: "TestX", File: "x_test.go"}}, Execution: &model.ExecutionProfile{Adapter: "go-test-v1", TimeoutSeconds: 1}}, Artifacts: []string{"x_test.go"}, Verification: &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean, DependencyDigests: map[string]map[string]string{}}}
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}
	if got := Derive(Inputs{Graph: g})["n"]; got.State != Stale || !got.ObservedEvidenceStale {
		t.Fatalf("legacy-shaped observed pass = %+v", got)
	}
	digest := "sha256:" + strings.Repeat("0", 64)
	g.Nodes[0].Verification.Attempt = &model.AttemptSummary{ID: "at-x", Digest: digest, CandidateDigest: digest, Protocol: model.EvidenceObservedV1}
	g.Nodes[0].ConsumedAttempts = map[string]model.ConsumedAttempt{"at-x": {Digest: digest, Seq: 1, Result: model.ResultPass}}
	if got := Derive(Inputs{Graph: g})["n"]; got.State != Green {
		t.Fatalf("matching observed pass = %+v", got)
	}
	g.Nodes[0].ConsumedAttempts["at-x"] = model.ConsumedAttempt{Digest: "sha256:other", Seq: 1, Result: model.ResultPass}
	if got := Derive(Inputs{Graph: g})["n"]; got.State != Stale {
		t.Fatalf("mismatched consumed marker = %+v", got)
	}
}
