package rules

// Exports for the graph compiler (internal/graph/compile).
//
// The compiler must resolve identifiers exactly the way the validator does —
// same related-graph reachability, same definition patterns, same retirement
// handling — or the two tools will disagree about which spec is reachable
// and which ids exist (Plans/SddGraph task 2.3's recorded trap). These
// wrappers are that single opinion, exported deliberately and narrowly:
// resolution and definitions only, no rule evaluation.

import (
	"path"
	"regexp"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisions"
)

// RelatedIdentifierSources returns every artifact whose identifiers `a` may
// cite: the specs reachable through the `related` graph and the designs on
// that path (the DD namespace owners). It is the resolution SDD122 and the
// SDD160 family use, verbatim.
func RelatedIdentifierSources(r *Root, a *Artifact) []*Artifact {
	return relatedSources(r, a)
}

// CitationHit is one resolved citation: which reachable source defines the
// id, under which canonical qualifier.
type CitationHit struct {
	// SourceRel is the defining artifact's root-relative path.
	SourceRel string
	// Qualifier is the canonical qualified spelling's prefix — the source's
	// directory ref as it appears in `related` (e.g. "Specs/M1").
	Qualifier string
	// ID is the bare identifier (e.g. "AC-01").
	ID string
	// Kind is the defining artifact's kind ("spec" or "design").
	Kind string
}

// CitationIndex resolves requirement citations — bare (`AC-01`) and
// qualified (`Specs/M1:AC-01`, `M1:AC-01`) — against an artifact's reachable
// identifier sources, with one opinion shared by the graph compiler and the
// validator's traceability rules. A bare id defined by more than one source
// is AMBIGUOUS, never first-wins: silent cross-source matching let one
// spec's citation satisfy another spec's same-numbered requirement, which
// defeats per-spec coverage (DD-4's "the plan's OWN requirement surface").
type CitationIndex struct {
	byKey     map[string]CitationHit
	ambiguous map[string][]string
	defined   map[string]map[string][]string // sourceRel -> family -> ids
	sources   []*Artifact
	// decisions resolves plan-decision citations (`pd-…` and
	// `<Plan>:pd-…`, Designs/PlanDecisions) from the perspective of
	// citingPlan: a bare id resolves in the citing plan's own file first,
	// then in the one other plan that carries it, else it is ambiguous.
	decisions  *decisions.Index
	citingPlan string
	// findings holds frozen review artifacts of the citing plan as
	// citation sources (ReviewDrivenAmendment DD-6): sourceRel -> finding
	// id -> title. An extend node cites the finding that demanded it.
	findings map[string]map[string]string
}

// KindReviewFinding is CitationHit.Kind for a frozen review finding.
const KindReviewFinding = "review"

// KindPlanDecisions is CitationHit.Kind for a plan-decision hit; SourceRel
// is then the decisions file's root-relative path and Qualifier the plan.
const KindPlanDecisions = "decisions"

// BuildCitationIndex walks the artifact's transitive related graph (specs
// and designs) and registers every defined id under three spellings: bare,
// `<dir>:<id>` (e.g. "Specs/M1:AC-01"), and `<basename>:<id>` (e.g.
// "M1:AC-01"). Any spelling claimed by two different (source, id) pairs
// resolves for neither and records qualified suggestions instead.
func BuildCitationIndex(r *Root, a *Artifact) *CitationIndex {
	x := buildCitationIndexFrom(relatedSources(r, a))
	x.decisions = r.DecisionIndex
	x.citingPlan = citingPlanOf(r, a)
	x.registerReviewFindings(r)
	return x
}

