package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// Lifecycle transition verbs (FR-21): `sdd task complete`, `sdd phase
// complete`, `sdd plan complete`.
//
// Each enforces the gate its schema declares by running the same rule
// implementations `sdd validate` uses. That reuse is the requirement, not an
// optimization: a second copy of "what makes a phase completable" would drift
// from the validator, and the gate that matters is the one the validator
// enforces.
//
// The check is performed by making the transition in memory and validating the
// result. If completing the entity would produce a diagnostic, the transition
// is refused and the diagnostic is the refusal — so the reason a gate is unmet
// is always a real finding with an artifact path and line, exactly as FR-21
// requires.

// completeOpts is what a `<kind> complete` transition needs. `approve` and
// `activate` are separate verbs rather than flags because each gates on
// something different, and because a plan that jumps straight to `complete`
// leaves no record that it was ever reviewed or started.
type completeOpts struct {
	ID     string
	DryRun bool
	JSON   bool
}

func cmdComplete(kind, path string, o completeOpts) error {
	if kind == "task" && o.ID == "" {
		return fmt.Errorf("task complete: --id is required")
	}

	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}
	if !art.Exists {
		return fmt.Errorf("%s complete: %s does not exist", kind, path)
	}
	// Write where the artifact was actually read. Read resolves a
	// planning-root-relative spelling; writing back to the unresolved
	// argument would create a shadow file at the literal path (./Plans/...)
	// while the artifact just read stays unchanged.
	path = art.Path

	today := time.Now().Format("2006-01-02")
	updated, err := applyTransition(art.Source, kind, o.ID, today)
	if err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}

	// Validate the would-be result, not the current state: the question is
	// whether completing is permitted, which only the completed form answers.
	blocking, err := gateDiagnostics(path, updated)
	if err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}
	// Two families cannot be satisfied while the transition is being checked,
	// and both are reported as follow-ups rather than refusals.
	//
	// SDD070-075 read the COMMITTED copy at HEAD, so a status that is not yet
	// committed can never satisfy them — it must be committed to be seen and
	// pass to be written.
	//
	// SDD173's clean-worktree branch is self-inflicted: gateDiagnostics writes
	// the candidate to disk so the real rules can evaluate it, which dirties
	// the very tree that check inspects. Refusing on that would make the verb
	// permanently unusable on a phase whose worktree is otherwise clean —
	// exactly the case it exists for. `sdd validate` still reports both after
	// the write lands, so neither is suppressed, only deferred.
	blocking, pending := splitCommitPending(blocking)

	res := transitionResult{
		Path: relPath(path), Kind: kind, Verb: "complete", ID: o.ID,
		To: "complete", DryRun: o.DryRun,
		Blocking: toGateFindings(blocking), Pending: toGateFindings(pending),
	}

	if len(blocking) > 0 {
		if o.JSON {
			return emitTransitionJSON(res)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s complete: refused — the completion gate is not met:\n", kind)
		for _, d := range blocking {
			fmt.Fprintf(&b, "  %s %s:%d: %s\n", d.Code, d.Path, d.Line, d.Message)
			fmt.Fprintf(&b, "      fix: %s\n", d.Correction)
		}
		return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}
	res.OK = true

	if o.DryRun {
		if o.JSON {
			return emitTransitionJSON(res)
		}
		fmt.Printf("%s complete: gate met; would mark %s complete\n", kind, describeTarget(kind, o.ID, path))
		return nil
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("%s complete: %w", kind, err)
	}
	res.Wrote = true
	if o.JSON {
		return emitTransitionJSON(res)
	}
	fmt.Printf("marked %s complete in %s\n", describeTarget(kind, o.ID, path), path)
	if len(pending) > 0 {
		fmt.Printf("  next: commit this change; %d evidence check(s) verify the "+
			"committed copy at HEAD and stay unmet until then\n", len(pending))
	}
	return nil
}

