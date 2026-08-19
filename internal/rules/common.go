package rules

import (
	"regexp"
	"sort"
	"strings"
)

// statusValues is the allowed `status:` values per artifact `type:`
// (originally mirroring sdd_validate.py's STATUS). It agrees with the schema
// registry in both directions — every schema-served type appears here with
// the schema's own status enum, and every entry here is backed by a schema —
// TestStatusValuesAgreeWithSchemaRegistry enforces both. A type the tool can
// scaffold (`sdd template <type>`, `apply --create`) but the validator calls
// "unknown" (SDD011) is a registry drift bug (B-4); the retired `retro` and
// `diagram` skills' types were dropped outright rather than kept as
// validator-only legacy entries.
var statusValues = map[string][]string{
	"research":     {"draft", "active", "archived"},
	"brainstorm":   {"draft", "active", "archived"},
	"spec":         {"draft", "review", "approved", "implemented", "superseded"},
	"design":       {"draft", "review", "approved", "implemented", "superseded"},
	"plan":         {"draft", "approved", "active", "complete", "archived"},
	"phase":        {"planned", "in-progress", "complete", "blocked", "deferred"},
	"plan-phase":   {"planned", "in-progress", "complete", "blocked", "deferred"},
	"debrief":      {"draft", "complete"},
	"decision-log": {"active", "archived"},
	"review":       {"open", "resolved", "superseded"},
	"note":         {"draft", "in-progress", "complete"},
	"notes":        {"draft", "in-progress", "complete"},
	"notes-index":  {"draft", "in-progress", "complete"},
	"findings":     {"draft", "in-progress", "complete"},
	"drift-log":    {"draft", "in-progress", "complete"},
	"reference":    {"draft", "active", "archived"},
}

// retiredTypes are artifact types whose skills were cut (the compact-core
// streamline removed /retro and /diagram). Artifacts carrying them are
// ignored outright: discovered for `related:` reference resolution, never
// validated — a legacy artifact nobody can create or edit any more must not
// be an error source either.
var retiredTypes = map[string]bool{"retro": true, "diagram": true}

var taskStatusValues = []string{"blocked", "complete", "deferred", "in-progress", "planned"}

var commonFields = []string{"title", "type", "status", "created", "updated"}

func sortedKinds() []string {
	out := make([]string, 0, len(statusValues))
	for k := range statusValues {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStatuses(kind string) []string {
	out := append([]string{}, statusValues[kind]...)
	sort.Strings(out)
	return out
}

func isEmptyMeta(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return s == ""
	}
	return false
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func init() {
	Register(&Rule{
		Code: "SDD010", Severity: Error, PyFunc: "_common",
		What: "a required top-level field (title/type/status/created/updated) is missing or empty",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			for _, field := range commonFields {
				if isEmptyMeta(a.Meta[field]) {
					emit(Diagnostic{
						Code: "SDD010", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Required field `" + field + "` is missing or empty.",
						Correction: "Add a nonempty `" + field + "` value.",
					})
				}
			}
		},
		Bad: []Example{{Name: "missing-title", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "title: Sample Research\n", "", 1),
		}}},
		Good: []Example{{Name: "all-present", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD011", Severity: Error, PyFunc: "_common",
		What: "`type:` names an unknown artifact kind",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			if _, ok := statusValues[a.Kind()]; ok {
				return
			}
			kind := a.Kind()
			if kind == "" {
				kind = "<missing>"
			}
			emit(Diagnostic{
				Code: "SDD011", Severity: Error, Path: a.Rel, Line: a.Line("type:", false),
				Message:    "Unknown type `" + kind + "`.",
				Correction: "Use one of: " + strings.Join(sortedKinds(), ", ") + ".",
			})
		},
		Bad: []Example{{Name: "bad-type", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "type: research", "type: nonsense", 1),
		}}},
		Good: []Example{{Name: "known-type", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD012", Severity: Error, PyFunc: "_common",
		What: "`status:` is not a value the artifact's `type:` allows",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			if _, ok := statusValues[a.Kind()]; !ok {
				return
			}
			allowed := statusValues[a.Kind()]
			for _, s := range allowed {
				if s == a.Status() {
					return
				}
			}
			status := a.Status()
			if status == "" {
				status = "<missing>"
			}
			emit(Diagnostic{
				Code: "SDD012", Severity: Error, Path: a.Rel, Line: a.Line("status:", false),
				Message:    "Status `" + status + "` is invalid for `" + a.Kind() + "`.",
				Correction: "Use one of: " + strings.Join(sortedStatuses(a.Kind()), ", ") + ".",
			})
		},
		Bad: []Example{{Name: "bad-status", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "status: draft", "status: bogus", 1),
		}}},
		Good: []Example{{Name: "known-status", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD013", Severity: Error, PyFunc: "_common",
		What: "`created`/`updated` is not an ISO YYYY-MM-DD date",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			if _, ok := statusValues[a.Kind()]; !ok {
				return
			}
			for _, field := range [2]string{"created", "updated"} {
				v, ok := a.Meta[field].(string)
				if ok && dateRe.MatchString(v) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD013", Severity: Error, Path: a.Rel, Line: a.Line(field+":", false),
					Message:    "`" + field + "` must be YYYY-MM-DD.",
					Correction: "Set `" + field + "` to an ISO date.",
				})
			}
		},
		Bad: []Example{{Name: "bad-date", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "created: 2024-01-01", "created: Jan 1 2024", 1),
		}}},
		Good: []Example{{Name: "iso-dates", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD014", Severity: Error, PyFunc: "_common",
		What: "`tags`/`related` is present but not a YAML list (phase artifacts are exempt)",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || a.Kind() == "phase" {
				return
			}
			if _, ok := statusValues[a.Kind()]; !ok {
				return
			}
			for _, field := range [2]string{"tags", "related"} {
				if _, ok := a.Meta[field].([]any); ok {
					continue
				}
				emit(Diagnostic{
					Code: "SDD014", Severity: Error, Path: a.Rel, Line: a.Line(field+":", false),
					Message:    "`" + field + "` must be a YAML list.",
					Correction: "Use `" + field + ": []` when empty.",
				})
			}
		},
		Bad: []Example{{Name: "tags-not-list", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "tags: []", "tags: onetag", 1),
		}}},
		Good: []Example{{Name: "lists", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})
}
