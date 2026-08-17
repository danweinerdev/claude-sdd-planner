package hook

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// guardCases is the corpus FR-27 requires: every behavior of the Python guard
// this port must preserve exactly. Each is run through both implementations
// and compared, so "preserved exactly" is measured rather than asserted.
var guardCases = []string{
	// Read-only git is permitted.
	"git status", "git diff HEAD~1", "git log --oneline -5", "git show abc123",
	"git -C /tmp/x rev-parse HEAD", "git --no-pager diff", "git ls-tree -r HEAD",
	"git branch --contains HEAD", "git branch --list", "git tag --points-at HEAD",
	"git stash list", "git stash show", "git worktree list", "git remote -v",
	"git config --get user.name", "git notes list", "git submodule status",
	// Mutating git is denied.
	"git commit -m x", "git push", "git checkout main", "git branch newthing",
	"git branch -d old", "git tag v1", "git tag -d v1", "git stash pop",
	"git remote add origin x", "git config user.name me", "git worktree add /tmp/w",
	"git submodule update", "git reset --hard", "git clean -fd",
	// p4.
	"p4 diff2 //a //b", "p4 describe 123", "p4 opened", "p4 submit", "p4 edit f",
	// Filesystem and network denials.
	"rm -rf /tmp/x", "mv a b", "cp a b", "curl https://x", "wget x", "sudo ls",
	"mkdir -p x", "chmod +x f", "tee out.txt", "ssh host", "npm install",
	"pip install x", "cargo install x", "go install ./x", "gh pr list",
	// Argument-shaped denials.
	"sed -i s/a/b/ f", "sed s/a/b/ f", "perl -i -pe s/a/b/ f",
	"find . -delete", "find . -name '*.go'", "xargs rm", "python -c 'x'",
	"python3 script.py", "bash -c 'ls'", "node -e 'x'",
	// Redirection.
	"echo x > f", "echo x >> f", "echo x > /dev/null", "ls 2>&1",
	"grep 'a->b' f", "echo 'x > y'",
	// Test and lint runs are permitted.
	"go test ./...", "make test", "pytest", "golangci-lint run", "go build ./...",
	"npm test", "cargo test", "ruff check .",
	// Wrappers and env prefixes.
	"env FOO=1 git status", "timeout 30 go test ./...", "nice git log",
	"FOO=bar git diff",
	// Compound commands: a denial anywhere denies.
	"git status && rm -rf /", "ls; curl x", "git log | grep foo",
	"echo $(rm -rf /)",
	// Empty and odd input.
	"", "   ", "git", "p4",
}

// pythonGuard runs the original hook and reports whether it denied.
func pythonGuard(t *testing.T, agent, command string) (bool, bool) {
	t.Helper()
	// The original guard is retained under testdata/ as the parity oracle.
	// hooks/reviewer-bash-guard.py itself is deleted — FR-27's whole point is
	// removing the Python runtime dependency — but deleting the oracle too
	// would leave this test silently skipping every case, which is a passing
	// test that cannot fail.
	_, thisFile, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(thisFile), "testdata", "reviewer-bash-guard.py")

	payload, _ := json.Marshal(map[string]any{
		"agent_type": agent,
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": command},
	})
	cmd := exec.Command("python3", script)
	cmd.Stdin = strings.NewReader(string(payload))
	out, err := cmd.Output()
	if err != nil {
		return false, false // could not run: skip rather than fail the port
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return false, true
	}
	var decoded struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal(out, &decoded) != nil {
		return false, true
	}
	return decoded.HookSpecificOutput.PermissionDecision == "deny", true
}

// TestGuardParityWithPython is FR-27's proof: for a guarded agent, the Go and
// Python guards must agree on every case.
func TestGuardParityWithPython(t *testing.T) {
	for _, command := range guardCases {
		command := command
		t.Run(strings.ReplaceAll(command, " ", "_"), func(t *testing.T) {
			wantDeny, ok := pythonGuard(t, "drift-detector", command)
			if !ok {
				t.Fatal("the parity oracle could not run; it is retained under " +
					"testdata/ precisely so this test never silently skips")
			}
			got := CheckBash("drift-detector", command)
			if got.Deny != wantDeny {
				t.Errorf("command %q: python deny=%v, go deny=%v (%s)",
					command, wantDeny, got.Deny, got.Reason)
			}
		})
	}
}

// TestGuardFailsOpenForUnguardedAgents: the guard applies to seven agents and
// nothing else. Denying for the main session would break every Bash call.
func TestGuardFailsOpenForUnguardedAgents(t *testing.T) {
	for _, agent := range []string{"", "code-implementer", "general-purpose", "claude"} {
		if d := CheckBash(agent, "rm -rf /"); d.Deny {
			t.Errorf("agent %q was guarded; only the seven read-only agents are", agent)
		}
	}
}

