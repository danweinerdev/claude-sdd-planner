package rules

// Exports for the graph compiler (internal/graph/compile).
//
// The compiler must resolve identifiers exactly the way the validator does —
// same related-graph reachability, same definition patterns, same retirement
// handling — or the two tools will disagree about which spec is reachable
// and which ids exist (Plans/SddGraph task 2.3's recorded trap). These
// wrappers are that single opinion, exported deliberately and narrowly:
// resolution and definitions only, no rule evaluation.

import "regexp"

// RelatedIdentifierSources returns every artifact whose identifiers `a` may
// cite: the specs reachable through the `related` graph and the designs on
// that path (the DD namespace owners). It is the resolution SDD122 and the
// SDD160 family use, verbatim.
func RelatedIdentifierSources(r *Root, a *Artifact) []*Artifact {
	return relatedSources(r, a)
}

// DirectRelatedSources resolves ONLY an artifact's own `related` list — no
// transitive hops. The graph compiler scopes its AC-coverage demand to the
// specs a plan directly claims (DD-4: coverage over the plan's OWN
// requirement surface), while citations keep resolving through the full
// transitive walk. Order follows the frontmatter list; unresolvable refs are
// skipped, matching the transitive walk's posture.
func DirectRelatedSources(r *Root, a *Artifact) []*Artifact {
	var out []*Artifact
	related, ok := a.Meta["related"].([]any)
	if !ok {
		return nil
	}
	for _, ref := range related {
		s, ok := ref.(string)
		if !ok {
			continue
		}
		if target := resolveRef(r, s); target != nil {
			out = append(out, target)
		}
	}
	return out
}

// DefinedIdentifiers returns the ids of one family (FR, NFR, AC, DD) that an
// artifact's body defines, using the validator's own definition patterns —
// which are retirement-safe by shape (a struck-through `~~**AC-37**~~` does
// not match). The returned set is a copy.
func DefinedIdentifiers(a *Artifact, family string) map[string]bool {
	out := map[string]bool{}
	for id := range specDefinedIDs(a)[family] {
		out[id] = true
	}
	return out
}

// IdentifierFamilies lists the families DefinedIdentifiers understands, in
// deterministic order.
func IdentifierFamilies() []string {
	return []string{"FR", "NFR", "AC", "DD"}
}

// DefinitionPattern returns the definition regex for one identifier family,
// or nil for an unknown family. Callers use it for SPAN extraction (where an
// item's text starts); the id inventory itself comes from
// DefinedIdentifiers so the two can never disagree.
func DefinitionPattern(family string) *regexp.Regexp {
	return specDefinitionRe[family]
}

// DecisionStatuses returns every decision-ledger entry's id -> status across
// the root, the same population the citation rules consult (first entry wins
// a duplicate id, since SDD032 already flags the duplicate).
func DecisionStatuses(r *Root) map[string]string {
	out := map[string]string{}
	for id, d := range allDecisions(r) {
		out[id] = d.status
	}
	return out
}

// CommentStripped returns an artifact body with HTML comments removed — the
// same preprocessing every citation and definition scan applies, so span
// extraction over the returned text agrees with DefinedIdentifiers.
func CommentStripped(body string) string {
	return noComments(body)
}
