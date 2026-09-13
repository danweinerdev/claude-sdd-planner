package states

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func passWithDeps(seq int, deps map[string]map[string]string) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean, DependencyDigests: deps}
}

// AC1: a dependency re-verified against identical bytes leaves the consumer
// GREEN, regardless of a newer observation sequence.
func TestIdenticalInputRerunLeavesConsumerCurrent(t *testing.T) {
	dep := node("dep", nil, &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean})
	dep.Artifacts = []string{"src/dep.ext"}
	consumer := node("consumer", []string{"dep"}, passWithDeps(2, map[string]map[string]string{"dep": {"src/dep.ext": "sha-dep"}}))
	digest := func(rel string) string { return map[string]string{"src/dep.ext": "sha-dep"}[rel] }

	// Re-verify the dependency: newer seq, identical bytes.
	dep.Verification = &model.Verification{Result: model.ResultPass, Seq: 9, Isolation: model.IsolationClean}
	g := &model.Graph{Version: 1, Nodes: []model.Node{dep, consumer}}
	st := Derive(Inputs{Graph: g, ArtifactDigest: digest})
	if st["consumer"].State != Green || st["consumer"].SeqStale || len(st["consumer"].DependencyStale) != 0 {
		t.Fatalf("identical-bytes rerun must not stale the consumer: %+v", st["consumer"])
	}

	// Negative control: the dependency's bytes changed.
	changed := func(rel string) string { return map[string]string{"src/dep.ext": "sha-dep-v2"}[rel] }
	st = Derive(Inputs{Graph: g, ArtifactDigest: changed})
	if st["consumer"].State != Stale || len(st["consumer"].DependencyStale) != 1 || st["consumer"].DependencyStale[0] != "dep" {
		t.Fatalf("changed dependency bytes must stale the consumer naming the dep: %+v", st["consumer"])
	}
}

// A scheduling-only dependency (no artifacts) never stales its consumer.
func TestSchedulingOnlyDependencyNeverStales(t *testing.T) {
	dep := node("dep", nil, &model.Verification{Result: model.ResultPass, Seq: 5, Isolation: model.IsolationClean})
	consumer := node("consumer", []string{"dep"}, passWithDeps(1, map[string]map[string]string{"dep": {}}))
	g := &model.Graph{Version: 1, Nodes: []model.Node{dep, consumer}}
	st := Derive(Inputs{Graph: g, ArtifactDigest: func(string) string { return "" }})
	if st["consumer"].State != Green {
		t.Fatalf("scheduling-only dep must not stale: %+v", st["consumer"])
	}
}

// A command-gated node (e.g. a full-suite gate) gets the same
// dependency-digest staleness a tests gate gets, including when the
// dependency is a review gate — which carries no Node.Artifacts, so its
// identity is the recorded scope-diff digests rather than a literal artifact
// comparison, and the ripple has to go through the dependency's own derived
// state instead.
func TestCommandGateStalesOnReviewDependencyDigestDrift(t *testing.T) {
	review := node("review-execution", nil, pass(1))
	review.Gate = model.Gate{Type: model.GateReview}
	review.Verification.ArtifactDigests = map[string]string{"src/a.ext": "sha-old"}

	full := node("full-gate", []string{"review-execution"}, nil)
	full.Gate = model.Gate{Type: model.GateCommand}
	full.Verification = &model.Verification{
		Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean,
		DependencyDigests: map[string]map[string]string{"review-execution": {}},
	}

	g := &model.Graph{Version: 1, Nodes: []model.Node{review, full}}
	current := map[string]string{"src/a.ext": "sha-old"}
	st := Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return current[rel] }})
	if st["full-gate"].State != Green {
		t.Fatalf("unchanged review dep must leave the command gate GREEN: %+v", st["full-gate"])
	}

	current["src/a.ext"] = "sha-new"
	st = Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return current[rel] }})
	if st["full-gate"].State != Stale || len(st["full-gate"].DependencyStale) != 1 || st["full-gate"].DependencyStale[0] != "review-execution" {
		t.Fatalf("a review dep's drifted scope digest must stale the command gate: %+v", st["full-gate"])
	}
}

