package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/internal/vcs"
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

const reviewUsage = `sdd review scaffold <phase-path> --frozen <base>..<endpoint>
                     [--out PATH] [--mode independent|mixed|single-agent]

Writes the four-lane phase-completion review artifact the gate requires, with
each lane's evidence left as a placeholder the validator refuses until it is
replaced by what the lane actually observed.`

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
		dest = defaultReviewPath(phasePath)
	}
	if existing, err := store.Read(dest); err == nil && existing.Exists && !o.Force {
		return fmt.Errorf("review scaffold: %s already exists; pass --force to replace it", dest)
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
	fmt.Printf("  next: replace each lane's evidence with what it observed, then\n")
	fmt.Printf("        add `- Final aligned review: %s; frozen: %s`\n",
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

// defaultReviewPath places the review beside the planning root's other
// retrospective artifacts, named for the phase it reviews.
func defaultReviewPath(phasePath string) string {
	name := strings.TrimSuffix(filepath.Base(phasePath), ".md")
	dir := filepath.Dir(phasePath)
	for i := 0; i < 4 && dir != "." && dir != string(filepath.Separator); i++ {
		if filepath.Base(dir) == "Plans" {
			return filepath.Join(filepath.Dir(dir), "Retro", name+"-review.md")
		}
		dir = filepath.Dir(dir)
	}
	return filepath.Join("Retro", name+"-review.md")
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
	b.WriteString("frozen: true\n")
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
