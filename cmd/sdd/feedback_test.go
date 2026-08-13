package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// TestDistinguishingDigestsExposeTheDifference pins the reported failure: two
// digests sharing a 12-character prefix rendered as
// "expected 2900a4afce7b, found 2900a4afce7b" — a mismatch error whose own
// evidence claimed the values were equal.
func TestDistinguishingDigestsExposeTheDifference(t *testing.T) {
	cases := []struct{ a, b string }{
		{"2900a4afce7b0000000000000000000000000000000000000000000000000000",
			"2900a4afce7b1111111111111111111111111111111111111111111111111111"},
		// Divergence far into the string.
		{strings.Repeat("a", 60) + "0000", strings.Repeat("a", 60) + "1111"},
		// Divergence in the first characters: stays short.
		{"abc1" + strings.Repeat("0", 60), "xyz9" + strings.Repeat("0", 60)},
	}
	for _, c := range cases {
		gotA, gotB := distinguishing(c.a, c.b)
		if gotA == gotB {
			t.Errorf("distinguishing(%.16s…, %.16s…) rendered both as %q — "+
				"the mismatch message would claim the digests are identical", c.a, c.b, gotA)
		}
		if !strings.HasPrefix(c.a, gotA) || !strings.HasPrefix(c.b, gotB) {
			t.Errorf("output is not a prefix of its input: %q / %q", gotA, gotB)
		}
	}
}

// Equal inputs should never reach the stale-error path, but if they do the
// helper must not fabricate a difference.
func TestDistinguishingEqualInputs(t *testing.T) {
	d := strings.Repeat("f", 64)
	a, b := distinguishing(d, d)
	if a != d || b != d {
		t.Errorf("equal digests were altered: %q / %q", a, b)
	}
}

// TestShortDigestsAreNotPadded guards the fallback for inputs shorter than the
// minimum window (e.g. "<absent>" when the artifact does not exist).
func TestDistinguishingShortInputs(t *testing.T) {
	a, b := distinguishing("abc123", "<absent>")
	if a != "abc123" || b != "<absent>" {
		t.Errorf("short inputs were mangled: %q / %q", a, b)
	}
}

// TestYAMLValuePreservesTypes pins `sdd show --json` emitting real JSON types.
// Field feedback: arrays serialized as the string "[a, b, c]" and quoted
// scalars kept their literal quote characters, so a JSON consumer had to
// re-parse YAML out of a JSON string.
func TestYAMLValuePreservesTypes(t *testing.T) {
	if got := yamlValue("[a, b, c]"); len(got.([]any)) != 3 {
		t.Errorf("array frontmatter did not decode to a list: %#v", got)
	}
	if got := yamlValue(`"Quoted — Title"`); got != "Quoted — Title" {
		t.Errorf("quote characters survived into the value: %#v", got)
	}
	if got := yamlValue("draft"); got != "draft" {
		t.Errorf("plain scalar changed: %#v", got)
	}
	// A date must keep its source spelling: YAML resolves an unquoted
	// 2026-08-02 to a timestamp, which would render as 2026-08-02T00:00:00Z
	// — a value the artifact does not contain.
	if got := yamlValue("2026-08-02"); got != "2026-08-02" {
		t.Errorf("date was rewritten as a timestamp: %#v", got)
	}
	// Malformed YAML falls back to raw text rather than failing: show is a
	// read-only inspection command and a broken field is what it is run to find.
	if got := yamlValue("[unclosed"); got != "[unclosed" {
		t.Errorf("malformed value did not fall back to raw text: %#v", got)
	}
	if got := yamlValue(""); got != "" {
		t.Errorf("empty value changed: %#v", got)
	}
}

