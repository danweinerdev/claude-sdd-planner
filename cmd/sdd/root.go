package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/internal/version"
	"github.com/spf13/cobra"
)

// The command tree. Cobra owns parsing, help, and suggestions; each command's
// RunE keeps calling the same cmdX(args) function that held its logic before,
// so the migration is confined to the CLI surface.
//
// Flags are GNU-style long form only (`--root`), which is what pflag accepts.
// The previous stdlib `flag` layer also took `-root`; that spelling is gone
// deliberately, as part of standardizing the interface before release.
//
// Two behaviors are load-bearing and must survive any future restructuring:
//   - exit codes (FR-03): 0 success, 1 refused mutation or authoritative
//     findings, 2 malformed invocation or the operation could not run. Cobra
//     defaults to 1 for every error, so main() maps them explicitly.
//   - `--json` on every subcommand that emits data (FR-04).
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "sdd",
		Short: "SDD toolchain — validation, artifact writes, hooks, provisioning",
		Long: `sdd is the deterministic half of the sdd-planner workflow: it compiles and
validates SDD artifacts, records completion evidence, drives lifecycle
transitions, maintains the decision ledger, and serves the plugin's hooks.

Artifacts are compiled documents. Create and modify them through this binary
rather than editing frontmatter by hand — a hand edit bypasses schema
compilation, digest tracking, and the refusal gates the workflow depends on.`,
		SilenceUsage:  true, // usage on a *runtime* error is noise
		SilenceErrors: true, // main() prints errors, with the right exit code
		// Suggestions replace the hand-rolled nearest()/editDistance() pair.
		SuggestionsMinimumDistance: 2,
		RunE: func(c *cobra.Command, args []string) error {
			return c.Help()
		},
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	root.AddCommand(
		versionCmd(),
		schemaCmd(),
		showCmd(),
		listCmd(),
		applyCmd(),
		sectionCmd(),
		templateCmd(),
		migrateCmd(),
		validateCmd(),
		nextCmd(),
		evidenceCmd(),
		reviewCmd(),
		decideCmd(),
		doctorCmd(),
		provisionCmd(),
		pluginCmd(),
		hookCmd(),
	)
	// task/phase/plan are three sibling roots sharing one handler.
	for _, level := range []string{"task", "phase", "plan"} {
		root.AddCommand(transitionCmd(level))
	}
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the binary's version",
		Args:  cobra.NoArgs,
		Run: func(c *cobra.Command, _ []string) {
			fmt.Fprintf(c.OutOrStdout(), "sdd %s\n", version.Version)
		},
	}
}

func schemaCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "schema",
		Short: "Inspect the embedded artifact schemas",
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List every artifact type and its section counts",
			Args:  cobra.NoArgs,
			RunE:  func(_ *cobra.Command, _ []string) error { return cmdSchema([]string{"list"}) },
		},
		&cobra.Command{
			Use:   "show <type>",
			Short: "Show one type's frontmatter fields and required sections",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return cmdSchema([]string{"show", args[0]})
			},
		},
	)
	return c
}

