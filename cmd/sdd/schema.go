package main

import (
	"fmt"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
)

func cmdSchema(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("schema: expected \"list\" or \"show <type>\"")
	}
	switch args[0] {
	case "list":
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
			fmt.Printf("%-8s %d sections (%d required)  %d namespaces  additionalSections=%s\n",
				s.Type, len(s.Headings), req, len(s.Namespaces), s.AdditionalSections)
		}
		return nil
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("schema show: expected an artifact type")
		}
		s, err := schema.Load(args[1])
		if err != nil {
			return err
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
	return fmt.Errorf("schema: unknown action %q", args[0])
}
