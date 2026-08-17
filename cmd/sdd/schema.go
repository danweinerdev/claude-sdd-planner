package main

import (
	"fmt"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
)

// schemaTypeSummary is one row of `schema list --json`.
type schemaTypeSummary struct {
	Type               string `json:"type"`
	Sections           int    `json:"sections"`
	RequiredSections   int    `json:"required_sections"`
	Namespaces         int    `json:"namespaces"`
	AdditionalSections string `json:"additional_sections"`
}

// schemaDetail is `schema show <type> --json`: the same contract the text
// form renders, in the shape a generator or a linter can consume.
type schemaDetail struct {
	Type               string              `json:"type"`
	AdditionalSections string              `json:"additional_sections"`
	Frontmatter        []schemaFieldDetail `json:"frontmatter"`
	Sections           []schemaHeadingInfo `json:"sections"`
}

type schemaFieldDetail struct {
	Key       string `json:"key"`
	Ownership string `json:"ownership"`
	Required  bool   `json:"required"`
	Default   string `json:"default,omitempty"`
}

type schemaHeadingInfo struct {
	Text        string `json:"text"`
	Required    bool   `json:"required"`
	IDNamespace string `json:"id_namespace,omitempty"`
	FreeProse   bool   `json:"free_prose,omitempty"`
}

func cmdSchema(action, artifactType string, jsonOut bool) error {
	switch action {
	case "list":
		var rows []schemaTypeSummary
		for _, t := range schema.Types() {
			s, err := schema.Load(t)
			if err != nil {
				return err
			}
			req := 0
			for _, h := range s.Headings {
				if h.Required {
					req++
				}
			}
			if jsonOut {
				rows = append(rows, schemaTypeSummary{
					Type: s.Type, Sections: len(s.Headings), RequiredSections: req,
					Namespaces: len(s.Namespaces), AdditionalSections: s.AdditionalSections,
				})
				continue
			}
			fmt.Printf("%-8s %d sections (%d required)  %d namespaces  additionalSections=%s\n",
				s.Type, len(s.Headings), req, len(s.Namespaces), s.AdditionalSections)
		}
		if jsonOut {
			return writeJSON(rows)
		}
		return nil
	case "show":
		s, err := schema.Load(artifactType)
		if err != nil {
			return err
		}
		if jsonOut {
			out := schemaDetail{Type: s.Type, AdditionalSections: s.AdditionalSections}
			for _, f := range s.Frontmatter {
				out.Frontmatter = append(out.Frontmatter, schemaFieldDetail{
					Key: f.Key, Ownership: string(f.Ownership()),
					Required: f.Required, Default: f.Default,
				})
			}
			for _, h := range s.Headings {
				out.Sections = append(out.Sections, schemaHeadingInfo{
					Text: h.Text, Required: h.Required,
					IDNamespace: h.IDNamespace, FreeProse: h.FreeProse,
				})
			}
			return writeJSON(out)
		}
		fmt.Printf("type: %s (additionalSections=%s)\n\nfrontmatter:\n", s.Type, s.AdditionalSections)
		for _, f := range s.Frontmatter {
			req := ""
			if f.Required {
				req = " required"
			}
			fmt.Printf("  %-10s %-6s%s\n", f.Key, f.Ownership(), req)
		}
		fmt.Println("\nsections:")
		for _, h := range s.Headings {
			var tags []string
			if h.Required {
				tags = append(tags, "required")
			} else {
				tags = append(tags, "optional")
			}
			if h.IDNamespace != "" {
				tags = append(tags, "ids="+h.IDNamespace)
			}
			if h.FreeProse {
				tags = append(tags, "free-prose")
			}
			fmt.Printf("  %-36s %s\n", h.Text, strings.Join(tags, " "))
		}
		return nil
	}
	return fmt.Errorf("schema: unknown action %q", action)
}