// splitCommitPending separates genuine refusals from the checks that inspect
// the committed/submitted copy of the planning artifact and therefore cannot
// pass before the transition itself is committed (git) or submitted (p4).
func splitCommitPending(all []rules.Diagnostic) (blocking, pending []rules.Diagnostic) {
	for _, d := range all {
		if strings.Contains(d.Message, "is not committed at HEAD") ||
			strings.Contains(d.Message, "is not submitted to the depot") ||
			strings.Contains(d.Message, "requires the current target worktree to be clean") {
			pending = append(pending, d)
			continue
		}
		blocking = append(blocking, d)
	}
	return
}

func describeTarget(kind, id, path string) string {
	if kind == "task" {
		return "task " + id
	}
	return kind
}

// applyTransition sets the target's status to `complete` in a copy of source.
func applyTransition(source, kind, id, today string) (string, error) {
	lines := strings.Split(source, "\n")
	switch kind {
	case "plan", "phase":
		if !setTopLevelStatus(lines, "complete") {
			return "", fmt.Errorf("no top-level `status:` field to advance")
		}
	case "task":
		if !setEntryStatus(lines, id, "complete") {
			return "", fmt.Errorf("no task `%s` in this phase's tasks[]", id)
		}
	}
	return restampUpdated(strings.Join(lines, "\n"), today), nil
}

// setTopLevelStatus rewrites the frontmatter's own `status:` line.
func setTopLevelStatus(lines []string, value string) bool {
	for i, l := range lines {
		if i > 0 && strings.TrimSpace(l) == "---" {
			return false
		}
		if strings.HasPrefix(l, "status:") {
			lines[i] = "status: " + value
			return true
		}
	}
	return false
}

// setEntryStatus rewrites one tasks[] entry's status, located by its id.
func setEntryStatus(lines []string, id, value string) bool {
	inEntry := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "- id:") {
			entryID := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- id:")), `"'`)
			inEntry = entryID == id
			continue
		}
		if inEntry && strings.HasPrefix(trimmed, "status:") {
			indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
			lines[i] = indent + "status: " + value
			return true
		}
	}
	return false
}

// gateDiagnostics validates a candidate document in place and returns the
// diagnostics that the transition itself would introduce.
//
// Pre-existing findings are excluded deliberately: refusing a completion
// because of an unrelated defect elsewhere would make the verb unusable in a
// root that is not already perfect, and those findings are `sdd validate`'s to
// report.
//
// Both runs are scoped to the transitioned artifact's plan (plus everything
// outside Plans/ and other plans' READMEs) via rules.ScopeToPlan. The scope
// is provably diff-neutral — see ScopeToPlan's contract — and removes the
// dominant cost of the gate on mature roots: re-verifying every other plan's
// completed tasks against the repository, twice, only to subtract the
// findings out again.
func gateDiagnostics(path, candidate string) ([]rules.Diagnostic, error) {
	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return nil, err
	}
	planRel, docRel := "", ""
	if abs, absErr := filepath.Abs(path); absErr == nil {
		if rel, relErr := filepath.Rel(root, abs); relErr == nil {
			slash := filepath.ToSlash(rel)
			planRel = rules.PlanRelOf(slash)
			// A spec or design lives outside Plans/, so PlanRelOf yields
			// nothing for it and the gate validated the whole root twice.
			// ScopeToDoc drops the half that cannot respond to a doc's status
			// flip — other plans' phase docs and reviews, whose completion
			// evidence is re-verified against the repository on every run.
			if planRel == "" {
				docRel = slash
			}
		}
	}
	run := func() ([]rules.Diagnostic, error) {
		loaded, err := rules.LoadRootRepo(root, repoRoot)
		if err != nil {
			return nil, err
		}
		switch {
		case planRel != "":
			loaded = rules.ScopeToPlan(loaded, planRel)
		case docRel != "":
			loaded = rules.ScopeToDoc(loaded, docRel)
		}
		// RunWithWaivers, not Run: the gate's criterion must be the same one
		// `sdd validate` applies by default, where an accepted exception
		// re-tags its finding Waived (reported, not invalidating). Plain Run
		// leaves matched errors at Error severity, which would make the gate
		// refuse transitions on findings the validator itself excuses.
		return rules.RunWithWaivers(loaded), nil
	}

	beforeDiags, err := run()
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	for _, d := range beforeDiags {
		existing[diagKey(d)] = true
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := store.WriteAtomic(path, candidate); err != nil {
		return nil, err
	}
	defer store.WriteAtomic(path, string(original))

	// The candidate write above is the one legitimate mid-process worktree
	// mutation, and the rules that read the index must see it rather than the
	// answer cached during the `before` run.
	vcs.InvalidateWorkingState()
	defer vcs.InvalidateWorkingState()

	afterDiags, err := run()
	if err != nil {
		return nil, err
	}
	var introduced []rules.Diagnostic
	for _, d := range afterDiags {
		// Only invalidating findings gate a transition — the same criterion
		// `sdd validate` uses for Invalid. A Waived finding is a human-accepted
		// exception recorded in the artifact's own frontmatter: it is still
		// reported by validate, but it does not make the root invalid, so it
		// must not make a transition refuse either (the gate's contract is
		// "the artifact validates", not "the artifact has no findings").
		if d.Severity != rules.Error {
			continue
		}
		if !existing[diagKey(d)] {
			introduced = append(introduced, d)
		}
	}
	return introduced, nil
}

