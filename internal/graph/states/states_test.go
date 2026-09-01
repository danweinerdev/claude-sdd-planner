package states

import (
	"fmt"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func node(id string, deps []string, v *model.Verification) model.Node {
	return model.Node{
		ID: id, Contract: "c", Deps: deps,
		Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1,
		Verification: v,
	}
}

func pass(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, Isolation: model.IsolationClean}
}

func fail(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultFail, Seq: seq, Isolation: model.IsolationClean}
}

func TestStateTable(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		node("green-root", nil, pass(1)),
		node("ready", []string{"green-root"}, nil),
		node("blocked", []string{"ready"}, nil),
		// RED outranks BLOCKED: last verification failed AND a dep is
		// non-GREEN — it must still report RED.
		node("red-with-nongreen-dep", []string{"ready"}, fail(2)),
		// Seq staleness: verified at 3, but its dep re-verified at 5.
		node("reverified-dep", nil, pass(5)),
		node("seq-stale", []string{"reverified-dep"}, pass(3)),
		// Transitive seq staleness: the newer seq travels through a GREEN
		// middle node... middle verified at 6 > 5 keeps chain honest.
		node("middle", []string{"reverified-dep"}, pass(6)),
		node("transitively-stale", []string{"middle"}, pass(4)),
	}}
	s := Derive(Inputs{Graph: g})

	want := map[string]State{
		"green-root":             Green,
		"ready":                  Ready,
		"blocked":                Blocked,
		"red-with-nongreen-dep":  Red,
		"reverified-dep":         Green,
		"seq-stale":              Stale,
		"middle":                 Green,
		"transitively-stale":     Stale,
	}
	for id, w := range want {
		if s[id].State != w {
			t.Errorf("%s = %s, want %s", id, s[id].State, w)
		}
	}
	if !s["seq-stale"].SeqStale || !s["transitively-stale"].SeqStale {
		t.Error("seq staleness must be attributed as the cause")
	}

	// Workable ≠ frontier: RED with a non-GREEN dep is workable but off the
	// frontier; STALE with GREEN deps is on it.
	if !s["red-with-nongreen-dep"].Workable || s["red-with-nongreen-dep"].OnFrontier {
		t.Error("RED with a non-GREEN dep is workable but not frontier")
	}
	if !s["seq-stale"].Workable || !s["seq-stale"].OnFrontier {
		t.Error("STALE with GREEN deps belongs on the frontier")
	}
	if got := Frontier(s); len(got) == 0 {
		t.Error("frontier must not be empty here")
	}
}

func TestDigestStaleness(t *testing.T) {
	n := node("a", nil, pass(1))
	n.Artifacts = []string{"src/a.ext", "src/b.ext", "src/new.ext"}
	n.Verification.ArtifactDigests = map[string]string{
		"src/a.ext": "sha256:aaaa",
		"src/b.ext": "sha256:bbbb",
		// src/new.ext was declared after the observation: never recorded.
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}

	current := map[string]string{
		"src/a.ext": "sha256:aaaa", // unchanged
		"src/b.ext": "sha256:DIFF", // silently edited
		"src/new.ext": "sha256:cccc",
	}
	s := Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return current[rel] }})
	ns := s["a"]
	if ns.State != Stale {
		t.Fatalf("state = %s, want STALE", ns.State)
	}
	if len(ns.DigestStale) != 2 || ns.DigestStale[0] != "src/b.ext" || ns.DigestStale[1] != "src/new.ext" {
		t.Fatalf("digest-stale artifacts = %v", ns.DigestStale)
	}

	// Without a digest source, the axis is disabled and the node is GREEN.
	s = Derive(Inputs{Graph: g})
	if s["a"].State != Green {
		t.Fatalf("digest axis disabled must not stale: %s", s["a"].State)
	}
}