func showCmd() *cobra.Command {
	var jsonOut bool
	var typ string
	c := &cobra.Command{
		Use:   "show <artifact-path>",
		Short: "Print an artifact's frontmatter, sections, and content digest",
		Long: `Reports the artifact's parsed frontmatter, its declared sections, and the
content digest to pass back via --expect on a later write.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdShow(reassemble(args, flagPair("--json", jsonOut), flagVal("--type", typ)))
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	c.Flags().StringVar(&typ, "type", "spec", "artifact type to assume when frontmatter omits it")
	return c
}

func listCmd() *cobra.Command {
	var jsonOut bool
	var root string
	c := &cobra.Command{
		Use:       "list [spec|design|plan|research]",
		Short:     "List artifacts under the resolved planning root",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"spec", "design", "plan", "research"},
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdList(reassemble(args, flagPair("--json", jsonOut), flagVal("--root", root)))
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	c.Flags().StringVar(&root, "root", "", "planning root (default: resolved from planning-config.json)")
	return c
}

func applyCmd() *cobra.Command {
	var dryRun, diff, create, jsonOut, supersede bool
	var retire, expect, typ string
	c := &cobra.Command{
		Use:   "apply <artifact-path>",
		Short: "Compile a Markdown proposal from stdin into an artifact",
		Long: `Reads a full Markdown proposal on stdin, compiles it against the artifact's
schema, and writes it atomically. Refuses non-compliant structure — that
refusal is the reason writes go through a compiler at all.

Pass --expect with the digest from 'sdd show' to refuse the write if the
artifact changed underneath you.

--supersede rewrites an existing artifact wholesale: the payload restates the
content freely, and the artifact's identifiers are carried forward onto the
rewritten items in order, so FR/NFR/AC numbers everything else cites stay
stable. Without it, apply treats the payload as an edit, so a full rewrite
reads as every identifier being deleted at once.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdApply(reassemble(args,
				flagPair("--dry-run", dryRun), flagPair("--diff", diff),
				flagPair("--create", create), flagPair("--json", jsonOut),
				flagPair("--supersede", supersede),
				flagVal("--retire", retire), flagVal("--expect", expect),
				flagVal("--type", typ)))
		},
	}
	f := c.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "print what would be written and write nothing")
	f.BoolVar(&diff, "diff", false, "show a line diff against the artifact on disk")
	f.BoolVar(&create, "create", false, "treat the target as new even if it exists")
	f.BoolVar(&supersede, "supersede", false, "replace the artifact's content, carrying its identifiers forward")
	f.BoolVar(&jsonOut, "json", false, "emit the result as JSON")
	f.StringVar(&retire, "retire", "", "comma-separated identifiers being deliberately retired")
	f.StringVar(&expect, "expect", "", "refuse unless the artifact's current digest equals this value")
	f.StringVar(&typ, "type", "spec", "artifact type schema to compile against")
	return c
}

func sectionCmd() *cobra.Command {
	var dryRun, diff, jsonOut bool
	var heading, expect, typ string
	set := &cobra.Command{
		Use:   "set <artifact-path>",
		Short: "Replace one section's body, read from stdin",
		Long: `Replaces only the named section, leaving every other section and the
frontmatter (aside from 'updated') byte-identical.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdSection(reassemble(append([]string{"set"}, args...),
				flagVal("--heading", heading), flagPair("--dry-run", dryRun),
				flagPair("--diff", diff), flagPair("--json", jsonOut),
				flagVal("--expect", expect), flagVal("--type", typ)))
		},
	}
	f := set.Flags()
	f.StringVar(&heading, "heading", "", `declared section heading to replace, e.g. "## Overview"`)
	f.BoolVar(&dryRun, "dry-run", false, "print what would be written and write nothing")
	f.BoolVar(&diff, "diff", false, "show a line diff against the artifact on disk")
	f.BoolVar(&jsonOut, "json", false, "emit the result as JSON")
	f.StringVar(&expect, "expect", "", "refuse unless the artifact's current digest equals this value")
	f.StringVar(&typ, "type", "spec", "artifact type schema to check against")
	_ = set.MarkFlagRequired("heading")

	c := &cobra.Command{Use: "section", Short: "Section-scoped artifact edits"}
	c.AddCommand(set)
	return c
}

func templateCmd() *cobra.Command {
	var check, forApply, tmplJSON bool
	var out, dir string
	c := &cobra.Command{
		Use:   "template [type]",
		Short: "Print an artifact template, or check the committed set for drift",
		Long: `The default output is a complete artifact, including the frontmatter fields
the tool owns (type, status, created, updated) — the form to write to disk and
fill in by hand.

--for-apply omits those fields, producing a payload 'sdd apply' accepts: apply
sets them itself and refuses a payload carrying a conflicting value.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdTemplate(reassemble(args, flagVal("--out", out),
				flagPair("--check", check), flagVal("--dir", dir),
				flagPair("--for-apply", forApply), flagPair("--json", tmplJSON)))
		},
	}
	c.Flags().StringVar(&out, "out", "", "write to this path instead of stdout")
	c.Flags().BoolVar(&check, "check", false, "regenerate every committed template and diff")
	c.Flags().StringVar(&dir, "dir", "shared/templates", "template directory for --check")
	c.Flags().BoolVar(&forApply, "for-apply", false, "omit tool-owned fields, so the output is a valid apply payload")
	c.Flags().BoolVar(&tmplJSON, "json", false, "emit the result as JSON")
	return c
}

