package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/version"
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
		graphCmd(),
	)
	// spec/design lifecycle verbs share one kind-parameterized handler.
	root.AddCommand(docCmd("spec"), docCmd("design"))
	// task/phase/plan are three sibling roots sharing one handler.
	for _, level := range []string{"task", "phase", "plan"} {
		root.AddCommand(transitionCmd(level))
	}
	refuseUnknownSubcommands(root)
	return root
}

// refuseUnknownSubcommands makes every command group error on an unrecognized
// verb instead of printing help and exiting 0.
//
// Cobra's default for a group with no RunE is to show help successfully, so
// `sdd spec bogus path.md` — a typo'd verb — looked like a no-op success and
// scripted callers saw exit 0 with the artifact untouched. A group invoked
// bare still prints help (that is a real request for it); a group given
// arguments it cannot dispatch now names the bad verb and exits non-zero.
func refuseUnknownSubcommands(root *cobra.Command) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		if !c.HasSubCommands() || c.RunE != nil || c.Run != nil {
			return
		}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("%s: unknown subcommand %q\n\n%s",
				cmd.CommandPath(), args[0], cmd.UsageString())
		}
	}
	walk(root)
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
	var schemaJSON, listJSON, showJSON bool
	c := &cobra.Command{
		Use:   "schema",
		Short: "Inspect the embedded artifact schemas",
		Args:  cobra.MaximumNArgs(1),
		// A bare type name (`sdd schema spec`) used to fall through to
		// cobra's help text with exit 0 — indistinguishable from "this type
		// has no schema". Treat it as shorthand for `schema show <type>`;
		// an unknown type is then a real error from cmdSchema.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return cmdSchema("show", args[0], schemaJSON)
		},
	}
	c.Flags().BoolVar(&schemaJSON, "json", false, "emit the schema as JSON")
	list := &cobra.Command{
		Use:   "list",
		Short: "List every artifact type and its section counts",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return cmdSchema("list", "", listJSON) },
	}
	list.Flags().BoolVar(&listJSON, "json", false, "emit the type list as JSON")

	show := &cobra.Command{
		Use:   "show <type>",
		Short: "Show one type's frontmatter fields and required sections",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdSchema("show", args[0], showJSON)
		},
	}
	show.Flags().BoolVar(&showJSON, "json", false, "emit the schema as JSON")

	c.AddCommand(list, show)
	return c
}

