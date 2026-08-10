package rules

import (
	"regexp"
	"strings"
)

// Family: Validator._phase_final_review — the durable, frozen all-lane review
// gate a phase must pass before it may be `complete`.
//
// Scope of this pass: SDD166 (the `Final aligned review` entry must exist,
// be unique, and resolve to a review artifact), SDD168 (its `frozen:`
// identity must equal the review's own `rev`), and SDD167 (that review must
// actually be a resolved, frozen, four-lane Aligned phase review).
//
// SDD170/173/174 also originate in this subsystem but verify repository state
// (the review is committed, the frozen range exists, nothing landed after it).
// They are left for a follow-up: each needs live git interrogation, which is a
// different kind of work from the frontmatter and evidence checks here.

// finalAlignedReviewRe ports parse_final_aligned_review's deliberately narrow
// syntax: `<path>; frozen: <identity>`, with neither part empty and neither
// containing a semicolon.
var finalAlignedReviewRe = regexp.MustCompile(
	`^([^;\s](?:[^;]*[^;\s])?); frozen: ([^;\s](?:[^;]*[^;\s])?)$`)

// parseFinalAlignedReview returns the review path and frozen identity, or
// ok=false when the value is absent or does not match the required shape.
func parseFinalAlignedReview(value string, present bool) (path, frozen string, ok bool) {
	if !present || value == "" {
		return "", "", false
	}
	m := finalAlignedReviewRe.FindStringSubmatch(value)
	if m == nil {
		return "", "", false
	}
	path = markdownScalar(m[1], true)
	frozen = markdownScalar(m[2], true)
	if path == "" || frozen == "" {
		return "", "", false
	}
	return path, frozen, true
}

// phaseReviewLanes is the exact set of stable lanes a phase-completion review
// must report, each exactly once.
var phaseReviewLanes = map[string]bool{
	"review_plan_drift":      true,
	"review_quality":         true,
	"review_spec_compliance": true,
	"review_blind_spots":     true,
}

var fullHexRe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
var laneWordRe = regexp.MustCompile(`[A-Za-z0-9_./:-]+`)
var conclusoryEvidenceRe = regexp.MustCompile(
	`^(?:no|none|zero)(?: (?:blocking|material|significant|actionable|critical|major|minor))* ` +
		`(?:findings?|issues?|concerns?|problems?|defects?|regressions?)(?: (?:were|was))?` +
		`(?: (?:found|identified|detected|observed))?$`)

// genericLaneWords are words that carry no observation on their own; evidence
// built only from these is conclusory rather than concrete.
var genericLaneWords = map[string]bool{
	"a": true, "an": true, "and": true, "aligned": true, "boundary": true,
	"boundaries": true, "case": true, "cases": true, "code": true, "edge": true,
	"ok": true, "pass": true, "passed": true, "plan": true, "quality": true,
	"requirement": true, "requirements": true, "review": true, "scope": true,
	"success": true, "successful": true, "successfully": true, "task": true,
}

// usefulLaneEvidence ports useful_lane_evidence(): it rejects blank and
// conclusory lane evidence without demanding copied tool output.
func usefulLaneEvidence(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	words := laneWordRe.FindAllString(s, -1)
	lowered := make([]string, 0, len(words))
	for _, w := range words {
		lowered = append(lowered, strings.ToLower(w))
	}
	if conclusoryEvidenceRe.MatchString(strings.Join(lowered, " ")) {
		return false
	}
	if len(words) < 3 {
		return false
	}
	for _, w := range words {
		if !genericLaneWords[strings.ToLower(strings.Trim(w, ".,:;!?"))] {
			return true
		}
	}
	return false
}

