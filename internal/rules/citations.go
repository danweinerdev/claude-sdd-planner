package rules

import "regexp"

// Family (h): Validator._citations — SDD120 through SDD122. Decision
// citations (`D-NNNN`) resolve against the whole ledger; FR/NFR/AC citations
// resolve against the specs related (transitively, through plan/design/
// review) to the citing artifact.

var (
	citeDRe   = regexp.MustCompile(`\bD-(\d{4,})\b`)
	citeFRRe  = regexp.MustCompile(`\bFR-(\d{2,})\b`)
	citeNFRRe = regexp.MustCompile(`\bNFR-(\d{2,})\b`)
	citeACRe  = regexp.MustCompile(`\bAC-(\d{2,})\b`)
)

// decisionEntry is one decision-log entry with the fields citation checks need.
type decisionEntry struct {
	id     string
	status string
}

func allDecisions(r *Root) map[string]decisionEntry {
	out := map[string]decisionEntry{}
	for _, a := range r.Artifacts {
		if a.Meta == nil || a.Kind() != "decision-log" {
			continue
		}
		for _, e := range asAnyList(a.Meta["decisions"]) {
			m := planEntry(e)
			if m == nil {
				continue
			}
			id, ok := m["id"].(string)
			if !ok {
				continue
			}
			if _, exists := out[id]; exists {
				continue // SDD032 already flags the duplicate; first wins here.
			}
			out[id] = decisionEntry{id: id, status: metaStr(m, "status")}
		}
	}
	return out
}

// isLiveArtifact mirrors Validator._is_live.
func isLiveArtifact(a *Artifact) bool {
	if a.Kind() == "debrief" || a.Kind() == "retro" {
		return false
	}
	return a.Status() != "archived" && a.Status() != "superseded"
}

// resolveRef mirrors Validator.resolve: a `related`-style reference resolved
// against the root's discovered artifacts by exact path, `<path>/README.md`,
// or `<path>.md`.
func resolveRef(r *Root, ref string) *Artifact {
	if ref == "" {
		return nil
	}
	for _, cand := range []string{ref, ref + "/README.md", ref + ".md"} {
		if a, ok := r.ByPath[cand]; ok {
			return a
		}
	}
	return nil
}

// relatedSpecs mirrors Validator._related_specs: every spec transitively
// reachable from an artifact's own `related` (and a phase's plan, or a
// review's `review_of`), following plan/design/review hops.
func relatedSpecs(r *Root, a *Artifact) []*Artifact {
	seen := map[string]bool{a.Rel: true}
	frontier := []*Artifact{a}
	if a.Kind() == "phase" {
		if plan := metaStr(a.Meta, "plan"); plan != "" {
			if p, ok := r.ByPath["Plans/"+plan+"/README.md"]; ok {
				frontier = append(frontier, p)
				seen[p.Rel] = true
			}
		}
	}
	if a.Kind() == "review" {
		if ref, ok := a.Meta["review_of"].(string); ok {
			if target := resolveRef(r, ref); target != nil {
				frontier = append(frontier, target)
				seen[target.Rel] = true
			}
		}
	}
	result := map[string]*Artifact{}
	for len(frontier) > 0 {
		current := frontier[0]
		frontier = frontier[1:]
		related, ok := current.Meta["related"].([]any)
		if !ok {
			continue
		}
		for _, ref := range related {
			s, ok := ref.(string)
			if !ok {
				continue
			}
			target := resolveRef(r, s)
			if target == nil {
				continue
			}
			switch target.Kind() {
			case "spec":
				result[target.Rel] = target
			case "plan", "design", "review":
				if !seen[target.Rel] {
					seen[target.Rel] = true
					frontier = append(frontier, target)
				}
			}
		}
	}
	out := make([]*Artifact, 0, len(result))
	for _, v := range result {
		out = append(out, v)
	}
	return out
}

// citationBody mirrors _citations' search text: the comment-stripped body
// plus every task's `justifies` (and, unless the task is complete, its
// `verification`) so an id cited only from plan frontmatter is still found.
func citationBody(a *Artifact) string {
	body := noComments(a.Body)
	tasks, ok := a.Meta["tasks"].([]any)
	if !ok {
		return body
	}
	for _, t := range tasks {
		m := planEntry(t)
		if m == nil {
			continue
		}
		if v, ok := m["justifies"].(string); ok {
			body += "\n" + v
		}
		if metaStr(m, "status") != "complete" {
			if v, ok := m["verification"].(string); ok {
				body += "\n" + v
			}
		}
	}
	return body
}

// citationLine mirrors Validator._citation_line: prefer the body location,
// falling back to the frontmatter source when the id is YAML-only.
func citationLine(a *Artifact, text string) int {
	if containsStr(a.Body, text) {
		return a.Line(text, true)
	}
	return a.Line(text, false)
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || indexOf(s, sub) >= 0
}

