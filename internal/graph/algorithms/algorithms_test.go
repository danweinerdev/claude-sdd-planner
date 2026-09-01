package algorithms

import (
	"reflect"
	"strings"
	"testing"
)

func TestTopoSortOrdersDepsFirst(t *testing.T) {
	g := Graph{
		"c": {"b"},
		"b": {"a"},
		"a": nil,
		"d": {"a", "b"},
	}
	order := TopoSort(g)
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if len(order) != 4 {
		t.Fatalf("topo order must cover the acyclic graph, got %v", order)
	}
	for id, deps := range g {
		for _, dep := range deps {
			if pos[dep] > pos[id] {
				t.Fatalf("dep %s must precede %s in %v", dep, id, order)
			}
		}
	}
	// Deterministic: same input, same order.
	if again := TopoSort(g); !reflect.DeepEqual(order, again) {
		t.Fatalf("topo order must be deterministic: %v vs %v", order, again)
	}
}

func TestTopoSortOmitsCycleMembers(t *testing.T) {
	g := Graph{
		"a": nil,
		"b": {"c"},
		"c": {"b"},
		"d": {"a"},
	}
	order := TopoSort(g)
	joined := strings.Join(order, ",")
	if strings.Contains(joined, "b") || strings.Contains(joined, "c") {
		t.Fatalf("cycle members must be omitted from the partial order: %v", order)
	}
	if !strings.Contains(joined, "a") || !strings.Contains(joined, "d") {
		t.Fatalf("acyclic remainder must still be ordered: %v", order)
	}
}

func TestCyclesFindsEveryCycleDeterministically(t *testing.T) {
	g := Graph{
		"a":    {"b"},
		"b":    {"a"},
		"self": {"self"},
		"x":    {"y"},
		"y":    {"z"},
		"z":    {"x"},
		"ok":   {"a"}, // depends into a cycle but is not part of one
	}
	got := Cycles(g)
	want := [][]string{{"a", "b"}, {"self"}, {"x", "y", "z"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cycles = %v, want %v", got, want)
	}
	if again := Cycles(g); !reflect.DeepEqual(got, again) {
		t.Fatalf("cycle detection must be deterministic")
	}
}

func TestCyclesEmptyOnDAG(t *testing.T) {
	g := Graph{"a": nil, "b": {"a"}, "c": {"a", "b"}}
	if got := Cycles(g); len(got) != 0 {
		t.Fatalf("a DAG has no cycles, got %v", got)
	}
}

func TestCyclesIterativeSurvivesDeepChains(t *testing.T) {
	// A recursion-based Tarjan would overflow on a long chain; the iterative
	// form must not.
	g := Graph{}
	prev := ""
	for i := 0; i < 100000; i++ {
		id := "n" + string(rune('a'+i%26)) + itoa(i)
		if prev == "" {
			g[id] = nil
		} else {
			g[id] = []string{prev}
		}
		prev = id
	}
	if got := Cycles(g); len(got) != 0 {
		t.Fatalf("deep chain has no cycles, got %d", len(got))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestDependencyClosure(t *testing.T) {
	g := Graph{
		"gate": {"a", "b"},
		"a":    {"shared"},
		"b":    {"shared"},
		"shared": nil,
		"other":  nil,
	}
	got := DependencyClosure(g, "gate")
	want := map[string]bool{"a": true, "b": true, "shared": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("closure = %v, want %v", got, want)
	}
	if got["gate"] {
		t.Fatal("closure must exclude the start node")
	}
}
