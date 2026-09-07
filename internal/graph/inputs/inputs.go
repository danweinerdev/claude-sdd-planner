// Package inputs is the single shared resolver for a plan graph node's
// declared read-only context: whole files, or sections of Markdown files, the
// work reads but never writes. Every consumer — compile, validation, audit,
// `next --claim` context, `set-inputs`, and state drift — resolves through
// this package, so a declaration can never mean different things (or hash
// differently) in embed, validate, and recheck. The posture is the same one
// intent anchoring takes for citations (Designs/SddGraph DD-4): one resolver,
// one hasher, used by embed and recheck alike.
//
// Two hard rules govern the resolver:
//
//   - Paths resolve against an explicit root pair only — the repository root
//     or the planning root, passed in — never the process working directory.
//   - Refusals are absolute: an absolute or escaping path, a symlink escape,
//     a directory, a section on a binary or non-Markdown file, or a missing /
//     ambiguous heading all refuse. There is no fallback; a wrong input is a
//     finding, not a silently different read.
package inputs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
)

// Roots is the explicit root pair every resolution runs against. Both are
// absolute; callers obtain them from planning-root resolution, never from the
// working directory.
type Roots struct {
	// Repository is the absolute target repository root, including plan mappings.
	// It need not contain planning-config.json.
	Repository string
	// Planning is the absolute planning root. Inputs with root "planning"
	// resolve here.
	Planning string
}

// Kind is what a declaration resolves to.
type Kind string

const (
	// File is a whole-file input.
	File Kind = "file"
	// Section is a heading-delimited section of a Markdown input.
	Section Kind = "section"
)

// Resolved is one input's fingerprintable content.
type Resolved struct {
	// Key is model.InputKey of the declaration the content resolves.
	Key string
	// Root is the root selector ("repository" or "planning").
	Root string
	// Path is the declared root-relative path, forward slashes.
	Path string
	// Kind is File or Section.
	Kind Kind
	// Binary reports whether the source was detected as binary. Binary whole
	// files are fingerprinted over raw bytes; sections are never binary.
	Binary bool
	// Digest is the fingerprint: sha256:<hex> over the normalized text (text
	// files) or the raw bytes (binary files).
	Digest string
	// Text is the normalized content ("" for binary inputs).
	Text string
	// Headings is the selected section's heading path (Section kind only).
	Headings []string
	// HeadingLine is the 1-indexed source line of the selected heading
	// (Section kind only; 0 for whole files).
	HeadingLine int
}

// Resolver memoizes Resolve within one invocation, keyed by input key. A
// derive pass may consult the same input from many nodes; reading it once is
// both faster and consistent (every node sees the same bytes even if the file
// changes mid-pass).
type Resolver struct {
	roots     Roots
	canonical Roots
	cache     map[string]resolvedOrErr
}

type resolvedOrErr struct {
	resolved Resolved
	err      error
}

// NewResolver returns a resolver against the given roots. Roots are
// canonicalized once (symlinks resolved) so containment checks are against
// real directories, not a spelling that a symlink could escape.
func NewResolver(roots Roots) *Resolver {
	return &Resolver{
		roots:     roots,
		canonical: canonicalize(roots),
		cache:     map[string]resolvedOrErr{},
	}
}

// Resolve resolves one declared input. Results are memoized by input key; a
// resolution error (missing, ambiguous, escape, directory, binary/non-Markdown
// section) is also memoized so every caller sees the same refusal.
func (r *Resolver) Resolve(spec model.Input) (Resolved, error) {
	key := model.InputKey(spec)
	if v, ok := r.cache[key]; ok {
		return v.resolved, v.err
	}
	resolved, err := resolve(r.canonical, spec)
	r.cache[key] = resolvedOrErr{resolved, err}
	return resolved, err
}

// resolve is the pure resolution: one declared input against one canonical
// root pair. It is split from Resolver so tests can drive it directly.
func resolve(roots Roots, spec model.Input) (Resolved, error) {
	out := Resolved{Key: model.InputKey(spec), Root: spec.Root, Path: filepath.ToSlash(spec.Path)}
	base, ok := baseFor(roots, spec.Root)
	if !ok {
		return out, fmt.Errorf("unknown input root %q; valid roots are %q and %q", spec.Root, model.InputRootRepository, model.InputRootPlanning)
	}

	full, err := safeJoin(base, spec.Path)
	if err != nil {
		return out, err
	}

	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return out, fmt.Errorf("input %q does not exist under the %s root", spec.Path, spec.Root)
		}
		return out, fmt.Errorf("input %q: %v", spec.Path, err)
	}
	if !info.Mode().IsRegular() {
		return out, fmt.Errorf("input %q is not a regular file (directory or special file)", spec.Path)
	}

	if spec.Section != nil {
		return resolveSection(full, out, spec)
	}
	return resolveWholeFile(full, out)
}

