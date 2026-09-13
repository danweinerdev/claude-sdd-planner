package compile

// Plan-README rendering: the README is plan identity the graph does not
// hold, so planReadmeUpdate performs surgical edits only — refreshing
// graph-owned phases[] entries (refreshReadmePhaseStatuses/scalarFieldEdit)
// and upserting one marker-delimited `## Graph View` section
// (renderGraphViewSection) — preserving everything else byte-for-byte. Split
// out of render.go at the phase-doc/README seam (task 6): a pure move, no
// behavior change.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	istore "github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"gopkg.in/yaml.v3"
)

// updateReadme applies the preflightable, surgical README projection.
func updateReadme(planDir, plan string, groups []phaseGroup, closed map[string]bool) (bool, error) {
	before, err := os.ReadFile(filepath.Join(planDir, "README.md"))
	if err != nil {
		return false, err
	}
	out, changed, err := planReadmeUpdate(planDir, plan, groups, closed)
	if err != nil || !changed {
		return false, err
	}
	return true, istore.WriteAtomicExpecting(filepath.Join(planDir, "README.md"), out, istore.Digest(string(before)))
}

func planReadmeUpdate(planDir, plan string, groups []phaseGroup, closed map[string]bool) (string, bool, error) {
	path := filepath.Join(planDir, "README.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("compile: reading plan README: %w", err)
	}
	src := string(raw)
	out := src

	out, err = refreshReadmePhaseStatuses(planDir, plan, out, groups, closed)
	if err != nil {
		return "", false, err
	}

	section := renderGraphViewSection(plan, groups)
	if beginCount := strings.Count(out, graphViewBegin); beginCount > 1 {
		return "", false, fmt.Errorf("compile: %s has a malformed graph-view section (multiple begin markers)", path)
	} else if beginCount == 1 {
		begin := strings.Index(out, graphViewBegin)
		end := strings.Index(out, graphViewEnd)
		switch {
		case end >= 0 && end < begin:
			return "", false, fmt.Errorf("compile: %s has a malformed graph-view section (begin without end)", path)
		case end < 0:
			// A begin marker with no end marker at all: a previous (buggy)
			// run left the section half-written. Exactly one begin and no
			// end is unambiguous — repair it in place by treating the span
			// from the begin marker to the next depth<=2 heading (or EOF) as
			// the section to replace, the same natural-extent rule every
			// other evidence/section writer here uses, and emit exactly one
			// well-formed begin/end pair.
			searchFrom := begin + len(graphViewBegin)
			// Skip past the section's own `## Graph View` heading first (the
			// same shape legacyGraphViewSpan matches) so the next-H2 search
			// below does not match that heading itself and produce a
			// zero-length span.
			if headingLoc := legacyGraphViewHeadingRe.FindStringIndex(out[searchFrom:]); headingLoc != nil {
				searchFrom += headingLoc[1]
			}
			spanEnd := len(out)
			if rest := nextH2HeadingRe.FindStringIndex(out[searchFrom:]); rest != nil {
				spanEnd = searchFrom + rest[0]
			}
			out = out[:begin] + section + out[spanEnd:]
		default:
			out = out[:begin] + section + out[end+len(graphViewEnd):]
		}
	} else if legacyBegin, legacyEnd, found := legacyGraphViewSpan(out); found {
		// A pre-begin-marker render left an orphaned `## Graph View`
		// section: a heading (optionally followed by `graph-view:end`, the
		// only marker that predates the begin marker) with no matching
		// begin. Upserting without recognizing this span would insert a
		// SECOND section, leaving the document with one begin marker but
		// two end markers — the next render's begin-anchored search then
		// pairs the new begin with the OLD orphaned end (whichever comes
		// first), reporting "begin without end". Replacing the whole
		// legacy span in place is what upsert would have done had the
		// begin marker always been there.
		out = out[:legacyBegin] + section + out[legacyEnd:]
	} else if i := planEvidenceHeadingRe.FindStringIndex(out); i != nil {
		// Insert BEFORE the Plan Completion Evidence section, never after:
		// evidence writers replace that section's whole extent (up to the
		// next depth<=2 heading), and the begin marker is a comment, not a
		// heading — a section appended after the evidence heading gets its
		// begin marker swallowed by the next evidence write, orphaning the
		// projection from the lifecycle strip that keeps frozen review
		// pins honest.
		out = out[:i[0]] + section + "\n\n" + out[i[0]:]
	} else {
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "\n" + section + "\n"
	}

	// The plan evidence's Verified date is rendered as the same
	// {VERIFIED_DATE} placeholder the phase docs use, then filled with
	// whatever `updated` stamp this render settles on below — never the
	// render-time clock directly. That is what makes an unchanged closed
	// plan's Verified line, and therefore its whole evidence body, stay
	// byte-identical across days: the placeholder participates in the
	// byte-stability comparison the same way the rest of the projection
	// does (review-execution 56815db-b F-01 item 5).
	out, err = applyPlanEvidence(planDir, plan, out, groups, closed, "{VERIFIED_DATE}")
	if err != nil {
		return "", false, err
	}

	prevUpdated := now().Format("2006-01-02")
	if m := updatedLineRe.FindStringSubmatch(src); m != nil {
		prevUpdated = m[1]
	}
	// Byte-stability check: filling the {VERIFIED_DATE} placeholder — WITHIN
	// the `## Plan Completion Evidence` section only, never document-wide,
	// because the plan's own free text could legitimately contain that
	// literal string (review-execution ef1962e F-01 item 3) — with the
	// PREVIOUS updated stamp and comparing against src tells whether
	// anything besides the date actually changed — the same shape the
	// phase docs' planWrite performs.
	if fillPlanEvidenceDate(out, prevUpdated) == src {
		return "", false, nil
	}
	// A real change restamps the README's updated date (both the
	// frontmatter field and every {VERIFIED_DATE} placeholder the evidence
	// body carries) to today; an unchanged README is never touched, so
	// idempotent re-renders stay byte-stable.
	today := now().Format("2006-01-02")
	out = fillPlanEvidenceDate(out, today)
	end, err := readmeFrontmatterEnd(out)
	if err != nil {
		return "", false, err
	}
	out = updatedLineRe.ReplaceAllString(out[:end], "updated: "+today) + out[end:]
	return out, true, nil
}

