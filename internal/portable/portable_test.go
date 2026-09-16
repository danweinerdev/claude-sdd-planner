package portable

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot resolves the repository root from this test file's location, so
// the integration tests run against the real canonical tree — the same
// philosophy as the regression corpus: the actual content is the fixture.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller info")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// TestGenerateRealTree is the leak gate: no Claude-specific mechanics may
// reach the portable tree. A hit here means a canonical edit introduced a
// Claude-ism that the transforms don't cover — fix it with a phrase rule, a
// marker block, or a .portable.md variant.
func TestGenerateRealTree(t *testing.T) {
	r, err := Generate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Files) == 0 {
		t.Fatal("no files generated")
	}

	// Every lifecycle skill must be present.
	for _, n := range []string{"brainstorm", "code-review", "debrief", "decide", "design",
		"implement", "plan", "poke-holes", "research", "setup", "specify", "validate"} {
		if _, ok := r.Files["skills/sdd-"+n+"/SKILL.md"]; !ok {
			t.Errorf("missing skills/sdd-%s/SKILL.md", n)
		}
	}
	for _, p := range []string{
		"plugin.json",
		"skills/sdd-test-design/SKILL.md",
		"skills/sdd-test-generate/SKILL.md",
		"skills/sdd-test-assess/SKILL.md",
		"shared/agent-runtime.md",
		"shared/frontmatter-schema.md",
		"shared/test-design.md",
		"shared/language-specs/go.md",
		"shared/agent-prompts/researcher.md",
		"shared/review-prompts/quality.md",
		"README.md",
	} {
		if _, ok := r.Files[p]; !ok {
			t.Errorf("missing %s", p)
		}
	}

	for _, p := range []string{
		"skills/sdd-test-design/SKILL.md",
		"skills/sdd-test-generate/SKILL.md",
		"skills/sdd-test-assess/SKILL.md",
		"shared/test-design.md",
	} {
		if !contains(r.Generated, p) {
			t.Errorf("%s does not have generated provenance", p)
		}
	}
	for _, p := range []string{
		"skills/sdd-test-design/SKILL.md",
		"skills/sdd-test-generate/SKILL.md",
		"skills/sdd-test-assess/SKILL.md",
	} {
		content := string(r.Files[p])
		if strings.Contains(content, "docs/TDD-TEST-DESIGN.md") {
			t.Errorf("%s retained canonical-only guide path", p)
		}
		if !strings.Contains(content, "shared/test-design.md") {
			t.Errorf("%s does not reference packaged guide", p)
		}
	}

	// Claude-isms that must never leak. "CLAUDE.md" and ".claude/" appear in
	// deliberate "do not use" sentences in portable-authored files, so they
	// are checked only in generated (transformed) files, not variants or
	// overrides.
	// shared/agent-runtime.md is the portable runtime spec: it names Claude
	// paths and slash commands solely to prohibit them, so it is exempt.
	leakExempt := map[string]bool{"shared/agent-runtime.md": true}
	leakTerms := []string{"sdd-planner:", "the Task tool", "~/.claude", "## Path Resolution"}
	for rel, content := range r.Files {
		if !strings.HasSuffix(rel, ".md") || leakExempt[rel] {
			continue
		}
		for _, term := range leakTerms {
			if strings.Contains(string(content), term) {
				t.Errorf("%s: leaked %q into portable tree", rel, term)
			}
		}
	}
	generated := map[string]bool{}
	for _, g := range r.Generated {
		generated[g] = true
	}
	for rel, content := range r.Files {
		if !generated[rel] || !strings.HasSuffix(rel, ".md") || leakExempt[rel] {
			continue
		}
		for _, term := range []string{"CLAUDE.md", "claude-md-full", "slash command"} {
			if strings.Contains(string(content), term) {
				t.Errorf("%s: generated file leaked %q", rel, term)
			}
		}
	}

	// Claude-only templates must not cross over.
	for _, p := range []string{"shared/templates/claude-md-full.md", "shared/templates/claude-md-snippet.md"} {
		if _, ok := r.Files[p]; ok {
			t.Errorf("%s crossed into the portable tree", p)
		}
	}

	// Manifest carries the canonical version.
	version, minSdd, err := canonicalVersion(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(r.Files["plugin.json"])
	if !strings.Contains(manifest, `"version": "`+version+`"`) {
		t.Errorf("manifest version not synced to canonical %s:\n%s", version, manifest)
	}
	if !strings.Contains(manifest, `"minSddVersion": "`+minSdd+`"`) {
		t.Errorf("manifest minSddVersion not synced to canonical %s", minSdd)
	}
}

