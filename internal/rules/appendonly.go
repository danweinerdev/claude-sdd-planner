package rules

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// Family: Validator._append_only_repository_history — SDD154/155/156 (a
// previously tracked spec element, phase, or task id was removed) and SDD164
// (a previously tracked artifact changed type or disappeared).
//
// These compare the repository's committed HEAD against both the worktree and
// the index. Checking the worktree alone would pass a deletion that is only
// staged, and no walk of the current tree can find a deleted artifact at all —
// which is exactly what these rules look for. That is why they need
// TrackedPaths and FileInIndex rather than the existing FileAt.
//
// Diagnostics carry no artifact: the subject may not exist any more. Python
// passes path= explicitly for the same reason.
//
// One narrow exemption (see phaseConversionExcuses): a v1 PHASE document that
// disappears or changes type during a deliberate graph conversion is not a
// removal — its task ids survive as graph nodes (or the retired register), so
// SDD156 and SDD164 for that document are suppressed. Every other artifact
// kind, and every non-converted phase, keeps the normal append-only checks.

var specRetainedRemovedRe = regexp.MustCompile(
	`(?im)^\s*-\s+(?:\[[ xX]\]\s+)?\*\*((?:FR|NFR|AC)-\d{2,})\*\*\s*:\s*removed\s+[—-]\s+see\s+\S.*$`)

var specRetainedStruckRe = regexp.MustCompile(
	`(?m)^\s*-\s+(?:\[[ xX]\]\s+)?~~\*\*((?:FR|NFR|AC)-\d{2,})\*\*\s*:\s*\S.*~~\s*$`)

// specDefinitionIDsFromSource ports spec_definition_ids(): every FR/NFR/AC a
// spec source defines. It reads visible markdown, so a definition inside a
// fenced block or comment does not count.
func specDefinitionIDsFromSource(source string) map[string]bool {
	body := visibleMarkdown(source)
	out := map[string]bool{}
	for _, family := range []string{"FR", "NFR", "AC"} {
		for _, m := range specDefinitionRe[family].FindAllStringSubmatch(body, -1) {
			out[m[1]] = true
		}
	}
	return out
}

// specRetainedIDs ports spec_retained_ids(): the ids a spec still accounts
// for, whether by defining them or by explicitly retiring them. A retired id
// is retained — the rule is append-only, not immutable.
func specRetainedIDs(source string) map[string]bool {
	out := specDefinitionIDsFromSource(source)
	for _, line := range strings.Split(visibleMarkdown(source), "\n") {
		for _, re := range []*regexp.Regexp{specRetainedRemovedRe, specRetainedStruckRe} {
			if m := re.FindStringSubmatch(line); m != nil {
				out[m[1]] = true
				break
			}
		}
	}
	return out
}

// frontmatterEntryIDs ports frontmatter_entry_ids(): the ids declared in a
// source's named block sequence.
func frontmatterEntryIDs(source, field string) map[string]bool {
	out := map[string]bool{}
	a := parseArtifactBytes([]byte(source), "", "")
	if a == nil || a.Meta == nil {
		return out
	}
	for _, e := range asAnyList(a.Meta[field]) {
		m := planEntry(e)
		if m == nil {
			continue
		}
		if id := metaStr(m, "id"); id != "" {
			out[id] = true
		}
	}
	return out
}

// retainedIDCheck is one kind's append-only comparison, as Python's
// if/elif chain selects it.
type retainedIDCheck struct {
	code       string
	noun       string
	correction string
	prior      map[string]bool
	retained   map[string]bool
}

