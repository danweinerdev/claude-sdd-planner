package main

// `sdd review supersede` scaffolds a sibling artifact for a frozen review
// whose reviewed range needs a fresh look after material change (the
// "supersede it with a fresh scaffold" convention shared/review-artifacts.md
// documents). Today that scaffold is hand-made and the lane evidence is
// copied over by script; this verb owns that mechanical step: same frozen
// range, same review_of, every lane_results row carried over verbatim
// (nothing was re-observed yet), findings emptied and status reset to open
// so the new review starts from a clean slate, and a `supersedes:` field
// naming the old artifact.
//
// `frozen: true` makes a review's *content* immutable (shared/review-
// artifacts.md); it does not exempt it from the same status supersession
// every other artifact kind carries (SDD099/SDD100/SDD101, the spec/design
// `supersedes`/`superseded_by` pair `sdd spec|design supersede` already
// writes). This command writes that reciprocal half too — `status:
// superseded` and `superseded_by:` on the old artifact — atomically with the
// new scaffold, the same two-sided link `docLifecycle` records for specs and
// designs, gated the same way (no new finding the transition itself would
// introduce).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/spf13/cobra"
)

type reviewSupersedeOpts struct {
	Out  string
	JSON bool
}

// supersedeFacts is the subset of a review artifact's frontmatter this
// command reads to build the sibling scaffold.
type supersedeFacts struct {
	Title                    string `yaml:"title"`
	ReviewOf                 string `yaml:"review_of"`
	Rev                      string `yaml:"rev"`
	ReviewScope              string `yaml:"review_scope"`
	ReviewedPlanningRevision string `yaml:"reviewed_planning_revision"`
	ReviewMode               string `yaml:"review_mode"`
	LaneResults              []struct {
		Lane             string `yaml:"lane"`
		Result           string `yaml:"result"`
		ReviewedIdentity string `yaml:"reviewed_identity"`
		Evidence         string `yaml:"evidence"`
	} `yaml:"lane_results"`
}