func diagKey(d rules.Diagnostic) string {
	return d.Code + "\x00" + d.Path + "\x00" + fmt.Sprint(d.Line) + "\x00" + d.Message
}

// candidateArtifactErrors validates a candidate document in place and returns
// the Error-severity diagnostics ON THAT ARTIFACT — not the introduced-only
// diff gateDiagnostics computes. The freezing verbs need this stronger form:
// a frozen artifact is immutable, so any invalid byte it carries at freeze
// time becomes permanently invalid, whether or not the freezing transition
// introduced it. Waivers are honored (RunWithWaivers), the same criterion
// `sdd validate` applies by default.
func candidateArtifactErrors(path, candidate string) ([]rules.Diagnostic, error) {
	root, repoRoot, err := resolveRoots(".", "")
	if err != nil {
		return nil, err
	}
	rel := ""
	if abs, absErr := filepath.Abs(path); absErr == nil {
		if r, relErr := filepath.Rel(root, abs); relErr == nil {
			rel = filepath.ToSlash(r)
		}
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := store.WriteAtomic(path, candidate); err != nil {
		return nil, err
	}
	defer store.WriteAtomic(path, string(original))
	vcs.InvalidateWorkingState()
	defer vcs.InvalidateWorkingState()

	loaded, err := rules.LoadRootRepo(root, repoRoot)
	if err != nil {
		return nil, err
	}
	if planRel := rules.PlanRelOf(rel); planRel != "" {
		loaded = rules.ScopeToPlan(loaded, planRel)
	} else if rel != "" {
		loaded = rules.ScopeToDoc(loaded, rel)
	}
	var out []rules.Diagnostic
	for _, d := range rules.RunWithWaivers(loaded) {
		if d.Severity == rules.Error && d.Path == rel {
			out = append(out, d)
		}
	}
	return out, nil
}

var _ = artifact.Parse

// planLifecycle implements `sdd plan approve` and `sdd plan activate`.
//
// Each enforces the one precondition that makes the transition meaningful:
// approving a plan requires it to validate (an invalid plan has not been
// reviewed, whatever a human says), and activating requires it to have been
// approved first. Neither invents a gate the schema does not declare.
func planLifecycle(verb, path string, dryRun, jsonOut bool) error {
	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("plan %s: %w", verb, err)
	}
	if !art.Exists {
		return fmt.Errorf("plan %s: %s does not exist", verb, path)
	}
	path = art.Path // write where the artifact was read, never the unresolved argument
	doc := artifact.Parse(art.Source)
	current, _ := doc.FM("status")
	current = strings.Trim(current, `"'`)

	want, from := "approved", "draft"
	if verb == "activate" {
		want, from = "active", "approved"
	}
	res := transitionResult{
		Path: relPath(path), Kind: "plan", Verb: verb,
		From: current, To: want, DryRun: dryRun,
	}
	if current == want {
		res.OK, res.Already = true, true
		if jsonOut {
			return emitTransitionJSON(res)
		}
		fmt.Printf("plan %s: already %s\n", verb, want)
		return nil
	}
	if current != from {
		return fmt.Errorf("plan %s: plan is %q; %s moves a plan from %q to %q. "+
			"A plan that skips a state leaves no record it was ever in it",
			verb, current, verb, from, want)
	}

	today := time.Now().Format("2006-01-02")
	lines := strings.Split(art.Source, "\n")
	if !setTopLevelStatus(lines, want) {
		return fmt.Errorf("plan %s: no top-level `status:` field to advance", verb)
	}
	updated := restampUpdated(strings.Join(lines, "\n"), today)

	// Approving asserts the plan is fit to build from, so it must validate.
	if verb == "approve" {
		blocking, err := gateDiagnostics(path, updated)
		if err != nil {
			return fmt.Errorf("plan approve: %w", err)
		}
		var pending []rules.Diagnostic
		blocking, pending = splitCommitPending(blocking)
		res.Blocking, res.Pending = toGateFindings(blocking), toGateFindings(pending)
		if len(blocking) > 0 {
			if jsonOut {
				return emitTransitionJSON(res)
			}
			var b strings.Builder
			b.WriteString("plan approve: refused — the plan does not validate:\n")
			for _, d := range blocking {
				fmt.Fprintf(&b, "  %s %s:%d: %s\n", d.Code, d.Path, d.Line, d.Message)
			}
			return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
		}
	}
	res.OK = true

	if dryRun {
		if jsonOut {
			return emitTransitionJSON(res)
		}
		fmt.Printf("plan %s: would move %s from %s to %s\n", verb, path, current, want)
		return nil
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("plan %s: %w", verb, err)
	}
	res.Wrote = true
	if jsonOut {
		return emitTransitionJSON(res)
	}
	fmt.Printf("plan %s: %s is now %s\n", verb, path, want)
	return nil
}