func showCmd() *cobra.Command {
	var o showOpts
	c := &cobra.Command{
		Use:   "show <artifact-path>",
		Short: "Print an artifact's frontmatter, sections, and content digest",
		Long: `Reports the artifact's parsed frontmatter, its declared sections, and the
content digest to pass back via --expect on a later write.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdShow(args[0], o)
		},
	}
	c.Flags().BoolVar(&o.JSON, "json", false, "emit JSON")
	c.Flags().StringVar(&o.Type, "type", "spec", "artifact type to assume when frontmatter omits it")
	return c
}

func listCmd() *cobra.Command {
	var o listOpts
	c := &cobra.Command{
		Use:       "list [spec|design|plan|research]",
		Short:     "List artifacts under the resolved planning root",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"spec", "design", "plan", "research"},
		RunE: func(_ *cobra.Command, args []string) error {
			artifactType := ""
			if len(args) == 1 {
				artifactType = args[0]
			}
			return cmdList(artifactType, o)
		},
	}
	c.Flags().BoolVar(&o.JSON, "json", false, "emit JSON")
	c.Flags().StringVar(&o.Root, "root", "", "planning root (default: resolved from planning-config.json)")
	return c
}

func applyCmd() *cobra.Command {
	var o applyOpts
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
			return cmdApply(args[0], o)
		},
	}
	f := c.Flags()
	f.BoolVar(&o.DryRun, "dry-run", false, "print what would be written and write nothing")
	f.BoolVar(&o.Quiet, "quiet", false, "with --dry-run: report the verdict without dumping the whole document")
	f.BoolVar(&o.Diff, "diff", false, "show a line diff against the artifact on disk")
	f.BoolVar(&o.Create, "create", false, "treat the target as new even if it exists")
	f.BoolVar(&o.Supersede, "supersede", false, "replace the artifact's content, carrying its identifiers forward")
	f.BoolVar(&o.JSON, "json", false, "emit the result as JSON")
	f.StringVar(&o.Retire, "retire", "", "comma-separated identifiers being deliberately retired")
	f.StringVar(&o.Expect, "expect", "", "refuse unless the artifact's current digest equals this value")
	f.StringVar(&o.Type, "type", "", "artifact type schema to compile against (default: resolved from the artifact's or payload's `type:` frontmatter)")
	return c
}

func sectionCmd() *cobra.Command {
	var o sectionSetOpts
	set := &cobra.Command{
		Use:   "set <artifact-path>",
		Short: "Replace one section's body, read from stdin",
		Long: `Replaces only the named section, leaving every other section and the
frontmatter (aside from 'updated') byte-identical.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdSectionSet(args[0], o)
		},
	}
	f := set.Flags()
	f.StringVar(&o.Heading, "heading", "", `declared section heading to replace, e.g. "## Overview"`)
	f.BoolVar(&o.DryRun, "dry-run", false, "print what would be written and write nothing")
	f.BoolVar(&o.Diff, "diff", false, "show a line diff against the artifact on disk")
	f.BoolVar(&o.JSON, "json", false, "emit the result as JSON")
	f.StringVar(&o.Expect, "expect", "", "refuse unless the artifact's current digest equals this value")
	f.StringVar(&o.Type, "type", "", "artifact type schema to check against (default: resolved from the artifact's `type:` frontmatter)")
	_ = set.MarkFlagRequired("heading")

	c := &cobra.Command{Use: "section", Short: "Section-scoped artifact edits"}
	c.AddCommand(set)
	return c
}

func templateCmd() *cobra.Command {
	var o templateOpts
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
			artifactType := ""
			if len(args) == 1 {
				artifactType = args[0]
			}
			return cmdTemplate(artifactType, o)
		},
	}
	c.Flags().StringVar(&o.Out, "out", "", "write to this path instead of stdout")
	c.Flags().BoolVar(&o.Check, "check", false, "regenerate every committed template and diff")
	c.Flags().StringVar(&o.Dir, "dir", "shared/templates", "template directory for --check")
	c.Flags().BoolVar(&o.ForApply, "for-apply", false, "omit tool-owned fields, so the output is a valid apply payload")
	c.Flags().BoolVar(&o.JSON, "json", false, "emit the result as JSON")
	return c
}

