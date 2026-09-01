package rules

import (
	"errors"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Lifecycle normalization: the canonical form of an artifact with its
// lifecycle bookkeeping stripped out, so two revisions can be compared for a
// change in INTENT rather than in status.
//
// This is what lets SDD173 distinguish "the phase was marked complete after
// the review" (permitted) from "the phase's scope was rewritten after the
// review" (not). Both touch the same file; only the second invalidates the
// review.
//
// Ports lifecycle_normalized_artifact() and its helpers. The normalization
// removes exactly four things and preserves every other byte:
//
//   - top-level `updated:`, `status:`, and `waivers:` entries
//   - each phases[]/tasks[] entry's own `status:`
//   - the body of every completion-evidence section (replaced by the pending
//     marker, since retrospective evidence is written after the review)
//   - checkbox state under Subtasks and Acceptance Criteria
//
// `waivers` joins the stripped set for the same reason `status` is in it: an
// accepted exception is a judgment about a finding, not a change to what the
// phase is trying to do, and it is necessarily written after the review that
// surfaced the finding. Leaving it in would make declaring an exception
// invalidate the very review that justifies it — a rule that fires on its own
// remedy. What a waiver cannot do is hide a scope edit smuggled into the same
// commit: every other byte is still compared.
//
// errUnsupportedLifecycleNode mirrors Python raising ValueError: a flow-style
// or multiline lifecycle node cannot be excised by source span, so the
// comparison refuses rather than guessing. The caller reports it as an
// inability to compare, not as a change.
var errUnsupportedLifecycleNode = errors.New("unsupported flow or multiline lifecycle node")

var errMalformedFrontmatter = errors.New("malformed frontmatter")

// span is a half-open byte range within the frontmatter payload.
type span struct{ start, end int }

// frontmatterAndBody splits source at its delimiters without reconstructing
// any bytes, matching frontmatter_and_body().
func frontmatterAndBody(source string) (frontmatter, body string, err error) {
	lines := splitKeepends(source)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", errMalformedFrontmatter
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[:i+1], ""), strings.Join(lines[i+1:], ""), nil
		}
	}
	return "", "", errMalformedFrontmatter
}

// scalarSourceEnd returns the offset just past a scalar's source text, given
// the line it starts on and its start column.
//
// yaml.v3 reports a scalar's decoded Value, whose length is not its source
// length: quotes, escapes, doubled quotes, trailing spaces, and trailing
// comments all differ. Scanning the source is the only exact answer, and it is
// what Python gets for free from end_mark.index.
func scalarSourceEnd(line string, start int, style yaml.Style) int {
	if start >= len(line) {
		return len(line)
	}
	switch style {
	case yaml.DoubleQuotedStyle, yaml.SingleQuotedStyle:
		quote := line[start]
		for i := start + 1; i < len(line); i++ {
			if quote == '"' && line[i] == '\\' {
				i++
				continue
			}
			if line[i] != quote {
				continue
			}
			// A doubled single quote is an escaped quote, not the end.
			if quote == '\'' && i+1 < len(line) && line[i+1] == '\'' {
				i++
				continue
			}
			return i + 1
		}
		return len(line)
	default:
		end := len(line)
		if c := strings.Index(line[start:], " #"); c >= 0 {
			end = start + c
		}
		return start + len(strings.TrimRight(line[start:end], " \t"))
	}
}

// lineOffsets returns the byte offset at which each 1-indexed line begins.
func lineOffsets(s string) []int {
	offsets := []int{0, 0} // index 0 unused; line 1 starts at 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

// lifecycleEntrySpan ports lifecycle_entry_span: the removable source range of
// one `key: value` mapping entry, or an error when the node is flow-style or
// spans lines — cases whose bytes cannot be excised unambiguously.
func lifecycleEntrySpan(payload string, offsets []int, key, value *yaml.Node, containerFlow bool) (span, error) {
	if containerFlow ||
		value.Kind != yaml.ScalarNode ||
		value.Style == yaml.LiteralStyle || value.Style == yaml.FoldedStyle ||
		key.Line != value.Line {
		return span{}, errUnsupportedLifecycleNode
	}
	if key.Line <= 0 || key.Line >= len(offsets) {
		return span{}, errUnsupportedLifecycleNode
	}
	lineStart := offsets[key.Line]
	lineEnd := len(payload)
	if key.Line+1 < len(offsets) {
		lineEnd = offsets[key.Line+1]
	}
	line := strings.TrimRight(payload[lineStart:lineEnd], "\n")
	valueStart := value.Column - 1
	if valueStart < 0 || valueStart > len(line) {
		return span{}, errUnsupportedLifecycleNode
	}
	return span{
		start: lineStart + key.Column - 1,
		end:   lineStart + scalarSourceEnd(line, valueStart, value.Style),
	}, nil
}

// mappingEntries returns every (key, value) pair in a mapping node whose key
// is the given name. Python allows repeats because a duplicate key is a
// separate diagnostic, not a parse failure here.
func mappingEntries(node *yaml.Node, name string) [][2]*yaml.Node {
	var out [][2]*yaml.Node
	if node == nil || node.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind == yaml.ScalarNode && node.Content[i].Value == name {
			out = append(out, [2]*yaml.Node{node.Content[i], node.Content[i+1]})
		}
	}
	return out
}

