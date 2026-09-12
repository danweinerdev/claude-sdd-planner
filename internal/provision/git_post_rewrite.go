package provision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/store"
)

const postRewriteMarker = "# sdd managed post-rewrite hook v1"

const postRewriteDispatcher = `#!/bin/sh
# sdd managed post-rewrite hook v1
# Managed by sdd doctor. User hook, when present: post-rewrite.sdd-user

user_hook="$0.sdd-user"
tmp_root=${TMPDIR:-${TEMP:-/tmp}}
tmp=$(mktemp "${tmp_root%/}/sdd-post-rewrite.XXXXXX")
if [ -z "$tmp" ]; then
  echo "sdd post-rewrite: warning: could not create a temporary spool file" >&2
  if [ -x "$user_hook" ]; then
    "$user_hook" "$@"
    exit $?
  fi
  exit 0
fi
trap 'rm -f "$tmp"' 0 HUP INT TERM

spooled=1
if ! cat >"$tmp"; then
  spooled=0
  echo "sdd post-rewrite: warning: could not spool hook stdin completely; rewrite map NOT captured (a partial map would under-record lineage)" >&2
fi

user_status=0
if [ -x "$user_hook" ]; then
  "$user_hook" "$@" <"$tmp"
  user_status=$?
fi

if [ "$spooled" -ne 1 ]; then
  :
elif command -v sdd >/dev/null 2>&1; then
  if ! sdd hook post-rewrite "$@" <"$tmp"; then
    echo "sdd post-rewrite: warning: capture failed; run 'sdd hook post-rewrite $1' manually if needed" >&2
  fi
else
  echo "sdd post-rewrite: warning: sdd is not on PATH; rewrite map was not captured" >&2
fi

exit "$user_status"
`

// PostRewriteState is the read-only diagnosis of the repository's effective
// post-rewrite hook.
type PostRewriteState string

const (
	PostRewriteCurrent       PostRewriteState = "current"
	PostRewriteMissing       PostRewriteState = "missing"
	PostRewriteUserHook      PostRewriteState = "user-hook"
	PostRewriteStale         PostRewriteState = "stale"
	PostRewriteNotExecutable PostRewriteState = "not-executable"
	PostRewriteUnsafe        PostRewriteState = "unsafe"
	PostRewriteNoGit         PostRewriteState = "no-git"
	PostRewriteBare          PostRewriteState = "bare-unsupported"
)

// PostRewriteReport contains paths resolved from the target repository, never
// from the plugin installation or the sdd source checkout.
type PostRewriteReport struct {
	State      PostRewriteState
	HookPath   string
	BackupPath string
	Detail     string
	SDDPath    string
	Warning    string
	Changed    bool
}

type postRewriteLocation struct {
	state      PostRewriteState
	hookPath   string
	backupPath string
	detail     string
}

// CheckPostRewrite is strictly read-only. It resolves core.hooksPath (including
// relative paths), default common-dir hooks for linked worktrees, and all
// safety findings without creating the hooks directory or a lock sidecar.
func CheckPostRewrite(cwd string) (PostRewriteReport, error) {
	loc, err := resolvePostRewrite(cwd)
	if err != nil {
		return PostRewriteReport{}, err
	}
	return inspectPostRewrite(loc)
}

