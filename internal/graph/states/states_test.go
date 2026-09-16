package states

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func pass(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultPass, Seq: seq, ContractRev: 1, Isolation: model.IsolationClean}
}

func stateNode(id string, deps []string, v *model.Verification) model.Node {
	return model.Node{ID: id, Contract: "c", Deps: deps, Gate: model.Gate{Type: model.GateTests}, Hazards: model.Hazards{}, Estimate: 1, Verification: v}
}

func fail(seq int) *model.Verification {
	return &model.Verification{Result: model.ResultFail, Seq: seq, ContractRev: 1, Isolation: model.IsolationClean}
}

func TestStateTable(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		stateNode("green-root", nil, pass(1)),
		stateNode("ready", []string{"green-root"}, nil),
		stateNode("blocked", []string{"ready"}, nil),
		stateNode("red-with-nongreen-dep", []string{"ready"}, fail(2)),
		stateNode("reverified-dep", nil, pass(5)),
		stateNode("seq-stale", []string{"reverified-dep"}, pass(3)),
		stateNode("middle", []string{"reverified-dep"}, pass(6)),
		stateNode("transitively-current", []string{"middle"}, pass(7)),
	}}
	s := Derive(Inputs{Graph: g})
	want := map[string]State{"green-root": Green, "ready": Ready, "blocked": Blocked, "red-with-nongreen-dep": Red, "reverified-dep": Green, "seq-stale": Stale, "middle": Green, "transitively-current": Green}
	for id, expected := range want {
		if s[id].State != expected {
			t.Errorf("%s = %s, want %s", id, s[id].State, expected)
		}
	}
	if !s["red-with-nongreen-dep"].Workable || s["red-with-nongreen-dep"].OnFrontier {
		t.Error("RED with a non-GREEN dep is workable but not frontier")
	}
	if !s["seq-stale"].Workable || !s["seq-stale"].OnFrontier || !reflect.DeepEqual(s["seq-stale"].DependencyStale, []string{"reverified-dep"}) {
		t.Errorf("STALE with GREEN deps belongs on the frontier: %+v", s["seq-stale"])
	}
	if got := Frontier(s); len(got) == 0 {
		t.Error("frontier must not be empty here")
	}
}

func TestCycleMembersAreBlockedDefensively(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{stateNode("a", []string{"b"}, nil), stateNode("b", []string{"a"}, nil), stateNode("ok", nil, nil)}}
	s := Derive(Inputs{Graph: g})
	for _, id := range []string{"a", "b"} {
		if s[id].State != Blocked || !s[id].InCycle || s[id].Workable {
			t.Errorf("%s: cycle members are BLOCKED, flagged, never workable: %+v", id, s[id])
		}
	}
	if s["ok"].State != Ready {
		t.Errorf("acyclic remainder: %+v", s["ok"])
	}
}

func TestNothingDerivableIsStored(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{stateNode("a", nil, pass(1))}}
	first := Derive(Inputs{Graph: g})
	first["a"] = NodeState{ID: "a", State: Red}
	if second := Derive(Inputs{Graph: g}); second["a"].State != Green {
		t.Fatal("derive must be pure")
	}
}

func TestThousandNodePassIsFast(t *testing.T) {
	g := &model.Graph{Version: 1}
	for i := 0; i < 1000; i++ {
		var deps []string
		if i > 0 {
			deps = []string{fmt.Sprintf("n%03d", (i-1)/2)}
		}
		g.Nodes = append(g.Nodes, stateNode(fmt.Sprintf("n%03d", i), deps, pass(i%7)))
	}
	start := time.Now()
	s := Derive(Inputs{Graph: g})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("1000-node derive took %v", elapsed)
	}
	if len(s) != 1000 {
		t.Fatalf("derived %d states", len(s))
	}
}

func TestReviewCoverageMustMatchCurrentScope(t *testing.T) {
	a := stateNode("a", nil, pass(1))
	b := stateNode("b", nil, pass(1))
	gate := stateNode("review", []string{"a", "b"}, pass(2))
	gate.Gate = model.Gate{Type: model.GateReview}
	gate.Verification.Reviewed = map[string]model.ReviewedRef{"a": {ContractRev: 1}}
	g := &model.Graph{Version: 1, Nodes: []model.Node{a, b, gate}}
	got := Derive(Inputs{Graph: g})["review"]
	if got.State != Stale || len(got.ReviewStale) != 1 || got.ReviewStale[0] != "b" {
		t.Fatalf("review evidence that omits current scope must be stale: %+v", got)
	}
}

