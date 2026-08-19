package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/provision"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/schema"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/version"
)

// schemaInfo is one embedded schema's diagnostic summary.
type schemaInfo struct {
	Type            string `json:"type"`
	Sections        int    `json:"sections,omitempty"`
	FrontmatterMode string `json:"frontmatter_mode,omitempty"`
	Count           int    `json:"artifact_count,omitempty"`
	CountNote       string `json:"count_note,omitempty"`
	Error           string `json:"error,omitempty"`
}

// doctorReport is the whole `sdd doctor` diagnostic surface (FR-42, adapted
// to what this spike actually has: no plugin config, no MCP servers, no
// review lanes — just the schema set, the planning root, and artifact counts).
type doctorReport struct {
	Version           string       `json:"version"`
	BinaryPath        string       `json:"binary_path,omitempty"`
	PlanningRoot      string       `json:"planning_root,omitempty"`
	PlanningRootError string       `json:"planning_root_error,omitempty"`
	Schemas           []schemaInfo `json:"schemas"`
	PluginRoot        string       `json:"plugin_root,omitempty"`
	PluginRootSource  string       `json:"plugin_root_source,omitempty"`
	HookBinary        string       `json:"hook_binary,omitempty"`
	HookBinaryError   string       `json:"hook_binary_error,omitempty"`
	HooksFile         string       `json:"hooks_file,omitempty"`
	HooksFileError    string       `json:"hooks_file_error,omitempty"`
	// GitignoreSuggestion is advice, not a finding: an unignored lock sidecar
	// is untidy, never incorrect.
	GitignoreSuggestion string `json:"gitignore_suggestion,omitempty"`
}

// cmdDoctor reports the binary's own identity, the resolved planning root (or
// why it couldn't resolve one), and every embedded schema with how many
// artifacts of that type exist under the root.
// doctorOpts controls whether doctor repairs what it finds. Repair is the
// default because that is what makes it the one command to run; --check is for
// CI and for anyone who wants a verdict without a side effect.
type doctorOpts struct {
	JSON  bool
	Check bool
}

