package provision

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func gitHookRepo(t *testing.T) string {
	t.Helper()
	// Never install into a developer's globally configured hooks directory.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func TestPostRewriteLockReleasedAfterProcessExit(t *testing.T) {
	const env = "SDD_TEST_REWRITE_EXIT_LOCK"
	if path := os.Getenv(env); path != "" {
		if _, err := acquirePostRewriteLock(path); err != nil {
			os.Exit(2)
		}
		// Model an interrupted doctor: no deferred lock release runs.
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "install")
	cmd := exec.Command(os.Args[0], "-test.run=^TestPostRewriteLockReleasedAfterProcessExit$")
	cmd.Env = append(os.Environ(), env+"="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock holder: %v\n%s", err, out)
	}
	release, err := acquirePostRewriteLock(path)
	if err != nil {
		t.Fatalf("doctor cannot recover after an interrupted lock holder: %v", err)
	}
	release()
}

func TestPostRewriteResumesInterruptedUserHookBackup(t *testing.T) {
	repo := gitHookRepo(t)
	report, err := CheckPostRewrite(repo)
	if err != nil {
		t.Fatal(err)
	}
	user := []byte("#!/bin/sh\nexit 7\n")
	if err := os.WriteFile(report.HookPath, user, 0o755); err != nil {
		t.Fatal(err)
	}
	// Model exit after preserving the user inode but before installing the
	// dispatcher. This must remain distinct from a conflicting backup file.
	if err := os.Link(report.HookPath, report.BackupPath); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if _, err := InstallPostRewrite(repo); err != nil {
		t.Fatalf("interrupted preservation was not recoverable: %v", err)
	}
	if raw, err := os.ReadFile(report.BackupPath); err != nil || !bytes.Equal(raw, user) {
		t.Fatalf("original user hook was not preserved: %q, %v", raw, err)
	}
}

func TestPostRewriteWarnsForOldCaptureBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture")
	}
	repo := gitHookRepo(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sdd"), []byte("#!/bin/sh\nprintf 'Available Commands:\\n  sessionstart  Session context\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	report, err := CheckPostRewrite(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Warning, "does not expose") {
		t.Fatalf("doctor would imply an older PATH binary can capture rewrites: %+v", report)
	}
}

func runFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPostRewriteAbsentInstallCheckAndRepair(t *testing.T) {
	repo := gitHookRepo(t)
	before, err := CheckPostRewrite(repo)
	if err != nil {
		t.Fatal(err)
	}
	if before.State != PostRewriteMissing {
		t.Fatalf("fresh state = %s, want missing", before.State)
	}
	if _, err := os.Lstat(before.HookPath); !os.IsNotExist(err) {
		t.Fatalf("check wrote hook or returned unexpected error: %v", err)
	}

	installed, err := InstallPostRewrite(repo)
	if err != nil {
		t.Fatal(err)
	}
	if installed.State != PostRewriteCurrent || !installed.Changed {
		t.Fatalf("install = %+v", installed)
	}
	info, err := os.Stat(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed hook mode = %o, want executable", info.Mode().Perm())
	}
	mtime := info.ModTime()
	time.Sleep(20 * time.Millisecond)
	again, err := InstallPostRewrite(repo)
	if err != nil || again.Changed {
		t.Fatalf("idempotent install = %+v, err %v", again, err)
	}
	info, _ = os.Stat(installed.HookPath)
	if !info.ModTime().Equal(mtime) {
		t.Error("idempotent install changed hook mtime")
	}

	if err := os.Remove(installed.HookPath); err != nil {
		t.Fatal(err)
	}
	restored, err := InstallPostRewrite(repo)
	if err != nil || !restored.Changed || restored.State != PostRewriteCurrent {
		t.Fatalf("restore deleted hook = %+v, err %v", restored, err)
	}
	if err := os.WriteFile(restored.HookPath, []byte("#!/bin/sh\n# sdd managed post-rewrite hook v1\n# drift\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if state, _ := CheckPostRewrite(repo); state.State != PostRewriteStale {
		t.Fatalf("drift state = %s, want stale", state.State)
	}
	if fixed, err := InstallPostRewrite(repo); err != nil || !fixed.Changed {
		t.Fatalf("drift repair = %+v, err %v", fixed, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(restored.HookPath, 0o644); err != nil {
			t.Fatal(err)
		}
		if state, _ := CheckPostRewrite(repo); state.State != PostRewriteNotExecutable {
			t.Fatalf("mode state = %s, want not-executable", state.State)
		}
		if fixed, err := InstallPostRewrite(repo); err != nil || !fixed.Changed {
			t.Fatalf("mode repair = %+v, err %v", fixed, err)
		}
	}
}

func TestPostRewritePreservesUserHookInputArgsAndExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dispatcher execution assertion uses POSIX sh")
	}
	repo := gitHookRepo(t)
	state, _ := CheckPostRewrite(repo)
	if err := os.MkdirAll(filepath.Dir(state.HookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	userBody := fmt.Sprintf("#!/bin/sh\nprintf 'args:%%s\\n' \"$*\" > %q\ncat > %q\nexit 7\n", filepath.Join(logDir, "user.args"), filepath.Join(logDir, "user.stdin"))
	if err := os.WriteFile(state.HookPath, []byte(userBody), 0o755); err != nil {
		t.Fatal(err)
	}
	installed, err := InstallPostRewrite(repo)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(installed.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != userBody {
		t.Fatal("user hook backup did not preserve bytes")
	}

	binDir := t.TempDir()
	sddBody := fmt.Sprintf("#!/bin/sh\nprintf 'args:%%s\\n' \"$*\" > %q\ncat > %q\nexit 9\n", filepath.Join(logDir, "sdd.args"), filepath.Join(logDir, "sdd.stdin"))
	if err := os.WriteFile(filepath.Join(binDir, "sdd"), []byte(sddBody), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(installed.HookPath, "rebase", "extra")
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, "TMPDIR="+logDir)
	input := []byte(strings.Repeat("1", 40) + " " + strings.Repeat("2", 40) + "\n")
	cmd.Stdin = bytes.NewReader(input)
	err = cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 7 {
		t.Fatalf("dispatcher exit = %v, want original user-hook exit 7", err)
	}
	for _, name := range []string{"user.stdin", "sdd.stdin"} {
		got, err := os.ReadFile(filepath.Join(logDir, name))
		if err != nil || !bytes.Equal(got, input) {
			t.Fatalf("%s = %q, %v; want identical stdin", name, got, err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(logDir, "user.args")); string(got) != "args:rebase extra\n" {
		t.Fatalf("user args = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(logDir, "sdd.args")); string(got) != "args:hook post-rewrite rebase extra\n" {
		t.Fatalf("sdd args = %q", got)
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "sdd-post-rewrite.") {
			t.Fatalf("dispatcher leaked spool file %s", entry.Name())
		}
	}
}

func TestPostRewriteResolvesHooksPathAndLinkedWorktree(t *testing.T) {
	repo := gitHookRepo(t)
	rel := filepath.Join("custom", "hooks")
	runFixtureGit(t, repo, "config", "core.hooksPath", rel)
	state, err := InstallPostRewrite(repo)
	if err != nil {
		t.Fatal(err)
	}
	if state.HookPath != filepath.Join(repo, rel, "post-rewrite") {
		t.Fatalf("relative hooksPath resolved to %q", state.HookPath)
	}
	if got := runFixtureGit(t, repo, "config", "--get", "core.hooksPath"); got != rel {
		t.Fatalf("installation altered relative core.hooksPath: %q", got)
	}
	abs := filepath.Join(t.TempDir(), "absolute-hooks")
	runFixtureGit(t, repo, "config", "core.hooksPath", abs)
	state, err = InstallPostRewrite(repo)
	if err != nil || state.HookPath != filepath.Join(abs, "post-rewrite") {
		t.Fatalf("absolute hooksPath install = %+v, %v", state, err)
	}
	if got := runFixtureGit(t, repo, "config", "--get", "core.hooksPath"); got != abs {
		t.Fatalf("installation altered absolute core.hooksPath: %q", got)
	}

	runFixtureGit(t, repo, "config", "--unset", "core.hooksPath")
	file := filepath.Join(repo, "tracked")
	if err := os.WriteFile(file, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, repo, "add", "tracked")
	runFixtureGit(t, repo, "-c", "user.name=SDD", "-c", "user.email=sdd@example.invalid", "commit", "-q", "-m", "base")
	linked := filepath.Join(t.TempDir(), "linked")
	runFixtureGit(t, repo, "worktree", "add", "-q", "-b", "linked", linked)
	linkedState, err := InstallPostRewrite(linked)
	if err != nil {
		t.Fatal(err)
	}
	common := runFixtureGit(t, linked, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(common) {
		common = filepath.Join(linked, common)
	}
	if linkedState.HookPath != filepath.Join(filepath.Clean(common), "hooks", "post-rewrite") {
		t.Fatalf("linked-worktree hook = %q, want common-dir hook", linkedState.HookPath)
	}
}

func TestPostRewriteRefusesSymlinkAndBackupCollision(t *testing.T) {
	repo := gitHookRepo(t)
	state, _ := CheckPostRewrite(repo)
	if err := os.MkdirAll(filepath.Dir(state.HookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("user"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, state.HookPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if checked, _ := CheckPostRewrite(repo); checked.State != PostRewriteUnsafe {
		t.Fatalf("symlink state = %s, want unsafe", checked.State)
	}
	if _, err := InstallPostRewrite(repo); err == nil {
		t.Fatal("install overwrote an unsafe symlink")
	}
	if err := os.Remove(state.HookPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.HookPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.BackupPath, []byte("collision"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPostRewrite(repo); err == nil {
		t.Fatal("install overwrote a user hook despite backup collision")
	}
}

func TestPostRewriteConcurrentInstallPreservesOneUserBackup(t *testing.T) {
	repo := gitHookRepo(t)
	state, _ := CheckPostRewrite(repo)
	if err := os.MkdirAll(filepath.Dir(state.HookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const userHook = "#!/bin/sh\nexit 3\n"
	if err := os.WriteFile(state.HookPath, []byte(userHook), 0o755); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := InstallPostRewrite(repo)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent install: %v", err)
		}
	}
	backup, err := os.ReadFile(state.BackupPath)
	if err != nil || string(backup) != userHook {
		t.Fatalf("backup after concurrent install = %q, %v", backup, err)
	}
	checked, err := CheckPostRewrite(repo)
	if err != nil || checked.State != PostRewriteCurrent {
		t.Fatalf("state after concurrent install = %+v, %v", checked, err)
	}
}

func TestPostRewriteNoGitAndBareAreExplicitNoOps(t *testing.T) {
	plain := t.TempDir()
	state, err := InstallPostRewrite(plain)
	if err != nil || state.State != PostRewriteNoGit || !strings.Contains(state.Detail, "no repository hook") {
		t.Fatalf("plain directory = %+v, %v", state, err)
	}
	bare := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	state, err = InstallPostRewrite(bare)
	if err != nil || state.State != PostRewriteBare || !strings.Contains(state.Detail, "unsupported") {
		t.Fatalf("bare repository = %+v, %v", state, err)
	}
}
