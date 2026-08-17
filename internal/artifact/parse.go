// Package artifact parses an SDD artifact or payload into a positioned model
// (FR-29 needs line-accurate messages; FR-19 needs per-section matching).
//
// Spike scope: frontmatter is handled line-oriented over flat top-level keys,
// which is exactly what `spec` artifacts carry. Nested structured frontmatter
// (plans' phases[], phases' tasks[]) is out of scope here and is why the
// production port needs a real YAML node model.
package artifact

import (
	"bufio"
	"strings"
)

// FMEntry is one flat frontmatter key/value with its source line (1-indexed).
type FMEntry struct {
	Key   string
	Value string
	Line  int
}

// Section is one heading and the body lines beneath it, up to the next heading
// at the same or shallower depth.
type Section struct {
	Heading string // full heading line as written, trimmed
	Depth   int
	Line    int      // 1-indexed line of the heading
	Body    []string // lines between this heading and the next slot-depth heading
}

// Title is the heading text without leading hashes or trailing whitespace.
func (s Section) Title() string {
	return strings.TrimSpace(strings.TrimLeft(s.Heading, "#"))
}

// Doc is a parsed artifact or payload.
type Doc struct {
	HasFrontmatter bool
	Frontmatter    []FMEntry
	// FrontmatterRaw is every line between the `---` delimiters, verbatim.
	// Types whose frontmatter carries nested structure (a plan's phases[], a
	// phase's tasks[], a review's findings[]) are emitted from this rather than
	// regenerated, because a flat key/value model cannot round-trip them.
	FrontmatterRaw []string
	Preamble       []string // body lines before the first heading (e.g. the H1)
	Sections       []Section
	LineEnding     string // "\r\n" when the source used CRLF, else "\n"
	HadBOM         bool
}

// FM returns the value for a frontmatter key and whether it was present.
func (d *Doc) FM(key string) (string, bool) {
	for _, e := range d.Frontmatter {
		if e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

// FMLine returns the source line of a frontmatter key, or 0.
func (d *Doc) FMLine(key string) int {
	for _, e := range d.Frontmatter {
		if e.Key == key {
			return e.Line
		}
	}
	return 0
}

const bom = "\ufeff"

// Parse reads a document. CRLF and a UTF-8 BOM are accepted (NFR-05); the
// original line ending is recorded but emission always normalizes to LF.
func Parse(src string) *Doc {
	d := &Doc{LineEnding: "\n"}
	if strings.HasPrefix(src, bom) {
		d.HadBOM = true
		src = strings.TrimPrefix(src, bom)
	}
	if strings.Contains(src, "\r\n") {
		d.LineEnding = "\r\n"
		src = strings.ReplaceAll(src, "\r\n", "\n")
	}

	var lines []string
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	i := 0
	// Frontmatter: a mapping between standalone --- delimiters at the very top.
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		i = 1
		for ; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				d.HasFrontmatter = true
				i++
				break
			}
			raw := lines[i]
			d.FrontmatterRaw = append(d.FrontmatterRaw, raw)
			if strings.HasPrefix(strings.TrimSpace(raw), "#") || strings.TrimSpace(raw) == "" {
				continue // comment or blank
			}
			if raw != strings.TrimLeft(raw, " \t") {
				continue // nested/continuation line: handled by the block scan below
			}
			if k, v, ok := strings.Cut(raw, ":"); ok {
				key, val, keyLine := strings.TrimSpace(k), strings.TrimSpace(v), i+1
				// A key with no inline value may still hold a BLOCK sequence:
				//
				//   tags:
				//   - alpha
				//   - beta
				//
				// Reading only the key line reported such a field as present
				// but empty, so the payload-authoritative merge cleared the
				// author's populated list without even a note — the same
				// symptom whether the author wrote a block list or omitted the
				// field. Fold the items into the flow form the rest of the
				// pipeline already understands. Nested block MAPPINGS (phases,
				// tasks, decisions) are left alone: they are carried by the
				// YAML node tree, not by these flat entries.
				if val == "" {
					if items, next := blockSequenceItems(lines, i+1); len(items) > 0 {
						val = "[" + strings.Join(items, ", ") + "]"
						for j := i + 1; j < next; j++ {
							d.FrontmatterRaw = append(d.FrontmatterRaw, lines[j])
						}
						i = next - 1
					}
				}
				d.Frontmatter = append(d.Frontmatter, FMEntry{
					Key:   key,
					Value: val,
					Line:  keyLine,
				})
			}
		}
		if !d.HasFrontmatter {
			// Unterminated: treat the whole thing as body.
			i = 0
			d.Frontmatter = nil
			d.FrontmatterRaw = nil
		}
	}

	inFence := false
	var cur *Section
	for ; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			if cur != nil {
				cur.Body = append(cur.Body, line)
			} else {
				d.Preamble = append(d.Preamble, line)
			}
			continue
		}

		if !inFence && strings.HasPrefix(trimmed, "#") {
			depth := 0
			for depth < len(trimmed) && trimmed[depth] == '#' {
				depth++
			}
			// An ATX heading needs a space after the hashes.
			if depth <= 6 && depth < len(trimmed) && trimmed[depth] == ' ' {
				if depth == 1 {
					// The H1 belongs to the preamble, not a slot.
					if cur == nil {
						d.Preamble = append(d.Preamble, line)
						continue
					}
				}
				d.Sections = append(d.Sections, Section{Heading: trimmed, Depth: depth, Line: i + 1})
				cur = &d.Sections[len(d.Sections)-1]
				continue
			}
		}

		if cur != nil {
			cur.Body = append(cur.Body, line)
		} else {
			d.Preamble = append(d.Preamble, line)
		}
	}
	return d
}