func TestReviewGateDigestStalenessOverRecordedKeys(t *testing.T) {
	// A review gate declares no artifacts of its own; its observation
	// records the aggregate scope diff (every scope artifact's digest at
	// review time). Drift in ANY recorded key is ordinary digest staleness.
	gate := node("g1", nil, pass(1))
	gate.Gate = model.Gate{Type: model.GateReview}
	gate.Verification.ArtifactDigests = map[string]string{
		"src/a.ext": "sha256:aaaa",
		"src/b.ext": "sha256:bbbb",
	}
	g := &model.Graph{Version: 1, Nodes: []model.Node{gate}}

	fresh := map[string]string{"src/a.ext": "sha256:aaaa", "src/b.ext": "sha256:bbbb"}
	s := Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return fresh[rel] }})
	if s["g1"].State != Green {
		t.Fatalf("matching aggregate diff must stay GREEN: %+v", s["g1"])
	}

	drifted := map[string]string{"src/a.ext": "sha256:aaaa", "src/b.ext": "sha256:DIFF"}
	s = Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return drifted[rel] }})
	if s["g1"].State != Stale || len(s["g1"].DigestStale) != 1 || s["g1"].DigestStale[0] != "src/b.ext" {
		t.Fatalf("a drifted scope artifact must stale the gate: %+v", s["g1"])
	}

	// A reviewed artifact deleted from disk is drift too.
	gone := map[string]string{"src/a.ext": "sha256:aaaa"}
	s = Derive(Inputs{Graph: g, ArtifactDigest: func(rel string) string { return gone[rel] }})
	if s["g1"].State != Stale {
		t.Fatalf("a deleted reviewed artifact must stale the gate: %+v", s["g1"])
	}
}

func TestIntentStaleness(t *testing.T) {
	n := node("a", nil, pass(1))
	n.IntentHashes = map[string]string{"AC-01": "sha256:old", "FR-01": "sha256:same"}
	g := &model.Graph{Version: 1, Nodes: []model.Node{n}}

	s := Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{
		"AC-01": "sha256:new", // requirement reworded
		"FR-01": "sha256:same",
	}})
	ns := s["a"]
	if ns.State != Stale || len(ns.IntentStale) != 1 || ns.IntentStale[0] != "AC-01" {
		t.Fatalf("INTENT-STALE must name exactly the drifted citation: %+v", ns)
	}
	if ns.SeqStale || len(ns.DigestStale) != 0 {
		t.Fatal("intent staleness must be attributed distinctly from the other axes")
	}

	// A citation that no longer resolves at all is also intent-stale.
	s = Derive(Inputs{Graph: g, CurrentIntentHashes: map[string]string{"FR-01": "sha256:same"}})
	if got := s["a"].IntentStale; len(got) != 1 || got[0] != "AC-01" {
		t.Fatalf("an unresolvable citation is intent-stale: %v", got)
	}
}

func TestCycleMembersAreBlockedDefensively(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		node("a", []string{"b"}, nil),
		node("b", []string{"a"}, nil),
		node("ok", nil, nil),
	}}
	s := Derive(Inputs{Graph: g})
	for _, id := range []string{"a", "b"} {
		if s[id].State != Blocked || !s[id].InCycle || s[id].Workable {
			t.Errorf("%s: cycle members are BLOCKED, flagged, never workable: %+v", id, s[id])
		}
	}
	if s["ok"].State != Ready {
		t.Errorf("the acyclic remainder still derives: %+v", s["ok"])
	}
}

// TestNothingDerivableIsStored pins the DD-3 rule at the API level: Derive
// takes a graph and returns states; running it twice over the same inputs is
// pure, and mutating the returned map cannot affect a later derive.
func TestNothingDerivableIsStored(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{node("a", nil, pass(1))}}
	first := Derive(Inputs{Graph: g})
	first["a"] = NodeState{ID: "a", State: Red}
	second := Derive(Inputs{Graph: g})
	if second["a"].State != Green {
		t.Fatal("derive must be pure; a caller's mutation leaked into a later pass")
	}
}

func TestThousandNodePassIsFast(t *testing.T) {
	g := &model.Graph{Version: 1}
	for i := 0; i < 1000; i++ {
		var deps []string
		if i > 0 {
			deps = []string{fmt.Sprintf("n%03d", (i-1)/2)} // a wide-ish DAG
		}
		g.Nodes = append(g.Nodes, node(fmt.Sprintf("n%03d", i), deps, pass(i%7)))
	}
	start := time.Now()
	s := Derive(Inputs{Graph: g})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("1000-node derive took %v, want < 1s", elapsed)
	}
	if len(s) != 1000 {
		t.Fatalf("derived %d states", len(s))
	}
}
