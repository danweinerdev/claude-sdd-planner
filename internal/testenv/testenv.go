// Package testenv owns the hermetic Git environment for this repository's
// tests (Designs/TestSuiteReliability DD-1). Test-owned Git activity, whether
// launched by fixture setup, by validator code under test in-process, or by a
// child sdd process, runs under one explicit policy that excludes the
// workstation's configuration and cannot start background services.
//
// Production code never imports this package. Code under test inherits the
// policy because Install applies it to the process environment before the
// package's tests run; child processes receive it through Env.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The fixed fixture identity every test-owned commit is made under, so a
// commit's SHA is reproducible across machines and can be hard-coded into
// fixtures (the same values internal/rules' examples have always used).
const (
	FixtureName  = "sdd-fixture"
	FixtureEmail = "sdd-fixture@example.com"
	FixtureDate  = "2024-01-01T00:00:00+0000"
	// FixtureBranch is the explicit initial branch, matching what a null
	// global config produced for the existing frozen fixtures.
	FixtureBranch = "master"
)

// Policy is the installed hermetic environment: every path is owned by the
// test process and removed by cleanup.
type Policy struct {
	Root      string // parent of everything below
	Home      string // HOME (and USERPROFILE on Windows)
	Config    string // XDG_CONFIG_HOME
	Global    string // GIT_CONFIG_GLOBAL: the policy's explicit settings
	Templates string // GIT_TEMPLATE_DIR: empty, so no hooks are seeded
	Hooks     string // core.hooksPath: empty, so ambient hooks never run
}

// GlobalConfig is the path of the policy-owned global gitconfig.
func (p Policy) GlobalConfig() string { return p.Global }

// globalConfig is written to Policy.Global. Everything the workstation could
// have turned on is turned off explicitly rather than merely not inherited,
// and fixture-affecting defaults are pinned.
const globalConfig = `# Written by internal/testenv. Test-owned; never the user's configuration.
[core]
	fsmonitor = false
	untrackedCache = false
	autocrlf = false
	hooksPath = %s
[commit]
	gpgsign = false
[tag]
	gpgsign = false
[maintenance]
	auto = false
[gc]
	auto = 0
[init]
	defaultBranch = %s
[advice]
	detachedHead = false
`

// dropped are the inherited variables the policy removes: repository
// routing, configuration injection, prompts and helpers, editors, tracing,
// and identity, all of which the policy either pins or must not inherit.
var dropped = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_COMMON_DIR": true,
	"GIT_INDEX_FILE": true, "GIT_OBJECT_DIRECTORY": true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_NAMESPACE": true,
	"GIT_CEILING_DIRECTORIES": true, "GIT_TEMPLATE_DIR": true,
	"GIT_SSH": true, "GIT_SSH_COMMAND": true, "GIT_ASKPASS": true,
	"SSH_ASKPASS": true, "GIT_TERMINAL_PROMPT": true, "GIT_EDITOR": true,
	"GIT_SEQUENCE_EDITOR": true, "GIT_PAGER": true, "GIT_EXTERNAL_DIFF": true,
	"GIT_DIFF_OPTS": true, "GIT_PROXY_COMMAND": true, "GIT_CURL_VERBOSE": true,
	"GNUPGHOME": true, "EMAIL": true,
	"HOME": true, "XDG_CONFIG_HOME": true, "USERPROFILE": true,
	"HOMEDRIVE": true, "HOMEPATH": true,
}

// droppedPrefixes covers the enumerated families: GIT_CONFIG_* (COUNT,
// KEY_n, VALUE_n, PARAMETERS, GLOBAL, SYSTEM, NOSYSTEM), GIT_AUTHOR_*,
// GIT_COMMITTER_*, GIT_TRACE*.
var droppedPrefixes = []string{"GIT_CONFIG_", "GIT_AUTHOR_", "GIT_COMMITTER_", "GIT_TRACE"}

func isDropped(key string) bool {
	k := key
	if runtime.GOOS == "windows" {
		k = strings.ToUpper(key)
	}
	if dropped[k] {
		return true
	}
	for _, p := range droppedPrefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// Vars are the KEY=VALUE pairs the policy sets.
func (p Policy) Vars() []string {
	vars := []string{
		"HOME=" + p.Home,
		"XDG_CONFIG_HOME=" + p.Config,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_GLOBAL=" + p.Global,
		"GIT_TEMPLATE_DIR=" + p.Templates,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=" + FixtureName,
		"GIT_AUTHOR_EMAIL=" + FixtureEmail,
		"GIT_AUTHOR_DATE=" + FixtureDate,
		"GIT_COMMITTER_NAME=" + FixtureName,
		"GIT_COMMITTER_EMAIL=" + FixtureEmail,
		"GIT_COMMITTER_DATE=" + FixtureDate,
		"SDD_VCS_DISABLE_P4=1",
	}
	if runtime.GOOS == "windows" {
		vars = append(vars, "USERPROFILE="+p.Home)
	}
	return vars
}

// Env returns base (the process environment when nil) with the policy
// applied: dropped variables removed, policy variables appended. Platform
// launch essentials such as PATH, SystemRoot and TMPDIR pass through.
func (p Policy) Env(base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base)+16)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if isDropped(k) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, p.Vars()...)
}

// Apply sets the policy on the current process environment and returns a
// function that restores every variable it changed or removed.
func (p Policy) Apply() (restore func()) {
	type prev struct {
		val string
		set bool
	}
	saved := map[string]prev{}
	remember := func(k string) {
		if _, done := saved[k]; !done {
			v, ok := os.LookupEnv(k)
			saved[k] = prev{v, ok}
		}
	}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if isDropped(k) {
			remember(k)
			os.Unsetenv(k)
		}
	}
	for _, kv := range p.Vars() {
		k, v, _ := strings.Cut(kv, "=")
		remember(k)
		os.Setenv(k, v)
	}
	return func() {
		for k, pv := range saved {
			if pv.set {
				os.Setenv(k, pv.val)
			} else {
				os.Unsetenv(k)
			}
		}
	}
}

// Install creates the policy resources under an owned temporary root and
// applies them to the current process environment. Call it from TestMain
// before m.Run and call the returned cleanup explicitly before os.Exit —
// a deferred call is skipped by os.Exit.
func Install() (Policy, func(), error) {
	root, err := os.MkdirTemp("", "sdd-testenv-")
	if err != nil {
		return Policy{}, nil, err
	}
	p := Policy{
		Root:      root,
		Home:      filepath.Join(root, "home"),
		Config:    filepath.Join(root, "home", ".config"),
		Global:    filepath.Join(root, "gitconfig"),
		Templates: filepath.Join(root, "templates"),
		Hooks:     filepath.Join(root, "hooks"),
	}
	for _, d := range []string{filepath.Join(p.Config, "git"), p.Templates, p.Hooks} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			os.RemoveAll(root)
			return Policy{}, nil, err
		}
	}
	cfg := fmt.Sprintf(globalConfig, filepath.ToSlash(p.Hooks), FixtureBranch)
	if err := os.WriteFile(p.Global, []byte(cfg), 0o644); err != nil {
		os.RemoveAll(root)
		return Policy{}, nil, err
	}
	restore := p.Apply()
	cleanup := func() {
		restore()
		os.RemoveAll(root)
	}
	return p, cleanup, nil
}

// Main is the one-line TestMain body: install, run, clean up, exit.
//
//	func TestMain(m *testing.M) { testenv.Main(m) }
func Main(m interface{ Run() int }) {
	_, cleanup, err := Install()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		os.Exit(2)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