// phaseReviewSchemaErrors ports phase_review_schema_errors(). It returns the
// reasons a review's frontmatter fails the phase-gate schema; the caller only
// needs to know whether the list is empty, but building it in full keeps the
// port faithful and makes the reason available if it is ever surfaced.
func phaseReviewSchemaErrors(meta map[string]any) []string {
	var errs []string

	revision, _ := meta["reviewed_planning_revision"].(string)
	if !fullHexRe.MatchString(revision) {
		errs = append(errs, "reviewed_planning_revision must be a full 40-hex Git commit")
	}
	for _, field := range [2]string{"reviewed_phase_intent_sha256", "reviewed_plan_intent_sha256"} {
		if _, present := meta[field]; present {
			errs = append(errs, field+" is a removed custom SHA field")
		}
	}
	switch metaStr(meta, "review_mode") {
	case "independent", "mixed", "single-agent":
	default:
		errs = append(errs, "review_mode must be independent, mixed, or single-agent")
	}

	rows, ok := meta["lane_results"].([]any)
	if !ok || len(rows) != len(phaseReviewLanes) {
		return append(errs, "lane_results must contain exactly four entries")
	}

	rev := metaStr(meta, "rev")
	var lanes []string
	for _, row := range rows {
		m := planEntry(row)
		if m == nil {
			errs = append(errs, "each lane_results entry must be a mapping")
			continue
		}
		lane := metaStr(m, "lane")
		lanes = append(lanes, lane)
		if metaStr(m, "result") != "PASS/Aligned" {
			errs = append(errs, "lane `"+lane+"` result must be PASS/Aligned")
		}
		if metaStr(m, "reviewed_identity") != rev {
			errs = append(errs, "lane `"+lane+"` reviewed_identity must exactly equal rev")
		}
		if !usefulLaneEvidence(m["evidence"]) {
			errs = append(errs, "lane `"+lane+"` evidence must be a specific concrete observation")
		}
	}

	seen := map[string]bool{}
	for _, l := range lanes {
		seen[l] = true
	}
	if len(seen) != len(phaseReviewLanes) {
		return append(errs, "lane_results must name each stable lane exactly once")
	}
	for l := range phaseReviewLanes {
		if !seen[l] {
			return append(errs, "lane_results must name each stable lane exactly once")
		}
	}
	return errs
}

// isValidPhaseReview ports _is_valid_phase_review: every property a review
// must hold to close the phase it reviews.
func isValidPhaseReview(review, phase *Artifact) bool {
	reviewOf := normalizedValue(review.Meta["review_of"])
	if reviewOf != normalizedValue(phase.Rel) &&
		reviewOf != normalizedValue(strings.TrimSuffix(phase.Rel, ".md")) {
		return false
	}
	if metaStr(review.Meta, "review_scope") != "phase" {
		return false
	}
	if frozen, ok := review.Meta["frozen"].(bool); !ok || !frozen {
		return false
	}
	if metaStr(review.Meta, "verdict") != "Aligned" {
		return false
	}
	if metaStr(review.Meta, "status") != "resolved" {
		return false
	}
	if rev := metaStr(review.Meta, "rev"); rev == "" {
		return false
	}
	return len(phaseReviewSchemaErrors(review.Meta)) == 0
}

// phaseGateContext is a complete phase whose evidence section exists — the
// precondition every rule in this family shares.
type phaseGateContext struct {
	Phase *Artifact
	Body  string
	Line  int
}

// completePhasesWithEvidence yields each complete phase carrying a Phase
// Completion Evidence section. Python only runs the gate for those.
func completePhasesWithEvidence(r *Root) []phaseGateContext {
	var out []phaseGateContext
	for _, a := range r.Artifacts {
		if a.Meta == nil || a.Kind() != "phase" || metaStr(a.Meta, "status") != "complete" {
			continue
		}
		sec, ok := sections(a, 2)["Phase Completion Evidence"]
		if !ok {
			continue
		}
		out = append(out, phaseGateContext{Phase: a, Body: sec.Body, Line: sec.Line})
	}
	return out
}

// finalAlignedReviewOf returns the parsed entry for a phase, and whether the
// entry itself is well-formed. Python takes the value only when exactly one
// visible entry exists, so zero or several is a refusal either way.
func finalAlignedReviewOf(ctx phaseGateContext) (path, frozen string, ok bool) {
	values := evidenceValues(ctx.Body, "Final aligned review")
	if len(values) != 1 {
		return "", "", false
	}
	return parseFinalAlignedReview(values[0], true)
}

