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
	// Design Decisions are numbered from 1 without zero-padding (`DD-9`), so
	// unlike the spec families this accepts a single digit, plus the `DD-6a`
	// sub-decision suffix that appears in practice.
	citeDDRe = regexp.MustCompile(`\bDD-(\d{1,4}[a-z]?)\b`)
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
//
// It is relatedSources filtered to spec artifacts — the traceability rules
// (SDD160/161) walk each related SPEC's requirements and must not treat a
// design as one.
func relatedSpecs(r *Root, a *Artifact) []*Artifact {
	var out []*Artifact
	for _, s := range relatedSources(r, a) {
		if s.Kind() == "spec" {
			out = append(out, s)
		}
	}
	return out
}

// relatedSources returns every artifact whose identifiers `a` may cite: the
// specs reachable through the `related` graph, and the designs on that path,
// which own the DD namespace their Design Decisions define.
func relatedSources(r *Root, a *Artifact) []*Artifact {
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
			case "design":
				// A design is BOTH a source and a hop: it owns the DD
				// namespace its Design Decisions define, and it also links on
				// to the specs it realizes. Collecting it here is what makes a
				// plan's `DD-9` resolvable; continuing to traverse it keeps
				// the plan → design → spec path for FR/NFR/AC intact.
				result[target.Rel] = target
				if !seen[target.Rel] {
					seen[target.Rel] = true
					frontier = append(frontier, target)
				}
			case "plan", "review":
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
		What: "an FR/NFR/AC/DD citation does not resolve in a related spec or design",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				if a.Meta == nil {
					continue
				}
				body := citationBody(a)
				sources := relatedSources(r, a)
				for _, family := range []struct {
					name  string
					re    *regexp.Regexp
					owner string // the artifact kind that defines this family
				}{
					{"FR", citeFRRe, "spec"},
					{"NFR", citeNFRRe, "spec"},
					{"AC", citeACRe, "spec"},
					{"DD", citeDDRe, "design"},
				} {
					// An artifact never cites its own namespace: a spec's
					// FR-01 is a definition, and a design's DD-1 likewise.
					if a.Kind() == family.owner {
						continue
					}
					available := map[string]bool{}
					// The artifact's own definitions count for a family it
					// does not own — a design realizing FR-02 defines DD ids,
					// not FR ids, so its FR citations still resolve outward.
					for _, src := range sources {
						if src.Kind() != family.owner {
							continue
						}
						for id := range specDefinedIDs(src)[family.name] {
							available[id] = true
						}
					}
					for _, idx := range family.re.FindAllStringSubmatchIndex(body, -1) {
						value := family.name + "-" + body[idx[2]:idx[3]]
						if available[value] {
							continue
						}
						// A citation qualified by another artifact —
						// `ArkBootstrapApi DD-4` or `ArkBootstrapApi:DD-4` —
						// names an identifier that artifact owns, and is
						// deliberately not resolved here. This mirrors the
						// compiler's SPK040 exemption; without it, referring
						// to a neighbouring design's decision is
						// indistinguishable from a dangling local citation.
						if qualifiedCitation(body, idx[0]) {
							continue
						}
						emit(Diagnostic{
							Code: "SDD122", Severity: Error, Path: a.Rel, Line: citationLine(a, value),
							Message:    "Citation `" + value + "` does not resolve in a related " + family.owner + ".",
							Correction: "Relate the owning " + family.owner + " or correct the citation.",
						})
					}
				}
			}
		},
		Bad: []Example{
			{Name: "unresolved-fr", Files: map[string]string{
				"Research/bad.md": strReplace(validResearch, "## Context\n\nText.", "## Context\n\nSee FR-99."),
			}},
			{Name: "unresolved-dd", Files: map[string]string{
				"Research/bad-dd.md": strReplace(
					strReplace(validResearch, "related: []", "related: [Designs/Sample]"),
					"## Context\n\nText.", "## Context\n\nSee DD-9.",
				),
				"Designs/Sample/README.md": designWithDecisions("### DD-1 — Only decision\n"),
			}},
		},
		Good: []Example{
			{Name: "resolved-fr", Files: map[string]string{
				"Research/ok.md": strReplace(
					strReplace(validResearch, "related: []", "related: [Specs/Sample]"),
					"## Context\n\nText.", "## Context\n\nSee FR-01.",
				),
				"Specs/Sample/README.md": validSpecTemplate,
			}},
			{Name: "resolved-dd", Files: map[string]string{
				"Research/ok-dd.md": strReplace(
					strReplace(validResearch, "related: []", "related: [Designs/Sample]"),
					"## Context\n\nText.", "## Context\n\nSee DD-1.",
				),
				"Designs/Sample/README.md": designWithDecisions("### DD-1 — Only decision\n"),
			}},
		},
	})
}

func strReplace(s, old, new string) string {
	return replaceFirst(s, old, new)
}

// qualifiedCitation reports whether the identifier starting at `start` is
// preceded by an artifact qualifier — `ArkBootstrapApi:DD-4`. A qualified
// citation names an identifier owned by the named artifact and so is not
// resolved against the citing artifact's own related graph.
//
// Only the colon form counts. A bare space (`ReleaseControlService FR-08`)
// is ambiguous with ordinary prose ending in a capitalized word, and
// honoring it silently excused real dangling citations the validator had
// been catching. Requiring the colon keeps every unqualified dangling id
// failing, which is the point of the rule.
func qualifiedCitation(text string, start int) bool {
	i := start
	// Step back over exactly one separator: a colon or a single space.
	if i == 0 {
		return false
	}
	switch text[i-1] {
	case ':', ' ':
		i--
	default:
		return false
	}
	end := i
	for i > 0 && isQualifierChar(text[i-1]) {
		i--
	}
	if i == end {
		return false
	}
	word := text[i:end]
	// Only the COLON form qualifies. The space-separated form
	// (`ReleaseControlService FR-08`) is too weak to act on: it is
	// indistinguishable from prose that happens to end in a capitalized word
	// before a citation, and honoring it silently excused real dangling
	// citations that the validator had been reporting correctly. Authors who
	// mean an external reference write `ReleaseControlService:FR-08`, which
	// is what the templates and skills now emit.
	if text[end] != ':' {
		return false
	}
	return word[0] >= 'A' && word[0] <= 'Z'
}

func isQualifierChar(c byte) bool {
	return c == '_' || c == '-' || c == '.' ||
		('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}
