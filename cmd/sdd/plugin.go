package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/portable"
)

// cmdPlugin maintains the generated portable (OpenCode/Codex) plugin tree.
//
//	sdd plugin sync  [--root <repo>]   regenerate the portable trees from the canonical tree
//	sdd plugin check [--root <repo>]   fail (exit 1) if a portable tree is stale
//	sdd plugin status [--root <repo>]  print the generated/override provenance report
func cmdPlugin(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sdd plugin <sync|check|status> [--root <repo>]")
	}
	sub, rest := args[0], args[1:]
	root := "."
	jsonOut := false
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--root":
			if i+1 >= len(rest) {
				return fmt.Errorf("--root requires a path")
			}
			root = rest[i+1]
			i++
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("sdd plugin %s: unknown argument %q", sub, rest[i])
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	// The canonical tree is identified by its manifest; a wrong --root should
	// fail loudly, not generate an empty tree somewhere surprising.
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "plugin.json")); err != nil {
		return fmt.Errorf("%s is not the plugin repository root (no .claude-plugin/plugin.json)", root)
	}

	switch sub {
	case "sync":
		r, err := portable.Sync(root)
		if err != nil {
			return err
		}
		if jsonOut {
			return writeJSON(pluginResult{
				OK: true, Action: "sync", Trees: portable.OutDirs,
				Generated: r.Generated, Variants: r.Variants,
				Overridden: r.Overridden, PortableOnly: r.OverrideOnly,
			})
		}
		fmt.Printf("plugin sync: %s <- %d generated, %d variants, %d overridden, %d portable-only\n",
			strings.Join(portable.OutDirs, " + "), len(r.Generated), len(r.Variants), len(r.Overridden), len(r.OverrideOnly))
		return nil
	case "check":
		stale, err := portable.Check(root)
		if err != nil {
			return err
		}
		if jsonOut {
			res := pluginResult{OK: len(stale) == 0, Action: "check", Trees: portable.OutDirs, Stale: stale}
			if !res.OK {
				res.Remedy = "run `sdd plugin sync` and commit the result"
			}
			if err := writeJSON(res); err != nil {
				return err
			}
			if !res.OK {
				return &refusedError{n: len(stale)}
			}
			return nil
		}
		if len(stale) > 0 {
			for _, s := range stale {
				fmt.Fprintf(os.Stderr, "plugin check: STALE %s\n", s)
			}
			fmt.Fprintf(os.Stderr, "\n✗ portable trees are stale. Run: sdd plugin sync\n")
			return &refusedError{n: len(stale)}
		}
		fmt.Printf("✓ %s are in sync with the canonical tree\n", strings.Join(portable.OutDirs, " + "))
		return nil
	case "status":
		r, err := portable.Generate(root)
		if err != nil {
			return err
		}
		if jsonOut {
			return writeJSON(pluginResult{
				OK: true, Action: "status", Trees: portable.OutDirs,
				Generated: r.Generated, Variants: r.Variants,
				Overridden: r.Overridden, PortableOnly: r.OverrideOnly,
			})
		}
		fmt.Printf("generated (%d):\n", len(r.Generated))
		for _, p := range r.Generated {
			fmt.Printf("  %s\n", p)
		}
		fmt.Printf("variants — hand-maintained portable siblings (%d):\n", len(r.Variants))
		for _, p := range r.Variants {
			fmt.Printf("  %s\n", p)
		}
		fmt.Printf("overridden — convergence backlog (%d):\n", len(r.Overridden))
		for _, p := range r.Overridden {
			fmt.Printf("  %s\n", p)
		}
		fmt.Printf("portable-only (%d):\n", len(r.OverrideOnly))
		for _, p := range r.OverrideOnly {
			fmt.Printf("  %s\n", p)
		}
		return nil
	default:
		return fmt.Errorf("sdd plugin: unknown subcommand %q (want sync, check, or status)", sub)
	}
}

// pluginResult is the machine-readable outcome of a `sdd plugin` action
// (FR-04). check is a CI-shaped command — a pipeline should read which files
// are stale rather than parse the rendered report — and sync/status report the
// provenance of every generated file, which is the thing a maintainer wants
// when deciding whether a change belongs in the canonical tree or a variant.
type pluginResult struct {
	OK           bool     `json:"ok"`
	Action       string   `json:"action"`
	Trees        []string `json:"trees"`
	Generated    []string `json:"generated,omitempty"`
	Variants     []string `json:"variants,omitempty"`
	Overridden   []string `json:"overridden,omitempty"`
	PortableOnly []string `json:"portable_only,omitempty"`
	Stale        []string `json:"stale,omitempty"`
	Remedy       string   `json:"remedy,omitempty"`
}
