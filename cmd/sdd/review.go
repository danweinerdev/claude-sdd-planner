package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// `sdd review scaffold` writes the phase-completion review artifact the
// completion gate requires (shared/review-artifacts.md § Phase-completion
// review gate).
//
// The gate checks a precise frontmatter shape — review_scope, frozen, verdict,
// reviewed_planning_revision, review_mode, and exactly four lane_results whose
// reviewed_identity equals rev. Until now nothing produced that shape, so the
// only way to satisfy SDD166/167/168 was to hand-write it correctly, which is
// exactly the class of thing the tool should own.
//
// Scaffolding is deliberately not the same as passing. Lane evidence is left
// as a marker the validator REJECTS (SDD167 requires a specific concrete
// observation, not a conclusion), so a scaffolded review cannot close a phase
// until a human or an agent records what each lane actually observed. The
// command builds the structure; it does not manufacture the review.
//
// The review's own lifecycle is a transition, not an edit. A scaffold starts
// `frozen: false` + `status: open` so `sdd review evidence set` (and, for
// findings work, `apply`/`section set`) can still write it. `sdd review
// resolve` is the closing transition: it verifies the same schema SDD167
// enforces and sets `frozen: true` + `status: resolved` atomically. Freezing
// at resolution rather than at scaffold time is what keeps FR-46 coherent —
// the earlier shape (`frozen: true` at birth) made the artifact immutable
// while still `open`, so no supported command could ever resolve it.

// stableLanes are the data-layer lane identifiers the validator checks, in the
// order shared/review-artifacts.md lists them. Frontmatter always uses these
// names, whatever agent ran the lane.
var stableLanes = []struct{ lane, agent string }{
	{"review_plan_drift", "drift-detector"},
	{"review_quality", "quality-scanner"},
	{"review_spec_compliance", "spec-compliance"},
	{"review_blind_spots", "blind-spot-finder"},
}

// laneEvidencePlaceholder is intentionally refused by SDD167. A scaffold that
// validated clean would let a phase close on a review nobody performed.
const laneEvidencePlaceholder = "<REPLACE: what this lane inspected and observed>"

type reviewScaffoldOpts struct {
	Frozen string
	Out    string
	Mode   string
	Force  bool
	JSON   bool
}