// nextH2HeadingRe finds the next depth<=2 heading, the extent every
// evidence writer (this one included) replaces up to (SDD020's duplicate
// check: exactly one visible `## Plan Completion Evidence` section).
var nextH2HeadingRe = regexp.MustCompile(`(?m)^ {0,3}#{1,2}\s+`)

// fillPlanEvidenceDate substitutes every `{VERIFIED_DATE}` placeholder
// WITHIN the `## Plan Completion Evidence` section body only — the same
// extent applyPlanEvidence writes to (heading to the next depth<=2 heading
// or EOF) — never document-wide, because the plan's own free text (outside
// the generated sections) could legitimately contain that literal string
// (review-execution ef1962e F-01 item 3). A README with no such section is
// returned unchanged.
func fillPlanEvidenceDate(src, date string) string {
	loc := planEvidenceHeadingRe.FindStringIndex(src)
	if loc == nil {
		return src
	}
	bodyStart := loc[1]
	for bodyStart < len(src) && src[bodyStart] == '\n' {
		bodyStart++
	}
	bodyEnd := len(src)
	if rest := nextH2HeadingRe.FindStringIndex(src[bodyStart:]); rest != nil {
		bodyEnd = bodyStart + rest[0]
	}
	filled := strings.ReplaceAll(src[bodyStart:bodyEnd], "{VERIFIED_DATE}", date)
	return src[:bodyStart] + filled + src[bodyEnd:]
}

// applyPlanEvidence replaces the `## Plan Completion Evidence` section body
// with the derived evidence once every phase in groups is closed and every
// completed phase resolves a covering final review (renderPlanEvidence);
// otherwise the section (typically still `Pending — not complete.`) is left
// exactly as it is — this function never invents evidence and never
// regresses a section a human or an earlier render already completed.
func applyPlanEvidence(planDir, plan, src string, groups []phaseGroup, closed map[string]bool, verifiedDate string) (string, error) {
	body, ok, err := renderPlanEvidence(planDir, plan, verifiedDate, groups, closed)
	if err != nil {
		return "", err
	}
	if !ok {
		return src, nil
	}
	loc := planEvidenceHeadingRe.FindStringIndex(src)
	if loc == nil {
		return src, nil
	}
	bodyStart := loc[1]
	for bodyStart < len(src) && src[bodyStart] == '\n' {
		bodyStart++
	}
	rest := nextH2HeadingRe.FindStringIndex(src[bodyStart:])
	bodyEnd := len(src)
	if rest != nil {
		bodyEnd = bodyStart + rest[0]
	}
	return src[:bodyStart] + body + "\n" + src[bodyEnd:], nil
}

