package rules

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/vcs"
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

// placeholderRe matches an unfilled `<...>` template marker.
var placeholderRe = regexp.MustCompile(`<[^>]*>`)

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
	// An unfilled template placeholder is not an observation, however many
	// words it contains. Without this, `sdd review scaffold`'s own output
	// validated clean and could have closed a phase on a review nobody ran.
	if placeholderRe.MatchString(s) {
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

// PhaseReviewSchemaErrors exposes the phase-gate schema check to lifecycle
// verbs. `sdd review resolve` refuses on the same reasons the validator would
// report, so a review can never be resolved into a state SDD167 rejects.
func PhaseReviewSchemaErrors(meta map[string]any) []string {
	return phaseReviewSchemaErrors(meta)
}

// UsefulLaneEvidence exposes the lane-evidence quality check so `sdd review
// evidence set` refuses placeholder or conclusory text at write time instead
// of leaving it for the validator to reject later.
func UsefulLaneEvidence(v any) bool {
	return usefulLaneEvidence(v)
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

// --- SDD170: the cited review must be the committed bytes at HEAD ----------
//
// Ports Validator._verify_git_phase_review_committed. The gate above checks
// what the review says; this checks that those exact bytes are in history. A
// review that is correct only in the working tree proves nothing durable: it
// can be edited or deleted with no trace, so the phase it closed would rest on
// evidence that no longer exists.
//
// This dispatches on the PLANNING ROOT's SCM, because the question is whether
// the lifecycle record was committed, and lifecycle bookkeeping lives in the
// planning root.

// verifyGitPhaseReviewCommitted emits SDD170 unless the review artifact is
// tracked at HEAD, byte-identical to what is on disk, and still establishes
// the frozen review state when re-parsed from its committed bytes.
func verifyGitPhaseReviewCommitted(r *Root, ctx phaseGateContext, review *Artifact, frozen string, emit func(Diagnostic)) {
	fail := func(msg, fix string) {
		emit(Diagnostic{
			Code: "SDD170", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
			Message: msg, Correction: fix,
		})
	}

	repo := vcs.Detect(r.Dir)
	if !gitCapable(repo) {
		fail("Final aligned review cannot be checked because the Git planning root is not a worktree.",
			"Use a Git worktree and commit the phase review before phase completion.")
		return
	}

	relative, err := filepath.Rel(repo.Root(), review.AbsPath)
	if err != nil || strings.HasPrefix(relative, "..") {
		fail("Final aligned review `"+review.Rel+"` is outside the Git planning worktree.",
			"Store and commit the phase review in the planning root before phase completion.")
		return
	}
	relative = filepath.ToSlash(relative)

	committedBytes, err := repo.FileAt("HEAD", relative)
	if err != nil {
		fail("Final aligned review `"+review.Rel+"` is not committed at HEAD.",
			"Commit the exact final review artifact in the Git lifecycle record before phase completion.")
		return
	}
	if string(committedBytes) != review.Source {
		fail("Final aligned review `"+review.Rel+"` differs from its committed bytes at HEAD.",
			"Commit the exact reviewed artifact bytes, including its frontmatter, before phase completion.")
		return
	}

	// Re-parse the committed bytes rather than trusting the working-tree
	// artifact: they are equal here, but Python re-derives the frontmatter so
	// a malformed commit is reported as such.
	committed := parseArtifactBytes(committedBytes, review.Rel, review.AbsPath)
	if committed == nil || committed.Meta == nil {
		fail("Committed final aligned review `"+review.Rel+"` has malformed frontmatter.",
			"Commit a valid resolved frozen Aligned review artifact at HEAD.")
		return
	}
	if !isValidPhaseReview(committed, ctx.Phase) || metaStr(committed.Meta, "rev") != frozen {
		fail("Committed final aligned review `"+review.Rel+"` does not establish resolved frozen Aligned four-lane review state for `"+frozen+"`.",
			"Commit frontmatter with review_of, rev, review_scope: phase, frozen: true, verdict: Aligned, all four lanes, and status: resolved.")
	}
}

func init() {
	Register(&Rule{
		Code: "SDD170", Severity: Error, PyFunc: "_verify_git_phase_review_committed",
		What: "the cited final review is not committed at HEAD as the exact reviewed bytes",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			// Python guards this behind `self._planning_root_scm() == "git"`,
			// so a non-git planning root reports nothing here; SDD171 already
			// covers the missing-adapter case.
			if detectedSCM(r.Dir) != "git" {
				return
			}
			for _, ctx := range completePhasesWithEvidence(r) {
				path, frozen, ok := finalAlignedReviewOf(ctx)
				if !ok {
					continue // SDD166
				}
				review := resolveRelated(r, path)
				if review == nil || review.Kind() != "review" {
					continue // SDD166
				}
				verifyGitPhaseReviewCommitted(r, ctx, review, frozen, emit)
			}
		},
		Bad: []Example{{
			Name:  "review-not-committed",
			Files: phaseGateFiles(true, true),
			Setup: [][]string{
				{"git", "init", "-q"},
				{"git", "add", "Plans"},
				{"git", "commit", "-q", "-m", "plan"},
			},
		}},
		Good: []Example{{
			Name:  "review-committed",
			Files: phaseGateFiles(true, true),
			Setup: [][]string{
				{"git", "init", "-q"},
				{"git", "add", "."},
				{"git", "commit", "-q", "-m", "all"},
			},
		}},
	})
}

// --- SDD172 (task identities) and SDD173 (post-review target state) --------

// taskEvidenceBodies returns each task's `### Completion Evidence` block,
// keyed by task id, using the same extraction evidenceTargets does.
func taskEvidenceBodies(a *Artifact) map[string]string {
	out := map[string]string{}
	secs := sections(a, 2)
	for _, t := range asAnyList(a.Meta["tasks"]) {
		m := planEntry(t)
		if m == nil {
			continue
		}
		id := metaStr(m, "id")
		heading := taskHeadingFor(secs, id)
		if heading == "" {
			continue
		}
		blocks := headingBodies(secs[heading].Body, 3, "Completion Evidence")
		if len(blocks) != 1 {
			continue
		}
		out[id] = blocks[0]
	}
	return out
}

// phaseTaskGitIdentities ports _phase_task_git_identities: the completed-task
// commits that make up a phase's review range. A completed task whose evidence
// does not carry a full native Git revision cannot contribute a deterministic
// identity, and emits SDD172 rather than being silently skipped.
//
// This is SDD172's third emission site, the one left behind when the code was
// first ported — the other two live in the generic evidence path.
func phaseTaskGitIdentities(r *Root, phase *Artifact, line int, emit func(Diagnostic)) []taskIdentity {
	var identities []taskIdentity
	if detectedSCM(r.RepoForArtifact(phase.Rel)) != "git" {
		return identities
	}
	evidence := taskEvidenceBodies(phase)
	for _, t := range asAnyList(phase.Meta["tasks"]) {
		m := planEntry(t)
		if m == nil || metaStr(m, "status") != "complete" {
			continue
		}
		id := metaStr(m, "id")
		body := evidence[id]
		vcsVal := markdownScalar(evidenceValue(body, "VCS"))
		if vcsVal == "" {
			vcsVal = "missing"
		}
		revision := markdownScalar(evidenceValue(body, "Revision / checkpoint"))
		if revision == "" {
			revision = "missing"
		}
		if (vcsVal == "git" || vcsVal == "git-worktree") && fullHexRe.MatchString(revision) {
			identities = append(identities, taskIdentity{ID: id, Revision: revision})
			continue
		}
		emit(Diagnostic{
			Code: "SDD172", Severity: Error, Path: phase.Rel, Line: line,
			Message: "Git phase review range cannot validate completed task `" + id +
				"` identity `" + revision + "` with VCS `" + vcsVal +
				"` because no deterministic task-identity adapter is available.",
			Correction: "Record a clean full native Git revision/checkpoint in every completed task's evidence, or keep the phase non-complete until a deterministic adapter for the task identity is available.",
		})
	}
	return identities
}

// taskIdentity is one completed task's native Git revision, kept with its id
// because every diagnostic about it names the task.
type taskIdentity struct {
	ID       string
	Revision string
}

// gitFrozenRangeRe accepts only an immutable full Git range for a phase gate.
var gitFrozenRangeRe = regexp.MustCompile(`^([0-9a-fA-F]{40})\.\.([0-9a-fA-F]{40})$`)

// parseGitFrozenIdentity ports parse_git_frozen_identity.
func parseGitFrozenIdentity(value string) []string {
	m := gitFrozenRangeRe.FindStringSubmatch(value)
	if m == nil {
		return nil
	}
	return []string{m[1], m[2]}
}

// phaseLifecyclePaths ports _git_phase_lifecycle_paths: the explicit lifecycle
// artifacts permitted to change after a frozen review, as target-relative
// paths. Anything else changing is material and voids the review.
func phaseLifecyclePaths(r *Root, phase, review *Artifact, targetRoot string) map[string]bool {
	paths := []string{phase.AbsPath, review.AbsPath}
	if planName := planNameFor(phase); planName != "" {
		paths = append(paths, filepath.Join(r.Dir, "Plans", planName, "README.md"))
		for _, a := range r.Artifacts {
			if a.Meta == nil || a.Kind() != "debrief" {
				continue
			}
			if metaStr(a.Meta, "plan") != planName {
				continue
			}
			if metaStr(a.Meta, "phase") != metaStr(phase.Meta, "phase") {
				continue
			}
			paths = append(paths, a.AbsPath)
		}
	}
	allowed := map[string]bool{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		rel, err := filepath.Rel(targetRoot, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		allowed[filepath.ToSlash(rel)] = true
	}
	return allowed
}

// verifyGitPhasePostReviewState ports _verify_git_phase_post_review_state,
// less its final canonical-intent comparison (see the note in the rule).
func verifyGitPhasePostReviewState(r *Root, ctx phaseGateContext, review *Artifact, endpoint string, emit func(Diagnostic)) {
	fail := func(msg, fix string) {
		emit(Diagnostic{
			Code: "SDD173", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
			Message: msg, Correction: fix,
		})
	}

	repository := r.RepoForArtifact(ctx.Phase.Rel)
	repo := vcs.Detect(repository)
	if !gitCapable(repo) {
		fail("Git phase completion target `"+repository+"` is not a Git worktree.",
			"Use a target Git worktree for phase completion.")
		return
	}
	targetRoot := repo.Root()

	clean, _, err := repo.Clean()
	if err != nil || !clean {
		fail("Git phase completion requires the current target worktree to be clean after review.",
			"Commit only permitted lifecycle records, remove uncommitted changes, and rerun the full phase review after material changes.")
		return
	}

	current, err := repo.Head()
	if err != nil || current == "" {
		fail("Git phase completion target `"+targetRoot+"` has no current HEAD.",
			"Use a target worktree with the reviewed endpoint checked into history.")
		return
	}

	allowed := phaseLifecyclePaths(r, ctx.Phase, review, targetRoot)
	if len(allowed) == 0 {
		if current != endpoint {
			fail("Phase lifecycle files are outside the target repository, so target HEAD must remain the reviewed endpoint.",
				"Keep target HEAD at the frozen review endpoint or rerun the full phase review after target changes.")
		}
		return
	}
	if current == endpoint {
		return
	}

	if ok, err := repo.IsAncestor(endpoint, "HEAD"); err != nil || !ok {
		fail("Reviewed endpoint `"+endpoint+"` is not an ancestor of current target HEAD.",
			"Check out a descendant of the reviewed endpoint or rerun the full phase review.")
		return
	}

	commits, err := repo.RevisionsAfter(endpoint)
	if err != nil {
		fail("Cannot inspect committed target changes after the frozen phase review.",
			"Repair the target Git worktree and rerun phase completion validation.")
		return
	}
	changed := map[string]bool{}
	for _, commit := range commits {
		paths, err := repo.ChangedPaths(commit)
		if err != nil {
			fail("Cannot inspect every committed target change after the frozen phase review.",
				"Repair the target Git worktree and rerun phase completion validation.")
			return
		}
		for _, p := range paths {
			changed[p] = true
		}
	}
	var material []string
	for p := range changed {
		if !allowed[p] {
			material = append(material, p)
		}
	}
	if len(material) > 0 {
		sortStrings(material)
		fail("Committed target paths changed after the frozen phase review are not lifecycle-only: "+
			strings.Join(material, ", ")+".",
			"Rerun the full phase review after source, test, configuration, or other material changes.")
		return
	}

	// Lifecycle files may change after the review, but only as bookkeeping.
	// Compare each governed artifact's canonical INTENT at the frozen endpoint
	// against HEAD: identical means only status/evidence/checkboxes moved, and
	// the review still stands. Different means the scope it reviewed was
	// rewritten underneath it.
	for _, gov := range phaseLifecycleIntentPaths(r, ctx.Phase, targetRoot) {
		frozenIntent, frozenErr := gitLifecycleNormalized(repo, endpoint, gov.rel, gov.kind)
		currentIntent, currentErr := gitLifecycleNormalized(repo, "HEAD", gov.rel, gov.kind)
		if frozenErr != nil || currentErr != nil {
			fail("Cannot compare canonical "+gov.kind+" intent for lifecycle path `"+gov.rel+"` across the frozen phase review.",
				"Keep the governing phase and plan artifacts valid and present at both the frozen endpoint and HEAD, or rerun the full phase review.")
			return
		}
		if frozenIntent != currentIntent {
			fail("Lifecycle path `"+gov.rel+"` changes canonical "+gov.kind+" intent after the frozen phase review.",
				"Do not change phase/plan scope, requirements, tasks, or acceptance text after review; rerun the full phase review.")
			return
		}
	}
}

// governedPath is a lifecycle artifact whose intent must not move after the
// frozen review, with the artifact kind its normalization needs.
type governedPath struct {
	rel  string
	kind string
}

// phaseLifecycleIntentPaths ports _git_phase_lifecycle_intent_paths: the phase
// itself and its plan README, as target-relative paths.
func phaseLifecycleIntentPaths(r *Root, phase *Artifact, targetRoot string) []governedPath {
	candidates := []governedPath{{phase.AbsPath, "phase"}}
	if planName := planNameFor(phase); planName != "" {
		candidates = append(candidates, governedPath{
			filepath.Join(r.Dir, "Plans", planName, "README.md"), "plan",
		})
	}
	var out []governedPath
	for _, c := range candidates {
		rel, err := filepath.Rel(targetRoot, c.rel)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		out = append(out, governedPath{filepath.ToSlash(rel), c.kind})
	}
	return out
}

// gitLifecycleNormalized loads one revision's bytes for a path and returns its
// lifecycle-normalized form, or an error when the path is absent there or its
// lifecycle nodes cannot be excised unambiguously.
func gitLifecycleNormalized(repo vcs.Repo, revision, rel, kind string) (string, error) {
	raw, err := repo.FileAt(revision, rel)
	if err != nil {
		return "", err
	}
	return lifecycleNormalizedArtifact(string(raw), kind)
}

func init() {
	Register(&Rule{
		Code: "SDD173", Severity: Error, PyFunc: "_phase_final_review",
		What: "the frozen phase review identity or the post-review target state is invalid",
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
				// Python runs both halves only when the target repository is
				// git; the missing-adapter case is SDD172's.
				if detectedSCM(r.RepoForArtifact(ctx.Phase.Rel)) != "git" {
					continue
				}
				// The task identities are collected first, because the range
				// check below asserts the frozen range contains each of them.
				// SDD172 owns the diagnostics for tasks that yield none.
				tasks := phaseTaskGitIdentities(r, ctx.Phase, ctx.Line, func(Diagnostic) {})
				verifyPhaseReviewIdentity(r, ctx, frozen, tasks, emit)

				// The post-review state gate runs only once the frozen range
				// is a real range whose commits exist, matching Python's guard
				// in _phase_final_review.
				identities := parseGitFrozenIdentity(frozen)
				if len(identities) == 0 {
					continue
				}
				repo := vcs.Detect(r.RepoForArtifact(ctx.Phase.Rel))
				allExist := true
				for _, id := range identities {
					if ok, err := repo.RevisionExists(id); err != nil || !ok {
						allExist = false
						break
					}
				}
				if !allExist {
					continue
				}
				verifyGitPhasePostReviewState(r, ctx, review, identities[len(identities)-1], emit)
			}
		},
		Bad: []Example{{
			Name:  "dirty-target-after-review",
			Files: phaseGateRangeFiles(),
			Setup: [][]string{
				{"git", "init", "-q"},
				{"git", "add", "code.txt"},
				{"git", "commit", "-q", "-m", "base"},
				{"git", "add", "."},
				{"git", "commit", "-q", "-m", "rest"},
			},
		}},
		Good: []Example{{
			Name:  "non-git-target",
			Files: phaseGateFiles(true, true),
		}},
	})
}

