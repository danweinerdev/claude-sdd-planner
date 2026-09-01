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
