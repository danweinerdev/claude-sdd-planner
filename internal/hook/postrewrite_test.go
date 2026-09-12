package hook

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func initCaptureRepo(t *testing.T) string {
	t.Helper()
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

func TestCapturePostRewritePreservesRawRowsAndIsUnique(t *testing.T) {
	repo := initCaptureRepo(t)
	old1 := strings.Repeat("1", 40)
	old2 := strings.Repeat("2", 40)
	newRev := strings.Repeat("a", 40)
	raw := []byte(old1 + " " + newRev + "\n" + old2 + "\t" + newRev + "\n")

	first, err := CapturePostRewrite(repo, "rebase", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	second, err := CapturePostRewrite(repo, "rebase", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatalf("captures reused %q; each raw map must be immutable and unique", first.Path)
	}
	if first.Rows != 2 {
		t.Fatalf("rows = %d, want 2", first.Rows)
	}
	got, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("capture changed raw Git input:\n got %q\nwant %q", got, raw)
	}
	info, err := os.Stat(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("capture mode = %o, want immutable (no write bits)", info.Mode().Perm())
	}
	wantDir := gitCaptureOutput(t, repo, "rev-parse", "--git-path", "sdd/rewrite-maps")
	if !filepath.IsAbs(wantDir) {
		wantDir = filepath.Join(repo, wantDir)
	}
	if filepath.Clean(filepath.Dir(first.Path)) != filepath.Clean(wantDir) {
		t.Fatalf("capture dir = %q, want Git-private %q", filepath.Dir(first.Path), wantDir)
	}
}

func TestCapturePostRewriteRejectsInvalidOrUnboundedInput(t *testing.T) {
	repo := initCaptureRepo(t)
	valid := strings.Repeat("1", 40) + " " + strings.Repeat("2", 40) + "\n"
	for _, tc := range []struct {
		name string
		kind string
		body string
	}{
		{"kind", "merge", valid},
		{"empty", "amend", ""},
		{"columns", "amend", strings.Repeat("1", 40) + "\n"},
		{"object", "amend", "not-a-revision " + strings.Repeat("2", 40) + "\n"},
		{"trailing", "amend", valid + "garbage"},
		{"oversize", "rebase", strings.Repeat("x", maxPostRewriteBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CapturePostRewrite(repo, tc.kind, strings.NewReader(tc.body)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	mapDir := gitCaptureOutput(t, repo, "rev-parse", "--git-path", "sdd/rewrite-maps")
	if !filepath.IsAbs(mapDir) {
		mapDir = filepath.Join(repo, mapDir)
	}
	entries, err := os.ReadDir(mapDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid captures wrote %d file(s)", len(entries))
	}
}

func TestCapturePostRewriteUsesInvokingLinkedWorktreeMetadata(t *testing.T) {
	repo := initCaptureRepo(t)
	gitCaptureOutput(t, repo, "-c", "user.name=SDD", "-c", "user.email=sdd@example.invalid", "commit", "--allow-empty", "-q", "-m", "base")
	linked := filepath.Join(t.TempDir(), "linked")
	gitCaptureOutput(t, repo, "worktree", "add", "-q", "-b", "linked-capture", linked)
	raw := strings.Repeat("1", 40) + " " + strings.Repeat("2", 40) + "\n"
	got, err := CapturePostRewrite(linked, "amend", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	want := gitCaptureOutput(t, linked, "rev-parse", "--git-path", "sdd/rewrite-maps")
	if !filepath.IsAbs(want) {
		want = filepath.Join(linked, want)
	}
	if filepath.Clean(filepath.Dir(got.Path)) != filepath.Clean(want) {
		t.Fatalf("linked capture dir = %q, want %q", filepath.Dir(got.Path), want)
	}
}

func gitCaptureOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
