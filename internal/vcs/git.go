package vcs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/procexec"
)

func init() {
	RegisterProbe(probeGit)
}

// gitHex40 is the exact grammar scripts/sdd_validate.py accepts for a Git
// revision/checkpoint: a full 40-character hex object name.
var gitHex40 = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// gitRepo implements Repo for a normal git working tree, a linked worktree,
// or a bare repository, using exactly the plain-git commands
// shared/vcs-detection.md and scripts/sdd_validate.py already use.
type gitRepo struct {
	kind Kind
	dir  string // the directory Detect was called with
	root string // resolved repository root
}

// probeGit implements the git portion of shared/vcs-detection.md's detection
// algorithm (steps 1-5). It returns (nil, nil) when dir is not a git
// repository at all, so Detect falls through to the next probe, and an
// operational error when git itself could not answer: a directory with no
// `.git` marker is undecidable without a working git, and a directory with
// one cannot resolve its root.
func probeGit(dir string) (Repo, error) {
	if isDir(filepath.Join(dir, ".bare")) {
		return newGitRepo(dir, GitBare)
	}
	if isRegularFile(filepath.Join(dir, ".git")) {
		return newGitRepo(dir, GitWorktree)
	}
	if isDir(filepath.Join(dir, ".git")) {
		return newGitRepo(dir, Git)
	}
	out, err := runGit(dir, "rev-parse", "--is-bare-repository")
	if errors.Is(err, ErrOperational) {
		return nil, err
	}
	if err == nil && strings.TrimSpace(string(out)) == "true" {
		return newGitRepo(dir, GitBare)
	}
	_, err = runGit(dir, "rev-parse", "--git-dir")
	if errors.Is(err, ErrOperational) {
		return nil, err
	}
	if err == nil {
		return newGitRepo(dir, Git)
	}
	return nil, nil
}

func newGitRepo(dir string, kind Kind) (Repo, error) {
	root := dir
	if kind != GitBare {
		out, err := runGit(dir, "rev-parse", "--show-toplevel")
		if errors.Is(err, ErrOperational) {
			return nil, err
		}
		if err == nil {
			if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
				root = trimmed
			}
		}
	}
	// Root() is compared against OS-derived paths by every containment
	// check; git reports the long-form, forward-slash spelling while the OS
	// side may be short-named (Windows 8.3) or unresolved (/tmp on macOS).
	// Canonicalize once here so callers compare like with like.
	return &gitRepo{kind: kind, dir: dir, root: CanonPath(filepath.FromSlash(root))}, nil
}

// runGit builds an argv slice and runs git through the bounded runner —
// never a shell — so no path or revision string can be interpreted as a
// second command. A nonzero git exit comes back as an ordinary error the
// operation adapter interprets; every other failure (no git, permission,
// deadline, output overflow, drain) wraps ErrOperational.
func runGit(dir string, args ...string) ([]byte, error) {
	full := append([]string{"-C", dir}, args...)
	res, err := procexec.Run(context.Background(), "git", full, procexec.Policy{})
	if err != nil {
		var pe *procexec.Error
		if errors.As(err, &pe) && pe.Cause == procexec.CauseExit {
			return nil, fmt.Errorf("git %s: exit %d: %s", strings.Join(args, " "), pe.ExitCode, strings.TrimSpace(pe.Stderr))
		}
		return nil, fmt.Errorf("%w: git %s: %w", ErrOperational, strings.Join(args, " "), err)
	}
	return res.Stdout, nil
}

// absent maps a failed object query to its authoritative answer: an
// operational failure stays operational; a git exit means the object is not
// there.
func absent(err error, what string) error {
	if errors.Is(err, ErrOperational) {
		return err
	}
	return fmt.Errorf("%w: %s", ErrNotFound, what)
}

func (g *gitRepo) Kind() Kind   { return g.kind }
func (g *gitRepo) Root() string { return g.root }

func (g *gitRepo) RevisionSyntaxValid(s string) bool { return gitHex40.MatchString(s) }

func (g *gitRepo) RevisionExists(rev string) (bool, error) {
	out, err := runGit(g.root, "cat-file", "-t", rev)
	if err != nil {
		return false, absent(err, rev)
	}
	return strings.TrimSpace(string(out)) == "commit", nil
}

func (g *gitRepo) Head() (string, error) {
	out, err := runGit(g.root, "rev-parse", "HEAD")
	if err != nil {
		return "", absent(err, "HEAD")
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *gitRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	_, err := procexec.Run(context.Background(), "git",
		[]string{"-C", g.root, "merge-base", "--is-ancestor", ancestor, descendant}, procexec.Policy{})
	if err == nil {
		return true, nil
	}
	var pe *procexec.Error
	if errors.As(err, &pe) && pe.Cause == procexec.CauseExit {
		if pe.ExitCode == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git merge-base --is-ancestor %s %s: exit %d: %s", ancestor, descendant, pe.ExitCode, strings.TrimSpace(pe.Stderr))
	}
	return false, fmt.Errorf("%w: git merge-base --is-ancestor %s %s: %w", ErrOperational, ancestor, descendant, err)
}

func (g *gitRepo) Parents(rev string) ([]string, error) {
	out, err := runGit(g.root, "show", "-s", "--format=%P", rev)
	if err != nil {
		return nil, absent(err, rev)
	}
	fields := strings.Fields(string(out))
	return fields, nil
}

func (g *gitRepo) FileAt(rev, relPath string) ([]byte, error) {
	out, err := runGit(g.root, "show", rev+":"+relPath)
	if err != nil {
		return nil, absent(err, rev+":"+relPath)
	}
	return out, nil
}

// TrackedPaths lists paths recorded at rev under the given prefixes.
func (g *gitRepo) TrackedPaths(rev string, prefixes []string) ([]string, error) {
	args := []string{"ls-tree", "-r", "--name-only", "-z", rev, "--"}
	args = append(args, prefixes...)
	out, err := runGit(g.root, args...)
	if err != nil {
		return nil, absent(err, rev)
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// FileInIndex returns a path's staged contents.
func (g *gitRepo) FileInIndex(relPath string) ([]byte, error) {
	out, err := runGit(g.root, "show", ":"+relPath)
	if err != nil {
		return nil, absent(err, ":"+relPath)
	}
	return out, nil
}

func (g *gitRepo) ChangedPaths(rev string) ([]string, error) {
	out, err := runGit(g.root, "diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", "-m", "-z", rev)
	if err != nil {
		return nil, absent(err, rev)
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func (g *gitRepo) RevisionsAfter(rev string) ([]string, error) {
	out, err := runGit(g.root, "rev-list", rev+"..HEAD")
	if err != nil {
		return nil, absent(err, rev+"..HEAD")
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

func (g *gitRepo) Clean() (bool, []string, error) {
	out, err := runGit(g.root, "status", "--porcelain", "--ignore-submodules=none", "--untracked-files=all")
	if err != nil {
		return false, nil, fmt.Errorf("git status: %w", err)
	}
	var dirty []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		// Porcelain lines are "XY path" or "XY orig -> path" for renames; the
		// status codes occupy the first two columns.
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		dirty = append(dirty, path)
	}
	return len(dirty) == 0, dirty, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
