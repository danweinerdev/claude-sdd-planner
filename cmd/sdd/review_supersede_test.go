package main

import (
	"os"
	"strings"
	"testing"
)

// frozenSampleReview scaffolds a review, fills every lane's evidence, and
// resolves it — the frozen artifact `supersede` is built to consume.
func frozenSampleReview(t *testing.T) (dir, review string) {
	t.Helper()
	dir, review = scaffoldedReview(t)
	for _, lane := range reviewLaneIDs() {
		if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
			Lane:     lane,
			Evidence: "Inspected cmd/sdd/review.go and internal/rules/phasereview.go; diff matches the task scope with no unplanned changes",
		}); err != nil {
			t.Fatalf("evidence set %s: %v", lane, err)
		}
	}
	if err := cmdReviewResolve(review, reviewResolveOpts{}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return dir, review
}

// supersede must refuse a review that is not frozen: only a finished review
// is superseded, never edited in place.
func TestReviewSupersedeRefusesUnfrozenArtifact(t *testing.T) {
	_, review := scaffoldedReview(t)
	if err := cmdReviewSupersede(review, reviewSupersedeOpts{Out: "Plans/Sample Plan/reviews/02-x.md"}); err == nil {
		t.Fatal("supersede must refuse an unfrozen (still-open) review")
	}
}

// The new artifact carries forward review_of, rev, and every lane_results
// row verbatim, resets findings/status/frozen to a fresh, workable review,
// and names the old artifact via supersedes. The old artifact's reviewed
// content never changes (frozen means immutable), but its status
// reciprocates the link — status: superseded, superseded_by: <new path> —
// the same two-sided pair every other superseded artifact kind carries
// (SDD099/SDD101), which `sdd validate` enforces.
func TestReviewSupersedeCarriesLanesForwardAndResetsFindings(t *testing.T) {
	_, review := frozenSampleReview(t)
	before := readFile(t, review)

	if err := cmdReviewSupersede(review, reviewSupersedeOpts{}); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	newPath := strings.Replace(review, "01-sample-plan-code-review-", "02-sample-plan-code-review-", 1)
	newRel, err := planningRootRelative(newPath)
	if err != nil {
		t.Fatal(err)
	}
	oldSrc := readFile(t, review)
	if !strings.Contains(oldSrc, "\nstatus: superseded\n") {
		t.Fatalf("old artifact must flip to status: superseded:\n%s", oldSrc)
	}
	if !strings.Contains(oldSrc, "superseded_by: \""+newRel+"\"") {
		t.Fatalf("old artifact must record superseded_by pointing at the new artifact:\n%s", oldSrc)
	}
	// Only status/updated change in place, and exactly one line is inserted
	// (superseded_by:, which the scaffold never carried). The reviewed
	// content — lane_results, findings, the frozen identity — is untouched.
	beforeWithoutSupersededBy := strings.Replace(oldSrc, "superseded_by: \""+newRel+"\"\n", "", 1)
	beforeLines, afterLines := strings.Split(before, "\n"), strings.Split(beforeWithoutSupersededBy, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("supersede must add exactly one line (superseded_by) to the old artifact:\nbefore (%d lines):\n%s\nafter minus superseded_by (%d lines):\n%s",
			len(beforeLines), before, len(afterLines), beforeWithoutSupersededBy)
	}
	for i := range beforeLines {
		if beforeLines[i] == afterLines[i] {
			continue
		}
		if strings.HasPrefix(afterLines[i], "status:") || strings.HasPrefix(afterLines[i], "updated:") {
			continue
		}
		t.Fatalf("supersede changed an unexpected line %d:\nbefore: %q\nafter:  %q", i, beforeLines[i], afterLines[i])
	}

	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected the sibling artifact at %s: %v", newPath, err)
	}
	src := readFile(t, newPath)

	oldRel, err := planningRootRelative(review)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "supersedes: \""+oldRel+"\"") {
		t.Fatalf("new artifact must name the old one via supersedes:\n%s", src)
	}
	if !strings.Contains(src, "\nstatus: open\n") || !strings.Contains(src, "\nfrozen: false\n") {
		t.Fatalf("new artifact must start open and unfrozen:\n%s", src)
	}
	if !strings.Contains(src, "\nfindings: []\n") || !strings.Contains(src, "\nfollowups: []\n") {
		t.Fatalf("new artifact must start with no findings/followups:\n%s", src)
	}
	// review_of and rev carry forward exactly (same frozen range).
	if !strings.Contains(src, `review_of: "Plans/Sample Plan/01-First-Phase.md"`) {
		t.Fatalf("new artifact must carry forward review_of:\n%s", src)
	}
	for _, lane := range reviewLaneIDs() {
		if !strings.Contains(src, "lane: "+lane) {
			t.Fatalf("new artifact must carry forward lane %s:\n%s", lane, src)
		}
	}
	if !strings.Contains(src, "Inspected cmd/sdd/review.go") {
		t.Fatalf("new artifact must carry forward the lane evidence verbatim:\n%s", src)
	}
}

// Superseding an already-superseded review is refused: SDD099/SDD101 allow
// exactly one supersedes/superseded_by pair, so a second supersede of the
// same old artifact would either orphan the first link or contradict it.
func TestReviewSupersedeRefusesAlreadySupersededArtifact(t *testing.T) {
	_, review := frozenSampleReview(t)
	if err := cmdReviewSupersede(review, reviewSupersedeOpts{}); err != nil {
		t.Fatalf("first supersede: %v", err)
	}
	if err := cmdReviewSupersede(review, reviewSupersedeOpts{}); err == nil {
		t.Fatal("a second supersede of the same (now-superseded) artifact must refuse")
	}
}

// The default destination sequences within the same reviews/ directory, the
// way a fresh scaffold does — a stray existing NN- file bumps it forward.
func TestReviewSupersedeSequencesWithinReviewsDir(t *testing.T) {
	_, review := frozenSampleReview(t)
	stray := strings.Replace(review, "01-sample-plan-code-review-", "02-stray-", 1)
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdReviewSupersede(review, reviewSupersedeOpts{}); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	third := strings.Replace(review, "01-sample-plan-code-review-", "03-sample-plan-code-review-", 1)
	if _, err := os.Stat(third); err != nil {
		t.Fatalf("new artifact should take the next sequence slot %s: %v", third, err)
	}
}