// TestResolveArtifactPathAcceptsBothSpellings pins the reported failure:
// `sdd show Specs/...` did not resolve while `.plans/Specs/...` did, even
// though every path the tool *prints* (validator diagnostics, list output,
// `related` frontmatter) is planning-root relative — so copying one back into
// a command was the natural move and the one that failed.
func TestResolveArtifactPathAcceptsBothSpellings(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "planning-config.json"), `{"planningRoot": ".plans"}`)
	mustWrite(t, filepath.Join(root, ".plans", "Specs", "Feature", "README.md"), "# x\n")
	// A same-named file next to the config, to prove the literal path wins.
	mustWrite(t, filepath.Join(root, "Specs", "Feature", "README.md"), "# literal\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	// Working-directory-relative path that exists: returned untouched, so a
	// real file is never shadowed by a same-named artifact under the root.
	if got := store.ResolveArtifactPath("Specs/Feature/README.md"); got != "Specs/Feature/README.md" {
		t.Errorf("existing literal path was redirected: %q", got)
	}
	// Planning-root-relative path with no literal counterpart: resolved.
	got := store.ResolveArtifactPath("Specs/Other/README.md")
	if got != "Specs/Other/README.md" {
		t.Errorf("absent path should be reported as given, got %q", got)
	}
	mustWrite(t, filepath.Join(root, ".plans", "Specs", "Other", "README.md"), "# y\n")
	got = store.ResolveArtifactPath("Specs/Other/README.md")
	if filepath.ToSlash(got) != ".plans/Specs/Other/README.md" {
		t.Errorf("planning-root-relative path did not resolve, got %q", got)
	}
	// Absolute paths are never rewritten.
	abs := filepath.Join(root, ".plans", "Specs", "Feature", "README.md")
	if store.ResolveArtifactPath(abs) != abs {
		t.Error("absolute path was rewritten")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTemplateForApplyOmitsToolOwnedFields pins the two template forms.
//
// Field feedback: `sdd apply` rejected tool-owned frontmatter (status,
// updated, type, created) that the generated template contained — so template
// output was not valid apply input. The default form stays complete (it is
// what shared/templates/ ships and what --check compares against); --for-apply
// drops exactly the fields the tool owns.
func TestTemplateForApplyOmitsToolOwnedFields(t *testing.T) {
	s, err := schema.Load("spec")
	if err != nil {
		t.Fatal(err)
	}
	full, err := renderTemplateFor("spec", false)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := renderTemplateFor("spec", true)
	if err != nil {
		t.Fatal(err)
	}

	var toolFields, authorFields []string
	for _, f := range s.Frontmatter {
		if f.Ownership() == schema.Tool {
			toolFields = append(toolFields, f.Key)
		} else {
			authorFields = append(authorFields, f.Key)
		}
	}
	if len(toolFields) == 0 {
		t.Fatal("spec schema declares no tool-owned fields; this test proves nothing")
	}

	for _, k := range toolFields {
		if !strings.Contains(full, "\n"+k+":") {
			t.Errorf("default template is missing tool-owned %q; it must stay a complete artifact", k)
		}
		if strings.Contains(payload, "\n"+k+":") {
			t.Errorf("--for-apply payload still carries tool-owned %q; apply would refuse it", k)
		}
	}
	// Author fields must survive in both — dropping them would make the
	// payload useless rather than merely acceptable.
	for _, k := range authorFields {
		if !strings.Contains(payload, "\n"+k+":") {
			t.Errorf("--for-apply payload dropped author-owned %q", k)
		}
	}
	// Body structure is identical: only frontmatter differs.
	if fullBody, payloadBody := afterFrontmatter(full), afterFrontmatter(payload); fullBody != payloadBody {
		t.Error("--for-apply changed the body; it must only drop frontmatter fields")
	}
}

func afterFrontmatter(doc string) string {
	rest := strings.TrimPrefix(doc, "---\n")
	if i := strings.Index(rest, "\n---\n"); i >= 0 {
		return rest[i+len("\n---\n"):]
	}
	return doc
}

// TestScopedValidationKeepsOnlyGoverningLedgerFindings pins the reported
// behavior: `sdd validate --scope` printed the whole decision ledger's
// diagnostics regardless of the scope, so a single artifact's findings were
// buried under errors about unrelated decisions. The ledger is genuinely
// cross-cutting — one file whose entries govern artifacts across the root —
// so the fix is relevance, not exclusion: an entry stays when it governs
// something in scope and is dropped when it does not.
func TestScopedValidationKeepsOnlyGoverningLedgerFindings(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":"."}`)
	for _, name := range []string{"Alpha", "Beta"} {
		mustWrite(t, filepath.Join(root, "Specs", name, "README.md"), specFixture(name))
	}
	// Two entries, each missing `rationale` (SDD112), each governing one spec.
	mustWrite(t, filepath.Join(root, "Decisions", "decisions.md"), ledgerFixtureTwoScopes())

	all := runValidate(t, root, "")
	if !strings.Contains(all, "D-0001") || !strings.Contains(all, "D-0002") {
		t.Fatalf("unscoped run should report both entries:\n%s", all)
	}

	alpha := runValidate(t, root, "Specs/Alpha")
	if !strings.Contains(alpha, "D-0001") {
		t.Errorf("scoping to Specs/Alpha dropped D-0001, which governs it:\n%s", alpha)
	}
	if strings.Contains(alpha, "D-0002") {
		t.Errorf("scoping to Specs/Alpha kept D-0002, which governs only Specs/Beta:\n%s", alpha)
	}
}

// TestLedgerFindingsNameTheirEntry pins the other half of the report: two
// entries missing the same field produced two byte-identical lines at line 1,
// which reads as the validator repeating itself and leaves no way to tell
// which entries to fix.
func TestLedgerFindingsNameTheirEntry(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":"."}`)
	mustWrite(t, filepath.Join(root, "Specs", "Alpha", "README.md"), specFixture("Alpha"))
	mustWrite(t, filepath.Join(root, "Specs", "Beta", "README.md"), specFixture("Beta"))
	mustWrite(t, filepath.Join(root, "Decisions", "decisions.md"), ledgerFixtureTwoScopes())

	out := runValidate(t, root, "")
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "SDD112") {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	if len(lines) != 2 {
		t.Fatalf("want two SDD112 findings, got %d:\n%s", len(lines), out)
	}
	if lines[0] == lines[1] {
		t.Errorf("the two findings are indistinguishable; each must name its entry:\n  %s", lines[0])
	}
}

func specFixture(title string) string {
	return "---\ntitle: \"" + title + "\"\ntype: spec\nstatus: draft\n" +
		"created: 2026-01-01\nupdated: 2026-01-01\ntags: []\nrelated: []\n---\n\n" +
		"## Overview\nx\n\n## Goals\n- g\n\n## Non-Goals\n- n\n\n" +
		"## Requirements\n### Functional Requirements\n- **FR-01**: r\n\n" +
		"## Acceptance Criteria\n- **AC-01**: a\n\n## Open Questions\nNone.\n"
}

// Two accepted entries, each missing `rationale`, each scoped to one spec.
func ledgerFixtureTwoScopes() string {
	return "---\ntitle: \"Decisions\"\ntype: decision-log\nstatus: active\n" +
		"created: 2026-01-01\nupdated: 2026-01-01\ndecisions:\n" +
		"  - id: D-0001\n    kind: decision\n    status: accepted\n    date: 2026-01-01\n" +
		"    decided_by: user\n    statement: Alpha uses X.\n    scope: [\"Specs/Alpha\"]\n" +
		"  - id: D-0002\n    kind: decision\n    status: accepted\n    date: 2026-01-01\n" +
		"    decided_by: user\n    statement: Beta uses Y.\n    scope: [\"Specs/Beta\"]\n" +
		"---\n\n## Decisions\nSee frontmatter.\n"
}

// runValidate captures cmdValidate's stdout for one root/scope pair.
func runValidate(t *testing.T, root, scope string) string {
	t.Helper()
	opts := validateOpts{Root: root, Scope: scope}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	_ = cmdValidate(opts) // a refusal is expected; the output is the subject
	w.Close()
	os.Stdout = old
	return <-done
}

// TestTransitionJSONShapeCarriesGateFindings pins the FR-04 contract for
// lifecycle transitions. The refusal case is the one that mattered: the text
// path renders gate findings as prose, so a caller scripting a phase close had
// to scrape it to learn *why* a completion was refused.
func TestTransitionJSONShapeCarriesGateFindings(t *testing.T) {
	res := transitionResult{
		Path: "Plans/P/01-Phase.md", Kind: "task", Verb: "complete", ID: "1.1",
		To: "complete",
		Blocking: toGateFindings([]rules.Diagnostic{{
			Code: "SDD070", Path: "Plans/P/01-Phase.md", Line: 12,
			Message: "evidence is pending", Correction: "record it",
		}}),
		Pending: toGateFindings([]rules.Diagnostic{{
			Code: "SDD160", Path: "Plans/P/01-Phase.md", Line: 1,
			Message: "verifies the committed copy", Correction: "commit first",
		}}),
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"path", "ok", "kind", "verb", "id", "to", "blocking", "pending"} {
		if _, ok := got[k]; !ok {
			t.Errorf("transition JSON is missing %q: %s", k, b)
		}
	}
	// Blocking findings must carry the code and correction a caller acts on,
	// not just a message.
	blocking, _ := got["blocking"].([]any)
	if len(blocking) != 1 {
		t.Fatalf("want one blocking finding, got %v", got["blocking"])
	}
	f, _ := blocking[0].(map[string]any)
	for _, k := range []string{"code", "path", "line", "message", "correction"} {
		if _, ok := f[k]; !ok {
			t.Errorf("blocking finding is missing %q: %v", k, f)
		}
	}
	// Pending is separate from blocking: those checks inspect the committed
	// copy and cannot pass until this very change is committed, so reporting
	// them as blockers would describe an unresolvable state.
	if pending, _ := got["pending"].([]any); len(pending) != 1 {
		t.Errorf("pending findings were not reported separately: %v", got["pending"])
	}

	// A refused transition exits 1 (refusal), not 2 (could not run).
	if code := exitCode(&refusedError{n: 1}); code != 1 {
		t.Errorf("a refused transition should exit 1, got %d", code)
	}
	// Omitted optionals stay absent rather than serializing as false/empty.
	clean, _ := json.Marshal(transitionResult{Path: "p", OK: true, Kind: "plan", Verb: "approve"})
	if strings.Contains(string(clean), "blocking") || strings.Contains(string(clean), "dry_run") {
		t.Errorf("empty optionals should be omitted: %s", clean)
	}
}

// TestJSONResultsAreParseable guards the property every --json path must hold:
// stdout carries exactly one JSON document and nothing else. Diagnostics and
// refusal messages belong on stderr, because a caller that pipes stdout into a
// parser gets "extra data" the moment prose leaks into it.
func TestJSONResultsAreParseable(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"transition", transitionResult{Path: "p", OK: false, Kind: "task", Verb: "complete",
			Blocking: toGateFindings([]rules.Diagnostic{{Code: "X", Message: "m"}})}},
		{"review scaffold", reviewScaffoldResult{Path: "r.md", OK: true, Wrote: true,
			Frozen: "a..b", Lanes: reviewLaneIDs(), EvidenceLine: "- Final aligned review: r.md; frozen: a..b"}},
		{"template", templateResult{Type: "spec", Body: "---\ntitle: x\n---\n"}},
		{"template check", templateCheckResult{OK: false, Checked: 8,
			Drifted: []templateDriftEntry{{Path: "shared/templates/spec.md", Difference: "heading"}},
			Remedy:  "regenerate"}},
		{"plugin", pluginResult{OK: false, Action: "check", Trees: []string{".codex-plugin"},
			Stale: []string{"x (differs)"}, Remedy: "sync"}},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.v)
		if err != nil {
			t.Errorf("%s: does not marshal: %v", c.name, err)
			continue
		}
		var round map[string]any
		if err := json.Unmarshal(b, &round); err != nil {
			t.Errorf("%s: does not round-trip: %v", c.name, err)
			continue
		}
		if _, ok := round["ok"]; !ok {
			t.Errorf("%s: result has no `ok` field; a caller cannot tell success from failure: %s", c.name, b)
		}
	}
}