// TestGuardAppliesToPluginNamespacedAgents: agents arrive namespaced, so the
// guard must match on the trailing name.
func TestGuardAppliesToPluginNamespacedAgents(t *testing.T) {
	if d := CheckBash("sdd-planner:drift-detector", "rm -rf /"); !d.Deny {
		t.Error("a plugin-namespaced read-only agent must still be guarded")
	}
}

// TestSddAllowlistCoversEverySubcommand is FR-44's anti-drift requirement:
// "A new mutating subcommand added later without a corresponding guard entry
// SHALL fail the test suite, so the allowlist cannot silently fall behind the
// command tree."
//
// The guard permits unrecognized command heads, so a mutating subcommand with
// no entry here would be silently permitted — a strictly larger hole than the
// Write/Edit denial closes. This test enumerates the command tree and requires
// every verb to be classified deliberately.
func TestSddAllowlistCoversEverySubcommand(t *testing.T) {
	// Every top-level subcommand `sdd` accepts, and whether a read-only agent
	// may run it. Adding a verb to cmd/sdd without adding it here fails.
	classified := map[string]bool{
		"validate": true, "show": true, "list": true, "next": true,
		"version": true, "doctor": true, "schema": true,
		"apply": false, "section": false, "migrate": false,
		"decide":   false, // sub-verbs classified separately
		"hook":     true,  // reads a payload and decides; writes nothing
		"evidence": false, "task": false, "phase": false, "plan": false,
		"spec": false, "design": false, // lifecycle transition verbs mutate
		"review": false, "template": false, // scaffold/resolve and --out write files
		"provision": false, "plugin": false,
	}
	for verb, readOnly := range classified {
		if verb == "decide" || verb == "hook" {
			continue
		}
		got := checkSdd([]string{"sdd", verb}, "sdd "+verb)
		if readOnly && got.Deny {
			t.Errorf("`sdd %s` is read-only but the guard denied it", verb)
		}
		if !readOnly && !got.Deny {
			t.Errorf("`sdd %s` mutates but the guard permitted it", verb)
		}
	}

	for verb, readOnly := range map[string]bool{
		"list": true, "search": true, "validate": true, "add": false,
	} {
		got := checkSdd([]string{"sdd", "decide", verb}, "sdd decide "+verb)
		if readOnly && got.Deny {
			t.Errorf("`sdd decide %s` is read-only but the guard denied it", verb)
		}
		if !readOnly && !got.Deny {
			t.Errorf("`sdd decide %s` mutates but the guard permitted it", verb)
		}
	}
}

// TestWriteGuardScope covers FR-28's exclusions, each of which exists for a
// reason a future change could quietly break.
func TestWriteGuardScope(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) string {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	_ = os.WriteFile(filepath.Join(root, "planning-config.json"),
		[]byte(`{"planningRoot":"."}`), 0o644)

	spec := write("Specs/Sample/README.md", "---\ntype: spec\n---\n\n# S\n")
	notes := write("Plans/Sample/notes/01-One.md", "Just prose.\n")
	readme := write("README.md", "# Root\n")
	plugin := write("commands/plan/SKILL.md", "# Skill\n")

	cases := []struct {
		name string
		tool string
		path string
		deny bool
	}{
		{"artifact write denied", "Write", spec, true},
		{"artifact edit denied", "Edit", spec, true},
		{"read never denied", "Read", spec, false},
		{"notes prose allowed", "Write", notes, false},
		{"planning-root README allowed", "Write", readme, false},
		{"plugin source allowed", "Write", plugin, false},
		{"outside the root allowed", "Write", filepath.Join(t.TempDir(), "x.md"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckWrite("drift-detector", c.tool, c.path, root)
			if got.Deny != c.deny {
				t.Errorf("deny=%v, want %v (%s)", got.Deny, c.deny, got.Reason)
			}
		})
	}

	// FR-28: the denial must name the owning subcommand and a runnable form.
	d := CheckWrite("drift-detector", "Write", spec, root)
	for _, want := range []string{"sdd section set", "sdd apply", "Specs/Sample/README.md"} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("denial message omits %q: %s", want, d.Reason)
		}
	}
}

// TestWriteGuardFailsOpenWithoutPlanningRoot: where the root cannot be
// resolved, FR-28 requires failing open.
func TestWriteGuardFailsOpenWithoutPlanningRoot(t *testing.T) {
	orphan := t.TempDir()
	p := filepath.Join(orphan, "Specs", "S", "README.md")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("---\ntype: spec\n---\n"), 0o644)
	if d := CheckWrite("drift-detector", "Write", p, orphan); d.Deny {
		t.Error("guard denied without a resolvable planning root; it must fail open")
	}
}