func cmdReviewSupersede(path string, o reviewSupersedeOpts) error {
	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("review supersede: %w", err)
	}
	if !art.Exists {
		return fmt.Errorf("review supersede: %s does not exist", path)
	}
	path = art.Path
	doc := artifact.Parse(art.Source)
	if kind, _ := doc.FM("type"); strings.Trim(kind, `"'`) != "review" {
		return fmt.Errorf("review supersede: %s is not a review artifact", path)
	}
	if !isFrozenSource(art.Source) {
		return fmt.Errorf("review supersede: %s is not frozen; an open review is edited directly, not superseded", path)
	}
	var f supersedeFacts
	if err := yaml.Unmarshal([]byte(strings.Join(doc.FrontmatterRaw, "\n")), &f); err != nil {
		return fmt.Errorf("review supersede: %s's frontmatter does not parse: %w", path, err)
	}
	if strings.TrimSpace(f.ReviewOf) == "" || strings.TrimSpace(f.Rev) == "" {
		return fmt.Errorf("review supersede: %s carries no review_of/rev to carry forward", path)
	}

	oldRel, err := planningRootRelative(path)
	if err != nil {
		return fmt.Errorf("review supersede: %w", err)
	}

	dest := o.Out
	if dest == "" {
		dest = defaultSupersedeReviewPath(path, f.Rev)
	} else {
		dest = store.ResolveArtifactPath(dest)
		if err := store.CheckCreatePath(dest); err != nil {
			return fmt.Errorf("review supersede: %w", err)
		}
	}
	if existing, eerr := store.Read(dest); eerr == nil && existing.Exists {
		return fmt.Errorf("review supersede: %s already exists", dest)
	}

	today := time.Now().Format("2006-01-02")
	body := renderSupersedeReview(supersedeReviewInput{
		Title:                    f.Title,
		ReviewOf:                 f.ReviewOf,
		Rev:                      f.Rev,
		ReviewScope:              f.ReviewScope,
		ReviewMode:               f.ReviewMode,
		ReviewedPlanningRevision: f.ReviewedPlanningRevision,
		Supersedes:               oldRel,
		Date:                     today,
		Lanes:                    f.LaneResults,
	})
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("review supersede: %w", err)
	}
	if err := store.WriteAtomic(dest, body); err != nil {
		return fmt.Errorf("review supersede: %w", err)
	}

	// Reciprocate on the old artifact: status -> superseded, superseded_by ->
	// the new artifact, the same two-sided link `sdd spec|design supersede`
	// writes (SDD099/SDD101 require both sides). Gated exactly the way every
	// other lifecycle transition is: refused if it would introduce a finding
	// nothing already had. A refusal here rolls back the new scaffold too —
	// this command's contract is one link recorded whole, or nothing written.
	destRel, err := planningRootRelative(dest)
	if err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("review supersede: %w", err)
	}
	oldLines := strings.Split(art.Source, "\n")
	if !setTopLevelStatus(oldLines, "superseded") {
		_ = os.Remove(dest)
		return fmt.Errorf("review supersede: %s has no top-level `status:` field to advance", path)
	}
	oldLines = upsertTopLevelScalar(oldLines, "superseded_by", fmt.Sprintf("%q", destRel))
	oldUpdated := restampUpdated(strings.Join(oldLines, "\n"), today)

	blocking, err := gateDiagnostics(path, oldUpdated)
	if err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("review supersede: %w", err)
	}
	if len(blocking) > 0 {
		_ = os.Remove(dest)
		var b strings.Builder
		fmt.Fprintf(&b, "review supersede: refused — marking %s superseded introduces findings:\n", path)
		for _, d := range blocking {
			fmt.Fprintf(&b, "  %s %s:%d: %s\n", d.Code, d.Path, d.Line, d.Message)
		}
		return errors.New(strings.TrimRight(b.String(), "\n"))
	}
	if err := store.WriteAtomic(path, oldUpdated); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("review supersede: %w", err)
	}

	if o.JSON {
		return writeJSON(reviewSupersedeResult{
			Path:       relPath(dest),
			OK:         true,
			Wrote:      true,
			Supersedes: oldRel,
			ReviewOf:   f.ReviewOf,
			Rev:        f.Rev,
		})
	}
	fmt.Printf("scaffolded %s, superseding %s\n", dest, oldRel)
	fmt.Printf("  review_of: %s\n", f.ReviewOf)
	fmt.Printf("  rev: %s\n", f.Rev)
	fmt.Printf("  carried over %d lane_results row(s); findings reset, status: open\n", len(f.LaneResults))
	fmt.Printf("  %s: status -> superseded, superseded_by -> %s\n", oldRel, destRel)
	return nil
}

type reviewSupersedeResult struct {
	Path       string `json:"path"`
	OK         bool   `json:"ok"`
	Wrote      bool   `json:"wrote"`
	Supersedes string `json:"supersedes"`
	ReviewOf   string `json:"review_of"`
	Rev        string `json:"rev"`
}

type supersedeReviewInput struct {
	Title, ReviewOf, Rev, ReviewScope, ReviewMode, ReviewedPlanningRevision, Supersedes, Date string
	Lanes                                                                                     []struct {
		Lane             string `yaml:"lane"`
		Result           string `yaml:"result"`
		ReviewedIdentity string `yaml:"reviewed_identity"`
		Evidence         string `yaml:"evidence"`
	}
}

