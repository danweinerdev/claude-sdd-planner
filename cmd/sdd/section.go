package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/compile"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// cmdSection dispatches `sdd section <verb>`. Spike scope: `set` only (FR-22).
func cmdSection(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("section: expected \"set\"")
	}
	switch args[0] {
	case "set":
		return cmdSectionSet(args[1:])
	default:
		return fmt.Errorf("section: unknown action %q", args[0])
	}
}

// cmdSectionSet implements `sdd section set`: replace exactly one section's
// body, leaving every other section and the entire frontmatter (aside from
// `updated`) byte-identical (FR-22).
func cmdSectionSet(args []string) error {
	fs := flag.NewFlagSet("section set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	heading := fs.String("heading", "", "declared section heading to replace, e.g. \"## Overview\"")
	dryRun := fs.Bool("dry-run", false, "print what would be written and write nothing")
	diff := fs.Bool("diff", false, "show a line diff against the artifact on disk")
	jsonOut := fs.Bool("json", false, "emit the result as JSON")
	expect := fs.String("expect", "", "refuse unless the artifact's current digest equals this value (FR-48)")
	typ := fs.String("type", "spec", "artifact type schema to check against")

	positional, err := parseFlags(fs, args)
	if err != nil {
		return fmt.Errorf("section set: %w", err)
	}
	var target string
	for _, a := range positional {
		if target != "" {
			return fmt.Errorf("section set: unexpected extra argument %q", a)
		}
		target = a
	}
	if target == "" {
		return fmt.Errorf("section set: expected an artifact path")
	}
	if strings.TrimSpace(*heading) == "" {
		return fmt.Errorf("section set: --heading is required")
	}

	s, err := schema.Load(*typ)
	if err != nil {
		return err
	}

	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("section set: reading payload from stdin: %w", err)
	}

	art, err := store.Read(target)
	if err != nil {
		return err
	}
	if !art.Exists {
		return fmt.Errorf("section set: %s does not exist", target)
	}
	if *expect != "" && *expect != art.Digest {
		return &staleError{path: target, want: *expect, got: art.Digest}
	}

	doc := artifact.Parse(art.Source)

	var refs []compile.Refusal
	refs = append(refs, compile.CheckFrozen(doc, false)...)

	out, secRefs := setSection(doc, s, *heading, string(payload), time.Now().Format("2006-01-02"))
	refs = append(refs, secRefs...)

	rel := relPath(target)

	if *jsonOut {
		return emitSectionJSON(rel, art, out, refs, *dryRun)
	}

	if len(refs) > 0 {
		fmt.Fprintf(os.Stderr, "refused: %s (%d violation(s), nothing written)\n\n", rel, len(refs))
		for _, r := range refs {
			fmt.Fprintf(os.Stderr, "  %s\n", r)
		}
		return &refusedError{n: len(refs)}
	}

	unchanged := out == art.Source

	if *diff || *dryRun {
		if unchanged {
			fmt.Printf("%s — no change (byte-idempotent)\n", rel)
		} else if *diff {
			fmt.Printf("would write %s:\n%s", rel, lineDiff(art.Source, out))
		}
	}
	if *dryRun {
		if !unchanged && !*diff {
			fmt.Print(out)
		}
		return nil
	}
	if unchanged {
		fmt.Printf("%s unchanged; digest %s\n", rel, art.Digest[:12])
		return nil
	}
	if err := store.WriteAtomic(target, out); err != nil {
		return fmt.Errorf("section set: %w", err)
	}
	fmt.Printf("wrote %s; digest %s\n", rel, store.Digest(out)[:12])
	return nil
}

func emitSectionJSON(rel string, art *store.Artifact, out string, refs []compile.Refusal, dryRun bool) error {
	type refusal struct {
		Code       string `json:"code"`
		Line       int    `json:"line,omitempty"`
		Message    string `json:"message"`
		Correction string `json:"correction,omitempty"`
	}
	res := struct {
		Path      string    `json:"path"`
		OK        bool      `json:"ok"`
		DryRun    bool      `json:"dry_run"`
		Wrote     bool      `json:"wrote"`
		Unchanged bool      `json:"unchanged"`
		Digest    string    `json:"digest,omitempty"`
		Refusals  []refusal `json:"refusals,omitempty"`
	}{Path: rel, OK: len(refs) == 0, DryRun: dryRun}
	for _, r := range refs {
		res.Refusals = append(res.Refusals, refusal{r.Code, r.Line, r.Message, r.Correction})
	}
	if res.OK {
		res.Unchanged = out == art.Source
		res.Digest = store.Digest(out)
		if !dryRun && !res.Unchanged {
			if err := store.WriteAtomic(art.Path, out); err != nil {
				return fmt.Errorf("section set: %w", err)
			}
			res.Wrote = true
		}
	}
	if err := writeJSON(res); err != nil {
		return err
	}
	if !res.OK {
		return &refusedError{n: len(refs)}
	}
	return nil
}

