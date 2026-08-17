package rules

import (
	"regexp"
	"strings"
	"sync"
)

// mdLine pairs a body line's raw source with its "visible" rendering: HTML
// comments and fenced code blocks stripped, mirroring sdd_validate.py's
// markdown_lines(). Rules that scan headings or citations must use the
// visible form so commented-out or example content in a fenced block isn't
// mistaken for the artifact's real structure.
type mdLine struct {
	Source  string
	Visible string
}

var fenceOpenerRe = regexp.MustCompile("^(`{3,}|~{3,})([^\r\n]*)$")

func rawFenceOpener(raw string) (marker byte, length int, ok bool) {
	m := fenceOpenerRe.FindStringSubmatch(strings.TrimRight(raw, "\r\n"))
	if m == nil {
		return 0, 0, false
	}
	token, info := m[1], m[2]
	if token[0] == '`' && strings.Contains(info, "`") {
		return 0, 0, false
	}
	return token[0], len(token), true
}

// markdownIndentation returns CommonMark indentation columns (tabs advance to
// the next multiple of 4) and the text after the leading whitespace.
func markdownIndentation(raw string) (int, string) {
	columns := 0
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		if raw[i] == '\t' {
			columns += 4 - columns%4
		} else {
			columns++
		}
		i++
	}
	return columns, raw[i:]
}

// markdownLines ports sdd_validate.py's markdown_lines(): it tracks fenced
// code blocks (whose content is never "visible") and HTML comments (whose
// content is stripped even outside fences), returning both the raw and
// visible rendering of every line, keepends-style.
func markdownLines(body string) []mdLine {
	var result []mdLine
	rawLines := splitKeepends(body)

	var fenceMarker byte
	fenceLen := 0
	inFence := false
	inComment := false

	for _, raw := range rawLines {
		source := raw
		if inFence {
			indent, stripped := markdownIndentation(raw)
			trimmed := strings.TrimRight(stripped, "\r\n")
			if indent <= 3 && isFenceCloser(trimmed, fenceMarker, fenceLen) {
				inFence = false
			}
			visible := ""
			if strings.HasSuffix(raw, "\n") {
				visible = "\n"
			}
			result = append(result, mdLine{Source: source, Visible: visible})
			continue
		}
		if inComment {
			idx := strings.Index(raw, "-->")
			if idx < 0 {
				visible := ""
				if strings.HasSuffix(source, "\n") {
					visible = "\n"
				}
				result = append(result, mdLine{Source: source, Visible: visible})
				continue
			}
			inComment = false
			raw = raw[idx+3:]
		}
		rawIndent, strippedRaw := markdownIndentation(raw)
		if rawIndent <= 3 && !inComment {
			if marker, length, ok := rawFenceOpener(strippedRaw); ok {
				inFence, fenceMarker, fenceLen = true, marker, length
				visible := ""
				if strings.HasSuffix(source, "\n") {
					visible = "\n"
				}
				result = append(result, mdLine{Source: source, Visible: visible})
				continue
			}
		}
		if rawIndent >= 4 {
			visible := ""
			if strings.HasSuffix(source, "\n") {
				visible = "\n"
			}
			result = append(result, mdLine{Source: source, Visible: visible})
			continue
		}
		var visibleParts []string
		remaining := raw
		for remaining != "" {
			if inComment {
				idx := strings.Index(remaining, "-->")
				if idx < 0 {
					remaining = ""
					break
				}
				inComment = false
				remaining = remaining[idx+3:]
				continue
			}
			opening := strings.Index(remaining, "<!--")
			if opening < 0 {
				visibleParts = append(visibleParts, remaining)
				break
			}
			visibleParts = append(visibleParts, remaining[:opening])
			remaining = remaining[opening+4:]
			inComment = true
		}
		visibleText := strings.Join(visibleParts, "")
		if strings.HasSuffix(raw, "\n") && !strings.HasSuffix(visibleText, "\n") {
			visibleText += "\n"
		}
		result = append(result, mdLine{Source: source, Visible: visibleText})
	}
	return result
}

func isFenceCloser(stripped string, marker byte, length int) bool {
	i := 0
	for i < len(stripped) && stripped[i] == marker {
		i++
	}
	if i < length {
		return false
	}
	return strings.TrimSpace(stripped[i:]) == ""
}

