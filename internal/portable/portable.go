package portable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// OutDir is the generated tree, relative to the repository root.
const OutDir = "portable"

// OverrideDir holds hand-maintained files that replace or extend the
// generated output, mirroring the portable/ layout. Every file here is either
// portable-only content with no canonical source (e.g. shared/agent-runtime.md)
// or a divergence that has not yet been converged into the canonical tree.
// The override list that `sdd plugin sync` prints is therefore the
// convergence backlog, and an empty override directory is the finish line.
const OverrideDir = "portable-overrides"

// languages are the per-language reference skills that flatten into
// shared/language-specs/ in the portable tree.
var languages = []string{"cpp", "go", "java", "python", "rust", "swift", "typescript"}

// claudeOnlyShared are canonical shared/ files that make no sense in the
// portable tree (their portable counterparts, agents-md-*.md, live in
// overrides until the pair is converged into one marker-managed template).
var claudeOnlyShared = map[string]bool{
	"templates/claude-md-full.md":    true,
	"templates/claude-md-snippet.md": true,
}

// Result is one generated portable tree plus its provenance report.
type Result struct {
	Files map[string][]byte // relpath under portable/ -> content

	Generated    []string // produced by transformation from the canonical tree
	Variants     []string // a hand-maintained .portable.md sibling won
	Overridden   []string // canonical source exists, but an override file won
	OverrideOnly []string // no canonical source; the override is the content
}

// manifestTemplate is the portable plugin manifest (Codex plugin.json; the
// OpenCode install path discovers skills/ directly and needs no manifest).
// %s is the version, taken from the canonical .claude-plugin/plugin.json so
// `make bump-*` moves both trees together.
const manifestTemplate = `{
  "name": "sdd-planner",
  "version": %q,
  "minSddVersion": %q,
  "description": "Spec-driven development planning skills for research, requirements, designs, implementation plans, execution, review, debriefs, and validation.",
  "author": {
    "name": "Daniel Weiner"
  },
  "repository": "https://github.com/danweinerdev/claude-sdd-planner",
  "license": "MIT",
  "keywords": ["planning", "specification", "development", "review"],
  "skills": "./skills/",
  "interface": {
    "displayName": "SDD Planner",
    "shortDescription": "Spec-driven development lifecycle skills",
    "longDescription": "Create and maintain research, specifications, designs, implementation plans, reviews, debriefs, and completion evidence as structured Markdown artifacts.",
    "developerName": "Daniel Weiner",
    "category": "Productivity",
    "capabilities": ["Interactive", "Write"],
    "websiteURL": "https://github.com/danweinerdev/claude-sdd-planner",
    "defaultPrompt": [
      "Set up spec-driven planning for this repository.",
      "Create a specification and implementation plan for this feature.",
      "Review this implementation against its active plan."
    ]
  }
}
`

// variantSuffix marks a hand-maintained portable variant that replaces the
// generated transform of its sibling. commands/code-review/SKILL.portable.md
// shadows the transform of commands/code-review/SKILL.md, and
// shared/x.portable.md shadows shared/x.md — used where the two harnesses
// genuinely need different documents (dispatch contracts, resolution specs)
// rather than the same document reworded. The variant lives next to its
// canonical sibling so an edit to one is an in-your-face reminder to revisit
// the other.
const variantSuffix = ".portable.md"

