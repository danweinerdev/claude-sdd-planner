package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testenv"
)

// hostileGitHost mirrors internal/rules' fixture: a fake workstation whose
// system, global, HOME and injected configuration all point core.fsmonitor
// at a hook-style script that touches a sentinel whenever git consults the
// monitor (git status does). Test-owned; never the real user's config.
func hostileGitHost(t *testing.T) (env []string, sentinel string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook-style fsmonitor sentinel needs a POSIX sh; Windows evidence is the follow-on plan's scope")
	}
	host := t.TempDir()
	sentinel = filepath.Join(host, "sentinel")
	hook := filepath.Join(host, "fsmonitor-hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+sentinel+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\tfsmonitor = " + hook + "\n"
	home := filepath.Join(host, "home")
	if err := os.MkdirAll(filepath.Join(home, ".config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(host, "system"), filepath.Join(host, "global"), filepath.Join(home, ".gitconfig"), filepath.Join(home, ".config", "git", "config")} {
		if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") || k == "HOME" || k == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_SYSTEM="+filepath.Join(host, "system"),
		"GIT_CONFIG_GLOBAL="+filepath.Join(host, "global"),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.fsmonitor",
		"GIT_CONFIG_VALUE_0="+hook,
	)
	return env, sentinel
}

// materialize writes a registered rule example and runs its Setup under
// env, the way the rules harness does, so the fixture's committed identity
// is the one the example hard-codes.
func materialize(t *testing.T, ex rules.Example, env []string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range ex.Files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.ReplaceAll(content, "{{REPO}}", dir)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range ex.Setup {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// AC-01 (user-entrypoint): a child `sdd validate` process run under an
// explicit policy environment, against a root whose phase gate consults the
// worktree state (SDD173 runs git status), leaves the hostile sentinel
// untouched and uses only the fixture repository; git run directly under
// the hostile environment proves the sentinel is live.
func TestChildValidationHermetic(t *testing.T) {
	bin := stressBinary(t)
	hostile, sentinel := hostileGitHost(t)
	policy, cleanup, err := testenv.Install()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	env := policy.Env(hostile)

	var ex *rules.Example
	for _, r := range rules.All() {
		if r.Code != "SDD173" {
			continue
		}
		for i := range r.Bad {
			if r.Bad[i].Name == "dirty-target-after-review" {
				ex = &r.Bad[i]
			}
		}
	}
	if ex == nil {
		t.Fatal("SDD173 example dirty-target-after-review not registered")
	}
	root := materialize(t, *ex, env)

	cmd := exec.Command(bin, "validate", "--root", root)
	cmd.Dir, cmd.Env = root, env
	out, runErr := cmd.CombinedOutput()
	if !strings.Contains(string(out), "SDD173") {
		t.Fatalf("child validate did not reach the phase gate that consults git status (%v):\n%s", runErr, out)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("hostile fsmonitor hook fired inside the child sdd process under the policy environment")
	}

	ctl := exec.Command("git", "-C", root, "status", "--porcelain")
	ctl.Env = hostile
	_ = ctl.Run()
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("control: hostile fsmonitor hook did not fire (%v); the fixture proves nothing", err)
	}
}
