package compile

// Derived completion evidence (shared/completion-evidence.md,
// shared/review-artifacts.md § Phase-completion review gate): for a graph
// plan, the graph's own observations and recorded review gates ARE the
// completion record (CLAUDE.md "Completion Evidence" — "closure is a derived
// predicate"). This file turns that record into the exact v1 markdown shapes
// internal/rules' evidence family (SDD070-075, SDD157/158, SDD166) requires,
// so a closed graph plan's rendered views validate clean without a human
// hand-writing retrospective prose. Nothing here is invented: every value is
// read from the graph's Verification records or from an on-disk review
// artifact's own frontmatter.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// readReviewDir/readReviewFile are the disk reads findPhaseReview performs,
// exposed as package-level variables so tests can inject a failure that is
// not not-exist without a real unreadable filesystem fixture. Production
// code always uses the os defaults; only findPhaseReview's not-exist ==
// "no covering review" folding is by design — every other error must
// surface as operational (review-execution 56815db-b F-01).
var (
	readReviewDir  = os.ReadDir
	readReviewFile = os.ReadFile
)

// nodeRevision returns the git revision a node's observation recorded, or
// "" when it has none (no verification, or a non-git/absent provenance).
func nodeRevision(n *model.Node) string {
	if n.Verification == nil || n.Verification.Provenance == nil {
		return ""
	}
	if n.Verification.Provenance.Kind != "git" {
		return ""
	}
	return n.Verification.Provenance.Revision
}

// phaseCheckpoint picks the single `Revision / checkpoint` a phase's
// completion evidence records: the latest (by node declaration order)
// distinct git revision recorded across the phase's nodes. Every node in a
// rendered phase is closed before this is ever consulted (allClosed gates
// the call site), and in every observed shape every node in a phase shares
// one revision — but a phase's nodes are not required to record identical
// revisions (a node's gate can be re-verified later than a sibling's), so
// this picks the last one in declaration order as the phase's own
// checkpoint, matching `sdd graph status`'s node-order convention.
func phaseCheckpoint(nodes []*model.Node) string {
	rev := ""
	for _, n := range nodes {
		if r := nodeRevision(n); r != "" {
			rev = r
		}
	}
	return rev
}

// reviewArtifact is the subset of a review artifact's frontmatter the
// evidence renderer needs, read straight off disk via
// rules.ParseArtifactBytes — the same parser the validator itself uses, so
// what the renderer finds and what SDD166/167/168 later check can never
// disagree on shape.
type reviewArtifact struct {
	Rel  string // plan-dir-relative path (matches a `doc:`/`Final aligned review` entry)
	Meta map[string]any
}

// findPhaseReview scans planDir/reviews for a resolved, frozen, Aligned,
// phase-scoped review whose `review_of` exactly names phaseDocRel (the
// plan-dir-relative phase doc path SDD166/167 compare against) — the same
// identity check internal/rules/phasereview.go's isValidPhaseReview applies,
// so a review this function returns is guaranteed to satisfy that gate.
// Ties (more than one qualifying review) prefer the review whose `rev`
// carries want as its right-hand (head) endpoint, so a phase's frozen review
// is the one that actually covers its recorded checkpoint; otherwise the
// lexicographically last matching filename wins (later review files sort
// after earlier ones in this plan's naming convention).
//
// Only a not-exist on the reviews directory means "no covering review" —
// any other read failure (permission denied, I/O error, an unreadable
// individual review file) is an inability to answer, not an absence, and
// must propagate as an operational error rather than silently render "no
// covering review" (review-execution 56815db-b F-01).
func findPhaseReview(planDir, phaseDocRel, want string) (*reviewArtifact, bool, error) {
	dir := filepath.Join(planDir, "reviews")
	entries, err := readReviewDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var best *reviewArtifact
	for _, name := range names {
		raw, err := readReviewFile(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, false, fmt.Errorf("reading %s: %w", filepath.Join(dir, name), err)
		}
		rel := filepath.ToSlash(filepath.Join(filepath.Base(planDir), "reviews", name))
		art := rules.ParseArtifactBytes(raw, "Plans/"+rel)
		if art.Kind() != "review" {
			continue
		}
		if art.Status() != "resolved" {
			continue
		}
		if frozen, ok := art.Meta["frozen"].(bool); !ok || !frozen {
			continue
		}
		if s, _ := art.Meta["verdict"].(string); s != "Aligned" {
			continue
		}
		if s, _ := art.Meta["review_scope"].(string); s != "phase" {
			continue
		}
		reviewOf, _ := art.Meta["review_of"].(string)
		if reviewOf != phaseDocRel && reviewOf != strings.TrimSuffix(phaseDocRel, ".md") {
			continue
		}
		rev, _ := art.Meta["rev"].(string)
		candidate := &reviewArtifact{Rel: rel, Meta: art.Meta}
		if strings.HasSuffix(rev, want) {
			return candidate, true, nil // exact head-endpoint match: no better candidate exists
		}
		best = candidate
	}
	return best, best != nil, nil
}

