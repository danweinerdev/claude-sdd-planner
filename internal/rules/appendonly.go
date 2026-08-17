package rules

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// appendOnlyFinding is one diagnostic this family produces, before it is
// filtered to a single code.
type appendOnlyFinding struct {
	Code       string
	Path       string
	Message    string
	Correction string
}

// appendOnlyHistory ports _append_only_repository_history. It returns every
// finding across all four codes; each registered rule keeps only its own, so
// the rules agree on exactly what one scan found.
func appendOnlyHistory(r *Root) []appendOnlyFinding {
	var out []appendOnlyFinding
	repo := vcs.Detect(r.Dir)
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
			out = append(out, checkRetainedIDs(kind, baseline, content, artifactRelative, source.name)...)
		}
	}
	return out
}

// checkRetainedIDs ports _check_retained_ids.
func checkRetainedIDs(kind, baseline, current, rel, sourceName string) []appendOnlyFinding {
	var out []appendOnlyFinding

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
		Bad: []Example{{
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
		}},
		Good: []Example{{
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
		}},
	})

	Register(&Rule{
		Code: "SDD164", Severity: Error, PyFunc: "_check_retained_ids",
		What:      "a previously tracked artifact changed type or disappeared",
		CheckRoot: appendOnlyCheckRoot("SDD164"),
		Bad: []Example{{
			Name:  "artifact-deleted",
			Files: map[string]string{"Specs/Sample/README.md": validSpecTemplate},
			Setup: afterCommit([]string{"git", "rm", "-q", "Specs/Sample/README.md"}),
		}},
		Good: []Example{{
			Name:  "artifact-retained",
			Files: map[string]string{"Specs/Sample/README.md": validSpecTemplate},
			Setup: appendOnlySetup,
		}},
	})
}
