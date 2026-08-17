package hook

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/artifact"
)

// FR-28: a read-only agent may not Write or Edit a schema-recognized artifact
// under the resolved planning root.
//
// The scope is deliberately narrow, and each exclusion is load-bearing:
//
//   - Read is never denied. The reviewers' whole job is reading.
//   - Non-artifact files inside the planning root (notes/, a root README) are
//     not artifacts and carry no schema, so nothing is protected by refusing
//     them.
//   - Plugin source in the plugin's own repository is excluded, because the
//     agents are sometimes pointed at this repository as their target and
//     denying edits to commands/ or agents/ would block ordinary work on the
//     plugin itself.
//   - Where the planning root cannot be resolved confidently the guard fails
//     open, for the same reason the Bash guard does.

// pluginSourceDirs are this plugin's own source trees, excluded from FR-28.
var pluginSourceDirs = map[string]bool{
	"commands": true, "agents": true, "shared": true,
	"scripts": true, "hooks": true, "internal": true, "cmd": true, "tools": true,
}

// artifactDirs mirror the directories the validator walks.
var artifactDirs = map[string]bool{
	"Research": true, "Brainstorm": true, "Specs": true, "Designs": true,
	"Plans": true, "Decisions": true, "Retro": true, "Diagrams": true,
}

// CheckWrite returns the verdict for a Write or Edit on path by agent.
func CheckWrite(agent, tool, path, projectDir string) Decision {
	name := agent
	if i := strings.LastIndex(agent, ":"); i >= 0 {
		name = agent[i+1:]
	}
	if !readOnlyAgents[name] {
		return Decision{}
	}
	if tool != "Write" && tool != "Edit" && tool != "NotebookEdit" {
		return Decision{} // Read is never denied
	}
	if path == "" {
		return Decision{}
	}

	root := resolvePlanningRoot(projectDir)
	if root == "" {
		return Decision{} // cannot resolve confidently: fail open
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Decision{}
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return Decision{} // outside the planning root
	}
	rel = filepath.ToSlash(rel)

	parts := strings.Split(rel, "/")
	if len(parts) == 0 || pluginSourceDirs[parts[0]] {
		return Decision{}
	}
	if !artifactDirs[parts[0]] || !strings.HasSuffix(rel, ".md") {
		return Decision{} // not a schema-recognized artifact path
	}
	// A file that carries no artifact type is not schema-recognized; notes/
	// and other prose under an artifact directory stay writable.
	kind := artifactKind(abs)
	if kind == "" {
		return Decision{}
	}

	return Decision{
		Deny: true,
		Reason: "Blocked " + tool + " on `" + rel + "`: it is a schema-recognized `" +
			kind + "` artifact, which only the sdd CLI may write. " +
			"Use `sdd section set " + rel + " --heading \"## <section>\"` to change one " +
			"section, or `sdd apply " + rel + "` to recompile it. You are a read-only " +
			"sdd-planner reviewer: report what you found instead of changing it.",
	}
}

// artifactKind returns a file's declared frontmatter type, or "".
func artifactKind(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "" // absent: a new file under an artifact dir is not yet an artifact
	}
	doc := artifact.Parse(string(raw))
	if doc == nil {
		return ""
	}
	kind, _ := doc.FM("type")
	return strings.Trim(kind, `"'`)
}

// resolvePlanningRoot walks up from projectDir for planning-config.json and
// returns the planning root it names, or "" when none is found.
func resolvePlanningRoot(projectDir string) string {
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	dir, err := filepath.Abs(projectDir)
	if err != nil {
		return ""
	}
	for {
		cfg := filepath.Join(dir, "planning-config.json")
		if raw, err := os.ReadFile(cfg); err == nil {
			root := planningRootFrom(raw)
			if root == "" {
				root = "."
			}
			if !filepath.IsAbs(root) {
				root = filepath.Join(dir, root)
			}
			return root
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func planningRootFrom(raw []byte) string {
	var parsed struct {
		PlanningRoot string `json:"planningRoot"`
	}
	if jsonUnmarshal(raw, &parsed) != nil {
		return ""
	}
	return parsed.PlanningRoot
}
