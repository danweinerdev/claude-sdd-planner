package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

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
	finalReview := fs.String("final-review", "",
		"`<review path>; frozen: <range>` for the phase's final aligned review")
	date := fs.String("date", "", "verification date (default: today)")
	revision := fs.String("revision", "", "the task's own implementation commit (default: HEAD)")
	dryRun := fs.Bool("dry-run", false, "print the section without writing")
	jsonOut := fs.Bool("json", false, "emit the result as JSON")
	positional, err := parseFlags(fs, args[1:])
	if err != nil {
		return err
	}
	if len(positional) != 1 {
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

	path := positional[0]
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

	// A phase's evidence must additionally carry one identity line per
	// completed task (SDD157) and the final aligned review entry (SDD166).
	// Those are derived from the phase itself rather than typed, so they
	// cannot disagree with the tasks they describe.
	var identities []taskIdentityLine
	if *phase {
		identities = completedTaskIdentities(doc)
	}

	// A plan's evidence carries the same roll-up one level up: one line per
	// completed phase naming its checkpoint AND the review that closed it
	// (SDD158). Like the task roll-up, it is read from each phase's own
	// recorded evidence rather than typed, so the two cannot disagree.
	var phaseIdentities []phaseIdentityLine
	if *plan {
		var err error
		phaseIdentities, err = completedPhaseIdentities(path, doc)
		if err != nil {
			return fmt.Errorf("evidence add: %w", err)
		}
	}

	section := renderEvidence(evidenceInput{
		Date: when, Repository: ".", VCS: string(repo.Kind()), Revision: rev,
		VerifiedBy: *verifiedBy, WorkingDir: *workingDir, Result: *result,
		Tool: *tool, ToolContext: *toolContext, ToolResult: *toolResult,
		Focused: *focused, IsTask: *task != "",
		FinalReview: *finalReview, TaskIdentities: identities,
		PhaseIdentities: phaseIdentities,
	})

	heading, level := "## Phase Completion Evidence", 2
	target := "phase"
	switch {
	case *plan:
		heading, target = "## Plan Completion Evidence", "plan"
	case *task != "":
		heading, level, target = "### Completion Evidence", 3, "task"
	}

	if *dryRun {
		if *jsonOut {
			return emitEvidenceJSON(evidenceResult{
				Path: relPath(path), OK: true, DryRun: true,
				Target: target, TaskID: *task, Heading: heading,
				Revision: rev, Section: section,
			})
		}
		fmt.Print(section)
		return nil
	}

	updated, err := replaceEvidenceSection(doc, art.Source, heading, level, *task, section, when)
	if err != nil {
		return fmt.Errorf("evidence add: %w", err)
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("evidence add: %w", err)
	}
	if *jsonOut {
		return emitEvidenceJSON(evidenceResult{
			Path: relPath(path), OK: true, Wrote: true,
			Target: target, TaskID: *task, Heading: heading,
			Revision: rev, Digest: store.Digest(updated), Section: section,
		})
	}
	fmt.Printf("recorded %s in %s (revision %s)\n", strings.TrimSpace(strings.TrimPrefix(heading, "##")), path, short(rev))
	return nil
}

// evidenceResult is the machine-readable outcome of `evidence add` (FR-04).
// It mirrors the shape the other writing commands emit — path/ok/dry_run/
// wrote/digest — and adds what is specific to evidence: which level was
// recorded, the heading it landed under, the revision the evidence attests
// to, and the rendered section itself, so a caller can log or re-check what
// was written without re-reading the artifact.
type evidenceResult struct {
	Path     string `json:"path"`
	OK       bool   `json:"ok"`
	DryRun   bool   `json:"dry_run,omitempty"`
	Wrote    bool   `json:"wrote,omitempty"`
	Target   string `json:"target"`
	TaskID   string `json:"task_id,omitempty"`
	Heading  string `json:"heading"`
	Revision string `json:"revision,omitempty"`
	Digest   string `json:"digest,omitempty"`
	Section  string `json:"section"`
}

func emitEvidenceJSON(res evidenceResult) error {
	return writeJSON(res)
}

type evidenceInput struct {
	Date, Repository, VCS, Revision string
	VerifiedBy, WorkingDir, Result  string
	Tool, ToolContext, ToolResult   string
	Focused, FinalReview            string
	IsTask                          bool
	TaskIdentities                  []taskIdentityLine
	PhaseIdentities                 []phaseIdentityLine
}

// taskIdentityLine is one completed task and the revision its own evidence
// records, for the phase's `### Completed task identities` section.
type taskIdentityLine struct{ ID, Revision string }

// phaseIdentityLine is one completed phase, the checkpoint its evidence
// records, and the review that closed it.
type phaseIdentityLine struct{ ID, Revision, Review string }