func isFlow(n *yaml.Node) bool { return n.Style == yaml.FlowStyle }

// lifecycleBlockSpan excises a whole block-sequence entry — its key line
// through the last line of its nested content. lifecycleEntrySpan handles only
// single-line scalars, which a `waivers:` list is not.
//
// The end is found from the value's own final line rather than by scanning for
// the next key, so trailing comments or blank lines between entries cannot
// stretch the span past the value it belongs to.
func lifecycleBlockSpan(payload string, offsets []int, key, value *yaml.Node, containerFlow bool) (span, error) {
	if containerFlow || key.Line <= 0 || key.Line >= len(offsets) {
		return span{}, errUnsupportedLifecycleNode
	}
	last := value.Line
	var deepest func(n *yaml.Node)
	deepest = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Line > last {
			last = n.Line
		}
		for _, c := range n.Content {
			deepest(c)
		}
	}
	deepest(value)
	if last <= 0 || last >= len(offsets) {
		return span{}, errUnsupportedLifecycleNode
	}
	end := len(payload)
	if last+1 < len(offsets) {
		end = offsets[last+1]
	}
	return span{start: offsets[key.Line], end: end}, nil
}

// lifecycleFrontmatterSpans ports lifecycle_frontmatter_spans.
func lifecycleFrontmatterSpans(payload string, offsets []int, root *yaml.Node, kind string) ([]span, error) {
	var spans []span
	for _, name := range [2]string{"updated", "status"} {
		for _, kv := range mappingEntries(root, name) {
			s, err := lifecycleEntrySpan(payload, offsets, kv[0], kv[1], isFlow(root))
			if err != nil {
				return nil, err
			}
			spans = append(spans, s)
		}
	}
	// `waivers` is a block sequence rather than a scalar, so it needs a
	// whole-block span: from its key to the start of the next top-level key.
	for _, kv := range mappingEntries(root, "waivers") {
		s, err := lifecycleBlockSpan(payload, offsets, kv[0], kv[1], isFlow(root))
		if err != nil {
			return nil, err
		}
		spans = append(spans, s)
	}
	field := ""
	switch kind {
	case "plan":
		field = "phases"
	case "phase":
		field = "tasks"
	default:
		return spans, nil
	}
	for _, kv := range mappingEntries(root, field) {
		sequence := kv[1]
		if sequence.Kind != yaml.SequenceNode {
			continue
		}
		for _, entry := range sequence.Content {
			if entry.Kind != yaml.MappingNode {
				continue
			}
			for _, ekv := range mappingEntries(entry, "status") {
				s, err := lifecycleEntrySpan(payload, offsets, ekv[0], ekv[1], isFlow(entry))
				if err != nil {
					return nil, err
				}
				spans = append(spans, s)
			}
		}
	}
	return spans, nil
}

// lifecycleSequenceIDs ports lifecycle_sequence_ids: the string ids declared in
// a sequence field, used to find each task's body section.
func lifecycleSequenceIDs(root *yaml.Node, field string) []string {
	var ids []string
	for _, kv := range mappingEntries(root, field) {
		if kv[1].Kind != yaml.SequenceNode {
			continue
		}
		for _, entry := range kv[1].Content {
			if entry.Kind != yaml.MappingNode {
				continue
			}
			for _, ekv := range mappingEntries(entry, "id") {
				if ekv[1].Kind == yaml.ScalarNode && ekv[1].Tag == "!!str" {
					ids = append(ids, ekv[1].Value)
				}
			}
		}
	}
	return ids
}

// removeSpans deletes each span, highest offset first so earlier offsets stay
// valid.
func removeSpans(source string, spans []span) string {
	ordered := append([]span(nil), spans...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j-1].start < ordered[j].start; j-- {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
		}
	}
	result := source
	for _, s := range ordered {
		if s.start < 0 || s.end > len(result) || s.start > s.end {
			continue
		}
		result = result[:s.start] + result[s.end:]
	}
	return result
}

// visibleSectionRanges ports visible_section_ranges: each visible heading of
// the given level and text, paired with the line the section ends at.
func visibleSectionRanges(lines []mdLine, level int, heading string) [][2]int {
	marker := regexp.MustCompile(`^ {0,3}` + strings.Repeat("#", level) + `\s+` + regexp.QuoteMeta(heading) + `\s*$`)
	closer := regexp.MustCompile(`^ {0,3}#{1,` + itoa(level) + `}\s+`)
	var out [][2]int
	for i, l := range lines {
		if !marker.MatchString(strings.TrimRight(l.Visible, "\r\n")) {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if closer.MatchString(lines[j].Visible) {
				end = j
				break
			}
		}
		out = append(out, [2]int{i, end})
	}
	return out
}

