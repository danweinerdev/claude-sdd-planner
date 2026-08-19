package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

// Prerequisite A (target-repo resolution): ports sdd_validate.py's
// Validator._configure_repositories/_repo_for_path/_repo_for_artifact.
//
// Evidence carries a `Repository` label that must equal the exact resolved
// target repository root for the artifact recording it (D-0008,
// shared/path-resolution.md's Target Repository chain), and the git-identity
// rules need that same directory to run their adapter operations against. A
// plan targets code in a different repository via planning-config.json's
// `planMapping`/`repositories`; every other artifact — and any plan without a
// mapping — targets the same repository the planning root's own
// planning-config.json lives beside, which is `Root.RepoRoot` here (Python's
// `self.repo`).

// configureRepositories parses repoRoot/planning-config.json's
// `planMapping`/`repositories` and returns the resolved plan-name -> absolute
// target-directory map, plus any SDD000 diagnostics. It never returns an
// error: every failure mode is itself an SDD000 finding, exactly as
// _configure_repositories reports rather than raises.
func configureRepositories(repoRoot, diagPath string) (map[string]string, []Diagnostic) {
	planRepos := map[string]string{}
	configPath := filepath.Join(repoRoot, "planning-config.json")
	info, err := os.Stat(configPath)
	if err != nil || info.IsDir() {
		return planRepos, nil
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return planRepos, []Diagnostic{sdd000(diagPath, "Cannot parse `"+configPath+"`: "+err.Error(), "Correct planning-config.json before validation.")}
	}
	var doc struct {
		PlanMapping  map[string]any `json:"planMapping"`
		Repositories map[string]any `json:"repositories"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return planRepos, []Diagnostic{sdd000(diagPath, "Cannot parse `"+configPath+"`: "+store.DescribeJSONError(raw, err), "Correct planning-config.json before validation.")}
	}
	// json.Unmarshal leaves doc.PlanMapping/Repositories nil (not an error) when
	// the key is absent or not a JSON object into which map[string]any decodes;
	// a present-but-wrong-shaped value (e.g. a JSON array) is the SDD000 case
	// Python's isinstance(..., dict) check catches, so re-decode into `any` to
	// tell "absent" from "wrong shape".
	var loose struct {
		PlanMapping  any `json:"planMapping"`
		Repositories any `json:"repositories"`
	}
	_ = json.Unmarshal(raw, &loose)
	if !jsonObjectOrAbsent(loose.PlanMapping) || !jsonObjectOrAbsent(loose.Repositories) {
		return planRepos, []Diagnostic{sdd000(diagPath, "`planMapping` and `repositories` must be JSON objects.", "Correct planning-config.json repository mapping.")}
	}

	var diags []Diagnostic
	names := make([]string, 0, len(doc.PlanMapping))
	for name := range doc.PlanMapping {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, planName := range names {
		repositoryKey, _ := doc.PlanMapping[planName].(string)
		record := doc.Repositories[repositoryKey]
		rawPath, ok := repositoryPath(record)
		if !ok {
			diags = append(diags, sdd000(diagPath, "Plan mapping `"+planName+"` does not resolve to a repository path.", "Add repositories.<key>.path for every plan mapping."))
			continue
		}
		target := rawPath
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(configPath), target)
		}
		target = filepath.Clean(target)
		if resolved, err := filepath.Abs(target); err == nil {
			target = resolved
		}
		fi, err := os.Stat(target)
		if err != nil || !fi.IsDir() {
			diags = append(diags, sdd000(diagPath, "Mapped repository `"+target+"` for plan `"+planName+"` does not exist.", "Correct the mapping or create the target repository."))
			continue
		}
		planRepos[planName] = target
	}
	return planRepos, diags
}

// repositoryPath extracts a repositories.<key> record's path: either the
// record itself is a string, or it's an object carrying a string `path`.
func repositoryPath(record any) (string, bool) {
	switch v := record.(type) {
	case string:
		return v, true
	case map[string]any:
		p, ok := v["path"].(string)
		return p, ok
	default:
		return "", false
	}
}

// jsonObjectOrAbsent reports whether a decoded JSON value is either absent
// (nil) or a JSON object (map[string]any) — the two shapes Python's
// `mappings.get(..., {})` / `isinstance(..., dict)` combination accepts.
func jsonObjectOrAbsent(v any) bool {
	if v == nil {
		return true
	}
	_, ok := v.(map[string]any)
	return ok
}

func sdd000(path, message, correction string) Diagnostic {
	return Diagnostic{Code: "SDD000", Severity: Error, Path: path, Line: 1, Message: message, Correction: correction}
}

// RepoForArtifact returns the absolute directory of the repository that owns
// the code an artifact describes: a plan mapped in planning-config.json's
// `planMapping` targets its mapped repository; every other artifact —
// including an unmapped plan — targets Root.RepoRoot.
func (r *Root) RepoForArtifact(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) >= 2 && parts[0] == "Plans" {
		if target, ok := r.PlanRepos[parts[1]]; ok {
			return target
		}
	}
	return r.RepoRoot
}

func init() {
	Register(&Rule{
		Code: "SDD000", Severity: Error, PyFunc: "_configure_repositories",
		What: "planning-config.json's repository mapping cannot be resolved",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, d := range r.ConfigDiagnostics {
				emit(d)
			}
		},
		Bad: []Example{{Name: "missing-target-repository", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
			"planning-config.json":   `{"planMapping": {"Sample": "target"}, "repositories": {"target": {"path": "/nonexistent-sdd-target-repo"}}}`,
		}}},
		Good: []Example{{Name: "no-config-file", Files: map[string]string{
			"Plans/Sample/README.md": validPlan(false),
		}}},
	})
}