func cmdReviewScaffold(phasePath string, o reviewScaffoldOpts) error {
	switch o.Mode {
	case "independent", "mixed", "single-agent":
	default:
		return fmt.Errorf("review scaffold: --mode must be independent, mixed, or single-agent")
	}

	phaseArt, err := store.Read(phasePath)
	if err != nil {
		return fmt.Errorf("review scaffold: %w", err)
	}
	if !phaseArt.Exists {
		return fmt.Errorf("review scaffold: %s does not exist", phasePath)
	}
	// Use the resolved location from here on: the default review path is
	// derived from the phase's directory, so deriving it from the unresolved
	// argument would scaffold the review beside a path that may not exist.
	phasePath = phaseArt.Path
	phaseDoc := artifact.Parse(phaseArt.Source)
	if kind, _ := phaseDoc.FM("type"); strings.Trim(kind, `"'`) != "phase" {
		return fmt.Errorf("review scaffold: %s is not a phase artifact", phasePath)
	}

	// The frozen identity must be a real, forward, non-degenerate range whose
	// commits exist — the same shape SDD173 enforces. Checking here means a
	// scaffold never carries an identity the gate would reject.
	repo := vcs.Detect(evidenceRepoDir(phasePath))
	base, endpoint, err := parseFrozenRange(o.Frozen)
	if err != nil {
		return fmt.Errorf("review scaffold: %w", err)
	}
	for _, rev := range []string{base, endpoint} {
		exists, err := repo.RevisionExists(rev)
		if err != nil || !exists {
			return fmt.Errorf("review scaffold: revision %s does not exist in the target repository", rev)
		}
	}
	if ok, err := repo.IsAncestor(base, endpoint); err != nil || !ok {
		return fmt.Errorf("review scaffold: %s is not an ancestor of %s; "+
			"a reviewed range must move forward", base, endpoint)
	}

	planningRev, err := headOf(filepath.Dir(phasePath))
	if err != nil {
		return fmt.Errorf("review scaffold: cannot read the planning repository's revision: %w", err)
	}

	dest := o.Out
	if dest == "" {
		dest = defaultReviewPath(phasePath, endpoint)
	} else {
		// --out accepts the same spellings every other command does; resolve
		// it so the scaffold can never be created at a literal ./Plans/...
		// path outside the planning root, and refuse the escapes resolution
		// cannot redirect (absolute out-of-root paths, `..`).
		dest = store.ResolveArtifactPath(dest)
		if err := store.CheckCreatePath(dest); err != nil {
			return fmt.Errorf("review scaffold: %w", err)
		}
	}
	if existing, err := store.Read(dest); err == nil && existing.Exists {
		// A resolved review is frozen history (FR-46); --force must not be a
		// back door that rewrites it. A fresh review goes in a new file.
		if isFrozenSource(existing.Source) {
			return fmt.Errorf("review scaffold: %s is a frozen (resolved) review and may not be replaced, even with --force; scaffold a new review at a new path", dest)
		}
		if !o.Force {
			return fmt.Errorf("review scaffold: %s already exists; pass --force to replace it", dest)
		}
	}

	// References inside an artifact are planning-root-relative, not relative to
	// the caller's working directory. Writing the path as typed produced
	// SDD041/SDD080 on the very first scaffold.
	reviewOf, err := planningRootRelative(phasePath)
	if err != nil {
		return fmt.Errorf("review scaffold: %w", err)
	}

	title, _ := phaseDoc.FM("title")
	body := renderPhaseReview(phaseReviewInput{
		Title:       strings.Trim(title, `"`),
		ReviewOf:    reviewOf,
		Frozen:      o.Frozen,
		PlanningRev: planningRev,
		Mode:        o.Mode,
		Date:        time.Now().Format("2006-01-02"),
	})
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("review scaffold: %w", err)
	}
	if err := store.WriteAtomic(dest, body); err != nil {
		return fmt.Errorf("review scaffold: %w", err)
	}

	if o.JSON {
		return writeJSON(reviewScaffoldResult{
			Path:             relPath(dest),
			OK:               true,
			Wrote:            true,
			ReviewOf:         reviewOf,
			PhasePath:        relPath(phasePath),
			Frozen:           o.Frozen,
			Mode:             o.Mode,
			PlanningRevision: planningRev,
			Lanes:            reviewLaneIDs(),
			// The exact line the phase's evidence section must carry. It is
			// the one output a caller most needs verbatim, and reconstructing
			// it from the other fields means re-implementing the format.
			EvidenceLine: fmt.Sprintf("- Final aligned review: %s; frozen: %s",
				filepath.ToSlash(dest), o.Frozen),
		})
	}

	fmt.Printf("scaffolded %s for %s\n", dest, phasePath)
	fmt.Printf("  frozen: %s\n", o.Frozen)
	fmt.Printf("  next: record each lane's observation via\n")
	fmt.Printf("          sdd review evidence set %s --lane <id> --evidence \"...\"\n", filepath.ToSlash(dest))
	fmt.Printf("        then close the review with\n")
	fmt.Printf("          sdd review resolve %s\n", filepath.ToSlash(dest))
	fmt.Printf("        and add `- Final aligned review: %s; frozen: %s`\n",
		filepath.ToSlash(dest), o.Frozen)
	fmt.Printf("        to the phase's Phase Completion Evidence section.\n")
	return nil
}

