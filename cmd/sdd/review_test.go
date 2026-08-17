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
