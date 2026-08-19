package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// Family (i): Validator._evidence/_task_review_evidence/
// _valid_git_task_review_identity/_verify_clean_git_identity/
// _verify_evidence_committed/_verify_git_evidence_committed — the retrospective
// completion-evidence shape every `complete` task/phase/plan must carry
// (shared/completion-evidence.md), gated through the target repository's
// vcs.Repo (Prerequisite A) rather than exec'ing git directly.
//
// _evidence fires from two call sites Python has and this file reproduces as
// one shared scan (evidenceTargets): the plan/phase's own `## Plan/Phase
// Completion Evidence` section (_headings), and every task's `### Completion
// Evidence` section (_phase). Each registered SDD07x/SDD169/SDD172 rule is a
// thin CheckRoot that runs that one shared scan and keeps only its own code,
// so the rules agree on exactly what "the evidence family found" means.

const pendingMarker = "Pending — not complete."

var evidenceLabelRe = regexp.MustCompile(`(?m)^\s*-\s+(.+?):\s*(.*?)\s*$`)

// evidenceValues returns visible, non-fenced values for an exact evidence
// label, mirroring sdd_validate.py's evidence_values().
func evidenceValues(body, label string) []string {
	pattern := regexp.MustCompile(`^\s*-\s+` + regexp.QuoteMeta(label) + `:\s*(.*?)\s*$`)
	var out []string
	for _, l := range markdownLines(body) {
		if m := pattern.FindStringSubmatch(strings.TrimRight(l.Visible, "\r\n")); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func evidenceValue(body, label string) (string, bool) {
	values := evidenceValues(body, label)
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// markdownScalar strips a single layer of surrounding backticks, mirroring
// sdd_validate.py's markdown_scalar(); "" for an absent value.
func markdownScalar(value string, ok bool) string {
	if !ok {
		return ""
	}
	result := strings.TrimSpace(value)
	if len(result) >= 2 && result[0] == '`' && result[len(result)-1] == '`' {
		result = strings.TrimSpace(result[1 : len(result)-1])
	}
	return result
}

var evidenceHex40Re = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// p4RevisionRe is Perforce's durable-identity grammar: a bare submitted
// changelist number. Mirrors internal/vcs's p4Changelist — a shelved or
// pending changelist is mutable (a shelf can be re-shelved in place), so only
// a submitted numbered changelist identifies a point in history (G-2).
var p4RevisionRe = regexp.MustCompile(`^[0-9]+$`)

var evidenceExitZeroRe = regexp.MustCompile(`(?i)\bexit\s+0\b`)

// evidenceExpectedExitRe accepts a deliberate expected-failure run recorded
// honestly: `PASS (exit 2, expected)` names the exact nonzero status the
// experiment was designed to produce. Refusing anything but `exit 0` forced
// hypothesis-refutation runs (a normal part of spikes) out of the command
// table entirely (G-6). The expected status must be named — a bare "expected
// failure" would drop the exact-exit-status invariant every other row keeps.
var evidenceExpectedExitRe = regexp.MustCompile(`(?i)\bexit\s+[1-9]\d*\s*[,(]?\s*expected\b`)

// evidenceRows ports evidence_rows(): the four-column Command/Tool result
// table rows an evidence section must carry, requiring every cell populated
// and none carrying an unfilled `<placeholder>`.
func evidenceRows(body string) []struct {
	Kind string
	Row  [4]string
} {
	var rows []struct {
		Kind string
		Row  [4]string
	}
	active := ""
	for _, line := range strings.Split(visibleMarkdown(body), "\n") {
		cells := splitTableCells(line)
		switch {
		case cellsEqual(cells, "Command", "Working directory", "Result", "Observable evidence"):
			active = "command"
			continue
		case cellsEqual(cells, "Tool / inspection", "Context", "Result", "Observable evidence"):
			active = "tool"
			continue
		}
		if active == "" {
			continue
		}
		if len(cells) == 4 && allSeparatorCells(cells) {
			continue
		}
		if len(cells) != 4 || !strings.HasPrefix(strings.TrimLeft(line, " \t"), "|") {
			active = ""
			continue
		}
		var row [4]string
		copy(row[:], cells)
		if row[0] != "" && row[1] != "" && row[2] != "" && row[3] != "" && !anyPlaceholder(row) {
			rows = append(rows, struct {
				Kind string
				Row  [4]string
			}{active, row})
		}
	}
	return rows
}

func splitTableCells(line string) []string {
	trimmed := strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(trimmed, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = markdownScalar(strings.TrimSpace(p), true)
	}
	return out
}

func cellsEqual(cells []string, want ...string) bool {
	if len(cells) != len(want) {
		return false
	}
	for i, w := range want {
		if cells[i] != w {
			return false
		}
	}
	return true
}

var separatorCellRe = regexp.MustCompile(`^:?-{3,}:?$`)

func allSeparatorCells(cells []string) bool {
	for _, c := range cells {
		if !separatorCellRe.MatchString(c) {
			return false
		}
	}
	return true
}

func anyPlaceholder(row [4]string) bool {
	for _, v := range row {
		if strings.Contains(v, "<") && strings.Contains(v, ">") {
			return true
		}
	}
	return false
}

// stripEvidenceRows reduces evidence tables to the text SDD073 still owns:
// non-passing rows are dropped entirely (SDD072 already validates the Result
// cell exactly), and a passing row keeps only its Observable evidence cell.
func stripEvidenceRows(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		stripped := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(stripped, "|") {
			kept = append(kept, line)
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(stripped), "|"), "|")
		trimmed := make([]string, len(cells))
		for i, c := range cells {
			trimmed[i] = strings.TrimSpace(c)
		}
		if len(trimmed) == 4 && strings.HasPrefix(trimmed[2], "PASS") {
			kept = append(kept, trimmed[3])
		}
	}
	return strings.Join(kept, "\n")
}

// findFailingEvidence ports the FAILING_EVIDENCE regex without lookaround
// (Go's RE2 engine has none): a case-sensitive FAIL/FAILED token not adjacent
// to a word character or hyphen (so "fail-safe"/"FAIL-OPEN" don't match), or a
// lowercase `exit <nonzero>` word. Returns the leftmost qualifying match,
// mirroring re.search's leftmost-first semantics.
func findFailingEvidence(s string) (string, bool) {
	// An annotated expected failure (`exit 2, expected`) is a recorded
	// hypothesis-refutation result, not stray failing output; neutralize the
	// annotation before scanning so honest narration of a deliberate failure
	// does not read as SDD073 evidence (G-6).
	s = evidenceExpectedExitRe.ReplaceAllString(s, "[expected nonzero status]")
	bestStart := -1
	bestText := ""
	for i := 0; i < len(s); i++ {
		for _, tok := range []string{"FAILED", "FAIL"} {
			l := len(tok)
			if i+l > len(s) || s[i:i+l] != tok {
				continue
			}
			prevOK := i == 0 || !isWordOrHyphenByte(s[i-1])
			nextOK := i+l == len(s) || !isWordOrHyphenByte(s[i+l])
			if prevOK && nextOK {
				bestStart, bestText = i, tok
			}
			break
		}
		if bestStart == i {
			break // leftmost fail-family match found; nothing earlier can beat it
		}
	}
	if loc := exitNonzeroRe.FindStringIndex(s); loc != nil {
		if bestStart == -1 || loc[0] < bestStart {
			bestStart, bestText = loc[0], s[loc[0]:loc[1]]
		}
	}
	return bestText, bestStart != -1
}

var exitNonzeroRe = regexp.MustCompile(`\bexit\s+[1-9]\d*\b`)

func isWordOrHyphenByte(b byte) bool {
	return b == '-' || b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

var recheckMatchWordRe = regexp.MustCompile(`(?i)\bmatch(?:ed|es|ing)?\b`)
var recheckTimestampRe = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ][0-2]\d:[0-5]\d`)

var taskCompletionEvidenceNameRe = regexp.MustCompile(`^Task\s+(\S+)\s+Completion Evidence$`)

// validFocusedReviewSyntax ports valid_focused_review_syntax(): a quoted exact
// review command/tool, distinct from the generic placeholders "review"/"code
// review"/"diff", followed by the fixed complete-task-diff claim.
var focusedReviewRe = regexp.MustCompile("^`([^`;\n]+?)`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary$")

func validFocusedReviewSyntax(value string) bool {
	if value == "" {
		return false
	}
	m := focusedReviewRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return false
	}
	tool := strings.TrimSpace(m[1])
	if tool == "" {
		return false
	}
	switch strings.ToLower(tool) {
	case "review", "code review", "diff":
		return false
	}
	return true
}

// detectedSCM names the lifecycle transport Kind() reports for dir, in the
// three labels sdd_validate.py's detected_scm() ever returns. Kind() is the
// seam's own diagnostic label (internal/vcs/vcs.go: "for diagnostics and for
// rules that are legitimately git-specific") — these evidence rules are
// exactly that case, since Perforce has no adapter for the identity
// operations below and the message must name which SCM was found.
func detectedSCM(dir string) string {
	switch vcs.Detect(dir).Kind() {
	case vcs.Git, vcs.GitWorktree:
		return "git"
	case vcs.Perforce:
		return "perforce"
	default:
		return "none"
	}
}

// gitCapable reports whether repo is a git working tree (plain or linked
// worktree) as opposed to bare, Perforce, or absent — the same distinction
// detectedSCM draws, kept separate because several call sites already hold a
// vcs.Repo and don't need to re-detect.
func gitCapable(repo vcs.Repo) bool {
	switch repo.Kind() {
	case vcs.Git, vcs.GitWorktree:
		return true
	default:
		return false
	}
}

// expandHome replaces a leading "~" the way Python's Path.expanduser() does.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// evidenceTarget is one `_evidence` call site: a plan/phase's own completion-
// evidence section, or one task's.
type evidenceTarget struct {
	Artifact *Artifact
	Status   string
	Name     string
	Line     int
	Body     string
}

// evidenceTargets reproduces both Python call sites for _evidence: _headings'
// single call for a plan/phase's own `## Plan/Phase Completion Evidence`
// section, and _phase's one call per task's `### Completion Evidence`.
func evidenceTargets(r *Root) []evidenceTarget {
	var out []evidenceTarget
	for _, a := range r.Artifacts {
		if a.Meta == nil {
			continue
		}
		switch a.Kind() {
		case "plan", "phase":
			heading := "Plan Completion Evidence"
			if a.Kind() == "phase" {
				heading = "Phase Completion Evidence"
			}
			secs := sections(a, 2)
			if headingBodiesCount(a.Body, 2, heading) == 1 {
				if info, ok := secs[heading]; ok {
					out = append(out, evidenceTarget{Artifact: a, Status: a.Status(), Name: heading, Line: info.Line, Body: info.Body})
				}
			}
		}
		if a.Kind() != "phase" {
			continue
		}
		tasks, ok := a.Meta["tasks"].([]any)
		if !ok {
			continue
		}
		secs := sections(a, 2)
		for _, t := range tasks {
			m := planEntry(t)
			if m == nil {
				continue
			}
			id := metaStr(m, "id")
			heading := taskHeadingFor(secs, id)
			if heading == "" {
				continue
			}
			info := secs[heading]
			blocks := headingBodies(info.Body, 3, "Completion Evidence")
			if len(blocks) != 1 {
				continue
			}
			out = append(out, evidenceTarget{
				Artifact: a, Status: metaStr(m, "status"),
				Name: "Task " + id + " Completion Evidence", Line: info.Line, Body: blocks[0],
			})
		}
	}
	return out
}

func headingBodiesCount(body string, level int, label string) int {
	return len(headingBodies(body, level, label))
}

// evidenceCheckRoot returns a CheckRoot that runs the shared _evidence scan
// and keeps only diagnostics of one code, so every SDD07x/SDD169/SDD172 rule
// agrees on what the family found without recomputing its own copy of the
// port.
func evidenceCheckRoot(code string) func(*Root, func(Diagnostic)) {
	return func(r *Root, emit func(Diagnostic)) {
		for _, target := range evidenceTargets(r) {
			runEvidence(r, target, func(d Diagnostic) {
				if d.Code == code {
					emit(d)
				}
			})
		}
	}
}

// runEvidence ports Validator._evidence.
func runEvidence(r *Root, t evidenceTarget, emit func(Diagnostic)) {
	a, name, line, body := t.Artifact, t.Name, t.Line, t.Body
	visibleBody := visibleMarkdown(body)
	pending := strings.Contains(visibleBody, pendingMarker)
	if t.Status == "complete" && pending {
		emit(Diagnostic{Code: "SDD070", Severity: Error, Path: a.Rel, Line: line,
			Message: "Complete `" + name + "` is pending.", Correction: "Replace the marker with retrospective evidence."})
		return
	}
	if pending {
		return
	}
	for _, label := range []string{"Verified", "Repository", "VCS", "Revision / checkpoint", "Identity recheck"} {
		values := evidenceValues(body, label)
		v, ok := "", false
		if len(values) == 1 {
			v, ok = values[0], true
		}
		if len(values) != 1 || markdownScalar(v, ok) == "" {
			emit(Diagnostic{Code: "SDD071", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` must contain exactly one populated visible `" + label + "` label.",
				Correction: "Keep one populated `" + label + "` evidence line outside comments and fenced blocks."})
		}
	}
	verified := markdownScalar(evidenceValue(body, "Verified"))
	if verified != "" && !dateRe.MatchString(verified) {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` has invalid verification date `" + verified + "`.", Correction: "Use YYYY-MM-DD."})
	}
	vcsVal := markdownScalar(evidenceValue(body, "VCS"))
	if vcsVal != "" && vcsVal != "git" && vcsVal != "git-worktree" && vcsVal != "perforce" && vcsVal != "none" {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` has invalid VCS `" + vcsVal + "`.", Correction: "Use git, git-worktree, perforce, or none."})
	}
	revision := markdownScalar(evidenceValue(body, "Revision / checkpoint"))
	isGitVCS := vcsVal == "git" || vcsVal == "git-worktree"
	if taskCompletionEvidenceNameRe.MatchString(name) {
		taskReviewEvidence(r, a, name, body, revision, vcsVal, line, t.Status == "complete", emit)
	}
	if isGitVCS && revision != "" && !evidenceHex40Re.MatchString(revision) {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` has invalid Git revision/checkpoint `" + revision + "`.", Correction: "Record exactly one clean full native Git revision/checkpoint."})
	}
	isP4VCS := vcsVal == "perforce"
	if isP4VCS && revision != "" && !p4RevisionRe.MatchString(revision) {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` has invalid Perforce revision/checkpoint `" + revision + "`.",
			Correction: "Record exactly one submitted changelist number; a shelved or pending changelist is mutable and is not a durable native identity."})
	}
	if vcsVal == "none" && revision != "" && revision != "none" {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` with VCS `none` has revision/base `" + revision + "`.", Correction: "Use `none`."})
	}
	repository := markdownScalar(evidenceValue(body, "Repository"))
	expectedRepository := r.RepoForArtifact(a.Rel)
	if repository != "" {
		recordedRepository := cleanAbs(expandHome(repository))
		if recordedRepository != cleanAbs(expectedRepository) {
			emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` repository `" + recordedRepository + "` does not match target `" + cleanAbs(expectedRepository) + "`.",
				Correction: "Record the exact resolved target repository root."})
		}
	}
	removedLabels := []string{"Fallback reason", "Evidence exclusions", "Governing intent", "Ignored inputs", "Directory inputs", "Content snapshot"}
	removedFound := false
	for _, label := range removedLabels {
		if _, ok := evidenceValue(body, label); ok {
			removedFound = true
			break
		}
	}
	if removedFound {
		emit(Diagnostic{Code: "SDD074", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` uses removed synthetic identity fields.",
			Correction: "Remove fallback, snapshot, exclusion, and inventory labels; record the native SCM checkpoint only."})
	}
	if _, ok := evidenceValue(body, "Revision / base"); ok {
		emit(Diagnostic{Code: "SDD074", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` uses removed `Revision / base` identity evidence.",
			Correction: "Use only `Revision / checkpoint` with the native SCM identity."})
	}
	switch {
	case isGitVCS && revision != "":
		verifyCleanGitIdentity(r, a, revision, name, line, true, emit)
	case isP4VCS && revision != "" && p4RevisionRe.MatchString(revision):
		// The Perforce identity adapter (G-2): a submitted changelist number
		// is the durable native identity. Syntax was already checked above;
		// existence is verified against the target workspace.
		verifyCleanP4Identity(r, a, revision, name, line, emit)
	case t.Status == "complete":
		label := vcsVal
		if label == "" {
			label = "missing"
		}
		emit(Diagnostic{Code: "SDD172", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` cannot complete with VCS `" + label + "` because no validated native identity adapter is available.",
			Correction: "Keep the entity non-complete until a validated native SCM and lifecycle adapter are available."})
	}
	if t.Status == "complete" {
		verifyEvidenceCommitted(r, a, name, body, line, emit)
	}
	rows := evidenceRows(body)
	if len(rows) == 0 {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` has no conforming command or tool evidence row.", Correction: "Add a four-column command or tool row with PASS and specific observable evidence."})
	} else {
		for _, row := range rows {
			if !strings.HasPrefix(row.Row[2], "PASS") {
				emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
					Message: "`" + name + "` contains non-passing result `" + row.Row[2] + "`.", Correction: "Every required command and inspection row must record PASS."})
			}
			if row.Kind == "command" && !evidenceExitZeroRe.MatchString(row.Row[2]) && !evidenceExpectedExitRe.MatchString(row.Row[2]) {
				emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
					Message: "`" + name + "` command row lacks explicit `exit 0`.", Correction: "Record PASS with the command exit status: `PASS (exit 0)`, or `PASS (exit N, expected)` for a deliberate expected-failure run."})
			}
		}
	}
	if stray, ok := findFailingEvidence(stripEvidenceRows(visibleBody)); ok {
		emit(Diagnostic{Code: "SDD073", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` contains failing evidence `" + stray + "` outside a result row.",
			Correction: "Return it to a non-complete status until final checks pass, or move passing narration out of failure-shaped wording."})
	}
	recheck := markdownScalar(evidenceValue(body, "Identity recheck"))
	if recheck != "" && (!recheckMatchWordRe.MatchString(recheck) || !recheckTimestampRe.MatchString(recheck)) {
		emit(Diagnostic{Code: "SDD075", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` recheck lacks a timestamped matching result.",
			Correction: "Record the exact tool, ISO timestamp, and matching identity."})
	}
	if isGitVCS && revision != "" && !strings.Contains(strings.ToLower(recheck), strings.ToLower(revision)) {
		emit(Diagnostic{Code: "SDD075", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` recheck does not name tested Git revision/checkpoint `" + revision + "`.",
			Correction: "Record the exact tested Git revision/checkpoint in the identity-recheck procedure and result."})
	}
	if isP4VCS && revision != "" && !strings.Contains(recheck, revision) {
		emit(Diagnostic{Code: "SDD075", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` recheck does not name tested Perforce changelist `" + revision + "`.",
			Correction: "Record the exact tested submitted changelist number in the identity-recheck procedure and result."})
	}
}

// resolveSymlinks mirrors Python's Path.resolve() for the ".relative_to()"
// comparisons the git lifecycle checks make: t.TempDir() (and some real
// worktree roots) can traverse a symlink, while `git rev-parse
// --show-toplevel` always reports the fully resolved path, so the two must
// be resolved the same way before comparing prefixes. Falls back to the
// original path when it doesn't exist or can't be resolved.
func resolveSymlinks(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

func cleanAbs(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(p)
}

// verifyCleanGitIdentity ports Validator._verify_clean_git_identity.
func verifyCleanGitIdentity(r *Root, a *Artifact, revision, name string, line int, compareCurrent bool, emit func(Diagnostic)) {
	repository := r.RepoForArtifact(a.Rel)
	repo := vcs.Detect(repository)
	if !gitCapable(repo) {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` records Git but `" + repository + "` is not a Git worktree.", Correction: "Correct the repository/VCS evidence."})
		return
	}
	if ok, _ := repo.RevisionExists(revision); !ok {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` Git revision/checkpoint `" + revision + "` is not a commit in `" + repository + "`.", Correction: "Record an existing full native Git revision/checkpoint, not a tag or another Git object."})
		return
	}
	if !compareCurrent {
		return
	}
	if ok, _ := repo.IsAncestor(revision, "HEAD"); !ok {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` Git revision/checkpoint `" + revision + "` is not an ancestor of current HEAD.", Correction: "Check out a descendant containing the completed entity or use historical identity mode for an archival audit."})
	}
}

// verifyCleanP4Identity is the Perforce counterpart of
// verifyCleanGitIdentity (G-2): the target repository must be a Perforce
// client workspace and the recorded changelist must be a submitted changelist
// the server knows. Perforce has no HEAD and no commit DAG, so there is no
// ancestor-of-current-state check — changelists are server-global and a
// submitted number cannot be rewritten, which is the durability the git
// ancestry check exists to establish.
func verifyCleanP4Identity(r *Root, a *Artifact, revision, name string, line int, emit func(Diagnostic)) {
	repository := r.RepoForArtifact(a.Rel)
	repo := vcs.Detect(repository)
	if repo.Kind() != vcs.Perforce {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` records Perforce but `" + repository + "` is not a Perforce client workspace.", Correction: "Correct the repository/VCS evidence."})
		return
	}
	if ok, _ := repo.RevisionExists(revision); !ok {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` Perforce revision/checkpoint `" + revision + "` is not a submitted changelist known to `" + repository + "`.", Correction: "Record the exact submitted changelist number; submit the pending or shelved work first."})
	}
}

// verifyEvidenceCommitted ports Validator._verify_evidence_committed, which
// dispatches by the PLANNING ROOT's own SCM (not the artifact's target
// repository — the question is whether the lifecycle bookkeeping itself was
// committed, which happens in the planning root).
func verifyEvidenceCommitted(r *Root, a *Artifact, name, body string, line int, emit func(Diagnostic)) {
	switch scm := detectedSCM(r.Dir); scm {
	case "git":
		verifyGitEvidenceCommitted(r, a, name, body, line, emit)
	case "perforce":
		verifyP4EvidenceCommitted(r, a, name, body, line, emit)
	default:
		emit(Diagnostic{Code: "SDD171", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` is complete but no validated durable lifecycle adapter is available for planning-root SCM `" + scm + "`.",
			Correction: "Keep the entity non-complete until a validated durable lifecycle adapter is available."})
	}
}

// verifyGitEvidenceCommitted ports Validator._verify_git_evidence_committed.
func verifyGitEvidenceCommitted(r *Root, a *Artifact, name, body string, line int, emit func(Diagnostic)) {
	repo := vcs.Detect(r.Dir)
	if !gitCapable(repo) {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` is complete but the Git planning root is not a worktree.", Correction: "Use a Git worktree and commit the lifecycle/evidence artifact before finalizing completion."})
		return
	}
	relative, err := filepath.Rel(resolveSymlinks(repo.Root()), resolveSymlinks(a.AbsPath))
	if err != nil || strings.HasPrefix(relative, "..") {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` planning artifact cannot be resolved inside its Git worktree.", Correction: "Commit the lifecycle/evidence artifact before finalizing completion."})
		return
	}
	relative = filepath.ToSlash(relative)
	tracked, err := repo.FileAt("HEAD", relative)
	if err != nil {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` completion evidence is not committed at HEAD.", Correction: "Create the separate scoped lifecycle/evidence commit before finalizing completion."})
		return
	}
	committed := ParseArtifactBytes(tracked, a.Rel)
	if committed.Meta == nil {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` committed planning artifact is malformed.", Correction: "Commit a valid populated lifecycle artifact before finalizing completion."})
		return
	}
	planCommitted := func(planName string) *Artifact {
		planPath := filepath.Join(r.Dir, "Plans", planName, "README.md")
		planRepository := gitRootFS(planPath)
		if planRepository == "" || planRepository != repo.Root() {
			return nil
		}
		planRelative, relErr := filepath.Rel(planRepository, planPath)
		if relErr != nil {
			return nil
		}
		planRepo := vcs.Detect(planRepository)
		planAtHead, ferr := planRepo.FileAt("HEAD", filepath.ToSlash(planRelative))
		if ferr != nil {
			return nil
		}
		return ParseArtifactBytes(planAtHead, filepath.ToSlash(planRelative))
	}
	verifyCommittedLifecycle(a, name, body, line, committed, planCommitted, "committed at HEAD", emit)
}

// verifyP4EvidenceCommitted is the Perforce durable-lifecycle adapter (G-2):
// the same question as the git path — is the lifecycle bookkeeping itself
// submitted? — answered against the client's have-revision of the planning
// artifact. `#have` is what this workspace's state is actually based on: a
// submit from this workspace advances it, while an unsubmitted edit (or a
// file never added) makes the depot copy differ from disk, which is exactly
// the not-yet-durable condition the check exists to catch.
func verifyP4EvidenceCommitted(r *Root, a *Artifact, name, body string, line int, emit func(Diagnostic)) {
	repo := vcs.Detect(r.Dir)
	if repo.Kind() != vcs.Perforce {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` is complete but the planning root is not a Perforce client workspace.", Correction: "Submit the lifecycle/evidence artifact from a Perforce client workspace before finalizing completion."})
		return
	}
	relative, err := filepath.Rel(resolveSymlinks(repo.Root()), resolveSymlinks(a.AbsPath))
	if err != nil || strings.HasPrefix(relative, "..") {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` planning artifact cannot be resolved inside its Perforce client workspace.", Correction: "Submit the lifecycle/evidence artifact before finalizing completion."})
		return
	}
	relative = filepath.ToSlash(relative)
	tracked, err := repo.FileAt("have", relative)
	if err != nil {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` completion evidence is not submitted to the depot.", Correction: "Submit the separate scoped lifecycle/evidence changelist before finalizing completion."})
		return
	}
	committed := ParseArtifactBytes(tracked, a.Rel)
	if committed.Meta == nil {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message: "`" + name + "` submitted planning artifact is malformed.", Correction: "Submit a valid populated lifecycle artifact before finalizing completion."})
		return
	}
	planCommitted := func(planName string) *Artifact {
		planPath := filepath.Join(r.Dir, "Plans", planName, "README.md")
		planRelative, relErr := filepath.Rel(resolveSymlinks(repo.Root()), resolveSymlinks(planPath))
		if relErr != nil || strings.HasPrefix(planRelative, "..") {
			return nil
		}
		planAtHave, ferr := repo.FileAt("have", filepath.ToSlash(planRelative))
		if ferr != nil {
			return nil
		}
		return ParseArtifactBytes(planAtHave, filepath.ToSlash(planRelative))
	}
	verifyCommittedLifecycle(a, name, body, line, committed, planCommitted, "submitted to the depot", emit)
}

// verifyCommittedLifecycle is the SCM-independent half of the durable
// lifecycle check: given the committed/submitted copy of the planning
// artifact, verify the lifecycle status flip, checked criteria/subtasks, and
// evidence body all landed. commitDesc names the SCM's durability state in
// diagnostics ("committed at HEAD", "submitted to the depot") — transition
// verbs recognize both spellings as commit-pending rather than refusals.
func verifyCommittedLifecycle(a *Artifact, name, body string, line int, committed *Artifact, planCommitted func(planName string) *Artifact, commitDesc string, emit func(Diagnostic)) {
	var committedBody string
	haveBody := false
	lifecycleComplete := false
	if m := taskCompletionEvidenceNameRe.FindStringSubmatch(name); m != nil {
		taskID := m[1]
		if tasks, ok := committed.Meta["tasks"].([]any); ok {
			for _, t := range tasks {
				tm := planEntry(t)
				if tm != nil && metaStr(tm, "id") == taskID && metaStr(tm, "status") == "complete" {
					lifecycleComplete = true
					break
				}
			}
		}
		secs := sections(committed, 2)
		heading := taskHeadingFor(secs, taskID)
		if heading != "" {
			info := secs[heading]
			blocks := headingBodies(info.Body, 3, "Completion Evidence")
			if len(blocks) == 1 {
				committedBody, haveBody = blocks[0], true
			}
			subtasks := headingBodies(info.Body, 3, "Subtasks")
			lifecycleComplete = lifecycleComplete && len(subtasks) == 1 && !hasUncheckedCheckbox(subtasks[0])
		}
	} else if name == "Phase Completion Evidence" {
		lifecycleComplete = committed.Status() == "complete"
		blocks := headingBodies(committed.Body, 2, name)
		if len(blocks) > 0 {
			committedBody, haveBody = blocks[0], true
		}
		criteria := headingBodies(committed.Body, 2, "Acceptance Criteria")
		lifecycleComplete = lifecycleComplete && len(criteria) > 0 && !hasUncheckedCheckbox(criteria[0])
		planName := planNameFor(a)
		planComplete := false
		if planName != "" {
			if planArtifact := planCommitted(planName); planArtifact != nil {
				if phases, ok := planArtifact.Meta["phases"].([]any); ok {
					for _, p := range phases {
						pm := planEntry(p)
						if pm != nil && metaStr(pm, "id") == metaStr(a.Meta, "phase") && metaStr(pm, "status") == "complete" {
							planComplete = true
							break
						}
					}
				}
			}
		}
		lifecycleComplete = lifecycleComplete && planComplete
	} else if name == "Plan Completion Evidence" {
		lifecycleComplete = committed.Status() == "complete"
		blocks := headingBodies(committed.Body, 2, name)
		if len(blocks) > 0 {
			committedBody, haveBody = blocks[0], true
		}
	}
	if !lifecycleComplete || !haveBody {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` lifecycle completion is not " + commitDesc + ".",
			Correction: "Commit the complete status, checked criteria/subtasks, and evidence in the scoped lifecycle commit."})
		return
	}
	if strings.TrimSpace(noComments(body)) != strings.TrimSpace(noComments(committedBody)) {
		emit(Diagnostic{Code: "SDD072", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` completion evidence differs from its committed section.",
			Correction: "Commit the populated evidence and lifecycle status in a scoped bookkeeping commit."})
	}
}

// planNameFor ports Validator._plan_name: a phase names its plan directly, or
// the artifact's own path lives under Plans/<name>/, or — for an artifact that
// reviews something — the reviewed path does.
//
// The third branch matters for reviews, whose own path is under Retro/ but
// which belong to the plan they review.
func planNameFor(a *Artifact) string {
	if a.Kind() == "phase" {
		if plan, ok := a.Meta["plan"].(string); ok && plan != "" {
			return plan
		}
	}
	parts := strings.Split(a.Rel, "/")
	if len(parts) >= 2 && parts[0] == "Plans" {
		return parts[1]
	}
	if reviewOf, ok := a.Meta["review_of"].(string); ok {
		reviewParts := strings.Split(reviewOf, "/")
		if len(reviewParts) >= 2 && reviewParts[0] == "Plans" {
			return reviewParts[1]
		}
	}
	return ""
}

// gitRootFS walks up from a file path looking for a `.git` entry, mirroring
// sdd_validate.py's git_root() used against a planning artifact's path.
func gitRootFS(path string) string {
	current := filepath.Dir(path)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func oneOf(values []string) (string, bool) {
	if len(values) == 1 {
		return values[0], true
	}
	return "", false
}

// taskReviewEvidence ports Validator._task_review_evidence: a complete task
// needs a durable, auditable focused review distinct from the plain identity
// evidence above.
//
// The format layer (label completeness, focused-review syntax, PASS/Aligned
// result, string-equality identity shape) runs on any status once the author
// has started writing the review labels — deferring every check to the
// completion transition meant evidence validated clean while in-progress and
// then produced rounds of format findings the moment the status flipped
// (G-5). The identity layer (git object existence, parentage, ancestry) stays
// complete-only: an in-progress task legitimately has no final commit yet.
func taskReviewEvidence(r *Root, a *Artifact, name, body, revision, vcsVal string, line int, complete bool, emit func(Diagnostic)) {
	focusedValues := evidenceValues(body, "Focused review")
	reviewedValues := evidenceValues(body, "Reviewed candidate / final")
	resultValues := evidenceValues(body, "Review result")
	focusedRaw, haveFocusedRaw := oneOf(focusedValues)
	focused := markdownScalar(focusedRaw, haveFocusedRaw)
	reviewed := markdownScalar(oneOf(reviewedValues))
	result := markdownScalar(oneOf(resultValues))

	// Nothing drafted yet on a non-complete task: presence is enforced by
	// the completion transition, not while the work is still open.
	if !complete && len(focusedValues) == 0 && len(reviewedValues) == 0 && len(resultValues) == 0 {
		return
	}

	var missing []string
	if len(focusedValues) != 1 || focused == "" {
		missing = append(missing, "Focused review")
	}
	if len(reviewedValues) != 1 || reviewed == "" {
		missing = append(missing, "Reviewed candidate / final")
	}
	if len(resultValues) != 1 || result == "" {
		missing = append(missing, "Review result")
	}
	if len(missing) > 0 {
		emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` must contain exactly one populated visible auditable task-review label: " + strings.Join(missing, ", ") + ".",
			Correction: "Keep one populated visible Focused review, Reviewed candidate / final, and Review result label."})
		return
	}
	if !validFocusedReviewSyntax(focusedRaw) {
		emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` focused review must contain an exact nonempty command/tool followed by the complete-task-diff statement.",
			Correction: "Use `Focused review: `<exact command/tool>`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary`."})
	}
	isGitVCS := vcsVal == "git" || vcsVal == "git-worktree"
	switch {
	case complete && isGitVCS && revision != "" && !strings.HasSuffix(revision, "-dirty"):
		validGitTaskReviewIdentity(r, a, name, focused, reviewed, revision, line, emit)
	case complete, !isGitVCS && revision != "":
		// Complete non-git (or empty-revision) identity equality — and the
		// same pure string comparison as early feedback on a non-complete
		// non-git task once both labels are written.
		if reviewed != revision {
			emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` reviewed candidate/final `" + reviewed + "` does not exactly equal native `Revision / checkpoint` `" + revision + "`.",
				Correction: "For this SCM, record the exact native revision/checkpoint reviewed; no deterministic alternate review-identity adapter is available."})
		}
	}
	if result != "PASS/Aligned" {
		emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` review result `" + result + "` is not `PASS/Aligned`.",
			Correction: "Record `- Review result: PASS/Aligned` only after the focused review passes."})
	}
}

var gitDiffRangeRe = regexp.MustCompile(`^diff: ([0-9a-fA-F]{40})\.\.([0-9a-fA-F]{40})$`)

// validGitTaskReviewIdentity ports Validator._valid_git_task_review_identity:
// the clean-Git task-review identity adapter, validated against the target
// repository via the vcs.Repo seam rather than exec'd git.
func validGitTaskReviewIdentity(r *Root, a *Artifact, name, focused, reviewed, revision string, line int, emit func(Diagnostic)) bool {
	repository := r.RepoForArtifact(a.Rel)
	repo := vcs.Detect(repository)
	var identities []string
	var expectedReviewIdentity string
	if reviewed == revision {
		identities = []string{reviewed}
		expectedReviewIdentity = revision
	} else {
		m := gitDiffRangeRe.FindStringSubmatch(reviewed)
		if m == nil {
			emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` reviewed candidate/final must be the exact clean Git task commit or `diff: <full40>..<full40>`.",
				Correction: "Record the full task commit, or a full-commit diff range ending at `Revision / checkpoint`."})
			return false
		}
		identities = []string{m[1], m[2]}
		expectedReviewIdentity = m[1] + ".." + m[2]
		if m[1] == m[2] {
			emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` reviewed Git diff range has identical base and final commits.",
				Correction: "Use distinct full commits with the direct first parent of the task commit as base."})
			return false
		}
		if identities[len(identities)-1] != revision {
			emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` reviewed Git diff endpoint `" + identities[len(identities)-1] + "` does not equal `Revision / checkpoint` `" + revision + "`.",
				Correction: "Use a reviewed diff range whose exact endpoint is the task revision."})
			return false
		}
	}
	for _, id := range identities {
		if ok, _ := repo.RevisionExists(id); !ok {
			emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` reviewed Git identity names a commit absent from target repository `" + repository + "`.",
				Correction: "Use only full reviewed commits that exist in the target repository."})
			return false
		}
	}
	parents, perr := repo.Parents(revision)
	if perr != nil || len(parents) > 1 {
		emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` clean Git implementation revision `" + revision + "` is a merge commit.",
			Correction: "Record a non-merge, independently bisectable task implementation commit and review its complete diff."})
		return false
	}
	if len(identities) == 2 {
		if len(parents) != 1 || parents[0] != identities[0] {
			emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
				Message:    "`" + name + "` reviewed Git diff base `" + identities[0] + "` is not the direct first parent of task revision `" + revision + "`.",
				Correction: "Use `diff: <task revision first parent>..<task revision>` for a ranged focused review."})
			return false
		}
	}
	expectedCommand := "git show " + expectedReviewIdentity
	if len(identities) == 2 {
		expectedCommand = "git diff " + expectedReviewIdentity
	}
	recordedCommand := focused
	if m := focusedReviewRe.FindStringSubmatch(strings.TrimSpace(focused)); m != nil {
		recordedCommand = m[1]
	}
	if recordedCommand != expectedCommand {
		emit(Diagnostic{Code: "SDD169", Severity: Error, Path: a.Rel, Line: line,
			Message:    "`" + name + "` focused review command must be `" + expectedCommand + "` for reviewed identity `" + expectedReviewIdentity + "`.",
			Correction: "For clean Git, use `git show <full task commit>` or `git diff <full base>..<full task commit>` with no extra operands."})
		return false
	}
	return true
}

