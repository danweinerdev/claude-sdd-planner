package rules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/graph/model"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/vcs"
)

// VerifyRetirementSource resolves historical content in the Git repository
// containing the plan, not the plan's possibly different implementation repo.
// No fetch, ref creation, checkout or worktree read substitutes for history.
func VerifyRetirementSource(planDir string, source model.RetirementSource) (model.RetirementSource, error) {
	if source.VCS != "git" || source.Revision == "" || source.SourceID == "" {
		return source, fmt.Errorf("historical source requires git, a revision and a source id")
	}
	if source.Path == "" || path.IsAbs(source.Path) || path.Clean(source.Path) != source.Path ||
		source.Path == "." || source.Path == ".." || strings.HasPrefix(source.Path, "../") ||
		strings.ContainsAny(source.Path, "\\\x00") || (len(source.Path) > 1 && source.Path[1] == ':') {
		return source, fmt.Errorf("historical source path %q must be canonical and repository-relative", source.Path)
	}
	repo, err := vcs.DetectChecked(planDir)
	if err != nil {
		return source, fmt.Errorf("historical Git source cannot be verified: %w", err)
	}
	if repo.Kind() != vcs.Git && repo.Kind() != vcs.GitWorktree && repo.Kind() != vcs.GitBare {
		return source, fmt.Errorf("historical Git source cannot be verified: no Git repository contains the plan")
	}
	if !repo.RevisionSyntaxValid(source.Revision) {
		res, err := procexec.Run(context.Background(), "git",
			[]string{"-C", repo.Root(), "rev-parse", "--verify", "--end-of-options", source.Revision + "^{commit}"}, procexec.Policy{})
		if err != nil {
			var pe *procexec.Error
			if errors.As(err, &pe) && pe.Cause == procexec.CauseExit {
				return source, fmt.Errorf("historical revision %q is unavailable locally; supply/fetch the original history: %w", source.Revision, err)
			}
			return source, fmt.Errorf("historical revision %q cannot be resolved: %w: %v", source.Revision, vcs.ErrOperational, err)
		}
		source.Revision = strings.TrimSpace(string(res.Stdout))
	}
	if !repo.RevisionSyntaxValid(source.Revision) {
		return source, fmt.Errorf("historical revision did not resolve to an immutable Git commit")
	}
	source.Revision = strings.ToLower(source.Revision)
	if exists, err := repo.RevisionExists(source.Revision); err != nil || !exists {
		return source, fmt.Errorf("historical commit %s is unavailable locally (history may need fetching): %v", source.Revision, err)
	}
	raw, err := repo.FileAt(source.Revision, source.Path)
	if err != nil {
		return source, fmt.Errorf("historical file %q at %s cannot be read: %w", source.Path, source.Revision, err)
	}
	if strings.HasSuffix(source.Path, ".json") {
		g, err := model.DecodeGraph(raw)
		if err == nil {
			if g.NodeByID(source.SourceID) != nil {
				return source, nil
			}
			for _, id := range g.Retired {
				if id == source.SourceID {
					return source, nil
				}
			}
		}
	} else {
		for _, field := range []string{"tasks", "phases"} {
			if frontmatterEntryIDs(string(raw), field)[source.SourceID] {
				return source, nil
			}
		}
		if a := ParseArtifactBytes(raw, source.Path); a != nil {
			for _, family := range IdentifierFamilies() {
				if DefinedIdentifiers(a, family)[source.SourceID] {
					return source, nil
				}
			}
		}
	}
	return source, fmt.Errorf("id %q is not declared in historical file %q at %s", source.SourceID, source.Path, source.Revision)
}

// RetirementProblems is shared by compile, audit and ordinary validation.
// A historical reference is never treated as a live input fingerprint.
func RetirementProblems(planDir string, g *model.Graph) []string {
	retired := map[string]bool{}
	for _, id := range g.Retired {
		retired[id] = true
	}
	var problems []string
	var ids []string
	for id := range g.RetirementSources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		record := g.RetirementSources[id]
		if !retired[id] || g.NodeByID(id) != nil {
			problems = append(problems, fmt.Sprintf("retirement source %q does not name a retired, non-live id", id))
		}
		resolved, err := VerifyRetirementSource(planDir, record.Source)
		if err != nil {
			problems = append(problems, fmt.Sprintf("retirement %s: %v", id, err))
		} else if resolved.Revision != record.Source.Revision {
			problems = append(problems, fmt.Sprintf("retirement %s must pin the full commit %s, not a movable revision", id, resolved.Revision))
		}
		for _, replacement := range record.ReplacedBy {
			if replacement == id || (!retired[replacement] && g.NodeByID(replacement) == nil) {
				problems = append(problems, fmt.Sprintf("retirement %s has invalid replacement %q", id, replacement))
			}
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		if visiting[id] {
			return true
		}
		if done[id] {
			return false
		}
		visiting[id] = true
		for _, replacement := range g.RetirementSources[id].ReplacedBy {
			if walk(replacement) {
				return true
			}
		}
		visiting[id] = false
		done[id] = true
		return false
	}
	for _, id := range ids {
		if walk(id) {
			problems = append(problems, "retirement replacement chain contains a cycle")
			break
		}
	}
	return problems
}

func init() {
	Register(&Rule{Code: "SDD181", Severity: Error, Native: true,
		What: "a historical graph retirement cannot be verified",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, plan := range r.Artifacts {
				if plan.Kind() != "plan" {
					continue
				}
				dir := filepath.Dir(plan.AbsPath)
				raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(dir)+"-Graph.json"))
				if err != nil {
					continue
				} // Graph readability is checked by SDD180.
				g, err := model.DecodeGraph(raw)
				if err != nil {
					continue
				}
				for _, problem := range RetirementProblems(dir, g) {
					emit(Diagnostic{Code: "SDD181", Severity: Error, Path: plan.Rel, Line: 1, Message: problem,
						Correction: "Restore access to the pinned history or record valid provenance through graph retire; never infer success from unavailable Git objects."})
				}
			}
		},
		Good: []Example{{Name: "no-history-assertion", Files: map[string]string{"Plans/P/README.md": validPlan(false)}}},
		Bad: []Example{{Name: "unverifiable-retirement", Files: map[string]string{
			"Plans/P/README.md":    validPlan(false),
			"Plans/P/P-Graph.json": `{"version":1,"nodes":[],"retired":["old"],"retirement_sources":{"old":{"source":{"vcs":"git","revision":"0000000000000000000000000000000000000000","path":"old.md","source_id":"1.1"}}}}`,
		}}},
	})
}