func baseFor(roots Roots, selector string) (string, bool) {
	switch selector {
	case model.InputRootRepository:
		return roots.Repository, true
	case model.InputRootPlanning:
		return roots.Planning, true
	default:
		return "", false
	}
}

// canonicalize resolves each root's symlinks once, falling back to the clean
// absolute form when a root does not exist (a planning root may not yet be
// materialized on disk at resolver construction).
func canonicalize(roots Roots) Roots {
	canon := func(dir string) string {
		if dir == "" {
			return dir
		}
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return real
		}
		return filepath.Clean(dir)
	}
	return Roots{Repository: canon(roots.Repository), Planning: canon(roots.Planning)}
}

// safeJoin validates a declared root-relative path and joins it to the base,
// refusing absolute paths, `..`/`.` segments, backslashes, and symlink
// escapes.
func safeJoin(base, rel string) (string, error) {
	if base == "" || !filepath.IsAbs(base) {
		return "", fmt.Errorf("input root must be an explicit absolute directory, got %q", base)
	}
	if rel == "" {
		return "", fmt.Errorf("empty input path")
	}
	if filepath.IsAbs(rel) || (len(rel) >= 2 && rel[1] == ':') {
		return "", fmt.Errorf("absolute path %q; inputs are root-relative", rel)
	}
	if strings.Contains(rel, "\\") {
		return "", fmt.Errorf("path %q contains a backslash; input paths use forward slashes", rel)
	}
	for _, segment := range strings.Split(rel, "/") {
		if segment == "." || segment == ".." || segment == "" {
			return "", fmt.Errorf("path %q must use canonical root-relative segments without escapes", rel)
		}
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." {
		return "", fmt.Errorf("path %q names the root itself; declare a file", rel)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the root", rel)
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute path %q; inputs are root-relative", rel)
	}
	full := filepath.Join(base, clean)
	// Symlink escape: the resolved real path must stay within the canonical
	// base. This only fires for paths that exist (the missing-file case is
	// reported by the caller's Stat); an existing file reached through a
	// symlink pointing outside the root is exactly the escape we refuse.
	if real, err := filepath.EvalSymlinks(full); err == nil {
		if !within(base, real) {
			return "", fmt.Errorf("path %q resolves outside the root via symlink", rel)
		}
	}
	return full, nil
}

// within reports whether path is base or lies under base.
func within(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// resolveWholeFile fingerprints a whole file: raw SHA-256 for binary content,
// normalized (LF) SHA-256 for text.
func resolveWholeFile(full string, out Resolved) (Resolved, error) {
	raw, err := os.ReadFile(full)
	if err != nil {
		return out, fmt.Errorf("reading input %q: %v", out.Path, err)
	}
	out.Kind = File
	if isBinary(raw) {
		out.Binary = true
		out.Digest = sha256Hex(raw)
		return out, nil
	}
	text := normalizeNewlines(string(raw))
	out.Text = text
	out.Digest = sha256Hex([]byte(text))
	return out, nil
}

// resolveSection extracts a heading-delimited section of a Markdown file.
func resolveSection(full string, out Resolved, spec model.Input) (Resolved, error) {
	raw, err := os.ReadFile(full)
	if err != nil {
		return out, fmt.Errorf("reading input %q: %v", out.Path, err)
	}
	if isBinary(raw) {
		return out, fmt.Errorf("input %q is binary; sections require a Markdown file", out.Path)
	}
	if !markdownExtension(full) {
		return out, fmt.Errorf("input %q is not a Markdown file (extension %q); sections require Markdown", out.Path, filepath.Ext(full))
	}
	out.Kind = Section
	out.Headings = append([]string(nil), spec.Section.HeadingPath...)

	text := normalizeNewlines(string(raw))
	lines := strings.Split(text, "\n")
	headings := parseHeadings(lines)
	idx, err := selectHeading(headings, spec.Section.HeadingPath)
	if err != nil {
		return out, fmt.Errorf("input %q: %v", out.Path, err)
	}
	h := headings[idx]
	out.HeadingLine = h.start + 1 // 1-indexed
	section := strings.Join(lines[h.start:h.end], "\n")
	out.Text = section
	out.Digest = sha256Hex([]byte(section))
	return out, nil
}

// --- binary / markdown detection ---

// isBinary reports whether raw content is binary: a NUL byte in the leading
// window or invalid UTF-8. The NUL check is the standard heuristic — text
// never contains NUL, and a file that does is not line-ending-normalizable.
func isBinary(raw []byte) bool {
	window := raw
	if len(window) > 8000 {
		window = window[:8000]
	}
	if strings.IndexByte(string(window), 0) >= 0 {
		return true
	}
	if !utf8.Valid(raw) {
		return true
	}
	return false
}

// markdownExtensions is the closed set of extensions a section may target.
var markdownExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true, ".mkd": true, ".mdx": true,
}