func cmdDoctor(o doctorOpts) error {

	rep := doctorReport{Version: version.Version}
	pluginRoot, pluginSource := discoverPluginRoot()
	rep.PluginRoot, rep.PluginRootSource = pluginRoot, pluginSource
	rep.HookBinary, rep.HookBinaryError = checkHookBinary(pluginRoot, pluginSource)
	rep.HooksFile, rep.HooksFileError = checkHooksFile(pluginRoot, pluginSource, !o.Check)
	if exe, err := os.Executable(); err == nil {
		if abs, err2 := filepath.Abs(exe); err2 == nil {
			rep.BinaryPath = abs
		} else {
			rep.BinaryPath = exe
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("doctor: %w", err)
	}
	root, rootErr := store.FindPlanningRoot(wd)
	if rootErr != nil {
		rep.PlanningRootError = rootErr.Error()
	} else {
		rep.PlanningRoot = relPath(root)
		rep.GitignoreSuggestion = checkLockIgnore(root)
	}

	for _, t := range schema.Types() {
		s, err := schema.Load(t)
		if err != nil {
			rep.Schemas = append(rep.Schemas, schemaInfo{Type: t, Error: err.Error()})
			continue
		}
		info := schemaInfo{
			Type:            t,
			Sections:        len(s.Headings),
			FrontmatterMode: s.FrontmatterMode,
		}
		if rootErr == nil {
			if paths, err := store.List(root, t); err != nil {
				info.CountNote = "artifact counting not supported for this type"
			} else {
				info.Count = len(paths)
			}
		} else {
			info.CountNote = "planning root unresolved"
		}
		rep.Schemas = append(rep.Schemas, info)
	}

	if o.JSON {
		if err := writeJSON(rep); err != nil {
			return err
		}
	} else {
		printDoctorReport(rep)
	}

	if rootErr != nil {
		return fmt.Errorf("doctor: %w", rootErr)
	}
	return nil
}

func printDoctorReport(r doctorReport) {
	fmt.Printf("sdd %s\n  binary: %s\n", r.Version, r.BinaryPath)
	if r.PluginRoot != "" {
		fmt.Printf("  plugin root: %s (via %s)\n", r.PluginRoot, r.PluginRootSource)
		if r.PluginRootSource != "CLAUDE_PLUGIN_ROOT" {
			fmt.Printf("  hooks: not applicable — portable installations carry no hooks; the runtime uses `sdd` from PATH\n")
		}
	}
	// The hook binary is reported even when healthy: its absence is the one
	// failure with no other symptom, so silence here would be ambiguous.
	if r.HookBinaryError != "" {
		fmt.Printf("  hook binary: %s — %s\n", r.HookBinary, r.HookBinaryError)
	} else if r.HookBinary != "" {
		fmt.Printf("  hook binary: %s (OK)\n", r.HookBinary)
	}
	if r.HooksFileError != "" {
		fmt.Printf("  hooks file: %s — %s\n", r.HooksFile, r.HooksFileError)
	} else if r.HooksFile != "" {
		fmt.Printf("  hooks file: %s (current)\n", r.HooksFile)
	}
	if r.PlanningRootError != "" {
		fmt.Printf("  planning root: ERROR: %s\n", r.PlanningRootError)
	} else {
		fmt.Printf("  planning root: %s\n", r.PlanningRoot)
	}
	if r.GitignoreSuggestion != "" {
		fmt.Printf("  suggestion: %s\n", r.GitignoreSuggestion)
	}
	fmt.Println("  schemas:")
	for _, s := range r.Schemas {
		if s.Error != "" {
			fmt.Printf("    %-10s ERROR: %s\n", s.Type, s.Error)
			continue
		}
		count := fmt.Sprintf("%d artifacts", s.Count)
		if s.CountNote != "" {
			count = s.CountNote
		}
		fmt.Printf("    %-10s %2d sections  mode=%-8s %s\n", s.Type, s.Sections, s.FrontmatterMode, count)
	}
}

// checkHookBinary reports on ${CLAUDE_PLUGIN_ROOT}/bin/sdd — the copy the
// hook wrappers prefer.
//
// It is no longer the dead-hook state F-02 identified. The wrappers
// (hooks/sdd-hook.sh and .ps1) resolve the plugin copy first and fall back to
// PATH, so a missing copy degrades version pinning rather than killing the
// hooks. What it still catches is the case worth naming: the hooks will run
// whatever `sdd` happens to be on PATH, which may not be the version this
// plugin was admitted against.
// checkHooksFile reports on the generated hooks.json and, unless the caller
// asked for a read-only report, repairs it.
//
// Repairing here is the point of running doctor at all. A stale hooks.json is
// invisible by construction — the events it declares keep firing, so nothing
// looks wrong while a newly added event never runs — and a report that only
// tells the user to run a second command leaves the broken state in place for
// however long it takes them to do it. The write is small, idempotent, and
// entirely within the plugin's own directory.
func checkHooksFile(root, source string, repair bool) (path, problem string) {
	// Hooks exist only in the Claude Code plugin tree; a portable
	// (Codex/OpenCode) installation ships skills/ + shared/ and no hooks, so
	// there is nothing to check or repair there.
	if root == "" || source != "CLAUDE_PLUGIN_ROOT" {
		return "", ""
	}
	path = provision.HooksPath(root)
	state, err := provision.CheckHooks(root)
	if err != nil {
		return path, "cannot be read: " + err.Error()
	}
	if state == provision.HooksCurrent {
		return path, ""
	}

	was := "absent"
	if state == provision.HooksStale {
		was = "did not match this version's hook set"
	}
	if !repair {
		return path, was + "; run `sdd doctor` to regenerate it"
	}
	if _, _, err := provision.InstallHooks(root); err != nil {
		return path, was + ", and could not be regenerated: " + err.Error()
	}
	return path, was + " — regenerated for " + runtime.GOOS +
		"; restart the session for it to take effect"
}

func checkHookBinary(root, source string) (path, problem string) {
	if root == "" {
		return "", "no plugin root found (CLAUDE_PLUGIN_ROOT unset; no sdd-planner under " +
			"${CODEX_HOME:-~/.codex}/plugins/cache or ~/.agents/plugins); cannot check the hook binary path"
	}
	// A portable (Codex/OpenCode) installation carries no hooks or pinned
	// binary by design — the runtime invokes whatever `sdd` is on PATH.
	if source != "CLAUDE_PLUGIN_ROOT" {
		return "", ""
	}
	name := "sdd"
	if runtime.GOOS == "windows" {
		name = "sdd.exe"
	}
	p := filepath.Join(root, "bin", name)
	if _, err := os.Stat(p); err != nil {
		if onPath, lookErr := exec.LookPath("sdd"); lookErr == nil {
			return p, "absent — the hooks will use " + onPath +
				" from PATH instead; run `sdd provision` to pin this plugin's binary"
		}
		return p, "absent, and no `sdd` on PATH — the hooks are a silent no-op; run `sdd provision`"
	}
	if err := exec.Command(p, "version").Run(); err != nil {
		return p, "present but not executable: " + err.Error()
	}
	return p, ""
}

// discoverPluginRoot resolves the installed plugin tree, following the same
// chain shared/agent-runtime.md documents for the skills themselves (G-3):
//
//  1. CLAUDE_PLUGIN_ROOT — the Claude Code runtime sets it explicitly.
//  2. Codex's installed plugin cache:
//     ${CODEX_HOME:-$HOME/.codex}/plugins/cache/<marketplace>/<plugin>/<version>/
//  3. OpenCode's plugin directory: $HOME/.agents/plugins/<plugin>/
//
// A candidate counts only when it actually is this plugin: it must carry
// shared/agent-runtime.md and a plugin.json whose name is "sdd-planner".
// Returns the root and its provenance, or ("", "") when nothing resolves.
func discoverPluginRoot() (root, source string) {
	if r := os.Getenv("CLAUDE_PLUGIN_ROOT"); r != "" {
		return r, "CLAUDE_PLUGIN_ROOT"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if matches, _ := filepath.Glob(filepath.Join(codexHome, "plugins", "cache", "*", "*", "*")); len(matches) > 0 {
		// Highest version last in lexical glob order; prefer the newest.
		for i := len(matches) - 1; i >= 0; i-- {
			if isSddPluginRoot(matches[i]) {
				return matches[i], "codex plugin cache"
			}
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(home, ".agents", "plugins", "*")); len(matches) > 0 {
		for _, m := range matches {
			if isSddPluginRoot(m) {
				return m, "~/.agents/plugins"
			}
		}
	}
	return "", ""
}

// isSddPluginRoot applies agent-runtime.md's own root test: the sibling
// shared/ resource must exist and the manifest must name this plugin.
func isSddPluginRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "shared", "agent-runtime.md")); err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		// Legacy layout kept the manifest under .claude-plugin/.
		raw, err = os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
		if err != nil {
			return false
		}
	}
	var m struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	return m.Name == "sdd-planner"
}