// verifyPhaseReviewIdentity ports _verify_phase_review_identity: the frozen
// range must be a real, forward, non-degenerate Git range whose endpoint is
// the phase's recorded checkpoint, and which actually contains every completed
// task's revision.
//
// This is what makes the frozen identity meaningful. A range that omits a
// completed task's commit did not review that task, however Aligned the review
// claims to be.
func verifyPhaseReviewIdentity(r *Root, ctx phaseGateContext, frozen string, tasks []taskIdentity, emit func(Diagnostic)) {
	fail := func(msg, fix string) {
		emit(Diagnostic{
			Code: "SDD173", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
			Message: msg, Correction: fix,
		})
	}
	repository := r.RepoForArtifact(ctx.Phase.Rel)
	repo := vcs.Detect(repository)
	exists := func(rev string) bool {
		ok, err := repo.RevisionExists(rev)
		return err == nil && ok
	}

	checkpoint := markdownScalar(evidenceValue(ctx.Body, "Revision / checkpoint"))
	if !fullHexRe.MatchString(checkpoint) {
		fail("Git phase completion requires `Revision / checkpoint` to be one clean full native Git revision/checkpoint.",
			"Record the exact full native Git revision/checkpoint as `Revision / checkpoint`; a validated integration merge is allowed, but do not use a dirty or fallback identity.")
		return
	}
	identities := parseGitFrozenIdentity(frozen)
	if len(identities) == 0 {
		fail("Git phase review identity `"+frozen+"` is not an exact `<full40>..<full40>` range.",
			"Use an immutable full-commit range in both review `rev` and `frozen:`.")
		return
	}
	if identities[0] == identities[1] {
		fail("Git phase review range has identical base and endpoint commits.",
			"Use distinct full commits that bound the reviewed phase diff.")
		return
	}
	endpoint := identities[len(identities)-1]
	if endpoint != checkpoint {
		fail("Git phase review endpoint `"+endpoint+"` does not equal phase `Revision / checkpoint` `"+checkpoint+"`.",
			"Review the final phase revision/checkpoint or use a range whose endpoint is that exact checkpoint; a validated integration merge is allowed.")
	}
	allExist := true
	for _, id := range identities {
		if exists(id) {
			continue
		}
		allExist = false
		fail("Git phase review identity commit `"+id+"` does not exist in target repository `"+repository+"`.",
			"Use only full commits that exist in the target repository.")
	}
	if !allExist {
		return
	}
	if ok, err := repo.IsAncestor(identities[0], identities[1]); err != nil || !ok {
		fail("Git phase review range base `"+identities[0]+"` is not an ancestor of endpoint `"+identities[1]+"`.",
			"Use a forward reviewed range whose base is an ancestor of the phase checkpoint.")
		return
	}
	for _, t := range tasks {
		if !exists(t.Revision) {
			fail("Completed task `"+t.ID+"` evidence revision/checkpoint `"+t.Revision+"` does not exist in target repository `"+repository+"`.",
				"Record an existing clean full native Git revision/checkpoint in the completed task's evidence before completing the phase.")
			continue
		}
		if ok, err := repo.IsAncestor(t.Revision, identities[1]); err != nil || !ok {
			fail("Git phase review range `"+frozen+"` omits completed task `"+t.ID+"` evidence revision/checkpoint `"+t.Revision+"` because it is not an ancestor of the endpoint.",
				"Use a frozen range whose endpoint descends from every completed task evidence revision/checkpoint.")
			continue
		}
		if ok, err := repo.IsAncestor(t.Revision, identities[0]); err == nil && ok {
			fail("Git phase review range `"+frozen+"` omits completed task `"+t.ID+"` evidence revision/checkpoint `"+t.Revision+"` because it is at or before the range base.",
				"Move the frozen range base before every completed task evidence revision/checkpoint.")
		}
	}
}

