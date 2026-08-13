package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// walk visits every command in the tree.
func walk(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, sub := range c.Commands() {
		walk(sub, fn)
	}
}

// TestEveryCommandDocumented is the gate that replaces the hand-maintained
// usage() literal. That string had already drifted before this migration —
// `decide validate` shipped without ever appearing in help, and the text still
// pointed at a Python validator deleted several releases earlier. Help is now
// generated from this tree, so the only way it can rot is a command added
// without a Short description.
func TestEveryCommandDocumented(t *testing.T) {
	walk(newRootCmd(), func(c *cobra.Command) {
		if c.Name() == "sdd" || c.Name() == "help" || c.Name() == "completion" {
			return
		}
		if strings.TrimSpace(c.Short) == "" {
			t.Errorf("%q has no Short description; it would appear blank in help", c.CommandPath())
		}
		// A parent that only groups subcommands needs no RunE, but a leaf
		// without one is a command that silently does nothing.
		if !c.HasSubCommands() && c.RunE == nil && c.Run == nil {
			t.Errorf("%q is a leaf with no Run/RunE", c.CommandPath())
		}
	})
}

// TestFlagsAreDocumented catches a flag added without usage text, which renders
// as a blank column in --help.
func TestFlagsAreDocumented(t *testing.T) {
	walk(newRootCmd(), func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if strings.TrimSpace(f.Usage) == "" {
				t.Errorf("%s --%s has no usage text", c.CommandPath(), f.Name)
			}
		})
	})
}

// TestSubcommandsMatchDispatch pins the command surface. A rename or removal is
// a breaking change for every skill and hook that shells out to this binary, so
// it must be a deliberate edit here rather than a silent side effect.
func TestSubcommandsMatchDispatch(t *testing.T) {
	want := map[string]bool{
		"apply": true, "decide": true, "doctor": true, "evidence": true,
		"hook": true, "list": true, "migrate": true, "next": true,
		"phase": true, "plan": true, "plugin": true, "provision": true,
		"review": true, "schema": true, "section": true, "show": true,
		"task": true, "template": true, "validate": true, "version": true,
	}
	got := map[string]bool{}
	for _, c := range newRootCmd().Commands() {
		n := c.Name()
		if n == "help" || n == "completion" { // cobra built-ins
			continue
		}
		got[n] = true
	}
	for n := range want {
		if !got[n] {
			t.Errorf("subcommand %q disappeared from the command tree", n)
		}
	}
	for n := range got {
		if !want[n] {
			t.Errorf("subcommand %q is new; add it to this list deliberately", n)
		}
	}
}

// TestJSONFlagCoverage enforces FR-04: every command that emits data offers
// --json, so callers never have to scrape rendered text.
func TestJSONFlagCoverage(t *testing.T) {
	// Commands that emit no machine-consumable data, with the reason.
	exempt := map[string]string{
		"sdd":                   "root",
		"sdd version":           "single literal line",
		"sdd help":              "cobra builtin",
		"sdd completion":        "shell script output",
		"sdd schema":            "group",
		"sdd schema list":       "human-oriented schema listing",
		"sdd schema show":       "human-oriented schema listing",
		"sdd section":           "group",
		"sdd evidence":          "group",
		"sdd review":            "group",
		"sdd decide":            "group",
		"sdd hook":              "emits its own hook JSON protocol",
		"sdd hook pretooluse":   "emits its own hook JSON protocol",
		"sdd hook sessionstart": "emits its own hook JSON protocol",
		"sdd plugin":            "group",
		"sdd plugin sync":       "progress reporting",
		"sdd plugin check":      "progress reporting",
		"sdd plugin status":     "progress reporting",
		"sdd template":          "emits template text or a drift report",
		"sdd review scaffold":   "writes an artifact and reports the path",
		// Pre-existing FR-04 gap, not introduced by the cobra migration:
		// cmdEvidence has no JSON rendering path at all, so advertising a
		// --json flag the handler ignores would be worse than this exemption.
		// Closing it means teaching cmdEvidence to emit the written section
		// and target as JSON, then deleting this line.
		"sdd evidence add": "handler has no JSON rendering path yet",
		"sdd task":         "group",
		"sdd phase":        "group",
		"sdd plan":         "group",
	}
	walk(newRootCmd(), func(c *cobra.Command) {
		path := c.CommandPath()
		if _, ok := exempt[path]; ok {
			return
		}
		if c.HasSubCommands() {
			return
		}
		if c.Flags().Lookup("json") == nil {
			t.Errorf("%s has no --json flag (FR-04); add it or exempt it with a reason", path)
		}
	})
}
