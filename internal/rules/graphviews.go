package rules

import (
	"regexp"
	"strings"
)

// Graph-projection markers (Plans/SddGraph 5.6). These strings are the
// SOURCE OF TRUTH for the markers `internal/graph/compile`'s renderer emits
// — the renderer consumes these constants (rules cannot import compile;
// compile already imports rules), so the validator's recognition and the
// renderer's emission can never drift apart. They must stay byte-identical
// across releases: existing rendered views carry the old bytes, and a
// changed marker would silently strip their exemptions.
const (
	// GeneratedViewMarkerPrefix opens the per-document marker every
	// rendered view carries. The full marker names the plan
	// ("<prefix><Plan>-Graph.json. Regenerate with ..."); recognition keys
	// on the prefix alone so it is plan-name-independent.
	GeneratedViewMarkerPrefix = "<!-- GENERATED VIEW — source of truth: "

	// GraphViewBegin/GraphViewEnd delimit the generated Graph View section
	// compile upserts into a plan README. Everything between them —
	// markers inclusive — is a projection of the committed graph, not plan
	// intent: lifecycle normalization strips it, so a frozen phase
	// review's README pin survives the upsert.
	GraphViewBegin = "<!-- graph-view:begin — generated section, do not edit -->"
	GraphViewEnd   = "<!-- graph-view:end -->"
)

// IsGeneratedView reports whether an artifact's source carries the rendered
// view marker — the recognition SDD163 keys its exemption on: a projection
// is owned by the committed graph, never by the README phases[] array.
func IsGeneratedView(source string) bool {
	return strings.Contains(source, GeneratedViewMarkerPrefix)
}

// stripGraphViewSection removes the marker-delimited generated section from
// a plan README body, symmetric by construction: applied to BOTH sides of a
// lifecycle comparison, and followed by unconditional trailing-newline
// normalization so a README from before the upsert and the same README
// after it normalize identically.
func stripGraphViewSection(body string) string {
	if begin := strings.Index(body, GraphViewBegin); begin >= 0 {
		if end := strings.Index(body[begin:], GraphViewEnd); end >= 0 {
			body = body[:begin] + body[begin+end+len(GraphViewEnd):]
		}
	}
	body = tripleNewlineRe.ReplaceAllString(body, "\n\n")
	return strings.TrimRight(body, "\n") + "\n"
}

var tripleNewlineRe = regexp.MustCompile(`\n{3,}`)
