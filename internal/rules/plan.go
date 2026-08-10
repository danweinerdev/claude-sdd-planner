package rules

import (
	"path"
)

// Family (e): Validator._plan — SDD052 through SDD060, plus SDD150/151/152
// (a resolved phase doc's type/plan/title agreement with its README entry).

func planEntry(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func metaStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func init() {
	Register(&Rule{
		Code: "SDD052", Severity: Error, PyFunc: "_plan",
		What: "a plan's `phases` frontmatter field is not a list",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "plan" {
				return
			}
			if _, ok := a.Meta["phases"].([]any); ok {
				return
			}
			emit(Diagnostic{
				Code: "SDD052", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "`phases` must be a list.",
				Correction: "Use `phases: []` when empty.",
			})
		},
		Bad: []Example{{Name: "phases-not-list", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw("phases: none\n"),
		}}},
		Good: []Example{{Name: "phases-list", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
		}}},
	})

	Register(&Rule{
		Code: "SDD053", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry is not a mapping",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "plan" {
				return
			}
			phases, ok := a.Meta["phases"].([]any)
			if !ok {
				return
			}
			for _, p := range phases {
				if _, ok := p.(map[string]any); ok {
					continue
				}
				emit(Diagnostic{
					Code: "SDD053", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "A phase entry is not a mapping.",
					Correction: "Add id, title, status, and doc fields.",
				})
			}
		},
		Bad: []Example{{Name: "phase-not-mapping", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw("phases:\n  - onlystring\n"),
		}}},
		Good: []Example{{Name: "phase-mapping", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}, "Plans/Sample/01-One.md"),
		}}},
	})

	Register(&Rule{
		Code: "SDD054", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry is missing id/title/status/doc",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "plan" {
				return
			}
			for _, p := range asAnyList(a.Meta["phases"]) {
				m := planEntry(p)
				if m == nil {
					continue
				}
				for _, field := range []string{"id", "title", "status", "doc"} {
					if isEmptyMeta(m[field]) {
						emit(Diagnostic{
							Code: "SDD054", Severity: Error, Path: a.Rel, Line: 1,
							Message:    "Phase entry is missing `" + field + "`.",
							Correction: "Add `" + field + "` to the entry.",
						})
					}
				}
			}
		},
		Bad: []Example{{Name: "missing-title", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "status": "planned", "doc": "01-One.md",
			}, "Plans/Sample/01-One.md"),
		}}},
		Good: []Example{{Name: "complete-entry", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}, "Plans/Sample/01-One.md"),
		}}},
	})

	Register(&Rule{
		Code: "SDD055", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry has an invalid status",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "plan" {
				return
			}
			for _, p := range asAnyList(a.Meta["phases"]) {
				m := planEntry(p)
				if m == nil {
					continue
				}
				status, _ := m["status"].(string)
				if isAllowedStatus("phase", status) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD055", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Phase `" + metaStr(m, "id") + "` has invalid status `" + status + "`.",
					Correction: "Use an allowed phase status.",
				})
			}
		},
		Bad: []Example{{Name: "bad-status", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "bogus", "doc": "01-One.md",
			}, "Plans/Sample/01-One.md"),
		}}},
		Good: []Example{{Name: "good-status", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}, "Plans/Sample/01-One.md"),
		}}},
	})

	Register(&Rule{
		Code: "SDD056", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry's `doc` does not resolve to a known artifact",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "plan" {
					continue
				}
				for _, p := range asAnyList(a.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					doc, hasDoc := m["doc"].(string)
					id := metaStr(m, "id")
					if hasDoc && doc != "" {
						target := path.Join(path.Dir(a.Rel), doc)
						if _, ok := r.ByPath[target]; ok {
							continue
						}
					}
					docDisplay := doc
					if !hasDoc {
						docDisplay = "None"
					}
					emit(Diagnostic{
						Code: "SDD056", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Phase `" + id + "` doc `" + docDisplay + "` does not resolve.",
						Correction: "Point `doc` at an existing phase file.",
					})
				}
			}
		},
		Bad: []Example{{Name: "doc-missing", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-Missing.md",
			}, ""),
		}}},
		Good: []Example{{Name: "doc-resolves", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "planned"),
		}}},
	})

	Register(&Rule{
		Code: "SDD057", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry id disagrees with its doc's own `phase` field",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "plan" {
					continue
				}
				for _, p := range asAnyList(a.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					doc, _ := m["doc"].(string)
					id := metaStr(m, "id")
					if doc == "" {
						continue
					}
					target, ok := r.ByPath[path.Join(path.Dir(a.Rel), doc)]
					if !ok {
						continue
					}
					got := metaStr(target.Meta, "phase")
					if got == id {
						continue
					}
					emit(Diagnostic{
						Code: "SDD057", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Phase `" + id + "` disagrees with `" + doc + "` id `" + metaOrNone(target.Meta, "phase") + "`.",
						Correction: "Make both ids identical.",
					})
				}
			}
		},
		Bad: []Example{{Name: "id-mismatch", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}, ""),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "2", "One", "planned"),
		}}},
		Good: []Example{{Name: "id-agrees", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}, "Plans/Sample/01-One.md"),
		}}},
	})

	Register(&Rule{
		Code: "SDD058", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry status disagrees with its doc's status",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "plan" {
					continue
				}
				for _, p := range asAnyList(a.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					doc, _ := m["doc"].(string)
					id := metaStr(m, "id")
					if doc == "" {
						continue
					}
					target, ok := r.ByPath[path.Join(path.Dir(a.Rel), doc)]
					if !ok {
						continue
					}
					if target.Status() == metaStr(m, "status") {
						continue
					}
					emit(Diagnostic{
						Code: "SDD058", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Phase `" + id + "` status disagrees with `" + doc + "`.",
						Correction: "Make both statuses identical.",
					})
				}
			}
		},
		Bad: []Example{{Name: "status-mismatch", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}, ""),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "complete"),
		}}},
		Good: []Example{{Name: "status-agrees", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}, "Plans/Sample/01-One.md"),
		}}},
	})

	Register(&Rule{
		Code: "SDD059", Severity: Error, PyFunc: "_plan",
		What: "a complete plan contains a phase entry whose doc is not complete",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "plan" || a.Status() != "complete" {
					continue
				}
				for _, p := range asAnyList(a.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					doc, _ := m["doc"].(string)
					id := metaStr(m, "id")
					if doc == "" {
						continue
					}
					target, ok := r.ByPath[path.Join(path.Dir(a.Rel), doc)]
					if !ok || target.Status() == "complete" {
						continue
					}
					emit(Diagnostic{
						Code: "SDD059", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Complete plan contains incomplete phase `" + id + "`.",
						Correction: "Complete every phase first.",
					})
				}
			}
		},
		Bad: []Example{{Name: "incomplete-phase", Files: map[string]string{
			"Plans/Sample/README.md": planStatus("complete", map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "planned"),
		}}},
		Good: []Example{{Name: "all-complete", Files: map[string]string{
			"Plans/Sample/README.md": planStatus("complete", map[string]string{
				"id": "1", "title": "One", "status": "complete", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "complete"),
		}}},
	})

	Register(&Rule{
		Code: "SDD060", Severity: Error, PyFunc: "_plan",
		What: "a plan declares the same phase id more than once",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() != "plan" {
				return
			}
			var ids []string
			for _, p := range asAnyList(a.Meta["phases"]) {
				m := planEntry(p)
				if m == nil {
					continue
				}
				ids = append(ids, metaStr(m, "id"))
			}
			for _, v := range stringDuplicates(ids) {
				emit(Diagnostic{
					Code: "SDD060", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Duplicate phase id `" + v + "`.",
					Correction: "Assign a unique append-only phase id.",
				})
			}
		},
		Bad: []Example{{Name: "duplicate-id", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
  - id: "1"
    title: Two
    status: planned
    doc: 02-Two.md
`),
		}}},
		Good: []Example{{Name: "unique-ids", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
		}}},
	})
}

func asAnyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func isAllowedStatus(kind, status string) bool {
	for _, s := range statusValues[kind] {
		if s == status {
			return true
		}
	}
	return false
}

// resolvedPhaseEntries walks every plan's phases[] entries whose `doc`
// resolves to a known artifact, yielding the plan, the entry, and the target
// artifact — the shared shape SDD150/151/152 (and SDD057/058/059) check.
func resolvedPhaseEntries(r *Root, visit func(plan *Artifact, entry map[string]any, target *Artifact)) {
	for _, plan := range r.Artifacts {
		if plan.Meta == nil || plan.Kind() != "plan" {
			continue
		}
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
			if !ok {
				continue
			}
			visit(plan, m, target)
		}
	}
}

func init() {
	Register(&Rule{
		Code: "SDD150", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry's `doc` resolves to a non-`phase` artifact",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			resolvedPhaseEntries(r, func(plan *Artifact, m map[string]any, target *Artifact) {
				if target.Kind() == "phase" {
					return
				}
				doc, _ := m["doc"].(string)
				emit(Diagnostic{
					Code: "SDD150", Severity: Error, Path: plan.Rel, Line: 1,
					Message:    "Phase `" + metaStr(m, "id") + "` doc `" + doc + "` has type `" + target.Kind() + "`.",
					Correction: "Point `doc` at a `type: phase` artifact.",
				})
			})
		},
		Bad: []Example{{Name: "doc-not-phase", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "not-a-phase.md",
			}),
			"Plans/Sample/not-a-phase.md": validResearch,
		}}},
		Good: []Example{{Name: "doc-is-phase", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "planned"),
		}}},
	})

	Register(&Rule{
		Code: "SDD151", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry's `doc` belongs to a different plan",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			resolvedPhaseEntries(r, func(plan *Artifact, m map[string]any, target *Artifact) {
				planName := path.Base(path.Dir(plan.Rel))
				got := metaStr(target.Meta, "plan")
				if got == planName {
					return
				}
				doc, _ := m["doc"].(string)
				emit(Diagnostic{
					Code: "SDD151", Severity: Error, Path: plan.Rel, Line: 1,
					Message:    "Phase `" + metaStr(m, "id") + "` doc `" + doc + "` belongs to plan `" + metaOrNone(target.Meta, "plan") + "`.",
					Correction: "Set its `plan` field to `" + planName + "`.",
				})
			})
		},
		Bad: []Example{{Name: "doc-belongs-elsewhere", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("OtherPlan", "1", "One", "planned"),
		}}},
		Good: []Example{{Name: "doc-agrees", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "planned"),
		}}},
	})

	Register(&Rule{
		Code: "SDD152", Severity: Error, PyFunc: "_plan",
		What: "a plan's phase entry's title disagrees with its doc's title",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			resolvedPhaseEntries(r, func(plan *Artifact, m map[string]any, target *Artifact) {
				docTitle, _ := target.Meta["title"].(string)
				entryTitle, _ := m["title"].(string)
				if docTitle == entryTitle {
					return
				}
				doc, _ := m["doc"].(string)
				emit(Diagnostic{
					Code: "SDD152", Severity: Error, Path: plan.Rel, Line: 1,
					Message:    "Phase `" + metaStr(m, "id") + "` title disagrees with `" + doc + "`.",
					Correction: "Make the phase entry and document titles identical.",
				})
			})
		},
		Bad: []Example{{Name: "title-disagrees", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "Different Title", "planned"),
		}}},
		Good: []Example{{Name: "title-agrees", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "planned"),
		}}},
	})
}

// metaOrNone renders a frontmatter value the way Python's f-string
// interpolation of `meta.get(key)` does: an absent or null key becomes the
// literal "None", not an empty string. Messages interpolating a possibly
// missing field must use this, since the oracle compares message text.
//
// (Distinct from phase.go's pyRepr, which mimics Python's repr() quoting.)
func metaOrNone(m map[string]any, key string) string {
	v, present := m[key]
	if !present || v == nil {
		return "None"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return metaStr(m, key)
}
