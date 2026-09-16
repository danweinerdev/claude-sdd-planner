package states

import (
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func TestDependencyReobservationStalesDirectConsumerOnly(t *testing.T) {
	a := model.Node{ID: "a", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(4)}
	b := model.Node{ID: "b", Deps: []string{"a"}, Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(2)}
	c := model.Node{ID: "c", Deps: []string{"b"}, Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(3)}
	g := &model.Graph{Version: 1, Nodes: []model.Node{a, b, c}}
	got := Derive(Inputs{Graph: g})
	if got["b"].State != Stale || len(got["b"].DependencyStale) != 1 || got["b"].DependencyStale[0] != "a" {
		t.Fatalf("B must stale after newer A: %+v", got["b"])
	}
	if got["c"].State != Green || len(got["c"].DependencyStale) != 0 {
		t.Fatalf("C compares B's observation sequence directly and stays GREEN: %+v", got["c"])
	}
}

func TestLegacyObservationKeepsSequenceSemantics(t *testing.T) {
	dep := model.Node{ID: "dep", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(9)}
	consumer := model.Node{ID: "consumer", Deps: []string{"dep"}, Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(2)}
	g := &model.Graph{Version: 1, Nodes: []model.Node{dep, consumer}}
	if got := Derive(Inputs{Graph: g})["consumer"]; got.State != Stale {
		t.Fatalf("sequence is the sole dependency freshness rule: %+v", got)
	}
}

func TestSchedulingOnlyDependencyReobservationStalesConsumer(t *testing.T) {
	dep := model.Node{ID: "dep", Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(5)}
	consumer := model.Node{ID: "consumer", Deps: []string{"dep"}, Gate: model.Gate{Type: model.GateCommand}, Hazards: model.Hazards{}, Estimate: 1, Verification: pass(1)}
	g := &model.Graph{Version: 1, Nodes: []model.Node{dep, consumer}}
	st := Derive(Inputs{Graph: g})
	if st["consumer"].State != Stale || len(st["consumer"].DependencyStale) != 1 || st["consumer"].DependencyStale[0] != "dep" {
		t.Fatalf("sequence freshness stales every direct consumer: %+v", st["consumer"])
	}
}