func readmeFrontmatterEnd(src string) (int, error) {
	if !strings.HasPrefix(src, "---\n") {
		return 0, fmt.Errorf("compile: README requires YAML frontmatter")
	}
	at := strings.Index(src[4:], "\n---\n")
	if at < 0 {
		return 0, fmt.Errorf("compile: README frontmatter is not closed")
	}
	return 4 + at + 1, nil // beginning of the closing delimiter
}

// refreshReadmePhaseStatuses changes only the owned scalar tokens. YAML node
// positions keep comments, quoting, extra fields, flow style and identity prose
// intact instead of serializing the entire frontmatter through a flat model.
func refreshReadmePhaseStatuses(planDir, plan, src string, groups []phaseGroup, closed map[string]bool) (string, error) {
	end, err := readmeFrontmatterEnd(src)
	if err != nil {
		return "", err
	}
	fm := src[4:end]
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return "", fmt.Errorf("compile: invalid README YAML: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return "", fmt.Errorf("compile: README frontmatter must be a mapping")
	}
	var phases, key *yaml.Node
	for i := 0; i < len(doc.Content[0].Content); i += 2 {
		if doc.Content[0].Content[i].Value == "phases" {
			if phases != nil {
				return "", fmt.Errorf("compile: duplicate README phases field")
			}
			key, phases = doc.Content[0].Content[i], doc.Content[0].Content[i+1]
		}
	}
	if phases == nil {
		return src, nil
	}
	if phases.Kind != yaml.SequenceNode {
		return "", fmt.Errorf("compile: README phases must be a sequence")
	}
	offset := func(n *yaml.Node) (int, error) {
		at := 4
		for line := 1; line < n.Line; line++ {
			next := strings.IndexByte(src[at:end], '\n')
			if next < 0 {
				return 0, fmt.Errorf("compile: invalid phase source position")
			}
			at += next + 1
		}
		// YAML columns count Unicode code points, not bytes.
		for column := 1; column < n.Column; column++ {
			if at >= end {
				return 0, fmt.Errorf("compile: invalid phase source column")
			}
			_, size := utf8.DecodeRuneInString(src[at:end])
			at += size
		}
		return at, nil
	}
	if len(phases.Content) == 0 && len(groups) > 0 {
		start, err := offset(key)
		if err != nil {
			return "", err
		}
		value, err := offset(phases)
		if err != nil {
			return "", err
		}
		lineEnd := strings.IndexByte(src[value:end], '\n')
		if lineEnd < 0 {
			return "", fmt.Errorf("compile: unsupported empty phases layout")
		}
		lineEnd += value
		close := strings.IndexByte(src[value:lineEnd], ']')
		if close < 0 || src[value] != '[' || strings.TrimSpace(src[value+1:value+close]) != "" {
			return "", fmt.Errorf("compile: empty phases must use [] on one line")
		}
		close += value
		var b strings.Builder
		b.WriteString(strings.TrimRight(src[start:value], " \t"))
		b.WriteString(src[close+1 : lineEnd])
		b.WriteByte('\n')
		for _, ph := range groups {
			fmt.Fprintf(&b, "  - id: %d\n    title: %s\n    status: %s\n    doc: %s\n", ph.Ordinal, strconv.Quote(ph.Title), phaseStatus(ph.Nodes, closed), strconv.Quote(ph.Doc))
		}
		return src[:start] + b.String() + src[lineEnd+1:], nil
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, entry := range phases.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		fields := map[string]*yaml.Node{}
		for i := 0; i < len(entry.Content); i += 2 {
			k := entry.Content[i].Value
			if fields[k] != nil {
				return "", fmt.Errorf("compile: duplicate phase field %q", k)
			}
			fields[k] = entry.Content[i+1]
		}
		if fields["id"] == nil || fields["doc"] == nil {
			continue
		}
		for _, ph := range groups {
			if fields["doc"].Value != ph.Doc {
				continue
			}
			old, err := os.ReadFile(filepath.Join(planDir, ph.Doc))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "", err
			}
			if !strings.Contains(string(old), viewMarker(plan)) {
				continue
			}
			if fields["id"].Value != strconv.Itoa(ph.Ordinal) {
				return "", fmt.Errorf("compile: generated phase %s has README id %q, expected %d; reconcile the identity instead of silently leaving a stale status", ph.Doc, fields["id"].Value, ph.Ordinal)
			}
			statusEdit, err := scalarFieldEdit(src, end, offset, fields["status"], phaseStatus(ph.Nodes, closed), "status", ph.Doc)
			if err != nil {
				return "", err
			}
			if statusEdit != nil {
				edits = append(edits, *statusEdit)
			}
			// SDD152: the README phase entry's title and the phase doc's own
			// `title` frontmatter must agree exactly. The doc's title is the
			// human-derived one (phaseTitle); this keeps the README entry in
			// sync with it the same way status is kept in sync, rather than
			// leaving a raw phase label the initial README write may have
			// used.
			titleEdit, err := scalarFieldEdit(src, end, offset, fields["title"], ph.Title, "title", ph.Doc)
			if err != nil {
				return "", err
			}
			if titleEdit != nil {
				edits = append(edits, *titleEdit)
			}
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		src = src[:e.start] + e.text + src[e.end:]
	}
	return src, nil
}

