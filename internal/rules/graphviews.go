package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
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

// taskNodeID maps a v1 task id to its graph node id — the convert naming:
// `2.3` -> `task-2-3`. Stable and reversible, byte-identical to
// internal/graph/convert's nodeID (rules cannot import convert: compile and
// convert import rules, so the mapping is repeated here instead). reviews.go's
// SDD096 and appendonly.go's graph-conversion exemption both use it, so the
// validator's recognition and the converter's emission can never drift.
func taskNodeID(taskID string) string {
	return "task-" + strings.ReplaceAll(taskID, ".", "-")
}

// statGraphFile is the os.Stat indirection isGraphPlan queries — a seam so
// tests can inject an operational stat failure (permission denied, a
// transient FS error) without a real unreadable directory. Production
// behavior is unchanged: it is os.Stat.
var statGraphFile = os.Stat

// isGraphPlanDir reports whether dir (a plan directory) carries a committed
// `<Plan>-Graph.json` beside its README, memoized on r so the underlying
// os.Stat runs at most once per plan directory per Root's lifetime — the
// same repoCache convention root.go documents: one validation pass asks
// this of the plan README and every one of its phase docs, over and over.
//
// A stat failure that IS not-exist means "no graph": the ordinary, expected
// answer for a v1 plan. Any OTHER stat failure (permission denied, a
// transient FS fault) is an inability to answer the predicate, not evidence
// of absence — it is recorded on r's operational-failure collector via
// recordFailure, the same path Repo() uses, and reported back to the caller
// so it can refuse to treat the plan as v1 rather than silently doing so.
func isGraphPlanDir(r *Root, dir string) (bool, error) {
	r.graphPlanMu.Lock()
	defer r.graphPlanMu.Unlock()
	if v, ok := r.graphPlanCache[dir]; ok {
		return v, nil
	}
	_, err := statGraphFile(filepath.Join(dir, filepath.Base(dir)+"-Graph.json"))
	if err != nil && !os.IsNotExist(err) {
		opErr := fmt.Errorf("%w: stat %s: %v", vcs.ErrOperational, dir, err)
		r.recordFailure(opErr)
		return false, opErr
	}
	graphPlan := err == nil
	if r.graphPlanCache == nil {
		r.graphPlanCache = map[string]bool{}
	}
	r.graphPlanCache[dir] = graphPlan
	return graphPlan, nil
}

// isGraphPlan reports whether a is exempt from the v1 completion-evidence
// rules because it is part of a graph plan's derived-closure protocol
// (CLAUDE.md "Completion Evidence": "states derive from observations ...,
// completion is sync-only ..., review gates green only from frozen Aligned
// review artifacts, and closure is a derived predicate").
//
// The exemption is scoped PER DOCUMENT, not per directory (review-execution
// finding F-02): a plan README is exempt whenever its directory carries the
// committed graph — the README's own Graph View section is itself a
// projection, and SDD059/070/158's README-level checks have no other way to
// learn the plan is graph-managed. A PHASE document is exempt only when it
// is itself a rendered projection of that graph (IsGeneratedView) — a
// hand-authored phase doc sitting beside a graph is still the plan author's
// own markdown, not a projection, and stays under the v1 rules.
//
// An operational stat failure (see isGraphPlanDir) is recorded on r and
// reported as "not exempt" to the caller — never silently treated as
// absence — so the aborted evaluation's callback-boundary check catches it
// before any rule can turn the inability into a finding either way.
func isGraphPlan(r *Root, a *Artifact) bool {
	dir := filepath.Dir(a.AbsPath)
	graphPlan, err := isGraphPlanDir(r, dir)
	if err != nil {
		return false
	}
	if !graphPlan {
		return false
	}
	if a.Kind() == "phase" {
		return IsGeneratedView(a.Source)
	}
	return true
}

// planGraphIDs loads a plan's committed graph and returns every id that can
// anchor a follow-up: live node ids AND the append-only retired register —
// the tool's own tombstone place, which is what lets a frozen (immutable)
// review's tracked_in survive an in-place graph rebuild that superseded its
// v1 task.
func planGraphIDs(plan *Artifact) (map[string]bool, bool) {
	dir := filepath.Dir(plan.AbsPath)
	raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(dir)+"-Graph.json"))
	if err != nil {
		return nil, false
	}
	var g struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
		Retired []string `json:"retired"`
	}
	if json.Unmarshal(raw, &g) != nil {
		return nil, false
	}
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		ids[n.ID] = true
	}
	for _, id := range g.Retired {
		ids[id] = true
	}
	return ids, true
}

// planGraphJustifies loads a plan's committed graph (`<Plan>-Graph.json`
// beside the README) and returns every node's justifies entries. (nil,
// false) when no graph exists or it does not parse — the graph subsystem
// owns malformed-graph refusals; traceability just falls back to the v1
// harvest. Decoding is deliberately minimal and tolerant: this reader wants
// citations, not the full model, and must not fail when the model grows.
func planGraphJustifies(plan *Artifact) ([]string, bool) {
	dir := filepath.Dir(plan.AbsPath)
	raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(dir)+"-Graph.json"))
	if err != nil {
		return nil, false
	}
	var g struct {
		Nodes []struct {
			Justifies []string `json:"justifies"`
		} `json:"nodes"`
	}
	if json.Unmarshal(raw, &g) != nil {
		return nil, false
	}
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.Justifies...)
	}
	return out, true
}