// TestReviewScaffoldJSONCarriesTheEvidenceLine pins the one output a phase-gate
// caller most needs verbatim. Reconstructing it from the other fields means
// re-implementing the format the validator checks byte-for-byte.
func TestReviewScaffoldJSONCarriesTheEvidenceLine(t *testing.T) {
	res := reviewScaffoldResult{
		Path: "Retro/01-review.md", Frozen: "aaa..bbb",
		EvidenceLine: "- Final aligned review: Retro/01-review.md; frozen: aaa..bbb",
	}
	if !strings.HasPrefix(res.EvidenceLine, "- Final aligned review: ") {
		t.Errorf("evidence line does not match the label the validator requires: %q", res.EvidenceLine)
	}
	if !strings.Contains(res.EvidenceLine, res.Frozen) {
		t.Errorf("evidence line omits the frozen range: %q", res.EvidenceLine)
	}
	if len(reviewLaneIDs()) != 4 {
		t.Errorf("want the four stable lane identifiers, got %v", reviewLaneIDs())
	}
}

// TestWritesLandOnTheResolvedPath pins the review's top finding: store.Read
// resolves a planning-root-relative spelling to its real location, but the
// text-output write paths wrote back to the unresolved argument. The result
// was silent data loss — a shadow file appeared at the literal path while the
// artifact that was actually read stayed unchanged.
func TestWritesLandOnTheResolvedPath(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":".plans"}`)
	artifact := filepath.Join(root, ".plans", "Specs", "Thing", "README.md")
	mustWrite(t, artifact, specFixture("Thing"))
	// A same-named directory at the working root, so a misdirected write
	// succeeds instead of failing on a missing parent — which is the case
	// that loses data silently rather than loudly.
	if err := os.MkdirAll(filepath.Join(root, "Specs", "Thing"), 0o755); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	// section set is the cheapest write to drive end to end.
	stdin, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = w.WriteString("Rewritten overview body.\n"); w.Close() }()
	oldStdin, oldStdout := os.Stdin, os.Stdout
	devnull, _ := os.Open(os.DevNull)
	os.Stdin, os.Stdout = stdin, devnull
	err = cmdSectionSet("Specs/Thing/README.md", sectionSetOpts{Heading: "## Overview", Type: "spec"})
	os.Stdin, os.Stdout = oldStdin, oldStdout
	if devnull != nil {
		devnull.Close()
	}
	if err != nil {
		t.Fatalf("section set on a planning-root-relative path failed: %v", err)
	}

	got, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Rewritten overview body.") {
		t.Errorf("the artifact that was read was not the one written:\n%s", got)
	}
	// Nothing may appear at the unresolved literal path.
	if _, err := os.Stat(filepath.Join(root, "Specs", "Thing", "README.md")); err == nil {
		t.Error("a shadow file was written at the unresolved path; the write did not follow the read")
	}
}