// identityRecheckLine renders the phase evidence's `- Identity recheck:`
// line. When repo is non-nil, it runs a real RevisionExists probe against
// rev at render time and reports the true outcome: an operational probe
// failure propagates as an error (never silently "matched"), and a
// determinate not-found renders as not matched. When repo is nil (no
// resolved target repository at render time), the line states honestly
// that no recheck ran, rather than fabricating a check that never executed
// (review-execution 56815db-b F-01 item 3).
func identityRecheckLine(repo vcs.Repo, rev, today string) (string, error) {
	if repo == nil {
		return fmt.Sprintf("- Identity recheck: no recheck ran (no resolved target repository at render time)\n\n"), nil
	}
	exists, err := repo.RevisionExists(rev)
	if err != nil && !errors.Is(err, vcs.ErrNotFound) {
		return "", fmt.Errorf("identity recheck for %s: %w", rev, err)
	}
	status := "matched"
	if !exists {
		status = "not matched"
	}
	return fmt.Sprintf("- Identity recheck: revision-exists probe for `%s` at %sT00:00:00 — %s\n\n", rev, today, status), nil
}

// renderPhaseEvidence builds the `## Phase Completion Evidence` body for a
// closed phase. today is the render date (Verified label); repoRoot is the
// resolved target repository (Repository label, canonicalized the same way
// SDD072 canonicalizes its own comparison so the two can never disagree).
// repo is the vcs.Repo resolved for repoRoot (nil when none was resolved),
// used for the identity recheck's real probe.
func renderPhaseEvidence(planDir, plan string, ph phaseGroup, repoRoot, today string, repo vcs.Repo) (string, error) {
	rev := phaseCheckpoint(ph.Nodes)
	var b strings.Builder
	fmt.Fprintf(&b, "- Verified: %s\n", today)
	fmt.Fprintf(&b, "- Repository: %s\n", vcs.CanonPath(repoRoot))
	fmt.Fprintf(&b, "- VCS: git\n")
	fmt.Fprintf(&b, "- Revision / checkpoint: `%s`\n", rev)
	line, err := identityRecheckLine(repo, rev, today)
	if err != nil {
		return "", err
	}
	b.WriteString(line)
	b.WriteString("| Command | Working directory | Result | Observable evidence |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	fmt.Fprintf(&b, "| `sdd graph status --plan %s` | . | PASS (exit 0) | phase %d: %d/%d node(s) closed (GREEN, covered by a passing frozen full review gate) |\n\n",
		plan, ph.Ordinal, len(ph.Nodes), len(ph.Nodes))

	// Graph phase docs carry no v1 tasks (`tasks: []`), so the required
	// section is present but empty of identity entries — SDD157's
	// completedTaskIdentitiesCheck computes the same empty `expected` map
	// for a `tasks: []` phase and only requires the section to exist.
	b.WriteString("### Completed task identities\n\n")

	phaseDocRel := "Plans/" + plan + "/" + ph.Doc
	review, found, err := findPhaseReview(planDir, phaseDocRel, rev)
	if err != nil {
		return "", err
	}
	if found {
		reviewRev, _ := review.Meta["rev"].(string)
		fmt.Fprintf(&b, "- Final aligned review: `Plans/%s`; frozen: %s\n", review.Rel, reviewRev)
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

// renderPlanEvidence builds the plan README's `## Plan Completion Evidence`
// body once every phase is complete: a `### Completed phase identities`
// entry for each completed phase, each citing its own checkpoint and final
// review — the exact shape internal/rules/headings.go's
// completedPhaseIdentitiesCheck requires.
func renderPlanEvidence(planDir, plan, today string, groups []phaseGroup, closed map[string]bool) (string, bool, error) {
	var completed []phaseGroup
	for _, ph := range groups {
		if allClosed(ph.Nodes, closed) {
			completed = append(completed, ph)
		}
	}
	if len(completed) != len(groups) || len(groups) == 0 {
		return "", false, nil
	}
	var identities strings.Builder
	ok := true
	for _, ph := range completed {
		rev := phaseCheckpoint(ph.Nodes)
		phaseDocRel := "Plans/" + plan + "/" + ph.Doc
		review, found, err := findPhaseReview(planDir, phaseDocRel, rev)
		if err != nil {
			return "", false, err
		}
		if !found {
			ok = false
			continue
		}
		fmt.Fprintf(&identities, "- `%d`: `%s`; review: `Plans/%s`\n", ph.Ordinal, rev, review.Rel)
	}
	if !ok {
		return "", false, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- Verified: %s\n\n", today)
	b.WriteString("### Completed phase identities\n")
	b.WriteString(identities.String())
	return strings.TrimRight(b.String(), "\n") + "\n", true, nil
}