// registerReviewFindings adds every frozen, resolved review artifact that
// reviews a document under the citing plan as a citation source: its
// `findings[].id` values resolve bare (when unambiguous) and qualified by
// the artifact's path. Unfrozen artifacts are never sources — a citation
// cannot point at text that can still change.
func (x *CitationIndex) registerReviewFindings(r *Root) {
	if x.citingPlan == "" {
		return
	}
	prefix := "Plans/" + x.citingPlan + "/"
	x.findings = map[string]map[string]string{}
	register := func(key string, hit CitationHit) {
		if prior, taken := x.byKey[key]; taken {
			if prior == hit {
				return
			}
			delete(x.byKey, key)
			x.ambiguous[key] = appendUnique(x.ambiguous[key], prior.Qualifier+":"+prior.ID, hit.Qualifier+":"+hit.ID)
			return
		}
		if others := x.ambiguous[key]; others != nil {
			x.ambiguous[key] = appendUnique(others, hit.Qualifier+":"+hit.ID)
			return
		}
		x.byKey[key] = hit
	}
	for _, a := range r.Artifacts {
		if a.Meta == nil || a.Kind() != "review" {
			continue
		}
		frozen, _ := a.Meta["frozen"].(bool)
		if !frozen || metaStr(a.Meta, "status") != "resolved" {
			continue
		}
		reviewOf, _ := a.Meta["review_of"].(string)
		if !strings.HasPrefix(reviewOf, prefix) {
			continue
		}
		qualifier := SourceQualifier(a.Rel)
		base := path.Base(qualifier)
		per := map[string]string{}
		for _, item := range asAnyList(a.Meta["findings"]) {
			m := planEntry(item)
			if m == nil {
				continue
			}
			id := metaStr(m, "id")
			if id == "" {
				continue
			}
			per[id] = metaStr(m, "title")
			hit := CitationHit{SourceRel: a.Rel, Qualifier: qualifier, ID: id, Kind: KindReviewFinding}
			register(id, hit)
			register(qualifier+":"+id, hit)
			if base != qualifier {
				register(base+":"+id, hit)
			}
		}
		if len(per) > 0 {
			x.findings[a.Rel] = per
		}
	}
	for key := range x.ambiguous {
		sortStrings(x.ambiguous[key])
	}
}

// ReviewFindings returns the frozen review findings this index resolves:
// sourceRel -> finding id -> title (the fingerprintable text).
func (x *CitationIndex) ReviewFindings() map[string]map[string]string { return x.findings }

// citingPlanOf names the plan an artifact cites from: the plan itself, a
// phase's plan, or a review's reviewed plan. "" for artifacts outside any
// plan.
func citingPlanOf(r *Root, a *Artifact) string {
	rel := a.Rel
	if a.Kind() == "phase" {
		if plan := metaStr(a.Meta, "plan"); plan != "" {
			return plan
		}
	}
	if a.Kind() == "review" {
		if ref, ok := a.Meta["review_of"].(string); ok {
			if target := resolveRef(r, ref); target != nil {
				rel = target.Rel
			}
		}
	}
	if strings.HasPrefix(rel, "Plans/") {
		rest := strings.TrimPrefix(rel, "Plans/")
		if i := strings.IndexByte(rest, '/'); i > 0 {
			return rest[:i]
		}
	}
	return ""
}

// PlanDecisionHit renders a located plan decision as a citation hit.
func PlanDecisionHit(l decisions.Located) CitationHit {
	return CitationHit{
		SourceRel: "Plans/" + l.Plan + "/" + l.Plan + "-Decisions.json",
		Qualifier: l.Plan, ID: l.Entry.ID, Kind: KindPlanDecisions,
	}
}

// CitingPlan returns the plan this index resolves bare decision ids for.
func (x *CitationIndex) CitingPlan() string { return x.citingPlan }

// PlanDecisions returns the decision files this index can resolve into.
func (x *CitationIndex) PlanDecisions() []decisions.PlanFile {
	if x.decisions == nil {
		return nil
	}
	return x.decisions.Files()
}

// DecisionSuccessor reports the entry that supersedes a plan-decision id,
// if any plan under the root recorded one.
func (x *CitationIndex) DecisionSuccessor(id string) (decisions.Located, bool) {
	if x.decisions == nil {
		return decisions.Located{}, false
	}
	return x.decisions.SuccessorOf(id)
}

// CitationKey keys one resolved (source, id) pair the way every per-spec
// coverage consumer keys it, so the compiler's AC demand and the
// validator's traceability rules can never disagree on identity.
func CitationKey(sourceRel, id string) string { return sourceRel + "\x00" + id }

// buildCitationIndexFrom indexes an explicit source list. Non-spec/design
// artifacts are skipped; order is preserved.
func buildCitationIndexFrom(sources []*Artifact) *CitationIndex {
	x := &CitationIndex{
		byKey:     map[string]CitationHit{},
		ambiguous: map[string][]string{},
		defined:   map[string]map[string][]string{},
	}
	register := func(key string, hit CitationHit) {
		if prior, taken := x.byKey[key]; taken {
			if prior == hit {
				return
			}
			delete(x.byKey, key)
			x.ambiguous[key] = appendUnique(x.ambiguous[key],
				prior.Qualifier+":"+prior.ID, hit.Qualifier+":"+hit.ID)
			return
		}
		if others := x.ambiguous[key]; others != nil {
			x.ambiguous[key] = appendUnique(others, hit.Qualifier+":"+hit.ID)
			return
		}
		x.byKey[key] = hit
	}
	for _, src := range sources {
		kind := src.Kind()
		if kind != "spec" && kind != "design" {
			continue
		}
		x.sources = append(x.sources, src)
		qualifier := SourceQualifier(src.Rel)
		base := path.Base(qualifier)
		perFamily := map[string][]string{}
		for _, family := range IdentifierFamilies() {
			var ids []string
			for id := range DefinedIdentifiers(src, family) {
				ids = append(ids, id)
				hit := CitationHit{SourceRel: src.Rel, Qualifier: qualifier, ID: id, Kind: kind}
				register(id, hit)
				register(qualifier+":"+id, hit)
				if base != qualifier {
					register(base+":"+id, hit)
				}
			}
			sortStrings(ids)
			perFamily[family] = ids
		}
		x.defined[src.Rel] = perFamily
	}
	for key := range x.ambiguous {
		sortStrings(x.ambiguous[key])
	}
	return x
}

