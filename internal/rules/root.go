package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// artifactDirs mirrors sdd_validate.py's ARTIFACT_DIRS: the top-level
// directories under a planning root that are walked for `*.md` artifacts.
var artifactDirs = []string{"Research", "Brainstorm", "Specs", "Designs", "Plans", "Decisions", "Retro", "Diagrams"}

// Root is every artifact discovered under a planning root, plus the index
// rules need to resolve cross-artifact references.
type Root struct {
	Dir       string
	Artifacts []*Artifact
	ByPath    map[string]*Artifact // Rel -> Artifact, for successfully parsed artifacts only

	// RepoRoot is the repository every artifact targets by default (Python's
	// Validator.repo): the directory planning-config.json lives beside. Set by
	// LoadRoot to Dir; LoadRootRepo lets a caller (cmd/sdd) supply the real one
	// when the planning root and the repository differ.
	RepoRoot string
	// PlanRepos maps a plan name to its mapped target repository's absolute
	// directory, populated by SDD000's CheckRoot from planning-config.json.
	// RepoForArtifact reads this; a plan absent here targets RepoRoot.
	PlanRepos map[string]string
	// ConfigDiagnostics are the SDD000 findings from parsing
	// RepoRoot/planning-config.json, computed once up front (mirroring
	// Python's Validator.__init__ calling _configure_repositories
	// immediately) so every other rule sees a populated PlanRepos.
	ConfigDiagnostics []Diagnostic

	// bareDiagnostics memoizes runBare's result for this Root. The waiver
	// rules (SDD176/SDD177) each need the full non-waiver rule sweep to know
	// which codes actually fire, and each was re-running it: `sdd validate`
	// evaluated every rule THREE times, and a lifecycle transition — which
	// validates before and after — six. The sweep is a pure function of the
	// loaded Root, which is never mutated after loading, so one evaluation
	// serves every caller.
	bareDiagnostics []Diagnostic
	bareComputed    bool
}

// Artifact mirrors the Python validator's Artifact dataclass plus the parse
// failure it would otherwise short-circuit on. A file that fails to parse
// still gets an *Artifact* (so the _parse family's rules can run over it),
// but ParseStage is nonzero and Meta/Body are unset.
type Artifact struct {
	Rel     string // planning-root-relative, forward slashes
	AbsPath string

	// Populated by the discovery step, in Python's _parse order. ParseStage is
	// "" for a fully parsed artifact, or the code of the parse failure that
	// stopped modeling: SDD002 (unreadable), SDD003 (CRLF, non-fatal: parsing
	// continues), SDD004 (no opening delimiter), SDD005 (no closing
	// delimiter), SDD006 (invalid YAML), SDD007 (frontmatter not a mapping).
	ParseStage  string
	ParseDetail string // for SDD002/SDD006: the underlying error text
	ParseLine   int
	HasCRLF     bool

	Source   string   // full decoded file content, as read
	Lines    []string // Source split on "\n", each element without its own trailing "\n"
	Meta     map[string]any
	MetaRaw  []string // raw frontmatter lines, between the `---` delimiters
	Body     string
	BodyLine int // 1-indexed source line of the first body line

	// definedIDs memoizes specDefinedIDs for this artifact. SDD122 queries it
	// once per citing artifact per identifier family, so on a large root the
	// same full-body regex scan ran hundreds of times. An Artifact is
	// immutable once loaded, so the first scan is the only one needed.
	definedIDs map[string]map[string]bool
	// sectionCache memoizes sections(a, level) per heading depth, for the same
	// reason: many rules ask for the same artifact's sections.
	sectionCache map[int]map[string]sectionInfo
}

// Kind returns the `type:` frontmatter field, or "" when absent/non-string.
func (a *Artifact) Kind() string {
	if v, ok := a.Meta["type"].(string); ok {
		return v
	}
	return ""
}

// Status returns the `status:` frontmatter field, or "" when absent/non-string.
func (a *Artifact) Status() string {
	if v, ok := a.Meta["status"].(string); ok {
		return v
	}
	return ""
}