func migrateCmd() *cobra.Command {
	var dryRun, diff, jsonOut, allowFrozen bool
	var typ string
	c := &cobra.Command{
		Use:   "migrate <artifact-path>",
		Short: "Upgrade an artifact that predates the current schema",
		Long: `Inserts missing required sections and author frontmatter from schema
defaults, reporting every insertion. A separate verb on purpose: apply keeps
refusing non-compliant structure.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdMigrate(reassemble(args, flagPair("--dry-run", dryRun),
				flagPair("--diff", diff), flagPair("--json", jsonOut),
				flagPair("--allow-frozen", allowFrozen), flagVal("--type", typ)))
		},
	}
	f := c.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "report what would change and write nothing")
	f.BoolVar(&diff, "diff", false, "show a line diff")
	f.BoolVar(&jsonOut, "json", false, "emit the result as JSON")
	f.BoolVar(&allowFrozen, "allow-frozen", false, "permit migrating a frozen artifact")
	f.StringVar(&typ, "type", "", "artifact type to assume when frontmatter omits it")
	return c
}

func validateCmd() *cobra.Command {
	var jsonOut, noWaivers bool
	var root, scope, format string
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate every artifact under the planning root (read-only)",
		Long: `Schema-driven validation over a whole planning root: structure, statuses,
dependencies, identifiers, completion evidence, review state, and
decision-ledger consistency.

Exit 0 means the checks passed; exit 1 means the diagnostics are authoritative
findings; exit 2 means validation could not run.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdValidate(reassemble(nil, flagVal("--root", root),
				flagVal("--scope", scope), flagVal("--format", format),
				flagPair("--json", jsonOut), flagPair("--no-waivers", noWaivers)))
		},
	}
	f := c.Flags()
	f.StringVar(&root, "root", "", "planning root (default: resolved from planning-config.json)")
	f.StringVar(&scope, "scope", "", "limit findings to an artifact/path and paths it directly relates to")
	f.StringVar(&format, "format", "text", "output format: text|json")
	f.BoolVar(&jsonOut, "json", false, "shorthand for --format json")
	f.BoolVar(&noWaivers, "no-waivers", false, "ignore accepted exceptions and report every finding as an error")
	return c
}

func nextCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "next [plan-path]",
		Short: "Report current state and the literal next command to run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdNext(reassemble(args, flagPair("--json", jsonOut)))
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return c
}

