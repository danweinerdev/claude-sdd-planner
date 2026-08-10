package rules

import (
	"path"
)

// Family (g): Validator._graphs — SDD130 through SDD135, plus SDD136/SDD137
// from the shared `_deps` helper it calls for every phase and task entry.

func depsOf(entry map[string]any) []string {
	v, ok := entry["depends_on"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, d := range v {
		if s, ok := d.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// depsMalformed reports whether an entry's `depends_on` (if present) is not a
// list (SDD136), or is a list containing a non-scalar element (SDD137).
//
// Python's test is `isinstance(item, (str, int))`, and the frontmatter is now
// decoded by a real YAML parser, so a nested list or mapping arrives as []any
// or map[string]any and the type check answers this directly.
//
// An earlier version additionally flagged any element containing "[]{}"
// characters. That was a workaround for the hand-rolled parser this package
// used to depend on, which collapsed everything inside a flow list to strings
// and left bracket characters as the only surviving trace of nesting. Against
// a real parser the heuristic is not merely redundant but wrong: it made a
// legitimate id like "nested[bad]" — a string to YAML and to Python — report
// SDD137 that Python never emits.
func depsMalformed(entry map[string]any) (notList, nonScalar bool) {
	v, present := entry["depends_on"]
	if !present || v == nil {
		return false, false
	}
	list, ok := v.([]any)
	if !ok {
		return true, false
	}
	for _, d := range list {
		switch d.(type) {
		case string, int, int64, float64, bool:
			// Scalars. YAML integers decode through the node walker as
			// strings, so `- 1` is a string here and passes either way.
		default:
			nonScalar = true
		}
	}
	return false, nonScalar
}

// findCyclesGeneric ports sdd_validate.py's cycles(): every distinct cycle in
// a directed graph, as the ordered list of node ids that closes it, returned
// in a canonical (lexicographically minimal rotation) and sorted order.
func findCyclesGeneric(graph map[string][]string) [][]string {
	state := map[string]int{}
	var stack []string
	found := map[string][]string{}

	var visit func(node string)
	visit = func(node string) {
		state[node] = 1
		stack = append(stack, node)
		for _, neighbor := range graph[node] {
			if _, ok := graph[neighbor]; !ok {
				continue
			}
			switch state[neighbor] {
			case 0:
				visit(neighbor)
			case 1:
				start := -1
				for i, n := range stack {
					if n == neighbor {
						start = i
						break
					}
				}
				body := append([]string{}, stack[start:]...)
				best := minRotation(body)
				found[joinKey(best)] = best
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
	}

	var order []string
	for node := range graph {
		order = append(order, node)
	}
	sortStrings(order)
	for _, node := range order {
		if state[node] == 0 {
			visit(node)
		}
	}

	var keys []string
	for k := range found {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := make([][]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, found[k])
	}
	return out
}

func minRotation(body []string) []string {
	best := append([]string{}, body...)
	best = append(best, body[0])
	for i := 1; i < len(body); i++ {
		cand := append([]string{}, body[i:]...)
		cand = append(cand, body[:i]...)
		cand = append(cand, body[i])
		if joinKey(cand) < joinKey(best) {
			best = cand
		}
	}
	return best
}

func joinKey(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\x00"
		}
		out += s
	}
	return out
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

func init() {
	Register(&Rule{
		Code: "SDD130", Severity: Error, PyFunc: "_graphs",
		What: "a plan phase's `depends_on` names a phase not in the plan",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				phases := asAnyList(plan.Meta["phases"])
				ids := map[string]bool{}
				for _, p := range phases {
					if m := planEntry(p); m != nil {
						ids[metaStr(m, "id")] = true
					}
				}
				for _, p := range phases {
					m := planEntry(p)
					if m == nil {
						continue
					}
					id := metaStr(m, "id")
					for _, dep := range depsOf(m) {
						if !ids[dep] {
							emit(Diagnostic{
								Code: "SDD130", Severity: Error, Path: plan.Rel, Line: 1,
								Message:    "Phase `" + id + "` depends on unknown `" + dep + "`.",
								Correction: "Reference a phase in this plan.",
							})
						}
					}
				}
			}
		},
		Bad: []Example{{Name: "unknown-phase-dep", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
    depends_on: ["9"]
`),
		}}},
		Good: []Example{{Name: "known-phase-dep", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
  - id: "2"
    title: Two
    status: planned
    doc: 02-Two.md
    depends_on: ["1"]
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD131", Severity: Error, PyFunc: "_graphs",
		What: "a plan phase's `depends_on` includes itself",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				for _, p := range asAnyList(plan.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					id := metaStr(m, "id")
					for _, dep := range depsOf(m) {
						if dep == id {
							emit(Diagnostic{
								Code: "SDD131", Severity: Error, Path: plan.Rel, Line: 1,
								Message:    "Phase `" + id + "` depends on itself.",
								Correction: "Remove the self-dependency.",
							})
						}
					}
				}
			}
		},
		Bad: []Example{{Name: "self-dep", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
    depends_on: ["1"]
`),
		}}},
		Good: []Example{{Name: "no-self-dep", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
		}}},
	})

	Register(&Rule{
		Code: "SDD132", Severity: Error, PyFunc: "_graphs",
		What: "a plan's phase dependency graph has a cycle",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				graph := map[string][]string{}
				for _, p := range asAnyList(plan.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					graph[metaStr(m, "id")] = depsOf(m)
				}
				for _, cycle := range findCyclesGeneric(graph) {
					emit(Diagnostic{
						Code: "SDD132", Severity: Error, Path: plan.Rel, Line: 1,
						Message:    "Phase dependency cycle: " + joinArrow(cycle) + ".",
						Correction: "Make the graph acyclic.",
					})
				}
			}
		},
		Bad: []Example{{Name: "phase-cycle", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
    depends_on: ["2"]
  - id: "2"
    title: Two
    status: planned
    doc: 02-Two.md
    depends_on: ["1"]
`),
		}}},
		Good: []Example{{Name: "acyclic", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
  - id: "2"
    title: Two
    status: planned
    doc: 02-Two.md
    depends_on: ["1"]
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD133", Severity: Error, PyFunc: "_graphs",
		What: "a task's `depends_on` names a task not in the plan",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				planTasks, taskPhase := planTaskIndex(r, plan)
				for taskID, task := range planTasks {
					phase := taskPhase[taskID]
					for _, dep := range depsOf(task) {
						if _, ok := planTasks[dep]; !ok {
							emit(Diagnostic{
								Code: "SDD133", Severity: Error, Path: phase.Rel, Line: 1,
								Message:    "Task `" + taskID + "` depends on unknown `" + dep + "`.",
								Correction: "Reference a task in this plan.",
							})
						}
					}
				}
			}
		},
		Bad: []Example{{Name: "unknown-task-dep", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "Sample Phase", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
    depends_on: ["9.9"]
`),
		}}},
		Good: []Example{{Name: "known-task-dep", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "Sample Phase", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
  - id: "1.2"
    title: Second
    status: planned
    verification: x
    justifies: FR-01
    depends_on: ["1.1"]
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD134", Severity: Error, PyFunc: "_graphs",
		What: "a task's `depends_on` includes itself",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				planTasks, taskPhase := planTaskIndex(r, plan)
				for taskID, task := range planTasks {
					phase := taskPhase[taskID]
					for _, dep := range depsOf(task) {
						if dep == taskID {
							emit(Diagnostic{
								Code: "SDD134", Severity: Error, Path: phase.Rel, Line: 1,
								Message:    "Task `" + taskID + "` depends on itself.",
								Correction: "Remove the self-dependency.",
							})
						}
					}
				}
			}
		},
		Bad: []Example{{Name: "self-task-dep", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "Sample Phase", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
    depends_on: ["1.1"]
`),
		}}},
		Good: []Example{{Name: "no-self-task-dep", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "Sample Phase", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD135", Severity: Error, PyFunc: "_graphs",
		What: "a plan's task dependency graph has a cycle",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				planTasks, taskPhase := planTaskIndex(r, plan)
				graph := map[string][]string{}
				for taskID, task := range planTasks {
					graph[taskID] = depsOf(task)
				}
				for _, cycle := range findCyclesGeneric(graph) {
					phase := taskPhase[cycle[0]]
					emit(Diagnostic{
						Code: "SDD135", Severity: Error, Path: phase.Rel, Line: 1,
						Message:    "Task dependency cycle: " + joinArrow(cycle) + ".",
						Correction: "Make the graph acyclic.",
					})
				}
			}
		},
		Bad: []Example{{Name: "task-cycle", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "Sample Phase", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
    depends_on: ["1.2"]
  - id: "1.2"
    title: Second
    status: planned
    verification: x
    justifies: FR-01
    depends_on: ["1.1"]
`),
		}}},
		Good: []Example{{Name: "acyclic-tasks", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "Sample Phase", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
  - id: "1.2"
    title: Second
    status: planned
    verification: x
    justifies: FR-01
    depends_on: ["1.1"]
`),
		}}},
	})
}

func init() {
	Register(&Rule{
		Code: "SDD136", Severity: Error, PyFunc: "_deps",
		What: "a phase or task entry's `depends_on` is present but not a list",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				for _, p := range asAnyList(plan.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					if notList, _ := depsMalformed(m); notList {
						emit(Diagnostic{
							Code: "SDD136", Severity: Error, Path: plan.Rel, Line: 1,
							Message:    "`depends_on` for phase `" + metaStr(m, "id") + "` is not a list.",
							Correction: "Use a YAML list or omit it.",
						})
					}
				}
				tasks, owner := planTaskIndex(r, plan)
				for taskID, task := range tasks {
					if notList, _ := depsMalformed(task); notList {
						phase := owner[taskID]
						emit(Diagnostic{
							Code: "SDD136", Severity: Error, Path: phase.Rel, Line: 1,
							Message:    "`depends_on` for task `" + taskID + "` is not a list.",
							Correction: "Use a YAML list or omit it.",
						})
					}
				}
			}
		},
		Bad: []Example{{Name: "depends-on-scalar", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
    depends_on: notalist
`),
		}}},
		Good: []Example{{Name: "depends-on-list", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
		}}},
	})

	Register(&Rule{
		Code: "SDD137", Severity: Error, PyFunc: "_deps",
		What: "a phase or task entry's `depends_on` list contains a non-scalar element",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Meta == nil || plan.Kind() != "plan" {
					continue
				}
				for _, p := range asAnyList(plan.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					if _, nonScalar := depsMalformed(m); nonScalar {
						emit(Diagnostic{
							Code: "SDD137", Severity: Error, Path: plan.Rel, Line: 1,
							Message:    "`depends_on` for phase `" + metaStr(m, "id") + "` contains a non-scalar.",
							Correction: "Use only ids.",
						})
					}
				}
				tasks, owner := planTaskIndex(r, plan)
				for taskID, task := range tasks {
					if _, nonScalar := depsMalformed(task); nonScalar {
						phase := owner[taskID]
						emit(Diagnostic{
							Code: "SDD137", Severity: Error, Path: phase.Rel, Line: 1,
							Message:    "`depends_on` for task `" + taskID + "` contains a non-scalar.",
							Correction: "Use only ids.",
						})
					}
				}
			}
		},
		// A genuinely nested element. It has to be real nesting, not an id that
		// merely contains bracket characters: "nested[bad]" is a plain string
		// to YAML, so Python does not flag it and neither should this.
		Bad: []Example{{Name: "depends-on-nested", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
    depends_on: [["nested"]]
`),
		}}},
		Good: []Example{{Name: "depends-on-scalars", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
		}}},
	})
}

// planTaskIndex collects every task declared by a plan's phase docs, keyed by
// task id, alongside the phase Artifact that declared it.
func planTaskIndex(r *Root, plan *Artifact) (map[string]map[string]any, map[string]*Artifact) {
	tasks := map[string]map[string]any{}
	owner := map[string]*Artifact{}
	for _, p := range asAnyList(plan.Meta["phases"]) {
		m := planEntry(p)
		if m == nil {
			continue
		}
		doc, _ := m["doc"].(string)
		if doc == "" {
			continue
		}
		target, ok := r.ByPath[path.Join(path.Dir(plan.Rel), doc)]
		if !ok || target.Meta == nil {
			continue
		}
		for _, t := range asAnyList(target.Meta["tasks"]) {
			tm := planEntry(t)
			if tm == nil {
				continue
			}
			id, ok := tm["id"].(string)
			if !ok {
				continue
			}
			tasks[id] = tm
			owner[id] = target
		}
	}
	return tasks, owner
}

func joinArrow(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += " -> "
		}
		out += s
	}
	return out
}