// --- SDD174: the review's planning revision must match current intent ------
//
// Ports _verify_phase_review_planning_revision. A phase review records the
// planning-root commit it reviewed (`reviewed_planning_revision`). This binds
// that claim to reality: the commit must exist, still be reachable, and the
// phase and plan README as of that commit must have the same canonical intent
// they have now. Otherwise the review examined a different plan than the one
// being closed.

func verifyPhaseReviewPlanningRevision(r *Root, ctx phaseGateContext, review *Artifact, emit func(Diagnostic)) {
	fail := func(msg, fix string) {
		emit(Diagnostic{
			Code: "SDD174", Severity: Error, Path: ctx.Phase.Rel, Line: ctx.Line,
			Message: msg, Correction: fix,
		})
	}

	planName := planNameFor(ctx.Phase)
	var plan *Artifact
	if planName != "" {
		plan = r.ByPath["Plans/"+planName+"/README.md"]
	}
	if plan == nil {
		fail("Phase review cannot validate its plan README at the reviewed planning revision.",
			"Ensure the reviewed phase belongs to a discoverable plan README before completing the phase.")
		return
	}

	repo := vcs.Detect(r.Dir)
	if !gitCapable(repo) {
		fail("Phase review requires a Git planning-root lifecycle adapter.",
			"Keep the phase non-complete until the planning root is a Git worktree.")
		return
	}

	revision := metaStr(review.Meta, "reviewed_planning_revision")
	if !fullHexRe.MatchString(revision) {
		// SDD167's schema check already reports a malformed value; Python
		// returns here rather than reporting it twice.
		return
	}
	if ok, err := repo.RevisionExists(revision); err != nil || !ok {
		fail("Final review `"+review.Rel+"` planning revision `"+revision+"` does not exist in planning Git history.",
			"Record an existing full planning Git commit in `reviewed_planning_revision`.")
		return
	}
	if ok, err := repo.IsAncestor(revision, "HEAD"); err != nil || !ok {
		fail("Final review `"+review.Rel+"` planning revision `"+revision+"` is not an ancestor of planning HEAD.",
			"Use a reviewed planning revision retained by the current planning Git history.")
		return
	}

	for _, target := range []struct {
		artifact *Artifact
		label    string
	}{{ctx.Phase, "phase"}, {plan, "plan README"}} {
		relative, err := filepath.Rel(repo.Root(), target.artifact.AbsPath)
		if err != nil || strings.HasPrefix(relative, "..") {
			fail("Final review `"+review.Rel+"` cannot load "+target.label+" outside the planning Git worktree.",
				"Store phase and plan lifecycle artifacts under the planning root.")
			continue
		}
		historical, err := gitLifecycleNormalized(repo, revision, filepath.ToSlash(relative), target.artifact.Kind())
		if err != nil {
			fail("Final review `"+review.Rel+"` cannot load "+target.label+" at planning revision `"+revision+"`.",
				"Review a planning commit that contains the current phase and plan README.")
			continue
		}
		current, err := lifecycleNormalizedArtifact(target.artifact.Source, target.artifact.Kind())
		if err != nil {
			fail("Final review `"+review.Rel+"` cannot normalize current lifecycle "+target.label+" content.",
				"Use block-style single-line lifecycle fields before completing the phase review.")
			continue
		}
		if historical != current {
			fail("Final review `"+review.Rel+"` does not match current lifecycle-normalized "+target.label+" content.",
				"Rerun the four-lane review after changing phase or plan intent.")
		}
	}
}