func retainedIDsFor(kind, baseline, current string) retainedIDCheck {
	switch kind {
	case "spec":
		return retainedIDCheck{
			code: "SDD154", noun: "spec",
			correction: "Restore the id and mark it retired with `removed — see <reason/citation>` or a struck-through definition.",
			prior:      specDefinitionIDsFromSource(baseline),
			retained:   specRetainedIDs(current),
		}
	case "plan":
		return retainedIDCheck{
			code: "SDD155", noun: "phase",
			correction: "Restore the append-only phase id and preserve its historical entry.",
			prior:      frontmatterEntryIDs(baseline, "phases"),
			retained:   frontmatterEntryIDs(current, "phases"),
		}
	default:
		return retainedIDCheck{
			code: "SDD156", noun: "task",
			correction: "Restore the append-only task id and preserve its historical entry.",
			prior:      frontmatterEntryIDs(baseline, "tasks"),
			retained:   frontmatterEntryIDs(current, "tasks"),
		}
	}
}

// appendOnlyScans counts history scans so a test can prove one evaluation
// scans once (FR-08).
var appendOnlyScans atomic.Int64

// appendOnlyFinding is one diagnostic this family produces, before it is
// filtered to a single code.
type appendOnlyFinding struct {
	Code       string
	Path       string
	Message    string
	Correction string
}

// appendOnlyHistory returns the family's findings for this Root, scanning
// history at most once per Root (FR-08, DD-6). Each registered rule keeps
// only its own code, so the four rules agree on exactly what one scan found.
// The result is evaluation-local (DD-7): it lives on the Root, which is
// immutable once loaded; a caller that needs to observe a HEAD, index or
// worktree change loads a new Root.
func appendOnlyHistory(r *Root) []appendOnlyFinding {
	r.appendMu.Lock()
	defer r.appendMu.Unlock()
	if !r.appendDone {
		r.appendFindings = scanAppendOnlyHistory(r)
		r.appendDone = true
	}
	return append([]appendOnlyFinding(nil), r.appendFindings...)
}

// scanAppendOnlyHistory ports _append_only_repository_history: one pass over
// the repository comparing HEAD, the index and the worktree.
func scanAppendOnlyHistory(r *Root) []appendOnlyFinding {
	appendOnlyScans.Add(1)
	var out []appendOnlyFinding
	repo := r.Repo(r.Dir)
	if !gitCapable(repo) {
		return out
	}
	prefix, err := relativeWithin(repo.Root(), r.Dir)
	if err != nil {
		return out
	}
	var roots []string
	for _, name := range []string{"Specs", "Plans"} {
		if prefix == "." {
			roots = append(roots, name)
			continue
		}
		roots = append(roots, prefix+"/"+name)
	}
	tracked, err := repo.TrackedPaths("HEAD", roots)
	if err != nil {
		return out
	}

	for _, repositoryRelative := range tracked {
		if !strings.HasSuffix(repositoryRelative, ".md") {
			continue
		}
		baselineBytes, err := repo.FileAt("HEAD", repositoryRelative)
		if err != nil {
			continue
		}
		baseline := string(baselineBytes)
		baseArtifact := parseArtifactBytes([]byte(baseline), "", "")
		if baseArtifact == nil || baseArtifact.Meta == nil {
			continue
		}
		kind := metaStr(baseArtifact.Meta, "type")
		if kind != "spec" && kind != "plan" && kind != "phase" {
			continue
		}

		artifactRelative := repositoryRelative
		if prefix != "." && strings.HasPrefix(repositoryRelative, prefix+"/") {
			artifactRelative = strings.TrimPrefix(repositoryRelative, prefix+"/")
		}

		worktreeBytes, worktreeErr := os.ReadFile(filepath.Join(repo.Root(), filepath.FromSlash(repositoryRelative)))
		worktree, worktreeOK := "", false
		if worktreeErr == nil {
			worktree, worktreeOK = string(worktreeBytes), true
		}
		indexBytes, indexErr := repo.FileInIndex(repositoryRelative)
		index, indexOK := "", false
		if indexErr == nil {
			index, indexOK = string(indexBytes), true
		}

		for _, source := range []struct {
			name    string
			content string
			present bool
		}{{"worktree", worktree, worktreeOK}, {"index", index, indexOK}} {
			content := source.content
			if !source.present {
				content = ""
			}
			excused := false
			if kind == "phase" {
				excused = phaseConversionExcuses(repo, baseline, repositoryRelative, source.name)
			}
			out = append(out, checkRetainedIDs(kind, baseline, content, artifactRelative, source.name, excused)...)
		}
	}
	return out
}