// setSection replaces exactly one section's body, leaving the frontmatter
// (aside from `updated`) and every other section byte-identical (FR-22).
// Returns the compiled bytes and any refusals; on refusal, output is "".
func setSection(doc *artifact.Doc, s *schema.Schema, heading, rawBody, today string) (string, []compile.Refusal) {
	target, actual := findSection(doc, heading)
	if target == nil {
		return "", []compile.Refusal{{
			Code:       "SEC010",
			Message:    fmt.Sprintf("section %q does not exist in the artifact", heading),
			Correction: fmt.Sprintf("actual headings: %s", strings.Join(actual, ", ")),
		}}
	}
	if s.Heading(heading) == nil && s.AdditionalSections == "refused" {
		return "", []compile.Refusal{{
			Code:       "SEC011",
			Message:    fmt.Sprintf("section %q is not declared by the %s schema", heading, s.Type),
			Correction: "target a declared section; this schema refuses undeclared additional sections",
		}}
	}

	newBody := splitLines(rawBody)
	if refs := lintSectionBody(newBody, target.Depth); len(refs) > 0 {
		return "", refs
	}

	fmLines, ok := compile.RestampFrontmatter(doc, today)
	if !ok {
		return "", []compile.Refusal{{
			Code: "FM01", Line: 1,
			Message:    "frontmatter cannot be modeled as YAML, so it cannot be rewritten safely",
			Correction: "Correct the frontmatter syntax; a `{{PLACEHOLDER}}` value must be quoted.",
		}}
	}

	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range fmLines {
		b.WriteString(l + "\n")
	}
	b.WriteString("---\n")
	for _, l := range doc.Preamble {
		b.WriteString(l + "\n")
	}
	for i := range doc.Sections {
		sec := &doc.Sections[i]
		b.WriteString(sec.Heading + "\n")
		body := sec.Body
		if sec == target {
			// A replacement body arrives as the caller typed it, with no
			// trailing blank line, while every parsed section carries one
			// before the next heading. Without this the written section reads
			// as `text` immediately followed by `## Next`, which is valid
			// Markdown but visibly unlike every section around it.
			body = newBody
			for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
				body = body[:len(body)-1]
			}
			if i < len(doc.Sections)-1 {
				body = append(body, "")
			}
		}
		for _, l := range body {
			b.WriteString(l + "\n")
		}
	}
	return b.String(), nil
}

// findSection returns the section exactly matching heading, and — when it
// doesn't — the full list of headings actually present, so the refusal can
// name them (FR-22, FR-29).
func findSection(doc *artifact.Doc, heading string) (*artifact.Section, []string) {
	heading = strings.TrimSpace(heading)
	var actual []string
	for i := range doc.Sections {
		actual = append(actual, doc.Sections[i].Heading)
		if doc.Sections[i].Heading == heading {
			return &doc.Sections[i], nil
		}
	}
	return nil, actual
}

// lintSectionBody refuses a payload containing a frontmatter delimiter (---)
// or a heading at or shallower than the target section's own depth; a deeper
// subheading is allowed (FR-22).
func lintSectionBody(lines []string, targetDepth int) []compile.Refusal {
	var refs []compile.Refusal
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "---" {
			refs = append(refs, compile.Refusal{
				Code: "SEC020", Line: i + 1,
				Message:    "payload contains a frontmatter delimiter (---)",
				Correction: "remove the --- line; section set replaces only this section's body",
			})
			continue
		}
		depth := 0
		for depth < len(t) && t[depth] == '#' {
			depth++
		}
		if depth > 0 && depth <= 6 && depth < len(t) && t[depth] == ' ' && depth <= targetDepth {
			refs = append(refs, compile.Refusal{
				Code: "SEC021", Line: i + 1,
				Message: fmt.Sprintf("payload heading %q is at depth %d, at or shallower than the target section's own depth %d",
					t, depth, targetDepth),
				Correction: "provide the section body only; omit the heading line itself. " +
					"To change a different section, run section set again with that --heading",
			})
		}
	}
	return refs
}

func splitLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