// reviewScaffoldResult is the machine-readable outcome of `review scaffold`
// (FR-04). /code-review drives the phase gate, so the fields it needs — where
// the artifact landed, the frozen identity it must echo, and the evidence line
// to paste — are reported rather than scraped from prose.
type reviewScaffoldResult struct {
	Path             string   `json:"path"`
	OK               bool     `json:"ok"`
	Wrote            bool     `json:"wrote"`
	ReviewOf         string   `json:"review_of"`
	PhasePath        string   `json:"phase_path"`
	Frozen           string   `json:"frozen"`
	Mode             string   `json:"mode"`
	PlanningRevision string   `json:"planning_revision"`
	Lanes            []string `json:"lanes"`
	EvidenceLine     string   `json:"evidence_line"`
}

func parseFrozenRange(v string) (base, endpoint string, err error) {
	if v == "" {
		return "", "", fmt.Errorf("--frozen is required: the review must name the exact reviewed range")
	}
	parts := strings.Split(v, "..")
	if len(parts) != 2 || len(parts[0]) != 40 || len(parts[1]) != 40 {
		return "", "", fmt.Errorf("--frozen must be an exact `<full40>..<full40>` range, got %q", v)
	}
	if parts[0] == parts[1] {
		return "", "", fmt.Errorf("--frozen has identical base and endpoint; " +
			"a reviewed range must bound a real diff")
	}
	return parts[0], parts[1], nil
}

// planningRootRelative renders a path the way artifact references must be
// written: relative to the planning root that owns it.
func planningRootRelative(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err := store.FindPlanningRoot(filepath.Dir(abs))
	if err != nil {
		return filepath.ToSlash(path), nil
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path), nil
	}
	return filepath.ToSlash(rel), nil
}

func headOf(dir string) (string, error) {
	repo := vcs.Detect(dir)
	return repo.Head()
}

// defaultReviewPath places the review where shared/review-artifacts.md says
// reviews live — a reviews/ directory beside the reviewed artifact — named
// <NN>-<target-slug>-code-review-<shortrev>.md per that file's § Naming.
func defaultReviewPath(phasePath, endpoint string) string {
	planDir := filepath.Dir(phasePath)
	reviewsDir := filepath.Join(planDir, "reviews")
	slug := kebabSlug(filepath.Base(planDir))
	if slug == "" {
		slug = kebabSlug(strings.TrimSuffix(filepath.Base(phasePath), ".md"))
	}
	short := endpoint
	if len(short) > 7 {
		short = short[:7]
	}
	return filepath.Join(reviewsDir,
		fmt.Sprintf("%02d-%s-code-review-%s.md", nextReviewSequence(reviewsDir), slug, short))
}

// kebabSlug lowercases a name and collapses every non-alphanumeric run to a
// single hyphen, the target-slug form review-artifacts.md § Naming specifies.
func kebabSlug(name string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		default:
			pendingHyphen = true
		}
	}
	return b.String()
}

// nextReviewSequence returns the next zero-padded sequence number within a
// reviews/ directory: one past the highest NN- prefix already present.
func nextReviewSequence(reviewsDir string) int {
	max := 0
	entries, err := os.ReadDir(reviewsDir)
	if err != nil {
		return 1
	}
	for _, e := range entries {
		name := e.Name()
		i := 0
		for i < len(name) && name[i] >= '0' && name[i] <= '9' {
			i++
		}
		if i == 0 || i >= len(name) || name[i] != '-' {
			continue
		}
		n := 0
		for _, c := range name[:i] {
			n = n*10 + int(c-'0')
		}
		if n > max {
			max = n
		}
	}
	return max + 1
}

type phaseReviewInput struct {
	Title, ReviewOf, Frozen, PlanningRev, Mode, Date string
}