func init() {
	Register(&Rule{
		Code: "SDD120", Severity: Error, PyFunc: "_citations",
		What: "a `D-NNNN` citation does not resolve to a known decision",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			decisions := allDecisions(r)
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() == "decision-log" {
					continue
				}
				body := citationBody(a)
				for _, m := range citeDRe.FindAllStringSubmatch(body, -1) {
					id := "D-" + m[1]
					if _, ok := decisions[id]; ok {
						continue
					}
					emit(Diagnostic{
						Code: "SDD120", Severity: Error, Path: a.Rel, Line: citationLine(a, id),
						Message:    "Citation `" + id + "` does not resolve.",
						Correction: "Correct it or restore the decision.",
					})
				}
			}
		},
		Bad: []Example{{Name: "unresolved-decision", Files: map[string]string{
			"Research/bad.md": strReplace(validResearch, "## Context\n\nText.", "## Context\n\nSee D-0099."),
		}}},
		Good: []Example{{Name: "resolved-decision", Files: map[string]string{
			"Research/ok.md":         strReplace(validResearch, "## Context\n\nText.", "## Context\n\nSee D-0001."),
			"Decisions/decisions.md": decisionLog("\n  - id: D-0001\n    status: accepted\n    question: Q\n    statement: S\n    scope: []\n"),
		}}},
	})

	Register(&Rule{
		Code: "SDD121", Severity: Error, PyFunc: "_citations",
		What: "a live artifact cites a rejected/superseded decision",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			decisions := allDecisions(r)
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() == "decision-log" || !isLiveArtifact(a) {
					continue
				}
				body := citationBody(a)
				for _, m := range citeDRe.FindAllStringSubmatch(body, -1) {
					id := "D-" + m[1]
					d, ok := decisions[id]
					if !ok || (d.status != "rejected" && d.status != "superseded") {
						continue
					}
					emit(Diagnostic{
						Code: "SDD121", Severity: Error, Path: a.Rel, Line: citationLine(a, id),
						Message:    "Live artifact cites `" + id + "` with status `" + d.status + "`.",
						Correction: "Cite the accepted replacement or reconcile content.",
					})
				}
			}
		},
		Bad: []Example{{Name: "cites-rejected", Files: map[string]string{
			"Research/bad.md":        strReplace(validResearch, "## Context\n\nText.", "## Context\n\nSee D-0001."),
			"Decisions/decisions.md": decisionLog("\n  - id: D-0001\n    status: rejected\n    question: Q\n    statement: S\n    scope: []\n"),
		}}},
		Good: []Example{{Name: "cites-accepted", Files: map[string]string{
			"Research/ok.md":         strReplace(validResearch, "## Context\n\nText.", "## Context\n\nSee D-0001."),
			"Decisions/decisions.md": decisionLog("\n  - id: D-0001\n    status: accepted\n    question: Q\n    statement: S\n    scope: []\n"),
		}}},
	})

	Register(&Rule{
		Code: "SDD122", Severity: Error, PyFunc: "_citations",
		What: "an FR/NFR/AC citation does not resolve in a related spec",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() == "spec" {
					continue
				}
				body := citationBody(a)
				specs := relatedSpecs(r, a)
				for _, family := range []struct {
					name string
					re   *regexp.Regexp
				}{{"FR", citeFRRe}, {"NFR", citeNFRRe}, {"AC", citeACRe}} {
					available := map[string]bool{}
					for _, spec := range specs {
						for id := range specDefinedIDs(spec)[family.name] {
							available[id] = true
						}
					}
					for _, m := range family.re.FindAllStringSubmatch(body, -1) {
						value := family.name + "-" + m[1]
						if available[value] {
							continue
						}
						emit(Diagnostic{
							Code: "SDD122", Severity: Error, Path: a.Rel, Line: citationLine(a, value),
							Message:    "Citation `" + value + "` does not resolve in a related spec.",
							Correction: "Relate the owning spec or correct the citation.",
						})
					}
				}
			}
		},
		Bad: []Example{{Name: "unresolved-fr", Files: map[string]string{
			"Research/bad.md": strReplace(validResearch, "## Context\n\nText.", "## Context\n\nSee FR-99."),
		}}},
		Good: []Example{{Name: "resolved-fr", Files: map[string]string{
			"Research/ok.md": strReplace(
				strReplace(validResearch, "related: []", "related: [Specs/Sample]"),
				"## Context\n\nText.", "## Context\n\nSee FR-01.",
			),
			"Specs/Sample/README.md": validSpecTemplate,
		}}},
	})
}

func strReplace(s, old, new string) string {
	return replaceFirst(s, old, new)
}