func init() {
	Register(&Rule{
		Code: "SDD174", Severity: Error, PyFunc: "_verify_phase_review_planning_revision",
		What: "the review's planning revision is missing, unreachable, or reviewed different intent",
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
				verifyPhaseReviewPlanningRevision(r, ctx, review, emit)
			}
		},
		Bad: []Example{{
			Name:  "planning-revision-absent-from-history",
			Files: withPlanReadme(phaseGateFiles(true, true)),
			Setup: [][]string{
				{"git", "init", "-q"},
				{"git", "add", "."},
				{"git", "commit", "-q", "-m", "all"},
			},
		}},
		Good: []Example{{
			// A phase with no Phase Completion Evidence section never reaches
			// the gate at all, which is the cleanest non-finding available:
			// every other shape trips one branch or another, since the rule's
			// whole job is to reject reviews it cannot bind to history.
			Name:  "phase-not-complete",
			Files: map[string]string{"Plans/Sample/01-One.md": validPhase()},
		}},
	})
}

// --- SDD175: plan checkpoint must descend from every phase checkpoint ------
//
// Ports _verify_git_plan_phase_checkpoints. A plan claiming a Git checkpoint
// asserts that checkpoint contains the work of every phase it completed. This
// verifies the claim: each completed phase's checkpoint must exist in the same
// target repository and be an ancestor of the plan's.
//
// Without it a plan could record a checkpoint predating its own phases, so the
// revision it points at would not contain the work it says is done.