// taskEvidencePhase returns a minimal, structurally valid phase document
// (status in-progress, so the top-level `## Phase Completion Evidence`
// section stays inertly pending) whose single task `1.1` carries taskStatus
// and evidenceBody as its literal `### Completion Evidence` content — the
// fixture the evidence-family rules need to exercise arbitrary label content
// that the generic phaseWithTasks() template doesn't expose.
func taskEvidencePhase(taskStatus, evidenceBody string) string {
	return `---
title: Sample Phase
type: phase
status: in-progress
created: 2024-01-01
updated: 2024-01-01
plan: Sample
phase: "1"
deliverable: A thing.
tasks:
  - id: "1.1"
    title: First
    status: ` + taskStatus + `
    verification: x
    justifies: FR-01
---

## Overview

Text.

## Acceptance Criteria

- [ ] Works.

## 1.1: First

### Subtasks

- [x] Step.

### Notes

None.

### Completion Evidence

` + evidenceBody + `

## Phase Completion Evidence

Pending — not complete.
`
}

func init() {
	Register(&Rule{
		Code: "SDD070", Severity: Error, PyFunc: "_evidence",
		What:      "a complete task/phase/plan's completion evidence still reads the pending marker",
		CheckRoot: evidenceCheckRoot("SDD070"),
		Bad: []Example{{Name: "complete-but-pending", Files: map[string]string{
			"Plans/Sample/01-One.md": checkedPhase("complete", "1", "Sample", `
  - id: "1.1"
    title: First
    status: complete
    verification: x
    justifies: FR-01
`),
		}}},
		Good: []Example{{Name: "planned-pending-is-fine", Files: map[string]string{
			"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`),
		}}},
	})

	Register(&Rule{
		Code: "SDD071", Severity: Error, PyFunc: "_evidence",
		What:      "a populated completion-evidence section is missing one of its five required labels",
		CheckRoot: evidenceCheckRoot("SDD071"),
		Bad: []Example{{Name: "missing-labels", Files: map[string]string{
			"Plans/Sample/01-One.md": taskEvidencePhase("planned", "- Verified: 2024-01-01"),
		}}},
		Good: []Example{{Name: "all-five-labels", Files: map[string]string{
			"Plans/Sample/01-One.md": taskEvidencePhase("planned", `- Verified: 2024-01-01
- Repository: /tmp/nonexistent-sdd-target
- VCS: none
- Revision / checkpoint: none
- Identity recheck: none`),
		}}},
	})
}