// splitKeepends splits s into lines the way Python's str.splitlines(keepends=True)
// does for "\n"-terminated text: each element retains its trailing "\n" except
// possibly the last.
func splitKeepends(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// visibleMarkdown returns a body's visible-only rendering.
func visibleMarkdown(body string) string {
	var b strings.Builder
	for _, l := range markdownLines(body) {
		b.WriteString(l.Visible)
	}
	return b.String()
}

// noComments strips HTML comments from text, matching sdd_validate.py's
// no_comments() (a plain regex over the whole string, not fence-aware).
var commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

func noComments(text string) string {
	return commentRe.ReplaceAllString(text, "")
}

// headingBodies returns the visible, comment-stripped body of every visible
// heading at the given level whose text exactly matches label.
func headingBodies(body string, level int, label string) []string {
	lines := markdownLines(body)
	marker := regexp.MustCompile(`^ {0,3}` + strings.Repeat("#", level) + `\s+` + regexp.QuoteMeta(label) + `\s*$`)
	var starts []int
	for i, l := range lines {
		if marker.MatchString(strings.TrimRight(l.Visible, "\r\n")) {
			starts = append(starts, i)
		}
	}
	headingAny := regexp.MustCompile(`^ {0,3}#{1,` + itoa(level) + `}\s+`)
	var result []string
	for _, start := range starts {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if headingAny.MatchString(lines[i].Visible) {
				end = i
				break
			}
		}
		var b strings.Builder
		for _, l := range lines[start+1 : end] {
			b.WriteString(l.Visible)
		}
		result = append(result, strings.TrimSpace(noComments(b.String())))
	}
	return result
}

func itoa(n int) string {
	digits := "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%10]}, b...)
		n /= 10
	}
	return string(b)
}

// sectionInfo is one level-N section's heading line and visible+comment-laden
// body (comments are stripped only by consumers that need it, matching
// Python's sections() which returns raw visible text).
type sectionInfo struct {
	Line int
	Body string
	// Order is the heading's position among same-depth headings in document
	// order. Go map iteration is random, but Python iterates a dict (which
	// preserves insertion order); consumers that need "the first heading
	// matching a pattern" (e.g. taskHeadingFor) must sort on this rather than
	// range over the map directly.
	Order int
}

// sections ports the Validator.sections() method: every heading at exactly
// the given depth, keyed by its trimmed title, mapped to its 1-indexed source
// line and the visible text between it and the next same-depth heading.
func sections(a *Artifact, level int) map[string]sectionInfo {
	// Memoized per (artifact, level): the rule sweep asks for an artifact's
	// sections from many rules, and each call re-split the body, re-scanned
	// every line, and — worst — recompiled the heading regex. All three are
	// pure functions of the immutable body.
	if cached, ok := a.sectionCache[level]; ok {
		return cached
	}
	lines := markdownLines(a.Body)
	pattern := headingPattern(level)
	type match struct {
		index   int
		heading string
	}
	var matches []match
	for i, l := range lines {
		if m := pattern.FindStringSubmatch(strings.TrimRight(l.Visible, "\r\n")); m != nil {
			matches = append(matches, match{i, strings.TrimSpace(m[1])})
		}
	}
	result := map[string]sectionInfo{}
	for idx, m := range matches {
		end := len(lines)
		if idx+1 < len(matches) {
			end = matches[idx+1].index
		}
		line := a.BodyLine + m.index
		var b strings.Builder
		for _, l := range lines[m.index+1 : end] {
			b.WriteString(l.Visible)
		}
		result[m.heading] = sectionInfo{Line: line, Body: b.String(), Order: idx}
	}
	if a.sectionCache == nil {
		a.sectionCache = map[int]map[string]sectionInfo{}
	}
	a.sectionCache[level] = result
	return result
}

// headingPattern returns the compiled matcher for a heading depth. Compiling
// it inside sections() meant a fresh regexp.MustCompile on every call, which
// on a large root is thousands of compilations of a handful of patterns.
func headingPattern(level int) *regexp.Regexp {
	headingPatternMu.Lock()
	defer headingPatternMu.Unlock()
	if p, ok := headingPatterns[level]; ok {
		return p
	}
	p := regexp.MustCompile(`^ {0,3}` + strings.Repeat("#", level) + `\s+(.+?)\s*$`)
	headingPatterns[level] = p
	return p
}

var (
	headingPatternMu sync.Mutex
	headingPatterns  = map[int]*regexp.Regexp{}
)