func renderPhaseReview(in phaseReviewInput) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: \"Phase review: %s\"\n", in.Title)
	b.WriteString("type: review\n")
	// `open` until every lane's evidence is real; the gate additionally
	// requires `resolved`, so a scaffold cannot close a phase by itself.
	b.WriteString("status: open\n")
	fmt.Fprintf(&b, "created: %s\n", in.Date)
	fmt.Fprintf(&b, "updated: %s\n", in.Date)
	b.WriteString("tags: [review]\n")
	fmt.Fprintf(&b, "related: [\"%s\"]\n", in.ReviewOf)
	fmt.Fprintf(&b, "review_of: \"%s\"\n", in.ReviewOf)
	fmt.Fprintf(&b, "rev: \"%s\"\n", in.Frozen)
	b.WriteString("review_scope: phase\n")
	// Not yet frozen: the artifact must stay writable while evidence and
	// findings are recorded. `sdd review resolve` flips this to true together
	// with `status: resolved`; from then on SPK050 makes the bytes immutable.
	b.WriteString("frozen: false\n")
	b.WriteString("verdict: Aligned\n")
	fmt.Fprintf(&b, "reviewed_planning_revision: \"%s\"\n", in.PlanningRev)
	fmt.Fprintf(&b, "review_mode: %s\n", in.Mode)
	b.WriteString("lane_results:\n")
	for _, l := range stableLanes {
		fmt.Fprintf(&b, "  - lane: %s\n", l.lane)
		b.WriteString("    result: PASS/Aligned\n")
		fmt.Fprintf(&b, "    reviewed_identity: \"%s\"\n", in.Frozen)
		fmt.Fprintf(&b, "    evidence: \"%s\"\n", laneEvidencePlaceholder)
	}
	b.WriteString("findings: []\n")
	b.WriteString("followups: []\n")
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "# Phase review: %s\n\n", in.Title)
	fmt.Fprintf(&b, "Reviewed `%s` at frozen identity `%s`.\n\n", in.ReviewOf, in.Frozen)
	b.WriteString("## Findings\n\nNone.\n\n")
	b.WriteString("## Resolution Log\n\nNone.\n")
	return b.String()
}

// reviewLaneIDs lists the stable lane identifiers a scaffolded review carries,
// read from the same table that renders the artifact so the two cannot drift.
func reviewLaneIDs() []string {
	out := make([]string, 0, len(stableLanes))
	for _, l := range stableLanes {
		out = append(out, l.lane)
	}
	return out
}

// isFrozenSource reports whether an artifact's frontmatter carries
// `frozen: true` — the FR-46 immutability marker.
func isFrozenSource(source string) bool {
	v, ok := artifact.Parse(source).FM("frozen")
	return ok && strings.EqualFold(strings.TrimSpace(strings.Trim(v, `"'`)), "true")
}

// --- sdd review evidence set ------------------------------------------------

type reviewEvidenceOpts struct {
	Lane     string
	Evidence string
	DryRun   bool
	JSON     bool
}