func migrateCmd() *cobra.Command {
	var o migrateOpts
	c := &cobra.Command{
		Use:   "migrate <artifact-path>",
		Short: "Upgrade an artifact that predates the current schema",
		Long: `Inserts missing required sections and author frontmatter from schema
defaults, reporting every insertion. A separate verb on purpose: apply keeps
refusing non-compliant structure.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return cmdMigrate(target, o)
		},
	}
	f := c.Flags()
	f.BoolVar(&o.DryRun, "dry-run", false, "report what would change and write nothing")
	f.BoolVar(&o.Diff, "diff", false, "show a line diff")
	f.BoolVar(&o.JSON, "json", false, "emit the result as JSON")
	f.BoolVar(&o.AllowFrozen, "allow-frozen", false, "also migrate complete/frozen artifacts (FR-46 exemption)")
	f.BoolVar(&o.NoStubs, "no-stub-sections", false, "do not insert placeholder bodies for missing required sections")
	f.StringVar(&o.Type, "type", "", "artifact type to assume when frontmatter omits it")
	f.BoolVar(&o.All, "all", false, "migrate every artifact under the planning root and print a summary worklist")
	return c
}

func validateCmd() *cobra.Command {
	var o validateOpts
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
			return cmdValidate(o)
		},
	}
	f := c.Flags()
	f.StringVar(&o.Root, "root", "", "planning root (default: resolved from planning-config.json)")
	f.StringVar(&o.Scope, "scope", "", "limit findings to an artifact/path and paths it directly relates to")
	f.StringVar(&o.Format, "format", "text", "output format: text|json")
	f.BoolVar(&o.JSON, "json", false, "shorthand for --format json")
	f.BoolVar(&o.NoWaivers, "no-waivers", false, "ignore accepted exceptions and report every finding as an error")
	return c
}

func nextCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "next [plan-path]",
		Short: "Report current state and the literal next command to run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			planPath := ""
			if len(args) == 1 {
				planPath = args[0]
			}
			return cmdNext(planPath, jsonOut)
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return c
}

func evidenceCmd() *cobra.Command {
	var o evidenceOpts
	add := &cobra.Command{
		Use:   "add <artifact-path>",
		Short: "Record retrospective completion evidence",
		Long: `Records what actually ran — the exact command, its working directory, and
the observable result — for a task, a phase, or a plan. Evidence is the gate
every 'complete' transition checks.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdEvidenceAdd(args[0], o)
		},
	}
	f := add.Flags()
	f.StringVar(&o.Task, "task", "", "record evidence for this task id")
	f.BoolVar(&o.Phase, "phase", false, "record the phase's own completion evidence")
	f.BoolVar(&o.Plan, "plan", false, "record the plan's own completion evidence")
	f.StringVar(&o.VerifiedBy, "verified-by", "", "exact command that was run")
	f.StringVar(&o.WorkingDir, "working-dir", ".", "working directory for the command, repo-relative")
	f.StringVar(&o.Result, "result", "", "observable evidence the command produced")
	f.StringVar(&o.Tool, "tool", "", "optional tool/inspection row")
	f.StringVar(&o.ToolContext, "tool-context", "", "context for the tool row")
	f.StringVar(&o.ToolResult, "tool-result", "", "observable evidence for the tool row")
	f.StringVar(&o.Focused, "focused-review", "", "exact focused-review command (tasks only)")
	f.StringVar(&o.FinalReview, "final-review", "", "`<review path>; frozen: <range>` for the phase's final aligned review")
	f.StringVar(&o.Date, "date", "", "verification date (default: today)")
	f.StringVar(&o.Revision, "revision", "", "the task's own implementation commit (default: HEAD)")
	f.BoolVar(&o.DryRun, "dry-run", false, "print the section without writing")
	f.BoolVar(&o.JSON, "json", false, "emit the result as JSON")

	c := &cobra.Command{Use: "evidence", Short: "Completion-evidence records"}
	c.AddCommand(add)
	return c
}

