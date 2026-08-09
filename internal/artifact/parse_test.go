package artifact

import (
	"strings"
	"testing"
)

const sample = `---
title: "Thing"
type: spec
status: draft
tags: [a, b]
---

# Thing

## Overview
One.

## Requirements

### Functional Requirements
- **FR-01**: first

#### A. Grouping
- **FR-02**: second
`

func TestParseFrontmatterPositions(t *testing.T) {
	d := Parse(sample)
	if !d.HasFrontmatter {
		t.Fatal("HasFrontmatter = false, want true")
	}
	for _, tc := range []struct {
		key, val string
		line     int
	}{
		{"title", `"Thing"`, 2},
		{"type", "spec", 3},
		{"status", "draft", 4},
		{"tags", "[a, b]", 5},
	} {
		got, ok := d.FM(tc.key)
		if !ok {
			t.Errorf("FM(%q) missing", tc.key)
			continue
		}
		if got != tc.val {
			t.Errorf("FM(%q) = %q, want %q", tc.key, got, tc.val)
		}
		if l := d.FMLine(tc.key); l != tc.line {
			t.Errorf("FMLine(%q) = %d, want %d", tc.key, l, tc.line)
		}
	}
}

func TestParseSectionPositionsAndDepth(t *testing.T) {
	d := Parse(sample)
	want := []struct {
		heading string
		depth   int
		line    int
	}{
		{"## Overview", 2, 10},
		{"## Requirements", 2, 13},
		{"### Functional Requirements", 3, 15},
		{"#### A. Grouping", 4, 18},
	}
	if len(d.Sections) != len(want) {
		t.Fatalf("got %d sections, want %d: %+v", len(d.Sections), len(want), d.Sections)
	}
	for i, w := range want {
		s := d.Sections[i]
		if s.Heading != w.heading || s.Depth != w.depth || s.Line != w.line {
			t.Errorf("section %d = (%q, d=%d, line=%d), want (%q, d=%d, line=%d)",
				i, s.Heading, s.Depth, s.Line, w.heading, w.depth, w.line)
		}
	}
}

// The H1 is the document title, not a slot.
func TestParseH1GoesToPreamble(t *testing.T) {
	d := Parse(sample)
	joined := strings.Join(d.Preamble, "\n")
	if !strings.Contains(joined, "# Thing") {
		t.Errorf("H1 not in preamble; preamble = %q", joined)
	}
	for _, s := range d.Sections {
		if s.Depth == 1 {
			t.Errorf("H1 became a section: %q", s.Heading)
		}
	}
}

func TestParseAcceptsCRLFAndBOM(t *testing.T) {
	crlf := "\ufeff" + strings.ReplaceAll(sample, "\n", "\r\n")
	d := Parse(crlf)
	if !d.HadBOM {
		t.Error("HadBOM = false, want true")
	}
	if d.LineEnding != "\r\n" {
		t.Errorf("LineEnding = %q, want CRLF", d.LineEnding)
	}
	if got, _ := d.FM("title"); got != `"Thing"` {
		t.Errorf("title through CRLF+BOM = %q", got)
	}
	if len(d.Sections) != 4 {
		t.Errorf("got %d sections through CRLF, want 4", len(d.Sections))
	}
}

// A heading inside a fenced block is content, not structure — otherwise a
// Mermaid diagram or a code sample would forge sections.
func TestParseIgnoresHeadingsInFences(t *testing.T) {
	src := "## Overview\n\n```\n## Not A Heading\n```\n\n## Goals\n"
	d := Parse(src)
	if len(d.Sections) != 2 {
		t.Fatalf("got %d sections, want 2: %+v", len(d.Sections), d.Sections)
	}
	if d.Sections[1].Heading != "## Goals" {
		t.Errorf("second section = %q, want ## Goals", d.Sections[1].Heading)
	}
}

func TestParseRejectsNonATXHash(t *testing.T) {
	d := Parse("##NoSpace\n\n## Real\n")
	if len(d.Sections) != 1 || d.Sections[0].Heading != "## Real" {
		t.Errorf("sections = %+v, want only '## Real'", d.Sections)
	}
}

func TestVisibleLinesSkipsFences(t *testing.T) {
	body := []string{"live one", "```", "hidden", "```", "live two"}
	got := VisibleLines(body)
	if len(got) != 2 || got[0].Text != "live one" || got[1].Text != "live two" {
		t.Fatalf("VisibleLines = %+v", got)
	}
	if got[1].Offset != 4 {
		t.Errorf("second visible line offset = %d, want 4", got[1].Offset)
	}
}

func TestStripCodeSpans(t *testing.T) {
	for _, tc := range []struct{ in, wantAbsent string }{
		{"cites `FR-99` as a literal", "FR-99"},
		{"a `b` c `FR-42` d", "FR-42"},
	} {
		if got := StripCodeSpans(tc.in); strings.Contains(got, tc.wantAbsent) {
			t.Errorf("StripCodeSpans(%q) = %q, still contains %q", tc.in, got, tc.wantAbsent)
		}
	}
	if got := StripCodeSpans("plain FR-01 text"); !strings.Contains(got, "FR-01") {
		t.Errorf("StripCodeSpans removed an uncoded identifier: %q", got)
	}
}

func TestTrimBlank(t *testing.T) {
	got := TrimBlank([]string{"", "  ", "a", "", "b", "", ""})
	if len(got) != 3 || got[0] != "a" || got[2] != "b" {
		t.Errorf("TrimBlank = %q", got)
	}
}

func TestParseUnterminatedFrontmatter(t *testing.T) {
	d := Parse("---\ntitle: x\n\n## Overview\n")
	if d.HasFrontmatter {
		t.Error("HasFrontmatter = true for unterminated frontmatter")
	}
	if len(d.Frontmatter) != 0 {
		t.Errorf("Frontmatter = %+v, want none", d.Frontmatter)
	}
}