// Line finds the 1-indexed line of the first occurrence of text, searching
// Source when body is false or Body when body is true, mirroring the Python
// Artifact.line() helper.
func (a *Artifact) Line(text string, body bool) int {
	if !body {
		for i, l := range a.Lines {
			if strings.Contains(l, text) {
				return i + 1
			}
		}
		return 1
	}
	offset := a.BodyLine - 1
	for i, l := range splitKeepLines(a.Body) {
		if strings.Contains(l, text) {
			return i + 1 + offset
		}
	}
	return 1
}

func splitKeepLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	// splitlines() semantics: a trailing "\n" does not produce a final empty
	// element.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// LoadRoot walks a planning root's artifact directories and models every
// `*.md` file found, in Python's _discover order (sorted paths). The
// repository every artifact targets by default (Python's Validator.repo) is
// dir itself; use LoadRootRepo when the planning root and the repository
// housing planning-config.json differ.
func LoadRoot(dir string) (*Root, error) {
	return LoadRootRepo(dir, dir)
}

// LoadRootRepo is LoadRoot with an explicit repository directory — the
// directory planning-config.json lives beside, Python's Validator.repo —
// distinct from the planning root when a plan's target repository is not the
// one holding the planning artifacts.
func LoadRootRepo(dir, repoRoot string) (*Root, error) {
	r := &Root{Dir: dir, ByPath: map[string]*Artifact{}, RepoRoot: repoRoot}
	r.PlanRepos, r.ConfigDiagnostics = configureRepositories(repoRoot, dir)
	var paths []string
	for _, name := range artifactDirs {
		base := filepath.Join(dir, name)
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.Walk(base, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".md") {
				paths = append(paths, p)
			}
			return nil
		})
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)
		a := parseArtifact(p, rel)
		r.Artifacts = append(r.Artifacts, a)
		if a.ParseStage == "" || a.ParseStage == "SDD003" {
			r.ByPath[rel] = a
		}
	}
	return r, nil
}

func parseArtifact(path, rel string) *Artifact {
	a := &Artifact{Rel: rel, AbsPath: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		a.ParseStage = "SDD002"
		a.ParseDetail = err.Error()
		return a
	}
	return parseArtifactBytes(raw, rel, path)
}

// ParseArtifactBytes builds the same Artifact model LoadRoot produces from a
// live file, but from raw bytes instead — this is Prerequisite B (historical
// artifact reconstruction): the evidence rules that verify a phase/plan's
// completion evidence was committed use a vcs.Repo.FileAt(rev, path) to fetch
// an artifact's bytes as of a revision, then need to inspect it with exactly
// the same section/frontmatter helpers a live artifact uses. rel is the
// planning-root-relative path the historical bytes are being modeled as; no
// on-disk path exists for a historical artifact, so AbsPath is left empty.
func ParseArtifactBytes(raw []byte, rel string) *Artifact {
	return parseArtifactBytes(raw, rel, "")
}

func parseArtifactBytes(raw []byte, rel, path string) *Artifact {
	a := &Artifact{Rel: rel, AbsPath: path}
	if !utf8.Valid(raw) {
		a.ParseStage = "SDD002"
		a.ParseDetail = "invalid UTF-8"
		return a
	}
	source := string(raw)
	a.Source = source
	if strings.Contains(source, "\r\n") {
		a.HasCRLF = true
	}
	a.Lines = splitKeepLines(source)

	lines := strings.Split(source, "\n")
	// Reattach the trailing "\n" convention: work with raw lines including
	// the possibility of a final line without one; splitlines(keepends) is
	// approximated closely enough for delimiter scanning by trimming.
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		a.ParseStage = "SDD004"
		return a
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		a.ParseStage = "SDD005"
		return a
	}
	a.MetaRaw = lines[1:end]
	meta, mErr, mLine := parseFrontmatter(a.MetaRaw)
	if mErr != "" {
		a.ParseStage = "SDD006"
		a.ParseDetail = mErr
		a.ParseLine = mLine + 2 // +2: MetaRaw is offset by the opening `---` line, and lines are 1-indexed
		return a
	}
	if meta == nil {
		a.ParseStage = "SDD007"
		return a
	}
	a.Meta = meta
	a.Body = strings.Join(lines[end+1:], "\n")
	a.BodyLine = end + 2
	if a.HasCRLF {
		// Non-fatal: parsing continues, but SDD003 must still fire.
		a.ParseStage = "SDD003"
	}
	return a
}

