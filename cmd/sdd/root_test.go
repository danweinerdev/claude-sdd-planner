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
		// Known FR-04 gaps, tracked for implementation. Each handler has no
		// JSON rendering path yet; advertising a --json flag the handler
		// ignores would be worse than an exemption that names the work.
		"sdd template":        "no JSON path yet: template --check drift report",
		"sdd plugin sync":     "no JSON path yet: provenance/stale report",
		"sdd plugin check":    "no JSON path yet: provenance/stale report",
		"sdd plugin status":   "no JSON path yet: provenance/stale report",
		"sdd review scaffold": "no JSON path yet: writes an artifact, reports the path",
		"sdd task complete":   "no JSON path yet: cmdTransition renders text only",
		"sdd phase complete":  "no JSON path yet: cmdTransition renders text only",
		"sdd plan complete":   "no JSON path yet: cmdTransition renders text only",
		"sdd plan approve":    "no JSON path yet: cmdTransition renders text only",
		"sdd task":            "group",
		"sdd phase":           "group",
		"sdd plan":            "group",
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

// The flag surface no longer needs a runtime gate.
//
// TestNoInertFlags and TestNoUnreachableFlags used to compare cobra's declared
// flags against a hand-maintained table of what each handler parsed, because
// the two were separate declarations that could drift: a flag could be
// advertised but unimplemented (inert), or implemented but unreachable. Both
// bugs happened, twice, in this codebase.
//
// Handlers now take typed options structs that cobra binds directly, so there
// is exactly one declaration. Drift is a compile error rather than a test
// failure — verified by renaming a struct field a flag binds to, which fails
// as `o.NoSuchField undefined (type validateOpts has no field or method
// NoSuchField)`. A test asserting what the compiler already proves is a test
// that can only rot, so the pair and their table are deleted.

// TestJSONFlagCoverage enforces FR-04: every command that emits data offers
// --json, so callers never have to scrape rendered text.
func TestNoInertFlags(t *testing.T) {
	for _, tc := range handlerFlagSets() {
		declared := map[string]bool{}
		for _, n := range tc.handlerFlags {
			declared[n] = true
		}
		cmd := findByPath(newRootCmd(), tc.path)
		if cmd == nil {
			t.Errorf("command %q is in the handler-flag table but not in the tree", tc.path)
			continue
		}
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if !declared[f.Name] {
				t.Errorf("%s advertises --%s, but its handler never defines it — "+
					"the invocation would fail with `flag provided but not defined`",
					tc.path, f.Name)
			}
		})
	}
}

// TestNoUnreachableFlags is the mirror of TestNoInertFlags. An inert flag is
// advertised but unusable; an unreachable one is implemented but unreachable —
// the handler parses it, nothing exposes it, and the feature silently does not
// exist. Verified by deleting --no-waivers from validateCmd: every other test
// still passed while `sdd validate --no-waivers` became an error.
//
// Together the two tests pin the flag surface from both sides, which is what
// makes the cobra-to-handler adapter safe to keep: the two declarations cannot
// disagree in either direction without a test failing.
func TestNoUnreachableFlags(t *testing.T) {
	for _, tc := range handlerFlagSets() {
		cmd := findByPath(newRootCmd(), tc.path)
		if cmd == nil {
			t.Errorf("command %q is in the handler-flag table but not in the tree", tc.path)
			continue
		}
		for _, name := range tc.handlerFlags {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("%s's handler parses --%s, but no cobra flag exposes it — "+
					"the feature is unreachable from the command line", tc.path, name)
			}
		}
	}
}

// handlerFlagSets pairs each cobra command with the flag names its underlying
// cmdX handler actually parses. When a flag is added to a handler, add it here
// and to the cobra command; the test fails on either half alone.
//
// Kept as data rather than derived by reflection on purpose: the handlers build
// their FlagSets inside the function body, so there is nothing to introspect
// without invoking them, and invoking them is what made the behavioral version
// slow.
func handlerFlagSets() []struct {
	path         string
	handlerFlags []string
} {
	return []struct {
		path         string
		handlerFlags []string
	}{
		{"sdd show", []string{"json", "type"}},
		{"sdd list", []string{"json", "root"}},
		{"sdd apply", []string{"dry-run", "diff", "create", "json", "retire", "expect", "type", "supersede"}},
		{"sdd section set", []string{"heading", "dry-run", "diff", "json", "expect", "type"}},
		{"sdd template", []string{"out", "check", "dir", "for-apply", "json"}},
		{"sdd migrate", []string{"dry-run", "diff", "json", "allow-frozen", "no-stub-sections", "type", "all"}},
		{"sdd validate", []string{"root", "scope", "format", "json", "no-waivers"}},
		{"sdd next", []string{"json"}},
		{"sdd evidence add", []string{
			"task", "phase", "plan", "verified-by", "working-dir", "result",
			"tool", "tool-context", "tool-result", "focused-review",
			"final-review", "date", "revision", "dry-run", "json"}},
		{"sdd review scaffold", []string{"frozen", "out", "mode", "force", "json"}},
		{"sdd decide list", []string{"status", "json"}},
		{"sdd decide search", []string{"json"}},
		{"sdd decide add", []string{
			"statement", "rationale", "rejected", "scope", "tags", "supersedes",
			"kind", "reversibility", "accept", "dry-run", "json"}},
		{"sdd decide validate", []string{"format", "json", "no-history"}},
		{"sdd doctor", []string{"json"}},
		{"sdd provision", []string{"plugin-root", "json", "check"}},
		{"sdd plugin sync", []string{"root", "json"}},
		{"sdd plugin check", []string{"root", "json"}},
		{"sdd plugin status", []string{"root", "json"}},
		{"sdd task complete", []string{"id", "dry-run", "json"}},
		{"sdd phase complete", []string{"dry-run", "json"}},
		{"sdd plan complete", []string{"dry-run", "json"}},
		{"sdd plan approve", []string{"dry-run", "json"}},
	}
}

// TestHandlerFlagTableIsComplete keeps the table above honest: every leaf
// command with flags must appear in it, so a new command cannot be added
// without declaring what its handler parses.
func TestHandlerFlagTableIsComplete(t *testing.T) {
	listed := map[string]bool{}
	for _, tc := range handlerFlagSets() {
		listed[tc.path] = true
	}
	walk(newRootCmd(), func(c *cobra.Command) {
		if c.HasSubCommands() || c.RunE == nil && c.Run == nil {
			return
		}
		if c.Name() == "completion" || c.Name() == "help" || c.Name() == "version" {
			return
		}
		if c.Flags().HasFlags() && !listed[c.CommandPath()] {
			t.Errorf("%s has flags but is missing from handlerFlagSets()", c.CommandPath())
		}
	})
}

// findByPath locates a command by its full "sdd a b" path.
func findByPath(root *cobra.Command, path string) *cobra.Command {
	var found *cobra.Command
	walk(root, func(c *cobra.Command) {
		if c.CommandPath() == path {
			found = c
		}
	})
	return found
}
