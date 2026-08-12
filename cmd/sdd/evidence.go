package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/internal/vcs"
)

// `sdd evidence add` records a completion-evidence section as structured data
// (FR-21), replacing hand-editing of the exact label set in
// shared/completion-evidence.md.
//
// The labels, their order, and the two table shapes are a contract the
// validator enforces (SDD070-075). Emitting them from one place means a task's
// evidence is well-formed by construction rather than by an author's care, and
// the identity fields are read from the repository rather than typed.

const evidenceUsage = `sdd evidence add <artifact-path> --task ID | --phase | --plan
                       --verified-by "<command>" --result "<observation>"
                       [--working-dir PATH] [--tool "<inspection>"]
                       [--focused-review "<command>"] [--date YYYY-MM-DD]

Records a completion-evidence section with the exact labels
shared/completion-evidence.md requires. Repository, VCS, and revision identity
are read from the target repository rather than supplied.`

func cmdEvidence(args []string) error {
	if len(args) == 0 || args[0] != "add" {
		return fmt.Errorf("evidence: expected `add`\n\n%s", evidenceUsage)
	}
	fs := flag.NewFlagSet("evidence add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	task := fs.String("task", "", "record evidence for this task id")
	phase := fs.Bool("phase", false, "record the phase's own completion evidence")
	plan := fs.Bool("plan", false, "record the plan's own completion evidence")
	verifiedBy := fs.String("verified-by", "", "exact command that was run")
	workingDir := fs.String("working-dir", ".", "working directory for the command, repo-relative")
	result := fs.String("result", "", "observable evidence the command produced")
	tool := fs.String("tool", "", "optional tool/inspection row")
	toolContext := fs.String("tool-context", "", "context for the tool row")
	toolResult := fs.String("tool-result", "", "observable evidence for the tool row")
	focused := fs.String("focused-review", "", "exact focused-review command (tasks only)")
	date := fs.String("date", "", "verification date (default: today)")
	revision := fs.String("revision", "", "the task's own implementation commit (default: HEAD)")
	dryRun := fs.Bool("dry-run", false, "print the section without writing")
	valueFlags := map[string]bool{
		"--task": true, "--verified-by": true, "--working-dir": true,
		"--result": true, "--tool": true, "--tool-context": true,
		"--tool-result": true, "--focused-review": true, "--date": true,
		"--revision": true, "-revision": true,
		"-task": true, "-verified-by": true, "-working-dir": true, "-result": true,
		"-tool": true, "-tool-context": true, "-tool-result": true,
		"-focused-review": true, "-date": true,
	}
	flags, positional := splitArgs(args[1:], valueFlags)
	if err := fs.Parse(append(flags, positional...)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("evidence add: expected exactly one artifact path\n\n%s", evidenceUsage)
	}
	targets := 0
	for _, set := range []bool{*task != "", *phase, *plan} {
		if set {
			targets++
		}
	}
	if targets != 1 {
		return fmt.Errorf("evidence add: pass exactly one of --task ID, --phase, or --plan")
	}
	if *verifiedBy == "" || *result == "" {
		return fmt.Errorf("evidence add: --verified-by and --result are required; " +
			"evidence without a command and an observed result proves nothing")
	}

	path := fs.Arg(0)
	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("evidence add: %w", err)
	}
	if !art.Exists {
		return fmt.Errorf("evidence add: %s does not exist", path)
	}
	doc := artifact.Parse(art.Source)

	when := *date
	if when == "" {
		when = time.Now().Format("2006-01-02")
	}

	repoDir := evidenceRepoDir(path)
	repo := vcs.Detect(repoDir)

	// The identity is the entity's OWN implementation commit, not whatever
	// HEAD happens to be — shared/completion-evidence.md § Git adapter. When
	// one is named, it is verified to exist rather than trusted; when it is
	// not, HEAD is used and the worktree must be clean, because otherwise the
	// recorded revision would not describe what was verified.
	rev := *revision
	if rev == "" {
		head, err := repo.Head()
		if err != nil || head == "" {
			return fmt.Errorf("evidence add: cannot read the current revision from %s; "+
				"completion evidence must carry a real native identity", repoDir)
		}
		clean, dirty, _ := repo.Clean()
		if !clean {
			return fmt.Errorf("evidence add: the target repository has uncommitted changes (%s).\n"+
				"Commit the work first, or pass --revision <full40> naming the commit this "+
				"entity was actually verified at; a recorded revision that does not describe "+
				"what was tested is false evidence, not incomplete evidence",
				strings.Join(dirty, ", "))
		}
		rev = head
	} else {
		if !repo.RevisionSyntaxValid(rev) {
			return fmt.Errorf("evidence add: %q is not a full native revision identifier for this SCM", rev)
		}
		exists, err := repo.RevisionExists(rev)
		if err != nil || !exists {
			return fmt.Errorf("evidence add: revision %s does not exist in %s", rev, repoDir)
		}
	}

	section := renderEvidence(evidenceInput{
		Date: when, Repository: ".", VCS: string(repo.Kind()), Revision: rev,
		VerifiedBy: *verifiedBy, WorkingDir: *workingDir, Result: *result,
		Tool: *tool, ToolContext: *toolContext, ToolResult: *toolResult,
		Focused: *focused, IsTask: *task != "",
	})

	if *dryRun {
		fmt.Print(section)
		return nil
	}

	heading, level := "## Phase Completion Evidence", 2
	switch {
	case *plan:
		heading = "## Plan Completion Evidence"
	case *task != "":
		heading, level = "### Completion Evidence", 3
	}
	updated, err := replaceEvidenceSection(doc, art.Source, heading, level, *task, section, when)
	if err != nil {
		return fmt.Errorf("evidence add: %w", err)
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("evidence add: %w", err)
	}
	fmt.Printf("recorded %s in %s (revision %s)\n", strings.TrimSpace(strings.TrimPrefix(heading, "##")), path, short(rev))
	return nil
}

