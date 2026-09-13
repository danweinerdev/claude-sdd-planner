package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hostileHost builds a fake workstation configuration under test ownership:
// a system and a global gitconfig that both enable a fsmonitor and route
// hooks at a directory whose pre-commit hook touches a sentinel file, plus
// GIT_CONFIG_COUNT injection of the same hooks path. It never inspects or
// modifies the real user's configuration.
// The control environment carries only the hooks injection: a commit under
// the full hostile configuration cannot even succeed (gpg signing, a
// fsmonitor daemon), which is the point of the policy but makes a poor
// control for "the sentinel is live".
func hostileHost(t *testing.T) (env, control []string, sentinel string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook sentinel needs a POSIX sh; Windows evidence is the follow-on plan's scope")
	}
	host := t.TempDir()
	hooks := filepath.Join(host, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel = filepath.Join(host, "sentinel")
	hook := "#!/bin/sh\ntouch " + sentinel + "\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\tfsmonitor = true\n\thooksPath = " + hooks + "\n[commit]\n\tgpgsign = true\n"
	global := filepath.Join(host, "gitconfig")
	system := filepath.Join(host, "system-gitconfig")
	for _, p := range []string{global, system} {
		if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(host, "home")
	if err := os.MkdirAll(filepath.Join(home, ".config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	control = append(scrubGitVars(os.Environ()),
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0="+hooks,
	)
	env = append(scrubGitVars(os.Environ()),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_GLOBAL="+global,
		"GIT_CONFIG_SYSTEM="+system,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0="+hooks,
	)
	return env, control, sentinel
}

func scrubGitVars(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") || k == "HOME" || k == "XDG_CONFIG_HOME" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func git(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitOnce(t *testing.T, env []string) string {
	t.Helper()
	repo := t.TempDir()
	git(t, env, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, env, repo, "add", "f")
	git(t, env, repo, "-c", "user.name=ctl", "-c", "user.email=ctl@example.com", "commit", "-q", "-m", "m")
	return repo
}

// FR-01, FR-02: under a hostile host configuration the policy environment
// leaves the sentinel untouched, disables fsmonitor and signing, and keeps
// fixture identity explicit; the same commands with the raw hostile
// environment prove the sentinel is live.
func TestHermeticGitPolicy(t *testing.T) {
	hostile, control, sentinel := hostileHost(t)

	commitOnce(t, control)
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("control: hostile hooks path did not fire the sentinel (%v); the fixture proves nothing", err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}

	p, cleanup, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	env := p.Env(hostile)

	repo := commitOnce(t, env)
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("hostile hooks path fired under the hermetic policy")
	}
	if v := git(t, env, repo, "config", "core.fsmonitor"); v != "false" {
		t.Errorf("core.fsmonitor = %q under policy, want false", v)
	}
	if v, _ := gitMaybe(env, repo, "config", "commit.gpgsign"); v != "false" && v != "" {
		t.Errorf("commit.gpgsign = %q under policy, want false or unset", v)
	}
	if v := git(t, env, repo, "log", "-1", "--format=%an <%ae> %aI"); v != "sdd-fixture <sdd-fixture@example.com> 2024-01-01T00:00:00Z" {
		t.Errorf("author identity/time under policy = %q, want the fixed fixture identity", v)
	}
	if v := git(t, env, repo, "symbolic-ref", "--short", "HEAD"); v != "master" {
		t.Errorf("initial branch = %q under policy, want the explicit fixture default master", v)
	}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_PARAMETERS", "GIT_DIR", "GIT_WORK_TREE":
			t.Errorf("policy environment still carries %s=%s", k, v)
		case "PATH":
			if v == "" {
				t.Error("policy environment dropped PATH")
			}
		}
	}

	// Install applies the same policy to the process environment, so code
	// under test that inherits os.Environ() is covered without importing
	// this package; cleanup restores the previous values.
	if got := os.Getenv("GIT_CONFIG_GLOBAL"); got != p.GlobalConfig() {
		t.Errorf("Install: GIT_CONFIG_GLOBAL = %q, want %q", got, p.GlobalConfig())
	}
	cleanup()
	if got := os.Getenv("GIT_CONFIG_GLOBAL"); got == p.GlobalConfig() {
		t.Error("cleanup did not restore GIT_CONFIG_GLOBAL")
	}
}

func gitMaybe(env []string, dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// FR-03: an explicit, intentional fixture setting still takes effect under
// the policy, so hermeticity cannot hide configuration behavior a test
// means to exercise.
func TestIntentionalGitConfig(t *testing.T) {
	hostile, _, sentinel := hostileHost(t)
	p, cleanup, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	env := p.Env(hostile)

	repo := t.TempDir()
	git(t, env, repo, "init", "-q")
	hooks := filepath.Dir(sentinel) + "/hooks"
	git(t, env, repo, "config", "core.hooksPath", hooks)
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, env, repo, "add", "f")
	git(t, env, repo, "commit", "-q", "-m", "m")
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("explicit repo-local hooksPath did not take effect under the policy: %v", err)
	}
	if v := git(t, env, repo, "log", "-1", "--format=%an"); v != "sdd-fixture" {
		t.Errorf("author = %q, want sdd-fixture", v)
	}
}

// FR-02: null and config paths are platform-correct; nothing assumes
// /dev/null exists.
func TestNullConfigPathIsPlatformCorrect(t *testing.T) {
	p, cleanup, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	env := p.Env(nil)
	var system, global, nosys string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "GIT_CONFIG_SYSTEM":
			system = v
		case "GIT_CONFIG_GLOBAL":
			global = v
		case "GIT_CONFIG_NOSYSTEM":
			nosys = v
		}
	}
	if system != os.DevNull {
		t.Errorf("GIT_CONFIG_SYSTEM = %q, want the platform null device %q", system, os.DevNull)
	}
	if nosys != "1" {
		t.Errorf("GIT_CONFIG_NOSYSTEM = %q, want 1", nosys)
	}
	if global == "" || global == os.DevNull || !filepath.IsAbs(global) {
		t.Errorf("GIT_CONFIG_GLOBAL = %q, want an absolute policy-owned file", global)
	}
	if _, err := os.Stat(global); err != nil {
		t.Errorf("policy global config missing: %v", err)
	}
	if strings.Contains(strings.Join(env, "\n"), "/dev/null") && os.DevNull != "/dev/null" {
		t.Error("policy hard-codes /dev/null on a platform whose null device differs")
	}
}
