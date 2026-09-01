package main

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/hook"
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
		"apply": true, "compile": true, "decide": true, "doctor": true, "evidence": true,
		"graph": true,
		"hook": true, "list": true, "migrate": true, "next": true,
		"phase": true, "plan": true, "plugin": true, "provision": true,
		"review": true, "schema": true, "section": true, "show": true,
		"spec": true, "design": true,
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

// TestGuardClassifiesEverySubcommand enforces FR-44 against the real command
// tree: every top-level verb and every `sdd graph` sub-verb must be
// deliberately classified in the hook's exported read-only maps. A verb
// added to the binary without a guard posture fails here — classification
// is never an accident of the allowlist's default-deny.
func TestGuardClassifiesEverySubcommand(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		n := c.Name()
		if n == "help" || n == "completion" {
			continue
		}
		if _, classified := hook.SddVerbReadOnly[n]; !classified {
			t.Errorf("subcommand %q has no guard classification; add it to hook.SddVerbReadOnly deliberately (FR-44)", n)
		}
		if n != "graph" {
			continue
		}
		for _, sub := range c.Commands() {
			sn := sub.Name()
			if sn == "help" || sn == "completion" {
				continue
			}
			if _, classified := hook.SddGraphVerbReadOnly[sn]; !classified {
				t.Errorf("graph sub-verb %q has no guard classification; add it to hook.SddGraphVerbReadOnly deliberately (FR-44)", sn)
			}
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
		"sdd task":              "group",
		"sdd phase":             "group",
		"sdd plan":              "group",
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
		{"sdd schema", []string{"json"}},
		{"sdd schema list", []string{"json"}},
		{"sdd schema show", []string{"json"}},
		{"sdd show", []string{"json", "type"}},
		{"sdd list", []string{"json", "root"}},
		{"sdd apply", []string{"dry-run", "quiet", "diff", "create", "json", "retire", "expect", "type", "supersede"}},
		{"sdd section set", []string{"heading", "dry-run", "diff", "json", "expect", "type"}},
		{"sdd template", []string{"out", "check", "dir", "for-apply", "schema", "json"}},
		{"sdd migrate", []string{"dry-run", "diff", "json", "allow-frozen", "no-stub-sections", "type", "all"}},
		{"sdd validate", []string{"root", "scope", "format", "json", "no-waivers"}},
		{"sdd next", []string{"json", "claim", "by"}},
		{"sdd graph release", []string{"plan", "by", "force", "json"}},
		{"sdd graph sync", []string{"plan", "node", "by", "report", "command-exit", "command-log", "json"}},
		{"sdd graph review", []string{"plan", "node", "artifact", "by", "json"}},
		{"sdd graph path", []string{"plan", "json"}},
		{"sdd graph risk", []string{"plan", "json"}},
		{"sdd graph shape", []string{"plan", "json"}},
		{"sdd graph status", []string{"plan", "json"}},
		{"sdd graph show", []string{"plan", "json"}},
		{"sdd graph export", []string{"plan", "format", "json"}},
		{"sdd graph split", []string{"plan", "node", "file", "json"}},
		{"sdd graph set-tests", []string{"plan", "node", "by", "file", "json"}},
		{"sdd graph gc", []string{"plan", "json"}},
		{"sdd graph hazards", []string{"json"}},
		{"sdd graph init", []string{"plan", "json"}},
		{"sdd graph propose", []string{"plan", "file", "json"}},
		{"sdd graph assemble", []string{"plan", "json"}},
		{"sdd compile", []string{"plan", "json"}},
		{"sdd graph convert", []string{"plan", "json"}},
		{"sdd evidence add", []string{
			"task", "phase", "plan", "verified-by", "working-dir", "result",
			"tool", "tool-context", "tool-result", "focused-review",
			"final-review", "date", "revision", "dry-run", "json"}},
		{"sdd review scaffold", []string{"frozen", "out", "mode", "force", "json"}},
		{"sdd review evidence set", []string{"lane", "evidence", "dry-run", "json"}},
		{"sdd review resolve", []string{"accept-followups", "dry-run", "json"}},
		{"sdd decide list", []string{"status", "json"}},
		{"sdd decide search", []string{"json"}},
		{"sdd decide add", []string{
			"statement", "rationale", "rejected", "scope", "tags", "supersedes",
			"kind", "reversibility", "accept", "dry-run", "json"}},
		{"sdd decide validate", []string{"format", "json", "no-history"}},
		{"sdd doctor", []string{"json", "check"}},
		{"sdd provision", []string{"plugin-root", "json", "check"}},
		{"sdd plugin sync", []string{"root", "json"}},
		{"sdd plugin check", []string{"root", "json"}},
		{"sdd plugin status", []string{"root", "json"}},
		{"sdd task complete", []string{"id", "dry-run", "json"}},
		{"sdd phase complete", []string{"dry-run", "json"}},
		{"sdd plan complete", []string{"dry-run", "json"}},
		{"sdd plan approve", []string{"dry-run", "json"}},
		{"sdd plan activate", []string{"dry-run", "json"}},
		{"sdd spec submit", []string{"dry-run", "json"}},
		{"sdd spec approve", []string{"dry-run", "json"}},
		{"sdd spec implement", []string{"dry-run", "json"}},
		{"sdd spec supersede", []string{"by", "dry-run", "json"}},
		{"sdd design submit", []string{"dry-run", "json"}},
		{"sdd design approve", []string{"dry-run", "json"}},
		{"sdd design implement", []string{"dry-run", "json"}},
		{"sdd design supersede", []string{"by", "dry-run", "json"}},
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