func reviewCmd() *cobra.Command {
	var o reviewScaffoldOpts
	scaffold := &cobra.Command{
		Use:   "scaffold <phase-path>",
		Short: "Create the persisted four-lane review artifact for a phase gate",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdReviewScaffold(args[0], o)
		},
	}
	f := scaffold.Flags()
	f.StringVar(&o.Frozen, "frozen", "", "the reviewed identity: <full40>..<full40>")
	f.StringVar(&o.Out, "out", "", "output path (default: <plan>/reviews/<NN>-<plan>-code-review-<rev>.md)")
	f.StringVar(&o.Mode, "mode", "independent", "independent | mixed | single-agent")
	f.BoolVar(&o.Force, "force", false, "overwrite an existing review artifact")
	f.BoolVar(&o.JSON, "json", false, "emit the result as JSON")
	_ = scaffold.MarkFlagRequired("frozen")

	var eo reviewEvidenceOpts
	evidenceSet := &cobra.Command{
		Use:   "set <review-path>",
		Short: "Record one lane's concrete observation on an open review",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdReviewEvidenceSet(args[0], eo)
		},
	}
	ef := evidenceSet.Flags()
	ef.StringVar(&eo.Lane, "lane", "", "stable lane identifier (review_plan_drift | review_quality | review_spec_compliance | review_blind_spots)")
	ef.StringVar(&eo.Evidence, "evidence", "", "the lane's observation (default: read from stdin)")
	ef.BoolVar(&eo.DryRun, "dry-run", false, "check without writing")
	ef.BoolVar(&eo.JSON, "json", false, "emit the result as JSON")
	_ = evidenceSet.MarkFlagRequired("lane")
	evidence := &cobra.Command{Use: "evidence", Short: "Lane evidence on an open review"}
	evidence.AddCommand(evidenceSet)

	var ro reviewResolveOpts
	resolve := &cobra.Command{
		Use:   "resolve <review-path>",
		Short: "Close an open phase-gate review: verify the gate, then set frozen: true and status: resolved",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdReviewResolve(args[0], ro)
		},
	}
	rf := resolve.Flags()
	rf.BoolVar(&ro.AcceptFollowups, "accept-followups", false, "resolve despite untracked followups the user explicitly accepted as floating")
	rf.BoolVar(&ro.DryRun, "dry-run", false, "report the verdict without writing")
	rf.BoolVar(&ro.JSON, "json", false, "emit the result as JSON")

	c := &cobra.Command{Use: "review", Short: "Persisted review artifacts"}
	c.AddCommand(scaffold, evidence, resolve)
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
			return cmdDecideList(status, listJSON)
		},
	}
	list.Flags().StringVar(&status, "status", "", "filter: accepted|proposed|rejected|superseded")
	list.Flags().BoolVar(&listJSON, "json", false, "emit JSON")

	var searchJSON bool
	search := &cobra.Command{
		Use: "search <term>", Short: "Search ledger statements", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return cmdDecideSearch(args[0], searchJSON)
		},
	}
	search.Flags().BoolVar(&searchJSON, "json", false, "emit JSON")

	var a decideAddOpts
	add := &cobra.Command{
		Use: "add", Short: "Append a decision (collision-checked)", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdDecideAdd(a)
		},
	}
	af := add.Flags()
	af.StringVar(&a.Statement, "statement", "", "the decided statement (required)")
	af.StringVar(&a.Rationale, "rationale", "", "why this over the alternatives")
	af.StringVar(&a.Rejected, "rejected", "", "comma-separated anti-choices")
	af.StringVar(&a.Scope, "scope", "", "comma-separated governed artifacts")
	af.StringVar(&a.Tags, "tags", "", "comma-separated tags")
	af.StringVar(&a.Supersedes, "supersedes", "", "D-NNNN this entry supersedes")
	af.StringVar(&a.Kind, "kind", "decision", "decision|assumption|definition|answered-question")
	af.StringVar(&a.Reversibility, "reversibility", "two-way", "one-way|two-way")
	af.BoolVar(&a.Accept, "accept", false, "record as accepted rather than proposed")
	af.BoolVar(&a.DryRun, "dry-run", false, "print what would be written and write nothing")
	af.BoolVar(&a.JSON, "json", false, "emit JSON")
	_ = add.MarkFlagRequired("statement")

	var v decideValidateOpts
	validate := &cobra.Command{
		Use:   "validate [ledger-path]",
		Short: "Audit the ledger's format, ids, supersession, and immutability",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ledger := ""
			if len(args) == 1 {
				ledger = args[0]
			}
			return cmdDecideValidate(ledger, v)
		},
	}
	validate.Flags().StringVar(&v.Format, "format", "text", "output format: text|json")
	validate.Flags().BoolVar(&v.JSON, "json", false, "shorthand for --format json")
	validate.Flags().BoolVar(&v.NoHistory, "no-history", false, "skip Git history checks; only for an explicitly unversioned audit")

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
			return cmdComplete(level, args[0], completeOpts{ID: id, DryRun: dryRun, JSON: jsonOut})
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
		// approve and activate were both implemented in planLifecycle from the
		// start, but only approve was ever wired — `sdd plan activate` existed
		// in the usage text and nowhere else.
		for _, verb := range []struct{ name, short string }{
			{"approve", "Mark a plan approved"},
			{"activate", "Mark an approved plan active"},
		} {
			verb := verb
			var dry, js bool
			cmd := &cobra.Command{
				Use: verb.name + " <plan-path>", Short: verb.short, Args: cobra.ExactArgs(1),
				RunE: func(_ *cobra.Command, args []string) error {
					return planLifecycle(verb.name, args[0], dry, js)
				},
			}
			cmd.Flags().BoolVar(&dry, "dry-run", false, "report the outcome without writing")
			cmd.Flags().BoolVar(&js, "json", false, "emit the result as JSON")
			c.AddCommand(cmd)
		}
	}
	return c
}

