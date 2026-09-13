package rules

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/testenv"
)

// hostileGitHost builds a fake workstation configuration under test
// ownership: system and global gitconfigs, a HOME with its own .gitconfig,
// and GIT_CONFIG_COUNT injection, all pointing core.fsmonitor at a
// hook-style script that touches a sentinel file whenever git consults the
// monitor (git status does). It never reads the real user's configuration.
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

const hermeticChildEnv = "RULES_HERMETIC_CHILD_FIXTURE"

// FR-01 / FR-03 / AC-01: in-process validation under a hostile ambient
// configuration leaves the sentinel untouched and uses only the fixture
// repository. The subject is the process boundary, so the validation runs
// in a child test process whose TestMain installs the policy over the
// hostile environment; this parent proves the sentinel is live by running
// git directly under that environment.
func TestInProcessValidationHermetic(t *testing.T) {
	hostile, sentinel := hostileGitHost(t)
	p := appendOnlyGoodFixture(t)

	// Child: the real package TestMain installs the policy, then the child
	// case loads the root, evaluates it, and asks the adapter for the
	// worktree state (git status) — the call a hostile fsmonitor hooks.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestHermeticChildCase$", "-test.v", "-test.count=1")
	child.Env = append(append([]string{}, hostile...), hermeticChildEnv+"="+p.dir)
	out, err := child.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: TestHermeticChildCase") {
		t.Fatalf("child validation process failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("hostile fsmonitor hook fired during in-process validation under the policy")
	}

	// Control: git run directly under the hostile environment does fire it.
	ctl := exec.Command("git", "-C", p.dir, "status", "--porcelain")
	ctl.Env = hostile
	_ = ctl.Run()
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("control: hostile fsmonitor hook did not fire (%v); the fixture proves nothing", err)
	}
}

// TestHermeticChildCase is the body TestInProcessValidationHermetic runs in
// a child process; it is a no-op in an ordinary run.
func TestHermeticChildCase(t *testing.T) {
	dir := os.Getenv(hermeticChildEnv)
	if dir == "" {
		t.Skip("child case; driven by TestInProcessValidationHermetic")
	}
	if got := os.Getenv("GIT_CONFIG_COUNT"); got != "" {
		t.Fatalf("policy left GIT_CONFIG_COUNT=%q in the process environment", got)
	}
	if got := os.Getenv("HOME"); strings.Contains(got, "hostile") || got == "" {
		t.Fatalf("HOME=%q is not policy-owned", got)
	}
	root := freshRoot(t, dir)
	if _, err := RunChecked(root); err != nil {
		t.Fatalf("evaluation: %v", err)
	}
	if clean, _, err := root.Repo(root.Dir).Clean(); err != nil || !clean {
		t.Fatalf("Clean() = %v, %v; want a clean fixture", clean, err)
	}
	_ = testenv.FixtureName
}

// FR-01 / AC-01 (review F-01): fixture setup commands inherit the policy's
// global configuration instead of overriding it.
//
// The observable is what a Setup command itself sees, not what a later probe
// sees: the policy's pins (fsmonitor, signing, gc, maintenance) govern git's
// behavior while the command runs and are never persisted into the fixture's
// .git/config, so reading the fixture's config afterwards under the process
// environment reports the policy either way and would prove nothing. This
// test therefore runs the introspection through runSetup — the same env
// composition every fixture Setup command uses — and asserts the pins are in
// effect there. The identity assertions pin the other half of the contract:
// dropping the config override must not disturb the fixed author and date
// the hard-coded fixture SHAs depend on.
func TestFixtureSetupInheritsPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the config introspection probe is a POSIX shell command")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh unavailable: %v", err)
	}

	dir := t.TempDir()
	const seen = "seen-config.txt"
	setup := [][]string{
		{"git", "init", "-q"},
		{"sh", "-c", "git config --get core.fsmonitor > " + seen + "; git config --get commit.gpgsign >> " + seen},
	}
	if err := runSetup(dir, setup, setupPolicy); err != nil {
		t.Fatalf("setup could not read the policy's pins, so they are not in effect for fixture setup: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, seen))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(b))
	if want := []string{"false", "false"}; !slices.Equal(got, want) {
		t.Errorf("a Setup command saw core.fsmonitor, commit.gpgsign = %v, want %v (the policy's pins)", got, want)
	}

	// The fixed identity still governs, so hard-coded fixture SHAs hold.
	p := appendOnlyGoodFixture(t)
	head := func(format string) string {
		t.Helper()
		out, err := exec.Command("git", "-C", p.dir, "log", "-1", "--format="+format).Output()
		if err != nil {
			t.Fatalf("git log %s: %v", format, err)
		}
		return strings.TrimSpace(string(out))
	}
	if g := head("%an"); g != testenv.FixtureName {
		t.Errorf("HEAD author name = %q, want %q", g, testenv.FixtureName)
	}
	if g := head("%ae"); g != testenv.FixtureEmail {
		t.Errorf("HEAD author email = %q, want %q", g, testenv.FixtureEmail)
	}
	if g := head("%ad"); !strings.HasPrefix(g, "Mon Jan 1 00:00:00 2024") {
		t.Errorf("HEAD author date = %q, want the fixed fixture date 2024-01-01", g)
	}
}