// readSourceFile returns a repository-relative path's bytes from one
// repository state: the worktree on disk or the staged index. ok is false when
// the file is absent from that state — which is itself meaningful: an unstaged
// graph must never excuse a staged deletion.
func readSourceFile(repo vcs.Repo, repoRel, sourceName string) ([]byte, bool) {
	if sourceName == "index" {
		raw, err := repo.FileInIndex(repoRel)
		if err != nil {
			return nil, false
		}
		return raw, true
	}
	raw, err := os.ReadFile(filepath.Join(repo.Root(), filepath.FromSlash(repoRel)))
	if err != nil {
		return nil, false
	}
	return raw, true
}

// phaseConversionExcuses reports whether a v1 phase document's removal or
// replacement in one repository state (worktree or index) is a deliberate
// graph conversion, in which case SDD156/SDD164 for that document in that
// state are suppressed.
//
// The recognition is deliberately narrow — every condition must hold, each
// evaluated against the SAME state (worktree history against the worktree
// graph/README/views, index history against the index graph/README/views):
//
//  1. the owning same-plan graph exists in that state,
//  2. it parses as a valid graph (the strict model decoder),
//  3. it carries at least one live (replacement) node,
//  4. every baseline task id is accounted for by a live node id or the
//     append-only retired register, under the convert naming (1.1 -> task-1-1),
//     and
//  5. the current plan README (same state) declares a generated replacement
//     phase view — same plan, same phase ordinal, source-of-truth marker — so
//     a graph stub or an arbitrary graph in a parent cannot excuse a deletion.
func phaseConversionExcuses(repo vcs.Repo, baseline, phaseRepoRel, sourceName string) bool {
	baseArt := parseArtifactBytes([]byte(baseline), "", "")
	if baseArt == nil || baseArt.Meta == nil {
		return false
	}
	phaseID := metaStr(baseArt.Meta, "phase")
	if phaseID == "" {
		return false
	}
	baselineTasks := frontmatterEntryIDs(baseline, "tasks")
	if len(baselineTasks) == 0 {
		return false
	}

	planDir := path.Dir(phaseRepoRel)
	planName := path.Base(planDir)

	graphBytes, ok := readSourceFile(repo, path.Join(planDir, planName+"-Graph.json"), sourceName)
	if !ok {
		return false
	}
	g, err := model.DecodeGraph(graphBytes)
	if err != nil || len(g.Nodes) == 0 {
		return false
	}
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		ids[n.ID] = true
	}
	for _, id := range g.Retired {
		ids[id] = true
	}
	for taskID := range baselineTasks {
		if !ids[taskNodeID(taskID)] {
			return false
		}
	}

	readmeBytes, ok := readSourceFile(repo, path.Join(planDir, "README.md"), sourceName)
	if !ok {
		return false
	}
	readmeArt := parseArtifactBytes(readmeBytes, "", "")
	if readmeArt == nil || readmeArt.Meta == nil {
		return false
	}
	for _, p := range asAnyList(readmeArt.Meta["phases"]) {
		m := planEntry(p)
		if m == nil {
			continue
		}
		doc := metaStr(m, "doc")
		if doc == "" {
			continue
		}
		viewBytes, ok := readSourceFile(repo, path.Join(planDir, doc), sourceName)
		if !ok || !IsGeneratedView(string(viewBytes)) {
			continue
		}
		viewArt := parseArtifactBytes(viewBytes, "", "")
		if viewArt == nil || viewArt.Meta == nil {
			continue
		}
		if metaStr(viewArt.Meta, "plan") != planName {
			continue
		}
		if metaStr(viewArt.Meta, "phase") != phaseID {
			continue
		}
		return true
	}
	return false
}