// validGitEvidenceRev is a real Git commit's SHA, computed once (by literally
// running these commands) and hardcoded here: rules_test.go's runExample runs
// Example.Setup under a fixed author/committer identity and timestamp
// (setupEnv), so committing the exact same tree under the exact same
// metadata always produces this exact object id. It is the root commit of a
// one-file repository whose only tracked file is `code.txt` containing
// "code\n".
const validGitEvidenceRev = "1c8a628dfcda49f5ff17b5a2c551228ea23de68a"

// validGitTaskEvidenceBody is a fully conforming `### Completion Evidence`
// body for a complete task under clean Git identity: every SDD070-075/169/172
// check should stay quiet on it. {{REPO}} is substituted by runExample with
// the fixture root (Prerequisite A's RepoForArtifact resolves to exactly that
// directory when no planning-config.json repository mapping exists).
func validGitTaskEvidenceBody() string {
	return "- Verified: 2024-01-01\n" +
		"- Repository: {{REPO}}\n" +
		"- VCS: git\n" +
		"- Revision / checkpoint: " + validGitEvidenceRev + "\n" +
		"- Identity recheck: `git cat-file -t " + validGitEvidenceRev + "`; matched at 2024-01-01T00:00:00\n" +
		"- Focused review: `git show " + validGitEvidenceRev + "`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary\n" +
		"- Reviewed candidate / final: " + validGitEvidenceRev + "\n" +
		"- Review result: PASS/Aligned\n" +
		"\n" +
		"| Command | Working directory | Result | Observable evidence |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `go test ./...` | . | PASS (exit 0) | ok |\n"
}

