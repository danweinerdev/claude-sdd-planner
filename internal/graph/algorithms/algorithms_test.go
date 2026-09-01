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

func TestGraphAnalyticsCriticalPath(t *testing.T) {
	// Diamond with a heavy arm: a -> b(2) -> d and a -> c(5) -> d.
	g := Graph{"a": nil, "b": {"a"}, "c": {"a"}, "d": {"b", "c"}}
	est := map[string]int{"a": 1, "b": 2, "c": 5, "d": 1}
	rep := CriticalPath(g, est)
	if !reflect.DeepEqual(rep.Path, []string{"a", "c", "d"}) {
		t.Fatalf("critical path = %v", rep.Path)
	}
	if rep.Length != 7 || rep.Total != 9 {
		t.Fatalf("length/total = %d/%d, want 7/9", rep.Length, rep.Total)
	}
	if rep.Ceiling < 1.28 || rep.Ceiling > 1.29 {
		t.Fatalf("ceiling = %v, want 9/7", rep.Ceiling)
	}

	// Missing estimates default to 1; empty graph is a zero report.
	rep = CriticalPath(Graph{"solo": nil}, nil)
	if !reflect.DeepEqual(rep.Path, []string{"solo"}) || rep.Length != 1 || rep.Total != 1 || rep.Ceiling != 1 {
		t.Fatalf("solo: %+v", rep)
	}
	if rep := CriticalPath(Graph{}, nil); rep.Length != 0 || rep.Total != 0 || len(rep.Path) != 0 {
		t.Fatalf("empty graph: %+v", rep)
	}

	// A pure chain prices zero parallelism: ceiling exactly 1.
	chain := Graph{"a": nil, "b": {"a"}, "c": {"b"}}
	rep = CriticalPath(chain, nil)
	if rep.Ceiling != 1 || !reflect.DeepEqual(rep.Path, []string{"a", "b", "c"}) {
		t.Fatalf("chain: %+v", rep)
	}
}

func TestGraphAnalyticsCutVertices(t *testing.T) {
	// Two fans joined at one waist node m: removing m disconnects them.
	g := Graph{"a": nil, "b": nil, "m": {"a", "b"}, "x": {"m"}, "y": {"m"}}
	if got := CutVertices(g); !reflect.DeepEqual(got, []string{"m"}) {
		t.Fatalf("waist fixture: %v", got)
	}
	// A diamond has no cut vertex: every node has a way around.
	diamond := Graph{"a": nil, "b": {"a"}, "c": {"a"}, "d": {"b", "c"}}
	if got := CutVertices(diamond); len(got) != 0 {
		t.Fatalf("diamond must have no cut vertices: %v", got)
	}
	// A chain's interior nodes are all cut vertices.
	chain := Graph{"a": nil, "b": {"a"}, "c": {"b"}}
	if got := CutVertices(chain); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("chain interior: %v", got)
	}
	// Disconnected components: cut vertices are per-component.
	two := Graph{"a": nil, "b": {"a"}, "c": {"b"}, "p": nil, "q": {"p"}}
	if got := CutVertices(two); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("two components: %v", got)
	}
}

func TestGraphAnalyticsDepthHistogramAndSilhouette(t *testing.T) {
	// Graph-derived: diamond depths are 0,1,1,2 -> [1 2 1] -> MIXED
	// (widens then narrows: neither funnel nor hourglass nor flat).
	diamond := Graph{"a": nil, "b": {"a"}, "c": {"a"}, "d": {"b", "c"}}
	hist := DepthHistogram(diamond)
	if !reflect.DeepEqual(hist, []int{1, 2, 1}) {
		t.Fatalf("diamond histogram = %v", hist)
	}
	if got := Silhouette(hist); got != ShapeMixed {
		t.Fatalf("diamond = %s, want MIXED", got)
	}

	// One known answer per class.
	for _, tc := range []struct {
		hist []int
		want string
	}{
		{[]int{5}, ShapeFlat},          // everything at once
		{[]int{3, 4}, ShapeFlat},       // two shallow levels
		{[]int{1, 1, 1}, ShapeChain},   // serial plan
		{[]int{4, 2, 1}, ShapeFunnel},  // converging
		{[]int{4, 1, 3}, ShapeHourglass}, // waist
		{[]int{1, 2, 1}, ShapeMixed},
		{nil, ShapeFlat}, // empty graph makes no shape claim beyond FLAT
	} {
		if got := Silhouette(tc.hist); got != tc.want {
			t.Errorf("Silhouette(%v) = %s, want %s", tc.hist, got, tc.want)
		}
	}

	// Chain graph end to end: histogram all-ones, class CHAIN.
	chain := Graph{"a": nil, "b": {"a"}, "c": {"b"}}
	if got := Silhouette(DepthHistogram(chain)); got != ShapeChain {
		t.Fatalf("chain graph = %s", got)
	}
}