// checkRetainedIDs ports _check_retained_ids. excused marks a phase document
// whose removal was recognized as a deliberate graph conversion; in that case
// both SDD164 (disappeared/type-changed) and SDD156 (task id removed) are
// suppressed, because the graph accounts for the document's task ids.
func checkRetainedIDs(kind, baseline, current, rel, sourceName string, excused bool) []appendOnlyFinding {
	var out []appendOnlyFinding
	if excused {
		return out
	}

	currentArtifact := parseArtifactBytes([]byte(current), "", "")
	if currentArtifact == nil || currentArtifact.Meta == nil ||
		metaStr(currentArtifact.Meta, "type") != kind {
		out = append(out, appendOnlyFinding{
			Code: "SDD164", Path: rel,
			Message:    "Previously tracked `" + kind + "` artifact changed type or disappeared from the " + sourceName + ".",
			Correction: "Restore the artifact as `type: " + kind + "` at its tracked path before moving or superseding it.",
		})
	}

	check := retainedIDsFor(kind, baseline, current)
	var removed []string
	for id := range check.prior {
		if !check.retained[id] {
			removed = append(removed, id)
		}
	}
	sortStrings(removed)
	for _, id := range removed {
		out = append(out, appendOnlyFinding{
			Code: check.code, Path: rel,
			Message:    "Previously tracked " + check.noun + " id `" + id + "` was removed from the " + sourceName + ".",
			Correction: check.correction,
		})
	}
	return out
}

// appendOnlyCheckRoot builds a CheckRoot that runs the one shared scan and
// keeps only the given code, the same shape the evidence family uses.
func appendOnlyCheckRoot(code string) func(*Root, func(Diagnostic)) {
	return func(r *Root, emit func(Diagnostic)) {
		for _, f := range appendOnlyHistory(r) {
			if f.Code != code {
				continue
			}
			emit(Diagnostic{
				Code: f.Code, Severity: Error, Path: f.Path, Line: 1,
				Message: f.Message, Correction: f.Correction,
			})
		}
	}
}

var errOutsideRoot = errors.New("path is outside the root")

// relativeWithin returns target relative to base, or an error when target is
// not inside base. "." when they are the same directory.
func relativeWithin(base, target string) (string, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errOutsideRoot
	}
	return rel, nil
}

// appendOnlySetup commits everything, so HEAD carries the baseline these
// rules compare against. A Bad example then perturbs the worktree or index
// after this runs.
var appendOnlySetup = [][]string{
	{"git", "init", "-q"},
	{"git", "add", "."},
	{"git", "commit", "-q", "-m", "baseline"},
}

// afterCommit appends steps that run once the baseline exists.
func afterCommit(steps ...[]string) [][]string {
	out := append([][]string{}, appendOnlySetup...)
	return append(out, steps...)
}