// InstallPostRewrite installs or repairs only sdd's managed dispatcher. A
// pre-existing user hook is first preserved at the sibling .sdd-user path.
// The lock serializes cooperating doctor processes; O_EXCL/hard-link and a
// fresh inspection under that lock provide the compare-and-swap boundary.
func InstallPostRewrite(cwd string) (PostRewriteReport, error) {
	loc, err := resolvePostRewrite(cwd)
	if err != nil {
		return PostRewriteReport{}, err
	}
	if loc.state == PostRewriteNoGit || loc.state == PostRewriteBare {
		return inspectPostRewrite(loc)
	}
	if err := os.MkdirAll(filepath.Dir(loc.hookPath), 0o755); err != nil {
		return PostRewriteReport{}, fmt.Errorf("post-rewrite hook: creating hooks directory: %w", err)
	}
	release, err := acquirePostRewriteLock(loc.hookPath + ".sdd-install.lock")
	if err != nil {
		return PostRewriteReport{}, err
	}
	defer release()

	report, err := inspectPostRewrite(loc)
	if err != nil {
		return PostRewriteReport{}, err
	}
	switch report.State {
	case PostRewriteCurrent:
		return report, nil
	case PostRewriteUnsafe:
		return report, fmt.Errorf("post-rewrite hook: refusing unsafe state at %s: %s", report.HookPath, report.Detail)
	case PostRewriteNotExecutable:
		if err := repairPostRewriteMode(report.HookPath); err != nil {
			return report, fmt.Errorf("post-rewrite hook: restoring executable permission: %w", err)
		}
		report.State, report.Detail, report.Changed = PostRewriteCurrent, "managed dispatcher is current and executable", true
		return report, nil
	}
	expectedInfo, expectedRaw, expectedAbsent, err := postRewriteSnapshot(report.HookPath)
	if err != nil {
		return report, err
	}

	tmp, err := writePostRewriteTemp(report.HookPath)
	if err != nil {
		return report, err
	}
	defer os.Remove(tmp)

	backupCreated := false
	if report.State == PostRewriteUserHook {
		// A hard link is a filesystem CAS: it both preserves the exact inode and
		// refuses if another process/user created the backup name first.
		if err := os.Link(report.HookPath, report.BackupPath); err != nil {
			if !os.IsExist(err) {
				return report, fmt.Errorf("post-rewrite hook: preserving user hook at %s: %w", report.BackupPath, err)
			}
			// A prior interrupted installation may already have preserved the
			// same inode. The identity check below distinguishes that from a
			// user backup collision, which remains a refusal.
		} else {
			backupCreated = true
		}
		backupInfo, err := os.Lstat(report.BackupPath)
		if err != nil || backupInfo.Mode()&os.ModeSymlink != 0 || !backupInfo.Mode().IsRegular() ||
			expectedInfo == nil || !os.SameFile(expectedInfo, backupInfo) {
			if backupCreated {
				_ = os.Remove(report.BackupPath)
			}
			return report, fmt.Errorf("post-rewrite hook: user hook changed while its backup was being created")
		}
	}
	if err := verifyPostRewriteSnapshot(report.HookPath, expectedInfo, expectedRaw, expectedAbsent); err != nil {
		if backupCreated {
			_ = os.Remove(report.BackupPath)
		}
		return report, err
	}
	if err := replaceHookFile(tmp, report.HookPath); err != nil {
		if backupCreated {
			_ = os.Remove(report.BackupPath)
		}
		return report, fmt.Errorf("post-rewrite hook: installing managed dispatcher: %w", err)
	}
	report.State, report.Detail, report.Changed = PostRewriteCurrent, "managed dispatcher is current and executable", true
	return report, nil
}

