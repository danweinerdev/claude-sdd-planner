package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
