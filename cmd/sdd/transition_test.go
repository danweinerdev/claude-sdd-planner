package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalSpec is deliberately schema-incomplete: gateDiagnostics only counts
// diagnostics a transition *introduces*, so lifecycle verbs must work on
// imperfect (e.g. legacy) artifacts whose pre-existing findings are sdd
// validate's to report.
func minimalSpec(status, extraSections string) string {
	return "---\n" +
		"title: \"Sample Spec\"\n" +
		"type: spec\n" +
		"status: " + status + "\n" +
		"created: 2026-08-01\n" +
		"updated: 2026-08-01\n" +
		"tags: [spec]\n" +
		"---\n\n# Sample Spec\n\nSome content.\n" + extraSections
}

func writeSpecFixture(t *testing.T, status, extraSections string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	specDir := filepath.Join(dir, "Specs", "Sample")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(specDir, "README.md")
	if err := os.WriteFile(path, []byte(minimalSpec(status, extraSections)), 0o644); err != nil {
		t.Fatal(err)
	}
	// A successor for --by to resolve against: supersede refuses a dangling
	// link, so the replacement must exist before it can be named.
	successorDir := filepath.Join(dir, "Specs", "Sample-v2")
	if err := os.MkdirAll(successorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(successorDir, "README.md"),
		[]byte(minimalSpec("draft", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The full chain the specify/design skills describe but previously had no
// write path for: draft -> review -> approved -> implemented -> superseded.
func TestDocLifecycleChain(t *testing.T) {
	path := writeSpecFixture(t, "draft", "")

	steps := []struct{ verb, want string }{
		{"submit", "status: review"},
		{"approve", "status: approved"},
		{"implement", "status: implemented"},
	}
	for _, s := range steps {
		if err := docLifecycle("spec", s.verb, path, "", false, false); err != nil {
			t.Fatalf("spec %s: %v", s.verb, err)
		}
		if src := readFile(t, path); !strings.Contains(src, "\n"+s.want+"\n") {
			t.Fatalf("after %s want %q; got:\n%s", s.verb, s.want, src)
		}
	}

	if err := docLifecycle("spec", "supersede", path, "Specs/Sample-v2/README.md", false, false); err != nil {
		t.Fatalf("spec supersede: %v", err)
	}
	src := readFile(t, path)
	if !strings.Contains(src, "\nstatus: superseded\n") {
		t.Fatalf("supersede must set status: superseded; got:\n%s", src)
	}
	if !strings.Contains(src, "\nsuperseded_by: \"Specs/Sample-v2/README.md\"\n") {
		t.Fatalf("supersede --by must record superseded_by; got:\n%s", src)
	}

	// Re-running the final verb is an already-there no-op, like the other
	// lifecycle transitions.
	if err := docLifecycle("spec", "supersede", path, "", false, false); err != nil {
		t.Fatalf("second supersede should be a no-op: %v", err)
	}
}

// Superseding without --by and linking the successor afterwards is normal —
// the replacement often does not exist yet at supersede time. Treating the
// second call as an already-there no-op silently discarded the link while
// reporting success, making the successor permanently unrecordable.
func TestDocSupersedeLinksSuccessorAfterTheFact(t *testing.T) {
	path := writeSpecFixture(t, "draft", "")

	if err := docLifecycle("spec", "supersede", path, "", false, false); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if src := readFile(t, path); strings.Contains(src, "superseded_by:") {
		t.Fatalf("supersede without --by must not invent a link; got:\n%s", src)
	}

	if err := docLifecycle("spec", "supersede", path, "Specs/Sample-v2/README.md", false, false); err != nil {
		t.Fatalf("late --by: %v", err)
	}
	src := readFile(t, path)
	if !strings.Contains(src, "\nsuperseded_by: \"Specs/Sample-v2/README.md\"\n") {
		t.Fatalf("late --by must record the successor; got:\n%s", src)
	}
	if !strings.Contains(src, "\nstatus: superseded\n") {
		t.Fatalf("late --by must leave status superseded; got:\n%s", src)
	}

	// Re-running with the same successor is a genuine no-op.
	before := readFile(t, path)
	if err := docLifecycle("spec", "supersede", path, "Specs/Sample-v2/README.md", false, false); err != nil {
		t.Fatalf("idempotent supersede: %v", err)
	}
	if readFile(t, path) != before {
		t.Fatal("re-superseding with an unchanged successor must not rewrite the artifact")
	}
}

func TestDocLifecycleRefusesSkippedState(t *testing.T) {
	path := writeSpecFixture(t, "draft", "")
	if err := docLifecycle("spec", "approve", path, "", false, false); err == nil {
		t.Fatal("approve must refuse a draft artifact; submit comes first")
	}
	if err := docLifecycle("spec", "submit", path, "", false, false); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := docLifecycle("spec", "implement", path, "", false, false); err == nil {
		t.Fatal("implement must refuse a review artifact; approve comes first")
	}
}

// SDD153 gates approval: an approved spec with a blocking open question is a
// finding the transition itself introduces, so approve refuses it.
func TestDocApproveRefusesBlockingOpenQuestion(t *testing.T) {
	path := writeSpecFixture(t, "review",
		"\n## Open Questions\n\n- Should we do this at all?\n")
	err := docLifecycle("spec", "approve", path, "", false, false)
	if err == nil || !strings.Contains(err.Error(), "SDD153") {
		t.Fatalf("approve must refuse on an introduced SDD153 finding; got %v", err)
	}
	if src := readFile(t, path); !strings.Contains(src, "\nstatus: review\n") {
		t.Fatalf("refused approve must not write; got:\n%s", src)
	}
}

// A --by that does not resolve is almost always a typo; recording it would
// produce a supersession chain pointing at nothing.
func TestDocSupersedeRefusesDanglingSuccessor(t *testing.T) {
	path := writeSpecFixture(t, "draft", "")
	err := docLifecycle("spec", "supersede", path, "Specs/Does-Not-Exist/README.md", false, false)
	if err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("supersede must refuse a dangling --by; got %v", err)
	}
	if src := readFile(t, path); !strings.Contains(src, "\nstatus: draft\n") {
		t.Fatalf("a refused supersede must not write; got:\n%s", src)
	}
}

func TestDocLifecycleRejectsWrongKind(t *testing.T) {
	path := writeSpecFixture(t, "draft", "")
	if err := docLifecycle("design", "submit", path, "", false, false); err == nil {
		t.Fatal("design verbs must refuse a spec artifact")
	}
	if err := docLifecycle("spec", "submit", path, "--not-a-verb", false, false); err == nil {
		t.Fatal("--by must be rejected outside supersede")
	}
}
