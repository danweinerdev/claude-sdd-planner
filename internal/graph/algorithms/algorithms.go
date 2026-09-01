// Package algorithms is pure graph theory over node-id adjacency: it knows
// nothing of state, disk, or the model package (Designs/SddGraph
// § Components). Compile uses TopoSort/Cycles for its semantic findings;
// phase 3's derived states walk the same order; phase 4 adds critical path,
// cut vertices, and silhouette on the same representation.
//
// Every function is deterministic: ids are visited in sorted order, so two
// runs over the same graph produce identical results, orders, and error
// text — a requirement, not a nicety, because these outputs land in
// committed findings and diffs.
package algorithms

import "sort"

// Graph is an adjacency map: node id -> the ids it depends on. Callers own
// the invariant that referenced ids exist (compile reports dangling deps as
// its own finding before asking for cycles).
type Graph map[string][]string

// sortedIDs returns the graph's node ids in sorted order.
func sortedIDs(g Graph) []string {
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// TopoSort returns a dependency-first order: every node appears after all of
// its deps. Nodes inside cycles are omitted — Cycles reports them, and a
// partial order over the acyclic remainder is still useful for rendering.
// The order is deterministic (Kahn's algorithm with a sorted frontier).
func TopoSort(g Graph) []string {
	// Count unresolved deps per node, ignoring dangling references: a dep
	// outside the graph cannot gate anything here (it is compile's dangling
	// finding, not a scheduling input).
	remaining := map[string]int{}
	dependants := map[string][]string{}
	for _, id := range sortedIDs(g) {
		count := 0
		for _, dep := range g[id] {
			if _, ok := g[dep]; ok {
				count++
				dependants[dep] = append(dependants[dep], id)
			}
		}
		remaining[id] = count
	}
	var frontier []string
	for _, id := range sortedIDs(g) {
		if remaining[id] == 0 {
			frontier = append(frontier, id)
		}
	}
	var order []string
	for len(frontier) > 0 {
		sort.Strings(frontier)
		id := frontier[0]
		frontier = frontier[1:]
		order = append(order, id)
		for _, dependant := range dependants[id] {
			remaining[dependant]--
			if remaining[dependant] == 0 {
				frontier = append(frontier, dependant)
			}
		}
	}
	return order
}

// Cycles returns every dependency cycle: the strongly connected components
// with more than one member, plus single nodes that depend on themselves.
// Each cycle's members are sorted, and cycles are ordered by their first
// member, so findings are stable across runs. Tarjan's algorithm, iterative
// so a pathological chain cannot overflow the stack.
func Cycles(g Graph) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	next := 0
	var out [][]string

	// Neighbors restricted to ids present in the graph, sorted for
	// determinism.
	neighbors := func(id string) []string {
		var ns []string
		for _, dep := range g[id] {
			if _, ok := g[dep]; ok {
				ns = append(ns, dep)
			}
		}
		sort.Strings(ns)
		return ns
	}

	type frame struct {
		id string
		ns []string
		i  int
	}
	strongconnect := func(root string) {
		frames := []frame{{id: root, ns: neighbors(root)}}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		onStack[root] = true

		for len(frames) > 0 {
			f := &frames[len(frames)-1]
			if f.i < len(f.ns) {
				n := f.ns[f.i]
				f.i++
				if _, seen := index[n]; !seen {
					index[n], low[n] = next, next
					next++
					stack = append(stack, n)
					onStack[n] = true
					frames = append(frames, frame{id: n, ns: neighbors(n)})
				} else if onStack[n] && index[n] < low[f.id] {
					low[f.id] = index[n]
				}
				continue
			}
			// Frame complete: pop an SCC if this is its root.
			if low[f.id] == index[f.id] {
				var members []string
				for {
					top := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[top] = false
					members = append(members, top)
					if top == f.id {
						break
					}
				}
				if len(members) > 1 || selfLoop(g, f.id) {
					sort.Strings(members)
					out = append(out, members)
				}
			}
			done := *f
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				parent := &frames[len(frames)-1]
				if low[done.id] < low[parent.id] {
					low[parent.id] = low[done.id]
				}
			}
		}
	}

	for _, id := range sortedIDs(g) {
		if _, seen := index[id]; !seen {
			strongconnect(id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func selfLoop(g Graph, id string) bool {
	for _, dep := range g[id] {
		if dep == id {
			return true
		}
	}
	return false
}

// CriticalWeight returns, for every node, the heaviest estimate-sum path
// from that node DOWNSTREAM through its dependants to a sink, including the
// node's own estimate — "how much of the wall-clock floor still hangs off
// this node". `next` serves the frontier heaviest-first so a capacity-1
// provider works the node that keeps the floor from rising (DD-14's
// critical-path preference, in the minimal form scheduling needs; the full
// analytics surface lands with `graph path`). Cycle members are omitted,
// matching TopoSort.
func CriticalWeight(g Graph, estimate map[string]int) map[string]int {
	dependants := map[string][]string{}
	for _, id := range sortedIDs(g) {
		for _, dep := range g[id] {
			if _, ok := g[dep]; ok {
				dependants[dep] = append(dependants[dep], id)
			}
		}
	}
	order := TopoSort(g)
	weight := make(map[string]int, len(order))
	// Walk the order backwards: every dependant is later in a topological
	// order, so its weight is final by the time its dep is visited.
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		best := 0
		for _, dependant := range dependants[id] {
			if w := weight[dependant]; w > best {
				best = w
			}
		}
		weight[id] = estimate[id] + best
	}
	return weight
}

// DependencyClosure returns every id reachable from start through deps
// (start excluded), restricted to ids present in the graph. Deterministic
// BFS. Phase 4's review-gate scope derivation and compile's coverage
// invariant both consume it.
func DependencyClosure(g Graph, start string) map[string]bool {
	out := map[string]bool{}
	frontier := []string{start}
	for len(frontier) > 0 {
		id := frontier[0]
		frontier = frontier[1:]
		for _, dep := range g[id] {
			if _, ok := g[dep]; !ok || out[dep] {
				continue
			}
			out[dep] = true
			frontier = append(frontier, dep)
		}
	}
	return out
}

// PathReport is the critical-path analysis `graph path` prints (DD-14:
// the ceiling prices parallelism — no capacity can beat the heaviest
// dependency chain).
type PathReport struct {
	// Path is the heaviest estimate-sum chain, source to sink.
	Path []string `json:"path"`
	// Length is the estimate sum along Path: the wall-clock floor.
	Length int `json:"length"`
	// Total is the estimate sum over every node: the serial cost.
	Total int `json:"total"`
	// Ceiling is Total/Length: the best speedup unlimited capacity buys.
	Ceiling float64 `json:"ceiling"`
}

// CriticalPath computes the heaviest estimate-sum dependency chain. Missing
// estimates default to 1 (the model's floor); cycle members are omitted,
// matching TopoSort. An empty (or all-cycle) graph returns a zero report.
func CriticalPath(g Graph, estimate map[string]int) PathReport {
	est := func(id string) int {
		if e := estimate[id]; e > 0 {
			return e
		}
		return 1
	}
	weight := map[string]int{}
	order := TopoSort(g)
	// Same recurrence as CriticalWeight, inlined so the two stay
	// definitionally identical while this one also reconstructs the path.
	dependants := map[string][]string{}
	for _, id := range sortedIDs(g) {
		for _, dep := range g[id] {
			if _, ok := g[dep]; ok {
				dependants[dep] = append(dependants[dep], id)
			}
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		best := 0
		for _, dependant := range dependants[id] {
			if w := weight[dependant]; w > best {
				best = w
			}
		}
		weight[id] = est(id) + best
	}

	rep := PathReport{}
	for _, id := range order {
		rep.Total += est(id)
	}
	// The heaviest full chain starts at a source: extending any path
	// backward only adds positive estimates.
	start := ""
	for _, id := range order {
		if len(g[id]) > 0 {
			continue // not a source (has deps inside the graph)
		}
		if start == "" || weight[id] > weight[start] {
			start = id
		}
	}
	if start == "" {
		return rep
	}
	rep.Length = weight[start]
	for cur := start; cur != ""; {
		rep.Path = append(rep.Path, cur)
		next := ""
		want := weight[cur] - est(cur)
		if want <= 0 {
			break
		}
		deps := append([]string(nil), dependants[cur]...)
		sortStrings(deps)
		for _, d := range deps {
			if weight[d] == want {
				next = d
				break
			}
		}
		cur = next
	}
	if rep.Length > 0 {
		rep.Ceiling = float64(rep.Total) / float64(rep.Length)
	}
	return rep
}

// CutVertices returns the articulation points of the dependency graph viewed
// undirected: nodes whose removal disconnects work that is currently
// connected — the waists where a single node's failure stalls both sides
// (DD-14: cut vertices aim review attention). Sorted; deterministic.
func CutVertices(g Graph) []string {
	undirected := map[string][]string{}
	addEdge := func(a, b string) {
		undirected[a] = append(undirected[a], b)
		undirected[b] = append(undirected[b], a)
	}
	for _, id := range sortedIDs(g) {
		if _, ok := undirected[id]; !ok {
			undirected[id] = nil
		}
		for _, dep := range g[id] {
			if _, ok := g[dep]; ok {
				addEdge(id, dep)
			}
		}
	}
	ids := make([]string, 0, len(undirected))
	for id := range undirected {
		ids = append(ids, id)
	}
	sortStrings(ids)
	for _, id := range ids {
		sortStrings(undirected[id])
	}

	// Iterative Hopcroft-Tarjan articulation points.
	disc := map[string]int{}
	low := map[string]int{}
	parent := map[string]string{}
	cut := map[string]bool{}
	timer := 0
	for _, root := range ids {
		if _, seen := disc[root]; seen {
			continue
		}
		type frame struct {
			id   string
			next int
		}
		stack := []frame{{root, 0}}
		timer++
		disc[root], low[root] = timer, timer
		rootChildren := 0
		for len(stack) > 0 {
			f := &stack[len(stack)-1]
			if f.next < len(undirected[f.id]) {
				n := undirected[f.id][f.next]
				f.next++
				if _, seen := disc[n]; !seen {
					parent[n] = f.id
					if f.id == root {
						rootChildren++
					}
					timer++
					disc[n], low[n] = timer, timer
					stack = append(stack, frame{n, 0})
				} else if n != parent[f.id] && disc[n] < low[f.id] {
					low[f.id] = disc[n]
				}
			} else {
				stack = stack[:len(stack)-1]
				if p := parent[f.id]; p != "" {
					if low[f.id] < low[p] {
						low[p] = low[f.id]
					}
					if p != root && low[f.id] >= disc[p] {
						cut[p] = true
					}
				}
			}
		}
		if rootChildren > 1 {
			cut[root] = true
		}
	}
	var out []string
	for id := range cut {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

// DepthHistogram returns the node count per dependency depth: depth 0 is a
// source (no in-graph deps), and every other node sits one past its deepest
// dep. Cycle members are omitted, matching TopoSort.
func DepthHistogram(g Graph) []int {
	depth := map[string]int{}
	var hist []int
	for _, id := range TopoSort(g) {
		d := 0
		for _, dep := range g[id] {
			if dd, ok := depth[dep]; ok && dd+1 > d {
				d = dd + 1
			}
		}
		depth[id] = d
		for len(hist) <= d {
			hist = append(hist, 0)
		}
		hist[d]++
	}
	return hist
}

// Silhouette classes (DD-14: the histogram's shape diagnoses decomposition
// quality — a CHAIN prices zero parallelism, an HOURGLASS names a waist).
const (
	ShapeFlat      = "FLAT"
	ShapeChain     = "CHAIN"
	ShapeFunnel    = "FUNNEL"
	ShapeHourglass = "HOURGLASS"
	ShapeMixed     = "MIXED"
)

// Silhouette classifies a depth histogram. Rules, first match wins:
// CHAIN — more than one level, every level exactly one node (a serial plan).
// FLAT — at most two levels (everything runs nearly at once).
// HOURGLASS — an interior level strictly narrower than both ends (a waist).
// FUNNEL — widths never increase with depth (wide start converging).
// MIXED — everything else.
func Silhouette(hist []int) string {
	if len(hist) == 0 {
		return ShapeFlat
	}
	allOnes := true
	for _, w := range hist {
		if w != 1 {
			allOnes = false
			break
		}
	}
	switch {
	case allOnes && len(hist) > 1:
		return ShapeChain
	case len(hist) <= 2:
		return ShapeFlat
	}
	first, last := hist[0], hist[len(hist)-1]
	interiorMin := hist[1]
	for _, w := range hist[1 : len(hist)-1] {
		if w < interiorMin {
			interiorMin = w
		}
	}
	if interiorMin < first && interiorMin < last {
		return ShapeHourglass
	}
	narrowing := true
	for i := 1; i < len(hist); i++ {
		if hist[i] > hist[i-1] {
			narrowing = false
			break
		}
	}
	if narrowing {
		return ShapeFunnel
	}
	return ShapeMixed
}

// sortStrings is sort.Strings, aliased locally so every walk in this package
// funnels through one deterministic-order primitive.
func sortStrings(s []string) { sort.Strings(s) }