// lockIgnorePattern is what a repository needs to keep the advisory lock
// sidecars out of version control.
const lockIgnorePattern = "*.sdd-lock"

// checkLockIgnore suggests ignoring the lock sidecars when the repository
// holding the planning root does not already.
//
// Reading and writing artifacts creates `.<name>.sdd-lock` files next to them.
// They carry no content, only lock state, and are recreated on demand — so
// committing one is harmless but pointless noise in every future diff.
//
// This only ever SUGGESTS. The file belongs to the user's repository, and a
// tool that edits .gitignore because it noticed something is a tool that
// edits files nobody asked it to touch. It also stays silent when the pattern
// is already covered, when there is no .gitignore to speak of, and when the
// planning root is not in a Git repository at all — advice that repeats after
// being acted on is advice people learn to skip.
func checkLockIgnore(planningRoot string) string {
	repo := planningRoot
	for {
		if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(repo)
		if parent == repo {
			return "" // not a Git repository: nothing to ignore into
		}
		repo = parent
	}

	path := filepath.Join(repo, ".gitignore")
	raw, err := os.ReadFile(path)
	if err != nil {
		// No .gitignore at all. Worth suggesting, since the sidecars will
		// otherwise show up as untracked files.
		return "add `" + lockIgnorePattern + "` to " + relPath(path) +
			" so artifact lock sidecars stay out of version control"
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == lockIgnorePattern {
			return ""
		}
	}
	return "add `" + lockIgnorePattern + "` to " + relPath(path) +
		" so artifact lock sidecars stay out of version control"
}