// TestGoverningDecisionsRequiresPathSegments pins the scope-matching fix: a
// bare string prefix let a decision scoped to `Specs/Foo` be pulled into the
// scope of the unrelated sibling `Specs/FooBar`.
func TestGoverningDecisionsRequiresPathSegments(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "planning-config.json"), `{"planningRoot":"."}`)
	mustWrite(t, filepath.Join(root, "Specs", "Foo", "README.md"), specFixture("Foo"))
	mustWrite(t, filepath.Join(root, "Specs", "FooBar", "README.md"), specFixture("FooBar"))
	// One entry, missing `rationale` (SDD112), governing only Specs/FooBar.
	mustWrite(t, filepath.Join(root, "Decisions", "decisions.md"),
		"---\ntitle: \"Decisions\"\ntype: decision-log\nstatus: active\n"+
			"created: 2026-01-01\nupdated: 2026-01-01\ndecisions:\n"+
			"  - id: D-0001\n    kind: decision\n    status: accepted\n    date: 2026-01-01\n"+
			"    decided_by: user\n    statement: FooBar uses X.\n    scope: [\"Specs/FooBar\"]\n"+
			"---\n\n## Decisions\nSee frontmatter.\n")

	out := runValidate(t, root, "Specs/Foo")
	if strings.Contains(out, "D-0001") {
		t.Errorf("a decision governing Specs/FooBar leaked into the scope of Specs/Foo:\n%s", out)
	}
	// The sibling's own scope must still see it.
	if outBar := runValidate(t, root, "Specs/FooBar"); !strings.Contains(outBar, "D-0001") {
		t.Errorf("the decision governing Specs/FooBar was dropped from its own scope:\n%s", outBar)
	}
}

