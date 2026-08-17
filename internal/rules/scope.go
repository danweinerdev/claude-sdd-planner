package rules

import (
	"path"
	"strings"
)

// ScopeToPlan returns a shallow copy of r restricted to the artifacts that
// can influence a lifecycle-transition gate inside one plan: everything in
// the root EXCEPT other plans' non-README artifacts (their phase docs,
// reviews, and any other files under Plans/<other>/).
//
// Why this is sound — and why it is exactly this shape.
//
// A transition gate (cmd/sdd's gateDiagnostics) validates the root twice,
// before and after the candidate status flip, and refuses only on the
// diagnostics the flip INTRODUCES. Scoping distorts what the rules see, but
// any distortion that is identical in both runs cancels in that diff — a
// foreign plan's findings, spurious unresolved references to dropped
// artifacts, a shrunken review task-index — none of it can change the gate.
// The only way scoping can change the verdict is through a rule whose output
// RESPONDS TO THE FLIP while reading artifacts the scope dropped.
//
// A full audit of the registry (every CheckRoot, classified by anchor,
// inputs, and status-sensitivity) found the flip-sensitive rules to be:
//
//   - completion evidence (SDD070-075, SDD169, SDD171, SDD172): the flipped
//     doc itself + planning-config repo mapping + the target repository.
//   - phase/plan rollups and consistency (SDD055, SDD058, SDD059, SDD065,
//     SDD067-069, SDD157, SDD158): the plan's own directory.
//   - phase-completion reviews (SDD166-168, SDD170, SDD173-175): the plan's
//     directory, `related`/`review_of` targets, config, and the repository.
//   - identifier-section gating (SDD153) and status grammar (SDD012): the
//     artifact itself.
//   - traceability (SDD160-162): the plan's directory plus the specs and
//     designs transitively reachable over `related` chains.
//   - citations (SDD120-122): completing a task removes its `verification`
//     text from the citation body, so resolution can change — against the
//     ledger and the transitive `related` spec closure.
//
// Every one of those inputs survives this scope: the plan's own directory is
// kept whole, and specs, designs, research, brainstorms, decision logs, and
// standalone reviews (Retro/) are all outside Plans/<other>/ and kept. The
// `related` closure and the citation/traceability spec resolution can hop
// THROUGH another plan's README (relatedSpecs follows plan hops), and
// SDD163's ownership map and SDD096/098's plan-name resolution are built
// from plan READMEs — which is why foreign READMEs are kept rather than the
// whole foreign directory dropped.
//
// What the scope drops — foreign phase docs and reviews — is precisely what
// makes full-root gating slow: every completed task in every other plan
// re-verified against the repository (revision existence, ancestry,
// committed copies), twice. Those rules are not flip-sensitive for THIS
// plan's transition, so their findings cancel; only their cost was real.
//
// The trade this makes deliberately: `sdd validate` remains the full-root
// truth, and a transition gate is allowed to see a strict subset as long as
// the introduced-diagnostics diff is identical. When adding a rule that
// makes a status flip in one plan produce findings that depend on OTHER
// plans' phase docs, this scope must learn about it — say so in the rule and
// extend the filter.
func ScopeToPlan(r *Root, planRel string) *Root {
	planPrefix := strings.TrimSuffix(planRel, "/") + "/"
	scoped := &Root{
		Dir:               r.Dir,
		RepoRoot:          r.RepoRoot,
		PlanRepos:         r.PlanRepos,
		ConfigDiagnostics: r.ConfigDiagnostics,
		ByPath:            map[string]*Artifact{},
	}
	for _, a := range r.Artifacts {
		if !keepForPlanScope(a.Rel, planPrefix) {
			continue
		}
		scoped.Artifacts = append(scoped.Artifacts, a)
	}
	for rel, a := range r.ByPath {
		if keepForPlanScope(rel, planPrefix) {
			scoped.ByPath[rel] = a
		}
	}
	return scoped
}

// keepForPlanScope keeps everything outside Plans/, the scoped plan's whole
// directory, and other plans' README.md files (ownership, citations, and
// `related` chains read plan READMEs; nothing flip-sensitive reads a foreign
// phase doc).
func keepForPlanScope(rel, planPrefix string) bool {
	if !strings.HasPrefix(rel, "Plans/") {
		return true
	}
	if strings.HasPrefix(rel, planPrefix) {
		return true
	}
	return path.Base(rel) == "README.md" && strings.Count(rel, "/") == 2
}

// PlanRelOf returns the "Plans/<Name>" prefix owning a planning-root-relative
// artifact path, or "" when the path is not inside a plan directory — the
// caller should then validate unscoped.
func PlanRelOf(rel string) string {
	parts := strings.SplitN(path.Clean(strings.ReplaceAll(rel, "\\", "/")), "/", 3)
	if len(parts) < 3 || parts[0] != "Plans" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// ScopeToDoc returns a shallow copy of r restricted to what a SPEC or DESIGN
// status transition can influence: everything outside Plans/, plus plan
// READMEs, minus other plans' phase docs and reviews.
//
// The soundness argument is ScopeToPlan's, and a doc flip is strictly more
// contained than a plan's. Flipping a spec or design's status changes:
//
//   - SDD012 status grammar and SDD153 open-question gating, on the artifact
//     itself;
//   - SDD178/SDD179 supersession, which read the artifact and its `related`
//     targets — specs, designs, and plan READMEs, all kept;
//   - SDD121 (a live artifact citing a retired decision) and the citation
//     family, which read the ledger and the `related` closure, all kept;
//   - SDD160-162 traceability, anchored on PLANS: a plan README is kept, and
//     the rules read the specs and designs it reaches, which are kept.
//
// Nothing about a spec or design's status makes another plan's PHASE DOC
// produce a different finding — phase docs carry completion evidence, which
// responds to task/phase/plan status, not to a spec's. Those findings are
// identical in both gate runs and cancel in the diff.
//
// This matters because docs live outside Plans/, so PlanRelOf returned "" for
// them and their transitions validated the entire root twice, unscoped: a
// `design submit` on a 224-artifact root took 17.8s — long enough to look
// like a hang and to exceed a caller's timeout.
func ScopeToDoc(r *Root, docRel string) *Root {
	scoped := &Root{
		Dir:               r.Dir,
		RepoRoot:          r.RepoRoot,
		PlanRepos:         r.PlanRepos,
		ConfigDiagnostics: r.ConfigDiagnostics,
		ByPath:            map[string]*Artifact{},
	}
	for _, a := range r.Artifacts {
		if keepForDocScope(a.Rel) {
			scoped.Artifacts = append(scoped.Artifacts, a)
		}
	}
	for rel, a := range r.ByPath {
		if keepForDocScope(rel) {
			scoped.ByPath[rel] = a
		}
	}
	return scoped
}

// keepForDocScope keeps everything outside Plans/ plus plan READMEs. Foreign
// phase docs and reviews — the expensive, repository-verifying half — are
// dropped, because no spec/design status flip changes what they report.
func keepForDocScope(rel string) bool {
	if !strings.HasPrefix(rel, "Plans/") {
		return true
	}
	return path.Base(rel) == "README.md" && strings.Count(rel, "/") == 2
}