// reviewEvidenceResult is the machine-readable outcome of `review evidence
// set` (FR-04).
type reviewEvidenceResult struct {
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	Wrote  bool   `json:"wrote,omitempty"`
	Lane   string `json:"lane"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// cmdReviewEvidenceSet records one lane's concrete observation on an open,
// not-yet-frozen phase review. It is the supported write path between
// `review scaffold` and `review resolve`: the placeholder the scaffold left is
// replaced here, and the same evidence-quality check SDD167 applies is
// enforced at write time so a refusal happens where it can still be fixed.
func cmdReviewEvidenceSet(path string, o reviewEvidenceOpts) error {
	if !isStableLane(o.Lane) {
		return fmt.Errorf("review evidence set: --lane must be one of %s",
			strings.Join(reviewLaneIDs(), ", "))
	}

	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("review evidence set: %w", err)
	}
	if !art.Exists {
		return fmt.Errorf("review evidence set: %s does not exist", path)
	}
	path = art.Path // write where the artifact was read, never the unresolved argument
	doc := artifact.Parse(art.Source)
	if kind, _ := doc.FM("type"); strings.Trim(kind, `"'`) != "review" {
		return fmt.Errorf("review evidence set: %s is not a review artifact", path)
	}
	// Frozen is the immutability marker, not status: a review that reached
	// `resolved` without freezing (interrupted resolve, hand edit) is still
	// repairable, and refusing here would strand it — `review resolve` could
	// then never be satisfied because its evidence could never be filled.
	if isFrozenSource(art.Source) {
		return fmt.Errorf("review evidence set: %s is frozen (resolved); frozen reviews are immutable — scaffold a fresh review for new work", path)
	}

	evidence := strings.TrimSpace(o.Evidence)
	if evidence == "" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("review evidence set: reading stdin: %w", err)
		}
		evidence = strings.TrimSpace(string(raw))
	}
	if evidence == "" {
		return fmt.Errorf("review evidence set: no evidence given; pass --evidence or write it on stdin")
	}
	if strings.ContainsAny(evidence, "\n\r") {
		return fmt.Errorf("review evidence set: evidence must be a single line; fold the observation into one sentence")
	}
	// The same bar SDD167 sets: a placeholder or a conclusory "no findings"
	// is not an observation. Refusing here keeps the gate honest without
	// making the caller round-trip through `sdd validate`.
	if !rules.UsefulLaneEvidence(evidence) {
		return fmt.Errorf("review evidence set: evidence must be a specific concrete observation (inspected paths, behaviors, or results), not a placeholder or a generic conclusion")
	}

	lines := strings.Split(art.Source, "\n")
	if !setLaneEvidence(lines, o.Lane, evidence) {
		return fmt.Errorf("review evidence set: no lane_results entry for lane %q in %s", o.Lane, path)
	}
	updated := restampUpdated(strings.Join(lines, "\n"), time.Now().Format("2006-01-02"))

	res := reviewEvidenceResult{Path: relPath(path), Lane: o.Lane, DryRun: o.DryRun, OK: true}
	if o.DryRun {
		if o.JSON {
			return writeJSON(res)
		}
		fmt.Printf("review evidence set: would record %s evidence on %s\n", o.Lane, path)
		return nil
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("review evidence set: %w", err)
	}
	res.Wrote = true
	if o.JSON {
		return writeJSON(res)
	}
	fmt.Printf("recorded %s evidence on %s\n", o.Lane, path)
	return nil
}

func isStableLane(lane string) bool {
	for _, l := range stableLanes {
		if l.lane == lane {
			return true
		}
	}
	return false
}

// setLaneEvidence rewrites the named lane's `evidence:` line inside the
// frontmatter's lane_results block, leaving every other byte untouched. It
// scans only the frontmatter — the body may legitimately quote lane lines.
func setLaneEvidence(lines []string, lane, evidence string) bool {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	inLane := false
	for i := 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "---" {
			return false
		}
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "- lane:") {
			v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- lane:")), `"'`)
			inLane = v == lane
			continue
		}
		if inLane && strings.HasPrefix(trimmed, "evidence:") {
			indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
			lines[i] = indent + `evidence: "` + yamlEscape(evidence) + `"`
			return true
		}
	}
	return false
}

// yamlEscape makes a string safe inside a double-quoted YAML scalar.
func yamlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// --- sdd review resolve -----------------------------------------------------

type reviewResolveOpts struct {
	AcceptFollowups bool
	DryRun          bool
	JSON            bool
}