// variantFor returns the variant content for a canonical file if a
// .portable.md sibling exists.
func variantFor(canonicalPath string) ([]byte, bool, error) {
	v := strings.TrimSuffix(canonicalPath, ".md") + variantSuffix
	raw, err := os.ReadFile(v)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

// Generate builds the portable tree in memory from the canonical tree at
// repoRoot, then applies overrides.
func Generate(repoRoot string) (*Result, error) {
	r := &Result{Files: map[string][]byte{}}

	version, minSdd, err := canonicalVersion(repoRoot)
	if err != nil {
		return nil, err
	}
	r.Files[".codex-plugin/plugin.json"] = []byte(fmt.Sprintf(manifestTemplate, version, minSdd))
	r.Generated = append(r.Generated, ".codex-plugin/plugin.json")

	// Lifecycle skills: commands/<name>/SKILL.md -> skills/sdd-<name>/SKILL.md
	cmdDirs, err := os.ReadDir(filepath.Join(repoRoot, "commands"))
	if err != nil {
		return nil, fmt.Errorf("reading commands/: %w", err)
	}
	for _, d := range cmdDirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		src := filepath.Join(repoRoot, "commands", name, "SKILL.md")
		rel := path.Join("skills", "sdd-"+name, "SKILL.md")
		if v, ok, err := variantFor(src); err != nil {
			return nil, err
		} else if ok {
			r.Files[rel] = v
			r.Variants = append(r.Variants, rel)
			continue
		}
		raw, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		out, err := transformSkill(string(raw), name, "commands/"+name+"/SKILL.md")
		if err != nil {
			return nil, err
		}
		r.Files[rel] = []byte(out)
		r.Generated = append(r.Generated, rel)
	}

	// Model-only decision-log skill keeps skill form in the portable tree.
	dlSrc := filepath.Join(repoRoot, "skills", "decision-log", "SKILL.md")
	if v, ok, err := variantFor(dlSrc); err != nil {
		return nil, err
	} else if ok {
		r.Files["skills/sdd-decision-log/SKILL.md"] = v
		r.Variants = append(r.Variants, "skills/sdd-decision-log/SKILL.md")
	} else if raw, err := os.ReadFile(dlSrc); err == nil {
		out, err := transformSkill(string(raw), "decision-log", "skills/decision-log/SKILL.md")
		if err != nil {
			return nil, err
		}
		rel := "skills/sdd-decision-log/SKILL.md"
		r.Files[rel] = []byte(out)
		r.Generated = append(r.Generated, rel)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// Language reference skills flatten to shared/language-specs/<lang>.md.
	for _, lang := range languages {
		src := filepath.Join(repoRoot, "skills", lang+"-specifications", "SKILL.md")
		raw, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		out, err := transformLangSpec(string(raw), "skills/"+lang+"-specifications/SKILL.md")
		if err != nil {
			return nil, err
		}
		rel := path.Join("shared", "language-specs", lang+".md")
		r.Files[rel] = []byte(out)
		r.Generated = append(r.Generated, rel)
	}

	// Shared docs and templates.
	sharedRoot := filepath.Join(repoRoot, "shared")
	err = filepath.WalkDir(sharedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(sharedRoot, p)
		rel = filepath.ToSlash(rel)
		if claudeOnlyShared[rel] || strings.HasSuffix(rel, variantSuffix) {
			return nil
		}
		outRel := path.Join("shared", rel)
		if strings.HasSuffix(rel, ".md") {
			if v, ok, err := variantFor(p); err != nil {
				return err
			} else if ok {
				r.Files[outRel] = v
				r.Variants = append(r.Variants, outRel)
				return nil
			}
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".md") {
			out, err := transformDoc(string(raw), "shared/"+rel)
			if err != nil {
				return err
			}
			r.Files[outRel] = []byte(out)
		} else {
			r.Files[outRel] = raw
		}
		r.Generated = append(r.Generated, outRel)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Overrides win wholesale.
	ovRoot := filepath.Join(repoRoot, OverrideDir)
	if _, statErr := os.Stat(ovRoot); statErr == nil {
		err = filepath.WalkDir(ovRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(ovRoot, p)
			rel = filepath.ToSlash(rel)
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if _, exists := r.Files[rel]; exists {
				r.Overridden = append(r.Overridden, rel)
				r.Generated = remove(r.Generated, rel)
				r.Variants = remove(r.Variants, rel)
			} else {
				r.OverrideOnly = append(r.OverrideOnly, rel)
			}
			r.Files[rel] = raw
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(r.Generated)
	sort.Strings(r.Variants)
	sort.Strings(r.Overridden)
	sort.Strings(r.OverrideOnly)
	return r, nil
}

func remove(list []string, v string) []string {
	out := list[:0]
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

func canonicalVersion(repoRoot string) (version, minSdd string, err error) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".claude-plugin", "plugin.json"))
	if err != nil {
		return "", "", fmt.Errorf("reading canonical manifest: %w", err)
	}
	var m struct {
		Version       string `json:"version"`
		MinSddVersion string `json:"minSddVersion"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", "", fmt.Errorf("parsing canonical manifest: %w", err)
	}
	if m.Version == "" || m.MinSddVersion == "" {
		return "", "", fmt.Errorf("canonical manifest is missing version or minSddVersion")
	}
	return m.Version, m.MinSddVersion, nil
}

// Sync writes the generated tree to <repoRoot>/portable, replacing whatever
// is there. The tree is fully generated, so a clean rewrite is correct — any
// hand edit to portable/ is a bug by definition.
func Sync(repoRoot string) (*Result, error) {
	r, err := Generate(repoRoot)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(repoRoot, OutDir)
	if err := os.RemoveAll(out); err != nil {
		return nil, err
	}
	for rel, content := range r.Files {
		dst := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Check regenerates in memory and compares against the on-disk portable/
// tree. It returns the list of stale paths (missing, differing, or orphaned)
// without writing anything.
func Check(repoRoot string) (stale []string, err error) {
	r, err := Generate(repoRoot)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(repoRoot, OutDir)
	seen := map[string]bool{}
	if _, statErr := os.Stat(out); statErr == nil {
		err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(out, p)
			rel = filepath.ToSlash(rel)
			seen[rel] = true
			want, ok := r.Files[rel]
			if !ok {
				stale = append(stale, rel+" (orphan: no canonical source)")
				return nil
			}
			got, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, want) {
				stale = append(stale, rel+" (differs from generated content)")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for rel := range r.Files {
		if !seen[rel] {
			stale = append(stale, rel+" (missing from portable/)")
		}
	}
	sort.Strings(stale)
	return stale, nil
}