func markdownExtension(path string) bool {
	return markdownExtensions[strings.ToLower(filepath.Ext(path))]
}

// normalizeNewlines collapses CRLF and lone CR to LF, so a text input hashes
// identically across Windows and POSIX checkouts.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// --- heading parsing ---

// heading is one parsed heading in a Markdown document.
type heading struct {
	level  int    // 1..6
	title  string // trimmed title (ATX trailing #s stripped)
	start  int    // 0-based line index of the heading's first line
	end    int    // 0-based line index (exclusive) where the section ends
	parent int    // index into the headings slice of the parent, or -1
}

// countIndent returns CommonMark indentation columns (tabs advance to the
// next multiple of 4).
func countIndent(line string) int {
	cols := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			cols++
		case '\t':
			cols += 4 - cols%4
		default:
			return cols
		}
	}
	return cols
}

// fenceOpener reports whether a (indent <= 3) line opens a fenced code block,
// returning the fence marker and length. The info string may not contain a
// backtick for a backtick fence.
func fenceOpener(line string) (marker byte, length int, ok bool) {
	s := strings.TrimSpace(line)
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return 0, 0, false
	}
	m := s[0]
	n := 0
	for n < len(s) && s[n] == m {
		n++
	}
	if m == '`' && n < 3 {
		return 0, 0, false
	}
	if m == '~' && n < 3 {
		return 0, 0, false
	}
	if m == '`' && strings.Contains(s[n:], "`") {
		return 0, 0, false
	}
	return m, n, true
}

// fenceCloser reports whether a line closes a fenced block of the given
// marker and length.
func fenceCloser(line string, marker byte, length int) bool {
	s := strings.TrimSpace(line)
	if s == "" || s[0] != marker {
		return false
	}
	n := 0
	for n < len(s) && s[n] == marker {
		n++
	}
	return n >= length && strings.TrimSpace(s[n:]) == ""
}

// atxHeading detects an ATX heading: up to 3 spaces, 1-6 `#`, then whitespace
// (or end of line), with trailing closing `#`s stripped. A `#` not followed
// by whitespace (e.g. `#include`) is not a heading.
func atxHeading(line string) (level int, title string, ok bool) {
	// up to 3 leading spaces
	i := 0
	for i < 3 && i < len(line) && line[i] == ' ' {
		i++
	}
	s := line[i:]
	if s == "" || s[0] != '#' {
		return 0, "", false
	}
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n > 6 {
		return 0, "", false
	}
	after := s[n:]
	if after == "" {
		return n, "", true
	}
	if after[0] != ' ' && after[0] != '\t' {
		return 0, "", false
	}
	title = strings.TrimSpace(after)
	title = stripClosingHashes(title)
	return n, title, true
}

// stripClosingHashes strips a trailing run of `#` (optionally surrounded by
// spaces) that closes an ATX heading, per CommonMark: only when the `#` run is
// preceded by whitespace.
func stripClosingHashes(title string) string {
	for {
		trimmed := strings.TrimRight(title, " \t")
		if trimmed == "" || !strings.HasSuffix(trimmed, "#") {
			return trimmed
		}
		runStart := len(trimmed)
		for runStart > 0 && trimmed[runStart-1] == '#' {
			runStart--
		}
		if runStart > 0 && trimmed[runStart-1] != ' ' && trimmed[runStart-1] != '\t' {
			return trimmed // `#` not preceded by whitespace: literal
		}
		title = strings.TrimRight(trimmed[:runStart], " \t")
	}
}