// validGitTaskEvidenceExample is the shared Good fixture for the whole
// evidence-git family: a real one-commit-then-two repository (Setup) whose
// task 1.1 records validGitTaskEvidenceRev as its clean, ancestor-of-HEAD,
// committed, reviewed implementation.
func validGitTaskEvidenceExample(name string) Example {
	return Example{
		Name: name,
		Files: map[string]string{
			"code.txt":               "code\n",
			"Plans/Sample/01-One.md": taskEvidencePhase("complete", validGitTaskEvidenceBody()),
		},
		Setup: [][]string{
			{"git", "init", "-q"},
			{"git", "add", "code.txt"},
			{"git", "commit", "-q", "-m", "impl"},
			{"git", "add", "Plans/Sample/01-One.md"},
			{"git", "commit", "-q", "-m", "plan"},
		},
	}
}

func init() {
	Register(&Rule{
		Code: "SDD072", Severity: Error, PyFunc: "_evidence",
		What:      "completion evidence has an invalid date/VCS/revision/repository, no PASS row, or an unmet Git identity/lifecycle-commit check",
		CheckRoot: evidenceCheckRoot("SDD072"),
		Bad: []Example{
			{Name: "invalid-vcs", Files: map[string]string{
				"Plans/Sample/01-One.md": taskEvidencePhase("complete", `- Verified: 2024-01-01
- Repository: {{REPO}}
- VCS: bogus
- Revision / checkpoint: none
- Identity recheck: none`),
			}},
			// G-2: a shelved/pending changelist is mutable, not a durable
			// Perforce identity — only a bare submitted changelist number is.
			{Name: "invalid-p4-revision", Files: map[string]string{
				"Plans/Sample/01-One.md": taskEvidencePhase("planned", `- Verified: 2024-01-01
- Repository: {{REPO}}
- VCS: perforce
- Revision / checkpoint: pending CL 56834931, shelf of 2026-08-17
- Identity recheck: none`),
			}},
		},
		Good: []Example{
			validGitTaskEvidenceExample("clean-git-identity"),
			// G-6: a deliberate expected-failure experiment is first-class
			// command evidence when the Result cell names the exact status:
			// `PASS (exit N, expected)`.
			{Name: "expected-failure-row", Files: map[string]string{
				"Plans/Sample/01-One.md": taskEvidencePhase("planned", "- Verified: 2024-01-01\n"+
					"\n"+
					"| Command | Working directory | Result | Observable evidence |\n"+
					"| --- | --- | --- | --- |\n"+
					"| `go test ./... -run TestMustRefuse` | . | PASS (exit 1, expected) | refusal path fires as designed |\n"),
			}},
		},
	})

	Register(&Rule{
		Code: "SDD073", Severity: Error, PyFunc: "_evidence",
		What:      "completion evidence contains failing-shaped output outside a conforming result row",
		CheckRoot: evidenceCheckRoot("SDD073"),
		Bad: []Example{{Name: "stray-fail", Files: map[string]string{
			"Plans/Sample/01-One.md": taskEvidencePhase("planned", "- Verified: 2024-01-01\n\nSuite output: 3 FAILED, 1 passed."),
		}}},
		Good: []Example{validGitTaskEvidenceExample("no-stray-failures")},
	})

	Register(&Rule{
		Code: "SDD074", Severity: Error, PyFunc: "_evidence",
		What:      "completion evidence uses a removed synthetic identity field",
		CheckRoot: evidenceCheckRoot("SDD074"),
		Bad: []Example{{Name: "fallback-reason", Files: map[string]string{
			"Plans/Sample/01-One.md": taskEvidencePhase("planned", "- Verified: 2024-01-01\n- Fallback reason: none"),
		}}},
		Good: []Example{validGitTaskEvidenceExample("no-removed-fields")},
	})

	Register(&Rule{
		Code: "SDD075", Severity: Error, PyFunc: "_evidence",
		What:      "the identity recheck lacks a timestamped matching result, or doesn't name the tested revision",
		CheckRoot: evidenceCheckRoot("SDD075"),
		Bad: []Example{{Name: "recheck-no-timestamp", Files: map[string]string{
			"Plans/Sample/01-One.md": taskEvidencePhase("planned", "- Identity recheck: looks fine"),
		}}},
		Good: []Example{validGitTaskEvidenceExample("recheck-names-revision")},
	})

	Register(&Rule{
		Code: "SDD171", Severity: Error, PyFunc: "_verify_evidence_committed",
		What:      "a complete entity's lifecycle bookkeeping has no validated durable adapter for the planning root's SCM",
		CheckRoot: evidenceCheckRoot("SDD171"),
		Bad: []Example{{Name: "no-git-planning-root", Files: map[string]string{
			"Plans/Sample/01-One.md": taskEvidencePhase("complete", `- Verified: 2024-01-01
- Repository: {{REPO}}
- VCS: git
- Revision / checkpoint: `+validGitEvidenceRev+`
- Identity recheck: `+"`git cat-file -t "+validGitEvidenceRev+"`"+`; matched at 2024-01-01T00:00:00
- Focused review: `+"`git show "+validGitEvidenceRev+"`"+`; complete task diff reviewed for correctness, scope, tests, maintainability, and task boundary
- Reviewed candidate / final: `+validGitEvidenceRev+`
- Review result: PASS/Aligned

| Command | Working directory | Result | Observable evidence |
| --- | --- | --- | --- |
| `+"`go test ./...`"+` | . | PASS (exit 0) | ok |`),
		}}},
		Good: []Example{validGitTaskEvidenceExample("git-planning-root")},
	})

	Register(&Rule{
		Code: "SDD169", Severity: Error, PyFunc: "_task_review_evidence",
		What: "a task's focused review/reviewed-identity/review-result evidence is malformed — or, on a complete task, missing or unresolvable",
		// The format layer (label completeness once any review label is
		// written, focused-review syntax, PASS/Aligned result) runs on every
		// status so evidence is written correctly the first time (G-5); the
		// git identity layer runs only on complete tasks.
		CheckRoot: evidenceCheckRoot("SDD169"),
		Bad: []Example{
			{Name: "missing-review-labels", Files: map[string]string{
				"Plans/Sample/01-One.md": taskEvidencePhase("complete", `- Verified: 2024-01-01
- Repository: {{REPO}}
- VCS: none
- Revision / checkpoint: none
- Identity recheck: none`),
			}},
			// G-5: a malformed focused-review line is caught while the task
			// is still in-progress, not first at the completion transition.
			{Name: "in-progress-bad-format", Files: map[string]string{
				"Plans/Sample/01-One.md": taskEvidencePhase("in-progress", `- Verified: 2024-01-01
- Focused review: looked at the diff, seemed fine
- Reviewed candidate / final: none
- Review result: PASS/Aligned`),
			}},
		},
		Good: []Example{
			validGitTaskEvidenceExample("resolved-clean-git-review"),
			// No review labels drafted yet on an in-progress task: presence
			// is the completion transition's concern, not the format layer's.
			{Name: "in-progress-not-yet-drafted", Files: map[string]string{
				"Plans/Sample/01-One.md": taskEvidencePhase("in-progress", `- Verified: 2024-01-01

| Command | Working directory | Result | Observable evidence |
| --- | --- | --- | --- |
| `+"`go test ./...`"+` | . | PASS (exit 0) | ok |`),
			}},
		},
	})

	Register(&Rule{
		Code: "SDD172", Severity: Error, PyFunc: "_evidence",
		What: "a complete entity's VCS has no validated native identity adapter",
		// Two emission sites. The generic evidence path covers a complete
		// entity whose own VCS has no adapter; phaseTaskGitIdentities covers a
		// completed TASK whose evidence carries no deterministic Git identity,
		// which is what a phase's review range is assembled from. The second
		// was missed when this code was first ported.
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			evidenceCheckRoot("SDD172")(r, emit)
			for _, ctx := range completePhasesWithEvidence(r) {
				phaseTaskGitIdentities(r, ctx.Phase, ctx.Line, emit)
			}
		},
		Bad: []Example{{Name: "no-adapter-for-none-vcs", Files: map[string]string{
			"Plans/Sample/01-One.md": taskEvidencePhase("complete", `- Verified: 2024-01-01
- Repository: {{REPO}}
- VCS: none
- Revision / checkpoint: none
- Identity recheck: none`),
		}}},
		Good: []Example{validGitTaskEvidenceExample("git-adapter-available")},
	})
}