// docCmd builds the `sdd spec` / `sdd design` lifecycle command: the status
// chain draft → review → approved → implemented (→ superseded) driven by
// verbs, mirroring the plan lifecycle, so spec/design statuses finally have a
// supported write path.
func docCmd(kind string) *cobra.Command {
	c := &cobra.Command{
		Use:   kind,
		Short: fmt.Sprintf("Lifecycle transitions for a %s", kind),
	}
	for _, verb := range []struct{ name, short string }{
		{"submit", fmt.Sprintf("Move a draft %s into review", kind)},
		{"approve", fmt.Sprintf("Mark a reviewed %s approved (validation-gated)", kind)},
		{"implement", fmt.Sprintf("Mark an approved %s implemented", kind)},
		{"supersede", fmt.Sprintf("Mark a %s superseded, optionally linking its replacement", kind)},
	} {
		verb := verb
		var by string
		var dry, js bool
		cmd := &cobra.Command{
			Use:   fmt.Sprintf("%s <%s-path>", verb.name, kind),
			Short: verb.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				// An explicitly empty --by is a mistake, not "no successor":
				// it silently produced `superseded_by: ""`, indistinguishable
				// from an intentional unlinked supersession. Omit the flag for
				// that.
				if c.Flags().Changed("by") && strings.TrimSpace(by) == "" {
					return fmt.Errorf("%s supersede: --by was given an empty value; "+
						"omit the flag entirely to supersede without linking a successor", kind)
				}
				return docLifecycle(kind, verb.name, args[0], strings.TrimSpace(by), dry, js)
			},
		}
		if verb.name == "supersede" {
			cmd.Flags().StringVar(&by, "by", "", "planning-root-relative path of the replacing artifact (recorded as superseded_by)")
		}
		cmd.Flags().BoolVar(&dry, "dry-run", false, "report the outcome without writing")
		cmd.Flags().BoolVar(&js, "json", false, "emit the result as JSON")
		c.AddCommand(cmd)
	}
	return c
}

func doctorCmd() *cobra.Command {
	var o doctorOpts
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the environment and repair the hook installation",
		Long: `Reports the binary in use, the resolved planning root, and the embedded
schema set, and regenerates hooks.json when it is absent or does not match this
version's hook set.

Run it once when starting to use sdd in a project: a stale hooks.json is
invisible otherwise, because the events it does declare keep firing while a
newly added one silently never runs. Pass --check to report without repairing.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdDoctor(o)
		},
	}
	c.Flags().BoolVar(&o.JSON, "json", false, "emit JSON")
	c.Flags().BoolVar(&o.Check, "check", false, "report only; do not repair the hooks file")
	return c
}

func provisionCmd() *cobra.Command {
	var o provisionOpts
	c := &cobra.Command{
		Use:   "provision",
		Short: "Resolve an sdd binary and refresh the plugin-root copy",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cmdProvision(o)
		},
	}
	c.Flags().StringVar(&o.PluginRoot, "plugin-root", "", "plugin root (default: $CLAUDE_PLUGIN_ROOT)")
	c.Flags().BoolVar(&o.JSON, "json", false, "emit the outcome as JSON")
	c.Flags().BoolVar(&o.Check, "check", false, "resolve and report without writing the copy")
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
				return cmdPlugin(s.use, root, jsonOut)
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
			RunE: func(_ *cobra.Command, _ []string) error { return cmdHook(e.use) },
		})
	}
	return c
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