func resolvePostRewrite(cwd string) (postRewriteLocation, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: git is not installed or not on PATH: %w", err)
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: resolving cwd: %w", err)
	}
	if info, err := os.Stat(absCWD); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: invalid cwd %s: %w", absCWD, err)
	}
	bare, err := gitOutput(absCWD, "rev-parse", "--is-bare-repository")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
			return postRewriteLocation{state: PostRewriteNoGit, detail: "current directory is not a Git repository; no repository hook to install"}, nil
		}
		return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: probing Git repository: %w", err)
	}
	if strings.TrimSpace(bare) == "true" {
		return postRewriteLocation{state: PostRewriteBare, detail: "bare Git repositories do not have a worktree post-rewrite capture target; hook installation is unsupported"}, nil
	}
	top, err := gitOutput(absCWD, "rev-parse", "--show-toplevel")
	if err != nil {
		return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: resolving worktree root: %w", err)
	}
	top = filepath.Clean(filepath.FromSlash(strings.TrimSpace(top)))

	hooksDir := ""
	configured, found, err := gitConfigPath(absCWD, "core.hooksPath")
	if err != nil {
		return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: reading core.hooksPath: %w", err)
	}
	if found {
		configured = filepath.FromSlash(strings.TrimSpace(configured))
		if configured == "" {
			return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: core.hooksPath is configured as an empty path")
		}
		if filepath.IsAbs(configured) {
			hooksDir = configured
		} else {
			hooksDir = filepath.Join(top, configured)
		}
	} else {
		common, err := gitOutput(absCWD, "rev-parse", "--git-common-dir")
		if err != nil {
			return postRewriteLocation{}, fmt.Errorf("post-rewrite hook: resolving Git common directory: %w", err)
		}
		common = filepath.FromSlash(strings.TrimSpace(common))
		if !filepath.IsAbs(common) {
			common = filepath.Join(absCWD, common)
		}
		hooksDir = filepath.Join(common, "hooks")
	}
	hookPath := filepath.Clean(filepath.Join(hooksDir, "post-rewrite"))
	return postRewriteLocation{hookPath: hookPath, backupPath: hookPath + ".sdd-user"}, nil
}