// renderSupersedeReview builds the sibling artifact: same frozen range and
// review_of, every lane_results row carried over verbatim (unchanged from
// the superseded artifact — nothing has been re-observed yet), findings and
// followups emptied, status reset to open/unfrozen so the new review can be
// worked, and a supersedes field pointing back at the old artifact.
func renderSupersedeReview(in supersedeReviewInput) string {
	title := strings.Trim(in.Title, `"'`)
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", yamlQuote(title))
	b.WriteString("type: review\n")
	b.WriteString("status: open\n")
	fmt.Fprintf(&b, "created: %s\n", in.Date)
	fmt.Fprintf(&b, "updated: %s\n", in.Date)
	b.WriteString("tags: [review]\n")
	fmt.Fprintf(&b, "related: [%s]\n", yamlQuote(in.ReviewOf))
	fmt.Fprintf(&b, "review_of: %s\n", yamlQuote(in.ReviewOf))
	fmt.Fprintf(&b, "rev: %s\n", yamlQuote(in.Rev))
	if in.ReviewScope != "" {
		fmt.Fprintf(&b, "review_scope: %s\n", in.ReviewScope)
	}
	b.WriteString("frozen: false\n")
	b.WriteString("verdict: Aligned\n")
	if in.ReviewedPlanningRevision != "" {
		fmt.Fprintf(&b, "reviewed_planning_revision: %s\n", yamlQuote(in.ReviewedPlanningRevision))
	}
	if in.ReviewMode != "" {
		fmt.Fprintf(&b, "review_mode: %s\n", in.ReviewMode)
	}
	// supersedes: the pointer back at the frozen artifact this one replaces.
	// SDD100/SDD101 check it resolves to a review and is reciprocated by the
	// old artifact's superseded_by — this command writes both halves.
	fmt.Fprintf(&b, "supersedes: %s\n", yamlQuote(in.Supersedes))
	b.WriteString("lane_results:\n")
	for _, l := range in.Lanes {
		fmt.Fprintf(&b, "  - lane: %s\n", l.Lane)
		fmt.Fprintf(&b, "    result: %s\n", yamlQuote(l.Result))
		if l.ReviewedIdentity != "" {
			fmt.Fprintf(&b, "    reviewed_identity: %s\n", yamlQuote(l.ReviewedIdentity))
		}
		fmt.Fprintf(&b, "    evidence: %s\n", yamlQuote(l.Evidence))
	}
	b.WriteString("findings: []\n")
	b.WriteString("followups: []\n")
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Supersedes `%s`. Reviewed `%s` at frozen identity `%s`.\n\n", in.Supersedes, in.ReviewOf, in.Rev)
	b.WriteString("## Findings\n\nNone.\n\n")
	b.WriteString("## Resolution Log\n\nNone.\n")
	return b.String()
}

// defaultSupersedeReviewPath places the new artifact beside the superseded
// one, in the same reviews/ directory, using the plan directory's slug the
// way defaultReviewPath does for a fresh scaffold — but derived from the old
// review's own path, not from a phase doc (the input here is a review, not a
// phase).
func defaultSupersedeReviewPath(oldReviewPath, rev string) string {
	reviewsDir := filepath.Dir(oldReviewPath)
	planDir := filepath.Dir(reviewsDir)
	slug := kebabSlug(filepath.Base(planDir))
	if slug == "" {
		slug = kebabSlug(strings.TrimSuffix(filepath.Base(oldReviewPath), ".md"))
	}
	// The naming convention keys on the endpoint, not the whole base..endpoint
	// range (defaultReviewPath's convention); fall back to the raw value for
	// a non-Git adapter's rev shape, which parseFrozenRange does not accept.
	short := rev
	if _, endpoint, err := parseFrozenRange(rev); err == nil {
		short = endpoint
	}
	if len(short) > 7 {
		short = short[:7]
	}
	return filepath.Join(reviewsDir,
		fmt.Sprintf("%02d-%s-code-review-%s.md", nextReviewSequence(reviewsDir), slug, short))
}

// yamlQuote renders a string as a double-quoted YAML scalar, escaping the
// same way yamlEscape does for the evidence-set path.
func yamlQuote(s string) string {
	return `"` + yamlEscape(s) + `"`
}

func reviewSupersedeCmd() *cobra.Command {
	var o reviewSupersedeOpts
	c := &cobra.Command{
		Use:   "supersede <review-path> --out <new-path>",
		Short: "Scaffold a sibling review that supersedes a frozen one, carrying lane_results forward",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdReviewSupersede(args[0], o)
		},
	}
	c.Flags().StringVar(&o.Out, "out", "", "output path for the new artifact (default: next NN-<plan>-code-review-<rev>.md in the same reviews/ directory)")
	c.Flags().BoolVar(&o.JSON, "json", false, "emit the result as JSON")
	return c
}