func init() {
	Register(&Rule{
		Code: "SDD166", Severity: Error, PyFunc: "_phase_final_review",
		What: "a complete phase lacks exactly one valid `Final aligned review` entry resolving to a review",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, ctx := range completePhasesWithEvidence(r) {
				path, _, ok := finalAlignedReviewOf(ctx)
				if !ok {
					emit(Diagnostic{
						Code: "SDD166", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
						Message:    "Complete phase must contain exactly one valid visible `Final aligned review` entry.",
						Correction: "Keep one `- Final aligned review: <review artifact path>; frozen: <exact revision/range>` line outside comments and fenced blocks.",
					})
					continue
				}
				review := resolveRelated(r, path)
				if review != nil && review.Kind() == "review" {
					continue
				}
				emit(Diagnostic{
					Code: "SDD166", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
					Message:    "`Final aligned review` `" + path + "` does not resolve to a review artifact.",
					Correction: "Point it at the persisted final phase code-review artifact.",
				})
			}
		},
		Bad: []Example{{Name: "missing-final-aligned-review", Files: map[string]string{
			"Plans/Sample/01-One.md": completePhaseNoReview(),
		}}},
		Good: []Example{{Name: "valid-final-aligned-review", Files: phaseGateFiles(true, true)}},
	})

	Register(&Rule{
		Code: "SDD168", Severity: Error, PyFunc: "_phase_final_review",
		What: "a `Final aligned review` frozen identity does not equal the review's `rev`",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, ctx := range completePhasesWithEvidence(r) {
				path, frozen, ok := finalAlignedReviewOf(ctx)
				if !ok {
					continue // SDD166
				}
				review := resolveRelated(r, path)
				if review == nil || review.Kind() != "review" {
					continue // SDD166
				}
				if metaStr(review.Meta, "rev") == frozen {
					continue
				}
				emit(Diagnostic{
					Code: "SDD168", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
					Message:    "Final review `" + review.Rel + "` frozen identity `" + frozen + "` does not exactly match its frontmatter `rev`.",
					Correction: "Use the exact nonempty review `rev` after `frozen:` in the Final aligned review entry.",
				})
			}
		},
		Bad:  []Example{{Name: "frozen-rev-mismatch", Files: phaseGateFiles(false, true)}},
		Good: []Example{{Name: "frozen-rev-matches", Files: phaseGateFiles(true, true)}},
	})

	Register(&Rule{
		Code: "SDD167", Severity: Error, PyFunc: "_phase_final_review",
		What: "the final review is not a resolved, frozen, four-lane Aligned phase review",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, ctx := range completePhasesWithEvidence(r) {
				path, _, ok := finalAlignedReviewOf(ctx)
				if !ok {
					continue // SDD166
				}
				review := resolveRelated(r, path)
				if review == nil || review.Kind() != "review" {
					continue // SDD166
				}
				if isValidPhaseReview(review, ctx.Phase) {
					continue
				}
				emit(Diagnostic{
					Code: "SDD167", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
					Message:    "Final review `" + review.Rel + "` is not a resolved, frozen Aligned phase review across all four lanes.",
					Correction: "Record review_scope: phase, frozen: true, verdict: Aligned, the four stable lanes, and resolved status on a review of this phase.",
				})
			}

			// Python's second SDD167 site, in _review: any review declaring
			// `review_scope: phase` has its frontmatter checked directly, and
			// each violation is reported ON THE REVIEW, naming the defect.
			//
			// The two sites answer different questions. The gate above says a
			// phase cannot close because its review does not qualify; this
			// says the review itself is malformed and which field is wrong. A
			// phase-scoped review no phase cites is still checked here, so
			// this cannot be folded into the loop above.
			for _, a := range r.Artifacts {
				if a.Meta == nil || a.Kind() != "review" {
					continue
				}
				// Python reaches this only after the findings/followups list
				// checks; a non-list `findings` returns before it.
				if _, ok := a.Meta["findings"].([]any); !ok {
					continue
				}
				if metaStr(a.Meta, "review_scope") != "phase" {
					continue
				}
				for _, issue := range phaseReviewSchemaErrors(a.Meta) {
					emit(Diagnostic{
						Code: "SDD167", Severity: Error, Path: a.Rel, Line: 1,
						Message:    "Phase review frontmatter is invalid: " + issue + ".",
						Correction: "Set valid `review_mode` and exactly one PASS/Aligned lane_results entry with matching reviewed_identity and nonempty evidence for each stable lane.",
					})
				}
			}
		},
		Bad: []Example{
			{Name: "review-not-aligned", Files: phaseGateFiles(true, false)},
			{Name: "phase-review-bad-schema", Files: map[string]string{
				"Retro/phase-review.md": replaceFirst(
					phaseGateReview("r-2024-01-01-01", true),
					"review_mode: independent", "review_mode: guesswork"),
			}},
		},
		Good: []Example{{Name: "review-aligned", Files: phaseGateFiles(true, true)}},
	})
}
