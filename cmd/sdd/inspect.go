package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
)

// cmdShow exposes an artifact's state without requiring the caller to parse
// Markdown (FR-25).
func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "emit JSON")
	typ := fs.String("type", "spec", "artifact type to assume when frontmatter omits it")

	flags, positional := splitArgs(args, map[string]bool{"-type": true, "--type": true})
	if err := fs.Parse(flags); err != nil {
		return fmt.Errorf("show: %w", err)
	}
	if len(positional) == 0 {
		return fmt.Errorf("show: expected an artifact path")
	}
	if len(positional) > 1 {
		return fmt.Errorf("show: unexpected extra argument %q", positional[1])
	}

	out, err := showArtifact(positional[0], *typ)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(out)
	}

	fmt.Printf("%s\n  type: %s\n  digest: %s\n", out.Path, out.Type, out.Digest[:12])
	fmt.Println("  frontmatter:")
	for _, k := range sortedKeys(out.Frontmatter) {
		fmt.Printf("    %-10s %s\n", k+":", out.Frontmatter[k])
	}
	for ns, ids := range out.Identifiers {
		fmt.Printf("  %s (%d): %s\n", ns, len(ids), strings.Join(ids, ", "))
	}
	for ns, ids := range out.Retired {
		fmt.Printf("  %s retired (%d): %s\n", ns, len(ids), strings.Join(ids, ", "))
	}
	fmt.Printf("  sections (%d):\n", len(out.Sections))
	for _, sec := range out.Sections {
		tag := ""
		if !sec.Declared {
			tag = "  [undeclared]"
		} else if sec.FreeProse {
			tag = "  [free-prose]"
		}
		fmt.Printf("    line %-5d %-40s %3d lines%s\n", sec.Line, sec.Heading, sec.Lines, tag)
	}
	if len(out.Missing) > 0 {
		fmt.Printf("  missing required (%d): %s\n", len(out.Missing), strings.Join(out.Missing, ", "))
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type sectionInfo struct {
	Heading   string `json:"heading"`
	Depth     int    `json:"depth"`
	Line      int    `json:"line"`
	Declared  bool   `json:"declared"`
	FreeProse bool   `json:"free_prose,omitempty"`
	Lines     int    `json:"body_lines"`
}

type showOut struct {
	Path        string              `json:"path"`
	Digest      string              `json:"digest"`
	Type        string              `json:"type"`
	Frontmatter map[string]string   `json:"frontmatter"`
	Identifiers map[string][]string `json:"identifiers,omitempty"`
	Retired     map[string][]string `json:"retired,omitempty"`
	Sections    []sectionInfo       `json:"sections"`
	Missing     []string            `json:"missing_required,omitempty"`
}

func showArtifact(path, typ string) (*showOut, error) {
	art, err := store.Read(path)
	if err != nil {
		return nil, err
	}
	if !art.Exists {
		return nil, fmt.Errorf("show: %s does not exist", path)
	}
	doc := artifact.Parse(art.Source)
	if t, ok := doc.FM("type"); ok && strings.TrimSpace(t) != "" {
		typ = strings.Trim(strings.TrimSpace(t), `"`)
	}
	out := &showOut{
		Path: relPath(path), Digest: art.Digest, Type: typ,
		Frontmatter: map[string]string{},
	}
	for _, e := range doc.Frontmatter {
		out.Frontmatter[e.Key] = e.Value
	}

	s, err := schema.Load(typ)
	if err != nil {
		// Unknown type: report structure without schema judgment.
		for _, sec := range doc.Sections {
			out.Sections = append(out.Sections, sectionInfo{
				Heading: sec.Heading, Depth: sec.Depth, Line: sec.Line,
				Lines: len(artifact.TrimBlank(sec.Body)),
			})
		}
		return out, nil
	}

	folded := artifact.FoldDeeper(doc.Sections, s.MaxSlotDepth())
	present := map[string]bool{}
	for _, sec := range folded {
		h := s.Heading(sec.Heading)
		info := sectionInfo{
			Heading: sec.Heading, Depth: sec.Depth, Line: sec.Line,
			Declared: h != nil, Lines: len(artifact.TrimBlank(sec.Body)),
		}
		if h != nil {
			present[h.Text] = true
			info.FreeProse = h.FreeProse
		}
		out.Sections = append(out.Sections, info)
	}
	for _, h := range s.Headings {
		if h.Required && !present[h.Text] {
			out.Missing = append(out.Missing, h.Text)
		}
	}

	live, retired := collectIDs(s, folded)
	if len(live) > 0 {
		out.Identifiers = live
	}
	if len(retired) > 0 {
		out.Retired = retired
	}
	return out, nil
}

// collectIDs reports declared and retired identifiers per namespace. It
// duplicates a little of internal/compile deliberately: `show` must describe
// what is on disk even when the artifact would not compile.
func collectIDs(s *schema.Schema, folded []artifact.Section) (live, retired map[string][]string) {
	live, retired = map[string][]string{}, map[string][]string{}
	for _, h := range s.Headings {
		if h.IDNamespace == "" {
			continue
		}
		for _, sec := range folded {
			if sec.Heading != h.Text {
				continue
			}
			for _, vl := range artifact.VisibleLines(sec.Body) {
				m := idDeclRe.FindStringSubmatch(vl.Text)
				if m == nil {
					continue
				}
				if !strings.HasPrefix(m[2], h.IDNamespace+"-") {
					continue
				}
				if m[1] == "~~" || m[3] == "~~" {
					retired[h.IDNamespace] = append(retired[h.IDNamespace], m[2])
				} else {
					live[h.IDNamespace] = append(live[h.IDNamespace], m[2])
				}
			}
		}
	}
	for _, m := range []map[string][]string{live, retired} {
		for k := range m {
			sort.Strings(m[k])
			if len(m[k]) == 0 {
				delete(m, k)
			}
		}
	}
	return
}

// cmdList enumerates artifacts of a type from the resolved planning root.
func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "emit JSON")
	root := fs.String("root", "", "planning root (default: resolved from planning-config.json)")

	flags, positional := splitArgs(args, map[string]bool{"-root": true, "--root": true})
	if err := fs.Parse(flags); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	typ := "spec"
	if len(positional) > 1 {
		return fmt.Errorf("list: unexpected extra argument %q", positional[1])
	}
	if len(positional) == 1 {
		typ = strings.TrimSuffix(positional[0], "s")
	}

	resolved := *root
	if resolved == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		resolved, err = store.FindPlanningRoot(wd)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
	}
	paths, err := store.List(resolved, typ)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	sort.Strings(paths)

	type row struct {
		Path   string `json:"path"`
		Title  string `json:"title,omitempty"`
		Status string `json:"status,omitempty"`
		Digest string `json:"digest"`
	}
	var rows []row
	for _, p := range paths {
		art, err := store.Read(filepath.Join(resolved, p))
		if err != nil {
			return err
		}
		doc := artifact.Parse(art.Source)
		title, _ := doc.FM("title")
		status, _ := doc.FM("status")
		rows = append(rows, row{p, strings.Trim(title, `"`), status, art.Digest})
	}

	if *jsonOut {
		return writeJSON(struct {
			Root      string `json:"root"`
			Type      string `json:"type"`
			Artifacts []row  `json:"artifacts"`
		}{relPath(resolved), typ, rows})
	}
	if len(rows) == 0 {
		fmt.Printf("no %s artifacts under %s\n", typ, relPath(resolved))
		return nil
	}
	for _, r := range rows {
		fmt.Printf("%-12s %-44s %s\n", r.Status, r.Path, r.Title)
	}
	return nil
}