func verifyGitPlanPhaseCheckpoints(r *Root, plan *Artifact, body string, line int, emit func(Diagnostic)) {
	fail := func(msg, fix string) {
		emit(Diagnostic{
			Code: "SDD175", Severity: Error, Path: plan.Rel, Line: line,
			Message: msg, Correction: fix,
		})
	}

	planVCS := markdownScalar(evidenceValue(body, "VCS"))
	if planVCS != "git" && planVCS != "git-worktree" {
		return
	}
	repository := r.RepoForArtifact(plan.Rel)
	if detectedSCM(repository) != "git" {
		fail("Git plan evidence targets `"+repository+"`, which has no Git identity adapter.",
			"Record plan Git evidence only for its Git target repository.")
		return
	}
	repo := vcs.Detect(repository)
	exists := func(rev string) bool {
		ok, err := repo.RevisionExists(rev)
		return err == nil && ok
	}

	planCheckpoint := markdownScalar(evidenceValue(body, "Revision / checkpoint"))
	if !fullHexRe.MatchString(planCheckpoint) {
		fail("Git plan completion requires a full native Git `Revision / checkpoint`.",
			"Record the target repository's exact full native Git revision/checkpoint; a validated integration merge is allowed.")
		return
	}
	if !exists(planCheckpoint) {
		fail("Plan Git checkpoint `"+planCheckpoint+"` does not exist in target repository `"+repository+"`.",
			"Record an existing target-repository Git checkpoint.")
		return
	}

	for _, p := range asAnyList(plan.Meta["phases"]) {
		m := planEntry(p)
		if m == nil || metaStr(m, "status") != "complete" {
			continue
		}
		doc, ok := m["doc"].(string)
		if !ok || doc == "" {
			continue
		}
		phaseID := metaStr(m, "id")
		phase := r.ByPath[path.Join(path.Dir(plan.Rel), doc)]
		if phase == nil {
			continue
		}
		phaseRepository := r.RepoForArtifact(phase.Rel)
		if phaseRepository != repository {
			fail("Completed phase `"+phaseID+"` targets `"+phaseRepository+"`, not plan target `"+repository+
				"`; cross-repository checkpoint ordering is unsupported.",
				"Keep all completed phase and plan Git checkpoints in one target repository.")
			continue
		}
		phaseBody := sections(phase, 2)["Phase Completion Evidence"].Body
		phaseVCS := markdownScalar(evidenceValue(phaseBody, "VCS"))
		phaseCheckpoint := markdownScalar(evidenceValue(phaseBody, "Revision / checkpoint"))
		if phaseVCS != "git" && phaseVCS != "git-worktree" {
			display := phaseVCS
			if display == "" {
				display = "missing"
			}
			fail("Completed phase `"+phaseID+"` VCS `"+display+"` is incompatible with Git plan evidence.",
				"Record a Git/git-worktree phase checkpoint in the plan target repository.")
			continue
		}
		if !fullHexRe.MatchString(phaseCheckpoint) {
			fail("Completed phase `"+phaseID+"` lacks a full native Git revision/checkpoint for Git plan evidence.",
				"Record the phase's exact full native Git revision/checkpoint; a validated integration merge is allowed.")
			continue
		}
		if !exists(phaseCheckpoint) {
			fail("Completed phase `"+phaseID+"` Git checkpoint `"+phaseCheckpoint+"` does not exist in target repository `"+repository+"`.",
				"Record an existing phase Git checkpoint in the plan target repository.")
			continue
		}
		if ok, err := repo.IsAncestor(phaseCheckpoint, planCheckpoint); err != nil || !ok {
			fail("Completed phase `"+phaseID+"` Git checkpoint `"+phaseCheckpoint+
				"` is not an ancestor of plan checkpoint `"+planCheckpoint+"`.",
				"Record a plan checkpoint equal to or descending from every completed phase checkpoint.")
		}
	}
}