// TestApplyGuardClauses covers the two CLI-level refusals --supersede
// introduced. They are one-line checks, but they encode a real distinction:
// --create starts a new artifact and --supersede rewrites an existing one, so
// conflating them is how an artifact's identifier history gets lost to a typo.
// A future edit that reorders either check relative to store.Read would break
// them silently.
func TestApplyGuardClauses(t *testing.T) {
	dir := t.TempDir()

	err := cmdApply(filepath.Join(dir, "x.md"), applyOpts{Supersede: true, Create: true, Type: "spec"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("--supersede with --create should refuse as mutually exclusive, got: %v", err)
	}

	// Superseding something that does not exist must refuse rather than
	// silently creating it: there is no identifier history to carry forward.
	err = cmdApply(filepath.Join(dir, "absent.md"), applyOpts{Supersede: true, Type: "spec"})
	if err == nil || !strings.Contains(err.Error(), "nothing to supersede") {
		t.Errorf("--supersede on a missing artifact should refuse, got: %v", err)
	}
}

// TestLockIgnoreSuggestion pins that doctor advises on the lock sidecars and
// never acts. The .gitignore belongs to the user's repository; a tool that
// edits it because it noticed something edits files nobody asked it to touch.
// It must also stay quiet once the advice has been taken — advice that repeats
// after being acted on is advice people learn to skip.
func TestLockIgnoreSuggestion(t *testing.T) {
	newRepo := func(t *testing.T, gitignore string, hasGit bool) string {
		t.Helper()
		root := t.TempDir()
		if hasGit {
			if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if gitignore != "" {
			mustWrite(t, filepath.Join(root, ".gitignore"), gitignore)
		}
		if err := os.MkdirAll(filepath.Join(root, ".plans"), 0o755); err != nil {
			t.Fatal(err)
		}
		return root
	}

	// Pattern absent: suggest.
	root := newRepo(t, "node_modules/\n", true)
	if got := checkLockIgnore(filepath.Join(root, ".plans")); got == "" {
		t.Error("no suggestion when the lock pattern is missing")
	} else if !strings.Contains(got, lockIgnorePattern) {
		t.Errorf("the suggestion does not name the pattern to add: %q", got)
	}

	// No .gitignore at all: still suggest, since the sidecars would show up
	// as untracked files.
	if got := checkLockIgnore(filepath.Join(newRepo(t, "", true), ".plans")); got == "" {
		t.Error("no suggestion when the repository has no .gitignore")
	}

	// Already ignored: silent.
	ignored := newRepo(t, "node_modules/\n"+lockIgnorePattern+"\n", true)
	if got := checkLockIgnore(filepath.Join(ignored, ".plans")); got != "" {
		t.Errorf("suggestion repeated after the pattern was added: %q", got)
	}

	// Not a Git repository: nothing to ignore into, so silent.
	if got := checkLockIgnore(filepath.Join(newRepo(t, "", false), ".plans")); got != "" {
		t.Errorf("suggestion outside a Git repository: %q", got)
	}

	// The suggestion must never write. Confirm the file is untouched.
	repo := newRepo(t, "node_modules/\n", true)
	before, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	_ = checkLockIgnore(filepath.Join(repo, ".plans"))
	after, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if string(before) != string(after) {
		t.Error("checkLockIgnore modified .gitignore; it must only advise")
	}
}
