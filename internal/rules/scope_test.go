package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanRelOf(t *testing.T) {
	cases := map[string]string{
		"Plans/Alpha/README.md":   "Plans/Alpha",
		"Plans/Alpha/01-One.md":   "Plans/Alpha",
		"Plans/Alpha/sub/deep.md": "Plans/Alpha",
		"Plans/README.md":         "",
		"Specs/Thing/README.md":   "",
		"Research/topic.md":       "",
		"":                        "",
	}
	for in, want := range cases {
		if got := PlanRelOf(in); got != want {
			t.Errorf("PlanRelOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// scopeFixture is a root with two plans: Alpha (the scoped one) and Beta (a
// foreign plan whose phase doc must be dropped but whose README must stay).
func scopeFixture() map[string]string {
	return map[string]string{
		"Plans/Alpha/README.md": planWithPhase(map[string]string{
			"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
		}),
		"Plans/Alpha/01-One.md": phaseWithTasks("1", "Alpha", `
  - id: "1.1"
    title: First
    status: planned
    verification: run the tests
    justifies: prevents regressions in the thing
`),
		"Plans/Beta/README.md": planWithPhase(map[string]string{
			"id": "1", "title": "One", "status": "planned", "doc": "01-One.md",
		}),
		"Plans/Beta/01-One.md": phaseWithTasks("1", "Beta", `
  - id: "1.1"
    title: First
    status: planned
    verification: run the tests
    justifies: prevents regressions in the thing
`),
		"Specs/Sample/README.md": validSpecTemplate,
		"Research/topic.md":      validResearch,
		"Decisions/decisions.md": decisionLog("\n  - id: D-0001\n    status: accepted\n    question: Q\n    statement: S\n    scope: []\n"),
	}
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScopeToPlanFilter(t *testing.T) {
	dir := writeFixture(t, scopeFixture())
	root, err := LoadRoot(dir)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	scoped := ScopeToPlan(root, "Plans/Alpha")

	kept := map[string]bool{}
	for _, a := range scoped.Artifacts {
		kept[a.Rel] = true
	}
	for _, want := range []string{
		"Plans/Alpha/README.md", "Plans/Alpha/01-One.md",
		"Plans/Beta/README.md", // foreign READMEs stay: ownership + related chains
		"Specs/Sample/README.md", "Research/topic.md", "Decisions/decisions.md",
	} {
		if !kept[want] {
			t.Errorf("scoped root should keep %s", want)
		}
		if scoped.ByPath[want] == nil {
			t.Errorf("scoped ByPath should keep %s", want)
		}
	}
	if kept["Plans/Beta/01-One.md"] {
		t.Errorf("scoped root should drop the foreign phase doc")
	}
	if scoped.ByPath["Plans/Beta/01-One.md"] != nil {
		t.Errorf("scoped ByPath should drop the foreign phase doc")
	}
}

// TestScopeGateEquivalence asserts the property the transition gate depends
// on: for a status flip inside one plan, the set of diagnostics the flip
// introduces is identical whether the root is validated in full or scoped to
// that plan. The fixture gives the foreign plan its own defect (a complete
// task with no completion evidence) so the test also proves foreign findings
// are subtracted identically on both paths.
func TestScopeGateEquivalence(t *testing.T) {
	files := scopeFixture()
	// A pre-existing defect in the foreign plan: complete without evidence.
	files["Plans/Beta/01-One.md"] = strings.Replace(files["Plans/Beta/01-One.md"],
		"status: planned\n    verification", "status: complete\n    verification", 1)
	dir := writeFixture(t, files)

	before := files["Plans/Alpha/01-One.md"]
	after := strings.Replace(before,
		"status: planned\n    verification", "status: complete\n    verification", 1)
	if after == before {
		t.Fatal("fixture flip did not apply")
	}

	full := introducedByFlip(t, dir, "", "Plans/Alpha/01-One.md", before, after)
	scoped := introducedByFlip(t, dir, "Plans/Alpha", "Plans/Alpha/01-One.md", before, after)

	if len(full) == 0 {
		t.Fatal("fixture flip should introduce diagnostics (complete task without evidence)")
	}
	if len(full) != len(scoped) {
		t.Fatalf("introduced sets differ:\nfull:   %v\nscoped: %v", full, scoped)
	}
	for i := range full {
		if full[i] != scoped[i] {
			t.Fatalf("introduced sets differ:\nfull:   %v\nscoped: %v", full, scoped)
		}
	}
}

// A SPEC or DESIGN status flip must produce the same introduced-diagnostics
// set whether the gate sees the whole root or ScopeToDoc's subset. Docs live
// outside Plans/, so PlanRelOf yielded nothing for them and their transitions
// validated everything twice — a design submit on a 224-artifact root took
// 17.8s, long enough to read as a hang.
func TestScopeToDocGateEquivalence(t *testing.T) {
	files := scopeFixture()
	// A pre-existing defect in a foreign plan's PHASE DOC — exactly what the
	// doc scope drops. Its findings must cancel in the diff either way.
	files["Plans/Beta/01-One.md"] = strings.Replace(files["Plans/Beta/01-One.md"],
		"status: planned\n    verification", "status: complete\n    verification", 1)
	// A design whose flip introduces a real finding: approving it with a
	// blocking open question trips SDD153.
	files["Designs/Sample/README.md"] = strings.Replace(
		validDesign("Text."), "## Open Questions\n\nNone.\n",
		"## Open Questions\n\n- Should we do this at all?\n", 1)
	dir := writeFixture(t, files)

	before := files["Designs/Sample/README.md"]
	after := strings.Replace(before, "status: draft", "status: approved", 1)
	if after == before {
		t.Fatal("fixture flip did not apply")
	}

	full := introducedByFlip(t, dir, "", "Designs/Sample/README.md", before, after)
	scoped := introducedByFlipDoc(t, dir, "Designs/Sample/README.md", before, after)

	if len(full) == 0 {
		t.Fatal("fixture flip should introduce diagnostics (approved design with a blocking open question)")
	}
	if len(full) != len(scoped) {
		t.Fatalf("introduced sets differ:\nfull:   %v\nscoped: %v", full, scoped)
	}
	for i := range full {
		if full[i] != scoped[i] {
			t.Fatalf("introduced sets differ:\nfull:   %v\nscoped: %v", full, scoped)
		}
	}
}

// introducedByFlipDoc is introducedByFlip with ScopeToDoc applied.
func introducedByFlipDoc(t *testing.T, dir, flipPath, before, after string) []string {
	t.Helper()
	return introducedByFlipWith(t, dir, flipPath, before, after, func(r *Root) *Root {
		return ScopeToDoc(r, flipPath)
	})
}

// introducedByFlip runs the before/after gate diff over dir, optionally
// scoped, flipping flipPath's content from before to after between the runs.
func introducedByFlip(t *testing.T, dir, scopedTo, flipPath, before, after string) []string {
	t.Helper()
	return introducedByFlipWith(t, dir, flipPath, before, after, func(r *Root) *Root {
		if scopedTo == "" {
			return r
		}
		return ScopeToPlan(r, scopedTo)
	})
}

// introducedByFlipWith is introducedByFlip parameterized by the scoping
// function, so the plan and doc scopes are proven by the same harness.
func introducedByFlipWith(t *testing.T, dir, flipPath, before, after string, scope func(*Root) *Root) []string {
	t.Helper()
	diff := map[string]int{}
	run := func(sign int) {
		root, err := LoadRoot(dir)
		if err != nil {
			t.Fatalf("LoadRoot: %v", err)
		}
		root = scope(root)
		for _, d := range Run(root) {
			diff[d.Code+"\x00"+d.Path+"\x00"+d.Message] += sign
		}
	}
	p := filepath.Join(dir, flipPath)
	run(-1)
	if err := os.WriteFile(p, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
			t.Fatal(err)
		}
	}()
	run(+1)

	var out []string
	for key, n := range diff {
		if n > 0 {
			out = append(out, key)
		}
	}
	sortStrings(out)
	return out
}

// TestScopeGateEquivalenceGitBacked proves the same equivalence property over
// the repository-backed rule families (completion evidence, git identities,
// committed-copy checks) that the bare-directory test cannot reach: the
// fixture is a real git repository, the foreign plan carries a completed task
// whose evidence the full-root path re-verifies against git and the scoped
// path drops, and the flip makes the scoped plan's task complete with valid
// but not-yet-committed lifecycle state — findings that only the VCS rules
// can produce.
func TestScopeGateEquivalenceGitBacked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	// The scoped plan is named Sample because taskEvidencePhase hardcodes
	// `plan: Sample`; the foreign plan reuses the same body renamed to Beta.
	samplePhase := func(status string) string {
		return strings.ReplaceAll(taskEvidencePhase(status, validGitTaskEvidenceBody()), "{{REPO}}", dir)
	}
	betaPhase := strings.Replace(samplePhase("complete"), "plan: Sample", "plan: Beta", 1)

	files := map[string]string{
		"code.txt": "code\n",
		"Plans/Sample/README.md": planWithPhase(map[string]string{
			"id": "1", "title": "Sample Phase", "status": "in-progress", "doc": "01-One.md",
		}),
		"Plans/Sample/01-One.md": samplePhase("planned"),
		"Plans/Beta/README.md": planWithPhase(map[string]string{
			"id": "1", "title": "Sample Phase", "status": "in-progress", "doc": "01-One.md",
		}),
		"Plans/Beta/01-One.md": betaPhase,
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Commit code.txt first so its SHA is the deterministic
	// validGitEvidenceRev both evidence bodies reference, then commit the
	// planning artifacts (with the scoped task still planned).
	for _, args := range [][]string{
		{"git", "init", "-q"},
		{"git", "add", "code.txt"},
		{"git", "commit", "-q", "-m", "impl"},
		{"git", "add", "-A"},
		{"git", "commit", "-q", "-m", "plan"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), setupEnv...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}

	// The committed-copy check runs at phase boundaries (D-0024), so the
	// flip closes the phase as well as the task.
	before := samplePhase("planned")
	after := strings.Replace(samplePhase("complete"), "status: in-progress", "status: complete", 1)
	full := introducedByFlip(t, dir, "", "Plans/Sample/01-One.md", before, after)
	scoped := introducedByFlip(t, dir, "Plans/Sample", "Plans/Sample/01-One.md", before, after)

	if len(full) == 0 {
		t.Fatal("flip should introduce diagnostics (lifecycle state not committed at HEAD)")
	}
	// The introduced findings must include a repository-backed one, or this
	// test is not exercising what it claims to.
	gitBacked := false
	for _, key := range full {
		if strings.Contains(key, "HEAD") {
			gitBacked = true
			break
		}
	}
	if !gitBacked {
		t.Fatalf("expected a git-backed introduced finding (committed-copy check); got: %v", full)
	}
	if len(full) != len(scoped) {
		t.Fatalf("introduced sets differ:\nfull:   %v\nscoped: %v", full, scoped)
	}
	for i := range full {
		if full[i] != scoped[i] {
			t.Fatalf("introduced sets differ:\nfull:   %v\nscoped: %v", full, scoped)
		}
	}
}
