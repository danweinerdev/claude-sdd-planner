package rules

import (
	"path"
	"strings"
)

// Family (g cont'd): Validator._phase_ownership — SDD163: a phase document's
// physical location, its own declared `plan` field, and the exactly-one plan
// README that lists it as a `phases[].doc` entry must all name the same plan.

// pyListRepr renders a []string the way Python's str(list) does — used
// verbatim in SDD163's message, which embeds the owning-plans list.
func pyListRepr(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	parts := make([]string, len(items))
	for i, v := range items {
		parts[i] = "'" + v + "'"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func init() {
	Register(&Rule{
		Code: "SDD163", Severity: Error, PyFunc: "_phase_ownership",
		What: "a phase's physical location, declared `plan` field, and owning plan README disagree",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			owners := map[string][]string{}
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "plan" {
					continue
				}
				parts := strings.Split(a.Rel, "/")
				if len(parts) < 2 {
					continue
				}
				planName := parts[len(parts)-2]
				for _, p := range asAnyList(a.Meta["phases"]) {
					m := planEntry(p)
					if m == nil {
						continue
					}
					doc, ok := m["doc"].(string)
					if !ok {
						continue
					}
					target := path.Join(path.Dir(a.Rel), doc)
					owners[target] = append(owners[target], planName)
				}
			}
			for _, phase := range r.Artifacts {
				if phase.Meta == nil || phase.Kind() != "phase" {
					continue
				}
				parts := strings.Split(phase.Rel, "/")
				physicalPlan := ""
				if len(parts) >= 3 && parts[0] == "Plans" {
					physicalPlan = parts[1]
				}
				declaredPlan := metaStr(phase.Meta, "plan")
				listed := owners[phase.Rel]
				if len(listed) == 1 && listed[0] == physicalPlan && declaredPlan == physicalPlan {
					continue
				}
				emit(Diagnostic{
					Code: "SDD163", Severity: Error, Path: phase.Rel, Line: 1,
					Message: "Phase ownership is inconsistent: path plan `" + physicalPlan +
						"`, declared plan `" + declaredPlan + "`, listed by " + pyListRepr(listed) + ".",
					Correction: "Place the phase under its owning plan, set the matching `plan` field, and list it exactly once in that plan README.",
				})
			}
		},
		Bad: []Example{{Name: "unlisted-phase", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "planned"),
		}}},
		Good: []Example{{Name: "listed-once", Files: map[string]string{
			"Plans/Sample/README.md": planWithPhase(map[string]string{
				"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
			}),
			"Plans/Sample/01-One.md": phaseDoc("Sample", "1", "One", "planned"),
		}}},
	})
}
