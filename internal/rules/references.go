package rules

import "strings"

// Family (c): Validator._references — SDD040/SDD041. Every non-phase
// artifact's `related` list is checked entry-by-entry against the resolver
// Validator.resolve() implements: a path is looked up as-is, as
// `<path>/README.md`, and as `<path>.md`, against the planning root's index
// of successfully parsed artifacts.

// resolveRelated ports Validator.resolve(): a `related` entry resolves only
// when it is a safe, root-relative path (no absolute path, no backslash, no
// `.`/`..` path segment) that names a known artifact directly, as a
// directory's README, or with a `.md` suffix appended.
func resolveRelated(r *Root, reference string) *Artifact {
	value := strings.TrimSpace(reference)
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return nil
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return nil
		}
	}
	for _, candidate := range [3]string{value, value + "/README.md", value + ".md"} {
		if a, ok := r.ByPath[candidate]; ok {
			return a
		}
	}
	return nil
}

func init() {
	Register(&Rule{
		Code: "SDD040", Severity: Error, PyFunc: "_references",
		What: "a `related` entry is not a nonempty string",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil {
				return
			}
			related, ok := a.Meta["related"].([]any)
			if !ok {
				return
			}
			for _, ref := range related {
				s, ok := ref.(string)
				if ok && s != "" {
					continue
				}
				emit(Diagnostic{
					Code: "SDD040", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "A `related` entry is not a nonempty string.",
					Correction: "Use a planning-root-relative artifact path.",
				})
			}
		},
		Bad: []Example{{Name: "empty-entry", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "related: []", "related: [\"\"]", 1),
		}}},
		Good: []Example{{Name: "no-related", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD041", Severity: Error, PyFunc: "_references",
		What: "a `related` path does not resolve to a known artifact",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil {
					continue
				}
				related, ok := a.Meta["related"].([]any)
				if !ok {
					continue
				}
				for _, ref := range related {
					s, ok := ref.(string)
					if !ok || s == "" {
						continue // SDD040's finding
					}
					if resolveRelated(r, s) != nil {
						continue
					}
					emit(Diagnostic{
						Code: "SDD041", Severity: Error, Path: a.Rel, Line: a.Line(s, false),
						Message:    "Related path `" + s + "` does not resolve.",
						Correction: "Point it at an existing artifact directory or Markdown file.",
					})
				}
			}
		},
		Bad: []Example{{Name: "unresolved-path", Files: map[string]string{
			"Research/bad.md": strings.Replace(validResearch, "related: []", "related: [\"Research/nope.md\"]", 1),
		}}},
		Good: []Example{{Name: "resolved-path", Files: map[string]string{
			"Research/ok.md":    strings.Replace(validResearch, "related: []", "related: [\"Research/other.md\"]", 1),
			"Research/other.md": validResearch,
		}}},
	})
}