// parseFrontmatter decodes an artifact's frontmatter into a generic map using
// a real YAML parser (gopkg.in/yaml.v3).
//
// The three-way return is the contract the caller's diagnostics depend on:
//   - (map, "", 0)  — parsed as a mapping.
//   - (nil, "", 0)  — valid YAML that is empty or is not a mapping (SDD007).
//   - (nil, msg, n) — malformed YAML; n is the 0-indexed line within lines
//     that caused it (SDD006).
//
// Values are normalized to the shapes the rules expect: scalars as strings
// (except bools, which stay bool), sequences as []any, and nested mappings as
// map[string]any. Scalar-to-string normalization matters because rules compare
// against string literals ("2024-01-01", "1"), and YAML would otherwise hand
// back time.Time and int for those.
func parseFrontmatter(lines []string) (map[string]any, string, int) {
	src := strings.Join(lines, "\n")

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(src), &root); err != nil {
		return nil, yamlErrMessage(err), yamlErrLine(err)
	}
	// An empty document yields a Node with no content.
	if root.Kind == 0 || len(root.Content) == 0 {
		return nil, "", 0
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, "", 0 // valid YAML, but not a mapping — SDD007's trigger.
	}
	m, ok := nodeToAny(doc).(map[string]any)
	if !ok {
		return nil, "", 0
	}
	return m, "", 0
}

// nodeToAny converts a yaml.Node tree into the generic shapes the rules
// consume. Scalars become strings so that dates, numbers, and versions compare
// as written, with booleans kept as bool since rules test them as such.
func nodeToAny(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
		}
		return nodeToAny(n.Content[0])
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = nodeToAny(n.Content[i+1])
		}
		return out
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, nodeToAny(c))
		}
		return out
	case yaml.AliasNode:
		if n.Alias != nil {
			return nodeToAny(n.Alias)
		}
		return ""
	default: // scalar
		return scalarToAny(n)
	}
}

// scalarToAny maps a scalar node to bool for an unquoted YAML boolean, the
// empty string for null, and the literal source text otherwise.
func scalarToAny(n *yaml.Node) any {
	// A quoted scalar is always a string, even if it reads like a bool.
	if n.Style == yaml.SingleQuotedStyle || n.Style == yaml.DoubleQuotedStyle {
		return n.Value
	}
	switch n.Tag {
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err == nil {
			return b
		}
	case "!!null":
		return ""
	}
	return n.Value
}

var yamlLineRe = regexp.MustCompile(`^yaml: line (\d+): `)

// yamlErrMessage renders a parse failure using the wording the validator has
// always used, so the diagnostic text stays stable.
func yamlErrMessage(err error) string {
	msg := err.Error()
	if len(msg) > 0 {
		msg = yamlLineRe.ReplaceAllString(msg, "")
		msg = strings.TrimPrefix(msg, "yaml: ")
	}
	return "could not parse frontmatter: " + msg
}

// yamlErrLine extracts the 0-indexed line yaml.v3 blames, defaulting to the
// first line when the error carries no position.
func yamlErrLine(err error) int {
	if m := yamlLineRe.FindStringSubmatch(err.Error()); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil && n > 0 {
			return n - 1
		}
	}
	if te, ok := err.(*yaml.TypeError); ok && len(te.Errors) > 0 {
		if m := yamlLineRe.FindStringSubmatch(te.Errors[0]); m != nil {
			if n, convErr := strconv.Atoi(m[1]); convErr == nil && n > 0 {
				return n - 1
			}
		}
	}
	return 0
}

func unquoteScalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func leadingSpaces(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}