// Resolve returns the hit for a citation spelling, when exactly one source
// claims it.
func (x *CitationIndex) Resolve(citation string) (CitationHit, bool) {
	if hit, ok := x.byKey[citation]; ok {
		return hit, true
	}
	if x.decisions != nil {
		if _, _, isRef := decisions.ParseRef(citation); isRef {
			if l, ok := x.decisions.Resolve(citation, x.citingPlan); ok {
				return PlanDecisionHit(l), true
			}
		}
	}
	return CitationHit{}, false
}

// Ambiguous returns the qualified spellings competing for a citation, or
// nil when the citation is not ambiguous.
func (x *CitationIndex) Ambiguous(citation string) []string {
	if x.decisions != nil {
		if amb := x.decisions.Ambiguous(citation, x.citingPlan); len(amb) > 0 {
			return amb
		}
	}
	return append([]string(nil), x.ambiguous[citation]...)
}

// DefinedBy lists one source's defined ids for a family, sorted.
func (x *CitationIndex) DefinedBy(sourceRel, family string) []string {
	return append([]string(nil), x.defined[sourceRel][family]...)
}

// UnrelatedSource explains a qualified citation that resolves nowhere in
// this index when the qualifier names a real spec or design that the
// citing artifact's `related` graph never reaches and that artifact
// defines the id: the root-relative path of that artifact, or "" when the
// citation is bare, its qualifier names nothing, or the named artifact does
// not define the id. Designs are discovered only through the citing
// artifact's own `related` chain — a spec's back-link to the design that
// realizes it is not a discovery hop — so the usual cause is a plan citing
// `Designs/X:DD-N` without relating `Designs/X` directly.
func (x *CitationIndex) UnrelatedSource(r *Root, citation string) string {
	colon := strings.LastIndex(citation, ":")
	if colon <= 0 || colon == len(citation)-1 {
		return ""
	}
	qualifier, id := citation[:colon], citation[colon+1:]
	family := ""
	for _, f := range IdentifierFamilies() {
		if strings.HasPrefix(id, f+"-") {
			family = f
		}
	}
	if family == "" {
		return ""
	}
	reachable := map[string]bool{}
	for _, src := range x.sources {
		reachable[src.Rel] = true
	}
	var candidates []*Artifact
	if a := resolveRef(r, qualifier); a != nil {
		candidates = append(candidates, a)
	} else {
		for _, a := range r.Artifacts {
			if a.Meta == nil {
				continue
			}
			if kind := a.Kind(); kind != "spec" && kind != "design" {
				continue
			}
			if path.Base(SourceQualifier(a.Rel)) == qualifier {
				candidates = append(candidates, a)
			}
		}
	}
	for _, a := range candidates {
		if kind := a.Kind(); kind != "spec" && kind != "design" {
			continue
		}
		if reachable[a.Rel] {
			continue
		}
		if DefinedIdentifiers(a, family)[id] {
			return a.Rel
		}
	}
	return ""
}

// Sources returns the reachable identifier sources, in walk order.
func (x *CitationIndex) Sources() []*Artifact {
	return append([]*Artifact(nil), x.sources...)
}

// Keys returns every unambiguous citation spelling the index resolves.
func (x *CitationIndex) Keys() []string {
	out := make([]string, 0, len(x.byKey))
	for k := range x.byKey {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// SourceQualifier renders an identifier source's canonical qualifier: its
// directory ref for README-rooted artifacts (e.g. "Specs/M1"), the
// extensionless path otherwise.
func SourceQualifier(rel string) string {
	if path.Base(rel) == "README.md" {
		return path.Dir(rel)
	}
	return strings.TrimSuffix(rel, ".md")
}

func appendUnique(list []string, values ...string) []string {
	seen := map[string]bool{}
	for _, v := range list {
		seen[v] = true
	}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			list = append(list, v)
		}
	}
	return list
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

// CommentStripped returns an artifact body with HTML comments removed — the
// same preprocessing every citation and definition scan applies, so span
// extraction over the returned text agrees with DefinedIdentifiers.
func CommentStripped(body string) string {
	return noComments(body)
}