// docTransitions is the spec/design lifecycle verb table. Every entry names
// the statuses a verb may leave, so skipping a state (draft straight to
// approved) is refused for the same reason plan activate refuses it: an
// artifact that skips a state leaves no record it was ever in it. Supersede
// is the exception — historical documents get replaced from any live state.
var docTransitions = map[string]struct {
	from []string
	to   string
}{
	"submit":    {[]string{"draft"}, "review"},
	"approve":   {[]string{"review"}, "approved"},
	"implement": {[]string{"approved"}, "implemented"},
	"supersede": {[]string{"draft", "review", "approved", "implemented"}, "superseded"},
}

// docLifecycle implements `sdd spec|design submit|approve|implement|supersede`.
//
// These close the gap that made spec/design statuses unreachable: `status` is
// tool-owned (FR-18) so `apply` refuses it in payloads, but no verb existed to
// move it — the specify/design skills' "set status: review/approved" had no
// supported write path at all. Every verb validates the would-be result and
// refuses on diagnostics the transition itself would introduce, which is what
// gives `approve` its teeth: SDD153 rejects an approved artifact with a
// blocking or unexplained open question.
//
// `supersede` optionally records the replacing artifact via --by; because the
// gate only counts *introduced* diagnostics, a legacy document can be
// superseded as-is without first migrating it into schema shape.
func docLifecycle(kind, verb, path, by string, dryRun, jsonOut bool) error {
	tr, ok := docTransitions[verb]
	if !ok {
		return fmt.Errorf("%s %s: unknown verb", kind, verb)
	}
	if by != "" && verb != "supersede" {
		return fmt.Errorf("%s %s: --by only applies to supersede", kind, verb)
	}
	if by != "" {
		// A successor that does not resolve is almost always a typo, and the
		// link is the whole point of passing --by: recording a dangling path
		// silently produces a supersession chain that goes nowhere.
		if succ, err := store.Read(by); err != nil || !succ.Exists {
			return fmt.Errorf("%s supersede: --by %s does not resolve to an artifact; "+
				"pass the successor's planning-root-relative path, or supersede it once the replacement exists",
				kind, by)
		}
	}

	art, err := store.Read(path)
	if err != nil {
		return fmt.Errorf("%s %s: %w", kind, verb, err)
	}
	if !art.Exists {
		return fmt.Errorf("%s %s: %s does not exist", kind, verb, path)
	}
	path = art.Path // write where the artifact was read, never the unresolved argument
	doc := artifact.Parse(art.Source)
	if typ, _ := doc.FM("type"); strings.Trim(typ, `"'`) != kind {
		return fmt.Errorf("%s %s: %s is not a %s artifact", kind, verb, path, kind)
	}
	current, _ := doc.FM("status")
	current = strings.Trim(current, `"'`)

	res := transitionResult{
		Path: relPath(path), Kind: kind, Verb: verb,
		From: current, To: tr.to, DryRun: dryRun,
	}
	// Already in the target state is a no-op — except when supersede is asked
	// to record a successor the artifact does not yet carry. Superseding
	// without --by and linking the replacement afterwards is a normal
	// sequence (the successor often does not exist yet at supersede time);
	// treating it as a no-op silently discarded the link and reported success.
	linkOnly := verb == "supersede" && by != "" &&
		strings.Trim(metaOf(doc, "superseded_by"), `"'`) != filepath.ToSlash(by)
	if current == tr.to && !linkOnly {
		res.OK, res.Already = true, true
		if jsonOut {
			return emitTransitionJSON(res)
		}
		fmt.Printf("%s %s: already %s\n", kind, verb, tr.to)
		return nil
	}
	allowed := current == tr.to // linkOnly: already there, recording the successor
	for _, f := range tr.from {
		allowed = allowed || current == f
	}
	if !allowed {
		return fmt.Errorf("%s %s: artifact is %q; %s moves a %s from %s to %q. "+
			"An artifact that skips a state leaves no record it was ever in it",
			kind, verb, current, verb, kind, quotedList(tr.from), tr.to)
	}

	// A superseded artifact must name its successor: SDD178 refuses the
	// status without the link, so allowing a bare supersede would have the
	// verb write a state its own validator rejects. Recording the link later
	// is still supported — `supersede --by` on an already-superseded artifact
	// fills it in — but the first transition has to establish it.
	if verb == "supersede" && by == "" && metaOf(doc, "superseded_by") == "" {
		return fmt.Errorf("%s supersede: --by is required; a superseded %s must name the "+
			"artifact that replaces it (SDD178). Pass --by <successor-path>", kind, kind)
	}

	today := time.Now().Format("2006-01-02")
	lines := strings.Split(art.Source, "\n")
	if !setTopLevelStatus(lines, tr.to) {
		return fmt.Errorf("%s %s: no top-level `status:` field to advance", kind, verb)
	}
	if verb == "supersede" && by != "" {
		lines = upsertTopLevelScalar(lines, "superseded_by", fmt.Sprintf("%q", filepath.ToSlash(by)))
	}
	updated := restampUpdated(strings.Join(lines, "\n"), today)

	blocking, err := gateDiagnostics(path, updated)
	if err != nil {
		return fmt.Errorf("%s %s: %w", kind, verb, err)
	}
	var pending []rules.Diagnostic
	blocking, pending = splitCommitPending(blocking)
	res.Blocking, res.Pending = toGateFindings(blocking), toGateFindings(pending)
	if len(blocking) > 0 {
		if jsonOut {
			return emitTransitionJSON(res)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s %s: refused — the transition introduces findings:\n", kind, verb)
		for _, d := range blocking {
			fmt.Fprintf(&b, "  %s %s:%d: %s\n", d.Code, d.Path, d.Line, d.Message)
			fmt.Fprintf(&b, "      fix: %s\n", d.Correction)
		}
		return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
	}
	res.OK = true

	if dryRun {
		if jsonOut {
			return emitTransitionJSON(res)
		}
		fmt.Printf("%s %s: would move %s from %s to %s\n", kind, verb, path, current, tr.to)
		return nil
	}
	if err := store.WriteAtomic(path, updated); err != nil {
		return fmt.Errorf("%s %s: %w", kind, verb, err)
	}
	res.Wrote = true
	if jsonOut {
		return emitTransitionJSON(res)
	}
	fmt.Printf("%s %s: %s is now %s\n", kind, verb, path, tr.to)
	return nil
}

// metaOf returns a frontmatter scalar, or "" when absent.
func metaOf(doc *artifact.Doc, key string) string {
	v, _ := doc.FM(key)
	return v
}

func quotedList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	return strings.Join(quoted, "|")
}