func init() {
	Register(&Rule{
		Code: "SDD175", Severity: Error, PyFunc: "_verify_git_plan_phase_checkpoints",
		What: "a plan's Git checkpoint does not descend from every completed phase's checkpoint",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				// Python reaches _verify_git_plan_phase_checkpoints only via
				// _plan_phase_identities, which _plan calls only for a plan
				// whose own status is `complete`. An incomplete plan has no
				// checkpoint to bind its phases to yet.
				if a.Meta == nil || a.Kind() != "plan" || a.Status() != "complete" {
					continue
				}
				sec, ok := sections(a, 2)["Plan Completion Evidence"]
				if !ok {
					continue
				}
				verifyGitPlanPhaseCheckpoints(r, a, sec.Body, sec.Line, emit)
			}
		},
		Bad: []Example{{
			Name: "plan-checkpoint-not-full-hex",
			Files: map[string]string{
				"Plans/Sample/README.md": replaceFirst(
					replaceFirst(validPlan(false), "status: draft", "status: complete"),
					"## Plan Completion Evidence\n\nPending — not complete.",
					"## Plan Completion Evidence\n\n- VCS: git\n- Revision / checkpoint: nope\n"),
			},
			Setup: [][]string{
				{"git", "init", "-q"},
				{"git", "add", "."},
				{"git", "commit", "-q", "-m", "all"},
			},
		}},
		Good: []Example{{
			Name: "non-git-plan-evidence",
			Files: map[string]string{
				"Plans/Sample/README.md": validPlan(false),
			},
		}},
	})
}