// setextUnderline detects a Setext underline line: a run of `=` (level 1) or
// `-` (level 2), optionally trailing spaces, up to 3 spaces indent.
func setextUnderline(line string) (level int, ok bool) {
	i := 0
	for i < 3 && i < len(line) && line[i] == ' ' {
		i++
	}
	s := line[i:]
	if s == "" {
		return 0, false
	}
	m := s[0]
	if m != '=' && m != '-' {
		return 0, false
	}
	for _, r := range s {
		if r == ' ' || r == '\t' {
			continue
		}
		if byte(r) != m {
			return 0, false
		}
	}
	if m == '=' {
		return 1, true
	}
	return 2, true
}

// parseHeadings scans normalized lines for ATX and Setext headings, skipping
// fenced and indented code blocks, and closes each heading's extent at the
// next heading of the same or higher (shallower) level.
func parseHeadings(lines []string) []heading {
	var out []heading
	var stack []int // indices into out, by increasing level

	inFence := false
	var fenceMarker byte
	var fenceLen int

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		indent := countIndent(raw)
		stripped := strings.TrimRight(raw, " \t")

		if inFence {
			if indent <= 3 && fenceCloser(stripped, fenceMarker, fenceLen) {
				inFence = false
			}
			continue
		}
		if indent <= 3 {
			if marker, length, ok := fenceOpener(stripped); ok {
				inFence, fenceMarker, fenceLen = true, marker, length
				continue
			}
		}
		if indent >= 4 {
			continue // indented code: never a heading
		}

		if level, title, ok := atxHeading(stripped); ok {
			out = append(out, heading{level: level, title: title, start: i, parent: -1})
			stack = pushHeading(stack, out, len(out)-1)
			continue
		}
		if level, ok := setextUnderline(stripped); ok && i > 0 {
			prev := strings.TrimRight(lines[i-1], " \t")
			prevIndent := countIndent(prev)
			if prevIndent >= 4 {
				continue
			}
			title := strings.TrimSpace(prev)
			if title == "" {
				continue // a standalone `---` (thematic break) is not a heading
			}
			if _, _, isATX := atxHeading(prev); isATX {
				continue
			}
			if _, isUL := setextUnderline(prev); isUL {
				continue
			}
			if isFenceBoundary(prev) {
				continue
			}
			out = append(out, heading{level: level, title: title, start: i - 1, parent: -1})
			stack = pushHeading(stack, out, len(out)-1)
			continue
		}
	}

	// Close each heading at the next same-or-higher heading (or EOF).
	for idx := range out {
		end := len(lines)
		for j := idx + 1; j < len(out); j++ {
			if out[j].level <= out[idx].level {
				end = out[j].start
				break
			}
		}
		out[idx].end = end
	}
	return out
}

// pushHeading drops every stack entry at or below the new heading's level and
// links the new heading to its parent.
func pushHeading(stack []int, out []heading, idx int) []int {
	level := out[idx].level
	for len(stack) > 0 && out[stack[len(stack)-1]].level >= level {
		stack = stack[:len(stack)-1]
	}
	if len(stack) > 0 {
		out[idx].parent = stack[len(stack)-1]
	}
	return append(stack, idx)
}

// isFenceBoundary reports whether a line is a fence opener or closer — used to
// keep a setext underline from treating a fence line as its "title".
func isFenceBoundary(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	if s[0] != '`' && s[0] != '~' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return len(s) >= 3
}

// selectHeading finds the unique heading whose ancestry-chain suffix equals
// the given heading path. The path's last element is the target's title; each
// preceding element is an immediate ancestor's title, in order. Extra
// ancestors above the path are allowed (that is what makes it a suffix).
// Exactly one match selects; zero or several refuse.
func selectHeading(headings []heading, path []string) (int, error) {
	if len(path) == 0 {
		return 0, fmt.Errorf("empty heading path")
	}
	var matches []int
	for idx := range headings {
		if headings[idx].title != path[len(path)-1] {
			continue
		}
		if ancestryMatches(headings, idx, path) {
			matches = append(matches, idx)
		}
	}
	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("heading path %q does not match any heading", strings.Join(path, " / "))
	case 1:
		return matches[0], nil
	default:
		return 0, fmt.Errorf("heading path %q is ambiguous (%d headings match; add an ancestor heading to disambiguate)", strings.Join(path, " / "), len(matches))
	}
}

// ancestryMatches reports whether heading idx's immediate-ancestor chain ends
// with path (the path is a suffix of the chain).
func ancestryMatches(headings []heading, idx int, path []string) bool {
	cur := idx
	for i := len(path) - 1; i >= 0; i-- {
		if cur < 0 || headings[cur].title != path[i] {
			return false
		}
		cur = headings[cur].parent
	}
	return true
}