// normalizeSectionRanges ports normalize_section_ranges: keep each section's
// heading line, replace its body with the replacement text.
func normalizeSectionRanges(lines []mdLine, ranges [][2]int, replacement string) string {
	starts := map[int]bool{}
	skipped := map[int]bool{}
	for _, rg := range ranges {
		starts[rg[0]] = true
		for i := rg[0] + 1; i < rg[1]; i++ {
			skipped[i] = true
		}
	}
	var b strings.Builder
	for i, l := range lines {
		switch {
		case starts[i]:
			b.WriteString(l.Source)
			if strings.HasSuffix(l.Source, "\n") {
				b.WriteString("\n")
			}
			b.WriteString(pendingMarker)
			b.WriteString("\n")
			_ = replacement
		case skipped[i]:
		default:
			b.WriteString(l.Source)
		}
	}
	return b.String()
}

// normalizeEvidenceSection replaces a named evidence section's body.
func normalizeEvidenceSection(text string, level int, heading string) string {
	lines := markdownLines(text)
	return normalizeSectionRanges(lines, visibleSectionRanges(lines, level, heading), pendingMarker)
}

// normalizeAllTaskEvidence replaces every declared task's Completion Evidence
// body, using the same section detection SDD067's orphan check uses.
func normalizeAllTaskEvidence(text string, taskIDs []string) string {
	lines := markdownLines(text)
	contained := declaredTaskEvidenceLines(text, taskIDs)
	var ranges [][2]int
	for start := range contained {
		end := len(lines)
		for j := start + 1; j < len(lines); j++ {
			if anyH1toH3Re.MatchString(lines[j].Visible) {
				end = j
				break
			}
		}
		ranges = append(ranges, [2]int{start, end})
	}
	return normalizeSectionRanges(lines, ranges, pendingMarker)
}

var checkedBoxRe = regexp.MustCompile(`^(\s*-\s+)\[[xX]\]`)

// normalizeCheckboxes clears checked boxes inside the named sections, so
// ticking a subtask after review is not a change in intent.
func normalizeCheckboxes(text string, level int, heading string) string {
	lines := markdownLines(text)
	covered := map[int]bool{}
	for _, rg := range visibleSectionRanges(lines, level, heading) {
		for i := rg[0] + 1; i < rg[1]; i++ {
			covered[i] = true
		}
	}
	var b strings.Builder
	for i, l := range lines {
		if covered[i] && strings.TrimRight(l.Visible, "\r\n") != "" && checkedBoxRe.MatchString(l.Source) {
			b.WriteString(checkedBoxRe.ReplaceAllString(l.Source, "$1[ ]"))
			continue
		}
		b.WriteString(l.Source)
	}
	return b.String()
}

// lifecycleNormalizedArtifact ports lifecycle_normalized_artifact: an
// artifact's bytes with lifecycle state removed, for comparing intent across
// revisions.
func lifecycleNormalizedArtifact(source, kind string) (string, error) {
	frontmatter, body, err := frontmatterAndBody(source)
	if err != nil {
		return "", err
	}
	// Compose only the payload between the delimiters, as Python does, so node
	// positions are relative to it.
	fmLines := splitKeepends(frontmatter)
	if len(fmLines) < 2 {
		return "", errMalformedFrontmatter
	}
	payload := strings.Join(fmLines[1:len(fmLines)-1], "")

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(payload), &doc); err != nil {
		return "", errMalformedFrontmatter
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", errMalformedFrontmatter
	}
	root := doc.Content[0]

	offsets := lineOffsets(payload)
	spans, err := lifecycleFrontmatterSpans(payload, offsets, root, kind)
	if err != nil {
		return "", err
	}
	// Spans are payload-relative; the payload starts after the opening
	// delimiter line, so shift them into frontmatter coordinates.
	shift := len(fmLines[0])
	for i := range spans {
		spans[i].start += shift
		spans[i].end += shift
	}
	normalizedFrontmatter := removeSpans(frontmatter, spans)

	switch kind {
	case "plan":
		// The generated Graph View section is a projection of the committed
		// graph, not plan intent: compile upserts it after phase reviews
		// freeze, and without the strip every frozen review's README pin
		// would read the projection as changed intent (Plans/SddGraph 5.6,
		// filed from the self-hosting pilot). Stripped BEFORE evidence
		// normalization: the begin marker is an HTML comment, not a
		// heading, so a section normalizer running first would swallow it
		// and strand the section body.
		body = stripGraphViewSection(body)
		body = normalizeEvidenceSection(body, 2, "Plan Completion Evidence")
	case "phase":
		body = normalizeAllTaskEvidence(body, lifecycleSequenceIDs(root, "tasks"))
		body = normalizeEvidenceSection(body, 2, "Phase Completion Evidence")
		body = normalizeCheckboxes(body, 3, "Subtasks")
		body = normalizeCheckboxes(body, 2, "Acceptance Criteria")
	}
	return normalizedFrontmatter + body, nil
}