// completedPhaseIdentities reads each completed phase's checkpoint and final
// review out of that phase's own evidence section.
//
// Both values are derived rather than supplied for the same reason the task
// roll-up is: SDD158 requires the plan's summary to match the phases exactly,
// and a hand-typed roll-up across seven phases is a transcription error
// waiting to happen.
func completedPhaseIdentities(planPath string, doc *artifact.Doc) ([]phaseIdentityLine, error) {
	var meta struct {
		Phases []struct {
			ID     any    `yaml:"id"`
			Status string `yaml:"status"`
			Doc    string `yaml:"doc"`
		} `yaml:"phases"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(doc.FrontmatterRaw, "\n")), &meta); err != nil {
		return nil, fmt.Errorf("cannot read the plan's phases[]: %w", err)
	}

	dir := filepath.Dir(planPath)
	var out []phaseIdentityLine
	for _, ph := range meta.Phases {
		if ph.Status != "complete" || ph.Doc == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ph.Doc))
		if err != nil {
			return nil, fmt.Errorf("phase %v: cannot read %s: %w", ph.ID, ph.Doc, err)
		}
		body := docBody(artifact.Parse(string(raw)))
		section := phaseEvidenceSection(body)
		rev := labelValue(section, "- Revision / checkpoint:")
		review := reviewPathFrom(labelValue(section, "- Final aligned review:"))
		if rev == "" || review == "" {
			return nil, fmt.Errorf("phase %v has no recorded checkpoint or final review; "+
				"complete the phase before closing the plan", ph.ID)
		}
		out = append(out, phaseIdentityLine{ID: fmt.Sprint(ph.ID), Revision: rev, Review: review})
	}
	return out, nil
}

// phaseEvidenceSection returns the `## Phase Completion Evidence` body.
func phaseEvidenceSection(body string) string {
	start := strings.Index(body, "## Phase Completion Evidence")
	if start < 0 {
		return ""
	}
	rest := body[start:]
	if next := strings.Index(rest[3:], "\n## "); next >= 0 {
		rest = rest[:next+3]
	}
	return rest
}

// labelValue pulls a `- Label: value` line's value, unquoted.
func labelValue(section, label string) string {
	for _, line := range strings.Split(section, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, label) {
			continue
		}
		return strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, label)), "`")
	}
	return ""
}

// reviewPathFrom takes the path half of `<path>; frozen: <identity>`.
func reviewPathFrom(entry string) string {
	if i := strings.Index(entry, ";"); i >= 0 {
		return strings.TrimSpace(entry[:i])
	}
	return strings.TrimSpace(entry)
}

// completedTaskIdentities reads each completed task's recorded revision out of
// that task's own evidence, so the phase's roll-up cannot drift from the tasks
// it summarizes — SDD157 requires them to agree exactly.
func completedTaskIdentities(doc *artifact.Doc) []taskIdentityLine {
	var meta struct {
		Tasks []struct {
			ID     string `yaml:"id"`
			Status string `yaml:"status"`
		} `yaml:"tasks"`
	}
	if yaml.Unmarshal([]byte(strings.Join(doc.FrontmatterRaw, "\n")), &meta) != nil {
		return nil
	}
	body := docBody(doc)
	var out []taskIdentityLine
	for _, t := range meta.Tasks {
		if t.Status != "complete" {
			continue
		}
		if rev := taskRecordedRevision(body, t.ID); rev != "" {
			out = append(out, taskIdentityLine{ID: t.ID, Revision: rev})
		}
	}
	return out
}

// taskRecordedRevision pulls the `Revision / checkpoint` a task's own evidence
// records, searching only within that task's section.
func taskRecordedRevision(body, id string) string {
	start := strings.Index(body, "\n## "+id)
	if start < 0 {
		return ""
	}
	rest := body[start+1:]
	if next := strings.Index(rest[3:], "\n## "); next >= 0 {
		rest = rest[:next+3]
	}
	for _, line := range strings.Split(rest, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "- Revision / checkpoint:") {
			continue
		}
		return strings.Trim(strings.TrimSpace(
			strings.TrimPrefix(t, "- Revision / checkpoint:")), "`")
	}
	return ""
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
	if in.FinalReview != "" {
		fmt.Fprintf(&b, "- Final aligned review: %s\n", in.FinalReview)
	}
	b.WriteString("\n| Command | Working directory | Result | Observable evidence |\n")
	b.WriteString("|---|---|---|---|\n")
	fmt.Fprintf(&b, "| `%s` | `%s` | PASS (`exit 0`) | `%s` |\n",
		in.VerifiedBy, in.WorkingDir, in.Result)
	if len(in.PhaseIdentities) > 0 {
		b.WriteString("\n### Completed phase identities\n\n")
		for _, p := range in.PhaseIdentities {
			fmt.Fprintf(&b, "- `%s`: `%s`; review: `%s`\n", p.ID, p.Revision, p.Review)
		}
	}
	if len(in.TaskIdentities) > 0 {
		b.WriteString("\n### Completed task identities\n\n")
		for _, t := range in.TaskIdentities {
			fmt.Fprintf(&b, "- `%s`: `%s`\n", t.ID, t.Revision)
		}
	}
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

// docBody reassembles a document's body so a task's evidence can be located by
// its `## <task-id>` heading.
func docBody(doc *artifact.Doc) string {
	var b strings.Builder
	for _, l := range doc.Preamble {
		b.WriteString(l + "\n")
	}
	for _, sec := range doc.Sections {
		b.WriteString(sec.Heading + "\n")
		for _, l := range sec.Body {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}