func TestGenerateTestCompositionResources(t *testing.T) {
	r, err := Generate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	guide, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "TDD-TEST-DESIGN.md"))
	if err != nil {
		t.Fatal(err)
	}
	wantGuide, err := transformDoc(string(guide), "docs/TDD-TEST-DESIGN.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(r.Files["shared/test-design.md"]); got != wantGuide {
		t.Error("packaged test-design guide is not the transformed canonical document")
	}

	wants := map[string][]string{
		"skills/sdd-test-design/SKILL.md": {
			"## Resources", "caller's resolved active plugin root", "skills/sdd-test-design/SKILL.md", "shared/test-design.md", "Never resolve resources from the target repository's current working directory",
			"## Context", "## Behavior and source", "## Risk and level", "## Existing coverage",
			"## Cases and oracle", "## Subject and seam", "## Red and sensitivity", "## Execution",
		},
		"skills/sdd-test-generate/SKILL.md": {
			"## Resources", "skills/sdd-test-generate/SKILL.md", "minimal interface-only stub", "Interface-only production stubs:",
			"## Files changed", "## Test identities", "## Scaffolding introduced", "## Unresolved findings",
		},
		"skills/sdd-test-assess/SKILL.md": {
			"## Resources", "skills/sdd-test-assess/SKILL.md", "valid isolated sensitivity run", "deliberate compiler-rejection harness test", "repository-produced metadata and deterministic validator facts",
			"## Assessment", "Assessment: ready", "Assessment: changes-required", "Assessment: blocked", "## Located findings", "## Next action",
		},
	}
	for rel, required := range wants {
		content := string(r.Files[rel])
		for _, text := range required {
			if !strings.Contains(content, text) {
				t.Errorf("%s missing composition contract %q", rel, text)
			}
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestNoRetiredDecisionCitations keeps the retired global-ledger D-NNNN id
// family out of active guidance. Its deliberately narrow canonical roots omit
// frozen planning artifacts and historical provenance/data; those records must
// preserve the identifiers they actually carried. Generated portable Markdown
// is checked separately so a transform or portable variant cannot reintroduce
// a dangling citation.
func TestNoRetiredDecisionCitations(t *testing.T) {
	retiredCitation := regexp.MustCompile(`\bD-[0-9]{4}\b`)
	root := repoRoot(t)

	check := func(label, rel string, content []byte) {
		t.Helper()
		if retiredCitation.Match(content) {
			t.Errorf("%s %s contains retired decision citation %q", label, rel, retiredCitation.Find(content))
		}
	}

	for _, rel := range []string{"README.md", "CLAUDE.md", "AGENTS.md"} {
		content, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		check("canonical", rel, content)
	}
	for _, dir := range []string{"commands", "skills", "shared", "agents"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			check("canonical", filepath.ToSlash(rel), content)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	generated, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range generated.Files {
		if strings.HasSuffix(rel, ".md") {
			check("portable", rel, content)
		}
	}
}

// TestCheckClean requires the committed portable trees (.codex-plugin/ and
// .opencode-plugin/) to match a fresh generation — the drift gate that keeps
// hand edits and forgotten syncs out of the repository. Wired into
// `make test` via `go test ./...`.
func TestCheckClean(t *testing.T) {
	stale, err := Check(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stale {
		t.Errorf("stale: %s", s)
	}
	if len(stale) > 0 {
		t.Log("run `sdd plugin sync` (or `make plugins`) and commit the result")
	}
}

// TestHeadingSpacing lints every generated markdown file for a heading that
// directly follows a non-blank line — the rendering defect a marker block
// without a trailing blank line produces (strict CommonMark does not promise
// a heading renders as one without a preceding blank line). Fence-aware so
// `# comments` inside code blocks don't false-positive.
func TestHeadingSpacing(t *testing.T) {
	r, err := Generate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range r.Files {
		if !strings.HasSuffix(rel, ".md") {
			continue
		}
		lines := strings.Split(string(content), "\n")
		inFence := false
		inComment := false
		inFrontmatter := strings.HasPrefix(string(content), "---\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if inFrontmatter {
				if i > 0 && trimmed == "---" {
					inFrontmatter = false
				}
				continue // YAML comments are #-lines too
			}
			if inComment {
				if strings.Contains(line, "-->") {
					inComment = false
				}
				continue
			}
			if strings.Contains(line, "<!--") && !strings.Contains(line, "-->") {
				inComment = true
				continue
			}
			if strings.HasPrefix(trimmed, "```") {
				inFence = !inFence
				continue
			}
			if inFence || i == 0 {
				continue
			}
			if strings.HasPrefix(line, "#") {
				// Heading-shaped: one to six #'s then a space.
				hashes := 0
				for hashes < len(line) && line[hashes] == '#' {
					hashes++
				}
				if hashes == 0 || hashes > 6 || hashes >= len(line) || line[hashes] != ' ' {
					continue
				}
				prev := strings.TrimSpace(lines[i-1])
				if prev != "" && !strings.HasPrefix(prev, "#") {
					t.Errorf("%s:%d: heading %q directly follows non-blank line %q — needs a blank line", rel, i+1, line, lines[i-1])
				}
			}
		}
	}
}
