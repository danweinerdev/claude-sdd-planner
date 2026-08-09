// Package ymlite parses the bounded subset of YAML this repo's frontmatter
// actually uses: a top-level block sequence of flat maps (phases[], tasks[],
// decisions[]), where every field value is either a scalar (bare, quoted, or
// numeric) or a flow-style list (`[a, b, c]` / `[]`). It is not a general YAML
// parser — block scalars, multi-line values, and nested sequences are out of
// scope because nothing in this codebase's frontmatter uses them.
package ymlite

import (
	"regexp"
	"strings"
)

// Sequence returns the entries of the named top-level block sequence in a
// frontmatter string. It composes Block and Items, which is what every caller
// actually wants; the two are kept separate for callers that already hold the
// split lines and need the block's bounds.
func Sequence(src, key string) []Item {
	lines := strings.Split(src, "\n")
	start, end, found := Block(lines, key)
	if !found {
		return nil
	}
	return Items(lines[start:end])
}

// Item is one entry of a block sequence, keyed by field name. A field's raw
// text is stored verbatim (still quoted / bracketed); use Str or List to
// decode it.
type Item map[string]string

// Str returns a scalar field with surrounding quotes stripped.
func (it Item) Str(key string) string {
	return unquote(it[key])
}

// List returns a flow-list field's elements, trimmed and unquoted. A scalar
// (non-bracketed) value is returned as a single-element list so callers don't
// need to special-case an author who wrote one bare word instead of `[word]`.
func (it Item) List(key string) []string {
	v := strings.TrimSpace(it[key])
	if v == "" {
		return nil
	}
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		return []string{unquote(v)}
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, unquote(strings.TrimSpace(p)))
	}
	return out
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

var kvRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):\s?(.*)$`)

// Block returns the line range [start, end) within raw holding the named
// top-level key's block sequence — every line after `key:` up to (but not
// including) the next line that starts at column 0, or the end of raw. found
// is false if the key never appears as an unindented `key:` line.
func Block(raw []string, key string) (start, end int, found bool) {
	want := key + ":"
	for i, l := range raw {
		if l != want {
			continue
		}
		start, found = i+1, true
		end = len(raw)
		for j := i + 1; j < len(raw); j++ {
			line := raw[j]
			if line == "" {
				continue // blank lines inside the block are permitted
			}
			if line[0] != ' ' && line[0] != '\t' {
				end = j
				break
			}
		}
		return start, end, true
	}
	return 0, 0, false
}

// Items parses a block sequence — the lines Block returns — into an ordered
// slice of Item. A new item begins at each line whose (stripped) content
// starts with "- "; every less-indented-than-a-dash-but-more-indented line
// that follows adds a field to the current item.
func Items(block []string) []Item {
	var items []Item
	var cur Item
	dashIndent := -1
	fieldIndent := -1

	for _, line := range block {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingSpaces(line)
		rest := line[indent:]
		if strings.HasPrefix(rest, "- ") || rest == "-" {
			if cur != nil {
				items = append(items, cur)
			}
			cur = Item{}
			if dashIndent == -1 {
				dashIndent = indent
				fieldIndent = indent + 2
			}
			content := strings.TrimPrefix(strings.TrimPrefix(rest, "-"), " ")
			if content != "" {
				setKV(cur, content)
			}
			continue
		}
		if cur != nil && indent >= fieldIndent {
			setKV(cur, rest)
		}
	}
	if cur != nil {
		items = append(items, cur)
	}
	return items
}

func setKV(it Item, s string) {
	m := kvRe.FindStringSubmatch(s)
	if m == nil {
		return
	}
	it[m[1]] = strings.TrimRight(m[2], " \t")
}

func leadingSpaces(s string) int {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}
