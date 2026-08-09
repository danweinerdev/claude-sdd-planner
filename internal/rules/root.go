package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/danweinerdev/claude-sdd-planner/internal/ymlite"
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

var topKeyRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*):(.*)$`)

// parseFrontmatter decodes the bounded YAML subset this repo's frontmatter
// uses (see internal/ymlite's package doc) into a generic map. It returns a
// nil map with no error when the frontmatter is empty or is not a mapping
// (SDD007's trigger); it returns an error string and the 0-indexed line
// within lines that caused it when the content cannot be modeled at all
// (SDD006's trigger).
func parseFrontmatter(lines []string) (map[string]any, string, int) {
	meta := map[string]any{}
	sawAnyKey := false
	sawNonMapping := false
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimRight(line, "\r")
		stripped := strings.TrimSpace(trimmed)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			i++
			continue
		}
		if leadingSpaces(trimmed) > 0 {
			// An indented line at a position we didn't expect (e.g. stray
			// continuation). Best-effort: skip it rather than fail outright.
			i++
			continue
		}
		if strings.HasPrefix(stripped, "-") {
			sawNonMapping = true
			i++
			continue
		}
		m := topKeyRe.FindStringSubmatch(trimmed)
		if m == nil {
			return nil, "could not parse frontmatter line: " + trimmed, i
		}
		key, rest := m[1], strings.TrimSpace(m[2])
		sawAnyKey = true
		if rest == "" {
			// Either a block sequence follows, or the value is empty.
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j < len(lines) && leadingSpaces(lines[j]) > 0 {
				start, end, found := ymlite.Block(lines, key)
				if found {
					items, errMsg, errOffset := parseBlockSequence(lines[start:end])
					if errMsg != "" {
						return nil, errMsg, start + errOffset
					}
					meta[key] = items
					i = end
					continue
				}
			}
			meta[key] = ""
			i++
			continue
		}
		if strings.HasPrefix(rest, "[") {
			it := ymlite.Item{key: rest}
			list := it.List(key)
			out := make([]any, len(list))
			for k, v := range list {
				out[k] = v
			}
			meta[key] = out
			i++
			continue
		}
		if !quotedScalarOK(rest) {
			return nil, "could not parse frontmatter line: " + trimmed, i
		}
		meta[key] = unquoteScalar(rest)
		i++
	}
	if sawNonMapping && !sawAnyKey {
		return nil, "", 0 // signalled via nil map below
	}
	if !sawAnyKey {
		return nil, "", 0
	}
	return meta, "", 0
}

var blockKVRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):\s?(.*)$`)

// parseBlockSequence decodes a block sequence's raw lines (as ymlite.Block
// bounds them) into a slice whose elements are either map[string]any (a
// mapping entry, e.g. one phases[]/tasks[]/decisions[] item) or string (a
// bare scalar entry, e.g. `- some-string`). ymlite.Items always models an
// entry as a flat map and silently drops a dash line that isn't `key:
// value` shaped, which is right for this repo's structured sequences but
// wrong for detecting the "not a mapping" case rules like SDD053 need.
func parseBlockSequence(block []string) ([]any, string, int) {
	var items []any
	var cur map[string]string
	var curScalar string
	curIsMap := false
	haveCur := false
	fieldIndent := -1
	errMsg := ""
	errOffset := 0

	flush := func() {
		if !haveCur {
			return
		}
		if curIsMap {
			items = append(items, itemToMap(ymlite.Item(cur)))
		} else {
			items = append(items, curScalar)
		}
		cur, curScalar, curIsMap, haveCur = nil, "", false, false
	}

	for lineIdx, line := range block {
		if errMsg != "" {
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingSpaces(line)
		rest := line[indent:]
		if strings.HasPrefix(rest, "- ") || rest == "-" {
			flush()
			cur = map[string]string{}
			haveCur = true
			if fieldIndent == -1 {
				fieldIndent = indent + 2
			}
			content := strings.TrimPrefix(strings.TrimPrefix(rest, "-"), " ")
			if content != "" {
				if strings.HasPrefix(content, "{") && strings.HasSuffix(strings.TrimSpace(content), "}") {
					cur = parseFlowMapping(content)
					curIsMap = true
				} else if m := blockKVRe.FindStringSubmatch(content); m != nil {
					v := strings.TrimRight(m[2], " \t")
					if !quotedScalarOK(v) {
						errMsg = "could not parse frontmatter line: " + strings.TrimRight(line, " \t")
						errOffset = lineIdx
						continue
					}
					cur[m[1]] = v
					curIsMap = true
				} else {
					curScalar = unquoteScalar(content)
				}
			}
			continue
		}
		if haveCur && indent >= fieldIndent {
			if m := blockKVRe.FindStringSubmatch(rest); m != nil {
				v := strings.TrimRight(m[2], " \t")
				if !quotedScalarOK(v) {
					errMsg = "could not parse frontmatter line: " + strings.TrimRight(line, " \t")
					errOffset = lineIdx
					continue
				}
				cur[m[1]] = v
				curIsMap = true
			}
		}
	}
	flush()
	return items, errMsg, errOffset
}

// parseFlowMapping decodes a single-line YAML flow mapping (`{ k: v, k2: v2
// }`), which this repo's frontmatter uses for compact `phases:` entries. It
// does not support nested flow collections as a value — none of this
// codebase's frontmatter needs them.
func parseFlowMapping(s string) map[string]string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	out := map[string]string{}
	for _, field := range splitFlowFields(s) {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		k, v, ok := strings.Cut(field, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = unquoteScalar(strings.TrimSpace(v))
	}
	return out
}

// splitFlowFields splits a flow mapping's inner content on top-level commas,
// treating commas inside a quoted scalar as literal.
func splitFlowFields(s string) []string {
	var out []string
	start := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// quotedScalarOK reports whether a frontmatter scalar that begins with a
// quote character is a well-formed YAML quoted scalar: the quote is closed
// (with backslash escapes for double-quoted, doubled quotes for
// single-quoted) and nothing but trailing whitespace or a comment follows the
// close. A scalar that doesn't start with a quote is always fine — this only
// guards the "unescaped internal quote" shape PyYAML refuses (SDD006).
func quotedScalarOK(s string) bool {
	if s == "" {
		return true
	}
	switch s[0] {
	case '"':
		i := 1
		for i < len(s) {
			if s[i] == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if s[i] == '"' {
				break
			}
			i++
		}
		if i >= len(s) {
			return false // unterminated
		}
		trailing := strings.TrimSpace(s[i+1:])
		return trailing == "" || strings.HasPrefix(trailing, "#")
	case '\'':
		i := 1
		for i < len(s) {
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i += 2
					continue
				}
				break
			}
			i++
		}
		if i >= len(s) {
			return false // unterminated
		}
		trailing := strings.TrimSpace(s[i+1:])
		return trailing == "" || strings.HasPrefix(trailing, "#")
	default:
		return true
	}
}

// itemToMap converts one ymlite.Item (a flat map of raw field text) into a
// map[string]any whose values are strings or []string, matching the shapes
// rules over phases[]/tasks[]/decisions[]/findings[] expect.
func itemToMap(it ymlite.Item) map[string]any {
	out := map[string]any{}
	for k, raw := range it {
		v := strings.TrimSpace(raw)
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			list := it.List(k)
			vals := make([]any, len(list))
			for i, s := range list {
				vals[i] = s
			}
			out[k] = vals
			continue
		}
		if v == "true" {
			out[k] = true
			continue
		}
		if v == "false" {
			out[k] = false
			continue
		}
		out[k] = it.Str(k)
	}
	return out
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