func evidenceCmd() *cobra.Command {
	var phase, plan, dryRun, jsonOut bool
	var task, verifiedBy, workingDir, result string
	var tool, toolContext, toolResult string
	var focused, finalReview, date, revision string
	add := &cobra.Command{
		Use:   "add <artifact-path>",
		Short: "Record retrospective completion evidence",
		Long: `Records what actually ran — the exact command, its working directory, and
the observable result — for a task, a phase, or a plan. Evidence is the gate
every 'complete' transition checks.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdEvidence(reassemble(append([]string{"add"}, args...),
				flagVal("--task", task), flagPair("--phase", phase), flagPair("--plan", plan),
				flagVal("--verified-by", verifiedBy), flagVal("--working-dir", workingDir),
				flagVal("--result", result), flagVal("--tool", tool),
				flagVal("--tool-context", toolContext), flagVal("--tool-result", toolResult),
				flagVal("--focused-review", focused), flagVal("--final-review", finalReview),
				flagVal("--date", date), flagVal("--revision", revision),
				flagPair("--dry-run", dryRun), flagPair("--json", jsonOut)))
		},
	}
	f := add.Flags()
	f.StringVar(&task, "task", "", "record evidence for this task id")
	f.BoolVar(&phase, "phase", false, "record the phase's own completion evidence")
	f.BoolVar(&plan, "plan", false, "record the plan's own completion evidence")
	f.StringVar(&verifiedBy, "verified-by", "", "exact command that was run")
	f.StringVar(&workingDir, "working-dir", ".", "working directory for the command, repo-relative")
	f.StringVar(&result, "result", "", "observable evidence the command produced")
	f.StringVar(&tool, "tool", "", "optional tool/inspection row")
	f.StringVar(&toolContext, "tool-context", "", "context for the tool row")
	f.StringVar(&toolResult, "tool-result", "", "observable evidence for the tool row")
	f.StringVar(&focused, "focused-review", "", "exact focused-review command (tasks only)")
	f.StringVar(&finalReview, "final-review", "", "`<review path>; frozen: <range>` for the phase's final aligned review")
	f.StringVar(&date, "date", "", "verification date (default: today)")
	f.StringVar(&revision, "revision", "", "the task's own implementation commit (default: HEAD)")
	f.BoolVar(&dryRun, "dry-run", false, "print the section without writing")
	f.BoolVar(&jsonOut, "json", false, "emit the result as JSON")

	c := &cobra.Command{Use: "evidence", Short: "Completion-evidence records"}
	c.AddCommand(add)
	return c
}

func reviewCmd() *cobra.Command {
	var force, jsonOut bool
	var frozen, out, mode string
	scaffold := &cobra.Command{
		Use:   "scaffold <phase-path>",
		Short: "Create the persisted four-lane review artifact for a phase gate",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdReview(reassemble(append([]string{"scaffold"}, args...),
				flagVal("--frozen", frozen), flagVal("--out", out),
				flagVal("--mode", mode), flagPair("--force", force),
				flagPair("--json", jsonOut)))
		},
	}
	f := scaffold.Flags()
	f.StringVar(&frozen, "frozen", "", "the reviewed identity: <full40>..<full40>")
	f.StringVar(&out, "out", "", "output path (default: Retro/<phase>-review.md)")
	f.StringVar(&mode, "mode", "independent", "independent | mixed | single-agent")
	f.BoolVar(&force, "force", false, "overwrite an existing review artifact")
	f.BoolVar(&jsonOut, "json", false, "emit the result as JSON")
	_ = scaffold.MarkFlagRequired("frozen")

	c := &cobra.Command{Use: "review", Short: "Persisted review artifacts"}
	c.AddCommand(scaffold)
	return c
}

func decideCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "decide",
		Short: "Read and write the decision ledger",
		Long: `The decision ledger is the persistent record of decided truths. Accepted
entries are standing constraints; 'add' refuses on a candidate collision with
an accepted entry unless --supersedes names it.`,
	}

	var listJSON bool
	var status string
	list := &cobra.Command{
		Use: "list", Short: "List ledger entries", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdDecide(reassemble([]string{"list"}, flagVal("--status", status), flagPair("--json", listJSON)))
		},
	}
	list.Flags().StringVar(&status, "status", "", "filter: accepted|proposed|rejected|superseded")
	list.Flags().BoolVar(&listJSON, "json", false, "emit JSON")

	var searchJSON bool
	search := &cobra.Command{
		Use: "search <term>", Short: "Search ledger statements", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdDecide(reassemble([]string{"search", args[0]}, flagPair("--json", searchJSON)))
		},
	}
	search.Flags().BoolVar(&searchJSON, "json", false, "emit JSON")

	var accept, addDry, addJSON bool
	var statement, rationale, rejected, scope, tags, supersedes, kind, reversibility string
	add := &cobra.Command{
		Use: "add", Short: "Append a decision (collision-checked)", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdDecide(reassemble([]string{"add"},
				flagVal("--statement", statement), flagVal("--rationale", rationale),
				flagVal("--rejected", rejected), flagVal("--scope", scope),
				flagVal("--tags", tags), flagVal("--supersedes", supersedes),
				flagVal("--kind", kind), flagVal("--reversibility", reversibility),
				flagPair("--accept", accept), flagPair("--dry-run", addDry),
				flagPair("--json", addJSON)))
		},
	}
	af := add.Flags()
	af.StringVar(&statement, "statement", "", "the decided statement (required)")
	af.StringVar(&rationale, "rationale", "", "why this over the alternatives")
	af.StringVar(&rejected, "rejected", "", "comma-separated anti-choices")
	af.StringVar(&scope, "scope", "", "comma-separated governed artifacts")
	af.StringVar(&tags, "tags", "", "comma-separated tags")
	af.StringVar(&supersedes, "supersedes", "", "D-NNNN this entry supersedes")
	af.StringVar(&kind, "kind", "decision", "decision|assumption|definition|answered-question")
	af.StringVar(&reversibility, "reversibility", "two-way", "one-way|two-way")
	af.BoolVar(&accept, "accept", false, "record as accepted rather than proposed")
	af.BoolVar(&addDry, "dry-run", false, "print what would be written and write nothing")
	af.BoolVar(&addJSON, "json", false, "emit JSON")
	_ = add.MarkFlagRequired("statement")

	var valJSON, noHistory bool
	var valFormat string
	validate := &cobra.Command{
		Use:   "validate [ledger-path]",
		Short: "Audit the ledger's format, ids, supersession, and immutability",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdDecideValidate(reassemble(args,
				flagVal("--format", valFormat),
				flagPair("--json", valJSON), flagPair("--no-history", noHistory)))
		},
	}
	validate.Flags().StringVar(&valFormat, "format", "text", "output format: text|json")
	validate.Flags().BoolVar(&valJSON, "json", false, "shorthand for --format json")
	validate.Flags().BoolVar(&noHistory, "no-history", false, "skip Git history checks; only for an explicitly unversioned audit")

	c.AddCommand(list, search, add, validate)
	return c
}

// transitionCmd builds one of `task`, `phase`, or `plan`. All three share the
// cmdTransition handler, dispatched by the level string exactly as the switch
// in main() used to do.
func transitionCmd(level string) *cobra.Command {
	c := &cobra.Command{
		Use:   level,
		Short: fmt.Sprintf("Lifecycle transitions for a %s", level),
	}
	var id string
	var dryRun, jsonOut bool
	complete := &cobra.Command{
		Use:   "complete <path>",
		Short: fmt.Sprintf("Mark a %s complete (evidence-gated)", level),
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdTransition(level, reassemble(append([]string{"complete"}, args...),
				flagVal("--id", id), flagPair("--dry-run", dryRun),
				flagPair("--json", jsonOut)))
		},
	}
	if level == "task" {
		complete.Flags().StringVar(&id, "id", "", "task id to transition")
		_ = complete.MarkFlagRequired("id")
	}
	complete.Flags().BoolVar(&dryRun, "dry-run", false, "report the outcome without writing")
	complete.Flags().BoolVar(&jsonOut, "json", false, "emit the result as JSON")
	c.AddCommand(complete)

	if level == "plan" {
		var apDry, apJSON bool
		approve := &cobra.Command{
			Use: "approve <plan-path>", Short: "Mark a plan approved", Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return cmdTransition(level, reassemble(append([]string{"approve"}, args...),
					flagPair("--dry-run", apDry), flagPair("--json", apJSON)))
			},
		}
		approve.Flags().BoolVar(&apDry, "dry-run", false, "report the outcome without writing")
		approve.Flags().BoolVar(&apJSON, "json", false, "emit the result as JSON")
		c.AddCommand(approve)
	}
	return c
}

func doctorCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Report binary identity, planning root, and schema set",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdDoctor(reassemble(nil, flagPair("--json", jsonOut)))
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return c
}

func provisionCmd() *cobra.Command {
	var jsonOut, checkOnly bool
	var pluginRoot string
	c := &cobra.Command{
		Use:   "provision",
		Short: "Resolve an sdd binary and refresh the plugin-root copy",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdProvision(reassemble(nil, flagVal("--plugin-root", pluginRoot),
				flagPair("--json", jsonOut), flagPair("--check", checkOnly)))
		},
	}
	c.Flags().StringVar(&pluginRoot, "plugin-root", "", "plugin root (default: $CLAUDE_PLUGIN_ROOT)")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit the outcome as JSON")
	c.Flags().BoolVar(&checkOnly, "check", false, "resolve and report without writing the copy")
	return c
}

func pluginCmd() *cobra.Command {
	var root string
	var jsonOut bool
	c := &cobra.Command{
		Use:   "plugin",
		Short: "Maintain the generated Codex/OpenCode plugin trees",
		Long: `The repo root is the canonical Claude plugin; .codex-plugin/ and
.opencode-plugin/ are generated from it. 'sync' regenerates them, 'check'
fails when either is stale, and 'status' reports each file's provenance.`,
	}
	for _, sub := range []struct{ use, short string }{
		{"sync", "Regenerate both portable trees"},
		{"check", "Fail if a portable tree is stale"},
		{"status", "Print generated/variant/override provenance"},
	} {
		s := sub
		sc := &cobra.Command{
			Use: s.use, Short: s.short, Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				return cmdPlugin(reassemble([]string{s.use}, flagVal("--root", root),
					flagPair("--json", jsonOut)))
			},
		}
		sc.Flags().StringVar(&root, "root", ".", "repository root")
		sc.Flags().BoolVar(&jsonOut, "json", false, "emit the result as JSON")
		c.AddCommand(sc)
	}
	return c
}

func hookCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hook",
		Short: "Serve the plugin's hooks (reads a payload on stdin)",
		Long: `Both events fail open on every error path and always exit 0: a hook that
exits nonzero or emits malformed JSON degrades the session for every later
call, which is worse than a missed denial.`,
	}
	for _, ev := range []struct{ use, short string }{
		{"pretooluse", "Guard read-only agents' Bash/Write/Edit calls"},
		{"sessionstart", "Inject accepted decision-ledger entries as context"},
	} {
		e := ev
		c.AddCommand(&cobra.Command{
			Use: e.use, Short: e.short, Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error { return cmdHook([]string{e.use}) },
		})
	}
	return c
}

// reassemble rebuilds the argv slice the cmdX functions still parse. Cobra owns
// the user-facing surface — parsing, validation, help, suggestions — and this
// adapter feeds the existing handlers without rewriting their bodies. Only
// flags the user actually set are emitted, so each handler's own defaults
// continue to apply.
func reassemble(positional []string, flags ...[]string) []string {
	out := append([]string{}, positional...)
	for _, f := range flags {
		out = append(out, f...)
	}
	return out
}

// flagVal emits `--name value` when value is non-empty.
func flagVal(name, value string) []string {
	if value == "" {
		return nil
	}
	return []string{name, value}
}

// flagPair emits `--name` when set.
func flagPair(name string, set bool) []string {
	if !set {
		return nil
	}
	return []string{name}
}

// exitCode maps an error to the FR-03 code. Cobra returns 1 for everything,
// which would collapse "refused" and "could not run" into one signal.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var re *refusedError
	if errors.As(err, &re) {
		return 1
	}
	return 2
}

func usageHint(err error) bool {
	s := err.Error()
	return strings.Contains(s, "unknown flag") ||
		strings.Contains(s, "unknown command") ||
		strings.Contains(s, "unknown shorthand") ||
		strings.Contains(s, "accepts") ||
		strings.Contains(s, "required flag")
}
