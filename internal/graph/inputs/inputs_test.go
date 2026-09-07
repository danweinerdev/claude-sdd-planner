package inputs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

func TestRootsCannotFallBackToWorkingDirectory(t *testing.T) {
	for _, root := range []string{"", ".", "relative"} {
		_, err := safeJoin(root, "docs/design.md")
		if err == nil {
			t.Fatalf("root %q must not resolve from CWD", root)
		}
	}
}

func testRoots(t *testing.T) Roots {
	t.Helper()
	repo := t.TempDir()
	plan := filepath.Join(repo, "planning")
	if err := os.MkdirAll(plan, 0o755); err != nil {
		t.Fatal(err)
	}
	return Roots{Repository: repo, Planning: plan}
}

func writeFile(t *testing.T, roots Roots, root, rel, content string) {
	t.Helper()
	base, _ := baseFor(canonicalize(roots), root)
	path := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func spec(root, path string, headings ...string) model.Input {
	in := model.Input{Root: root, Path: path}
	if headings != nil {
		in.Section = &model.InputSection{HeadingPath: headings}
	}
	return in
}

func TestWholeFileTextNormalizesCRLF(t *testing.T) {
	roots := testRoots(t)
	writeFile(t, roots, "repository", "docs/readme.md", "line one\r\nline two\r\n")
	res, err := resolve(canonicalize(roots), spec("repository", "docs/readme.md"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Kind != File {
		t.Fatalf("kind = %s, want file", res.Kind)
	}
	if res.Text != "line one\nline two\n" {
		t.Fatalf("text = %q, want LF-normalized", res.Text)
	}
	if !strings.HasPrefix(res.Digest, "sha256:") {
		t.Fatalf("digest %q has no sha256 prefix", res.Digest)
	}
	// The CRLF and LF forms must fingerprint identically.
	writeFile(t, roots, "repository", "docs/lf.md", "line one\nline two\n")
	lf, err := resolve(canonicalize(roots), spec("repository", "docs/lf.md"))
	if err != nil {
		t.Fatalf("resolve lf: %v", err)
	}
	if lf.Digest != res.Digest {
		t.Fatalf("CRLF digest %s != LF digest %s; line endings must normalize", res.Digest, lf.Digest)
	}
}

func TestWholeFileBinaryRawSHA256(t *testing.T) {
	roots := testRoots(t)
	raw := []byte{0x00, 0x01, 0x02, 0xff, 0x00, '\r', '\n'}
	writeFile(t, roots, "repository", "bin/blob.bin", string(raw))
	res, err := resolve(canonicalize(roots), spec("repository", "bin/blob.bin"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !res.Binary {
		t.Fatalf("binary not detected")
	}
	if res.Text != "" {
		t.Fatalf("binary input carried text %q", res.Text)
	}
	want := sha256Hex(raw)
	if res.Digest != want {
		t.Fatalf("binary digest = %s, want raw sha256 %s", res.Digest, want)
	}
}

func TestSectionATX(t *testing.T) {
	roots := testRoots(t)
	doc := "# Title\n\nintro\n\n## Requirements\n\nreq body\n\n### Functional\n\nfun body\n\n### Non-Functional\n\nnfr body\n\n## Design\n\nlater\n"
	writeFile(t, roots, "repository", "docs/spec.md", doc)
	res, err := resolve(canonicalize(roots), spec("repository", "docs/spec.md", "Requirements"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Kind != Section {
		t.Fatalf("kind = %s, want section", res.Kind)
	}
	if !strings.Contains(res.Text, "req body") {
		t.Fatalf("section text missing body: %q", res.Text)
	}
	if strings.Contains(res.Text, "## Design") {
		t.Fatalf("section leaked past its sibling heading: %q", res.Text)
	}
	if !strings.Contains(res.Text, "### Functional") {
		t.Fatalf("section should include nested subsections: %q", res.Text)
	}
}

func TestSectionSetext(t *testing.T) {
	roots := testRoots(t)
	// Two H2 setext siblings: each ends at the next same-or-higher heading.
	doc := "Overview\n--------\n\noverview body\n\nDetails\n-------\n\ndetail body\n"
	writeFile(t, roots, "repository", "docs/prd.md", doc)
	res, err := resolve(canonicalize(roots), spec("repository", "docs/prd.md", "Overview"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(res.Text, "overview body") {
		t.Fatalf("setext section missing body: %q", res.Text)
	}
	if strings.Contains(res.Text, "detail body") {
		t.Fatalf("setext section leaked past sibling: %q", res.Text)
	}
	res2, err := resolve(canonicalize(roots), spec("repository", "docs/prd.md", "Details"))
	if err != nil {
		t.Fatalf("resolve Details: %v", err)
	}
	if !strings.Contains(res2.Text, "detail body") {
		t.Fatalf("setext Details missing body: %q", res2.Text)
	}
}

func TestFencedCodeHeadingsIgnored(t *testing.T) {
	roots := testRoots(t)
	doc := "## Real\n\nbody\n\n```\n## Fake\n### AlsoFake\n```\n\n## After\n"
	writeFile(t, roots, "repository", "docs/fenced.md", doc)
	// "Fake" must not resolve.
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/fenced.md", "Fake")); err == nil {
		t.Fatalf("fenced heading resolved; it must be ignored")
	}
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/fenced.md", "After")); err != nil {
		t.Fatalf("real heading after a fence must resolve: %v", err)
	}
}

func TestIndentedCodeHeadingIgnored(t *testing.T) {
	roots := testRoots(t)
	doc := "## Real\n\n    ## Fake\n\n## After\n"
	writeFile(t, roots, "repository", "docs/indented.md", doc)
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/indented.md", "Fake")); err == nil {
		t.Fatalf("indented heading resolved; it must be ignored")
	}
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/indented.md", "After")); err != nil {
		t.Fatalf("real heading after indented code must resolve: %v", err)
	}
}

func TestHeadingPathSuffixThroughAncestry(t *testing.T) {
	roots := testRoots(t)
	doc := "# A\n\n## B\n\n### C\n\ntarget\n\n## B2\n\n### C\n\nother\n"
	writeFile(t, roots, "repository", "docs/tree.md", doc)

	// ["B", "C"] is a unique suffix of C-under-B's ancestry chain.
	res, err := resolve(canonicalize(roots), spec("repository", "docs/tree.md", "B", "C"))
	if err != nil {
		t.Fatalf("resolve [B C]: %v", err)
	}
	if !strings.Contains(res.Text, "target") {
		t.Fatalf("wrong section selected: %q", res.Text)
	}

	// ["C"] alone is ambiguous (two C headings).
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/tree.md", "C")); err == nil {
		t.Fatalf("bare C resolved; it is ambiguous and must refuse")
	}

	// ["B2", "C"] selects the other.
	res2, err := resolve(canonicalize(roots), spec("repository", "docs/tree.md", "B2", "C"))
	if err != nil {
		t.Fatalf("resolve [B2 C]: %v", err)
	}
	if !strings.Contains(res2.Text, "other") {
		t.Fatalf("wrong section selected: %q", res2.Text)
	}

	// ["A", "C"] is NOT a contiguous ancestry chain (C's parent is B).
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/tree.md", "A", "C")); err == nil {
		t.Fatalf("[A C] resolved; ancestry is contiguous, so it must be missing")
	}
}

func TestMissingHeadingRefuses(t *testing.T) {
	roots := testRoots(t)
	writeFile(t, roots, "repository", "docs/x.md", "## Present\n")
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/x.md", "Absent")); err == nil {
		t.Fatalf("missing heading resolved; it must refuse")
	}
}

func TestPathEscapeRefuses(t *testing.T) {
	roots := testRoots(t)
	for _, path := range []string{"../outside.md", "docs/../../outside.md", "a/../b.md", ".."} {
		if _, err := resolve(canonicalize(roots), spec("repository", path)); err == nil {
			t.Fatalf("path %q resolved; it must refuse", path)
		}
	}
}

func TestAbsolutePathRefuses(t *testing.T) {
	roots := testRoots(t)
	if _, err := resolve(canonicalize(roots), spec("repository", "/etc/passwd")); err == nil {
		t.Fatalf("absolute path resolved; it must refuse")
	}
}

func TestSymlinkEscapeRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	roots := testRoots(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("## Secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, _ := baseFor(canonicalize(roots), "repository")
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(canonicalize(roots), spec("repository", "link/secret.md")); err == nil {
		t.Fatalf("symlink escape resolved; it must refuse")
	}
}

func TestDirectoryRefuses(t *testing.T) {
	roots := testRoots(t)
	repo, _ := baseFor(canonicalize(roots), "repository")
	if err := os.MkdirAll(filepath.Join(repo, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(canonicalize(roots), spec("repository", "adir")); err == nil {
		t.Fatalf("directory resolved; it must refuse")
	}
}

func TestSectionOnBinaryRefuses(t *testing.T) {
	roots := testRoots(t)
	writeFile(t, roots, "repository", "docs/blob.md", string([]byte{0x00, 0x01, 0x02}))
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/blob.md", "X")); err == nil {
		t.Fatalf("section on binary resolved; it must refuse")
	}
}

func TestSectionOnNonMarkdownRefuses(t *testing.T) {
	roots := testRoots(t)
	writeFile(t, roots, "repository", "src/main.go", "package main\n\n// ## not a heading\n")
	if _, err := resolve(canonicalize(roots), spec("repository", "src/main.go", "X")); err == nil {
		t.Fatalf("section on non-Markdown resolved; it must refuse")
	}
}

func TestRootSelectorDistinguishesRoots(t *testing.T) {
	roots := testRoots(t)
	writeFile(t, roots, "repository", "shared.md", "repo")
	writeFile(t, roots, "planning", "shared.md", "planning")
	repo, err := resolve(canonicalize(roots), spec("repository", "shared.md"))
	if err != nil {
		t.Fatalf("repository resolve: %v", err)
	}
	plan, err := resolve(canonicalize(roots), spec("planning", "shared.md"))
	if err != nil {
		t.Fatalf("planning resolve: %v", err)
	}
	if repo.Text != "repo" || plan.Text != "planning" {
		t.Fatalf("root selectors crossed: repo=%q plan=%q", repo.Text, plan.Text)
	}
	if repo.Digest == plan.Digest {
		t.Fatalf("distinct roots must fingerprint distinctly")
	}
}

// TestPRDOutsidePlanningRoot: a docs/PRD file that lives outside the planning
// root but inside the repository resolves under root "repository" and refuses
// under root "planning" — the whole point of the two explicit roots.
func TestPRDOutsidePlanningRoot(t *testing.T) {
	repo := t.TempDir()
	plan := filepath.Join(repo, "planning")
	if err := os.MkdirAll(plan, 0o755); err != nil {
		t.Fatal(err)
	}
	roots := Roots{Repository: repo, Planning: plan}
	writeFile(t, roots, "repository", "docs/prd.md", "# PRD\n\n## Scope\n\nscope body\n")

	// The PRD is under the repository root, outside the planning root.
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/prd.md", "Scope")); err != nil {
		t.Fatalf("PRD outside the planning root must resolve under the repository root: %v", err)
	}
	if _, err := resolve(canonicalize(roots), spec("planning", "docs/prd.md")); err == nil {
		t.Fatalf("the same path under the planning root must refuse (the PRD is not there)")
	}
}

// TestResolutionIndependentOfCWD: resolution uses the explicit roots, never the
// process working directory — the same declaration resolves identically from
// any CWD.
func TestResolutionIndependentOfCWD(t *testing.T) {
	roots := testRoots(t)
	writeFile(t, roots, "repository", "docs/x.md", "# X\n\nbody\n")
	spec := spec("repository", "docs/x.md")

	other := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	if _, err := resolve(canonicalize(roots), spec); err != nil {
		t.Fatalf("resolution must not depend on CWD: %v", err)
	}
}

// TestUnclosedFenceSwallowsHeadings: a fenced block never closed swallows
// every following heading — they are code, not structure.
func TestUnclosedFenceSwallowsHeadings(t *testing.T) {
	roots := testRoots(t)
	writeFile(t, roots, "repository", "docs/unclosed.md", "## Real\n\nbody\n\n```\n## Fake\n### AlsoFake\n")
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/unclosed.md", "Real")); err != nil {
		t.Fatalf("a heading before the fence must resolve: %v", err)
	}
	if _, err := resolve(canonicalize(roots), spec("repository", "docs/unclosed.md", "Fake")); err == nil {
		t.Fatalf("a heading inside an unclosed fence resolved; it must be ignored")
	}
}

func TestInputKeyStable(t *testing.T) {
	a := model.Input{Root: "repository", Path: "docs/readme.md"}
	b := model.Input{Root: "repository", Path: "docs/readme.md", Section: &model.InputSection{HeadingPath: []string{"A", "B"}}}
	if model.InputKey(a) == model.InputKey(b) {
		t.Fatalf("whole-file and section inputs share a key")
	}
	if model.InputKey(a) != `{"root":"repository","path":"docs/readme.md"}` {
		t.Fatalf("key = %q", model.InputKey(a))
	}
	if model.InputKey(b) != `{"root":"repository","path":"docs/readme.md","section":{"heading_path":["A","B"]}}` {
		t.Fatalf("section key = %q", model.InputKey(b))
	}
}
