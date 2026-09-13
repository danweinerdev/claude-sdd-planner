package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
)

func TestClaudePluginRootSkipsPortableInstallation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows os.UserHomeDir
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))

	root := filepath.Join(home, ".agents", "plugins", "claude-sdd-planner")
	if err := os.MkdirAll(filepath.Join(root, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shared", "agent-runtime.md"), []byte("# runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"name":"sdd-planner"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	codexRoot := filepath.Join(home, ".codex", "plugins", "cache", "mkt", "sdd-planner", "2.8.0")
	if err := os.MkdirAll(filepath.Join(codexRoot, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexRoot, "shared", "agent-runtime.md"), []byte("# runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexRoot, "plugin.json"), []byte(`{"name":"sdd-planner"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, source := claudePluginRoot()
	if got != "" || source != "" {
		t.Errorf("claudePluginRoot() = (%q, %q), want portable installation ignored", got, source)
	}

	if path, problem := checkHookBinary(got, source); path != "" || problem != "" {
		t.Errorf("checkHookBinary on a portable root = (%q, %q), want silence", path, problem)
	}
	if path, problem := checkHooksFile(got, source, false); path != "" || problem != "" {
		t.Errorf("checkHooksFile on a portable root = (%q, %q), want silence", path, problem)
	}
}

func TestClaudePluginRootUsesRuntimeValue(t *testing.T) {
	want := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", want)

	got, source := claudePluginRoot()
	if got != want || source != "CLAUDE_PLUGIN_ROOT" {
		t.Errorf("claudePluginRoot() = (%q, %q), want (%q, CLAUDE_PLUGIN_ROOT)", got, source, want)
	}
}

func TestDoctorCheckIsNonWritingAndNormalModeRepairsGitHook(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := chdirTemp(t)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")

	var checkErr error
	checkOut, _ := captureStdout(t, func() error {
		checkErr = cmdDoctor(doctorOpts{Check: true})
		return checkErr
	})
	var refusal *refusedError
	if !errors.As(checkErr, &refusal) || exitCode(checkErr) != 1 {
		t.Fatalf("doctor --check error = %v (exit %d), want finding/exit 1", checkErr, exitCode(checkErr))
	}
	if !strings.Contains(checkOut, "Git post-rewrite hook") || !strings.Contains(checkOut, "missing") {
		t.Fatalf("check did not clearly report missing hook:\n%s", checkOut)
	}
	hookPath := strings.TrimSpace(runDoctorGit(t, repo, "rev-parse", "--git-path", "hooks/post-rewrite"))
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repo, hookPath)
	}
	if _, err := os.Lstat(hookPath); !os.IsNotExist(err) {
		t.Fatalf("doctor --check wrote %s: %v", hookPath, err)
	}

	if out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("doctor repair: %v\n%s", err, out)
	}
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("doctor did not install hook: %v", err)
	}
	if out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); err != nil {
		t.Fatalf("doctor --check after repair: %v\n%s", err, out)
	}
	if err := os.Remove(hookPath); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); exitCode(err) != 1 {
		t.Fatalf("check after deletion exit = %d, err %v", exitCode(err), err)
	}
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("doctor did not restore deleted managed hook: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\n# sdd managed post-rewrite hook v1\n# stale\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	staleBytes, _ := os.ReadFile(hookPath)
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); exitCode(err) != 1 {
		t.Fatalf("check on drift exit = %d, err %v", exitCode(err), err)
	}
	if after, _ := os.ReadFile(hookPath); string(after) != string(staleBytes) {
		t.Fatal("doctor --check rewrote drifted hook")
	}
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("doctor did not repair drifted hook: %v", err)
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime()
	if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
		t.Fatalf("idempotent doctor: %v", err)
	}
	info, _ = os.Stat(hookPath)
	if !info.ModTime().Equal(mtime) {
		t.Fatal("idempotent doctor rewrote current hook")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(hookPath, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) }); exitCode(err) != 1 {
			t.Fatalf("check on non-executable hook exit = %d, err %v", exitCode(err), err)
		}
		if _, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) }); err != nil {
			t.Fatalf("doctor did not repair executable mode: %v", err)
		}
		info, _ = os.Stat(hookPath)
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("doctor left hook non-executable: %o", info.Mode().Perm())
		}
	}
}

func runDoctorGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestDoctorUnsafeGitHookIsOperationalFailure(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := chdirTemp(t)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	hookPath := strings.TrimSpace(runDoctorGit(t, repo, "rev-parse", "--git-path", "hooks/post-rewrite"))
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repo, hookPath)
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "user-hook")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, hookPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) })
	if exitCode(err) != 2 || !strings.Contains(out, "unsafe") {
		t.Fatalf("unsafe check exit=%d err=%v output:\n%s", exitCode(err), err, out)
	}
	if info, statErr := os.Lstat(hookPath); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("doctor changed unsafe symlink: %v, %v", info, statErr)
	}
}

func TestDoctorNoGitIsExplicitNoOp(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	chdirTemp(t)
	out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) })
	if err != nil {
		t.Fatalf("doctor in plain directory: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no-git") || !strings.Contains(out, "no repository hook") {
		t.Fatalf("doctor did not explain no-Git no-op:\n%s", out)
	}
}

// Review F-01: doctor is the one command a user runs to learn why nothing
// works. On a platform with no containment adapter it must say so as a
// blocker naming the follow-on plan, and `--check` must fail on it — silence
// there is what left Windows users with an unexplained "git could not run".
func TestDoctorReportsMissingContainmentAdapter(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	chdirTemp(t)
	restore := procexec.ContainmentProbe
	t.Cleanup(func() { procexec.ContainmentProbe = restore })
	procexec.ContainmentProbe = func() (bool, string) {
		return false, "windows: no process-containment adapter (the Windows Job Object adapter, Designs/TestSuiteReliability DD-3, is a follow-on plan)"
	}

	out, err := captureStdout(t, func() error { return cmdDoctor(doctorOpts{}) })
	if err != nil {
		t.Fatalf("doctor (repair mode) must still report, not fail: %v\n%s", err, out)
	}
	for _, want := range []string{"windows", "process-containment adapter", "Job Object"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor report does not name %q:\n%s", want, out)
		}
	}

	out, err = captureStdout(t, func() error { return cmdDoctor(doctorOpts{Check: true}) })
	if code := exitCode(err); code == 0 {
		t.Fatalf("doctor --check exit=%d err=%v, want nonzero for the missing adapter\n%s", code, err, out)
	}
	if !strings.Contains(out, "process-containment adapter") {
		t.Errorf("doctor --check does not name the missing adapter:\n%s", out)
	}
}

// TestDoctorProbeUsesOwnedRunner pins doctor's own child-process discipline.
// The AST guard in internal/provision cannot see this file, so doctor's
// hook-binary probe was still forking an uncontained, unbounded child on the
// one command a user runs when nothing works (review 06-review-fixtures-63da43f F-01).
func TestDoctorProbeUsesOwnedRunner(t *testing.T) {
	t.Run("no direct os/exec", func(t *testing.T) {
		fset := token.NewFileSet()
		const name = "doctor.go"
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquoting import %s: %v", name, imp.Path.Value, err)
			}
			if path == "os/exec" {
				t.Errorf("%s:%d imports os/exec; doctor's probes must go through procexec under a policy",
					name, fset.Position(imp.Pos()).Line)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "exec" {
				return true
			}
			t.Errorf("%s:%d calls exec.%s; use procexec.Run under a policy instead",
				name, fset.Position(call.Pos()).Line, sel.Sel.Name)
			return true
		})
	})

	t.Run("hanging hook binary is bounded", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the fake hook binary is a POSIX shell script")
		}
		root := t.TempDir()
		bin := filepath.Join(root, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatalf("creating bin dir: %v", err)
		}
		// A read from a fifo nobody writes blocks in the kernel, so the stub
		// hangs without burning CPU while the deadline runs down.
		fifo, err := blockingReadSource(filepath.Join(root, "block.fifo"))
		if err != nil {
			t.Fatalf("creating blocking read source: %v", err)
		}
		// The stub records its own pid so the timeout path below can reap it.
		// Reaching that path means the runner's bound did NOT hold, so the
		// containment adapter cannot be relied on to have cleaned up — and a
		// test proving processes leak must not itself leak one.
		pidFile := filepath.Join(root, "stub.pid")
		script := "#!/bin/sh\necho $$ > " + pidFile + "\nread -r _ < " + fifo + "\n"
		if err := os.WriteFile(filepath.Join(bin, "sdd"), []byte(script), 0o755); err != nil {
			t.Fatalf("writing fake hook binary: %v", err)
		}
		t.Cleanup(func() {
			raw, err := os.ReadFile(pidFile)
			if err != nil {
				return
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil || pid <= 0 {
				return
			}
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		})

		policy := hookProbePolicy
		t.Cleanup(func() { hookProbePolicy = policy })
		hookProbePolicy = procexec.Policy{Timeout: 300 * time.Millisecond, Cleanup: 500 * time.Millisecond}

		type outcome struct{ path, problem string }
		done := make(chan outcome, 1)
		go func() {
			p, problem := checkHookBinary(root, "CLAUDE_PLUGIN_ROOT")
			done <- outcome{p, problem}
		}()

		bound := hookProbePolicy.Timeout + hookProbePolicy.Cleanup + 3*time.Second
		select {
		case got := <-done:
			if got.problem == "" {
				t.Fatalf("checkHookBinary reported a healthy binary for a hanging probe; want a bounded failure (path %q)", got.path)
			}
			if !strings.Contains(got.problem, procexec.CauseDeadline.String()) {
				t.Fatalf("problem = %q, want it to name the %v cause", got.problem, procexec.CauseDeadline)
			}
		case <-time.After(bound):
			t.Fatalf("checkHookBinary did not return within %v against a hanging hook binary", bound)
		}
	})
}