func TestLegacyReviewObservationRetainsPreReviewedSetMeaning(t *testing.T) {
	a := stateNode("a", nil, pass(1))
	gate := stateNode("review", []string{"a"}, pass(2))
	gate.Gate = model.Gate{Type: model.GateReview}
	got := Derive(Inputs{Graph: &model.Graph{Version: 1, Nodes: []model.Node{a, gate}}})["review"]
	if got.State != Green || len(got.ReviewStale) != 0 {
		t.Fatalf("legacy review meaning changed: %+v", got)
	}
}

func TestReviewScopeSubtractsOnlyCurrentGreenInnerReview(t *testing.T) {
	a := stateNode("a", nil, pass(1))
	inner := stateNode("inner", []string{"a"}, pass(2))
	inner.Gate = model.Gate{Type: model.GateReview}
	inner.Verification.Reviewed = map[string]model.ReviewedRef{"a": {ContractRev: 1}}
	outer := stateNode("outer", []string{"inner"}, nil)
	outer.Gate = model.Gate{Type: model.GateReview}
	g := &model.Graph{Version: 1, Nodes: []model.Node{a, inner, outer}}
	scope, err := ReviewScope(g, "outer")
	if err != nil || len(scope) != 0 {
		t.Fatalf("current inner review not subtracted: %v %v", scope, err)
	}
	g.NodeByID("a").ContractRev = 2
	scope, err = ReviewScope(g, "outer")
	if err != nil || !reflect.DeepEqual(scope, []string{"a", "inner"}) {
		t.Fatalf("stale inner review hid scope: %v %v", scope, err)
	}
}

func TestReviewScopeIsCycleSafeAndExcludesGate(t *testing.T) {
	a := stateNode("a", []string{"review"}, nil)
	gate := stateNode("review", []string{"a"}, nil)
	gate.Gate = model.Gate{Type: model.GateReview}
	scope, err := ReviewScope(&model.Graph{Version: 1, Nodes: []model.Node{a, gate}}, "review")
	if err != nil || !reflect.DeepEqual(scope, []string{"a"}) {
		t.Fatalf("cycle-safe scope = %v, err %v", scope, err)
	}
}

func TestDependencyReverificationStalesOnlyDirectConsumer(t *testing.T) {
	g := &model.Graph{Version: 1, Nodes: []model.Node{
		{ID: "dep", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(3)},
		{ID: "consumer", Deps: []string{"dep"}, Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(2)},
		{ID: "other", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(1)},
	}}
	got := Derive(Inputs{Graph: g})
	if got["consumer"].State != Stale || !reflect.DeepEqual(got["consumer"].DependencyStale, []string{"dep"}) {
		t.Fatalf("consumer=%+v", got["consumer"])
	}
	if got["other"].State != Green {
		t.Fatalf("unrelated=%+v", got["other"])
	}
}

func TestReviewStaleByScopeContractAndSequence(t *testing.T) {
	base := func() *model.Graph {
		return &model.Graph{Version: 1, Nodes: []model.Node{
			{ID: "work", ContractRev: 1, Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(1)},
			{ID: "review", Deps: []string{"work"}, Gate: model.Gate{Type: model.GateReview}, Hazards: model.Hazards{}, Estimate: 1, Verification: &model.Verification{Result: model.ResultPass, Seq: 2, ContractRev: 1, Isolation: model.IsolationClean, Reviewed: map[string]model.ReviewedRef{"work": {ContractRev: 1}}}},
		}}
	}
	if got := Derive(Inputs{Graph: base()})["review"]; got.State != Green {
		t.Fatalf("base=%+v", got)
	}
	for _, mutate := range []func(*model.Graph){
		func(g *model.Graph) { g.NodeByID("work").ContractRev = 2 },
		func(g *model.Graph) { g.NodeByID("work").Verification = pass(3) },
		func(g *model.Graph) {
			g.Nodes = append(g.Nodes, model.Node{ID: "new", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(1)})
			g.NodeByID("review").Deps = append(g.NodeByID("review").Deps, "new")
		},
	} {
		g := base()
		mutate(g)
		if got := Derive(Inputs{Graph: g})["review"]; got.State != Stale || len(got.ReviewStale) == 0 {
			t.Fatalf("review=%+v", got)
		}
	}
}