// upsertTopLevelScalar rewrites a top-level frontmatter scalar, or inserts it
// after the `status:` line when absent — the caller has just set status, so
// the anchor is guaranteed to exist.
func upsertTopLevelScalar(lines []string, key, value string) []string {
	if setTopLevelScalar(lines, key, value) {
		return lines
	}
	for i, l := range lines {
		if i > 0 && strings.TrimSpace(l) == "---" {
			break
		}
		if strings.HasPrefix(l, "status:") {
			out := append([]string{}, lines[:i+1]...)
			out = append(out, key+": "+value)
			return append(out, lines[i+1:]...)
		}
	}
	return lines
}

// transitionResult is the machine-readable outcome of a lifecycle transition
// (FR-04). One shape serves `task|phase|plan complete` and `plan approve`:
// they answer the same question — did the status move, and if not, what
// blocked it — and a caller scripting a phase close should not need a
// different parser per verb.
//
// Gate findings are the reason this matters most. The text path renders them
// as prose, so a caller that wants to know *why* a completion was refused had
// to scrape it; here they are structured, and `pending` separately carries the
// checks that inspect the committed copy and therefore cannot pass until the
// transition itself is committed.
type transitionResult struct {
	Path    string `json:"path"`
	OK      bool   `json:"ok"`
	Kind    string `json:"kind"`           // task | phase | plan
	Verb    string `json:"verb"`           // complete | approve | activate
	ID      string `json:"id,omitempty"`   // task id, when kind is task
	From    string `json:"from,omitempty"` // status before the transition
	To      string `json:"to,omitempty"`   // status after it
	DryRun  bool   `json:"dry_run,omitempty"`
	Wrote   bool   `json:"wrote,omitempty"`
	Already bool   `json:"already,omitempty"` // already in the target state; a no-op
	// Blocking lists the gate findings that refused the transition.
	Blocking []gateFinding `json:"blocking,omitempty"`
	// Pending lists checks that verify the committed copy at HEAD and stay
	// unmet until this change is committed. They do not block.
	Pending []gateFinding `json:"pending,omitempty"`
}

type gateFinding struct {
	Code       string `json:"code"`
	Path       string `json:"path"`
	Line       int    `json:"line,omitempty"`
	Message    string `json:"message"`
	Correction string `json:"correction,omitempty"`
}

func toGateFindings(diags []rules.Diagnostic) []gateFinding {
	var out []gateFinding
	for _, d := range diags {
		out = append(out, gateFinding{
			Code: d.Code, Path: d.Path, Line: d.Line,
			Message: d.Message, Correction: d.Correction,
		})
	}
	return out
}

// emitTransitionJSON writes the result and returns the refusal error when the
// transition did not happen, so --json and the text path agree on exit codes.
func emitTransitionJSON(res transitionResult) error {
	if err := writeJSON(res); err != nil {
		return err
	}
	if !res.OK {
		return &refusedError{n: len(res.Blocking)}
	}
	return nil
}