// reviewResolveResult is the machine-readable outcome of `review resolve`
// (FR-04). Blocking carries the refusal reasons so a scripted phase close can
// see why the review is not yet resolvable.
type reviewResolveResult struct {
	Path     string   `json:"path"`
	OK       bool     `json:"ok"`
	Wrote    bool     `json:"wrote,omitempty"`
	From     string   `json:"from,omitempty"`
	To       string   `json:"to,omitempty"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Already  bool     `json:"already,omitempty"`
	Blocking []string `json:"blocking,omitempty"`
}

// terminalFindingStatus mirrors shared/review-artifacts.md: `resolved`
// requires every finding to carry a terminal disposition.
var terminalFindingStatus = map[string]bool{
	"fixed": true, "deferred": true, "rejected": true, "answered": true,
}

// cmdReviewResolve is the closing transition for a review.
//
// For a phase-gate review (review_scope: phase) it verifies the review would
// satisfy the same schema SDD167 enforces, then sets `frozen: true` and
// `status: resolved` in one write. Freezing happens here — at resolution —
// never at scaffold time, so the artifact stays editable exactly while it is
// open and becomes immutable exactly when its content starts backing a phase
// completion.
//
// For an ordinary review it runs the reduced gate — every finding carries a
// terminal disposition, no follow-up floats untracked — and sets
// `status: resolved` without freezing. `status` is tool-owned, so without
// this branch an ordinary review could never legally leave `open`: this verb
// refused on scope while pointing at apply/section set, and apply refused the
// status key with SPK021 (G-1).
func cmdReviewResolve(path string, o reviewResolveOpts) error {
	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("review resolve: %w", err)
	}
	if !art.Exists {
		return fmt.Errorf("review resolve: %s does not exist", path)
	}
	path = art.Path // write where the artifact was read, never the unresolved argument
	doc := artifact.Parse(art.Source)
	if kind, _ := doc.FM("type"); strings.Trim(kind, `"'`) != "review" {
		return fmt.Errorf("review resolve: %s is not a review artifact", path)
	}
	scope, _ := doc.FM("review_scope")
	isPhaseGate := strings.Trim(scope, `"'`) == "phase"

	status, _ := doc.FM("status")
	status = strings.Trim(status, `"'`)
	res := reviewResolveResult{Path: relPath(path), From: status, To: "resolved", DryRun: o.DryRun}
	if status == "resolved" && (!isPhaseGate || isFrozenSource(art.Source)) {
		res.OK, res.Already = true, true
		if o.JSON {
			return writeJSON(res)
		}
		if isPhaseGate {
			fmt.Printf("review resolve: already resolved and frozen\n")
		} else {
			fmt.Printf("review resolve: already resolved\n")
		}
		return nil
	}
	// `resolved` but not frozen is an inconsistent half-state — the artifact
	// claims to be closed while the freeze that makes it immutable never
	// landed (an interrupted resolve, or a hand edit). Re-running the gate
	// completes it rather than refusing: refusing would strand the review in
	// exactly the un-closable state this verb exists to prevent.
	if status != "open" && status != "resolved" {
		return fmt.Errorf("review resolve: %s has status %q; resolve moves an open review to resolved", path, status)
	}

	// The gate: refuse on every reason the validator would reject the
	// resolved review. This is the same reuse discipline as the lifecycle
	// completion verbs — a second opinion on "what makes a review resolvable"
	// would drift from SDD167. The verdict/rev/four-lane checks are the
	// phase-gate machinery; an ordinary review's reduced gate is only the
	// scope-independent hygiene below (terminal findings, tracked followups).
	var blocking []string
	if isPhaseGate {
		if verdict, _ := doc.FM("verdict"); strings.Trim(verdict, `"'`) != "Aligned" {
			blocking = append(blocking, "verdict must be Aligned; a non-Aligned review is superseded by a fresh review after fixes land, not resolved")
		}
		if rev, _ := doc.FM("rev"); strings.Trim(rev, `"'`) == "" {
			blocking = append(blocking, "rev must carry the frozen reviewed identity")
		}
		blocking = append(blocking, rules.PhaseReviewSchemaErrors(fmMeta(doc.FrontmatterRaw))...)
	}
	for _, f := range fmSequence(doc.FrontmatterRaw, "findings") {
		if s := f.Str("status"); !terminalFindingStatus[s] {
			blocking = append(blocking, fmt.Sprintf("finding %s has status %q; every finding needs a terminal disposition (fixed, deferred, rejected, answered)", f.Str("id"), s))
		}
	}
	if !o.AcceptFollowups {
		for _, fu := range fmSequence(doc.FrontmatterRaw, "followups") {
			if fu.Str("tracked_in") == "" {
				blocking = append(blocking, fmt.Sprintf("followup %s is not tracked in a plan task; fill tracked_in, or pass --accept-followups after the user explicitly accepts it floating", fu.Str("id")))
			}
		}
	}
	if len(blocking) > 0 {
		res.Blocking = blocking
		if o.JSON {
			if err := writeJSON(res); err != nil {
				return err
			}
			return &refusedError{n: len(blocking)}
		}
		var b strings.Builder
		b.WriteString("review resolve: refused — the review is not resolvable:\n")
		for _, m := range blocking {
			fmt.Fprintf(&b, "  - %s\n", m)
		}
		return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}

	lines := strings.Split(art.Source, "\n")
	if !setTopLevelStatus(lines, "resolved") {
		return fmt.Errorf("review resolve: no top-level `status:` field to advance")
	}
	// Only phase-gate reviews freeze at resolution; an ordinary review stays
	// an editable record (and is scaffolded without a `frozen:` field).
	if isPhaseGate && !setTopLevelScalar(lines, "frozen", "true") {
		return fmt.Errorf("review resolve: no top-level `frozen:` field to advance — was this review scaffolded by `sdd review scaffold`?")
	}
	updated := restampUpdated(strings.Join(lines, "\n"), time.Now().Format("2006-01-02"))

	// The artifact-level freeze gate: run the validator over the resolved
	// candidate and refuse on any Error-severity finding on this review
	// itself (SDD086/087 finding shape, schema errors, dangling references).
	// Phase-gate reviews only, and deliberately stronger than the transition
	// verbs' introduced-only diff: a phase-gate review freezes at resolve and
	// is immutable afterwards (D-0020), so any invalid byte it carries at
	// freeze time would be invalid forever — the gate must catch pre-existing
	// findings too. An ordinary review stays an editable record, so its
	// defects remain repairable and `sdd validate` reporting them is enough.
	// Waivers are honored, the same criterion `sdd validate` applies.
	var artifactDiags []rules.Diagnostic
	if isPhaseGate {
		artifactDiags, err = candidateArtifactErrors(path, updated)
		if err != nil {
			return fmt.Errorf("review resolve: %w", err)
		}
	}
	if len(artifactDiags) > 0 {
		for _, d := range artifactDiags {
			res.Blocking = append(res.Blocking, fmt.Sprintf("%s: %s %s", d.Code, d.Message, d.Correction))
		}
		if o.JSON {
			if err := writeJSON(res); err != nil {
				return err
			}
			return &refusedError{n: len(artifactDiags)}
		}
		var b strings.Builder
		b.WriteString("review resolve: refused — the resolved review would not validate, and a frozen review cannot be repaired:\n")
		for _, d := range artifactDiags {
			fmt.Fprintf(&b, "  %s %s:%d: %s\n      fix: %s\n", d.Code, d.Path, d.Line, d.Message, d.Correction)
		}
		return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}

	outcome := "resolved"
	if isPhaseGate {
		outcome = "resolved and frozen"
	}
	res.OK = true
	if o.DryRun {
		if o.JSON {
			return writeJSON(res)
		}
		fmt.Printf("review resolve: gate met; would mark %s %s\n", path, outcome)
		return nil
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("review resolve: %w", err)
	}
	res.Wrote = true
	if o.JSON {
		return writeJSON(res)
	}
	fmt.Printf("review resolve: %s is now %s\n", path, outcome)
	return nil
}

// setTopLevelScalar rewrites a top-level frontmatter scalar line in place,
// same discipline as setTopLevelStatus: text surgery on one line, every other
// byte untouched.
func setTopLevelScalar(lines []string, key, value string) bool {
	for i, l := range lines {
		if i > 0 && strings.TrimSpace(l) == "---" {
			return false
		}
		if strings.HasPrefix(l, key+":") {
			lines[i] = key + ": " + value
			return true
		}
	}
	return false
}