// scalarFieldEdit computes the edit (if any) that brings a README phase
// entry's plain- or quoted-scalar field to the wanted value, matching
// refreshReadmePhaseStatuses' status-field logic exactly so title and status
// stay in lockstep. Returns nil, nil when the field already holds want.
func scalarFieldEdit(src string, end int, offset func(*yaml.Node) (int, error), field *yaml.Node, want, fieldName, doc string) (*struct {
	start, end int
	text       string
}, error) {
	if field != nil && field.Kind == yaml.ScalarNode && field.Tag == "!!str" && field.Value == want {
		return nil, nil
	}
	if field == nil || field.Kind != yaml.ScalarNode || field.Tag != "!!str" || field.Anchor != "" || field.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0 {
		return nil, fmt.Errorf("compile: generated phase %s requires a plain or quoted scalar %s", doc, fieldName)
	}
	start, err := offset(field)
	if err != nil {
		return nil, err
	}
	finish := start
	replacement := want
	switch field.Style {
	case yaml.DoubleQuotedStyle, yaml.SingleQuotedStyle:
		quote := src[start]
		if quote != '"' && quote != '\'' {
			return nil, fmt.Errorf("compile: quoted phase %s source span does not start at a quote", fieldName)
		}
		finish++
		for finish < end {
			if quote == '"' && src[finish] == '\\' {
				finish += 2
				continue
			}
			if src[finish] == quote {
				if quote == '\'' && finish+1 < end && src[finish+1] == '\'' {
					finish += 2
					continue
				}
				finish++
				break
			}
			finish++
		}
		if finish > end || src[finish-1] != quote {
			return nil, fmt.Errorf("compile: unterminated phase %s", fieldName)
		}
		if quote == '"' {
			replacement = strconv.Quote(want)
		} else {
			replacement = "'" + want + "'"
		}
	default:
		if !strings.HasPrefix(src[start:end], field.Value) || strings.ContainsAny(field.Value, "\r\n") {
			return nil, fmt.Errorf("compile: unsafe phase %s source span", fieldName)
		}
		finish = start + len(field.Value)
	}
	return &struct {
		start, end int
		text       string
	}{start, finish, replacement}, nil
}

func renderGraphViewSection(plan string, groups []phaseGroup) string {
	var b strings.Builder
	b.WriteString(graphViewBegin + "\n\n## Graph View\n\n")
	fmt.Fprintf(&b, "%s\n\n", viewMarker(plan))
	b.WriteString("| Phase | Nodes | Doc |\n|---|---|---|\n")
	total := 0
	for _, ph := range groups {
		fmt.Fprintf(&b, "| %d: %s | %d | `%s` |\n", ph.Ordinal, ph.Title, len(ph.Nodes), ph.Doc)
		total += len(ph.Nodes)
	}
	fmt.Fprintf(&b, "\n%d node(s) total. The committed graph (`%s-Graph.json`) is the source of\ntruth; these documents are projections.\n\n", total, plan)
	b.WriteString(graphViewEnd)
	return b.String()
}
