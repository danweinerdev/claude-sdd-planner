package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOK(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// The default destination must follow shared/review-artifacts.md — a reviews/
// directory beside the reviewed phase, named <NN>-<slug>-code-review-<rev>.md.
// The original implementation wrote to <planning-root>/Retro/ instead, and no
// test pinned the convention, so the drift went uncaught.
func TestDefaultReviewPathFollowsConvention(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "Plans", "My Plan")
	phase := filepath.Join(planDir, "01-First-Phase.md")
	endpoint := "abcdef0123456789abcdef0123456789abcdef01"

	got := defaultReviewPath(phase, endpoint)
	want := filepath.Join(planDir, "reviews", "01-my-plan-code-review-abcdef0.md")
	if got != want {
		t.Fatalf("defaultReviewPath = %q, want %q", got, want)
	}
	if strings.Contains(got, "Retro") {
		t.Fatalf("defaultReviewPath = %q still points at the legacy Retro/ layout", got)
	}
}

func TestDefaultReviewPathSequencesWithinReviewsDir(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "Plans", "Sample")
	reviews := filepath.Join(planDir, "reviews")
	if err := os.MkdirAll(reviews, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"01-sample-adversarial-review-1111111.md",
		"03-sample-code-review-2222222.md",
		".gitkeep",         // no NN prefix — ignored
		"notes.md",         // no NN prefix — ignored
		"7up-marketing.md", // digits not followed by a hyphen — ignored
	} {
		if err := os.WriteFile(filepath.Join(reviews, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := defaultReviewPath(filepath.Join(planDir, "02-Phase.md"),
		"abcdef0123456789abcdef0123456789abcdef01")
	if base := filepath.Base(got); !strings.HasPrefix(base, "04-") {
		t.Fatalf("sequence should continue past the highest NN- prefix; got %q", base)
	}
}

func TestKebabSlug(t *testing.T) {
	cases := map[string]string{
		"My Plan":       "my-plan",
		"ArkAgent":      "arkagent",
		"CLI_v2 (beta)": "cli-v2-beta",
		"--weird--":     "weird",
		"...":           "",
	}
	for in, want := range cases {
		if got := kebabSlug(in); got != want {
			t.Errorf("kebabSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// End-to-end: scaffolding without --out must land the artifact in the plan's
// reviews/ directory, and a second scaffold must take the next sequence slot.
func TestReviewScaffoldWritesIntoPlanReviewsDir(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "Plans", "Sample Plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	phase := filepath.Join(planDir, "01-First-Phase.md")
	phaseDoc := "---\ntitle: \"First Phase\"\ntype: phase\nstatus: in-progress\n---\n\n# First Phase\n"
	if err := os.WriteFile(phase, []byte(phaseDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	gitOK(t, dir, "init", "-q")
	gitOK(t, dir, "add", ".")
	gitOK(t, dir, "commit", "-q", "-m", "base")
	base := gitOK(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, dir, "add", ".")
	gitOK(t, dir, "commit", "-q", "-m", "work")
	endpoint := gitOK(t, dir, "rev-parse", "HEAD")

	opts := reviewScaffoldOpts{Frozen: base + ".." + endpoint, Mode: "independent"}
	if err := cmdReviewScaffold(phase, opts); err != nil {
		t.Fatalf("cmdReviewScaffold: %v", err)
	}

	want := filepath.Join(planDir, "reviews",
		"01-sample-plan-code-review-"+endpoint[:7]+".md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected review at %s: %v", want, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Retro")); !os.IsNotExist(err) {
		t.Fatalf("scaffold must not create a legacy Retro/ directory (err=%v)", err)
	}

	if err := cmdReviewScaffold(phase, opts); err != nil {
		t.Fatalf("second cmdReviewScaffold: %v", err)
	}
	second := filepath.Join(planDir, "reviews",
		"02-sample-plan-code-review-"+endpoint[:7]+".md")
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second scaffold should take the next sequence slot %s: %v", second, err)
	}
}

// scaffoldedReview builds a git-backed planning root with one phase, scaffolds
// its review, and returns the review path. Shared by the lifecycle tests.
func scaffoldedReview(t *testing.T) (dir, review string) {
	t.Helper()
	dir = t.TempDir()
	planDir := filepath.Join(dir, "Plans", "Sample Plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	phase := filepath.Join(planDir, "01-First-Phase.md")
	phaseDoc := "---\ntitle: \"First Phase\"\ntype: phase\nstatus: in-progress\n---\n\n# First Phase\n"
	if err := os.WriteFile(phase, []byte(phaseDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, dir, "init", "-q")
	gitOK(t, dir, "add", ".")
	gitOK(t, dir, "commit", "-q", "-m", "base")
	base := gitOK(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, dir, "add", ".")
	gitOK(t, dir, "commit", "-q", "-m", "work")
	endpoint := gitOK(t, dir, "rev-parse", "HEAD")

	opts := reviewScaffoldOpts{Frozen: base + ".." + endpoint, Mode: "independent"}
	if err := cmdReviewScaffold(phase, opts); err != nil {
		t.Fatalf("cmdReviewScaffold: %v", err)
	}
	return dir, filepath.Join(planDir, "reviews",
		"01-sample-plan-code-review-"+endpoint[:7]+".md")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A scaffold must start writable: frozen: false, status: open. The original
// shape froze the artifact at birth, which made `status: open` permanently
// unresolvable through supported commands (no write path survived SPK050).
func TestReviewScaffoldStartsUnfrozen(t *testing.T) {
	_, review := scaffoldedReview(t)
	src := readFile(t, review)
	if !strings.Contains(src, "\nfrozen: false\n") {
		t.Fatalf("scaffold must write frozen: false; got:\n%s", src)
	}
	if !strings.Contains(src, "\nstatus: open\n") {
		t.Fatalf("scaffold must write status: open; got:\n%s", src)
	}
}

func TestReviewEvidenceSetGates(t *testing.T) {
	_, review := scaffoldedReview(t)

	if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
		Lane: "not_a_lane", Evidence: "Inspected cmd/sdd/review.go for drift"}); err == nil {
		t.Fatal("evidence set must refuse an unknown lane")
	}
	if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
		Lane: "review_quality", Evidence: "No findings"}); err == nil {
		t.Fatal("evidence set must refuse conclusory evidence")
	}
	if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
		Lane: "review_quality", Evidence: "line one\nline two"}); err == nil {
		t.Fatal("evidence set must refuse multi-line evidence")
	}

	if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
		Lane:     "review_quality",
		Evidence: `Inspected cmd/sdd/review.go error paths; "resolve" refuses on open findings`,
	}); err != nil {
		t.Fatalf("evidence set: %v", err)
	}
	src := readFile(t, review)
	if !strings.Contains(src, `\"resolve\" refuses`) {
		t.Fatalf("evidence with quotes must be YAML-escaped; got:\n%s", src)
	}
	if strings.Count(src, "<REPLACE:") != 3 {
		t.Fatalf("exactly one placeholder should have been replaced; got:\n%s", src)
	}
}

// The full lifecycle: scaffold -> four evidence sets -> resolve. Resolve must
// refuse while any placeholder remains, then set frozen: true and
// status: resolved atomically, after which the artifact is immutable.
func TestReviewResolveLifecycle(t *testing.T) {
	_, review := scaffoldedReview(t)

	if err := cmdReviewResolve(review, reviewResolveOpts{}); err == nil {
		t.Fatal("resolve must refuse while lane evidence is still the placeholder")
	}

	for _, lane := range reviewLaneIDs() {
		if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
			Lane:     lane,
			Evidence: "Inspected cmd/sdd/review.go and internal/rules/phasereview.go; diff matches the task scope with no unplanned changes",
		}); err != nil {
			t.Fatalf("evidence set %s: %v", lane, err)
		}
	}

	if err := cmdReviewResolve(review, reviewResolveOpts{DryRun: true}); err != nil {
		t.Fatalf("resolve --dry-run: %v", err)
	}
	if src := readFile(t, review); !strings.Contains(src, "\nfrozen: false\n") {
		t.Fatalf("--dry-run must not write; got:\n%s", src)
	}

	if err := cmdReviewResolve(review, reviewResolveOpts{}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	src := readFile(t, review)
	if !strings.Contains(src, "\nfrozen: true\n") || !strings.Contains(src, "\nstatus: resolved\n") {
		t.Fatalf("resolve must set frozen: true and status: resolved together; got:\n%s", src)
	}

	// Resolved means frozen means immutable: further writes are refused, and
	// a repeated resolve is a no-op success, matching the other transitions.
	if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
		Lane: "review_quality", Evidence: "Re-inspected the diff after resolution and found drift"}); err == nil {
		t.Fatal("evidence set must refuse a frozen resolved review")
	}
	if err := cmdReviewResolve(review, reviewResolveOpts{}); err != nil {
		t.Fatalf("second resolve should be an already-resolved no-op: %v", err)
	}
}

// --force replaces an abandoned open scaffold, but a frozen (resolved) review
// is FR-46 history and must survive even --force.
func TestReviewScaffoldForceCannotReplaceFrozenReview(t *testing.T) {
	dir, review := scaffoldedReview(t)
	for _, lane := range reviewLaneIDs() {
		if err := cmdReviewEvidenceSet(review, reviewEvidenceOpts{
			Lane:     lane,
			Evidence: "Inspected cmd/sdd/review.go and internal/rules/phasereview.go; behavior matches the reviewed range",
		}); err != nil {
			t.Fatalf("evidence set %s: %v", lane, err)
		}
	}
	if err := cmdReviewResolve(review, reviewResolveOpts{}); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	base := gitOK(t, dir, "rev-parse", "HEAD~1")
	endpoint := gitOK(t, dir, "rev-parse", "HEAD")
	phase := filepath.Join(dir, "Plans", "Sample Plan", "01-First-Phase.md")
	err := cmdReviewScaffold(phase, reviewScaffoldOpts{
		Frozen: base + ".." + endpoint, Mode: "independent",
		Out: review, Force: true,
	})
	if err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("scaffold --force onto a frozen review must be refused; got %v", err)
	}
}