func inspectPostRewrite(loc postRewriteLocation) (PostRewriteReport, error) {
	report := PostRewriteReport{State: loc.state, HookPath: loc.hookPath, BackupPath: loc.backupPath, Detail: loc.detail}
	if loc.state == PostRewriteNoGit || loc.state == PostRewriteBare {
		return report, nil
	}
	if sdd, err := exec.LookPath("sdd"); err == nil {
		report.SDDPath = sdd
		report.Warning = postRewriteBinaryWarning(sdd)
	} else {
		report.Warning = "sdd is not on PATH; the dispatcher can be installed, but capture will warn and leave user hook behavior unchanged"
	}

	backupInfo, backupErr := os.Lstat(loc.backupPath)
	backupExists := backupErr == nil
	if backupErr != nil && !os.IsNotExist(backupErr) {
		return report, fmt.Errorf("post-rewrite hook: inspecting backup %s: %w", loc.backupPath, backupErr)
	}
	if backupExists && (backupInfo.Mode()&os.ModeSymlink != 0 || !backupInfo.Mode().IsRegular()) {
		report.State = PostRewriteUnsafe
		report.Detail = "backup path is a symlink or non-regular file; refusing to overwrite user data"
		return report, nil
	}

	info, err := os.Lstat(loc.hookPath)
	if os.IsNotExist(err) {
		report.State = PostRewriteMissing
		if backupExists {
			report.Detail = "managed dispatcher is missing; preserved user hook backup is present"
		} else {
			report.Detail = "managed dispatcher is missing"
		}
		return report, nil
	}
	if err != nil {
		return report, fmt.Errorf("post-rewrite hook: inspecting %s: %w", loc.hookPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		report.State = PostRewriteUnsafe
		report.Detail = "hook path is a symlink or non-regular file; refusing to replace it"
		return report, nil
	}
	raw, err := os.ReadFile(loc.hookPath)
	if err != nil {
		return report, fmt.Errorf("post-rewrite hook: reading %s: %w", loc.hookPath, err)
	}
	managed := bytes.HasPrefix(raw, []byte("#!/bin/sh\n"+postRewriteMarker+"\n"))
	if !managed {
		if backupExists && os.SameFile(info, backupInfo) {
			report.State = PostRewriteUserHook
			report.Detail = "user hook was preserved by an interrupted installation; dispatcher installation can resume"
		} else if backupExists {
			report.State = PostRewriteUnsafe
			report.Detail = "unmanaged user hook and sibling backup both exist; refusing backup collision"
		} else {
			report.State = PostRewriteUserHook
			report.Detail = "unmanaged user hook will be preserved before dispatcher installation"
		}
		return report, nil
	}
	if string(raw) != postRewriteDispatcher {
		report.State = PostRewriteStale
		report.Detail = "managed dispatcher content has drifted"
		return report, nil
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		report.State = PostRewriteNotExecutable
		report.Detail = "managed dispatcher is current but not executable"
		return report, nil
	}
	report.State = PostRewriteCurrent
	report.Detail = "managed dispatcher is current and executable"
	return report, nil
}

func postRewriteBinaryWarning(binary string) string {
	// Presence (or the same version label on an unreleased build) does not
	// establish command support. Probe the non-mutating command catalog so an
	// older PATH binary cannot silently make a current dispatcher ineffective.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "hook", "--help").Output()
	if err != nil {
		return "could not verify post-rewrite support in sdd on PATH; ensure a compatible user-installed binary is available"
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "post-rewrite" {
			return ""
		}
	}
	return "sdd on PATH does not expose hook post-rewrite; update the user-installed binary before relying on capture"
}

func gitOutput(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func gitConfigPath(cwd, key string) (value string, found bool, err error) {
	cmd := exec.Command("git", "-C", cwd, "config", "--path", "--get", key)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if err == nil {
		return strings.TrimSpace(stdout.String()), true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
}

func acquirePostRewriteLock(path string) (func(), error) {
	return store.AcquireExclusiveLock(path)
}

func writePostRewriteTemp(hookPath string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(hookPath), ".post-rewrite.sdd-tmp-*")
	if err != nil {
		return "", fmt.Errorf("post-rewrite hook: creating install temp file: %w", err)
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.WriteString(f, postRewriteDispatcher); err != nil {
		return "", fmt.Errorf("post-rewrite hook: writing install temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("post-rewrite hook: syncing install temp file: %w", err)
	}
	if err := f.Chmod(0o755); err != nil {
		return "", fmt.Errorf("post-rewrite hook: setting dispatcher executable: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("post-rewrite hook: closing install temp file: %w", err)
	}
	ok = true
	return path, nil
}

func postRewriteSnapshot(path string) (os.FileInfo, []byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil, true, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("post-rewrite hook: taking install snapshot: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, false, fmt.Errorf("post-rewrite hook: hook became a symlink or non-regular file before repair")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false, fmt.Errorf("post-rewrite hook: reading install snapshot: %w", err)
	}
	return info, raw, false, nil
}

func verifyPostRewriteSnapshot(path string, expected os.FileInfo, raw []byte, absent bool) error {
	info, err := os.Lstat(path)
	if absent {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("post-rewrite hook: hook appeared during repair; refusing to overwrite it")
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(expected, info) {
		return fmt.Errorf("post-rewrite hook: hook changed during repair; refusing to overwrite it")
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, raw) {
		return fmt.Errorf("post-rewrite hook: hook content changed during repair; refusing to overwrite it")
	}
	return nil
}

func repairPostRewriteMode(path string) error {
	expected, err := os.Lstat(path)
	if err != nil || expected.Mode()&os.ModeSymlink != 0 || !expected.Mode().IsRegular() {
		return fmt.Errorf("managed dispatcher changed before mode repair")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(expected, opened) {
		return fmt.Errorf("managed dispatcher changed before mode repair")
	}
	raw, err := io.ReadAll(f)
	if err != nil || string(raw) != postRewriteDispatcher {
		return fmt.Errorf("managed dispatcher content changed before mode repair")
	}
	return f.Chmod(0o755)
}

func replaceHookFile(tmp, hookPath string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(tmp, hookPath)
	}
	// Windows rename does not replace an existing destination. Move the old
	// file aside first, and roll it back if the second rename fails; never
	// create a failure window in which the user's only hook bytes are deleted.
	displaced := tmp + ".replaced"
	hadOld := false
	if _, err := os.Lstat(hookPath); err == nil {
		if err := os.Rename(hookPath, displaced); err != nil {
			return err
		}
		hadOld = true
	}
	if err := os.Rename(tmp, hookPath); err != nil {
		if hadOld {
			_ = os.Rename(displaced, hookPath)
		}
		return err
	}
	if hadOld {
		_ = os.Remove(displaced)
	}
	return nil
}