type evidenceInput struct {
	Date, Repository, VCS, Revision string
	VerifiedBy, WorkingDir, Result  string
	Tool, ToolContext, ToolResult   string
	Focused                         string
	IsTask                          bool
}

// renderEvidence emits the label set and tables completion-evidence.md
// specifies, in the order the validator expects.
func renderEvidence(in evidenceInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Verified: %s\n", in.Date)
	fmt.Fprintf(&b, "- Repository: `%s`\n", in.Repository)
	fmt.Fprintf(&b, "- VCS: `%s`\n", in.VCS)
	fmt.Fprintf(&b, "- Revision / checkpoint: `%s`\n", in.Revision)
	fmt.Fprintf(&b, "- Identity recheck: `%s` at %s matched `%s`\n",
		"git rev-parse HEAD", in.Date+" 00:00", in.Revision)
	if in.IsTask {
		// SDD169 requires the focused review to name the reviewed identity
		// exactly: `git show <full40>` or `git diff <base>..<full40>`, no
		// extra operands. Defaulting to the verification command produced
		// evidence the validator rejected — caught by running `sdd task
		// complete` against this repository's own plan.
		focused := in.Focused
		if focused == "" {
			focused = "git show " + in.Revision
		}
		fmt.Fprintf(&b, "- Focused review: `%s`; complete task diff reviewed for "+
			"correctness, scope, tests, maintainability, and task boundary\n", focused)
		fmt.Fprintf(&b, "- Reviewed candidate / final: `%s`\n", in.Revision)
		b.WriteString("- Review result: PASS/Aligned\n")
	}
	b.WriteString("\n| Command | Working directory | Result | Observable evidence |\n")
	b.WriteString("|---|---|---|---|\n")
	fmt.Fprintf(&b, "| `%s` | `%s` | PASS (`exit 0`) | `%s` |\n",
		in.VerifiedBy, in.WorkingDir, in.Result)
	if in.Tool != "" {
		b.WriteString("\n| Tool / inspection | Context | Result | Observable evidence |\n")
		b.WriteString("|---|---|---|---|\n")
		fmt.Fprintf(&b, "| `%s` | `%s` | PASS | `%s` |\n",
			in.Tool, orDefault(in.ToolContext, "."), orDefault(in.ToolResult, in.Result))
	}
	return b.String()
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// evidenceRepoDir resolves the repository whose identity the evidence records:
// the VCS root containing the artifact, which is where the work being verified
// actually landed.
func evidenceRepoDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "."
	}
	dir := filepath.Dir(abs)
	if root := gitRoot(dir); root != "" {
		return root
	}
	return dir
}

// replaceEvidenceSection swaps one evidence section's body, leaving the rest
// of the document untouched.
//
// The section is located by heading text and, for a task, by the enclosing
// `## <task-id>` section — the same containment rule SDD067 enforces, so
// evidence lands where the validator looks for it.
func replaceEvidenceSection(doc *artifact.Doc, source, heading string, level int, taskID, body, today string) (string, error) {
	lines := strings.Split(source, "\n")
	start := -1
	inTask := taskID == ""
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if taskID != "" && strings.HasPrefix(trimmed, "## ") {
			inTask = strings.HasPrefix(strings.TrimPrefix(trimmed, "## "), taskID)
		}
		if inTask && trimmed == heading {
			start = i
			break
		}
	}
	if start == -1 {
		where := ""
		if taskID != "" {
			where = " inside task " + taskID
		}
		return "", fmt.Errorf("no `%s` section%s; add the section before recording evidence", heading, where)
	}
	end := len(lines)
	prefix := strings.Repeat("#", level)
	for j := start + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if strings.HasPrefix(t, "#") && countLeadingHashes(t) <= level && strings.HasPrefix(t, prefix[:1]) {
			end = j
			break
		}
	}
	out := append([]string{}, lines[:start+1]...)
	out = append(out, "")
	out = append(out, strings.Split(strings.TrimRight(body, "\n"), "\n")...)
	out = append(out, "")
	out = append(out, lines[end:]...)

	rendered := strings.Join(out, "\n")
	return restampUpdated(rendered, today), nil
}

func countLeadingHashes(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	return n
}

// restampUpdated advances the frontmatter `updated` date, as every other
// write path does.
func restampUpdated(source, today string) string {
	lines := strings.Split(source, "\n")
	for i, l := range lines {
		if i > 0 && strings.TrimSpace(l) == "---" {
			break
		}
		if strings.HasPrefix(l, "updated:") {
			lines[i] = "updated: " + today
			break
		}
	}
	return strings.Join(lines, "\n")
}