// Dependency staleness ripples: a consumer of a consumer goes stale when the
// root dependency's bytes change, even though its own direct dep's bytes did not.
func TestDependencyStalenessRipples(t *testing.T) {
	root := node("root", nil, &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean})
	root.Artifacts = []string{"src/root.ext"}
	mid := node("mid", []string{"root"}, passWithDeps(2, map[string]map[string]string{"root": {"src/root.ext": "r1"}}))
	mid.Artifacts = []string{"src/mid.ext"}
	mid.Verification.ArtifactDigests = map[string]string{"src/mid.ext": "m1"}
	top := node("top", []string{"mid"}, passWithDeps(3, map[string]map[string]string{"mid": {"src/mid.ext": "m1"}}))
	g := &model.Graph{Version: 1, Nodes: []model.Node{root, mid, top}}
	digest := func(rel string) string { return map[string]string{"src/root.ext": "r2", "src/mid.ext": "m1"}[rel] }
	st := Derive(Inputs{Graph: g, ArtifactDigest: digest})
	if len(st["mid"].DependencyStale) != 1 || len(st["top"].DependencyStale) != 1 || st["top"].DependencyStale[0] != "mid" {
		t.Fatalf("staleness must ripple through mid to top: mid=%+v top=%+v", st["mid"], st["top"])
	}
}

// Legacy observations (no dependency digests) keep sequence semantics for
// themselves only.
func TestLegacyObservationKeepsSequenceSemantics(t *testing.T) {
	dep := node("dep", nil, &model.Verification{Result: model.ResultPass, Seq: 9, Isolation: model.IsolationClean})
	legacy := node("legacy", []string{"dep"}, &model.Verification{Result: model.ResultPass, Seq: 2, Isolation: model.IsolationClean})
	modern := node("modern", []string{"dep"}, passWithDeps(2, map[string]map[string]string{"dep": {}}))
	g := &model.Graph{Version: 1, Nodes: []model.Node{dep, legacy, modern}}
	st := Derive(Inputs{Graph: g, ArtifactDigest: func(string) string { return "" }})
	if !st["legacy"].SeqStale || st["legacy"].State != Stale {
		t.Fatalf("legacy observation keeps seq staleness: %+v", st["legacy"])
	}
	if st["modern"].SeqStale || st["modern"].State != Green {
		t.Fatalf("modern observation never derives seq staleness: %+v", st["modern"])
	}
}

// AC4 (snapshot binds evidence): a run's own intent snapshot is the identity;
// a compile anchor that differs from it is an advisory, never staleness.
func TestObservationSnapshotBindsIntentAndAnchorDriftIsAdvisory(t *testing.T) {
	n := node("a", nil, &model.Verification{Result: model.ResultPass, Seq: 1, Isolation: model.IsolationClean,
		DependencyDigests: map[string]map[string]string{}, IntentHashes: map[string]string{"AC-01": "sha:new"}})
	n.Justifies = []string{"AC-01"}
	n.IntentHashes = map[string]string{"AC-01": "sha:old"} // compile anchor predates the judged change
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}
	st := Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{"AC-01": "sha:new"}})
	if st["a"].State != Green || len(st["a"].IntentStale) != 0 || len(st["a"].AnchorAdvisory) != 1 || st["a"].AnchorAdvisory[0] != "AC-01" {
		t.Fatalf("run snapshot matches current → GREEN with an advisory: %+v", st["a"])
	}
	// Negative control: the text moved again after the run.
	st = Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{"AC-01": "sha:newer"}})
	if st["a"].State != Stale || len(st["a"].IntentStale) != 1 {
		t.Fatalf("text changed after the run → INTENT-STALE: %+v", st["a"])
	}
	// Acknowledged: anchor rebound to the snapshot → no advisory, still GREEN.
	g.Nodes[0].IntentHashes["AC-01"] = "sha:new"
	st = Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{"AC-01": "sha:new"}})
	if st["a"].State != Green || len(st["a"].AnchorAdvisory) != 0 {
		t.Fatalf("acknowledged anchor clears the advisory: %+v", st["a"])
	}
}