// TestDoctorProbeSkipsWhenContainmentUnsupported pins the hook-binary probe's
// behaviour on a platform with no containment adapter. procexec.Run refuses to
// launch anything there, so probing unconditionally made doctor report every
// healthy pinned binary on Windows as one that "did not answer `version`" —
// a derived symptom masquerading as a broken installation
// (review 06-review-fixtures-57d4ffb F-01). The probe must be skipped and
// reported as not probed, mirroring the git-hook probe's wording.
func TestDoctorProbeSkipsWhenContainmentUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the healthy fake hook binary is a POSIX shell script")
	}
	chdirTemp(t)

	const reason = "windows: no process-containment adapter (the Windows Job Object adapter, Designs/TestSuiteReliability DD-3, is a follow-on plan)"
	restore := procexec.ContainmentProbe
	t.Cleanup(func() { procexec.ContainmentProbe = restore })
	procexec.ContainmentProbe = func() (bool, string) { return false, reason }

	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("creating bin dir: %v", err)
	}
	// The marker is the observation: a skipped probe never runs the binary,
	// so asserting on the report alone would pass for an implementation that
	// runs it and then discards the answer.
	marker := filepath.Join(root, "ran")
	script := "#!/bin/sh\n: > " + marker + "\necho 'sdd v9.9.9'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "sdd"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing healthy fake hook binary: %v", err)
	}
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	for _, mode := range []struct {
		name string
		opts doctorOpts
	}{
		{"repair", doctorOpts{}},
		{"check", doctorOpts{Check: true}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			out, err := captureStdout(t, func() error { return cmdDoctor(mode.opts) })
			if mode.opts.Check {
				if code := exitCode(err); code == 0 {
					t.Fatalf("doctor --check exit=%d err=%v, want nonzero for the missing adapter\n%s", code, err, out)
				}
			} else if err != nil {
				t.Fatalf("doctor (repair mode) must still report, not fail: %v\n%s", err, out)
			}

			var line string
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, "hook binary:") {
					line = l
					break
				}
			}
			if line == "" {
				t.Fatalf("doctor printed no hook-binary line:\n%s", out)
			}
			if !strings.Contains(line, "not probed") || !strings.Contains(line, reason) {
				t.Errorf("hook binary line = %q, want it to read as not probed with the platform reason", line)
			}
			for _, unwanted := range []string{"did not answer", "not executable"} {
				if strings.Contains(line, unwanted) {
					t.Errorf("hook binary line = %q, must not report a broken binary (%q)", line, unwanted)
				}
			}
			if _, err := os.Stat(marker); err == nil {
				t.Errorf("doctor executed the pinned binary on a platform with no containment adapter (marker %s exists)", marker)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat marker: %v", err)
			}
		})
	}
}