func init() {
	Register(&Rule{
		Code: "SDD154", Severity: Error, PyFunc: "_check_retained_ids",
		What:      "a previously tracked spec element id was removed",
		CheckRoot: appendOnlyCheckRoot("SDD154"),
		Bad: []Example{{
			// Committed with FR-01 defined, then the whole spec is deleted
			// from the worktree, so every previously tracked element is gone.
			Name:  "spec-elements-removed",
			Files: map[string]string{"Specs/Sample/README.md": validSpecTemplate},
			Setup: afterCommit([]string{"git", "rm", "-q", "Specs/Sample/README.md"}),
		}},
		Good: []Example{{
			Name:  "spec-elements-retained",
			Files: map[string]string{"Specs/Sample/README.md": validSpecTemplate},
			Setup: appendOnlySetup,
		}},
	})

	Register(&Rule{
		Code: "SDD155", Severity: Error, PyFunc: "_check_retained_ids",
		What:      "a previously tracked plan phase id was removed",
		CheckRoot: appendOnlyCheckRoot("SDD155"),
		Bad: []Example{{
			// validPlan declares `phases: []`, so it has no phase id to lose;
			// this needs a plan that actually declares one.
			Name: "phase-ids-removed",
			Files: map[string]string{"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
`)},
			Setup: afterCommit([]string{"git", "rm", "-q", "Plans/Sample/README.md"}),
		}},
		Good: []Example{{
			Name: "phase-ids-retained",
			Files: map[string]string{"Plans/Sample/README.md": planWithPhasesRaw(`phases:
  - id: "1"
    title: One
    status: planned
    doc: 01-One.md
`)},
			Setup: appendOnlySetup,
		}},
	})

	Register(&Rule{
		Code: "SDD156", Severity: Error, PyFunc: "_check_retained_ids",
		What:      "a previously tracked phase task id was removed",
		CheckRoot: appendOnlyCheckRoot("SDD156"),
		Bad: []Example{
			{
				Name: "task-ids-removed",
				Files: map[string]string{
					"Plans/Sample/README.md": validPlan(false),
					"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`, false, true),
				},
				Setup: afterCommit([]string{"git", "rm", "-q", "Plans/Sample/01-One.md"}),
			},
			// A graph conversion is the sanctioned way to remove a v1 phase;
			// each of these perturbs one requirement of that exemption, so the
			// task ids must still be reported as removed.
			{
				Name: "conversion-unaccounted-task",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-missing-graph",
				Files: map[string]string{
					"Plans/Sample/README.md":  planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":  v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md": generatedPhaseView("Sample", "1", "One"),
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-malformed-graph",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": `{"version":1,"nodes":[`,
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-empty-graph",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": `{"version":1,"seq_counter":0,"nodes":[]}`,
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-missing-generated-view",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        phaseDoc("Sample", "1", "One", "planned"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-foreign-view",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Other", "1", "One"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
		},
		Good: []Example{
			{
				Name: "task-ids-retained",
				Files: map[string]string{
					"Plans/Sample/README.md": validPlan(false),
					"Plans/Sample/01-One.md": phaseWithTasks("1", "Sample", `
  - id: "1.1"
    title: First
    status: planned
    verification: x
    justifies: FR-01
`, false, true),
				},
				Setup: appendOnlySetup,
			},
			{
				// A deliberate graph conversion removes the v1 phase document
				// while its task ids survive as graph nodes — no task id was
				// lost, so SDD156 stays quiet.
				Name: "converted-phase-excused",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
		},
	})

	Register(&Rule{
		Code: "SDD164", Severity: Error, PyFunc: "_check_retained_ids",
		What:      "a previously tracked artifact changed type or disappeared",
		CheckRoot: appendOnlyCheckRoot("SDD164"),
		Bad: []Example{
			{
				Name:  "artifact-deleted",
				Files: map[string]string{"Specs/Sample/README.md": validSpecTemplate},
				Setup: afterCommit([]string{"git", "rm", "-q", "Specs/Sample/README.md"}),
			},
			{
				Name: "conversion-unaccounted-task",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-missing-graph",
				Files: map[string]string{
					"Plans/Sample/README.md":  planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":  v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md": generatedPhaseView("Sample", "1", "One"),
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-malformed-graph",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": `{"version":1,"nodes":[`,
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-empty-graph",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": `{"version":1,"seq_counter":0,"nodes":[]}`,
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-missing-generated-view",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        phaseDoc("Sample", "1", "One", "planned"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
			{
				Name: "conversion-foreign-view",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Other", "1", "One"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
		},
		Good: []Example{
			{
				Name:  "artifact-retained",
				Files: map[string]string{"Specs/Sample/README.md": validSpecTemplate},
				Setup: appendOnlySetup,
			},
			{
				// The same deliberate conversion as SDD156's Good example: the
				// v1 phase document disappears, but the graph accounts for it,
				// so SDD164 stays quiet too.
				Name: "converted-phase-excused",
				Files: map[string]string{
					"Plans/Sample/README.md":         planReadmeWithPhaseDoc("01-core.md"),
					"Plans/Sample/01-One.md":         v1PhaseWithTasks("Sample", "1", "1.1", "1.2"),
					"Plans/Sample/01-core.md":        generatedPhaseView("Sample", "1", "One"),
					"Plans/Sample/Sample-Graph.json": graphPlanJSON([]string{"task-1-1", "task-1-2"}, nil),
				},
				Setup: conversionRemoveSetup(),
			},
		},
	})
}