// FoldDeeper returns sections with every heading deeper than maxSlotDepth
// folded back into the enclosing section's body. Grouping subheadings (e.g.
// `#### A. Foo` inside `### Functional Requirements`) otherwise orphan
// everything beneath them: the parser opens a new section, so those lines never
// reach the enclosing slot's body and its identifiers become invisible.
func FoldDeeper(sections []Section, maxSlotDepth int) []Section {
	var out []Section
	for _, sec := range sections {
		if sec.Depth > maxSlotDepth && len(out) > 0 {
			p := &out[len(out)-1]
			p.Body = append(p.Body, sec.Heading)
			p.Body = append(p.Body, sec.Body...)
			continue
		}
		out = append(out, sec)
	}
	return out
}

// TrimBlank removes leading and trailing blank lines from a body.
func TrimBlank(body []string) []string {
	s, e := 0, len(body)
	for s < e && strings.TrimSpace(body[s]) == "" {
		s++
	}
	for e > s && strings.TrimSpace(body[e-1]) == "" {
		e--
	}
	return body[s:e]
}

// VisibleLines returns body lines outside fenced code blocks, paired with their
// offset within the body. Identifier resolution (FR-23) must not see fenced
// content.
type VisibleLine struct {
	Text   string
	Offset int
}

func VisibleLines(body []string) []VisibleLine {
	var out []VisibleLine
	inFence := false
	for i, l := range body {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, VisibleLine{Text: l, Offset: i})
	}
	return out
}

// StripCodeSpans blanks out inline code spans so their contents are treated as
// literals rather than citations (FR-23's code-span exemption).
func StripCodeSpans(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '`' {
			in = !in
			b.WriteByte(' ')
			continue
		}
		if in {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// blockSequenceItems reads a YAML block sequence starting at `from`, returning
// its scalar items and the index just past the block. It returns no items when
// the block is absent, or when any entry is a block MAPPING (`- key: value`) —
// those are nested structures (phases, tasks, decisions, lane_results) that
// the flat FMEntry list must not flatten.
func blockSequenceItems(lines []string, from int) ([]string, int) {
	var items []string
	i := from
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue // blank lines and comments do not end a block
		}
		if t == "---" || !strings.HasPrefix(t, "- ") {
			break // end of the sequence (next key, or the closing fence)
		}
		item := strings.TrimSpace(strings.TrimPrefix(t, "- "))
		// `- key: value` is a mapping entry: this is a nested structure, not a
		// scalar list. Leave the whole block to the YAML node tree.
		if k, _, ok := strings.Cut(item, ":"); ok && !strings.HasPrefix(item, `"`) &&
			!strings.HasPrefix(item, "'") && !strings.Contains(k, " ") {
			return nil, from
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, from
	}
	return items, i
}
